package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const loginPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>{{.AppName}} — Sign in</title>
<style>
  body { font-family: ui-sans-serif, system-ui, sans-serif; background:#111; color:#eee; margin:0; display:flex; min-height:100vh; align-items:center; justify-content:center; }
  .card { background:#1c1c1c; border:1px solid #333; border-radius:12px; padding:28px; width:100%; max-width:380px; }
  h1 { font-size:1.15rem; margin:0 0 6px; }
  p { color:#aaa; font-size:.9rem; margin:0 0 18px; }
  label { display:block; font-size:.8rem; margin:12px 0 4px; color:#bbb; }
  input { width:100%; box-sizing:border-box; padding:10px 12px; border-radius:8px; border:1px solid #444; background:#111; color:#fff; }
  button { margin-top:18px; width:100%; padding:10px; border:0; border-radius:8px; background:#c44; color:#fff; font-weight:600; cursor:pointer; }
  .err { background:#3a1515; color:#f88; padding:8px 10px; border-radius:8px; font-size:.85rem; margin-bottom:12px; }
</style>
</head>
<body>
<form class="card" method="post" action="{{.Action}}">
  <h1>Connect Grok to {{.AppName}}</h1>
  <p>Sign in with your Byzantine account. The token will include your org and tenant.</p>
  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
  <label>Email</label>
  <input type="email" name="email" required autocomplete="username"/>
  <label>Password</label>
  <input type="password" name="password" required autocomplete="current-password"/>
  <input type="hidden" name="client_id" value="{{.ClientID}}"/>
  <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}"/>
  <input type="hidden" name="state" value="{{.State}}"/>
  <input type="hidden" name="code_challenge" value="{{.Challenge}}"/>
  <input type="hidden" name="scope" value="{{.Scope}}"/>
  <button type="submit">Allow access</button>
</form>
</body>
</html>`

var loginTmpl = template.Must(template.New("login").Parse(loginPage))

func (a *app) publicBase() string {
	return strings.TrimRight(a.publicURL, "/")
}

// brandFrom returns the Brand matching the request's Host (or X-Forwarded-Host),
// or nil when the request is for the default Byzantine domain.
func (a *app) brandFrom(r *http.Request) *Brand {
	if len(a.brands) == 0 {
		return nil
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	host = strings.TrimSpace(strings.SplitN(host, ",", 2)[0])
	// strip port if present (handles host:port but not IPv6 brackets)
	if i := strings.LastIndex(host, ":"); i != -1 && strings.IndexByte(host, '[') == -1 {
		host = host[:i]
	}
	if b, ok := a.brands[host]; ok {
		return &b
	}
	return nil
}

func (a *app) handlePRM(w http.ResponseWriter, r *http.Request) {
	base := a.publicBase()
	if b := a.brandFrom(r); b != nil {
		base = strings.TrimRight(b.PublicURL, "/")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{"kan"},
		"resource_documentation":   base + "/api/v1/kan/ping",
	})
}

func (a *app) handleASMeta(w http.ResponseWriter, r *http.Request) {
	base := a.publicBase()
	issuer := base
	jwksURI := strings.TrimRight(a.publicURL, "/") + "/.well-known/jwks.json"
	if b := a.brandFrom(r); b != nil {
		base = strings.TrimRight(b.PublicURL, "/")
		issuer = strings.TrimRight(b.IssuerURL, "/")
		jwksURI = b.JwksURI
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                issuer,
		"jwks_uri":                              jwksURI,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"kan"},
	})
}

func (a *app) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	uris := body.RedirectURIs
	if len(uris) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	for _, u := range uris {
		if !validRedirectURI(u) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_redirect_uri"})
			return
		}
	}
	id := "grok-" + randomHex(8)
	if err := a.store.SaveOAuthClient(r.Context(), id, uris); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  id,
		"client_name":                body.ClientName,
		"redirect_uris":              uris,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
		"client_id_issued_at":        time.Now().Unix(),
	})
}

func (a *app) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	base := a.publicBase()
	appName := "Byzantine Kanban"
	if b := a.brandFrom(r); b != nil {
		base = strings.TrimRight(b.PublicURL, "/")
		appName = b.Name
	}
	q := r.URL.Query()
	a.renderLogin(w, loginData{
		AppName:     appName,
		Action:      base + "/oauth/authorize",
		ClientID:    q.Get("client_id"),
		RedirectURI: q.Get("redirect_uri"),
		State:       q.Get("state"),
		Challenge:   q.Get("code_challenge"),
		Scope:       q.Get("scope"),
	})
}

type loginData struct {
	AppName, Action, ClientID, RedirectURI, State, Challenge, Scope, Error string
}

func (a *app) renderLogin(w http.ResponseWriter, d loginData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = loginTmpl.Execute(w, d)
}

func (a *app) handleOAuthAuthorizePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	base := a.publicBase()
	appName := "Byzantine Kanban"
	var iamHostOverride string
	if b := a.brandFrom(r); b != nil {
		base = strings.TrimRight(b.PublicURL, "/")
		appName = b.Name
		// pass the brand's auth host so byz-iam mints tokens with the correct issuer
		if u, err := url.Parse(b.IssuerURL); err == nil {
			iamHostOverride = u.Host
		}
	}
	d := loginData{
		AppName:     appName,
		Action:      base + "/oauth/authorize",
		ClientID:    r.FormValue("client_id"),
		RedirectURI: r.FormValue("redirect_uri"),
		State:       r.FormValue("state"),
		Challenge:   r.FormValue("code_challenge"),
		Scope:       r.FormValue("scope"),
	}
	if d.ClientID == "" || d.RedirectURI == "" || d.Challenge == "" {
		d.Error = "Missing OAuth parameters. Start the connection again from Grok."
		a.renderLogin(w, d)
		return
	}
	if !a.redirectAllowed(r.Context(), d.ClientID, d.RedirectURI) {
		d.Error = "This redirect URI is not allowed for the client."
		a.renderLogin(w, d)
		return
	}
	if strings.TrimSpace(a.iamClientID) == "" {
		d.Error = "Server missing KAN_IAM_CLIENT_ID. Set it in supervisor and retry."
		a.renderLogin(w, d)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	access, refresh, expiresIn, err := a.iamLogin(r.Context(), email, password, iamHostOverride)
	if err != nil {
		d.Error = "Invalid email or password."
		a.renderLogin(w, d)
		return
	}
	claims, err := a.parseIAMToken(access)
	if err != nil {
		d.Error = "Signed in, but the token could not be read."
		a.renderLogin(w, d)
		return
	}
	if _, _, _, ok := rejectScope(claims); !ok {
		d.Error = "This account has no tenant. Kanban requires organization_id and tenant_id on the JWT."
		a.renderLogin(w, d)
		return
	}
	code := randomHex(24)
	if err := a.store.SaveOAuthCode(r.Context(), code, d.ClientID, d.RedirectURI, d.Challenge, access, refresh, expiresIn); err != nil {
		d.Error = "Could not issue authorization code."
		a.renderLogin(w, d)
		return
	}
	u, err := url.Parse(d.RedirectURI)
	if err != nil {
		d.Error = "Invalid redirect URI."
		a.renderLogin(w, d)
		return
	}
	q := u.Query()
	q.Set("code", code)
	if d.State != "" {
		q.Set("state", d.State)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (a *app) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	vals := url.Values{}
	if strings.Contains(r.Header.Get("Content-Type"), "json") {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		for k, v := range body {
			vals.Set(k, v)
		}
	} else {
		_ = r.ParseForm()
		vals = r.Form
	}
	grant := vals.Get("grant_type")
	if grant != "authorization_code" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	code := vals.Get("code")
	verifier := vals.Get("code_verifier")
	redirect := vals.Get("redirect_uri")
	clientID := vals.Get("client_id")
	stored, err := a.store.ConsumeOAuthCode(r.Context(), code)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	if clientID != "" && clientID != stored.ClientID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client"})
		return
	}
	if redirect != "" && redirect != stored.RedirectURI {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	if s256Challenge(verifier) != stored.CodeChallenge {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	out := map[string]any{
		"access_token": stored.AccessToken,
		"token_type":   "Bearer",
		"expires_in":   stored.ExpiresIn,
		"scope":        "kan",
	}
	if stored.RefreshToken != "" {
		out["refresh_token"] = stored.RefreshToken
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) redirectAllowed(ctx context.Context, clientID, redirectURI string) bool {
	if defaultGrokRedirectOK(redirectURI) && (clientID == "grok" || strings.HasPrefix(clientID, "grok-")) {
		return true
	}
	uris, err := a.store.GetOAuthClient(ctx, clientID)
	if err != nil {
		return clientID == "grok" && defaultGrokRedirectOK(redirectURI)
	}
	for _, u := range uris {
		if u == redirectURI {
			return true
		}
	}
	return false
}

func validRedirectURI(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" || p.Host == "" {
		return false
	}
	return true
}

func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// iamLogin authenticates against byz-iam. When iamHostOverride is non-empty it is
// sent as X-Forwarded-Host so byz-iam mints tokens with the brand's issuer URL.
func (a *app) iamLogin(ctx context.Context, email, password, iamHostOverride string) (access, refresh string, expiresIn int, err error) {
	payload, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
		"clientId": a.iamClientID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.iamURL, "/")+"/api/v1/login", bytes.NewReader(payload))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if iamHostOverride != "" {
		req.Header.Set("X-Forwarded-Host", iamHostOverride)
	}
	resp, err := a.httpc.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("iam login %d", resp.StatusCode)
	}
	var tr struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", "", 0, err
	}
	if tr.AccessToken == "" {
		return "", "", 0, fmt.Errorf("iam login empty token")
	}
	if tr.ExpiresIn <= 0 {
		tr.ExpiresIn = 3600
	}
	return tr.AccessToken, tr.RefreshToken, tr.ExpiresIn, nil
}

func (a *app) parseIAMToken(tokenStr string) (TokenClaims, error) {
	tok, err := jwt.Parse(tokenStr, a.jwks.Keyfunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !tok.Valid {
		return TokenClaims{}, fmt.Errorf("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return TokenClaims{}, fmt.Errorf("invalid claims")
	}
	tc := TokenClaims{
		OrganizationID: claimString(claims, "organization_id"),
		TenantID:       claimString(claims, "tenant_id"),
		UserID:         claimString(claims, "user_id"),
		ClientID:       claimString(claims, "client_id"),
		Subject:        claimString(claims, "sub"),
		GrantType:      claimString(claims, "grant_type"),
	}
	if tc.ClientID == "" {
		tc.ClientID = claimString(claims, "app_id")
	}
	return tc, nil
}

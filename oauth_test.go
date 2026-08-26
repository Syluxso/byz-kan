package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOAuthMetadata(t *testing.T) {
	a := &app{store: &Store{}, logBuf: NewLogBuffer(), publicURL: "https://api.byzantineapp.dev/kan"}
	h := withCORS(a.routes(withTestClaims))

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("prm %d %s", rec.Code, rec.Body.String())
	}
	var prm map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &prm)
	if prm["resource"] != "https://api.byzantineapp.dev/kan/mcp" {
		t.Fatalf("resource=%v", prm["resource"])
	}

	req = httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("as %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/oauth/authorize") || !strings.Contains(body, "S256") {
		t.Fatalf("as meta %s", body)
	}
}

func TestMCPUnauthorizedHasResourceMetadata(t *testing.T) {
	a := &app{store: &Store{}, logBuf: NewLogBuffer(), publicURL: "https://api.byzantineapp.dev/kan"}
	h := withCORS(a.routes(func(next http.HandlerFunc) http.HandlerFunc {
		return withJWT(nil, a.publicURL, next)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	wa := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(wa, "resource_metadata=") || !strings.Contains(wa, "oauth-protected-resource") {
		t.Fatalf("WWW-Authenticate=%q", wa)
	}
}

func TestOAuthTokenPKCE(t *testing.T) {
	st := testDB(t)
	if err := st.initOAuth(t.Context()); err != nil {
		t.Fatal(err)
	}
	a := &app{store: st, logBuf: NewLogBuffer(), publicURL: "https://api.byzantineapp.dev/kan"}
	h := withCORS(a.routes(withTestClaims))

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	challenge := s256Challenge(verifier)
	code := "testcodepkce1"
	if err := st.SaveOAuthCode(t.Context(), code, "grok", "https://grok.com/connectors/oauth/callback", challenge, "iam-access-token", "iam-refresh", 3600); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {"https://grok.com/connectors/oauth/callback"},
		"client_id":     {"grok"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token %d %s", rec.Code, rec.Body.String())
	}
	raw, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(raw), "iam-access-token") {
		t.Fatalf("body %s", raw)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reuse want 400 got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOAuthRegister(t *testing.T) {
	st := testDB(t)
	_ = st.initOAuth(t.Context())
	a := &app{store: st, logBuf: NewLogBuffer(), publicURL: "https://api.byzantineapp.dev/kan"}
	h := withCORS(a.routes(withTestClaims))
	body := `{"redirect_uris":["https://grok.com/connectors/oauth/callback"],"token_endpoint_auth_method":"none"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register %d %s", rec.Code, rec.Body.String())
	}
}

func TestS256AndGrokRedirect(t *testing.T) {
	v := "abc"
	if s256Challenge(v) == v || s256Challenge(v) == "" {
		t.Fatal("s256")
	}
	if !defaultGrokRedirectOK("https://grok.com/connectors/oauth/callback") {
		t.Fatal("grok.com should be allowed")
	}
	if defaultGrokRedirectOK("https://evil.example/cb") {
		t.Fatal("evil should be rejected")
	}
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newIAMStub stands in for byz-iam's /api/v1/oauth/refresh.
func newIAMStub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func postToken(t *testing.T, h http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// CW-30: the whole defect. grant_type=refresh_token used to return
// unsupported_grant_type, so an OAuth session died at the access token's TTL
// and could only be revived by an interactive login.
func TestRefreshGrantIsSupported(t *testing.T) {
	var gotBody map[string]string
	var gotForwardedHost string

	iam := newIAMStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/oauth/refresh" {
			t.Errorf("byz-kan called %s, want /api/v1/oauth/refresh", r.URL.Path)
		}
		gotForwardedHost = r.Header.Get("X-Forwarded-Host")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, http.StatusOK, map[string]any{
			"accessToken":  "new-access",
			"refreshToken": "rotated-refresh",
			"expiresIn":    3600,
		})
	})

	a := &app{
		store:       &Store{},
		logBuf:      NewLogBuffer(),
		publicURL:   "https://api.byzantineapp.dev/kan",
		iamURL:      iam.URL,
		iamClientID: "byz-admin",
		httpc:       iam.Client(),
	}
	h := withCORS(a.routes(withTestClaims))

	rec := postToken(t, h, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"old-refresh"},
		"client_id":     {"grok"},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("refresh grant returned %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["access_token"] != "new-access" {
		t.Fatalf("access_token=%v", out["access_token"])
	}
	// A refresh must return a refresh token, or a client that replaces its
	// stored copy each time is left with nothing for the next hour.
	if out["refresh_token"] != "rotated-refresh" {
		t.Fatalf("refresh_token=%v; rotation not passed through", out["refresh_token"])
	}
	if out["token_type"] != "Bearer" {
		t.Fatalf("token_type=%v", out["token_type"])
	}
	if gotBody["refreshToken"] != "old-refresh" {
		t.Fatalf("byz-kan sent refreshToken=%q", gotBody["refreshToken"])
	}
	if gotBody["clientId"] != "byz-admin" {
		t.Fatalf("byz-kan sent clientId=%q", gotBody["clientId"])
	}
	if gotForwardedHost != "" {
		t.Fatalf("unbranded request should not forward a host, got %q", gotForwardedHost)
	}
}

// IAM may not rotate. The client must still get a usable refresh token back
// rather than an empty field.
func TestRefreshGrantEchoesTokenWhenIAMDoesNotRotate(t *testing.T) {
	iam := newIAMStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"accessToken": "new-access",
			"expiresIn":   3600,
		})
	})
	a := &app{store: &Store{}, logBuf: NewLogBuffer(), iamURL: iam.URL,
		iamClientID: "byz-admin", httpc: iam.Client()}
	h := withCORS(a.routes(withTestClaims))

	rec := postToken(t, h, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"still-good"},
	})
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["refresh_token"] != "still-good" {
		t.Fatalf("refresh_token=%v, want the original echoed back", out["refresh_token"])
	}
}

// A branded host must keep its issuer on renewal, or a Cardwallah session
// silently comes back as a Byzantine one.
func TestRefreshGrantPreservesBrand(t *testing.T) {
	var forwarded, clientID string
	iam := newIAMStub(t, func(w http.ResponseWriter, r *http.Request) {
		forwarded = r.Header.Get("X-Forwarded-Host")
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		clientID = body["clientId"]
		writeJSON(w, http.StatusOK, map[string]any{"accessToken": "a", "expiresIn": 3600})
	})

	a := &app{
		store: &Store{}, logBuf: NewLogBuffer(), iamURL: iam.URL,
		iamClientID: "byz-admin", httpc: iam.Client(),
		brands: map[string]Brand{
			"mcp.cardwallah.com": {
				Name:      "Cardwallah",
				IssuerURL: "https://auth.cardwallah.com",
				PublicURL: "https://mcp.cardwallah.com",
				ClientID:  "cardwallah-front",
			},
		},
	}
	h := withCORS(a.routes(withTestClaims))

	req := httptest.NewRequest(http.MethodPost, "/oauth/token",
		strings.NewReader("grant_type=refresh_token&refresh_token=r"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Host", "mcp.cardwallah.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if forwarded != "auth.cardwallah.com" {
		t.Fatalf("X-Forwarded-Host=%q, want auth.cardwallah.com", forwarded)
	}
	if clientID != "cardwallah-front" {
		t.Fatalf("clientId=%q, want the brand's", clientID)
	}
}

// A rejected refresh token is invalid_grant: the client should re-authorize,
// not retry against a server it thinks is broken.
func TestRefreshGrantRejectionIsInvalidGrant(t *testing.T) {
	iam := newIAMStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	a := &app{store: &Store{}, logBuf: NewLogBuffer(), iamURL: iam.URL,
		iamClientID: "byz-admin", httpc: iam.Client()}
	h := withCORS(a.routes(withTestClaims))

	rec := postToken(t, h, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"expired"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestRefreshGrantRequiresAToken(t *testing.T) {
	a := &app{store: &Store{}, logBuf: NewLogBuffer(), iamClientID: "byz-admin",
		httpc: http.DefaultClient}
	h := withCORS(a.routes(withTestClaims))

	rec := postToken(t, h, url.Values{"grant_type": {"refresh_token"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

// Genuinely unknown grants must still be refused.
func TestUnknownGrantStillRejected(t *testing.T) {
	a := &app{store: &Store{}, logBuf: NewLogBuffer(), httpc: http.DefaultClient}
	h := withCORS(a.routes(withTestClaims))

	rec := postToken(t, h, url.Values{"grant_type": {"password"}})
	if !strings.Contains(rec.Body.String(), "unsupported_grant_type") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

// Clients discover the grant from metadata; without this they never try.
func TestDiscoveryAdvertisesRefreshGrant(t *testing.T) {
	a := &app{store: &Store{}, logBuf: NewLogBuffer(), publicURL: "https://api.byzantineapp.dev/kan"}
	h := withCORS(a.routes(withTestClaims))

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var meta struct {
		Grants []string `json:"grant_types_supported"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, g := range meta.Grants {
		if g == "refresh_token" {
			found = true
		}
	}
	if !found {
		t.Fatalf("grant_types_supported=%v", meta.Grants)
	}
}

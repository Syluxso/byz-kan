package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRejectScopeMissingTenant(t *testing.T) {
	status, _, detail, ok := rejectScope(TokenClaims{OrganizationID: "org"})
	if ok || status != http.StatusForbidden || detail != "token missing tenant_id" {
		t.Fatalf("status=%d detail=%q ok=%v", status, detail, ok)
	}
}

func TestRejectScopeMissingOrg(t *testing.T) {
	status, _, detail, ok := rejectScope(TokenClaims{TenantID: "ten"})
	if ok || status != http.StatusForbidden || detail != "token missing organization_id" {
		t.Fatalf("status=%d detail=%q ok=%v", status, detail, ok)
	}
}

func TestRejectScopeOK(t *testing.T) {
	if _, _, _, ok := rejectScope(TokenClaims{OrganizationID: "o", TenantID: "t"}); !ok {
		t.Fatal("expected ok")
	}
}

func TestHTTPMissingTenantIs403(t *testing.T) {
	a := &app{store: &Store{}, logBuf: NewLogBuffer()}
	h := withCORS(a.routes(withTestClaims))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/boards", nil)
	req.Header.Set("X-Test-Org", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d body=%s", rec.Code, rec.Body.String())
	}
	var p problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Detail != "token missing tenant_id" {
		t.Fatalf("detail=%q", p.Detail)
	}
}

func TestPingNoAuth(t *testing.T) {
	a := &app{store: &Store{}, logBuf: NewLogBuffer()}
	h := withCORS(a.routes(withTestClaims))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/kan/ping", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
}

package main

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// handleCreatePAT issues a long-lived HS256 personal access token.
// POST /api/v1/me/tokens  (no JWT middleware — auth is via email+password)
func (a *app) handleCreatePAT(w http.ResponseWriter, r *http.Request) {
	if len(a.patSecret) == 0 {
		writeProblem(w, http.StatusNotImplemented, "Not Implemented", "KAN_PAT_SECRET not configured")
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Email) == "" || body.Password == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "email and password required")
		return
	}

	var iamHostOverride, iamClientOverride string
	if b := a.brandFrom(r); b != nil {
		if u, err := url.Parse(b.IssuerURL); err == nil {
			iamHostOverride = u.Host
		}
		iamClientOverride = b.ClientID
	}

	access, _, _, err := a.iamLogin(r.Context(), body.Email, body.Password, iamHostOverride, iamClientOverride)
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "invalid email or password")
		return
	}

	tc, err := a.parseIAMToken(access)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to parse IAM token")
		return
	}

	now := time.Now()
	exp := now.Add(365 * 24 * time.Hour)

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"organization_id": tc.OrganizationID,
		"tenant_id":       tc.TenantID,
		"user_id":         tc.UserID,
		"client_id":       tc.ClientID,
		"sub":             tc.Subject,
		"grant_type":      "personal_access_token",
		"iat":             now.Unix(),
		"exp":             exp.Unix(),
	})

	signed, err := tok.SignedString(a.patSecret)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to sign token")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"token":      signed,
		"expires_at": exp.Format(time.RFC3339),
	})
}

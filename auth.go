package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

type ctxKey int

const claimsCtxKey ctxKey = 1

type TokenClaims struct {
	OrganizationID string
	TenantID       string
	UserID         string
	ClientID       string
	Subject        string
	GrantType      string
}

func (c TokenClaims) OwnerUserID() string {
	if c.UserID != "" {
		return c.UserID
	}
	return c.Subject
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withJWT(k keyfunc.Keyfunc, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeProblem(w, http.StatusUnauthorized, "Unauthorized", "missing bearer token")
			return
		}
		tokenStr := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		tok, err := jwt.Parse(tokenStr, k.Keyfunc,
			jwt.WithValidMethods([]string{"RS256"}),
			jwt.WithExpirationRequired(),
		)
		if err != nil || !tok.Valid {
			writeProblem(w, http.StatusUnauthorized, "Unauthorized", "invalid token")
			return
		}
		claims, ok := tok.Claims.(jwt.MapClaims)
		if !ok {
			writeProblem(w, http.StatusUnauthorized, "Unauthorized", "invalid claims")
			return
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
		if status, title, detail, ok := rejectScope(tc); !ok {
			writeProblem(w, status, title, detail)
			return
		}
		ctx := context.WithValue(r.Context(), claimsCtxKey, tc)
		next(w, r.WithContext(ctx))
	}
}

func rejectScope(tc TokenClaims) (status int, title, detail string, ok bool) {
	if tc.OrganizationID == "" {
		return http.StatusForbidden, "Forbidden", "token missing organization_id", false
	}
	if tc.TenantID == "" {
		return http.StatusForbidden, "Forbidden", "token missing tenant_id", false
	}
	return 0, "", "", true
}

func withTestClaims(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tc := TokenClaims{
			OrganizationID: strings.TrimSpace(r.Header.Get("X-Test-Org")),
			TenantID:       strings.TrimSpace(r.Header.Get("X-Test-Tenant")),
			UserID:         strings.TrimSpace(r.Header.Get("X-Test-User")),
			Subject:        strings.TrimSpace(r.Header.Get("X-Test-User")),
		}
		if status, title, detail, ok := rejectScope(tc); !ok {
			writeProblem(w, status, title, detail)
			return
		}
		next(w, r.WithContext(withClaims(r.Context(), tc)))
	}
}

func claimsFrom(ctx context.Context) (TokenClaims, bool) {
	v, ok := ctx.Value(claimsCtxKey).(TokenClaims)
	return v, ok
}

func withClaims(ctx context.Context, tc TokenClaims) context.Context {
	return context.WithValue(ctx, claimsCtxKey, tc)
}

func claimString(claims jwt.MapClaims, key string) string {
	v, ok := claims[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" || s == "null" {
			return ""
		}
		return s
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

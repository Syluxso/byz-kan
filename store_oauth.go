package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const oauthInitSQL = `
CREATE TABLE IF NOT EXISTS kan.oauth_clients (
    client_id      TEXT PRIMARY KEY,
    redirect_uris  JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS kan.oauth_codes (
    code            TEXT PRIMARY KEY,
    client_id       TEXT NOT NULL,
    redirect_uri    TEXT NOT NULL,
    code_challenge  TEXT NOT NULL,
    access_token    TEXT NOT NULL,
    refresh_token   TEXT,
    expires_in      INT NOT NULL DEFAULT 3600,
    expires_at      TIMESTAMPTZ NOT NULL,
    used_at         TIMESTAMPTZ
);
`

func (s *Store) initOAuth(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, oauthInitSQL)
	return err
}

func (s *Store) SaveOAuthClient(ctx context.Context, clientID string, redirectURIs []string) error {
	b, err := json.Marshal(redirectURIs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO kan.oauth_clients (client_id, redirect_uris)
VALUES ($1, $2::jsonb)
ON CONFLICT (client_id) DO UPDATE SET redirect_uris = EXCLUDED.redirect_uris
`, clientID, string(b))
	return err
}

func (s *Store) GetOAuthClient(ctx context.Context, clientID string) ([]string, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT redirect_uris FROM kan.oauth_clients WHERE client_id = $1`, clientID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var uris []string
	if err := json.Unmarshal(raw, &uris); err != nil {
		return nil, err
	}
	return uris, nil
}

func (s *Store) SaveOAuthCode(ctx context.Context, code, clientID, redirectURI, challenge, access, refresh string, expiresIn int) error {
	exp := time.Now().Add(5 * time.Minute)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO kan.oauth_codes (code, client_id, redirect_uri, code_challenge, access_token, refresh_token, expires_in, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, code, clientID, redirectURI, challenge, access, nilIfEmpty(refresh), expiresIn, exp)
	return err
}

type oauthCode struct {
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	AccessToken   string
	RefreshToken  string
	ExpiresIn     int
}

func (s *Store) ConsumeOAuthCode(ctx context.Context, code string) (oauthCode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return oauthCode{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var c oauthCode
	var refresh string
	var exp time.Time
	var used *time.Time
	err = tx.QueryRowContext(ctx, `
SELECT client_id, redirect_uri, code_challenge, access_token, COALESCE(refresh_token, ''), expires_in, expires_at, used_at
FROM kan.oauth_codes WHERE code = $1
FOR UPDATE
`, code).Scan(&c.ClientID, &c.RedirectURI, &c.CodeChallenge, &c.AccessToken, &refresh, &c.ExpiresIn, &exp, &used)
	if err != nil {
		return oauthCode{}, errNotFound
	}
	c.RefreshToken = refresh
	if used != nil || time.Now().After(exp) {
		return oauthCode{}, errInvalid
	}
	if _, err := tx.ExecContext(ctx, `UPDATE kan.oauth_codes SET used_at = now() WHERE code = $1`, code); err != nil {
		return oauthCode{}, err
	}
	if err := tx.Commit(); err != nil {
		return oauthCode{}, err
	}
	return c, nil
}

func defaultGrokRedirectOK(uri string) bool {
	u := strings.TrimSpace(uri)
	switch {
	case strings.HasPrefix(u, "https://grok.com/"):
		return true
	case strings.HasPrefix(u, "https://www.grok.com/"):
		return true
	case strings.HasPrefix(u, "https://grok.x.ai/"):
		return true
	case strings.HasPrefix(u, "https://x.ai/"):
		return true
	default:
		return false
	}
}

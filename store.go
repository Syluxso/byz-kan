package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	_ "embed"

	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed docs/SCHEMA.sql
var initSQL string

const schema = "kan"

var (
	errNotFound     = errors.New("not found")
	errConflict     = errors.New("conflict")
	errInvalid      = errors.New("invalid")
	errPrefixLocked = errors.New("prefix locked")
	errHasTickets   = errors.New("state has tickets")
)

type Store struct {
	db *sql.DB

	// hub fans mutations out to live SSE subscribers. Optional: nil in tests
	// that do not exercise streaming, and every publish path is nil-safe.
	hub *Hub
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// publish emits a board event to live subscribers. Safe on a nil hub.
// Called from store mutations so REST and MCP are covered by one path.
func (s *Store) publish(sc scope, evType, boardID, ticketID string, payload map[string]any) {
	if s.hub == nil || boardID == "" {
		return
	}
	s.hub.Publish(sc.OrgID, sc.TenantID, Event{
		Type:     evType,
		BoardID:  boardID,
		TicketID: ticketID,
		ActorID:  sc.ActorID,
		At:       time.Now().UTC(),
		Payload:  payload,
	})
}

func (s *Store) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, initSQL)
	return err
}

func (s *Store) CountActive(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kan.tickets WHERE deleted_at IS NULL`,
	).Scan(&n)
	return n, err
}

func isUniqueViolation(err error) bool {
	var e *pgconn.PgError
	return errors.As(err, &e) && e.Code == "23505"
}

func ptrStr(ns sql.NullString) *string {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	s := ns.String
	return &s
}

func ptrTime(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	t := nt.Time
	return &t
}

func ptrInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func ptrInt64(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func rawJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(b)
}

func jsonOrEmpty(raw json.RawMessage) any {
	if len(raw) == 0 {
		return `{}`
	}
	return string(raw)
}

func nilIfEmpty(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

type scope struct {
	OrgID    string
	TenantID string
	ActorID  string
	InclDel  bool
}

func (p scope) args() (include bool, owner any) {
	owner = nilIfEmpty(p.ActorID)
	return p.InclDel && p.ActorID != "", owner
}

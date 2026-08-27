package main

import (
	"context"
	"database/sql"
	"strings"
)

// CW-18: the shared agent/human thread. Separate from kan.comments by design —
// comments are product discussion on a ticket, messages are coordination
// between participants and can hang off a board with no ticket at all.

const messageSelect = `
SELECT id::text, organization_id::text, tenant_id::text, board_id::text, ticket_id::text,
       actor_type, actor_key, display_name, body, created_by::text,
       created_at, updated_at, deleted_at
FROM kan.messages
`

func scanMessage(row scanner) (MessageView, error) {
	var v MessageView
	var ticketID, createdBy sql.NullString
	var deleted sql.NullTime
	if err := row.Scan(&v.ID, &v.OrganizationID, &v.TenantID, &v.BoardID, &ticketID,
		&v.ActorType, &v.ActorKey, &v.DisplayName, &v.Body, &createdBy,
		&v.CreatedAt, &v.UpdatedAt, &deleted); err != nil {
		return MessageView{}, err
	}
	v.TicketID = ptrStr(ticketID)
	v.CreatedBy = ptrStr(createdBy)
	v.DeletedAt = ptrTime(deleted)
	return v, nil
}

// ListMessagesParams selects a thread.
//
// TicketID set  -> that ticket's thread only.
// BoardID only  -> the board-level thread (messages with no ticket).
// BoardAll      -> every message on the board, ticket-scoped ones included.
type ListMessagesParams struct {
	BoardID  string
	TicketID string
	BoardAll bool
}

func (s *Store) ListMessages(ctx context.Context, sc scope, p ListMessagesParams) ([]MessageView, error) {
	include, owner := sc.args()
	q := messageSelect + `
WHERE organization_id = $1::uuid AND tenant_id = $2::uuid
  AND (deleted_at IS NULL OR ($3 AND created_by::text = $4))
`
	args := []any{sc.OrgID, sc.TenantID, include, owner}
	n := 4

	switch {
	case p.TicketID != "":
		n++
		q += ` AND ticket_id = $` + itoa(n) + `::uuid`
		args = append(args, p.TicketID)
	case p.BoardID != "":
		n++
		q += ` AND board_id = $` + itoa(n) + `::uuid`
		args = append(args, p.BoardID)
		if !p.BoardAll {
			// The board thread is its own conversation; ticket chatter does not
			// spill into it unless explicitly asked for.
			q += ` AND ticket_id IS NULL`
		}
	}

	// Oldest first: a thread reads top to bottom.
	q += ` ORDER BY created_at ASC, id ASC LIMIT 500`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MessageView, 0)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetMessage(ctx context.Context, sc scope, id string) (MessageView, error) {
	include, owner := sc.args()
	row := s.db.QueryRowContext(ctx, messageSelect+`
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid
  AND (deleted_at IS NULL OR ($4 AND created_by::text = $5))
`, id, sc.OrgID, sc.TenantID, include, owner)
	m, err := scanMessage(row)
	if err == sql.ErrNoRows {
		return MessageView{}, errNotFound
	}
	return m, err
}

// CreateMessage posts to a board or ticket thread.
//
// When ticketID is set, boardID is taken from the ticket rather than trusted
// from the caller, so a message can never claim to be on a board its ticket
// does not belong to.
func (s *Store) CreateMessage(ctx context.Context, sc scope, boardID, ticketID, actorType, actorKey, displayName, body string) (MessageView, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return MessageView{}, errInvalid
	}
	actorType = strings.ToLower(strings.TrimSpace(actorType))
	if actorType == "" {
		actorType = "agent"
	}
	if actorType != "user" && actorType != "agent" {
		return MessageView{}, errInvalid
	}
	actorKey = strings.TrimSpace(actorKey)
	displayName = strings.TrimSpace(displayName)
	if actorKey == "" || displayName == "" {
		return MessageView{}, errInvalid
	}

	var ticket any
	if ticketID != "" {
		t, err := s.GetTicketByID(ctx, sc, ticketID)
		if err != nil {
			return MessageView{}, err
		}
		boardID = t.BoardID
		ticket = t.ID
	}
	if _, err := s.requireLiveBoard(ctx, sc, boardID); err != nil {
		return MessageView{}, err
	}

	var id string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO kan.messages
  (organization_id, tenant_id, board_id, ticket_id, actor_type, actor_key, display_name, body, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, $9::uuid)
RETURNING id::text
`, sc.OrgID, sc.TenantID, boardID, ticket, actorType, actorKey, displayName, body, nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		return MessageView{}, err
	}
	return s.GetMessage(ctx, sc, id)
}

func (s *Store) UpdateMessage(ctx context.Context, sc scope, id, body string) (MessageView, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return MessageView{}, errInvalid
	}
	if _, err := s.GetMessage(ctx, sc, id); err != nil {
		return MessageView{}, err
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.messages SET body = $4, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, body)
	if err != nil {
		return MessageView{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return MessageView{}, errNotFound
	}
	return s.GetMessage(ctx, sc, id)
}

func (s *Store) SoftDeleteMessage(ctx context.Context, sc scope, id string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.messages SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

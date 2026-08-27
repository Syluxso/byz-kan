package main

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type ListTicketsParams struct {
	BoardID    string
	StateID    string
	AssigneeID string
	TagID      string
	TagName    string // CW-14: filter by tag name; "#mcp" and "mcp" are the same tag
	Q          string
}

// normalizeTagName makes "#mcp", " #MCP " and "mcp" the same tag reference.
// Tags are unique per (tenant, lower(name), kind), so matching is case-insensitive.
func normalizeTagName(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "#"))
}

func (s *Store) CreateTicket(ctx context.Context, sc scope, boardID, stateID, parentID, title, body, ticketType string, priority, position int, dueAt *time.Time, estimate *int, cardData []byte) (TicketView, error) {
	board, err := s.requireLiveBoard(ctx, sc, boardID)
	if err != nil {
		return TicketView{}, err
	}
	if stateID == "" {
		stateID, err = s.defaultStateID(ctx, sc, boardID)
		if err != nil {
			return TicketView{}, err
		}
	} else {
		st, err := s.GetState(ctx, sc, stateID)
		if err != nil {
			return TicketView{}, err
		}
		if st.BoardID != boardID {
			return TicketView{}, errInvalid
		}
	}
	if ticketType == "" {
		ticketType = "ticket"
	}
	if ticketType != "ticket" && ticketType != "defect" {
		return TicketView{}, errInvalid
	}
	if cardData == nil {
		cardData = []byte(`{}`)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TicketView{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var number int
	err = tx.QueryRowContext(ctx, `
INSERT INTO kan.board_sequences (board_id, tenant_id, last_number)
VALUES ($1::uuid, $2::uuid, 1)
ON CONFLICT (board_id, tenant_id)
DO UPDATE SET last_number = kan.board_sequences.last_number + 1
RETURNING last_number
`, boardID, sc.TenantID).Scan(&number)
	if err != nil {
		return TicketView{}, err
	}
	key := ticketKey(board.KeyPrefix, number)

	completed := any(nil)
	st, err := s.GetState(ctx, sc, stateID)
	if err != nil {
		return TicketView{}, err
	}
	if strings.EqualFold(st.Name, "Completed") {
		completed = time.Now()
	}

	var id string
	err = tx.QueryRowContext(ctx, `
INSERT INTO kan.tickets (
  organization_id, tenant_id, board_id, state_id, parent_ticket_id,
  number, key, title, body, card_data, ticket_type, priority, position,
  due_at, estimate_minutes, completed_at, created_by
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid,
  $6, $7, $8, $9, $10::jsonb, $11, $12, $13,
  $14, $15, $16, $17::uuid
) RETURNING id::text
`, sc.OrgID, sc.TenantID, boardID, stateID, nilIfEmpty(parentID),
		number, key, title, nilIfEmpty(body), string(cardData), ticketType, priority, position,
		dueAt, estimate, completed, nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return TicketView{}, errConflict
		}
		return TicketView{}, err
	}
	if err := tx.Commit(); err != nil {
		return TicketView{}, err
	}
	_ = s.AppendActivity(ctx, sc.OrgID, sc.TenantID, sc.ActorID, &boardID, &id, "ticket.created", map[string]any{"key": key})
	s.publish(sc, "ticket.created", boardID, id, map[string]any{"key": key, "stateId": stateID})
	return s.GetTicketByID(ctx, sc, id)
}

func (s *Store) GetTicketByID(ctx context.Context, sc scope, id string) (TicketView, error) {
	include, owner := sc.args()
	row := s.db.QueryRowContext(ctx, ticketSelect+`
WHERE t.id = $1::uuid AND t.organization_id = $2::uuid AND t.tenant_id = $3::uuid
  AND (t.deleted_at IS NULL OR ($4 AND t.created_by::text = $5))
`, id, sc.OrgID, sc.TenantID, include, owner)
	v, err := scanTicket(row)
	if err == sql.ErrNoRows {
		return TicketView{}, errNotFound
	}
	return v, err
}

func (s *Store) GetTicketByKey(ctx context.Context, sc scope, key string) (TicketView, error) {
	include, owner := sc.args()
	row := s.db.QueryRowContext(ctx, ticketSelect+`
WHERE t.organization_id = $1::uuid AND t.tenant_id = $2::uuid AND upper(t.key) = upper($3)
  AND (t.deleted_at IS NULL OR ($4 AND t.created_by::text = $5))
`, sc.OrgID, sc.TenantID, strings.TrimSpace(key), include, owner)
	v, err := scanTicket(row)
	if err == sql.ErrNoRows {
		return TicketView{}, errNotFound
	}
	return v, err
}

func (s *Store) ListTickets(ctx context.Context, sc scope, p ListTicketsParams) ([]TicketView, error) {
	include, owner := sc.args()
	q := ticketSelect + `
WHERE t.organization_id = $1::uuid AND t.tenant_id = $2::uuid
  AND (t.deleted_at IS NULL OR ($3 AND t.created_by::text = $4))
`
	args := []any{sc.OrgID, sc.TenantID, include, owner}
	n := 4
	if p.BoardID != "" {
		n++
		q += ` AND t.board_id = $` + itoa(n) + `::uuid`
		args = append(args, p.BoardID)
	}
	if p.StateID != "" {
		n++
		q += ` AND t.state_id = $` + itoa(n) + `::uuid`
		args = append(args, p.StateID)
	}
	if p.AssigneeID != "" {
		n++
		q += ` AND EXISTS (SELECT 1 FROM kan.ticket_assignees a WHERE a.ticket_id = t.id AND a.user_id = $` + itoa(n) + `::uuid AND a.deleted_at IS NULL)`
		args = append(args, p.AssigneeID)
	}
	if p.TagID != "" {
		n++
		q += ` AND EXISTS (SELECT 1 FROM kan.ticket_tags tt WHERE tt.ticket_id = t.id AND tt.tag_id = $` + itoa(n) + `::uuid AND tt.deleted_at IS NULL)`
		args = append(args, p.TagID)
	}
	if name := normalizeTagName(p.TagName); name != "" {
		// Scope the tag to the caller's tenant as well as the ticket, so a tag
		// name can never reach across tenants.
		n++
		q += ` AND EXISTS (
  SELECT 1 FROM kan.ticket_tags tt
  JOIN kan.tags tg ON tg.id = tt.tag_id
  WHERE tt.ticket_id = t.id AND tt.deleted_at IS NULL
    AND tg.deleted_at IS NULL AND tg.tenant_id = t.tenant_id
    AND lower(tg.name) = lower($` + itoa(n) + `))`
		args = append(args, name)
	}
	if strings.TrimSpace(p.Q) != "" {
		n++
		q += ` AND (t.title ILIKE $` + itoa(n) + ` OR t.body ILIKE $` + itoa(n) + ` OR t.key ILIKE $` + itoa(n) + `)`
		args = append(args, "%"+strings.TrimSpace(p.Q)+"%")
	}
	q += `
ORDER BY s.position ASC, t.position ASC, t.created_at ASC
LIMIT 500
`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TicketView, 0)
	for rows.Next() {
		v, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTicket(ctx context.Context, sc scope, id string, title, body, ticketType *string, priority, position, estimate *int, clearEstimate bool, dueAt *time.Time, clearDue bool, parentID *string, clearParent bool, cardData []byte) (TicketView, error) {
	cur, err := s.GetTicketByID(ctx, sc, id)
	if err != nil {
		return TicketView{}, err
	}
	if cur.DeletedAt != nil {
		return TicketView{}, errNotFound
	}
	if title != nil {
		cur.Title = strings.TrimSpace(*title)
	}
	if body != nil {
		cur.Body = body
	}
	if ticketType != nil {
		if *ticketType != "ticket" && *ticketType != "defect" {
			return TicketView{}, errInvalid
		}
		cur.TicketType = *ticketType
	}
	if priority != nil {
		cur.Priority = *priority
	}
	if position != nil {
		cur.Position = *position
	}
	if clearEstimate {
		cur.EstimateMinutes = nil
	} else if estimate != nil {
		cur.EstimateMinutes = estimate
	}
	if clearDue {
		cur.DueAt = nil
	} else if dueAt != nil {
		cur.DueAt = dueAt
	}
	if clearParent {
		cur.ParentTicketID = nil
	} else if parentID != nil {
		cur.ParentTicketID = parentID
	}
	if cardData != nil {
		cur.CardData = cardData
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE kan.tickets SET
  title = $4, body = $5, ticket_type = $6, priority = $7, position = $8,
  estimate_minutes = $9, due_at = $10, parent_ticket_id = $11::uuid,
  card_data = $12::jsonb, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, cur.Title, nilIfEmpty(deref(cur.Body)), cur.TicketType, cur.Priority, cur.Position,
		cur.EstimateMinutes, cur.DueAt, nilIfEmpty(deref(cur.ParentTicketID)), jsonOrEmpty(cur.CardData))
	if err != nil {
		return TicketView{}, err
	}
	s.publish(sc, "ticket.updated", cur.BoardID, id, map[string]any{"key": cur.Key})
	return s.GetTicketByID(ctx, sc, id)
}

func (s *Store) MoveTicket(ctx context.Context, sc scope, id, stateID string, position *int) (TicketView, error) {
	cur, err := s.GetTicketByID(ctx, sc, id)
	if err != nil {
		return TicketView{}, err
	}
	st, err := s.GetState(ctx, sc, stateID)
	if err != nil {
		return TicketView{}, err
	}
	if st.BoardID != cur.BoardID {
		return TicketView{}, errInvalid
	}
	pos := cur.Position
	if position != nil {
		pos = *position
	}
	var completed any
	if strings.EqualFold(st.Name, "Completed") {
		now := time.Now()
		completed = now
	} else {
		completed = nil
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE kan.tickets SET state_id = $4::uuid, position = $5, completed_at = $6, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, stateID, pos, completed)
	if err != nil {
		return TicketView{}, err
	}
	_ = s.AppendActivity(ctx, sc.OrgID, sc.TenantID, sc.ActorID, &cur.BoardID, &id, "ticket.moved", map[string]any{"stateId": stateID})
	s.publish(sc, "ticket.moved", cur.BoardID, id, map[string]any{
		"key":         cur.Key,
		"stateId":     stateID,
		"fromStateId": cur.StateID,
		"completedAt": completed,
	})
	return s.GetTicketByID(ctx, sc, id)
}

func (s *Store) SoftDeleteTicket(ctx context.Context, sc scope, id string) error {
	cur, err := s.GetTicketByID(ctx, sc, id)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `UPDATE kan.tickets SET parent_ticket_id = NULL, updated_at = now() WHERE parent_ticket_id = $1::uuid`, id); err != nil {
		return err
	}
	for _, q := range []string{
		`UPDATE kan.comments SET deleted_at = now(), updated_at = now() WHERE ticket_id = $1::uuid AND deleted_at IS NULL`,
		`UPDATE kan.links SET deleted_at = now(), updated_at = now() WHERE ticket_id = $1::uuid AND deleted_at IS NULL`,
		`UPDATE kan.attachments SET deleted_at = now(), updated_at = now() WHERE ticket_id = $1::uuid AND deleted_at IS NULL`,
		`UPDATE kan.time_entries SET deleted_at = now(), updated_at = now() WHERE ticket_id = $1::uuid AND deleted_at IS NULL`,
		`UPDATE kan.ticket_assignees SET deleted_at = now(), updated_at = now() WHERE ticket_id = $1::uuid AND deleted_at IS NULL`,
		`UPDATE kan.ticket_watchers SET deleted_at = now(), updated_at = now() WHERE ticket_id = $1::uuid AND deleted_at IS NULL`,
		`UPDATE kan.ticket_tags SET deleted_at = now(), updated_at = now() WHERE ticket_id = $1::uuid AND deleted_at IS NULL`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.checklist_items SET deleted_at = now(), updated_at = now()
WHERE checklist_id IN (SELECT id FROM kan.checklists WHERE ticket_id = $1::uuid) AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE kan.checklists SET deleted_at = now(), updated_at = now() WHERE ticket_id = $1::uuid AND deleted_at IS NULL`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
UPDATE kan.tickets SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.AppendActivity(ctx, sc.OrgID, sc.TenantID, sc.ActorID, &cur.BoardID, &id, "ticket.deleted", map[string]any{})
	s.publish(sc, "ticket.deleted", cur.BoardID, id, map[string]any{"key": cur.Key})
	return nil
}

const ticketSelect = `
SELECT t.id::text, t.organization_id::text, t.tenant_id::text, t.board_id::text, t.state_id::text,
       t.parent_ticket_id::text, t.number, t.key, t.title, t.body, t.card_data, t.ticket_type,
       t.priority, t.position, t.due_at, t.estimate_minutes, t.logged_minutes, t.completed_at,
       t.created_by::text, t.created_at, t.updated_at, t.deleted_at
FROM kan.tickets t
LEFT JOIN kan.states s ON s.id = t.state_id
`

func scanTicket(row scanner) (TicketView, error) {
	var v TicketView
	var parent, body, createdBy sql.NullString
	var card []byte
	var due, completed, deleted sql.NullTime
	var estimate sql.NullInt64
	err := row.Scan(&v.ID, &v.OrganizationID, &v.TenantID, &v.BoardID, &v.StateID, &parent,
		&v.Number, &v.Key, &v.Title, &body, &card, &v.TicketType, &v.Priority, &v.Position,
		&due, &estimate, &v.LoggedMinutes, &completed, &createdBy, &v.CreatedAt, &v.UpdatedAt, &deleted)
	if err != nil {
		return TicketView{}, err
	}
	v.ParentTicketID = ptrStr(parent)
	v.Body = ptrStr(body)
	v.CardData = rawJSON(card)
	v.DueAt = ptrTime(due)
	v.EstimateMinutes = ptrInt(estimate)
	v.CompletedAt = ptrTime(completed)
	v.CreatedBy = ptrStr(createdBy)
	v.DeletedAt = ptrTime(deleted)
	return v, nil
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return fmtInt(n)
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

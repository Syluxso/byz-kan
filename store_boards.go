package main

import (
	"context"
	"database/sql"
	"strings"
)

func (s *Store) allocateKeyPrefix(ctx context.Context, tx *sql.Tx, tenantID, name, requested string) (string, error) {
	var base string
	if strings.TrimSpace(requested) != "" {
		p, err := normalizeKeyPrefix(requested)
		if err != nil {
			return "", err
		}
		base = p
	} else {
		base = deriveKeyPrefixFromName(name)
	}
	for n := 1; n <= 99; n++ {
		cand, err := nextPrefixCandidate(base, n)
		if err != nil {
			return "", err
		}
		var exists bool
		err = tx.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM kan.boards
  WHERE tenant_id = $1 AND upper(key_prefix) = upper($2) AND deleted_at IS NULL
)`, tenantID, cand).Scan(&exists)
		if err != nil {
			return "", err
		}
		if !exists {
			return cand, nil
		}
	}
	return "", errConflict
}

func (s *Store) CreateBoard(ctx context.Context, orgID, tenantID, actorID, name, description, keyPrefix string, isPublished bool, cardSchema, settings []byte) (BoardView, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardView{}, err
	}
	defer func() { _ = tx.Rollback() }()

	prefix, err := s.allocateKeyPrefix(ctx, tx, tenantID, name, keyPrefix)
	if err != nil {
		return BoardView{}, err
	}
	if cardSchema == nil {
		cardSchema = []byte(`{}`)
	}
	if settings == nil {
		settings = []byte(`{}`)
	}

	var id string
	err = tx.QueryRowContext(ctx, `
INSERT INTO kan.boards (organization_id, tenant_id, name, description, key_prefix, is_published, card_schema, settings, created_by)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::uuid)
RETURNING id::text
`, orgID, tenantID, name, nilIfEmpty(description), prefix, isPublished, string(cardSchema), string(settings), nilIfEmpty(actorID)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return BoardView{}, errConflict
		}
		return BoardView{}, err
	}

	defaults := []struct {
		name  string
		pos   int
		isDef bool
	}{
		{"Backlog", 0, true},
		{"In Progress", 1, false},
		{"Testing", 2, false},
		{"Completed", 3, false},
	}
	for _, st := range defaults {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO kan.states (organization_id, tenant_id, board_id, name, position, is_default, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::uuid)
`, orgID, tenantID, id, st.name, st.pos, st.isDef, nilIfEmpty(actorID)); err != nil {
			return BoardView{}, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO kan.board_sequences (board_id, tenant_id, last_number)
VALUES ($1::uuid, $2::uuid, 0)
`, id, tenantID); err != nil {
		return BoardView{}, err
	}

	if actorID != "" {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO kan.board_members (organization_id, tenant_id, board_id, user_id, role, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'owner', $4::uuid)
`, orgID, tenantID, id, actorID); err != nil {
			return BoardView{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return BoardView{}, err
	}

	_ = s.AppendActivity(ctx, orgID, tenantID, actorID, &id, nil, "board.created", map[string]any{"name": name, "keyPrefix": prefix})
	return s.GetBoard(ctx, scope{OrgID: orgID, TenantID: tenantID, ActorID: actorID}, id)
}

func (s *Store) GetBoard(ctx context.Context, sc scope, id string) (BoardView, error) {
	include, owner := sc.args()
	row := s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, name, description, key_prefix, is_published,
       card_schema, settings, created_by::text, created_at, updated_at, deleted_at
FROM kan.boards
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid
  AND (deleted_at IS NULL OR ($4 AND created_by::text = $5))
`, id, sc.OrgID, sc.TenantID, include, owner)
	b, err := scanBoard(row)
	if err == sql.ErrNoRows {
		return BoardView{}, errNotFound
	}
	if err != nil {
		return BoardView{}, err
	}
	states, err := s.ListStates(ctx, sc, id)
	if err != nil {
		return BoardView{}, err
	}
	b.States = states
	return b, nil
}

func (s *Store) ListBoards(ctx context.Context, sc scope, published *bool) ([]BoardView, error) {
	include, owner := sc.args()
	q := `
SELECT id::text, organization_id::text, tenant_id::text, name, description, key_prefix, is_published,
       card_schema, settings, created_by::text, created_at, updated_at, deleted_at
FROM kan.boards
WHERE organization_id = $1::uuid AND tenant_id = $2::uuid
  AND (deleted_at IS NULL OR ($3 AND created_by::text = $4))
`
	args := []any{sc.OrgID, sc.TenantID, include, owner}
	if published != nil {
		q += ` AND is_published = $5`
		args = append(args, *published)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BoardView, 0)
	for rows.Next() {
		b, err := scanBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) UpdateBoard(ctx context.Context, sc scope, id string, name, description *string, isPublished *bool, cardSchema, settings []byte, keyPrefix *string) (BoardView, error) {
	cur, err := s.GetBoard(ctx, sc, id)
	if err != nil {
		return BoardView{}, err
	}
	if cur.DeletedAt != nil {
		return BoardView{}, errNotFound
	}
	if keyPrefix != nil {
		n, err := s.countTicketsOnBoard(ctx, sc, id, true)
		if err != nil {
			return BoardView{}, err
		}
		if n > 0 {
			return BoardView{}, errPrefixLocked
		}
		p, err := normalizeKeyPrefix(*keyPrefix)
		if err != nil {
			return BoardView{}, err
		}
		cur.KeyPrefix = p
	}
	if name != nil {
		cur.Name = strings.TrimSpace(*name)
	}
	if description != nil {
		cur.Description = description
	}
	if isPublished != nil {
		cur.IsPublished = *isPublished
	}
	if cardSchema != nil {
		cur.CardSchema = cardSchema
	}
	if settings != nil {
		cur.Settings = settings
	}
	desc := any(nil)
	if cur.Description != nil {
		desc = *cur.Description
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE kan.boards
SET name = $4, description = $5, key_prefix = $6, is_published = $7,
    card_schema = $8::jsonb, settings = $9::jsonb, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, cur.Name, desc, cur.KeyPrefix, cur.IsPublished, jsonOrEmpty(cur.CardSchema), jsonOrEmpty(cur.Settings))
	if err != nil {
		if isUniqueViolation(err) {
			return BoardView{}, errConflict
		}
		return BoardView{}, err
	}
	return s.GetBoard(ctx, sc, id)
}

func (s *Store) SoftDeleteBoard(ctx context.Context, sc scope, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
UPDATE kan.boards SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE kan.states SET deleted_at = now(), updated_at = now()
WHERE board_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.board_members SET deleted_at = now(), updated_at = now()
WHERE board_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE kan.comments SET deleted_at = now(), updated_at = now()
WHERE ticket_id IN (SELECT id FROM kan.tickets WHERE board_id = $1::uuid) AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.links SET deleted_at = now(), updated_at = now()
WHERE ticket_id IN (SELECT id FROM kan.tickets WHERE board_id = $1::uuid) AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.attachments SET deleted_at = now(), updated_at = now()
WHERE ticket_id IN (SELECT id FROM kan.tickets WHERE board_id = $1::uuid) AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.checklist_items SET deleted_at = now(), updated_at = now()
WHERE checklist_id IN (
  SELECT c.id FROM kan.checklists c
  JOIN kan.tickets t ON t.id = c.ticket_id
  WHERE t.board_id = $1::uuid
) AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.checklists SET deleted_at = now(), updated_at = now()
WHERE ticket_id IN (SELECT id FROM kan.tickets WHERE board_id = $1::uuid) AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.time_entries SET deleted_at = now(), updated_at = now()
WHERE ticket_id IN (SELECT id FROM kan.tickets WHERE board_id = $1::uuid) AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.ticket_assignees SET deleted_at = now(), updated_at = now()
WHERE ticket_id IN (SELECT id FROM kan.tickets WHERE board_id = $1::uuid) AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.ticket_watchers SET deleted_at = now(), updated_at = now()
WHERE ticket_id IN (SELECT id FROM kan.tickets WHERE board_id = $1::uuid) AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.ticket_tags SET deleted_at = now(), updated_at = now()
WHERE ticket_id IN (SELECT id FROM kan.tickets WHERE board_id = $1::uuid) AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE kan.tickets SET deleted_at = now(), updated_at = now()
WHERE board_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.AppendActivity(ctx, sc.OrgID, sc.TenantID, sc.ActorID, &id, nil, "board.deleted", map[string]any{})
	return nil
}

func (s *Store) countTicketsOnBoard(ctx context.Context, sc scope, boardID string, includeDeleted bool) (int64, error) {
	q := `SELECT COUNT(*) FROM kan.tickets WHERE board_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid`
	if !includeDeleted {
		q += ` AND deleted_at IS NULL`
	}
	var n int64
	err := s.db.QueryRowContext(ctx, q, boardID, sc.OrgID, sc.TenantID).Scan(&n)
	return n, err
}

func (s *Store) requireLiveBoard(ctx context.Context, sc scope, boardID string) (BoardView, error) {
	b, err := s.GetBoard(ctx, scope{OrgID: sc.OrgID, TenantID: sc.TenantID, ActorID: sc.ActorID, InclDel: false}, boardID)
	if err != nil {
		return BoardView{}, err
	}
	return b, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanBoard(row scanner) (BoardView, error) {
	var b BoardView
	var desc, createdBy sql.NullString
	var card, settings []byte
	var deleted sql.NullTime
	err := row.Scan(&b.ID, &b.OrganizationID, &b.TenantID, &b.Name, &desc, &b.KeyPrefix, &b.IsPublished,
		&card, &settings, &createdBy, &b.CreatedAt, &b.UpdatedAt, &deleted)
	if err != nil {
		return BoardView{}, err
	}
	b.Description = ptrStr(desc)
	b.CreatedBy = ptrStr(createdBy)
	b.CardSchema = rawJSON(card)
	b.Settings = rawJSON(settings)
	b.DeletedAt = ptrTime(deleted)
	return b, nil
}

package main

import (
	"context"
	"database/sql"
	"strings"
)

func (s *Store) ListStates(ctx context.Context, sc scope, boardID string) ([]StateView, error) {
	include, owner := sc.args()
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, board_id::text, name, position, is_default,
       wip_limit, color, created_by::text, created_at, updated_at, deleted_at
FROM kan.states
WHERE board_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid
  AND (deleted_at IS NULL OR ($4 AND created_by::text = $5))
ORDER BY position ASC, created_at ASC
`, boardID, sc.OrgID, sc.TenantID, include, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]StateView, 0)
	for rows.Next() {
		st, err := scanState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) GetState(ctx context.Context, sc scope, id string) (StateView, error) {
	include, owner := sc.args()
	row := s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, board_id::text, name, position, is_default,
       wip_limit, color, created_by::text, created_at, updated_at, deleted_at
FROM kan.states
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid
  AND (deleted_at IS NULL OR ($4 AND created_by::text = $5))
`, id, sc.OrgID, sc.TenantID, include, owner)
	st, err := scanState(row)
	if err == sql.ErrNoRows {
		return StateView{}, errNotFound
	}
	return st, err
}

func (s *Store) CreateState(ctx context.Context, sc scope, boardID, name, color string, position int, isDefault bool, wipLimit *int) (StateView, error) {
	if _, err := s.requireLiveBoard(ctx, sc, boardID); err != nil {
		return StateView{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StateView{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if isDefault {
		if _, err := tx.ExecContext(ctx, `
UPDATE kan.states SET is_default = false, updated_at = now()
WHERE board_id = $1::uuid AND tenant_id = $2::uuid AND deleted_at IS NULL
`, boardID, sc.TenantID); err != nil {
			return StateView{}, err
		}
	}
	var id string
	err = tx.QueryRowContext(ctx, `
INSERT INTO kan.states (organization_id, tenant_id, board_id, name, position, is_default, wip_limit, color, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9::uuid)
RETURNING id::text
`, sc.OrgID, sc.TenantID, boardID, name, position, isDefault, wipLimit, nilIfEmpty(color), nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		return StateView{}, err
	}
	if err := tx.Commit(); err != nil {
		return StateView{}, err
	}
	s.publish(sc, "state.created", boardID, "", map[string]any{"stateId": id, "name": name})
	return s.GetState(ctx, sc, id)
}

func (s *Store) UpdateState(ctx context.Context, sc scope, id string, name *string, position *int, isDefault *bool, wipLimit *int, clearWIP bool, color *string) (StateView, error) {
	cur, err := s.GetState(ctx, sc, id)
	if err != nil {
		return StateView{}, err
	}
	if cur.DeletedAt != nil {
		return StateView{}, errNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StateView{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if isDefault != nil && *isDefault {
		if _, err := tx.ExecContext(ctx, `
UPDATE kan.states SET is_default = false, updated_at = now()
WHERE board_id = $1::uuid AND tenant_id = $2::uuid AND deleted_at IS NULL AND id <> $3::uuid
`, cur.BoardID, sc.TenantID, id); err != nil {
			return StateView{}, err
		}
		cur.IsDefault = true
	} else if isDefault != nil {
		cur.IsDefault = false
	}
	if name != nil {
		cur.Name = strings.TrimSpace(*name)
	}
	if position != nil {
		cur.Position = *position
	}
	if clearWIP {
		cur.WIPLimit = nil
	} else if wipLimit != nil {
		cur.WIPLimit = wipLimit
	}
	if color != nil {
		cur.Color = color
	}
	_, err = tx.ExecContext(ctx, `
UPDATE kan.states
SET name = $4, position = $5, is_default = $6, wip_limit = $7, color = $8, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, cur.Name, cur.Position, cur.IsDefault, cur.WIPLimit, nilIfEmpty(deref(cur.Color)))
	if err != nil {
		return StateView{}, err
	}
	if err := tx.Commit(); err != nil {
		return StateView{}, err
	}
	s.publish(sc, "state.updated", cur.BoardID, "", map[string]any{"stateId": id, "name": cur.Name})
	return s.GetState(ctx, sc, id)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Store) SoftDeleteState(ctx context.Context, sc scope, id string, force bool) error {
	st, err := s.GetState(ctx, sc, id)
	if err != nil {
		return err
	}
	if st.DeletedAt != nil {
		return errNotFound
	}
	var n int64
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM kan.tickets
WHERE state_id = $1::uuid AND deleted_at IS NULL
`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 && !force {
		return errHasTickets
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if n > 0 {
		var defID string
		err = tx.QueryRowContext(ctx, `
SELECT id::text FROM kan.states
WHERE board_id = $1::uuid AND tenant_id = $2::uuid AND is_default = true AND deleted_at IS NULL
LIMIT 1
`, st.BoardID, sc.TenantID).Scan(&defID)
		if err == sql.ErrNoRows || defID == "" || defID == id {
			return errInvalid
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE kan.tickets SET state_id = $2::uuid, updated_at = now()
WHERE state_id = $1::uuid AND deleted_at IS NULL
`, id, defID); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `
UPDATE kan.states SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errNotFound
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Tickets may have been reassigned to the default state, so the whole
	// board is stale, not just this swimlane.
	s.publish(sc, "state.deleted", st.BoardID, "", map[string]any{"stateId": id, "movedTickets": n})
	return nil
}

func (s *Store) ReorderStates(ctx context.Context, sc scope, boardID string, ids []string) error {
	if _, err := s.requireLiveBoard(ctx, sc, boardID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, id := range ids {
		res, err := tx.ExecContext(ctx, `
UPDATE kan.states SET position = $4, updated_at = now()
WHERE id = $1::uuid AND board_id = $2::uuid AND organization_id = $3::uuid AND tenant_id = $5::uuid AND deleted_at IS NULL
`, id, boardID, sc.OrgID, i, sc.TenantID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return errNotFound
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.publish(sc, "states.reordered", boardID, "", map[string]any{"stateIds": ids})
	return nil
}

func (s *Store) defaultStateID(ctx context.Context, sc scope, boardID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
SELECT id::text FROM kan.states
WHERE board_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid
  AND is_default = true AND deleted_at IS NULL
ORDER BY position ASC LIMIT 1
`, boardID, sc.OrgID, sc.TenantID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", errNotFound
	}
	return id, err
}

func scanState(row scanner) (StateView, error) {
	var st StateView
	var wip sql.NullInt64
	var color, createdBy sql.NullString
	var deleted sql.NullTime
	err := row.Scan(&st.ID, &st.OrganizationID, &st.TenantID, &st.BoardID, &st.Name, &st.Position, &st.IsDefault,
		&wip, &color, &createdBy, &st.CreatedAt, &st.UpdatedAt, &deleted)
	if err != nil {
		return StateView{}, err
	}
	st.WIPLimit = ptrInt(wip)
	st.Color = ptrStr(color)
	st.CreatedBy = ptrStr(createdBy)
	st.DeletedAt = ptrTime(deleted)
	return st, nil
}

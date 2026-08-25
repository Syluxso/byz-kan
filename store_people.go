package main

import (
	"context"
	"database/sql"
)

func (s *Store) ListMembers(ctx context.Context, sc scope, boardID string) ([]MemberView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, board_id::text, user_id::text, role, created_by::text, created_at, updated_at
FROM kan.board_members
WHERE board_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
ORDER BY created_at ASC
`, boardID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MemberView, 0)
	for rows.Next() {
		var m MemberView
		var createdBy sql.NullString
		if err := rows.Scan(&m.ID, &m.OrganizationID, &m.TenantID, &m.BoardID, &m.UserID, &m.Role, &createdBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.CreatedBy = ptrStr(createdBy)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) AddMember(ctx context.Context, sc scope, boardID, userID, role string) (MemberView, error) {
	if _, err := s.requireLiveBoard(ctx, sc, boardID); err != nil {
		return MemberView{}, err
	}
	if role == "" {
		role = "member"
	}
	if role != "owner" && role != "member" {
		return MemberView{}, errInvalid
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO kan.board_members (organization_id, tenant_id, board_id, user_id, role, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6::uuid)
ON CONFLICT DO NOTHING
RETURNING id::text
`, sc.OrgID, sc.TenantID, boardID, userID, role, nilIfEmpty(sc.ActorID)).Scan(&id)
	if err == sql.ErrNoRows {
		// unique index is partial (deleted_at IS NULL), ON CONFLICT DO NOTHING needs a constraint name.
		// Re-activate soft-deleted membership if present.
		err = s.db.QueryRowContext(ctx, `
UPDATE kan.board_members
SET deleted_at = NULL, role = $4, updated_at = now()
WHERE board_id = $1::uuid AND user_id = $2::uuid AND tenant_id = $3::uuid
RETURNING id::text
`, boardID, userID, sc.TenantID, role).Scan(&id)
	}
	if err != nil {
		if isUniqueViolation(err) {
			return MemberView{}, errConflict
		}
		return MemberView{}, err
	}
	return s.getMember(ctx, sc, id)
}

func (s *Store) getMember(ctx context.Context, sc scope, id string) (MemberView, error) {
	var m MemberView
	var createdBy sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, board_id::text, user_id::text, role, created_by::text, created_at, updated_at
FROM kan.board_members WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID).Scan(&m.ID, &m.OrganizationID, &m.TenantID, &m.BoardID, &m.UserID, &m.Role, &createdBy, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return MemberView{}, errNotFound
	}
	m.CreatedBy = ptrStr(createdBy)
	return m, err
}

func (s *Store) RemoveMember(ctx context.Context, sc scope, boardID, userID string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.board_members SET deleted_at = now(), updated_at = now()
WHERE board_id = $1::uuid AND user_id = $2::uuid AND organization_id = $3::uuid AND tenant_id = $4::uuid AND deleted_at IS NULL
`, boardID, userID, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (s *Store) ListAssignees(ctx context.Context, sc scope, ticketID string) ([]PersonLinkView, error) {
	return s.listPeople(ctx, sc, "kan.ticket_assignees", ticketID)
}

func (s *Store) AddAssignee(ctx context.Context, sc scope, ticketID, userID string) (PersonLinkView, error) {
	return s.addPerson(ctx, sc, "kan.ticket_assignees", ticketID, userID)
}

func (s *Store) RemoveAssignee(ctx context.Context, sc scope, ticketID, userID string) error {
	return s.removePerson(ctx, sc, "kan.ticket_assignees", ticketID, userID)
}

func (s *Store) ListWatchers(ctx context.Context, sc scope, ticketID string) ([]PersonLinkView, error) {
	return s.listPeople(ctx, sc, "kan.ticket_watchers", ticketID)
}

func (s *Store) AddWatcher(ctx context.Context, sc scope, ticketID, userID string) (PersonLinkView, error) {
	return s.addPerson(ctx, sc, "kan.ticket_watchers", ticketID, userID)
}

func (s *Store) RemoveWatcher(ctx context.Context, sc scope, ticketID, userID string) error {
	return s.removePerson(ctx, sc, "kan.ticket_watchers", ticketID, userID)
}

func (s *Store) listPeople(ctx context.Context, sc scope, table, ticketID string) ([]PersonLinkView, error) {
	if !allowedPeopleTable(table) {
		return nil, errInvalid
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, user_id::text, created_by::text, created_at
FROM `+table+`
WHERE ticket_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
ORDER BY created_at ASC
`, ticketID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PersonLinkView, 0)
	for rows.Next() {
		var p PersonLinkView
		var createdBy sql.NullString
		if err := rows.Scan(&p.ID, &p.OrganizationID, &p.TenantID, &p.TicketID, &p.UserID, &createdBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.CreatedBy = ptrStr(createdBy)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) addPerson(ctx context.Context, sc scope, table, ticketID, userID string) (PersonLinkView, error) {
	if !allowedPeopleTable(table) {
		return PersonLinkView{}, errInvalid
	}
	if _, err := s.GetTicketByID(ctx, sc, ticketID); err != nil {
		return PersonLinkView{}, err
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO `+table+` (organization_id, tenant_id, ticket_id, user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid)
RETURNING id::text
`, sc.OrgID, sc.TenantID, ticketID, userID, nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			err = s.db.QueryRowContext(ctx, `
UPDATE `+table+`
SET deleted_at = NULL, updated_at = now()
WHERE ticket_id = $1::uuid AND user_id = $2::uuid AND tenant_id = $3::uuid
RETURNING id::text
`, ticketID, userID, sc.TenantID).Scan(&id)
		}
		if err != nil {
			return PersonLinkView{}, err
		}
	}
	var p PersonLinkView
	var createdBy sql.NullString
	err = s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, user_id::text, created_by::text, created_at
FROM `+table+` WHERE id = $1::uuid
`, id).Scan(&p.ID, &p.OrganizationID, &p.TenantID, &p.TicketID, &p.UserID, &createdBy, &p.CreatedAt)
	p.CreatedBy = ptrStr(createdBy)
	return p, err
}

func (s *Store) removePerson(ctx context.Context, sc scope, table, ticketID, userID string) error {
	if !allowedPeopleTable(table) {
		return errInvalid
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE `+table+` SET deleted_at = now(), updated_at = now()
WHERE ticket_id = $1::uuid AND user_id = $2::uuid AND organization_id = $3::uuid AND tenant_id = $4::uuid AND deleted_at IS NULL
`, ticketID, userID, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	return nil
}

func allowedPeopleTable(t string) bool {
	return t == "kan.ticket_assignees" || t == "kan.ticket_watchers"
}

func (s *Store) ReplaceAssignees(ctx context.Context, sc scope, ticketID string, userIDs []string) ([]PersonLinkView, error) {
	return s.replacePeople(ctx, sc, "kan.ticket_assignees", ticketID, userIDs)
}

func (s *Store) ReplaceWatchers(ctx context.Context, sc scope, ticketID string, userIDs []string) ([]PersonLinkView, error) {
	return s.replacePeople(ctx, sc, "kan.ticket_watchers", ticketID, userIDs)
}

func (s *Store) replacePeople(ctx context.Context, sc scope, table, ticketID string, userIDs []string) ([]PersonLinkView, error) {
	if !allowedPeopleTable(table) {
		return nil, errInvalid
	}
	if _, err := s.GetTicketByID(ctx, sc, ticketID); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
UPDATE `+table+` SET deleted_at = now(), updated_at = now()
WHERE ticket_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, ticketID, sc.OrgID, sc.TenantID); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, uid := range userIDs {
		if !isUUID(uid) || seen[uid] {
			continue
		}
		seen[uid] = true
		var id string
		err := tx.QueryRowContext(ctx, `
INSERT INTO `+table+` (organization_id, tenant_id, ticket_id, user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid)
RETURNING id::text
`, sc.OrgID, sc.TenantID, ticketID, uid, nilIfEmpty(sc.ActorID)).Scan(&id)
		if err != nil {
			if !isUniqueViolation(err) {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `
UPDATE `+table+`
SET deleted_at = NULL, updated_at = now()
WHERE ticket_id = $1::uuid AND user_id = $2::uuid AND tenant_id = $3::uuid
`, ticketID, uid, sc.TenantID); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.listPeople(ctx, sc, table, ticketID)
}

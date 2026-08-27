package main

import (
	"context"
	"database/sql"
	"strings"
)

func (s *Store) ListTags(ctx context.Context, sc scope, kind string) ([]TagView, error) {
	q := `
SELECT id::text, organization_id::text, tenant_id::text, name, kind, color, created_by::text, created_at, updated_at, deleted_at
FROM kan.tags
WHERE organization_id = $1::uuid AND tenant_id = $2::uuid AND deleted_at IS NULL
`
	args := []any{sc.OrgID, sc.TenantID}
	if kind != "" {
		q += ` AND kind = $3`
		args = append(args, kind)
	}
	q += ` ORDER BY name ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TagView, 0)
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateTag(ctx context.Context, sc scope, name, kind, color string) (TagView, error) {
	if kind == "" {
		kind = "label"
	}
	// CW-14: store the bare slug so "#mcp" and "mcp" cannot become two tags.
	name = normalizeTagName(name)
	if name == "" {
		return TagView{}, errInvalid
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO kan.tags (organization_id, tenant_id, name, kind, color, created_by)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::uuid)
RETURNING id::text
`, sc.OrgID, sc.TenantID, strings.TrimSpace(name), kind, nilIfEmpty(color), nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return TagView{}, errConflict
		}
		return TagView{}, err
	}
	return s.getTag(ctx, sc, id)
}

func (s *Store) getTag(ctx context.Context, sc scope, id string) (TagView, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, name, kind, color, created_by::text, created_at, updated_at, deleted_at
FROM kan.tags WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	t, err := scanTag(row)
	if err == sql.ErrNoRows {
		return TagView{}, errNotFound
	}
	return t, err
}

func (s *Store) UpdateTag(ctx context.Context, sc scope, id string, name, kind, color *string) (TagView, error) {
	cur, err := s.getTag(ctx, sc, id)
	if err != nil {
		return TagView{}, err
	}
	if name != nil {
		cur.Name = normalizeTagName(*name)
		if cur.Name == "" {
			return TagView{}, errInvalid
		}
	}
	if kind != nil {
		cur.Kind = *kind
	}
	if color != nil {
		cur.Color = color
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE kan.tags SET name = $4, kind = $5, color = $6, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, cur.Name, cur.Kind, nilIfEmpty(deref(cur.Color)))
	if err != nil {
		if isUniqueViolation(err) {
			return TagView{}, errConflict
		}
		return TagView{}, err
	}
	return s.getTag(ctx, sc, id)
}

func (s *Store) SoftDeleteTag(ctx context.Context, sc scope, id string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.tags SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (s *Store) AddTicketTag(ctx context.Context, sc scope, ticketID, tagID string) error {
	if _, err := s.GetTicketByID(ctx, sc, ticketID); err != nil {
		return err
	}
	if _, err := s.getTag(ctx, sc, tagID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO kan.ticket_tags (organization_id, tenant_id, ticket_id, tag_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid)
`, sc.OrgID, sc.TenantID, ticketID, tagID, nilIfEmpty(sc.ActorID))
	if isUniqueViolation(err) {
		_, err = s.db.ExecContext(ctx, `
UPDATE kan.ticket_tags SET deleted_at = NULL, updated_at = now()
WHERE ticket_id = $1::uuid AND tag_id = $2::uuid AND tenant_id = $3::uuid
`, ticketID, tagID, sc.TenantID)
	}
	return err
}

func (s *Store) RemoveTicketTag(ctx context.Context, sc scope, ticketID, tagID string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.ticket_tags SET deleted_at = now(), updated_at = now()
WHERE ticket_id = $1::uuid AND tag_id = $2::uuid AND organization_id = $3::uuid AND tenant_id = $4::uuid AND deleted_at IS NULL
`, ticketID, tagID, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (s *Store) ListTicketTags(ctx context.Context, sc scope, ticketID string) ([]TagView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT g.id::text, g.organization_id::text, g.tenant_id::text, g.name, g.kind, g.color, g.created_by::text, g.created_at, g.updated_at, g.deleted_at
FROM kan.tags g
JOIN kan.ticket_tags tt ON tt.tag_id = g.id AND tt.deleted_at IS NULL
WHERE tt.ticket_id = $1::uuid AND g.organization_id = $2::uuid AND g.tenant_id = $3::uuid AND g.deleted_at IS NULL
ORDER BY g.name
`, ticketID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TagView, 0)
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanTag(row scanner) (TagView, error) {
	var t TagView
	var color, createdBy sql.NullString
	var deleted sql.NullTime
	err := row.Scan(&t.ID, &t.OrganizationID, &t.TenantID, &t.Name, &t.Kind, &color, &createdBy, &t.CreatedAt, &t.UpdatedAt, &deleted)
	t.Color = ptrStr(color)
	t.CreatedBy = ptrStr(createdBy)
	t.DeletedAt = ptrTime(deleted)
	return t, err
}

func (s *Store) ListComments(ctx context.Context, sc scope, ticketID string) ([]CommentView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, body, created_by::text, created_at, updated_at, deleted_at
FROM kan.comments
WHERE ticket_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
ORDER BY created_at ASC
`, ticketID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CommentView, 0)
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CreateComment(ctx context.Context, sc scope, ticketID, body string) (CommentView, error) {
	if _, err := s.GetTicketByID(ctx, sc, ticketID); err != nil {
		return CommentView{}, err
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO kan.comments (organization_id, tenant_id, ticket_id, body, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::uuid) RETURNING id::text
`, sc.OrgID, sc.TenantID, ticketID, body, nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		return CommentView{}, err
	}
	return s.getComment(ctx, sc, id)
}

func (s *Store) getComment(ctx context.Context, sc scope, id string) (CommentView, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, body, created_by::text, created_at, updated_at, deleted_at
FROM kan.comments WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	c, err := scanComment(row)
	if err == sql.ErrNoRows {
		return CommentView{}, errNotFound
	}
	return c, err
}

func (s *Store) UpdateComment(ctx context.Context, sc scope, id, body string) (CommentView, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.comments SET body = $4, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, body)
	if err != nil {
		return CommentView{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return CommentView{}, errNotFound
	}
	return s.getComment(ctx, sc, id)
}

func (s *Store) SoftDeleteComment(ctx context.Context, sc scope, id string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.comments SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	return nil
}

func scanComment(row scanner) (CommentView, error) {
	var c CommentView
	var createdBy sql.NullString
	var deleted sql.NullTime
	err := row.Scan(&c.ID, &c.OrganizationID, &c.TenantID, &c.TicketID, &c.Body, &createdBy, &c.CreatedAt, &c.UpdatedAt, &deleted)
	c.CreatedBy = ptrStr(createdBy)
	c.DeletedAt = ptrTime(deleted)
	return c, err
}

func (s *Store) ListLinks(ctx context.Context, sc scope, ticketID string) ([]LinkView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, url, title, link_type, created_by::text, created_at, updated_at, deleted_at
FROM kan.links WHERE ticket_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
ORDER BY created_at ASC
`, ticketID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]LinkView, 0)
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) CreateLink(ctx context.Context, sc scope, ticketID, url, title, linkType string) (LinkView, error) {
	if _, err := s.GetTicketByID(ctx, sc, ticketID); err != nil {
		return LinkView{}, err
	}
	if linkType == "" {
		linkType = "related"
	}
	switch linkType {
	case "related", "blocks", "remote_file", "other":
	default:
		return LinkView{}, errInvalid
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO kan.links (organization_id, tenant_id, ticket_id, url, title, link_type, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::uuid) RETURNING id::text
`, sc.OrgID, sc.TenantID, ticketID, url, nilIfEmpty(title), linkType, nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		return LinkView{}, err
	}
	return s.getLink(ctx, sc, id)
}

func (s *Store) getLink(ctx context.Context, sc scope, id string) (LinkView, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, url, title, link_type, created_by::text, created_at, updated_at, deleted_at
FROM kan.links WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	l, err := scanLink(row)
	if err == sql.ErrNoRows {
		return LinkView{}, errNotFound
	}
	return l, err
}

func (s *Store) UpdateLink(ctx context.Context, sc scope, id string, url, title, linkType *string) (LinkView, error) {
	cur, err := s.getLink(ctx, sc, id)
	if err != nil {
		return LinkView{}, err
	}
	if url != nil {
		cur.URL = strings.TrimSpace(*url)
	}
	if title != nil {
		cur.Title = title
	}
	if linkType != nil {
		switch *linkType {
		case "related", "blocks", "remote_file", "other":
			cur.LinkType = *linkType
		default:
			return LinkView{}, errInvalid
		}
	}
	if cur.URL == "" {
		return LinkView{}, errInvalid
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE kan.links SET url = $4, title = $5, link_type = $6, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, cur.URL, nilIfEmpty(deref(cur.Title)), cur.LinkType)
	if err != nil {
		return LinkView{}, err
	}
	return s.getLink(ctx, sc, id)
}

func (s *Store) SoftDeleteLink(ctx context.Context, sc scope, id string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.links SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	return nil
}

func scanLink(row scanner) (LinkView, error) {
	var l LinkView
	var title, createdBy sql.NullString
	var deleted sql.NullTime
	err := row.Scan(&l.ID, &l.OrganizationID, &l.TenantID, &l.TicketID, &l.URL, &title, &l.LinkType, &createdBy, &l.CreatedAt, &l.UpdatedAt, &deleted)
	l.Title = ptrStr(title)
	l.CreatedBy = ptrStr(createdBy)
	l.DeletedAt = ptrTime(deleted)
	return l, err
}

func (s *Store) ListAttachments(ctx context.Context, sc scope, ticketID string) ([]AttachmentView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, file_id::text, filename, content_type, size_bytes, created_by::text, created_at, updated_at, deleted_at
FROM kan.attachments WHERE ticket_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
ORDER BY created_at ASC
`, ticketID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AttachmentView, 0)
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) CreateAttachment(ctx context.Context, sc scope, ticketID, fileID, filename, contentType string, sizeBytes *int64) (AttachmentView, error) {
	if !isUUID(fileID) {
		return AttachmentView{}, errInvalid
	}
	if _, err := s.GetTicketByID(ctx, sc, ticketID); err != nil {
		return AttachmentView{}, err
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO kan.attachments (organization_id, tenant_id, ticket_id, file_id, filename, content_type, size_bytes, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8::uuid) RETURNING id::text
`, sc.OrgID, sc.TenantID, ticketID, fileID, nilIfEmpty(filename), nilIfEmpty(contentType), sizeBytes, nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		return AttachmentView{}, err
	}
	return s.getAttachment(ctx, sc, id)
}

func (s *Store) getAttachment(ctx context.Context, sc scope, id string) (AttachmentView, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, file_id::text, filename, content_type, size_bytes, created_by::text, created_at, updated_at, deleted_at
FROM kan.attachments WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	a, err := scanAttachment(row)
	if err == sql.ErrNoRows {
		return AttachmentView{}, errNotFound
	}
	return a, err
}

func (s *Store) SoftDeleteAttachment(ctx context.Context, sc scope, id string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.attachments SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	return nil
}

func scanAttachment(row scanner) (AttachmentView, error) {
	var a AttachmentView
	var filename, contentType, createdBy sql.NullString
	var size sql.NullInt64
	var deleted sql.NullTime
	err := row.Scan(&a.ID, &a.OrganizationID, &a.TenantID, &a.TicketID, &a.FileID, &filename, &contentType, &size, &createdBy, &a.CreatedAt, &a.UpdatedAt, &deleted)
	a.Filename = ptrStr(filename)
	a.ContentType = ptrStr(contentType)
	a.SizeBytes = ptrInt64(size)
	a.CreatedBy = ptrStr(createdBy)
	a.DeletedAt = ptrTime(deleted)
	return a, err
}

func (s *Store) ListChecklists(ctx context.Context, sc scope, ticketID string) ([]ChecklistView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, title, position, created_by::text, created_at, updated_at
FROM kan.checklists WHERE ticket_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
ORDER BY position ASC, created_at ASC
`, ticketID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChecklistView, 0)
	for rows.Next() {
		var c ChecklistView
		var createdBy sql.NullString
		if err := rows.Scan(&c.ID, &c.OrganizationID, &c.TenantID, &c.TicketID, &c.Title, &c.Position, &createdBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.CreatedBy = ptrStr(createdBy)
		items, err := s.ListChecklistItems(ctx, sc, c.ID)
		if err != nil {
			return nil, err
		}
		c.Items = items
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CreateChecklist(ctx context.Context, sc scope, ticketID, title string, position int) (ChecklistView, error) {
	if _, err := s.GetTicketByID(ctx, sc, ticketID); err != nil {
		return ChecklistView{}, err
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO kan.checklists (organization_id, tenant_id, ticket_id, title, position, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid) RETURNING id::text
`, sc.OrgID, sc.TenantID, ticketID, title, position, nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		return ChecklistView{}, err
	}
	return s.getChecklist(ctx, sc, id)
}

func (s *Store) getChecklist(ctx context.Context, sc scope, id string) (ChecklistView, error) {
	var c ChecklistView
	var createdBy sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, title, position, created_by::text, created_at, updated_at
FROM kan.checklists WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID).Scan(&c.ID, &c.OrganizationID, &c.TenantID, &c.TicketID, &c.Title, &c.Position, &createdBy, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return ChecklistView{}, errNotFound
	}
	if err != nil {
		return ChecklistView{}, err
	}
	c.CreatedBy = ptrStr(createdBy)
	items, err := s.ListChecklistItems(ctx, sc, id)
	if err != nil {
		return ChecklistView{}, err
	}
	c.Items = items
	return c, nil
}

func (s *Store) UpdateChecklist(ctx context.Context, sc scope, id string, title *string, position *int) (ChecklistView, error) {
	cur, err := s.getChecklist(ctx, sc, id)
	if err != nil {
		return ChecklistView{}, err
	}
	if title != nil {
		cur.Title = strings.TrimSpace(*title)
		if cur.Title == "" {
			return ChecklistView{}, errInvalid
		}
	}
	if position != nil {
		cur.Position = *position
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE kan.checklists SET title = $4, position = $5, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, cur.Title, cur.Position)
	if err != nil {
		return ChecklistView{}, err
	}
	return s.getChecklist(ctx, sc, id)
}

func (s *Store) SoftDeleteChecklist(ctx context.Context, sc scope, id string) error {
	if _, err := s.db.ExecContext(ctx, `
UPDATE kan.checklist_items SET deleted_at = now(), updated_at = now()
WHERE checklist_id = $1::uuid AND deleted_at IS NULL
`, id); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.checklists SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (s *Store) ListChecklistItems(ctx context.Context, sc scope, checklistID string) ([]ChecklistItemView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, checklist_id::text, title, is_done, position, created_by::text, created_at, updated_at
FROM kan.checklist_items WHERE checklist_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
ORDER BY position ASC, created_at ASC
`, checklistID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChecklistItemView, 0)
	for rows.Next() {
		var it ChecklistItemView
		var createdBy sql.NullString
		if err := rows.Scan(&it.ID, &it.OrganizationID, &it.TenantID, &it.ChecklistID, &it.Title, &it.IsDone, &it.Position, &createdBy, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		it.CreatedBy = ptrStr(createdBy)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) CreateChecklistItem(ctx context.Context, sc scope, checklistID, title string, position int) (ChecklistItemView, error) {
	if _, err := s.getChecklist(ctx, sc, checklistID); err != nil {
		return ChecklistItemView{}, err
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO kan.checklist_items (organization_id, tenant_id, checklist_id, title, position, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid) RETURNING id::text
`, sc.OrgID, sc.TenantID, checklistID, title, position, nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		return ChecklistItemView{}, err
	}
	return s.getChecklistItem(ctx, sc, id)
}

func (s *Store) getChecklistItem(ctx context.Context, sc scope, id string) (ChecklistItemView, error) {
	var it ChecklistItemView
	var createdBy sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, checklist_id::text, title, is_done, position, created_by::text, created_at, updated_at
FROM kan.checklist_items WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID).Scan(&it.ID, &it.OrganizationID, &it.TenantID, &it.ChecklistID, &it.Title, &it.IsDone, &it.Position, &createdBy, &it.CreatedAt, &it.UpdatedAt)
	if err == sql.ErrNoRows {
		return ChecklistItemView{}, errNotFound
	}
	it.CreatedBy = ptrStr(createdBy)
	return it, err
}

func (s *Store) UpdateChecklistItem(ctx context.Context, sc scope, id string, title *string, isDone *bool, position *int) (ChecklistItemView, error) {
	cur, err := s.getChecklistItem(ctx, sc, id)
	if err != nil {
		return ChecklistItemView{}, err
	}
	if title != nil {
		cur.Title = *title
	}
	if isDone != nil {
		cur.IsDone = *isDone
	}
	if position != nil {
		cur.Position = *position
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE kan.checklist_items SET title = $4, is_done = $5, position = $6, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, cur.Title, cur.IsDone, cur.Position)
	if err != nil {
		return ChecklistItemView{}, err
	}
	return s.getChecklistItem(ctx, sc, id)
}

func (s *Store) SoftDeleteChecklistItem(ctx context.Context, sc scope, id string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.checklist_items SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	return nil
}

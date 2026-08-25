package main

import (
	"context"
	"database/sql"
	"math"
	"time"
)

func minutesFromRange(start, end time.Time) int {
	if !end.After(start) {
		return 0
	}
	return int(math.Ceil(end.Sub(start).Seconds() / 60.0))
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func (s *Store) ListTimeEntries(ctx context.Context, sc scope, ticketID string) ([]TimeEntryView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, user_id::text,
       started_at, ended_at, minutes, note, created_by::text, created_at, updated_at
FROM kan.time_entries
WHERE ticket_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
ORDER BY created_at ASC
`, ticketID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TimeEntryView, 0)
	for rows.Next() {
		v, err := scanTimeEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) CreateTimeEntry(ctx context.Context, sc scope, ticketID, userID string, startedAt, endedAt *time.Time, minutes int, note string) (TimeEntryView, error) {
	tkt, err := s.GetTicketByID(ctx, sc, ticketID)
	if err != nil {
		return TimeEntryView{}, err
	}
	if userID == "" {
		userID = sc.ActorID
	}
	if userID == "" {
		return TimeEntryView{}, errInvalid
	}
	minutes, err = normalizeTimeMinutes(startedAt, endedAt, minutes)
	if err != nil {
		return TimeEntryView{}, err
	}
	var id string
	err = s.db.QueryRowContext(ctx, `
INSERT INTO kan.time_entries (organization_id, tenant_id, ticket_id, user_id, started_at, ended_at, minutes, note, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, $9::uuid)
RETURNING id::text
`, sc.OrgID, sc.TenantID, ticketID, userID, startedAt, endedAt, minutes, nilIfEmpty(note), nilIfEmpty(sc.ActorID)).Scan(&id)
	if err != nil {
		return TimeEntryView{}, err
	}
	if err := s.recomputeLoggedMinutes(ctx, ticketID); err != nil {
		return TimeEntryView{}, err
	}
	_ = s.AppendActivity(ctx, sc.OrgID, sc.TenantID, sc.ActorID, &tkt.BoardID, &ticketID, "time.logged", map[string]any{"minutes": minutes})
	return s.getTimeEntry(ctx, sc, id)
}

func normalizeTimeMinutes(startedAt, endedAt *time.Time, minutes int) (int, error) {
	if startedAt != nil && endedAt != nil {
		if minutes <= 0 {
			minutes = minutesFromRange(*startedAt, *endedAt)
		}
	}
	if minutes <= 0 && (startedAt == nil || endedAt == nil) {
		return 0, errInvalid
	}
	if minutes < 0 {
		return 0, errInvalid
	}
	if minutes == 0 {
		return 0, errInvalid
	}
	return minutes, nil
}

func (s *Store) getTimeEntry(ctx context.Context, sc scope, id string) (TimeEntryView, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, ticket_id::text, user_id::text,
       started_at, ended_at, minutes, note, created_by::text, created_at, updated_at
FROM kan.time_entries
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	v, err := scanTimeEntry(row)
	if err == sql.ErrNoRows {
		return TimeEntryView{}, errNotFound
	}
	return v, err
}

func (s *Store) UpdateTimeEntry(ctx context.Context, sc scope, id string, startedAt, endedAt *time.Time, minutes *int, note *string) (TimeEntryView, error) {
	cur, err := s.getTimeEntry(ctx, sc, id)
	if err != nil {
		return TimeEntryView{}, err
	}
	if startedAt != nil {
		cur.StartedAt = startedAt
	}
	if endedAt != nil {
		cur.EndedAt = endedAt
	}
	m := cur.Minutes
	if minutes != nil {
		m = *minutes
	}
	m, err = normalizeTimeMinutes(cur.StartedAt, cur.EndedAt, m)
	if err != nil {
		return TimeEntryView{}, err
	}
	if note != nil {
		cur.Note = note
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE kan.time_entries SET started_at = $4, ended_at = $5, minutes = $6, note = $7, updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID, cur.StartedAt, cur.EndedAt, m, nilIfEmpty(deref(cur.Note)))
	if err != nil {
		return TimeEntryView{}, err
	}
	if err := s.recomputeLoggedMinutes(ctx, cur.TicketID); err != nil {
		return TimeEntryView{}, err
	}
	return s.getTimeEntry(ctx, sc, id)
}

func (s *Store) SoftDeleteTimeEntry(ctx context.Context, sc scope, id string) error {
	cur, err := s.getTimeEntry(ctx, sc, id)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE kan.time_entries SET deleted_at = now(), updated_at = now()
WHERE id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL
`, id, sc.OrgID, sc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound
	}
	return s.recomputeLoggedMinutes(ctx, cur.TicketID)
}

func (s *Store) recomputeLoggedMinutes(ctx context.Context, ticketID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE kan.tickets SET logged_minutes = COALESCE((
  SELECT SUM(minutes) FROM kan.time_entries WHERE ticket_id = $1::uuid AND deleted_at IS NULL
), 0), updated_at = now()
WHERE id = $1::uuid
`, ticketID)
	return err
}

func scanTimeEntry(row scanner) (TimeEntryView, error) {
	var v TimeEntryView
	var started, ended sql.NullTime
	var note, createdBy sql.NullString
	err := row.Scan(&v.ID, &v.OrganizationID, &v.TenantID, &v.TicketID, &v.UserID, &started, &ended, &v.Minutes, &note, &createdBy, &v.CreatedAt, &v.UpdatedAt)
	v.StartedAt = ptrTime(started)
	v.EndedAt = ptrTime(ended)
	v.Note = ptrStr(note)
	v.CreatedBy = ptrStr(createdBy)
	return v, err
}

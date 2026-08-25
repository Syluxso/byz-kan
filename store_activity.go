package main

import (
	"context"
	"database/sql"
	"encoding/json"
)

func (s *Store) AppendActivity(ctx context.Context, orgID, tenantID, actorID string, boardID, ticketID *string, action string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var bid, tid any
	if boardID != nil && *boardID != "" {
		bid = *boardID
	}
	if ticketID != nil && *ticketID != "" {
		tid = *ticketID
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO kan.activity_events (organization_id, tenant_id, board_id, ticket_id, actor_id, action, payload)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7::jsonb)
`, orgID, tenantID, bid, tid, nilIfEmpty(actorID), action, string(b))
	return err
}

func (s *Store) ListTicketActivity(ctx context.Context, sc scope, ticketID string) ([]ActivityView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, board_id::text, ticket_id::text, actor_id::text, action, payload, created_at
FROM kan.activity_events
WHERE ticket_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid
ORDER BY created_at DESC
LIMIT 200
`, ticketID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivities(rows)
}

func (s *Store) ListBoardActivity(ctx context.Context, sc scope, boardID string) ([]ActivityView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, organization_id::text, tenant_id::text, board_id::text, ticket_id::text, actor_id::text, action, payload, created_at
FROM kan.activity_events
WHERE board_id = $1::uuid AND organization_id = $2::uuid AND tenant_id = $3::uuid
ORDER BY created_at DESC
LIMIT 200
`, boardID, sc.OrgID, sc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivities(rows)
}

func scanActivities(rows *sql.Rows) ([]ActivityView, error) {
	out := make([]ActivityView, 0)
	for rows.Next() {
		var v ActivityView
		var boardID, ticketID, actorID sql.NullString
		var payload []byte
		if err := rows.Scan(&v.ID, &v.OrganizationID, &v.TenantID, &boardID, &ticketID, &actorID, &v.Action, &payload, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.BoardID = ptrStr(boardID)
		v.TicketID = ptrStr(ticketID)
		v.ActorID = ptrStr(actorID)
		v.Payload = rawJSON(payload)
		out = append(out, v)
	}
	return out, rows.Err()
}

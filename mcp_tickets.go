package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CW-3: ticket patch, delete, and the filters REST already supports.

func (a *app) addTicketTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_ticket",
		Description: "Edit a ticket by UUID id or human key. Only provided fields change.",
	}, a.mcpUpdateTicket)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_ticket",
		Description: "Soft-delete a ticket by UUID id or human key. The ticket number is never reused.",
	}, a.mcpDeleteTicket)
}

// scopeAndTicket resolves the caller scope and a ticket UUID in one step, since
// nearly every ticket-scoped tool needs both and accepts id-or-key.
func (a *app) scopeAndTicket(ctx context.Context, req *mcp.CallToolRequest, id, key string) (scope, string, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return scope{}, "", err
	}
	tid, err := a.resolveTicketID(ctx, sc, id, key)
	if err != nil {
		return scope{}, "", err
	}
	return sc, tid, nil
}

// resolveTicketID accepts either a UUID or a human key (CW-1) and returns the UUID.
func (a *app) resolveTicketID(ctx context.Context, sc scope, id, key string) (string, error) {
	if isUUID(id) {
		return id, nil
	}
	if strings.TrimSpace(key) != "" {
		t, err := a.store.GetTicketByKey(ctx, sc, key)
		if err != nil {
			return "", err
		}
		return t.ID, nil
	}
	return "", fmt.Errorf("provide id or key")
}

type mcpUpdateTicketIn struct {
	ID              string  `json:"id,omitempty" jsonschema:"Ticket UUID; provide id or key"`
	Key             string  `json:"key,omitempty" jsonschema:"Human key like CW-1; provide id or key"`
	Title           *string `json:"title,omitempty"`
	Body            *string `json:"body,omitempty"`
	TicketType      *string `json:"ticketType,omitempty" jsonschema:"ticket or defect"`
	Priority        *int    `json:"priority,omitempty" jsonschema:"Any integer; higher is more urgent"`
	Position        *int    `json:"position,omitempty" jsonschema:"Order within its swimlane"`
	EstimateMinutes *int    `json:"estimateMinutes,omitempty"`
	ClearEstimate   bool    `json:"clearEstimate,omitempty"`
	DueAt           string  `json:"dueAt,omitempty" jsonschema:"RFC3339 timestamp"`
	ClearDue        bool    `json:"clearDueAt,omitempty"`
	ParentTicketID  *string `json:"parentTicketId,omitempty"`
	ClearParent     bool    `json:"clearParentTicketId,omitempty"`
	CardData        any     `json:"cardData,omitempty" jsonschema:"Arbitrary JSON object for Cardwallah card fields"`
}

func (a *app) mcpUpdateTicket(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateTicketIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	id, err := a.resolveTicketID(ctx, sc, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}

	var dueAt *time.Time
	if strings.TrimSpace(in.DueAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(in.DueAt))
		if err != nil {
			return nil, nil, fmt.Errorf("dueAt must be RFC3339: %w", err)
		}
		dueAt = &t
	}

	var cardData []byte
	if in.CardData != nil {
		b, err := json.Marshal(in.CardData)
		if err != nil {
			return nil, nil, fmt.Errorf("cardData must be JSON-encodable: %w", err)
		}
		cardData = b
	}

	out, err := a.store.UpdateTicket(ctx, sc, id,
		in.Title, in.Body, in.TicketType,
		in.Priority, in.Position, in.EstimateMinutes, in.ClearEstimate,
		dueAt, in.ClearDue,
		in.ParentTicketID, in.ClearParent,
		cardData)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpDeleteTicketIn struct {
	ID  string `json:"id,omitempty" jsonschema:"Ticket UUID; provide id or key"`
	Key string `json:"key,omitempty" jsonschema:"Human key like CW-1; provide id or key"`
}

func (a *app) mcpDeleteTicket(ctx context.Context, req *mcp.CallToolRequest, in mcpDeleteTicketIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	id, err := a.resolveTicketID(ctx, sc, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if err := a.store.SoftDeleteTicket(ctx, sc, id); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": id})
}

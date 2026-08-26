package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func claimsToScope(c TokenClaims) scope {
	return scope{
		OrgID:    c.OrganizationID,
		TenantID: c.TenantID,
		ActorID:  c.OwnerUserID(),
	}
}

func (a *app) newMCPServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "byz-kan", Version: "1.0.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_boards",
		Description: "List Kanban boards in the caller's org and tenant.",
	}, a.mcpListBoards)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_board",
		Description: "Create a Kanban board. Seeds Backlog, In Progress, Testing, Completed. Optional keyPrefix like SHIP.",
	}, a.mcpCreateBoard)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_tickets",
		Description: "List tickets. Optionally filter by boardId and search q (title/body/key).",
	}, a.mcpListTickets)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_ticket",
		Description: "Create a ticket on a board. Lands in the default Backlog state unless stateId is set. Returns key like SHIP-1.",
	}, a.mcpCreateTicket)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_ticket",
		Description: "Get a ticket by UUID id or human key (e.g. SHIP-1). Provide one of id or key.",
	}, a.mcpGetTicket)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "move_ticket",
		Description: "Move a ticket to another state. Moving to a state named Completed sets completedAt.",
	}, a.mcpMoveTicket)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "log_time",
		Description: "Log time on a ticket in minutes (or start/end timestamps). Updates loggedMinutes.",
	}, a.mcpLogTime)

	return s
}

func (a *app) mcpHTTPHandler() http.Handler {
	srv := a.newMCPServer()
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		JSONResponse:               true,
		DisableLocalhostProtection: true,
	})
}

func (a *app) scopeFromMCP(ctx context.Context, req *mcp.CallToolRequest) (scope, error) {
	if c, ok := claimsFrom(ctx); ok {
		return claimsToScope(c), nil
	}
	if req != nil && req.Extra != nil && req.Extra.Header != nil {
		h := req.Extra.Header
		org := strings.TrimSpace(h.Get("X-Test-Org"))
		if org != "" {
			tc := TokenClaims{
				OrganizationID: org,
				TenantID:       strings.TrimSpace(h.Get("X-Test-Tenant")),
				UserID:         strings.TrimSpace(h.Get("X-Test-User")),
				Subject:        strings.TrimSpace(h.Get("X-Test-User")),
			}
			if _, _, _, ok := rejectScope(tc); ok {
				return claimsToScope(tc), nil
			}
		}
	}
	return scope{}, fmt.Errorf("unauthorized: missing organization_id or tenant_id")
}

func mcpJSON(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, v, nil
}

type mcpListBoardsIn struct {
	Published *bool `json:"published,omitempty" jsonschema:"If set, filter by isPublished"`
}

func (a *app) mcpListBoards(ctx context.Context, req *mcp.CallToolRequest, in mcpListBoardsIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListBoards(ctx, sc, in.Published)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpCreateBoardIn struct {
	Name        string `json:"name" jsonschema:"Board name"`
	Description string `json:"description,omitempty"`
	KeyPrefix   string `json:"keyPrefix,omitempty" jsonschema:"Optional PREFIX for ticket keys, 2-8 A-Z0-9"`
}

func (a *app) mcpCreateBoard(ctx context.Context, req *mcp.CallToolRequest, in mcpCreateBoardIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	out, err := a.store.CreateBoard(ctx, sc.OrgID, sc.TenantID, sc.ActorID, strings.TrimSpace(in.Name), in.Description, in.KeyPrefix, false, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpListTicketsIn struct {
	BoardID string `json:"boardId,omitempty" jsonschema:"Optional board UUID"`
	Q       string `json:"q,omitempty" jsonschema:"Search title, body, or key"`
}

func (a *app) mcpListTickets(ctx context.Context, req *mcp.CallToolRequest, in mcpListTicketsIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListTickets(ctx, sc, ListTicketsParams{BoardID: in.BoardID, Q: in.Q})
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpCreateTicketIn struct {
	BoardID  string `json:"boardId" jsonschema:"Board UUID"`
	Title    string `json:"title" jsonschema:"Ticket title"`
	Body     string `json:"body,omitempty"`
	StateID  string `json:"stateId,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

func (a *app) mcpCreateTicket(ctx context.Context, req *mcp.CallToolRequest, in mcpCreateTicketIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.BoardID) || strings.TrimSpace(in.Title) == "" {
		return nil, nil, fmt.Errorf("boardId and title are required")
	}
	out, err := a.store.CreateTicket(ctx, sc, in.BoardID, in.StateID, "", strings.TrimSpace(in.Title), in.Body, "ticket", in.Priority, 0, nil, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpGetTicketIn struct {
	ID  string `json:"id,omitempty" jsonschema:"Ticket UUID"`
	Key string `json:"key,omitempty" jsonschema:"Human key like SHIP-1"`
}

func (a *app) mcpGetTicket(ctx context.Context, req *mcp.CallToolRequest, in mcpGetTicketIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	var out TicketView
	switch {
	case isUUID(in.ID):
		out, err = a.store.GetTicketByID(ctx, sc, in.ID)
	case strings.TrimSpace(in.Key) != "":
		out, err = a.store.GetTicketByKey(ctx, sc, in.Key)
	default:
		return nil, nil, fmt.Errorf("provide id or key")
	}
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpMoveTicketIn struct {
	ID      string `json:"id" jsonschema:"Ticket UUID"`
	StateID string `json:"stateId" jsonschema:"Target state UUID"`
}

func (a *app) mcpMoveTicket(ctx context.Context, req *mcp.CallToolRequest, in mcpMoveTicketIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) || !isUUID(in.StateID) {
		return nil, nil, fmt.Errorf("id and stateId are required UUIDs")
	}
	out, err := a.store.MoveTicket(ctx, sc, in.ID, in.StateID, nil)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpLogTimeIn struct {
	TicketID string `json:"ticketId" jsonschema:"Ticket UUID"`
	Minutes  int    `json:"minutes,omitempty" jsonschema:"Duration in minutes"`
	Note     string `json:"note,omitempty"`
}

func (a *app) mcpLogTime(ctx context.Context, req *mcp.CallToolRequest, in mcpLogTimeIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.TicketID) || in.Minutes <= 0 {
		return nil, nil, fmt.Errorf("ticketId and minutes (>0) are required")
	}
	out, err := a.store.CreateTimeEntry(ctx, sc, in.TicketID, sc.ActorID, nil, nil, in.Minutes, in.Note)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

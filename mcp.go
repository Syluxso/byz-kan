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
		Name:        "list_states",
		Description: "List a board's states (swimlanes) in order, e.g. Backlog, In Progress, Testing, Completed. Use this to resolve a state name to the UUID move_ticket needs.",
	}, a.mcpListStates)

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

	a.addBoardTools(s)      // CW-1, CW-11
	a.addStateTools(s)      // CW-2
	a.addTicketTools(s)     // CW-3
	a.addCollabTools(s)     // CW-4, CW-5, CW-6, CW-7
	a.addAttachmentTools(s) // CW-8, CW-9, CW-10

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

type mcpListStatesIn struct {
	BoardID string `json:"boardId" jsonschema:"Board UUID"`
}

func (a *app) mcpListStates(ctx context.Context, req *mcp.CallToolRequest, in mcpListStatesIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.BoardID) {
		return nil, nil, fmt.Errorf("boardId is required")
	}
	out, err := a.store.ListStates(ctx, sc, in.BoardID)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpListTicketsIn struct {
	BoardID  string `json:"boardId,omitempty" jsonschema:"Optional board UUID"`
	StateID  string `json:"stateId,omitempty" jsonschema:"Only tickets in this swimlane; resolve names with list_states"`
	Assignee string `json:"assignee,omitempty" jsonschema:"Only tickets assigned to this user UUID"`
	TagID    string `json:"tagId,omitempty" jsonschema:"Only tickets carrying this tag UUID"`
	Tag      string `json:"tag,omitempty" jsonschema:"Only tickets carrying this tag by name, e.g. mcp or #mcp. Combine with boardId to work one feature slice of a board."`
	Q        string `json:"q,omitempty" jsonschema:"Search title, body, or key"`
}

func (a *app) mcpListTickets(ctx context.Context, req *mcp.CallToolRequest, in mcpListTicketsIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListTickets(ctx, sc, ListTicketsParams{
		BoardID:    in.BoardID,
		StateID:    in.StateID,
		AssigneeID: in.Assignee,
		TagID:      in.TagID,
		TagName:    in.Tag,
		Q:          in.Q,
	})
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
	TicketID  string `json:"ticketId,omitempty" jsonschema:"Ticket UUID; provide ticketId or key"`
	Key       string `json:"key,omitempty" jsonschema:"Human key like CW-1; provide ticketId or key"`
	Minutes   int    `json:"minutes,omitempty" jsonschema:"Duration in minutes; omit if passing startedAt and endedAt"`
	StartedAt string `json:"startedAt,omitempty" jsonschema:"RFC3339 timestamp; with endedAt the duration is computed"`
	EndedAt   string `json:"endedAt,omitempty" jsonschema:"RFC3339 timestamp"`
	Note      string `json:"note,omitempty"`
}

func (a *app) mcpLogTime(ctx context.Context, req *mcp.CallToolRequest, in mcpLogTimeIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.TicketID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	started, err := parseOptionalRFC3339(in.StartedAt, "startedAt")
	if err != nil {
		return nil, nil, err
	}
	ended, err := parseOptionalRFC3339(in.EndedAt, "endedAt")
	if err != nil {
		return nil, nil, err
	}
	// CW-10: accept either an explicit duration or a start/end pair.
	if in.Minutes <= 0 && (started == nil || ended == nil) {
		return nil, nil, fmt.Errorf("provide minutes (>0), or both startedAt and endedAt")
	}
	out, err := a.store.CreateTimeEntry(ctx, sc, id, sc.ActorID, started, ended, in.Minutes, in.Note)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

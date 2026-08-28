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
		Name: "create_ticket",
		Description: "Create a ticket on a board. Lands in the default Backlog state unless stateId is set. Returns key like SHIP-1.\n\n" +
			"Title is the heading, body is evidence (logs, snippets), and cardData holds the shaped blocks the card renders: " +
			"story{asA,iWant,soThat}, acceptance[], scenarios[{name,given,when,then}], uat[], defect{repro,expected,actual}, " +
			"spike{question,timeboxMinutes,approach,findings,outcome,followUp}, chore{why,doneWhen}.\n\n" +
			"ticketType is story|defect|spike|chore. UAT and scenarios are sections, not types, so a defect can carry a UAT too. " +
			"If the user described the work only in passing, still create it in one call: the empty blocks for the type are seeded " +
			"for you. Send whatever the user actually gave you in cardData on the same call rather than creating a stub and patching it.\n\n" +
			"The result reports shaped (what is filled), omitted (what is worth adding next), and a hint. Use those to decide whether " +
			"to ask the user for the missing pieces or to fill them yourself with update_ticket.",
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
	a.addMessageTools(s)    // CW-18

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
	BoardID string `json:"boardId" jsonschema:"Board UUID"`
	Title   string `json:"title" jsonschema:"The heading. One line naming the work."`
	Body    string `json:"body,omitempty" jsonschema:"Evidence: logs, snippets, stack traces, history. Shaped fields belong in cardData, not here."`
	StateID string `json:"stateId,omitempty"`
	// Deliberately a pointer: absent must mean "seed", and a plain bool cannot
	// tell an omitted field from an explicit false.
	SeedShapes *bool  `json:"seedShapes,omitempty" jsonschema:"Default true. Seeds the empty shaped blocks for this type. Set false to store only what you send."`
	TicketType string `json:"ticketType,omitempty" jsonschema:"story (default) | defect | spike | chore. UAT and scenarios are NOT types - they are sections you may put in cardData on any type."`
	CardData   any    `json:"cardData,omitempty" jsonschema:"Shaped blocks. story{asA,iWant,soThat}; acceptance[]; scenarios[{name,given,when,then}]; uat[]; defect{repro,expected,actual}; spike{question,timeboxMinutes,approach,findings,outcome,followUp}; chore{why,doneWhen}. Any section may go on any type; unknown keys are stored untouched."`
	Priority   int    `json:"priority,omitempty"`
}

func (a *app) mcpCreateTicket(ctx context.Context, req *mcp.CallToolRequest, in mcpCreateTicketIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.BoardID) || strings.TrimSpace(in.Title) == "" {
		return nil, nil, fmt.Errorf("boardId and title are required")
	}

	ticketType, okType := normalizeTicketType(in.TicketType)
	if !okType {
		return nil, nil, fmt.Errorf("ticketType must be one of story, defect, spike, chore")
	}

	// cardData arrives as arbitrary JSON. Anything that is not an object has no
	// shape to merge into, so refuse it rather than dropping it silently.
	provided := map[string]any{}
	if in.CardData != nil {
		b, err := json.Marshal(in.CardData)
		if err != nil {
			return nil, nil, fmt.Errorf("cardData must be JSON-encodable: %w", err)
		}
		if err := json.Unmarshal(b, &provided); err != nil {
			return nil, nil, fmt.Errorf("cardData must be a JSON object")
		}
	}

	seed := in.SeedShapes == nil || *in.SeedShapes
	merged, shaped, omitted := shapeCardData(ticketType, strings.TrimSpace(in.Title), provided, seed)

	cardData, err := json.Marshal(merged)
	if err != nil {
		return nil, nil, fmt.Errorf("cardData must be JSON-encodable: %w", err)
	}

	out, err := a.store.CreateTicket(ctx, sc, in.BoardID, in.StateID, "",
		strings.TrimSpace(in.Title), in.Body, ticketType, in.Priority, 0, nil, nil, cardData)
	if err != nil {
		return nil, nil, err
	}

	env, err := ticketWithShapeEnvelope(out, shaped, omitted)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(env)
}

// ticketWithShapeEnvelope returns the ticket's own fields plus the
// shaped/omitted/hint envelope from CW-32, so an agent is told what it just
// wrote and what it could still add without making a second call to find out.
//
// The ticket stays flattened at the top level rather than nested under a
// "ticket" key: callers already read .key and .id straight off this result, and
// there is no reason to break them to add three fields.
func ticketWithShapeEnvelope(t TicketView, shaped, omitted []string) (map[string]any, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	env := map[string]any{}
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, err
	}
	env["shaped"] = shaped
	env["omitted"] = omitted
	env["hint"] = shapeHint(shaped, omitted)
	return env, nil
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

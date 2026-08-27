package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CW-1: board get/patch/delete, publish, members.
// CW-11: board and ticket activity feeds.

func (a *app) addBoardTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_board",
		Description: "Get one board by UUID, including its states.",
	}, a.mcpGetBoard)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_board",
		Description: "Rename or reconfigure a board. Only provided fields change. keyPrefix can only change while the board has no tickets.",
	}, a.mcpUpdateBoard)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_board",
		Description: "Soft-delete a board and cascade to its states and tickets.",
	}, a.mcpDeleteBoard)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_board_members",
		Description: "List members of a board.",
	}, a.mcpListBoardMembers)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_board_member",
		Description: "Add a user to a board by IAM user UUID. Role defaults to member.",
	}, a.mcpAddBoardMember)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_board_member",
		Description: "Remove a user from a board.",
	}, a.mcpRemoveBoardMember)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_board_activity",
		Description: "Board activity history, newest first.",
	}, a.mcpListBoardActivity)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_ticket_activity",
		Description: "Ticket activity history by UUID id or human key, newest first.",
	}, a.mcpListTicketActivity)
}

type mcpBoardIDIn struct {
	ID string `json:"id" jsonschema:"Board UUID"`
}

func (a *app) mcpGetBoard(ctx context.Context, req *mcp.CallToolRequest, in mcpBoardIDIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	out, err := a.store.GetBoard(ctx, sc, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpUpdateBoardIn struct {
	ID          string  `json:"id" jsonschema:"Board UUID"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	KeyPrefix   *string `json:"keyPrefix,omitempty" jsonschema:"2-8 A-Z0-9; only allowed while the board has no tickets"`
	IsPublished *bool   `json:"isPublished,omitempty"`
	Settings    any     `json:"settings,omitempty" jsonschema:"Arbitrary JSON object"`
	CardSchema  any     `json:"cardSchema,omitempty" jsonschema:"Arbitrary JSON object"`
}

func (a *app) mcpUpdateBoard(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateBoardIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	var settings, cardSchema []byte
	if in.Settings != nil {
		b, err := json.Marshal(in.Settings)
		if err != nil {
			return nil, nil, fmt.Errorf("settings must be JSON-encodable: %w", err)
		}
		settings = b
	}
	if in.CardSchema != nil {
		b, err := json.Marshal(in.CardSchema)
		if err != nil {
			return nil, nil, fmt.Errorf("cardSchema must be JSON-encodable: %w", err)
		}
		cardSchema = b
	}
	out, err := a.store.UpdateBoard(ctx, sc, in.ID, in.Name, in.Description, in.IsPublished, cardSchema, settings, in.KeyPrefix)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpDeleteBoard(ctx context.Context, req *mcp.CallToolRequest, in mcpBoardIDIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	if err := a.store.SoftDeleteBoard(ctx, sc, in.ID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": in.ID})
}

type mcpBoardRefIn struct {
	BoardID string `json:"boardId" jsonschema:"Board UUID"`
}

func (a *app) mcpListBoardMembers(ctx context.Context, req *mcp.CallToolRequest, in mcpBoardRefIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.BoardID) {
		return nil, nil, fmt.Errorf("boardId is required")
	}
	out, err := a.store.ListMembers(ctx, sc, in.BoardID)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpBoardMemberIn struct {
	BoardID string `json:"boardId" jsonschema:"Board UUID"`
	UserID  string `json:"userId" jsonschema:"IAM user UUID"`
	Role    string `json:"role,omitempty" jsonschema:"Defaults to member"`
}

func (a *app) mcpAddBoardMember(ctx context.Context, req *mcp.CallToolRequest, in mcpBoardMemberIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.BoardID) || !isUUID(in.UserID) {
		return nil, nil, fmt.Errorf("boardId and userId must be UUIDs")
	}
	role := strings.TrimSpace(in.Role)
	if role == "" {
		role = "member"
	}
	out, err := a.store.AddMember(ctx, sc, in.BoardID, in.UserID, role)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpRemoveBoardMember(ctx context.Context, req *mcp.CallToolRequest, in mcpBoardMemberIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.BoardID) || !isUUID(in.UserID) {
		return nil, nil, fmt.Errorf("boardId and userId must be UUIDs")
	}
	if err := a.store.RemoveMember(ctx, sc, in.BoardID, in.UserID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"removed": true, "boardId": in.BoardID, "userId": in.UserID})
}

func (a *app) mcpListBoardActivity(ctx context.Context, req *mcp.CallToolRequest, in mcpBoardRefIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.BoardID) {
		return nil, nil, fmt.Errorf("boardId is required")
	}
	out, err := a.store.ListBoardActivity(ctx, sc, in.BoardID)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpTicketRefIn struct {
	ID  string `json:"id,omitempty" jsonschema:"Ticket UUID; provide id or key"`
	Key string `json:"key,omitempty" jsonschema:"Human key like CW-1; provide id or key"`
}

func (a *app) mcpListTicketActivity(ctx context.Context, req *mcp.CallToolRequest, in mcpTicketRefIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	id, err := a.resolveTicketID(ctx, sc, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListTicketActivity(ctx, sc, id)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

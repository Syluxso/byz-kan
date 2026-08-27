package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CW-2: swimlane write operations. list_states lives in mcp.go (CW-12).

func (a *app) addStateTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_state",
		Description: "Create a swimlane on a board. Set isDefault to make new tickets land here.",
	}, a.mcpCreateState)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_state",
		Description: "Rename or reconfigure a swimlane. Only provided fields change.",
	}, a.mcpUpdateState)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_state",
		Description: "Soft-delete a swimlane. Fails if it still holds tickets unless force is true, which moves them to the board's default state.",
	}, a.mcpDeleteState)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "reorder_states",
		Description: "Set swimlane order on a board. Pass every state id in the desired left-to-right order.",
	}, a.mcpReorderStates)
}

type mcpCreateStateIn struct {
	BoardID   string `json:"boardId" jsonschema:"Board UUID"`
	Name      string `json:"name" jsonschema:"Swimlane name, e.g. In Review"`
	Position  int    `json:"position,omitempty" jsonschema:"Order on the board, lower is further left"`
	IsDefault bool   `json:"isDefault,omitempty" jsonschema:"Make this the state new tickets land in"`
	Color     string `json:"color,omitempty"`
	WIPLimit  *int   `json:"wipLimit,omitempty" jsonschema:"Optional work-in-progress limit"`
}

func (a *app) mcpCreateState(ctx context.Context, req *mcp.CallToolRequest, in mcpCreateStateIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.BoardID) || strings.TrimSpace(in.Name) == "" {
		return nil, nil, fmt.Errorf("boardId and name are required")
	}
	out, err := a.store.CreateState(ctx, sc, in.BoardID, strings.TrimSpace(in.Name), in.Color, in.Position, in.IsDefault, in.WIPLimit)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpUpdateStateIn struct {
	ID        string  `json:"id" jsonschema:"State UUID"`
	Name      *string `json:"name,omitempty"`
	Position  *int    `json:"position,omitempty"`
	IsDefault *bool   `json:"isDefault,omitempty"`
	WIPLimit  *int    `json:"wipLimit,omitempty"`
	ClearWIP  bool    `json:"clearWipLimit,omitempty" jsonschema:"Remove the WIP limit entirely"`
	Color     *string `json:"color,omitempty"`
}

func (a *app) mcpUpdateState(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateStateIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	out, err := a.store.UpdateState(ctx, sc, in.ID, in.Name, in.Position, in.IsDefault, in.WIPLimit, in.ClearWIP, in.Color)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpDeleteStateIn struct {
	ID    string `json:"id" jsonschema:"State UUID"`
	Force bool   `json:"force,omitempty" jsonschema:"Move any tickets in this state to the board default instead of failing"`
}

func (a *app) mcpDeleteState(ctx context.Context, req *mcp.CallToolRequest, in mcpDeleteStateIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	if err := a.store.SoftDeleteState(ctx, sc, in.ID, in.Force); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": in.ID})
}

type mcpReorderStatesIn struct {
	BoardID  string   `json:"boardId" jsonschema:"Board UUID"`
	StateIDs []string `json:"stateIds" jsonschema:"Every state UUID on the board, in the desired order"`
}

func (a *app) mcpReorderStates(ctx context.Context, req *mcp.CallToolRequest, in mcpReorderStatesIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.BoardID) || len(in.StateIDs) == 0 {
		return nil, nil, fmt.Errorf("boardId and a non-empty stateIds list are required")
	}
	if err := a.store.ReorderStates(ctx, sc, in.BoardID, in.StateIDs); err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListStates(ctx, sc, in.BoardID)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

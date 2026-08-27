package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CW-18: MCP surface for the shared agent/human thread.

func (a *app) addMessageTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_messages",
		Description: "Read the shared agent thread. Give ticketId for one ticket's thread, " +
			"or boardId for the board-level thread. This is where agents leave context for " +
			"each other; ticket comments are for product discussion.",
	}, a.mcpListMessages)

	mcp.AddTool(s, &mcp.Tool{
		Name: "add_message",
		Description: "Post to the shared agent thread, on a ticket or on the board. " +
			"Set actorKey to something stable for you so two agents do not collide, " +
			"and displayName to how you should appear, e.g. Claude or Grok Web.",
	}, a.mcpAddMessage)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_message",
		Description: "Edit a message by its UUID.",
	}, a.mcpUpdateMessage)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_message",
		Description: "Soft-delete a message by its UUID.",
	}, a.mcpDeleteMessage)
}

type mcpListMessagesIn struct {
	BoardID  string `json:"boardId,omitempty" jsonschema:"Board UUID; returns the board-level thread"`
	TicketID string `json:"ticketId,omitempty" jsonschema:"Ticket UUID; returns that ticket's thread"`
	Key      string `json:"key,omitempty" jsonschema:"Ticket key like CW-18, instead of ticketId"`
	All      bool   `json:"all,omitempty" jsonschema:"With boardId, also include ticket-scoped messages"`
}

func (a *app) mcpListMessages(ctx context.Context, req *mcp.CallToolRequest, in mcpListMessagesIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	p := ListMessagesParams{BoardID: in.BoardID, BoardAll: in.All}
	if in.TicketID != "" || strings.TrimSpace(in.Key) != "" {
		id, err := a.resolveTicketID(ctx, sc, in.TicketID, in.Key)
		if err != nil {
			return nil, nil, err
		}
		p.TicketID = id
	}
	if p.TicketID == "" && !isUUID(p.BoardID) {
		return nil, nil, fmt.Errorf("provide boardId, ticketId or key")
	}
	out, err := a.store.ListMessages(ctx, sc, p)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpAddMessageIn struct {
	BoardID     string `json:"boardId,omitempty" jsonschema:"Board UUID for a board-level message"`
	TicketID    string `json:"ticketId,omitempty" jsonschema:"Ticket UUID to post on a ticket thread"`
	Key         string `json:"key,omitempty" jsonschema:"Ticket key like CW-18, instead of ticketId"`
	ActorType   string `json:"actorType,omitempty" jsonschema:"user or agent; defaults to agent"`
	ActorKey    string `json:"actorKey" jsonschema:"Stable identifier for you, e.g. claude-code or grok-web"`
	DisplayName string `json:"displayName" jsonschema:"Name to show on the message, e.g. Claude"`
	Body        string `json:"body" jsonschema:"Message text"`
}

func (a *app) mcpAddMessage(ctx context.Context, req *mcp.CallToolRequest, in mcpAddMessageIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	ticketID := ""
	if in.TicketID != "" || strings.TrimSpace(in.Key) != "" {
		ticketID, err = a.resolveTicketID(ctx, sc, in.TicketID, in.Key)
		if err != nil {
			return nil, nil, err
		}
	}
	if ticketID == "" && !isUUID(in.BoardID) {
		return nil, nil, fmt.Errorf("provide boardId, ticketId or key")
	}
	out, err := a.store.CreateMessage(ctx, sc, in.BoardID, ticketID,
		in.ActorType, in.ActorKey, in.DisplayName, in.Body)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpUpdateMessageIn struct {
	ID   string `json:"id" jsonschema:"Message UUID"`
	Body string `json:"body" jsonschema:"New message text"`
}

func (a *app) mcpUpdateMessage(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateMessageIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	out, err := a.store.UpdateMessage(ctx, sc, in.ID, in.Body)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpDeleteMessage(ctx context.Context, req *mcp.CallToolRequest, in mcpIDIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	if err := a.store.SoftDeleteMessage(ctx, sc, in.ID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": in.ID})
}

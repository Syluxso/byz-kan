package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CW-4 assignees/watchers, CW-5 tags, CW-6 comments, CW-7 links.

func (a *app) addCollabTools(s *mcp.Server) {
	// CW-6 comments
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_comments",
		Description: "List comments on a ticket, by UUID id or human key.",
	}, a.mcpListComments)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_comment",
		Description: "Add a comment to a ticket. Use this to leave notes other agents and humans will see on the card.",
	}, a.mcpAddComment)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_comment",
		Description: "Edit a comment by its UUID.",
	}, a.mcpUpdateComment)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_comment",
		Description: "Soft-delete a comment by its UUID.",
	}, a.mcpDeleteComment)

	// CW-5 tags
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_tags",
		Description: "List tags in the caller's tenant. Optionally filter by kind.",
	}, a.mcpListTags)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_tag",
		Description: "Create a tag. kind can group tags, e.g. project.",
	}, a.mcpCreateTag)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_tag",
		Description: "Rename or recolor a tag.",
	}, a.mcpUpdateTag)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_tag",
		Description: "Soft-delete a tag.",
	}, a.mcpDeleteTag)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_ticket_tags",
		Description: "List tags attached to a ticket.",
	}, a.mcpListTicketTags)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_ticket_tag",
		Description: "Attach an existing tag to a ticket.",
	}, a.mcpAddTicketTag)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_ticket_tag",
		Description: "Detach a tag from a ticket.",
	}, a.mcpRemoveTicketTag)

	// CW-7 links
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_links",
		Description: "List remote URL links on a ticket.",
	}, a.mcpListLinks)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_link",
		Description: "Attach a remote URL to a ticket. For uploaded files use attachments instead.",
	}, a.mcpAddLink)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_link",
		Description: "Edit a link by its UUID.",
	}, a.mcpUpdateLink)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_link",
		Description: "Soft-delete a link by its UUID.",
	}, a.mcpDeleteLink)

	// CW-4 assignees and watchers
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_assignees",
		Description: "List users assigned to a ticket.",
	}, a.mcpListAssignees)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_assignee",
		Description: "Assign an IAM user to a ticket.",
	}, a.mcpAddAssignee)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_assignee",
		Description: "Unassign a user from a ticket.",
	}, a.mcpRemoveAssignee)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_assignees",
		Description: "Replace the whole assignee list on a ticket. Pass an empty list to clear it.",
	}, a.mcpSetAssignees)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_watchers",
		Description: "List users watching a ticket.",
	}, a.mcpListWatchers)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_watcher",
		Description: "Add a watcher to a ticket.",
	}, a.mcpAddWatcher)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_watcher",
		Description: "Remove a watcher from a ticket.",
	}, a.mcpRemoveWatcher)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_watchers",
		Description: "Replace the whole watcher list on a ticket. Pass an empty list to clear it.",
	}, a.mcpSetWatchers)
}

// ---- comments (CW-6) ----

type mcpAddCommentIn struct {
	ID   string `json:"id,omitempty" jsonschema:"Ticket UUID; provide id or key"`
	Key  string `json:"key,omitempty" jsonschema:"Human key like CW-1; provide id or key"`
	Body string `json:"body" jsonschema:"Comment text"`
}

func (a *app) mcpListComments(ctx context.Context, req *mcp.CallToolRequest, in mcpTicketRefIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListComments(ctx, sc, id)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpAddComment(ctx context.Context, req *mcp.CallToolRequest, in mcpAddCommentIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Body) == "" {
		return nil, nil, fmt.Errorf("body is required")
	}
	out, err := a.store.CreateComment(ctx, sc, id, in.Body)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpUpdateCommentIn struct {
	ID   string `json:"id" jsonschema:"Comment UUID"`
	Body string `json:"body" jsonschema:"New comment text"`
}

func (a *app) mcpUpdateComment(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateCommentIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) || strings.TrimSpace(in.Body) == "" {
		return nil, nil, fmt.Errorf("id and body are required")
	}
	out, err := a.store.UpdateComment(ctx, sc, in.ID, in.Body)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpIDIn struct {
	ID string `json:"id" jsonschema:"UUID"`
}

func (a *app) mcpDeleteComment(ctx context.Context, req *mcp.CallToolRequest, in mcpIDIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	if err := a.store.SoftDeleteComment(ctx, sc, in.ID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": in.ID})
}

// ---- tags (CW-5) ----

type mcpListTagsIn struct {
	Kind string `json:"kind,omitempty" jsonschema:"Optional kind filter, e.g. project"`
}

func (a *app) mcpListTags(ctx context.Context, req *mcp.CallToolRequest, in mcpListTagsIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListTags(ctx, sc, in.Kind)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpCreateTagIn struct {
	Name  string `json:"name" jsonschema:"Tag name"`
	Kind  string `json:"kind,omitempty" jsonschema:"Optional grouping, e.g. project"`
	Color string `json:"color,omitempty"`
}

func (a *app) mcpCreateTag(ctx context.Context, req *mcp.CallToolRequest, in mcpCreateTagIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	out, err := a.store.CreateTag(ctx, sc, strings.TrimSpace(in.Name), in.Kind, in.Color)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpUpdateTagIn struct {
	ID    string  `json:"id" jsonschema:"Tag UUID"`
	Name  *string `json:"name,omitempty"`
	Kind  *string `json:"kind,omitempty"`
	Color *string `json:"color,omitempty"`
}

func (a *app) mcpUpdateTag(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateTagIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	out, err := a.store.UpdateTag(ctx, sc, in.ID, in.Name, in.Kind, in.Color)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpDeleteTag(ctx context.Context, req *mcp.CallToolRequest, in mcpIDIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	if err := a.store.SoftDeleteTag(ctx, sc, in.ID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": in.ID})
}

type mcpTicketTagIn struct {
	ID    string `json:"id,omitempty" jsonschema:"Ticket UUID; provide id or key"`
	Key   string `json:"key,omitempty" jsonschema:"Human key like CW-1; provide id or key"`
	TagID string `json:"tagId" jsonschema:"Tag UUID"`
}

func (a *app) mcpListTicketTags(ctx context.Context, req *mcp.CallToolRequest, in mcpTicketRefIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListTicketTags(ctx, sc, id)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpAddTicketTag(ctx context.Context, req *mcp.CallToolRequest, in mcpTicketTagIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.TagID) {
		return nil, nil, fmt.Errorf("tagId is required")
	}
	if err := a.store.AddTicketTag(ctx, sc, id, in.TagID); err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListTicketTags(ctx, sc, id)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpRemoveTicketTag(ctx context.Context, req *mcp.CallToolRequest, in mcpTicketTagIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.TagID) {
		return nil, nil, fmt.Errorf("tagId is required")
	}
	if err := a.store.RemoveTicketTag(ctx, sc, id, in.TagID); err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListTicketTags(ctx, sc, id)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

// ---- links (CW-7) ----

type mcpAddLinkIn struct {
	ID       string `json:"id,omitempty" jsonschema:"Ticket UUID; provide id or key"`
	Key      string `json:"key,omitempty" jsonschema:"Human key like CW-1; provide id or key"`
	URL      string `json:"url" jsonschema:"Remote URL"`
	Title    string `json:"title,omitempty"`
	LinkType string `json:"linkType,omitempty" jsonschema:"Free-form category, e.g. repo or design"`
}

func (a *app) mcpListLinks(ctx context.Context, req *mcp.CallToolRequest, in mcpTicketRefIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListLinks(ctx, sc, id)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpAddLink(ctx context.Context, req *mcp.CallToolRequest, in mcpAddLinkIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.URL) == "" {
		return nil, nil, fmt.Errorf("url is required")
	}
	out, err := a.store.CreateLink(ctx, sc, id, strings.TrimSpace(in.URL), in.Title, in.LinkType)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpUpdateLinkIn struct {
	ID       string  `json:"id" jsonschema:"Link UUID"`
	URL      *string `json:"url,omitempty"`
	Title    *string `json:"title,omitempty"`
	LinkType *string `json:"linkType,omitempty"`
}

func (a *app) mcpUpdateLink(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateLinkIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	out, err := a.store.UpdateLink(ctx, sc, in.ID, in.URL, in.Title, in.LinkType)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpDeleteLink(ctx context.Context, req *mcp.CallToolRequest, in mcpIDIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	if err := a.store.SoftDeleteLink(ctx, sc, in.ID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": in.ID})
}

// ---- assignees and watchers (CW-4) ----

type mcpPersonIn struct {
	ID     string `json:"id,omitempty" jsonschema:"Ticket UUID; provide id or key"`
	Key    string `json:"key,omitempty" jsonschema:"Human key like CW-1; provide id or key"`
	UserID string `json:"userId" jsonschema:"IAM user UUID"`
}

type mcpPeopleIn struct {
	ID      string   `json:"id,omitempty" jsonschema:"Ticket UUID; provide id or key"`
	Key     string   `json:"key,omitempty" jsonschema:"Human key like CW-1; provide id or key"`
	UserIDs []string `json:"userIds" jsonschema:"Full replacement list of IAM user UUIDs; empty clears"`
}

func (a *app) mcpListAssignees(ctx context.Context, req *mcp.CallToolRequest, in mcpTicketRefIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListAssignees(ctx, sc, id)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpAddAssignee(ctx context.Context, req *mcp.CallToolRequest, in mcpPersonIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.UserID) {
		return nil, nil, fmt.Errorf("userId must be a UUID")
	}
	out, err := a.store.AddAssignee(ctx, sc, id, in.UserID)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpRemoveAssignee(ctx context.Context, req *mcp.CallToolRequest, in mcpPersonIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.UserID) {
		return nil, nil, fmt.Errorf("userId must be a UUID")
	}
	if err := a.store.RemoveAssignee(ctx, sc, id, in.UserID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"removed": true, "ticketId": id, "userId": in.UserID})
}

func (a *app) mcpSetAssignees(ctx context.Context, req *mcp.CallToolRequest, in mcpPeopleIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ReplaceAssignees(ctx, sc, id, in.UserIDs)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpListWatchers(ctx context.Context, req *mcp.CallToolRequest, in mcpTicketRefIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListWatchers(ctx, sc, id)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpAddWatcher(ctx context.Context, req *mcp.CallToolRequest, in mcpPersonIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.UserID) {
		return nil, nil, fmt.Errorf("userId must be a UUID")
	}
	out, err := a.store.AddWatcher(ctx, sc, id, in.UserID)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpRemoveWatcher(ctx context.Context, req *mcp.CallToolRequest, in mcpPersonIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.UserID) {
		return nil, nil, fmt.Errorf("userId must be a UUID")
	}
	if err := a.store.RemoveWatcher(ctx, sc, id, in.UserID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"removed": true, "ticketId": id, "userId": in.UserID})
}

func (a *app) mcpSetWatchers(ctx context.Context, req *mcp.CallToolRequest, in mcpPeopleIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ReplaceWatchers(ctx, sc, id, in.UserIDs)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

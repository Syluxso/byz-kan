package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CW-8 attachments, CW-9 checklists, CW-10 time entries.

func (a *app) addAttachmentTools(s *mcp.Server) {
	// CW-8 attachments
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_attachments",
		Description: "List attachments on a ticket, board or agent message. Attachments reference byz-file-service fileIds; bytes are never stored here.",
	}, a.mcpListAttachments)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_attachment",
		Description: "Attach an existing byz-file-service fileId to a ticket, board or agent message. Upload the bytes to byz-file-service first.",
	}, a.mcpAddAttachment)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_attachment",
		Description: "Soft-delete an attachment by its UUID. The underlying file is untouched.",
	}, a.mcpDeleteAttachment)

	// CW-9 checklists
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_checklists",
		Description: "List checklists on a ticket, including their items.",
	}, a.mcpListChecklists)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_checklist",
		Description: "Create a checklist on a ticket.",
	}, a.mcpCreateChecklist)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_checklist",
		Description: "Rename or reposition a checklist.",
	}, a.mcpUpdateChecklist)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_checklist",
		Description: "Soft-delete a checklist and its items.",
	}, a.mcpDeleteChecklist)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_checklist_item",
		Description: "Add an item to a checklist.",
	}, a.mcpAddChecklistItem)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_checklist_item",
		Description: "Edit a checklist item, including marking it done.",
	}, a.mcpUpdateChecklistItem)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_checklist_item",
		Description: "Soft-delete a checklist item.",
	}, a.mcpDeleteChecklistItem)

	// CW-10 time entries
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_time_entries",
		Description: "List time entries on a ticket.",
	}, a.mcpListTimeEntries)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_time_entry",
		Description: "Edit a time entry. The ticket's loggedMinutes is recomputed.",
	}, a.mcpUpdateTimeEntry)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_time_entry",
		Description: "Soft-delete a time entry. The ticket's loggedMinutes is recomputed.",
	}, a.mcpDeleteTimeEntry)
}

// ---- attachments (CW-8) ----

type mcpAttachmentParentIn struct {
	ParentType string `json:"parentType,omitempty" jsonschema:"ticket, board or message; defaults to ticket"`
	ParentID   string `json:"parentId,omitempty" jsonschema:"UUID of the parent"`
	ID         string `json:"id,omitempty" jsonschema:"Ticket UUID, when the parent is a ticket"`
	Key        string `json:"key,omitempty" jsonschema:"Ticket key like CW-19, when the parent is a ticket"`
}

// resolveParent accepts either the general parentType/parentId form or the
// ticket id/key form the tools used before CW-19, so existing callers keep
// working.
func (a *app) resolveParent(ctx context.Context, sc scope, parentType, parentID, id, key string) (string, string, error) {
	pt := strings.ToLower(strings.TrimSpace(parentType))
	if pt == "" {
		pt = "ticket"
	}
	if !AttachmentParents[pt] {
		return "", "", fmt.Errorf("parentType must be ticket, board or message")
	}
	if isUUID(parentID) {
		return pt, parentID, nil
	}
	if pt != "ticket" {
		return "", "", fmt.Errorf("parentId is required for parentType %s", pt)
	}
	tid, err := a.resolveTicketID(ctx, sc, id, key)
	if err != nil {
		return "", "", err
	}
	return "ticket", tid, nil
}

func (a *app) mcpListAttachments(ctx context.Context, req *mcp.CallToolRequest, in mcpAttachmentParentIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	pt, pid, err := a.resolveParent(ctx, sc, in.ParentType, in.ParentID, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListAttachments(ctx, sc, pt, pid)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpAddAttachmentIn struct {
	ParentType  string `json:"parentType,omitempty" jsonschema:"ticket, board or message; defaults to ticket"`
	ParentID    string `json:"parentId,omitempty" jsonschema:"UUID of the parent"`
	ID          string `json:"id,omitempty" jsonschema:"Ticket UUID, when the parent is a ticket"`
	Key         string `json:"key,omitempty" jsonschema:"Ticket key like CW-19, when the parent is a ticket"`
	FileID      string `json:"fileId" jsonschema:"byz-file-service file UUID; upload the bytes there first"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	SizeBytes   *int64 `json:"sizeBytes,omitempty"`
}

func (a *app) mcpAddAttachment(ctx context.Context, req *mcp.CallToolRequest, in mcpAddAttachmentIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	pt, pid, err := a.resolveParent(ctx, sc, in.ParentType, in.ParentID, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.FileID) == "" {
		return nil, nil, fmt.Errorf("fileId is required")
	}
	out, err := a.store.CreateAttachment(ctx, sc, pt, pid,
		strings.TrimSpace(in.FileID), in.Filename, in.ContentType, in.SizeBytes)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpDeleteAttachment(ctx context.Context, req *mcp.CallToolRequest, in mcpIDIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	if err := a.store.SoftDeleteAttachment(ctx, sc, in.ID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": in.ID})
}

// ---- checklists (CW-9) ----

type mcpCreateChecklistIn struct {
	ID       string `json:"id,omitempty" jsonschema:"Ticket UUID; provide id or key"`
	Key      string `json:"key,omitempty" jsonschema:"Human key like CW-1; provide id or key"`
	Title    string `json:"title" jsonschema:"Checklist title"`
	Position int    `json:"position,omitempty"`
}

func (a *app) mcpListChecklists(ctx context.Context, req *mcp.CallToolRequest, in mcpTicketRefIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListChecklists(ctx, sc, id)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpCreateChecklist(ctx context.Context, req *mcp.CallToolRequest, in mcpCreateChecklistIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, nil, fmt.Errorf("title is required")
	}
	out, err := a.store.CreateChecklist(ctx, sc, id, strings.TrimSpace(in.Title), in.Position)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpUpdateChecklistIn struct {
	ID       string  `json:"id" jsonschema:"Checklist UUID"`
	Title    *string `json:"title,omitempty"`
	Position *int    `json:"position,omitempty"`
}

func (a *app) mcpUpdateChecklist(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateChecklistIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	out, err := a.store.UpdateChecklist(ctx, sc, in.ID, in.Title, in.Position)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpDeleteChecklist(ctx context.Context, req *mcp.CallToolRequest, in mcpIDIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	if err := a.store.SoftDeleteChecklist(ctx, sc, in.ID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": in.ID})
}

type mcpAddChecklistItemIn struct {
	ChecklistID string `json:"checklistId" jsonschema:"Checklist UUID"`
	Title       string `json:"title" jsonschema:"Item text"`
	Position    int    `json:"position,omitempty"`
}

func (a *app) mcpAddChecklistItem(ctx context.Context, req *mcp.CallToolRequest, in mcpAddChecklistItemIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ChecklistID) || strings.TrimSpace(in.Title) == "" {
		return nil, nil, fmt.Errorf("checklistId and title are required")
	}
	out, err := a.store.CreateChecklistItem(ctx, sc, in.ChecklistID, strings.TrimSpace(in.Title), in.Position)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpUpdateChecklistItemIn struct {
	ID       string  `json:"id" jsonschema:"Checklist item UUID"`
	Title    *string `json:"title,omitempty"`
	IsDone   *bool   `json:"isDone,omitempty" jsonschema:"Mark the item done or not done"`
	Position *int    `json:"position,omitempty"`
}

func (a *app) mcpUpdateChecklistItem(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateChecklistItemIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	out, err := a.store.UpdateChecklistItem(ctx, sc, in.ID, in.Title, in.IsDone, in.Position)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpDeleteChecklistItem(ctx context.Context, req *mcp.CallToolRequest, in mcpIDIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	if err := a.store.SoftDeleteChecklistItem(ctx, sc, in.ID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": in.ID})
}

// ---- time entries (CW-10) ----

func (a *app) mcpListTimeEntries(ctx context.Context, req *mcp.CallToolRequest, in mcpTicketRefIn) (*mcp.CallToolResult, any, error) {
	sc, id, err := a.scopeAndTicket(ctx, req, in.ID, in.Key)
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.ListTimeEntries(ctx, sc, id)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

type mcpUpdateTimeEntryIn struct {
	ID        string  `json:"id" jsonschema:"Time entry UUID"`
	Minutes   *int    `json:"minutes,omitempty"`
	StartedAt string  `json:"startedAt,omitempty" jsonschema:"RFC3339 timestamp"`
	EndedAt   string  `json:"endedAt,omitempty" jsonschema:"RFC3339 timestamp"`
	Note      *string `json:"note,omitempty"`
}

func (a *app) mcpUpdateTimeEntry(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateTimeEntryIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	started, err := parseOptionalRFC3339(in.StartedAt, "startedAt")
	if err != nil {
		return nil, nil, err
	}
	ended, err := parseOptionalRFC3339(in.EndedAt, "endedAt")
	if err != nil {
		return nil, nil, err
	}
	out, err := a.store.UpdateTimeEntry(ctx, sc, in.ID, started, ended, in.Minutes, in.Note)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON(out)
}

func (a *app) mcpDeleteTimeEntry(ctx context.Context, req *mcp.CallToolRequest, in mcpIDIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !isUUID(in.ID) {
		return nil, nil, fmt.Errorf("id is required")
	}
	if err := a.store.SoftDeleteTimeEntry(ctx, sc, in.ID); err != nil {
		return nil, nil, err
	}
	return mcpJSON(map[string]any{"deleted": true, "id": in.ID})
}

func parseOptionalRFC3339(v, field string) (*time.Time, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(v))
	if err != nil {
		return nil, fmt.Errorf("%s must be RFC3339: %w", field, err)
	}
	return &t, nil
}

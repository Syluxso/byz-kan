package main

import (
	"net/http"
	"testing"
)

// collabFixture creates a board and one ticket to hang collab objects off.
func collabFixture(t *testing.T) (http.Handler, string, string, string, BoardView, TicketView) {
	t.Helper()
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Collab", "keyPrefix": "COL"}, &board)
	var tkt TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "collab target"}, &tkt)
	return h, org, tenant, user, board, tkt
}

// CW-6: comments round-trip, addressed by ticket key.
func TestMCPComments(t *testing.T) {
	h, org, tenant, user, _, tkt := collabFixture(t)

	var c CommentView
	mcpToolCall(t, h, org, tenant, user, "add_comment",
		map[string]any{"key": tkt.Key, "body": "first note"}, &c)
	if c.Body != "first note" {
		t.Fatalf("comment %+v", c)
	}

	var list []CommentView
	mcpToolCall(t, h, org, tenant, user, "list_comments", map[string]any{"key": tkt.Key}, &list)
	if len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("list_comments %+v", list)
	}

	var updated CommentView
	mcpToolCall(t, h, org, tenant, user, "update_comment",
		map[string]any{"id": c.ID, "body": "edited"}, &updated)
	if updated.Body != "edited" {
		t.Fatalf("update %+v", updated)
	}

	var del map[string]any
	mcpToolCall(t, h, org, tenant, user, "delete_comment", map[string]any{"id": c.ID}, &del)
	mcpToolCall(t, h, org, tenant, user, "list_comments", map[string]any{"key": tkt.Key}, &list)
	if len(list) != 0 {
		t.Fatalf("comment survived delete: %+v", list)
	}
}

// CW-5: tag CRUD plus attach/detach.
func TestMCPTags(t *testing.T) {
	h, org, tenant, user, _, tkt := collabFixture(t)

	var tag TagView
	mcpToolCall(t, h, org, tenant, user, "create_tag",
		map[string]any{"name": "cardwallah", "kind": "project"}, &tag)
	if tag.Name != "cardwallah" {
		t.Fatalf("tag %+v", tag)
	}

	var tags []TagView
	mcpToolCall(t, h, org, tenant, user, "list_tags", map[string]any{"kind": "project"}, &tags)
	if len(tags) != 1 {
		t.Fatalf("list_tags %+v", tags)
	}

	var attached []TagView
	mcpToolCall(t, h, org, tenant, user, "add_ticket_tag",
		map[string]any{"key": tkt.Key, "tagId": tag.ID}, &attached)
	if len(attached) != 1 || attached[0].ID != tag.ID {
		t.Fatalf("attach %+v", attached)
	}

	// Filtering by tag must find the ticket.
	var byTag []TicketView
	mcpToolCall(t, h, org, tenant, user, "list_tickets",
		map[string]any{"tagId": tag.ID}, &byTag)
	if len(byTag) != 1 || byTag[0].ID != tkt.ID {
		t.Fatalf("tagId filter %+v", byTag)
	}

	var detached []TagView
	mcpToolCall(t, h, org, tenant, user, "remove_ticket_tag",
		map[string]any{"key": tkt.Key, "tagId": tag.ID}, &detached)
	if len(detached) != 0 {
		t.Fatalf("detach left %+v", detached)
	}

	var renamed TagView
	mcpToolCall(t, h, org, tenant, user, "update_tag",
		map[string]any{"id": tag.ID, "name": "cw"}, &renamed)
	if renamed.Name != "cw" {
		t.Fatalf("rename %+v", renamed)
	}
	var del map[string]any
	mcpToolCall(t, h, org, tenant, user, "delete_tag", map[string]any{"id": tag.ID}, &del)
	mcpToolCall(t, h, org, tenant, user, "list_tags", map[string]any{}, &tags)
	for _, x := range tags {
		if x.ID == tag.ID {
			t.Fatal("deleted tag still listed")
		}
	}
}

// CW-7: links, per the ticket's own acceptance example.
func TestMCPLinks(t *testing.T) {
	h, org, tenant, user, _, tkt := collabFixture(t)

	var l LinkView
	mcpToolCall(t, h, org, tenant, user, "add_link", map[string]any{
		"key": tkt.Key, "url": "https://github.com/Syluxso/byz-kan", "title": "repo",
	}, &l)
	if l.URL != "https://github.com/Syluxso/byz-kan" {
		t.Fatalf("link %+v", l)
	}

	var list []LinkView
	mcpToolCall(t, h, org, tenant, user, "list_links", map[string]any{"key": tkt.Key}, &list)
	if len(list) != 1 {
		t.Fatalf("list_links %+v", list)
	}

	var updated LinkView
	mcpToolCall(t, h, org, tenant, user, "update_link",
		map[string]any{"id": l.ID, "title": "byz-kan repo"}, &updated)
	if updated.Title == nil || *updated.Title != "byz-kan repo" {
		t.Fatalf("update_link %+v", updated)
	}

	var del map[string]any
	mcpToolCall(t, h, org, tenant, user, "delete_link", map[string]any{"id": l.ID}, &del)
	mcpToolCall(t, h, org, tenant, user, "list_links", map[string]any{"key": tkt.Key}, &list)
	if len(list) != 0 {
		t.Fatalf("link survived delete: %+v", list)
	}
}

// CW-4: assignees and watchers, including full replacement.
func TestMCPAssigneesAndWatchers(t *testing.T) {
	h, org, tenant, user, _, tkt := collabFixture(t)
	u1, u2, u3 := newTestUUID(), newTestUUID(), newTestUUID()

	var added PersonLinkView
	mcpToolCall(t, h, org, tenant, user, "add_assignee",
		map[string]any{"key": tkt.Key, "userId": u1}, &added)
	if added.UserID != u1 {
		t.Fatalf("add_assignee %+v", added)
	}

	var people []PersonLinkView
	mcpToolCall(t, h, org, tenant, user, "list_assignees", map[string]any{"key": tkt.Key}, &people)
	if len(people) != 1 {
		t.Fatalf("list_assignees %+v", people)
	}

	// Filtering by assignee must find the ticket.
	var byAssignee []TicketView
	mcpToolCall(t, h, org, tenant, user, "list_tickets",
		map[string]any{"assignee": u1}, &byAssignee)
	if len(byAssignee) != 1 || byAssignee[0].ID != tkt.ID {
		t.Fatalf("assignee filter %+v", byAssignee)
	}

	// set_assignees replaces wholesale: u1 should be gone.
	var replaced []PersonLinkView
	mcpToolCall(t, h, org, tenant, user, "set_assignees",
		map[string]any{"key": tkt.Key, "userIds": []string{u2, u3}}, &replaced)
	if len(replaced) != 2 {
		t.Fatalf("set_assignees %+v", replaced)
	}
	for _, p := range replaced {
		if p.UserID == u1 {
			t.Fatal("set_assignees did not replace the previous list")
		}
	}

	mcpToolCall(t, h, org, tenant, user, "remove_assignee",
		map[string]any{"key": tkt.Key, "userId": u2}, &map[string]any{})
	mcpToolCall(t, h, org, tenant, user, "list_assignees", map[string]any{"key": tkt.Key}, &people)
	if len(people) != 1 || people[0].UserID != u3 {
		t.Fatalf("after remove %+v", people)
	}

	// Clearing with an empty list.
	mcpToolCall(t, h, org, tenant, user, "set_assignees",
		map[string]any{"key": tkt.Key, "userIds": []string{}}, &replaced)
	if len(replaced) != 0 {
		t.Fatalf("clear left %+v", replaced)
	}

	// Watchers are a separate list.
	var w PersonLinkView
	mcpToolCall(t, h, org, tenant, user, "add_watcher",
		map[string]any{"key": tkt.Key, "userId": u1}, &w)
	mcpToolCall(t, h, org, tenant, user, "list_watchers", map[string]any{"key": tkt.Key}, &people)
	if len(people) != 1 || people[0].UserID != u1 {
		t.Fatalf("watchers %+v", people)
	}
	mcpToolCall(t, h, org, tenant, user, "list_assignees", map[string]any{"key": tkt.Key}, &people)
	if len(people) != 0 {
		t.Fatal("adding a watcher leaked into assignees")
	}
}

// CW-9: checklist with items, one marked done.
func TestMCPChecklists(t *testing.T) {
	h, org, tenant, user, _, tkt := collabFixture(t)

	var cl ChecklistView
	mcpToolCall(t, h, org, tenant, user, "create_checklist",
		map[string]any{"key": tkt.Key, "title": "Ship"}, &cl)
	if cl.Title != "Ship" {
		t.Fatalf("checklist %+v", cl)
	}

	var i1, i2 ChecklistItemView
	mcpToolCall(t, h, org, tenant, user, "add_checklist_item",
		map[string]any{"checklistId": cl.ID, "title": "write tests"}, &i1)
	mcpToolCall(t, h, org, tenant, user, "add_checklist_item",
		map[string]any{"checklistId": cl.ID, "title": "deploy"}, &i2)

	var done ChecklistItemView
	mcpToolCall(t, h, org, tenant, user, "update_checklist_item",
		map[string]any{"id": i1.ID, "isDone": true}, &done)
	if !done.IsDone {
		t.Fatalf("item not marked done: %+v", done)
	}

	var lists []ChecklistView
	mcpToolCall(t, h, org, tenant, user, "list_checklists", map[string]any{"key": tkt.Key}, &lists)
	if len(lists) != 1 || len(lists[0].Items) != 2 {
		t.Fatalf("list_checklists %+v", lists)
	}
	var doneCount int
	for _, it := range lists[0].Items {
		if it.IsDone {
			doneCount++
		}
	}
	if doneCount != 1 {
		t.Fatalf("want exactly 1 done item, got %d", doneCount)
	}

	mcpToolCall(t, h, org, tenant, user, "delete_checklist_item",
		map[string]any{"id": i2.ID}, &map[string]any{})
	mcpToolCall(t, h, org, tenant, user, "list_checklists", map[string]any{"key": tkt.Key}, &lists)
	if len(lists[0].Items) != 1 {
		t.Fatalf("item survived delete: %+v", lists[0].Items)
	}
}

// CW-8: attachments are fileId references; a missing fileId must be rejected.
func TestMCPAttachments(t *testing.T) {
	h, org, tenant, user, _, tkt := collabFixture(t)

	rec := mcpCall(t, h, org, tenant, user,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_attachment","arguments":{"key":"`+tkt.Key+`"}}}`)
	if !isToolError(rec.Body.String()) {
		t.Fatalf("add_attachment without fileId should fail: %s", rec.Body.String())
	}

	fileID := newTestUUID()
	var att AttachmentView
	mcpToolCall(t, h, org, tenant, user, "add_attachment",
		map[string]any{"key": tkt.Key, "fileId": fileID, "filename": "spec.pdf"}, &att)
	if att.FileID != fileID {
		t.Fatalf("attachment %+v", att)
	}

	var list []AttachmentView
	mcpToolCall(t, h, org, tenant, user, "list_attachments", map[string]any{"key": tkt.Key}, &list)
	if len(list) != 1 {
		t.Fatalf("list_attachments %+v", list)
	}

	mcpToolCall(t, h, org, tenant, user, "delete_attachment",
		map[string]any{"id": att.ID}, &map[string]any{})
	mcpToolCall(t, h, org, tenant, user, "list_attachments", map[string]any{"key": tkt.Key}, &list)
	if len(list) != 0 {
		t.Fatalf("attachment survived delete: %+v", list)
	}
}

// CW-10: time entry CRUD keeps ticket.loggedMinutes consistent.
func TestMCPTimeEntries(t *testing.T) {
	h, org, tenant, user, _, tkt := collabFixture(t)

	var e1 TimeEntryView
	mcpToolCall(t, h, org, tenant, user, "log_time",
		map[string]any{"key": tkt.Key, "minutes": 30, "note": "first"}, &e1)
	if e1.Minutes != 30 {
		t.Fatalf("log_time %+v", e1)
	}

	// CW-10 also asks for start/end rather than an explicit duration.
	var e2 TimeEntryView
	mcpToolCall(t, h, org, tenant, user, "log_time", map[string]any{
		"key":       tkt.Key,
		"startedAt": "2026-08-26T10:00:00Z",
		"endedAt":   "2026-08-26T10:45:00Z",
	}, &e2)
	if e2.Minutes != 45 {
		t.Fatalf("start/end should compute 45 minutes, got %d", e2.Minutes)
	}

	var current TicketView
	mcpToolCall(t, h, org, tenant, user, "get_ticket", map[string]any{"id": tkt.ID}, &current)
	if current.LoggedMinutes != 75 {
		t.Fatalf("loggedMinutes=%d want 75", current.LoggedMinutes)
	}

	var updated TimeEntryView
	mcpToolCall(t, h, org, tenant, user, "update_time_entry",
		map[string]any{"id": e1.ID, "minutes": 10, "note": "corrected"}, &updated)
	if updated.Minutes != 10 {
		t.Fatalf("update_time_entry %+v", updated)
	}
	mcpToolCall(t, h, org, tenant, user, "get_ticket", map[string]any{"id": tkt.ID}, &current)
	if current.LoggedMinutes != 55 {
		t.Fatalf("after edit loggedMinutes=%d want 55", current.LoggedMinutes)
	}

	mcpToolCall(t, h, org, tenant, user, "delete_time_entry",
		map[string]any{"id": e2.ID}, &map[string]any{})
	mcpToolCall(t, h, org, tenant, user, "get_ticket", map[string]any{"id": tkt.ID}, &current)
	if current.LoggedMinutes != 10 {
		t.Fatalf("after delete loggedMinutes=%d want 10", current.LoggedMinutes)
	}

	var entries []TimeEntryView
	mcpToolCall(t, h, org, tenant, user, "list_time_entries", map[string]any{"key": tkt.Key}, &entries)
	if len(entries) != 1 || entries[0].ID != e1.ID {
		t.Fatalf("list_time_entries %+v", entries)
	}
}

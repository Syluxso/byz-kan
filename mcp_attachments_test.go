package main

import (
	"context"
	"testing"
)

// CW-19 acceptance: a file attaches to a ticket AND to a board, and each lists
// only its own.
func TestMCPAttachmentParents(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Files", "keyPrefix": "FIL"}, &board)
	var tkt TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "has a file"}, &tkt)
	var msg MessageView
	mcpToolCall(t, h, org, tenant, user, "add_message", map[string]any{
		"boardId": board.ID, "actorKey": "claude-code", "displayName": "Claude", "body": "with a file",
	}, &msg)

	ticketFile, boardFile, msgFile := newTestUUID(), newTestUUID(), newTestUUID()

	var a AttachmentView
	mcpToolCall(t, h, org, tenant, user, "add_attachment",
		map[string]any{"key": tkt.Key, "fileId": ticketFile, "filename": "spec.pdf"}, &a)
	if a.ParentType != "ticket" || a.ParentID != tkt.ID {
		t.Fatalf("ticket attachment parent wrong: %+v", a)
	}
	// The legacy column stays populated for ticket parents.
	if a.TicketID == nil || *a.TicketID != tkt.ID {
		t.Fatalf("legacy ticketId not set: %+v", a.TicketID)
	}

	mcpToolCall(t, h, org, tenant, user, "add_attachment",
		map[string]any{"parentType": "board", "parentId": board.ID, "fileId": boardFile}, &a)
	if a.ParentType != "board" || a.ParentID != board.ID {
		t.Fatalf("board attachment parent wrong: %+v", a)
	}
	if a.TicketID != nil {
		t.Fatalf("board attachment set a ticketId: %v", *a.TicketID)
	}

	mcpToolCall(t, h, org, tenant, user, "add_attachment",
		map[string]any{"parentType": "message", "parentId": msg.ID, "fileId": msgFile}, &a)
	if a.ParentType != "message" || a.ParentID != msg.ID {
		t.Fatalf("message attachment parent wrong: %+v", a)
	}

	// Each parent lists only its own file.
	check := func(args map[string]any, wantFile string, label string) {
		t.Helper()
		var list []AttachmentView
		mcpToolCall(t, h, org, tenant, user, "list_attachments", args, &list)
		if len(list) != 1 {
			t.Fatalf("%s returned %d attachments, want 1", label, len(list))
		}
		if list[0].FileID != wantFile {
			t.Fatalf("%s returned the wrong file", label)
		}
	}
	check(map[string]any{"key": tkt.Key}, ticketFile, "ticket")
	check(map[string]any{"parentType": "board", "parentId": board.ID}, boardFile, "board")
	check(map[string]any{"parentType": "message", "parentId": msg.ID}, msgFile, "message")
}

// A parent in another tenant, or one that does not exist, must be refused —
// an attachment row must never dangle off an unverified UUID.
func TestMCPAttachmentParentValidation(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Guard", "keyPrefix": "GRD"}, &board)

	bad := func(label, args string) {
		t.Helper()
		rec := mcpCall(t, h, org, tenant, user,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_attachment","arguments":`+args+`}}`)
		if !isToolError(rec.Body.String()) {
			t.Fatalf("%s should have failed: %s", label, rec.Body.String())
		}
	}

	bad("unknown parentType", `{"parentType":"planet","parentId":"`+board.ID+`","fileId":"`+newTestUUID()+`"}`)
	bad("nonexistent board", `{"parentType":"board","parentId":"`+newTestUUID()+`","fileId":"`+newTestUUID()+`"}`)
	bad("missing fileId", `{"parentType":"board","parentId":"`+board.ID+`"}`)
	bad("parentId required for board", `{"parentType":"board","fileId":"`+newTestUUID()+`"}`)

	// A board belonging to another tenant is invisible, so it is not a parent.
	rec := mcpCall(t, h, org, newTestUUID(), user,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_attachment","arguments":`+
			`{"parentType":"board","parentId":"`+board.ID+`","fileId":"`+newTestUUID()+`"}}}`)
	if !isToolError(rec.Body.String()) {
		t.Fatalf("cross-tenant parent accepted: %s", rec.Body.String())
	}
}

// Soft delete hides the row; the file id is untouched (blob lifecycle is
// byz-files' problem, not ours).
func TestMCPAttachmentSoftDelete(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Del", "keyPrefix": "DEL"}, &board)
	var a AttachmentView
	mcpToolCall(t, h, org, tenant, user, "add_attachment",
		map[string]any{"parentType": "board", "parentId": board.ID, "fileId": newTestUUID()}, &a)

	mcpToolCall(t, h, org, tenant, user, "delete_attachment",
		map[string]any{"id": a.ID}, &map[string]any{})

	var list []AttachmentView
	mcpToolCall(t, h, org, tenant, user, "list_attachments",
		map[string]any{"parentType": "board", "parentId": board.ID}, &list)
	if len(list) != 0 {
		t.Fatalf("deleted attachment still listed: %+v", list)
	}
}

// CW-19 migration: rows written before parent_type existed must still read as
// ticket attachments. Simulates a pre-migration row by blanking the new columns.
func TestLegacyAttachmentRowsStillRead(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	sc := scope{OrgID: org, TenantID: tenant, ActorID: user}

	board, err := st.CreateBoard(ctx, org, tenant, user, "Legacy", "", "LGC", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tkt, err := st.CreateTicket(ctx, sc, board.ID, "", "", "old", "", "ticket", 0, 0, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	att, err := st.CreateAttachment(ctx, sc, "ticket", tkt.ID, newTestUUID(), "old.pdf", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Make it look like a row from before the migration.
	if _, err := st.db.ExecContext(ctx,
		`UPDATE kan.attachments SET parent_type = NULL, parent_id = NULL WHERE id = $1::uuid`, att.ID); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListAttachments(ctx, sc, "ticket", tkt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("legacy row not readable: got %d", len(list))
	}
	if list[0].ParentType != "ticket" || list[0].ParentID != tkt.ID {
		t.Fatalf("legacy row did not fall back to ticket: %+v", list[0])
	}
}

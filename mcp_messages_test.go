package main

import (
	"testing"
)

// CW-18 acceptance: two agents post to the board thread and read each other
// back in order.
func TestMCPBoardMessageThread(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Thread", "keyPrefix": "THR"}, &board)

	post := func(actorKey, name, body string) MessageView {
		var m MessageView
		mcpToolCall(t, h, org, tenant, user, "add_message", map[string]any{
			"boardId": board.ID, "actorKey": actorKey, "displayName": name, "body": body,
		}, &m)
		return m
	}

	post("claude-code", "Claude", "Picked up CW-18, building the table now.")
	post("grok-web", "Grok Web", "Ack. I will stay off byz-kan until you push.")
	post("claude-code", "Claude", "Pushed. Yours.")

	var thread []MessageView
	mcpToolCall(t, h, org, tenant, user, "list_messages",
		map[string]any{"boardId": board.ID}, &thread)

	if len(thread) != 3 {
		t.Fatalf("thread has %d messages, want 3", len(thread))
	}
	// Oldest first: a thread reads top to bottom.
	if thread[0].DisplayName != "Claude" || thread[1].DisplayName != "Grok Web" {
		t.Fatalf("out of order: %v, %v", thread[0].DisplayName, thread[1].DisplayName)
	}
	if !thread[0].CreatedAt.Before(thread[2].CreatedAt) && thread[0].ID == thread[2].ID {
		t.Fatal("ordering is not chronological")
	}
	// Two agents must be distinguishable.
	if thread[0].ActorKey == thread[1].ActorKey {
		t.Fatal("actor keys collided")
	}
	// Default actor type is agent.
	if thread[0].ActorType != "agent" {
		t.Fatalf("actorType=%q want agent", thread[0].ActorType)
	}
	// Board-level messages carry no ticket.
	if thread[0].TicketID != nil {
		t.Fatalf("board message has ticketId %v", *thread[0].TicketID)
	}
}

// CW-18 acceptance: a ticket thread does not leak onto another ticket, and
// ticket chatter stays out of the board thread.
func TestMCPTicketMessageIsolation(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Isolate", "keyPrefix": "ISO"}, &board)

	var one, two TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "first"}, &one)
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "second"}, &two)

	var m MessageView
	mcpToolCall(t, h, org, tenant, user, "add_message", map[string]any{
		"key": one.Key, "actorKey": "claude-code", "displayName": "Claude", "body": "on ISO-1",
	}, &m)
	// The board is derived from the ticket, never trusted from the caller.
	if m.BoardID != board.ID || m.TicketID == nil || *m.TicketID != one.ID {
		t.Fatalf("message scoped wrong: %+v", m)
	}

	var onThread, otherThread, boardThread []MessageView
	mcpToolCall(t, h, org, tenant, user, "list_messages", map[string]any{"key": one.Key}, &onThread)
	mcpToolCall(t, h, org, tenant, user, "list_messages", map[string]any{"key": two.Key}, &otherThread)
	mcpToolCall(t, h, org, tenant, user, "list_messages", map[string]any{"boardId": board.ID}, &boardThread)

	if len(onThread) != 1 {
		t.Fatalf("ticket one thread has %d", len(onThread))
	}
	if len(otherThread) != 0 {
		t.Fatalf("message leaked onto ticket two: %+v", otherThread)
	}
	if len(boardThread) != 0 {
		t.Fatalf("ticket message leaked into the board thread: %+v", boardThread)
	}

	// ...unless explicitly asked for.
	var all []MessageView
	mcpToolCall(t, h, org, tenant, user, "list_messages",
		map[string]any{"boardId": board.ID, "all": true}, &all)
	if len(all) != 1 {
		t.Fatalf("all=true returned %d, want 1", len(all))
	}
}

// CW-18 acceptance: another tenant sees nothing.
func TestMCPMessageTenantIsolation(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Private", "keyPrefix": "PVT"}, &board)
	var m MessageView
	mcpToolCall(t, h, org, tenant, user, "add_message", map[string]any{
		"boardId": board.ID, "actorKey": "claude-code", "displayName": "Claude", "body": "internal",
	}, &m)

	var leaked []MessageView
	mcpToolCall(t, h, org, newTestUUID(), user, "list_messages",
		map[string]any{"boardId": board.ID}, &leaked)
	if len(leaked) != 0 {
		t.Fatalf("cross-tenant leak: %+v", leaked)
	}
}

// Edit and soft-delete.
func TestMCPMessageUpdateAndDelete(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Edit", "keyPrefix": "EDT"}, &board)
	var m MessageView
	mcpToolCall(t, h, org, tenant, user, "add_message", map[string]any{
		"boardId": board.ID, "actorKey": "claude-code", "displayName": "Claude", "body": "before",
	}, &m)

	var updated MessageView
	mcpToolCall(t, h, org, tenant, user, "update_message",
		map[string]any{"id": m.ID, "body": "after"}, &updated)
	if updated.Body != "after" {
		t.Fatalf("update gave %q", updated.Body)
	}

	var del map[string]any
	mcpToolCall(t, h, org, tenant, user, "delete_message", map[string]any{"id": m.ID}, &del)

	var thread []MessageView
	mcpToolCall(t, h, org, tenant, user, "list_messages",
		map[string]any{"boardId": board.ID}, &thread)
	if len(thread) != 0 {
		t.Fatalf("deleted message still listed: %+v", thread)
	}
}

// Required fields and the actor_type constraint.
func TestMCPMessageValidation(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Valid", "keyPrefix": "VLD"}, &board)

	bad := func(label string, args string) {
		t.Helper()
		rec := mcpCall(t, h, org, tenant, user,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_message","arguments":`+args+`}}`)
		if !isToolError(rec.Body.String()) {
			t.Fatalf("%s should have failed: %s", label, rec.Body.String())
		}
	}

	bad("empty body", `{"boardId":"`+board.ID+`","actorKey":"k","displayName":"n","body":"   "}`)
	bad("missing actorKey", `{"boardId":"`+board.ID+`","displayName":"n","body":"hi"}`)
	bad("missing displayName", `{"boardId":"`+board.ID+`","actorKey":"k","body":"hi"}`)
	bad("bad actorType", `{"boardId":"`+board.ID+`","actorKey":"k","displayName":"n","actorType":"robot","body":"hi"}`)
	bad("no board or ticket", `{"actorKey":"k","displayName":"n","body":"hi"}`)

	// actorType user is accepted.
	var m MessageView
	mcpToolCall(t, h, org, tenant, user, "add_message", map[string]any{
		"boardId": board.ID, "actorKey": user, "displayName": "Darryn",
		"actorType": "user", "body": "human here",
	}, &m)
	if m.ActorType != "user" {
		t.Fatalf("actorType=%q", m.ActorType)
	}
}

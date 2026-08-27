package main

import (
	"net/http"
	"testing"
)

// newCRUDApp builds an app with a live store for MCP CRUD tests.
func newCRUDApp(t *testing.T) http.Handler {
	t.Helper()
	a := &app{store: testDB(t), logBuf: NewLogBuffer()}
	return withCORS(a.routes(withTestClaims))
}

// CW-2: swimlane create / update / delete / reorder over MCP.
func TestMCPStateWriteOperations(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "State CRUD", "keyPrefix": "SCR"}, &board)

	var created StateView
	mcpToolCall(t, h, org, tenant, user, "create_state",
		map[string]any{"boardId": board.ID, "name": "In Review", "position": 2}, &created)
	if created.Name != "In Review" || created.BoardID != board.ID {
		t.Fatalf("created %+v", created)
	}

	var updated StateView
	mcpToolCall(t, h, org, tenant, user, "update_state",
		map[string]any{"id": created.ID, "name": "Peer Review"}, &updated)
	if updated.Name != "Peer Review" {
		t.Fatalf("update_state name=%q", updated.Name)
	}

	// Reorder: put the new lane first and confirm order comes back applied.
	var states []StateView
	mcpToolCall(t, h, org, tenant, user, "list_states", map[string]any{"boardId": board.ID}, &states)
	ids := []string{created.ID}
	for _, s := range states {
		if s.ID != created.ID {
			ids = append(ids, s.ID)
		}
	}
	var reordered []StateView
	mcpToolCall(t, h, org, tenant, user, "reorder_states",
		map[string]any{"boardId": board.ID, "stateIds": ids}, &reordered)
	if len(reordered) != len(ids) || reordered[0].ID != created.ID {
		t.Fatalf("reorder did not apply: first=%s want %s", reordered[0].ID, created.ID)
	}

	var del map[string]any
	mcpToolCall(t, h, org, tenant, user, "delete_state",
		map[string]any{"id": created.ID}, &del)
	if del["deleted"] != true {
		t.Fatalf("delete_state %+v", del)
	}
	mcpToolCall(t, h, org, tenant, user, "list_states", map[string]any{"boardId": board.ID}, &states)
	for _, s := range states {
		if s.ID == created.ID {
			t.Fatal("deleted state still listed")
		}
	}
}

// Deleting a swimlane that still holds tickets must not silently strand them.
func TestMCPDeleteStateWithTicketsNeedsForce(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Force", "keyPrefix": "FRC"}, &board)
	var states []StateView
	mcpToolCall(t, h, org, tenant, user, "list_states", map[string]any{"boardId": board.ID}, &states)

	var target StateView
	for _, s := range states {
		if s.Name == "In Progress" {
			target = s
		}
	}
	var tkt TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "stuck", "stateId": target.ID}, &tkt)

	// Without force this must fail rather than orphan the ticket.
	rec := mcpCall(t, h, org, tenant, user,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_state","arguments":{"id":"`+target.ID+`"}}}`)
	if body := rec.Body.String(); !isToolError(body) {
		t.Fatalf("delete_state with tickets should fail, got %s", body)
	}

	// With force the ticket is moved to the default state, not deleted.
	var del map[string]any
	mcpToolCall(t, h, org, tenant, user, "delete_state",
		map[string]any{"id": target.ID, "force": true}, &del)

	var moved TicketView
	mcpToolCall(t, h, org, tenant, user, "get_ticket", map[string]any{"id": tkt.ID}, &moved)
	if moved.StateID == target.ID {
		t.Fatal("ticket still points at the deleted state")
	}
	if moved.DeletedAt != nil {
		t.Fatal("force delete must move the ticket, not delete it")
	}
}

// CW-3: update and delete a ticket, addressed by key rather than UUID.
func TestMCPTicketUpdateAndDeleteByKey(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Ticket CRUD", "keyPrefix": "TCR"}, &board)
	var tkt TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "before"}, &tkt)
	if tkt.Key != "TCR-1" {
		t.Fatalf("key=%s", tkt.Key)
	}

	var updated TicketView
	mcpToolCall(t, h, org, tenant, user, "update_ticket", map[string]any{
		"key":      "TCR-1",
		"title":    "after",
		"priority": 7,
		"cardData": map[string]any{"suit": "spades"},
	}, &updated)
	if updated.Title != "after" || updated.Priority != 7 {
		t.Fatalf("updated %+v", updated)
	}
	if len(updated.CardData) == 0 || !containsJSON(updated.CardData, "spades") {
		t.Fatalf("cardData not stored: %s", string(updated.CardData))
	}

	// Untouched fields must survive a partial update.
	if updated.TicketType != tkt.TicketType {
		t.Fatalf("ticketType drifted: %q -> %q", tkt.TicketType, updated.TicketType)
	}

	var del map[string]any
	mcpToolCall(t, h, org, tenant, user, "delete_ticket", map[string]any{"key": "TCR-1"}, &del)
	if del["deleted"] != true {
		t.Fatalf("delete %+v", del)
	}

	// Numbers are never reused, even after a delete.
	var next TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "next"}, &next)
	if next.Key != "TCR-2" {
		t.Fatalf("key reuse: %s", next.Key)
	}
}

// CW-3: stateId filter on list_tickets.
func TestMCPListTicketsStateFilter(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Filter", "keyPrefix": "FLT"}, &board)
	var states []StateView
	mcpToolCall(t, h, org, tenant, user, "list_states", map[string]any{"boardId": board.ID}, &states)
	byName := map[string]string{}
	for _, s := range states {
		byName[s.Name] = s.ID
	}

	var a1, a2 TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "backlog one"}, &a1)
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "in progress one"}, &a2)
	var moved TicketView
	mcpToolCall(t, h, org, tenant, user, "move_ticket",
		map[string]any{"id": a2.ID, "stateId": byName["In Progress"]}, &moved)

	var inProgress []TicketView
	mcpToolCall(t, h, org, tenant, user, "list_tickets",
		map[string]any{"boardId": board.ID, "stateId": byName["In Progress"]}, &inProgress)
	if len(inProgress) != 1 || inProgress[0].ID != a2.ID {
		t.Fatalf("stateId filter returned %d tickets: %+v", len(inProgress), inProgress)
	}

	var all []TicketView
	mcpToolCall(t, h, org, tenant, user, "list_tickets",
		map[string]any{"boardId": board.ID}, &all)
	if len(all) != 2 {
		t.Fatalf("unfiltered returned %d", len(all))
	}
}

// CW-1: board get / update / publish / delete, and members.
func TestMCPBoardOperations(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Board Ops", "keyPrefix": "BOP"}, &board)

	var got BoardView
	mcpToolCall(t, h, org, tenant, user, "get_board", map[string]any{"id": board.ID}, &got)
	if got.ID != board.ID || len(got.States) != 4 {
		t.Fatalf("get_board %+v", got)
	}

	var updated BoardView
	mcpToolCall(t, h, org, tenant, user, "update_board", map[string]any{
		"id": board.ID, "name": "Renamed", "isPublished": true,
	}, &updated)
	if updated.Name != "Renamed" || !updated.IsPublished {
		t.Fatalf("update_board %+v", updated)
	}

	// Members. The creator is already owner, so add a second user.
	member := newTestUUID()
	var added MemberView
	mcpToolCall(t, h, org, tenant, user, "add_board_member",
		map[string]any{"boardId": board.ID, "userId": member}, &added)
	if added.UserID != member {
		t.Fatalf("added %+v", added)
	}
	var members []MemberView
	mcpToolCall(t, h, org, tenant, user, "list_board_members",
		map[string]any{"boardId": board.ID}, &members)
	found := false
	for _, m := range members {
		if m.UserID == member {
			found = true
		}
	}
	if !found {
		t.Fatalf("member not listed: %+v", members)
	}
	var removed map[string]any
	mcpToolCall(t, h, org, tenant, user, "remove_board_member",
		map[string]any{"boardId": board.ID, "userId": member}, &removed)
	mcpToolCall(t, h, org, tenant, user, "list_board_members",
		map[string]any{"boardId": board.ID}, &members)
	for _, m := range members {
		if m.UserID == member {
			t.Fatal("member still listed after removal")
		}
	}

	var del map[string]any
	mcpToolCall(t, h, org, tenant, user, "delete_board", map[string]any{"id": board.ID}, &del)
	if del["deleted"] != true {
		t.Fatalf("delete_board %+v", del)
	}
	var boards []BoardView
	mcpToolCall(t, h, org, tenant, user, "list_boards", map[string]any{}, &boards)
	for _, b := range boards {
		if b.ID == board.ID {
			t.Fatal("deleted board still listed")
		}
	}
}

// CW-11: activity reflects what actually happened.
func TestMCPActivityFeeds(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Activity", "keyPrefix": "ACT"}, &board)
	var tkt TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "watch me"}, &tkt)

	var states []StateView
	mcpToolCall(t, h, org, tenant, user, "list_states", map[string]any{"boardId": board.ID}, &states)
	var target string
	for _, s := range states {
		if s.Name == "Testing" {
			target = s.ID
		}
	}
	var moved TicketView
	mcpToolCall(t, h, org, tenant, user, "move_ticket",
		map[string]any{"id": tkt.ID, "stateId": target}, &moved)

	var acts []ActivityView
	mcpToolCall(t, h, org, tenant, user, "list_ticket_activity",
		map[string]any{"key": tkt.Key}, &acts)
	seen := map[string]bool{}
	for _, a := range acts {
		seen[a.Action] = true
	}
	if !seen["ticket.created"] || !seen["ticket.moved"] {
		t.Fatalf("ticket activity missing entries: %+v", acts)
	}

	var boardActs []ActivityView
	mcpToolCall(t, h, org, tenant, user, "list_board_activity",
		map[string]any{"boardId": board.ID}, &boardActs)
	if len(boardActs) == 0 {
		t.Fatal("board activity empty")
	}
}

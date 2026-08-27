package main

import (
	"testing"
)

// CW-14: an agent must be able to work one feature slice of a board by tag
// name, so "all #mcp tickets on the Cardwallah board" is a single call.
func TestMCPListTicketsByTagName(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Slice", "keyPrefix": "SLC"}, &board)

	// A tag created as "#mcp" must be stored as the bare slug.
	var tag TagView
	mcpToolCall(t, h, org, tenant, user, "create_tag",
		map[string]any{"name": "#mcp", "kind": "feature"}, &tag)
	if tag.Name != "mcp" {
		t.Fatalf("leading # not stripped on create: %q", tag.Name)
	}

	var other TagView
	mcpToolCall(t, h, org, tenant, user, "create_tag",
		map[string]any{"name": "auth", "kind": "feature"}, &other)

	tagged := map[string]bool{}
	for _, spec := range []struct {
		title string
		tagID string
	}{
		{"mcp one", tag.ID},
		{"mcp two", tag.ID},
		{"auth one", other.ID},
		{"untagged", ""},
	} {
		var tkt TicketView
		mcpToolCall(t, h, org, tenant, user, "create_ticket",
			map[string]any{"boardId": board.ID, "title": spec.title}, &tkt)
		if spec.tagID != "" {
			var out []TagView
			mcpToolCall(t, h, org, tenant, user, "add_ticket_tag",
				map[string]any{"id": tkt.ID, "tagId": spec.tagID}, &out)
			if spec.tagID == tag.ID {
				tagged[tkt.ID] = true
			}
		}
	}

	// The acceptance criterion: boardId + tag name in one call.
	for _, form := range []string{"mcp", "#mcp", "MCP", "  #mcp  "} {
		var got []TicketView
		mcpToolCall(t, h, org, tenant, user, "list_tickets",
			map[string]any{"boardId": board.ID, "tag": form}, &got)
		if len(got) != 2 {
			t.Fatalf("tag %q returned %d tickets, want 2", form, len(got))
		}
		for _, tk := range got {
			if !tagged[tk.ID] {
				t.Fatalf("tag %q returned untagged ticket %q", form, tk.Title)
			}
		}
	}

	// A different tag must not bleed in.
	var authOnly []TicketView
	mcpToolCall(t, h, org, tenant, user, "list_tickets",
		map[string]any{"boardId": board.ID, "tag": "auth"}, &authOnly)
	if len(authOnly) != 1 || authOnly[0].Title != "auth one" {
		t.Fatalf("auth filter returned %+v", authOnly)
	}

	// An unknown tag returns nothing rather than everything.
	var none []TicketView
	mcpToolCall(t, h, org, tenant, user, "list_tickets",
		map[string]any{"boardId": board.ID, "tag": "does-not-exist"}, &none)
	if len(none) != 0 {
		t.Fatalf("unknown tag returned %d tickets", len(none))
	}

	// No tag filter still returns the whole board.
	var all []TicketView
	mcpToolCall(t, h, org, tenant, user, "list_tickets",
		map[string]any{"boardId": board.ID}, &all)
	if len(all) != 4 {
		t.Fatalf("unfiltered returned %d, want 4", len(all))
	}
}

// A tag name must never match across tenants, even though names are not unique
// globally. Two tenants can both own a tag called "mcp".
func TestMCPTagNameFilterTenantIsolation(t *testing.T) {
	h := newCRUDApp(t)
	org := newTestUUID()
	tenantA, tenantB := newTestUUID(), newTestUUID()
	user := newTestUUID()

	mk := func(tenant, boardName, prefix string) (BoardView, string) {
		var b BoardView
		mcpToolCall(t, h, org, tenant, user, "create_board",
			map[string]any{"name": boardName, "keyPrefix": prefix}, &b)
		var tg TagView
		mcpToolCall(t, h, org, tenant, user, "create_tag",
			map[string]any{"name": "mcp", "kind": "feature"}, &tg)
		var tkt TicketView
		mcpToolCall(t, h, org, tenant, user, "create_ticket",
			map[string]any{"boardId": b.ID, "title": boardName + " work"}, &tkt)
		var out []TagView
		mcpToolCall(t, h, org, tenant, user, "add_ticket_tag",
			map[string]any{"id": tkt.ID, "tagId": tg.ID}, &out)
		return b, tkt.ID
	}

	boardA, ticketA := mk(tenantA, "Alpha", "ALF")
	_, ticketB := mk(tenantB, "Bravo", "BRV")

	var got []TicketView
	mcpToolCall(t, h, org, tenantA, user, "list_tickets",
		map[string]any{"boardId": boardA.ID, "tag": "mcp"}, &got)
	if len(got) != 1 || got[0].ID != ticketA {
		t.Fatalf("tenant A saw %+v", got)
	}
	for _, tk := range got {
		if tk.ID == ticketB {
			t.Fatal("tag name filter leaked a ticket across tenants")
		}
	}

	// Tenant-wide (no boardId) must also stay scoped.
	var wide []TicketView
	mcpToolCall(t, h, org, tenantA, user, "list_tickets",
		map[string]any{"tag": "mcp"}, &wide)
	if len(wide) != 1 || wide[0].ID != ticketA {
		t.Fatalf("tenant-wide tag filter returned %+v", wide)
	}
}

// "#mcp" and "mcp" must resolve to the same tag rather than creating two.
func TestMCPTagNameNormalizedOnCreate(t *testing.T) {
	h := newCRUDApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var first TagView
	mcpToolCall(t, h, org, tenant, user, "create_tag",
		map[string]any{"name": "mcp", "kind": "feature"}, &first)

	// Creating "#mcp" in the same kind collides with the existing tag.
	rec := mcpCall(t, h, org, tenant, user,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_tag","arguments":{"name":"#mcp","kind":"feature"}}}`)
	if !isToolError(rec.Body.String()) {
		t.Fatalf("creating #mcp beside mcp should conflict, got %s", rec.Body.String())
	}

	// An all-punctuation name is not a usable tag.
	rec = mcpCall(t, h, org, tenant, user,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_tag","arguments":{"name":"#"}}}`)
	if !isToolError(rec.Body.String()) {
		t.Fatalf("bare # should be rejected, got %s", rec.Body.String())
	}
}

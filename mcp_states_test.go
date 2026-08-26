package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mcpResult decodes a JSON-RPC tool response and unmarshals the tool payload into v.
func mcpResult(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("http %d %s", rec.Code, rec.Body.String())
	}
	var rpc struct {
		Error  *struct{ Message string } `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rpc); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if rpc.Error != nil {
		t.Fatalf("rpc error: %s", rpc.Error.Message)
	}
	if rpc.Result.IsError || len(rpc.Result.Content) == 0 {
		t.Fatalf("tool error: %s", rec.Body.String())
	}
	if v == nil {
		return
	}
	if err := json.Unmarshal([]byte(rpc.Result.Content[0].Text), v); err != nil {
		t.Fatalf("payload %s: %v", rpc.Result.Content[0].Text, err)
	}
}

// mcpToolCall invokes one MCP tool and decodes its payload into v.
func mcpToolCall(t *testing.T, h http.Handler, org, tenant, user, name string, args map[string]any, v any) {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatal(err)
	}
	rec := mcpCall(t, h, org, tenant, user, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+string(params)+`}`)
	mcpResult(t, rec, v)
}

// CW-12: list_states exposes swimlane UUIDs so an agent can chain
// list_states -> move_ticket and move a ticket by name, unaided.
func TestMCPListStatesAndMoveByName(t *testing.T) {
	st := testDB(t)
	a := &app{store: st, logBuf: NewLogBuffer()}
	h := withCORS(a.routes(withTestClaims))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "States Board", "keyPrefix": "STA"}, &board)

	var states []StateView
	mcpToolCall(t, h, org, tenant, user, "list_states",
		map[string]any{"boardId": board.ID}, &states)

	if len(states) != 4 {
		t.Fatalf("want 4 seeded states, got %d: %+v", len(states), states)
	}
	byName := map[string]StateView{}
	for _, s := range states {
		byName[s.Name] = s
	}
	for _, want := range []string{"Backlog", "In Progress", "Testing", "Completed"} {
		s, ok := byName[want]
		if !ok {
			t.Fatalf("list_states missing %q: %+v", want, states)
		}
		if !isUUID(s.ID) {
			t.Fatalf("state %q has non-UUID id %q", want, s.ID)
		}
		if s.BoardID != board.ID {
			t.Fatalf("state %q boardId=%s want %s", want, s.BoardID, board.ID)
		}
	}
	if states[0].Name != "Backlog" {
		t.Fatalf("states not ordered by position, first=%q", states[0].Name)
	}

	var tkt TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "Move me"}, &tkt)
	if tkt.StateID != byName["Backlog"].ID {
		t.Fatalf("new ticket not in Backlog: %s", tkt.StateID)
	}

	// The point of the ticket: resolve "Testing" by name, then move.
	var moved TicketView
	mcpToolCall(t, h, org, tenant, user, "move_ticket",
		map[string]any{"id": tkt.ID, "stateId": byName["Testing"].ID}, &moved)
	if moved.StateID != byName["Testing"].ID {
		t.Fatalf("move_ticket left ticket in %s", moved.StateID)
	}
	if moved.CompletedAt != nil {
		t.Fatalf("Testing must not set completedAt: %v", moved.CompletedAt)
	}
}

// A board's states must not leak across tenants.
func TestMCPListStatesTenantIsolation(t *testing.T) {
	st := testDB(t)
	a := &app{store: st, logBuf: NewLogBuffer()}
	h := withCORS(a.routes(withTestClaims))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Private", "keyPrefix": "PRV"}, &board)

	var leaked []StateView
	mcpToolCall(t, h, org, newTestUUID(), user, "list_states",
		map[string]any{"boardId": board.ID}, &leaked)
	if len(leaked) != 0 {
		t.Fatalf("foreign tenant saw %d states from tenant %s", len(leaked), tenant)
	}
}

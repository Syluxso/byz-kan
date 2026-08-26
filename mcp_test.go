package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPUnauthorized(t *testing.T) {
	a := &app{store: &Store{}, logBuf: NewLogBuffer()}
	h := withCORS(a.routes(withTestClaims))
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMCPToolsList(t *testing.T) {
	a := &app{store: &Store{}, logBuf: NewLogBuffer()}
	h := withCORS(a.routes(withTestClaims))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("X-Test-Org", org)
	req.Header.Set("X-Test-Tenant", tenant)
	req.Header.Set("X-Test-User", user)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rec.Code, rec.Body.String())
	}
	raw, _ := io.ReadAll(rec.Body)
	s := string(raw)
	for _, name := range []string{"list_boards", "create_board", "list_states", "list_tickets", "create_ticket", "get_ticket", "move_ticket", "log_time"} {
		if !strings.Contains(s, name) {
			t.Fatalf("tools/list missing %s: %s", name, s)
		}
	}
}

func mcpCall(t *testing.T, h http.Handler, org, tenant, user, payload string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("X-Test-Org", org)
	req.Header.Set("X-Test-Tenant", tenant)
	req.Header.Set("X-Test-User", user)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMCPCreateBoardAndTicket(t *testing.T) {
	st := testDB(t)
	a := &app{store: st, logBuf: NewLogBuffer()}
	h := withCORS(a.routes(withTestClaims))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	rec := mcpCall(t, h, org, tenant, user, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_board","arguments":{"name":"Grok Board","keyPrefix":"GRK"}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create_board http %d %s", rec.Code, rec.Body.String())
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
	var board BoardView
	if err := json.Unmarshal([]byte(rpc.Result.Content[0].Text), &board); err != nil {
		t.Fatalf("board json %s: %v", rpc.Result.Content[0].Text, err)
	}
	if board.KeyPrefix != "GRK" || len(board.States) != 4 {
		t.Fatalf("board %+v", board)
	}

	arg, _ := json.Marshal(map[string]any{
		"name": "create_ticket",
		"arguments": map[string]any{
			"boardId": board.ID,
			"title":   "From Grok",
		},
	})
	rec = mcpCall(t, h, org, tenant, user, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+string(arg)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create_ticket http %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rpc); err != nil {
		t.Fatalf("decode ticket %s: %v", rec.Body.String(), err)
	}
	if rpc.Result.IsError || len(rpc.Result.Content) == 0 {
		t.Fatalf("create_ticket error %s", rec.Body.String())
	}
	var tkt TicketView
	if err := json.Unmarshal([]byte(rpc.Result.Content[0].Text), &tkt); err != nil {
		t.Fatalf("ticket json: %v %s", err, rpc.Result.Content[0].Text)
	}
	if tkt.Key != "GRK-1" {
		t.Fatalf("key=%s", tkt.Key)
	}
}

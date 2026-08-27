package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// newEventApp builds an app whose store publishes into a live hub.
func newEventApp(t *testing.T) (*app, http.Handler) {
	t.Helper()
	st := testDB(t)
	hub := NewHub()
	st.hub = hub
	a := &app{store: st, logBuf: NewLogBuffer(), hub: hub}
	return a, withCORS(a.routes(withTestClaims))
}

// recvEvent waits for one event on ch, failing the test if none arrives.
func recvEvent(t *testing.T, ch <-chan Event, within time.Duration) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("event channel closed")
		}
		return ev
	case <-time.After(within):
		t.Fatal("timed out waiting for event")
	}
	return Event{}
}

// CW-13: two subscribers on the same board both see a move.
func TestHubFansOutToAllSubscribers(t *testing.T) {
	a, h := newEventApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	sc := scope{OrgID: org, TenantID: tenant, ActorID: user}

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "SSE Board", "keyPrefix": "SSE"}, &board)

	first, cancelFirst := a.hub.Subscribe(org, tenant, board.ID)
	defer cancelFirst()
	second, cancelSecond := a.hub.Subscribe(org, tenant, board.ID)
	defer cancelSecond()

	tkt, err := a.store.CreateTicket(context.Background(), sc, board.ID, "", "", "Live", "", "ticket", 0, 0, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev := recvEvent(t, first, 2*time.Second); ev.Type != "ticket.created" {
		t.Fatalf("first subscriber got %q", ev.Type)
	}
	if ev := recvEvent(t, second, 2*time.Second); ev.Type != "ticket.created" {
		t.Fatalf("second subscriber got %q", ev.Type)
	}

	var testing_ string
	for _, s := range board.States {
		if s.Name == "Testing" {
			testing_ = s.ID
		}
	}
	if _, err := a.store.MoveTicket(context.Background(), sc, tkt.ID, testing_, nil); err != nil {
		t.Fatal(err)
	}
	ev := recvEvent(t, first, 2*time.Second)
	if ev.Type != "ticket.moved" {
		t.Fatalf("type=%q", ev.Type)
	}
	if ev.BoardID != board.ID || ev.TicketID != tkt.ID {
		t.Fatalf("event ids wrong: %+v", ev)
	}
	if ev.ActorID != user {
		t.Fatalf("actorId=%q want %q", ev.ActorID, user)
	}
	if got, _ := ev.Payload["stateId"].(string); got != testing_ {
		t.Fatalf("payload stateId=%v want %s", ev.Payload["stateId"], testing_)
	}
	if ev.At.IsZero() {
		t.Fatal("event has no timestamp")
	}
}

// A subscriber must never receive another tenant's board events.
func TestHubTenantIsolation(t *testing.T) {
	a, h := newEventApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	sc := scope{OrgID: org, TenantID: tenant, ActorID: user}

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Tenant A", "keyPrefix": "TNA"}, &board)

	// Same board id, different tenant. Must stay silent.
	foreign, cancel := a.hub.Subscribe(org, newTestUUID(), board.ID)
	defer cancel()
	mine, cancelMine := a.hub.Subscribe(org, tenant, board.ID)
	defer cancelMine()

	if _, err := a.store.CreateTicket(context.Background(), sc, board.ID, "", "", "Secret", "", "ticket", 0, 0, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	recvEvent(t, mine, 2*time.Second) // the rightful subscriber gets it

	select {
	case ev := <-foreign:
		t.Fatalf("cross-tenant leak: %+v", ev)
	case <-time.After(300 * time.Millisecond):
	}
}

// A mutation driven over MCP (not REST) must also emit.
func TestMCPMutationEmitsEvent(t *testing.T) {
	a, h := newEventApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "MCP Board", "keyPrefix": "MCB"}, &board)

	sub, cancel := a.hub.Subscribe(org, tenant, board.ID)
	defer cancel()

	var tkt TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "via MCP"}, &tkt)
	if ev := recvEvent(t, sub, 2*time.Second); ev.Type != "ticket.created" {
		t.Fatalf("got %q", ev.Type)
	}

	var states []StateView
	mcpToolCall(t, h, org, tenant, user, "list_states", map[string]any{"boardId": board.ID}, &states)
	var target string
	for _, s := range states {
		if s.Name == "In Progress" {
			target = s.ID
		}
	}
	var moved TicketView
	mcpToolCall(t, h, org, tenant, user, "move_ticket",
		map[string]any{"id": tkt.ID, "stateId": target}, &moved)

	ev := recvEvent(t, sub, 2*time.Second)
	if ev.Type != "ticket.moved" {
		t.Fatalf("got %q", ev.Type)
	}
	if got, _ := ev.Payload["stateId"].(string); got != target {
		t.Fatalf("stateId=%v", ev.Payload["stateId"])
	}
}

// A subscriber that never drains must not stall the mutating caller.
func TestHubSlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	hub := NewHub()
	org, tenant, board := newTestUUID(), newTestUUID(), newTestUUID()
	_, cancel := hub.Subscribe(org, tenant, board)
	defer cancel()

	done := make(chan struct{})
	go func() {
		// Far more than subBuffer, with nothing reading.
		for i := 0; i < subBuffer*10; i++ {
			hub.Publish(org, tenant, Event{Type: "ticket.updated", BoardID: board})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}

// Unsubscribing must deregister and close, leaving no goroutine behind.
func TestHubUnsubscribeCleansUp(t *testing.T) {
	hub := NewHub()
	org, tenant, board := newTestUUID(), newTestUUID(), newTestUUID()

	ch, cancel := hub.Subscribe(org, tenant, board)
	if n := hub.subscriberCount(org, tenant, board); n != 1 {
		t.Fatalf("count=%d want 1", n)
	}
	cancel()
	if n := hub.subscriberCount(org, tenant, board); n != 0 {
		t.Fatalf("count=%d want 0 after cancel", n)
	}
	if _, open := <-ch; open {
		t.Fatal("channel should be closed after cancel")
	}
	cancel() // idempotent; must not panic on double close
	hub.Publish(org, tenant, Event{Type: "ticket.updated", BoardID: board})
}

// The SSE endpoint must stream a real event to an HTTP client and hang up cleanly.
func TestBoardEventsSSEStream(t *testing.T) {
	a, h := newEventApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	sc := scope{OrgID: org, TenantID: tenant, ActorID: user}

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Stream", "keyPrefix": "STM"}, &board)

	srv := httptest.NewServer(h)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/boards/"+board.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Test-Org", org)
	req.Header.Set("X-Test-Tenant", tenant)
	req.Header.Set("X-Test-User", user)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatal("missing X-Accel-Buffering: no; gateway will buffer the stream")
	}

	br := bufio.NewReader(resp.Body)
	// Opening comment confirms the stream is live before any event.
	line, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, ": subscribed") {
		t.Fatalf("preamble=%q err=%v", line, err)
	}

	// Wait for the handler to actually register before publishing.
	deadline := time.Now().Add(2 * time.Second)
	for a.hub.subscriberCount(org, tenant, board.ID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("handler never subscribed")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := a.store.CreateTicket(context.Background(), sc, board.ID, "", "", "Streamed", "", "ticket", 0, 0, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	var evType, data string
	for i := 0; i < 20; i++ {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			evType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
		if evType != "" && data != "" {
			break
		}
	}
	if evType != "ticket.created" {
		t.Fatalf("event type %q", evType)
	}
	var ev Event
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		t.Fatalf("data %q: %v", data, err)
	}
	if ev.BoardID != board.ID || ev.Type != "ticket.created" {
		t.Fatalf("payload %+v", ev)
	}

	// Disconnect must unregister the subscriber.
	resp.Body.Close()
	deadline = time.Now().Add(2 * time.Second)
	for a.hub.subscriberCount(org, tenant, board.ID) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber leaked after client disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A stream for a board in another tenant must be refused, not opened empty.
func TestBoardEventsRejectsForeignTenant(t *testing.T) {
	_, h := newEventApp(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Closed", "keyPrefix": "CLS"}, &board)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/boards/"+board.ID+"/events", nil)
	req.Header.Set("X-Test-Org", org)
	req.Header.Set("X-Test-Tenant", newTestUUID())
	req.Header.Set("X-Test-User", user)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("foreign tenant opened a stream: %d", rec.Code)
	}
}

// Goroutine accounting across many connect/disconnect cycles.
func TestHubNoGoroutineLeakAcrossManySubscribers(t *testing.T) {
	hub := NewHub()
	org, tenant, board := newTestUUID(), newTestUUID(), newTestUUID()

	before := runtime.NumGoroutine()
	for i := 0; i < 200; i++ {
		_, cancel := hub.Subscribe(org, tenant, board)
		hub.Publish(org, tenant, Event{Type: "ticket.updated", BoardID: board})
		cancel()
	}
	if n := hub.subscriberCount(org, tenant, board); n != 0 {
		t.Fatalf("%d subscribers still registered", n)
	}
	runtime.Gosched()
	if after := runtime.NumGoroutine(); after > before+10 {
		t.Fatalf("goroutines %d -> %d", before, after)
	}
}

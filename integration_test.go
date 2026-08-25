package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func testDB(t *testing.T) *Store {
	t.Helper()
	url := env("KAN_TEST_DB", env("DB_URL", "postgres://db:db@127.0.0.1:5441/kan?sslmode=disable"))
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Skipf("open db: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("postgres not available: %v", err)
	}
	st := NewStore(db)
	if err := st.init(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return st
}

func newTestUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func testClient(t *testing.T, st *Store, org, tenant, user string) *http.ServeMux {
	t.Helper()
	a := &app{store: st, logBuf: NewLogBuffer()}
	return a.routes(withTestClaims)
}

func doJSON(t *testing.T, h http.Handler, method, path, org, tenant, user string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Test-Org", org)
	req.Header.Set("X-Test-Tenant", tenant)
	req.Header.Set("X-Test-User", user)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v body=%s", rec.Result().Status, err, rec.Body.String())
	}
	return v
}

func TestV1Acceptance(t *testing.T) {
	if os.Getenv("KAN_SKIP_INTEGRATION") == "1" {
		t.Skip("skipped")
	}
	st := testDB(t)
	h := func(org, tenant, user string) http.Handler { return testClient(t, st, org, tenant, user) }

	org := newTestUUID()
	tenA := newTestUUID()
	tenB := newTestUUID()
	userA := newTestUUID()
	userB := newTestUUID()
	_ = userB

	// 3. board seeds four states; new ticket lands in Backlog
	rec := doJSON(t, h(org, tenA, userA), http.MethodPost, "/api/v1/boards", org, tenA, userA, map[string]any{
		"name": "Demo Board", "keyPrefix": "DEMO",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create board %d %s", rec.Code, rec.Body.String())
	}
	board := decode[BoardView](t, rec)
	if len(board.States) != 4 {
		t.Fatalf("states=%d", len(board.States))
	}
	if board.KeyPrefix != "DEMO" {
		t.Fatalf("prefix=%s", board.KeyPrefix)
	}
	var backlog, completed string
	for _, s := range board.States {
		if s.Name == "Backlog" && s.IsDefault {
			backlog = s.ID
		}
		if s.Name == "Completed" {
			completed = s.ID
		}
	}
	if backlog == "" || completed == "" {
		t.Fatal("missing backlog/completed")
	}

	rec = doJSON(t, h(org, tenA, userA), http.MethodPost, "/api/v1/boards/"+board.ID+"/tickets", org, tenA, userA, map[string]any{
		"title": "First", "priority": 7,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ticket %d %s", rec.Code, rec.Body.String())
	}
	t1 := decode[TicketView](t, rec)
	if t1.Key != "DEMO-1" {
		t.Fatalf("key=%s", t1.Key)
	}
	if t1.StateID != backlog {
		t.Fatalf("expected backlog got %s", t1.StateID)
	}
	if t1.Priority != 7 {
		t.Fatalf("priority mapped? got %d", t1.Priority)
	}

	// 4. monotonic keys; soft-delete does not reuse numbers
	rec = doJSON(t, h(org, tenA, userA), http.MethodPost, "/api/v1/boards/"+board.ID+"/tickets", org, tenA, userA, map[string]any{
		"title": "Second",
	})
	t2 := decode[TicketView](t, rec)
	if t2.Key != "DEMO-2" {
		t.Fatalf("key=%s", t2.Key)
	}
	rec = doJSON(t, h(org, tenA, userA), http.MethodDelete, "/api/v1/tickets/id/"+t2.ID, org, tenA, userA, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h(org, tenA, userA), http.MethodPost, "/api/v1/boards/"+board.ID+"/tickets", org, tenA, userA, map[string]any{
		"title": "Third",
	})
	t3 := decode[TicketView](t, rec)
	if t3.Key != "DEMO-3" {
		t.Fatalf("reused number? key=%s", t3.Key)
	}

	// 8. key lookup tenant scoped
	rec = doJSON(t, h(org, tenA, userA), http.MethodGet, "/api/v1/tickets/key/DEMO-1", org, tenA, userA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("key lookup %d", rec.Code)
	}

	// 2. other tenant can also mint DEMO-1 and cannot see A's ticket
	rec = doJSON(t, h(org, tenB, userA), http.MethodPost, "/api/v1/boards", org, tenB, userA, map[string]any{
		"name": "Other", "keyPrefix": "DEMO",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("tenant B board %d %s", rec.Code, rec.Body.String())
	}
	boardB := decode[BoardView](t, rec)
	rec = doJSON(t, h(org, tenB, userA), http.MethodPost, "/api/v1/boards/"+boardB.ID+"/tickets", org, tenB, userA, map[string]any{
		"title": "B first",
	})
	tb := decode[TicketView](t, rec)
	if tb.Key != "DEMO-1" {
		t.Fatalf("tenant B key=%s", tb.Key)
	}
	rec = doJSON(t, h(org, tenB, userA), http.MethodGet, "/api/v1/tickets/key/DEMO-1", org, tenB, userA, nil)
	gotB := decode[TicketView](t, rec)
	if gotB.ID != tb.ID {
		t.Fatal("tenant B key leaked tenant A ticket")
	}
	rec = doJSON(t, h(org, tenB, userA), http.MethodGet, "/api/v1/tickets/id/"+t1.ID, org, tenB, userA, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant id got %d", rec.Code)
	}

	// 10. move to Completed sets completedAt; move away clears
	rec = doJSON(t, h(org, tenA, userA), http.MethodPost, "/api/v1/tickets/id/"+t1.ID+"/move", org, tenA, userA, map[string]any{
		"stateId": completed,
	})
	moved := decode[TicketView](t, rec)
	if moved.CompletedAt == nil {
		t.Fatal("expected completedAt")
	}
	rec = doJSON(t, h(org, tenA, userA), http.MethodPost, "/api/v1/tickets/id/"+t1.ID+"/move", org, tenA, userA, map[string]any{
		"stateId": backlog,
	})
	moved = decode[TicketView](t, rec)
	if moved.CompletedAt != nil {
		t.Fatal("expected completedAt cleared")
	}

	// 6. time entries
	start := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	end := start.Add(61 * time.Second)
	rec = doJSON(t, h(org, tenA, userA), http.MethodPost, "/api/v1/tickets/id/"+t1.ID+"/time-entries", org, tenA, userA, map[string]any{
		"minutes": 10,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("time minutes %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h(org, tenA, userA), http.MethodPost, "/api/v1/tickets/id/"+t1.ID+"/time-entries", org, tenA, userA, map[string]any{
		"startedAt": start, "endedAt": end,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("time range %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h(org, tenA, userA), http.MethodGet, "/api/v1/tickets/id/"+t1.ID, org, tenA, userA, nil)
	t1 = decode[TicketView](t, rec)
	if t1.LoggedMinutes != 12 { // 10 + ceil(61s)=2
		t.Fatalf("loggedMinutes=%d want 12", t1.LoggedMinutes)
	}

	// 7. attachment rejects missing fileId
	rec = doJSON(t, h(org, tenA, userA), http.MethodPost, "/api/v1/tickets/id/"+t1.ID+"/attachments", org, tenA, userA, map[string]any{
		"filename": "x.png",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing fileId got %d %s", rec.Code, rec.Body.String())
	}
	fileID := newTestUUID()
	rec = doJSON(t, h(org, tenA, userA), http.MethodPost, "/api/v1/tickets/id/"+t1.ID+"/attachments", org, tenA, userA, map[string]any{
		"fileId": fileID, "filename": "x.png",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("attach %d %s", rec.Code, rec.Body.String())
	}

	// nested board ticket route
	rec = doJSON(t, h(org, tenA, userA), http.MethodGet, "/api/v1/boards/"+board.ID+"/tickets/id/"+t1.ID, org, tenA, userA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("nested get %d", rec.Code)
	}
	rec = doJSON(t, h(org, tenA, userA), http.MethodGet, "/api/v1/boards/"+boardB.ID+"/tickets/id/"+t1.ID, org, tenA, userA, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nested wrong board got %d", rec.Code)
	}

	// 5. soft-delete board hides tickets from non-owners
	rec = doJSON(t, h(org, tenA, userA), http.MethodDelete, "/api/v1/boards/"+board.ID, org, tenA, userA, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete board %d %s", rec.Code, rec.Body.String())
	}
	stranger := newTestUUID()
	rec = doJSON(t, h(org, tenA, stranger), http.MethodGet, "/api/v1/boards/"+board.ID, org, tenA, stranger, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger sees deleted board %d", rec.Code)
	}
	rec = doJSON(t, h(org, tenA, stranger), http.MethodGet, "/api/v1/tickets/id/"+t1.ID, org, tenA, stranger, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger sees deleted ticket %d", rec.Code)
	}
	rec = doJSON(t, h(org, tenA, userA), http.MethodGet, "/api/v1/boards/"+board.ID+"?includeDeleted=1", org, tenA, userA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner includeDeleted board %d %s", rec.Code, rec.Body.String())
	}

}

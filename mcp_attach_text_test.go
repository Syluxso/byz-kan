package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CW-39. The refusals matter more than the happy path here: this tool reads
// another service's bytes on the caller's behalf, so every way it says no is
// part of the contract.

// readApp builds an app whose byz-files is a stub, and returns both the handler
// and the app so a test can retune the stub.
func readApp(t *testing.T, files http.HandlerFunc) (http.Handler, *app) {
	t.Helper()
	a := &app{store: testDB(t), logBuf: NewLogBuffer(), httpc: http.DefaultClient}
	srv := httptest.NewServer(files)
	t.Cleanup(srv.Close)
	a.filesURL = srv.URL
	return withCORS(a.routes(withTestClaims)), a
}

func serveBody(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/content") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}
}

func serveStatus(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }
}

// toolErr calls a tool and returns the error text, failing if it succeeded.
func toolErr(t *testing.T, h http.Handler, org, tenant, user, name string, args map[string]any) string {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatal(err)
	}
	rec := mcpCall(t, h, org, tenant, user,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+string(params)+`}`)
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
		return rpc.Error.Message
	}
	if !rpc.Result.IsError {
		t.Fatalf("%s unexpectedly succeeded: %s", name, rec.Body.String())
	}
	if len(rpc.Result.Content) == 0 {
		t.Fatalf("%s failed with no message: %s", name, rec.Body.String())
	}
	return rpc.Result.Content[0].Text
}

// attachOn puts a file on a fresh ticket and returns the attachment row.
func attachOn(t *testing.T, h http.Handler, org, tenant, user, filename, ctype string, size *int64) AttachmentView {
	t.Helper()
	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Read", "keyPrefix": "RD"}, &board)
	var tkt TicketView
	mcpToolCall(t, h, org, tenant, user, "create_ticket",
		map[string]any{"boardId": board.ID, "title": "carries a file"}, &tkt)

	args := map[string]any{"key": tkt.Key, "fileId": newTestUUID(),
		"filename": filename, "contentType": ctype}
	if size != nil {
		args["sizeBytes"] = *size
	}
	var att AttachmentView
	mcpToolCall(t, h, org, tenant, user, "add_attachment", args, &att)
	return att
}

func TestGetAttachmentTextReadsMarkdown(t *testing.T) {
	h, _ := readApp(t, serveBody("# ADR 1\n\nUse byz-files.\n"))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	att := attachOn(t, h, org, tenant, user, "adr-0001.md", "text/markdown", nil)

	var out AttachmentTextOut
	mcpToolCall(t, h, org, tenant, user, "get_attachment_text",
		map[string]any{"id": att.ID}, &out)

	if !strings.Contains(out.Text, "Use byz-files.") {
		t.Fatalf("body not returned: %q", out.Text)
	}
	if out.Truncated {
		t.Fatal("truncated should be false when the file fits")
	}
	if out.SizeBytes != int64(len(out.Text)) {
		t.Fatalf("sizeBytes %d != text length %d", out.SizeBytes, len(out.Text))
	}
	if out.AttachmentID != att.ID || out.FileID != att.FileID {
		t.Fatalf("identity wrong: %+v", out)
	}
}

// contentType on the kan row is caller-asserted and often useless, so the
// extension has to carry it alone.
func TestGetAttachmentTextAcceptsExtensionWhenTypeIsUseless(t *testing.T) {
	h, _ := readApp(t, serveBody(`{"ok":true}`))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	att := attachOn(t, h, org, tenant, user, "data.json", "application/octet-stream", nil)

	var out AttachmentTextOut
	mcpToolCall(t, h, org, tenant, user, "get_attachment_text",
		map[string]any{"id": att.ID}, &out)
	if out.Text != `{"ok":true}` {
		t.Fatalf("unexpected body: %q", out.Text)
	}
}

func TestGetAttachmentTextRefusesBinary(t *testing.T) {
	// The stub fails the test if it is called at all: a png must be refused
	// before byz-files is ever asked.
	h, _ := readApp(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("byz-files was called for a png")
	})
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	att := attachOn(t, h, org, tenant, user, "XVwKW.png", "image/png", nil)

	msg := toolErr(t, h, org, tenant, user, "get_attachment_text", map[string]any{"id": att.ID})
	if !strings.Contains(msg, "image/png") {
		t.Fatalf("error should name the refused type: %s", msg)
	}
}

func TestGetAttachmentTextRefusesOversizeFromTheRow(t *testing.T) {
	h, _ := readApp(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("byz-files was called for a file already known to be too big")
	})
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	big := int64(textReadCap + 1)
	att := attachOn(t, h, org, tenant, user, "huge.txt", "text/plain", &big)

	msg := toolErr(t, h, org, tenant, user, "get_attachment_text", map[string]any{"id": att.ID})
	if !strings.Contains(msg, "over the") {
		t.Fatalf("error should explain the limit: %s", msg)
	}
}

// A row that understates its size must still not get a partial document back.
func TestGetAttachmentTextRefusesOversizeFromTheBody(t *testing.T) {
	h, _ := readApp(t, serveBody(strings.Repeat("x", textReadCap+10)))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	att := attachOn(t, h, org, tenant, user, "lies.txt", "text/plain", nil)

	msg := toolErr(t, h, org, tenant, user, "get_attachment_text", map[string]any{"id": att.ID})
	if !strings.Contains(msg, "larger than") {
		t.Fatalf("unexpected error: %s", msg)
	}
}

// CW-28: a 401 must name the cause or it costs someone an afternoon.
func TestGetAttachmentTextExplainsAPATRejection(t *testing.T) {
	h, _ := readApp(t, serveStatus(http.StatusUnauthorized))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	att := attachOn(t, h, org, tenant, user, "notes.md", "text/markdown", nil)

	msg := toolErr(t, h, org, tenant, user, "get_attachment_text", map[string]any{"id": att.ID})
	if !strings.Contains(msg, "CW-28") || !strings.Contains(msg, "IAM") {
		t.Fatalf("401 should point at the credential problem: %s", msg)
	}
}

// CW-29: a pointer byz-files never had reads as broken, not as empty.
func TestGetAttachmentTextExplainsAGhostFileID(t *testing.T) {
	h, _ := readApp(t, serveStatus(http.StatusNotFound))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	att := attachOn(t, h, org, tenant, user, "ghost.md", "text/markdown", nil)

	msg := toolErr(t, h, org, tenant, user, "get_attachment_text", map[string]any{"id": att.ID})
	if !strings.Contains(msg, "CW-29") {
		t.Fatalf("404 should point at the phantom-pointer defect: %s", msg)
	}
}

func TestGetAttachmentTextRejectsUnknownAttachment(t *testing.T) {
	h, _ := readApp(t, serveBody("should never be read"))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()

	toolErr(t, h, org, tenant, user, "get_attachment_text", map[string]any{"id": newTestUUID()})
	toolErr(t, h, org, tenant, user, "get_attachment_text", map[string]any{})
}

// The tenant boundary. Another tenant's attachment must be indistinguishable
// from one that does not exist, by id and by fileId alike.
func TestGetAttachmentTextDoesNotCrossTenants(t *testing.T) {
	h, _ := readApp(t, serveBody("secret"))
	orgA, tenantA, userA := newTestUUID(), newTestUUID(), newTestUUID()
	orgB, tenantB, userB := newTestUUID(), newTestUUID(), newTestUUID()

	att := attachOn(t, h, orgA, tenantA, userA, "private.md", "text/markdown", nil)

	toolErr(t, h, orgB, tenantB, userB, "get_attachment_text", map[string]any{"id": att.ID})
	toolErr(t, h, orgB, tenantB, userB, "get_attachment_text", map[string]any{"fileId": att.FileID})
}

// A fileId works on its own, but only because it is attached to something the
// caller can already see.
func TestGetAttachmentTextResolvesByFileID(t *testing.T) {
	h, _ := readApp(t, serveBody("found by fileId"))
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	att := attachOn(t, h, org, tenant, user, "notes.md", "text/markdown", nil)

	var out AttachmentTextOut
	mcpToolCall(t, h, org, tenant, user, "get_attachment_text",
		map[string]any{"fileId": att.FileID}, &out)
	if out.Text != "found by fileId" {
		t.Fatalf("unexpected body: %q", out.Text)
	}
	if out.AttachmentID != att.ID {
		t.Fatalf("resolved the wrong row: %s", out.AttachmentID)
	}
}

// The whole trust model in one test: byz-kan presents the CALLER's token to
// byz-files, never one of its own.
func TestFetchFileContentForwardsTheCallersToken(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	a := &app{filesURL: srv.URL, httpc: http.DefaultClient}
	body, size, err := a.fetchFileContent(context.Background(), newTestUUID(), "caller-token-123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if body != "ok" || size != 2 {
		t.Fatalf("body=%q size=%d", body, size)
	}
	if seen != "Bearer caller-token-123" {
		t.Fatalf("caller token not forwarded, files saw %q", seen)
	}
}

func TestCheckReadableAsText(t *testing.T) {
	ok := []struct{ name, ctype string }{
		{"a.md", "text/markdown"},
		{"a.md", ""},
		{"", "text/plain; charset=utf-8"},
		{"data.json", "application/octet-stream"},
		{"c.yaml", ""},
		{"", "APPLICATION/JSON"},
	}
	for _, c := range ok {
		if err := checkReadableAsText(c.name, c.ctype); err != nil {
			t.Errorf("checkReadableAsText(%q,%q) = %v, want nil", c.name, c.ctype, err)
		}
	}
	bad := []struct{ name, ctype string }{
		{"x.png", "image/png"},
		{"x.zip", "application/zip"},
		{"x.pdf", "application/pdf"},
		{"noext", ""},
		{"x.exe", "application/octet-stream"},
	}
	for _, c := range bad {
		if err := checkReadableAsText(c.name, c.ctype); err == nil {
			t.Errorf("checkReadableAsText(%q,%q) = nil, want refusal", c.name, c.ctype)
		}
	}
}

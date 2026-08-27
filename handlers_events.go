package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// heartbeatInterval keeps idle streams alive through proxy idle timeouts.
// Sent as an SSE comment, which clients ignore.
const heartbeatInterval = 25 * time.Second

// boardEvents streams live board mutations to the caller as Server-Sent Events.
//
//	GET /api/v1/boards/{boardId}/events
//
// Scope comes from the JWT like every other route, so a subscriber can only ever
// receive events for a board inside its own org and tenant.
//
// Note: the browser EventSource API cannot set an Authorization header, so the
// frontend must use a fetch-based SSE reader. A token is deliberately not
// accepted via query string, which would leak it into access logs and referrers.
func (a *app) boardEvents(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	boardID := r.PathValue("boardId")
	if !isUUID(boardID) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "boardId must be a UUID")
		return
	}
	// Confirm the board exists and is visible in this tenant before opening a
	// stream, so a bad id fails as a normal JSON error rather than a dead stream.
	if _, err := a.store.GetBoard(r.Context(), sc, boardID); err != nil {
		writeStoreError(w, err, "board not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "streaming unsupported")
		return
	}
	if a.hub == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable", "event hub not configured")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable proxy response buffering; without this events pool in the gateway
	// and arrive batched or not at all.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, cancel := a.hub.Subscribe(sc.OrgID, sc.TenantID, boardID)
	defer cancel()

	// Tell the client the stream is open before any event arrives, so it can
	// distinguish "connected and idle" from "still connecting".
	fmt.Fprintf(w, ": subscribed to board %s\n\n", boardID)
	flusher.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, open := <-events:
			if !open {
				return
			}
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, b); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

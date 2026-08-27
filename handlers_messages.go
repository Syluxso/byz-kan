package main

import (
	"net/http"
	"strings"
)

// CW-18 REST surface for the shared agent/human thread.

func (a *app) listBoardMessages(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	// ?all=1 includes ticket-scoped messages alongside the board thread.
	out, err := a.store.ListMessages(r.Context(), sc, ListMessagesParams{
		BoardID:  r.PathValue("boardId"),
		BoardAll: wantIncludeDeleted(r.URL.Query().Get("all")),
	})
	if err != nil {
		writeStoreError(w, err, "failed to list messages")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) listTicketMessages(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListMessages(r.Context(), sc, ListMessagesParams{TicketID: r.PathValue("id")})
	if err != nil {
		writeStoreError(w, err, "failed to list messages")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type messageBody struct {
	ActorType   string `json:"actorType"`
	ActorKey    string `json:"actorKey"`
	DisplayName string `json:"displayName"`
	Body        string `json:"body"`
}

func (a *app) createBoardMessage(w http.ResponseWriter, r *http.Request) {
	a.createMessage(w, r, r.PathValue("boardId"), "")
}

func (a *app) createTicketMessage(w http.ResponseWriter, r *http.Request) {
	a.createMessage(w, r, "", r.PathValue("id"))
}

func (a *app) createMessage(w http.ResponseWriter, r *http.Request, boardID, ticketID string) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body messageBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "body is required")
		return
	}
	// Fall back to the caller's own identity so a human posting from the UI
	// does not have to invent an actor key.
	if strings.TrimSpace(body.ActorKey) == "" {
		body.ActorKey = sc.ActorID
		if body.ActorType == "" {
			body.ActorType = "user"
		}
	}
	if strings.TrimSpace(body.DisplayName) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "displayName is required")
		return
	}
	out, err := a.store.CreateMessage(r.Context(), sc, boardID, ticketID,
		body.ActorType, body.ActorKey, body.DisplayName, body.Body)
	if err != nil {
		writeStoreError(w, err, "failed to create message")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) patchMessage(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.UpdateMessage(r.Context(), sc, r.PathValue("id"), body.Body)
	if err != nil {
		writeStoreError(w, err, "failed to update message")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) deleteMessage(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.SoftDeleteMessage(r.Context(), sc, r.PathValue("id")); err != nil {
		writeStoreError(w, err, "failed to delete message")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

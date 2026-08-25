package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

func (a *app) listTickets(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	out, err := a.store.ListTickets(r.Context(), sc, ListTicketsParams{
		BoardID:    r.PathValue("boardId"),
		StateID:    q.Get("stateId"),
		AssigneeID: q.Get("assignee"),
		TagID:      q.Get("tagId"),
		Q:          q.Get("q"),
	})
	if err != nil {
		log.Printf("list tickets: %v", err)
		writeStoreError(w, err, "failed to list tickets")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) listTenantTickets(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	out, err := a.store.ListTickets(r.Context(), sc, ListTicketsParams{
		BoardID:    q.Get("boardId"),
		StateID:    q.Get("stateId"),
		AssigneeID: q.Get("assignee"),
		TagID:      q.Get("tagId"),
		Q:          q.Get("q"),
	})
	if err != nil {
		log.Printf("list tenant tickets: %v", err)
		writeStoreError(w, err, "failed to list tickets")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createTicket(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		StateID         string          `json:"stateId"`
		ParentTicketID  string          `json:"parentTicketId"`
		Title           string          `json:"title"`
		Body            string          `json:"body"`
		CardData        json.RawMessage `json:"cardData"`
		TicketType      string          `json:"ticketType"`
		Priority        int             `json:"priority"`
		Position        int             `json:"position"`
		DueAt           *time.Time      `json:"dueAt"`
		EstimateMinutes *int            `json:"estimateMinutes"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "title is required")
		return
	}
	out, err := a.store.CreateTicket(r.Context(), sc, r.PathValue("boardId"), body.StateID, body.ParentTicketID,
		strings.TrimSpace(body.Title), body.Body, body.TicketType, body.Priority, body.Position, body.DueAt, body.EstimateMinutes, body.CardData)
	if err != nil {
		log.Printf("create ticket: %v", err)
		writeStoreError(w, err, "failed to create ticket")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) requireTicketOnBoard(w http.ResponseWriter, r *http.Request) (TicketView, bool) {
	sc, ok := a.sc(w, r)
	if !ok {
		return TicketView{}, false
	}
	tkt, err := a.store.GetTicketByID(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "ticket not found")
		return TicketView{}, false
	}
	if tkt.BoardID != r.PathValue("boardId") {
		writeProblem(w, http.StatusNotFound, "Not Found", "ticket not found")
		return TicketView{}, false
	}
	return tkt, true
}

func (a *app) getTicketOnBoard(w http.ResponseWriter, r *http.Request) {
	tkt, ok := a.requireTicketOnBoard(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, tkt)
}

func (a *app) patchTicketOnBoard(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireTicketOnBoard(w, r); !ok {
		return
	}
	a.patchTicket(w, r)
}

func (a *app) deleteTicketOnBoard(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireTicketOnBoard(w, r); !ok {
		return
	}
	a.deleteTicket(w, r)
}

func (a *app) getTicket(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.GetTicketByID(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "ticket not found")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) getTicketByKey(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.GetTicketByKey(r.Context(), sc, r.PathValue("key"))
	if err != nil {
		writeStoreError(w, err, "ticket not found")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) patchTicket(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Title           *string         `json:"title"`
		Body            *string         `json:"body"`
		TicketType      *string         `json:"ticketType"`
		Priority        *int            `json:"priority"`
		Position        *int            `json:"position"`
		EstimateMinutes *int            `json:"estimateMinutes"`
		ClearEstimate   bool            `json:"clearEstimateMinutes"`
		DueAt           *time.Time      `json:"dueAt"`
		ClearDueAt      bool            `json:"clearDueAt"`
		ParentTicketID  *string         `json:"parentTicketId"`
		ClearParent     bool            `json:"clearParentTicketId"`
		CardData        json.RawMessage `json:"cardData"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.UpdateTicket(r.Context(), sc, r.PathValue("id"), body.Title, body.Body, body.TicketType,
		body.Priority, body.Position, body.EstimateMinutes, body.ClearEstimate, body.DueAt, body.ClearDueAt,
		body.ParentTicketID, body.ClearParent, body.CardData)
	if err != nil {
		writeStoreError(w, err, "failed to update ticket")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) deleteTicket(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.SoftDeleteTicket(r.Context(), sc, r.PathValue("id")); err != nil {
		writeStoreError(w, err, "failed to delete ticket")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) moveTicket(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		StateID  string `json:"stateId"`
		Position *int   `json:"position"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !isUUID(body.StateID) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "stateId required")
		return
	}
	out, err := a.store.MoveTicket(r.Context(), sc, r.PathValue("id"), body.StateID, body.Position)
	if err != nil {
		writeStoreError(w, err, "failed to move ticket")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) ticketActivity(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListTicketActivity(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "failed to list activity")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) listAssignees(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListAssignees(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "failed to list assignees")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (a *app) replaceAssignees(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		UserIDs []string `json:"userIds"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.ReplaceAssignees(r.Context(), sc, r.PathValue("id"), body.UserIDs)
	if err != nil {
		writeStoreError(w, err, "failed to replace assignees")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) replaceWatchers(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		UserIDs []string `json:"userIds"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.ReplaceWatchers(r.Context(), sc, r.PathValue("id"), body.UserIDs)
	if err != nil {
		writeStoreError(w, err, "failed to replace watchers")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) addAssignee(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		UserID string `json:"userId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !isUUID(body.UserID) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "userId required")
		return
	}
	out, err := a.store.AddAssignee(r.Context(), sc, r.PathValue("id"), body.UserID)
	if err != nil {
		writeStoreError(w, err, "failed to add assignee")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (a *app) removeAssignee(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.RemoveAssignee(r.Context(), sc, r.PathValue("id"), r.PathValue("userId")); err != nil {
		writeStoreError(w, err, "failed to remove assignee")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *app) listWatchers(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListWatchers(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "failed to list watchers")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (a *app) addWatcher(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		UserID string `json:"userId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !isUUID(body.UserID) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "userId required")
		return
	}
	out, err := a.store.AddWatcher(r.Context(), sc, r.PathValue("id"), body.UserID)
	if err != nil {
		writeStoreError(w, err, "failed to add watcher")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (a *app) removeWatcher(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.RemoveWatcher(r.Context(), sc, r.PathValue("id"), r.PathValue("userId")); err != nil {
		writeStoreError(w, err, "failed to remove watcher")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

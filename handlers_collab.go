package main

import (
	"net/http"
	"strings"
	"time"
)

func (a *app) listTags(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListTags(r.Context(), sc, r.URL.Query().Get("kind"))
	if err != nil {
		writeStoreError(w, err, "failed to list tags")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createTag(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Name  string `json:"name"`
		Kind  string `json:"kind"`
		Color string `json:"color"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	out, err := a.store.CreateTag(r.Context(), sc, body.Name, body.Kind, body.Color)
	if err != nil {
		writeStoreError(w, err, "failed to create tag")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) patchTag(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Name  *string `json:"name"`
		Kind  *string `json:"kind"`
		Color *string `json:"color"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.UpdateTag(r.Context(), sc, r.PathValue("id"), body.Name, body.Kind, body.Color)
	if err != nil {
		writeStoreError(w, err, "failed to update tag")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) deleteTag(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.SoftDeleteTag(r.Context(), sc, r.PathValue("id")); err != nil {
		writeStoreError(w, err, "failed to delete tag")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) listTicketTags(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListTicketTags(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "failed to list ticket tags")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) addTicketTag(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.AddTicketTag(r.Context(), sc, r.PathValue("id"), r.PathValue("tagId")); err != nil {
		writeStoreError(w, err, "failed to add tag")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) removeTicketTag(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.RemoveTicketTag(r.Context(), sc, r.PathValue("id"), r.PathValue("tagId")); err != nil {
		writeStoreError(w, err, "failed to remove tag")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) listComments(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListComments(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "failed to list comments")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createComment(w http.ResponseWriter, r *http.Request) {
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
	if strings.TrimSpace(body.Body) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "body is required")
		return
	}
	out, err := a.store.CreateComment(r.Context(), sc, r.PathValue("id"), body.Body)
	if err != nil {
		writeStoreError(w, err, "failed to create comment")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) patchComment(w http.ResponseWriter, r *http.Request) {
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
	out, err := a.store.UpdateComment(r.Context(), sc, r.PathValue("id"), body.Body)
	if err != nil {
		writeStoreError(w, err, "failed to update comment")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) deleteComment(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.SoftDeleteComment(r.Context(), sc, r.PathValue("id")); err != nil {
		writeStoreError(w, err, "failed to delete comment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) listLinks(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListLinks(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "failed to list links")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createLink(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		URL      string `json:"url"`
		Title    string `json:"title"`
		LinkType string `json:"linkType"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.URL) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "url is required")
		return
	}
	out, err := a.store.CreateLink(r.Context(), sc, r.PathValue("id"), body.URL, body.Title, body.LinkType)
	if err != nil {
		writeStoreError(w, err, "failed to create link")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) patchLink(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		URL      *string `json:"url"`
		Title    *string `json:"title"`
		LinkType *string `json:"linkType"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.UpdateLink(r.Context(), sc, r.PathValue("id"), body.URL, body.Title, body.LinkType)
	if err != nil {
		writeStoreError(w, err, "failed to update link")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) deleteLink(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.SoftDeleteLink(r.Context(), sc, r.PathValue("id")); err != nil {
		writeStoreError(w, err, "failed to delete link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CW-19: attachments hang off a ticket, a board, or an agent message.
// The parent is taken from the route, so each path is unambiguous.

func (a *app) listTicketAttachments(w http.ResponseWriter, r *http.Request) {
	a.listAttachmentsFor(w, r, "ticket", r.PathValue("id"))
}

func (a *app) listBoardAttachments(w http.ResponseWriter, r *http.Request) {
	a.listAttachmentsFor(w, r, "board", r.PathValue("boardId"))
}

func (a *app) listMessageAttachments(w http.ResponseWriter, r *http.Request) {
	a.listAttachmentsFor(w, r, "message", r.PathValue("id"))
}

func (a *app) listAttachmentsFor(w http.ResponseWriter, r *http.Request, parentType, parentID string) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListAttachments(r.Context(), sc, parentType, parentID)
	if err != nil {
		writeStoreError(w, err, "failed to list attachments")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createTicketAttachment(w http.ResponseWriter, r *http.Request) {
	a.createAttachmentFor(w, r, "ticket", r.PathValue("id"))
}

func (a *app) createBoardAttachment(w http.ResponseWriter, r *http.Request) {
	a.createAttachmentFor(w, r, "board", r.PathValue("boardId"))
}

func (a *app) createMessageAttachment(w http.ResponseWriter, r *http.Request) {
	a.createAttachmentFor(w, r, "message", r.PathValue("id"))
}

func (a *app) createAttachmentFor(w http.ResponseWriter, r *http.Request, parentType, parentID string) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		FileID      string `json:"fileId"`
		Filename    string `json:"filename"`
		ContentType string `json:"contentType"`
		SizeBytes   *int64 `json:"sizeBytes"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !isUUID(body.FileID) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "fileId is required")
		return
	}
	out, err := a.store.CreateAttachment(r.Context(), sc, parentType, parentID,
		body.FileID, body.Filename, body.ContentType, body.SizeBytes)
	if err != nil {
		writeStoreError(w, err, "failed to create attachment")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.SoftDeleteAttachment(r.Context(), sc, r.PathValue("id")); err != nil {
		writeStoreError(w, err, "failed to delete attachment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) listChecklists(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListChecklists(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "failed to list checklists")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createChecklist(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Title    string `json:"title"`
		Position int    `json:"position"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "title is required")
		return
	}
	out, err := a.store.CreateChecklist(r.Context(), sc, r.PathValue("id"), body.Title, body.Position)
	if err != nil {
		writeStoreError(w, err, "failed to create checklist")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) patchChecklist(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Title    *string `json:"title"`
		Position *int    `json:"position"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.UpdateChecklist(r.Context(), sc, r.PathValue("id"), body.Title, body.Position)
	if err != nil {
		writeStoreError(w, err, "failed to update checklist")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) deleteChecklist(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.SoftDeleteChecklist(r.Context(), sc, r.PathValue("id")); err != nil {
		writeStoreError(w, err, "failed to delete checklist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) createChecklistItem(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Title    string `json:"title"`
		Position int    `json:"position"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "title is required")
		return
	}
	out, err := a.store.CreateChecklistItem(r.Context(), sc, r.PathValue("id"), body.Title, body.Position)
	if err != nil {
		writeStoreError(w, err, "failed to create checklist item")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) patchChecklistItem(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Title    *string `json:"title"`
		IsDone   *bool   `json:"isDone"`
		Position *int    `json:"position"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.UpdateChecklistItem(r.Context(), sc, r.PathValue("id"), body.Title, body.IsDone, body.Position)
	if err != nil {
		writeStoreError(w, err, "failed to update checklist item")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) deleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.SoftDeleteChecklistItem(r.Context(), sc, r.PathValue("id")); err != nil {
		writeStoreError(w, err, "failed to delete checklist item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) listTimeEntries(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListTimeEntries(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "failed to list time entries")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createTimeEntry(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		UserID    string     `json:"userId"`
		StartedAt *time.Time `json:"startedAt"`
		EndedAt   *time.Time `json:"endedAt"`
		Minutes   int        `json:"minutes"`
		Note      string     `json:"note"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.CreateTimeEntry(r.Context(), sc, r.PathValue("id"), body.UserID, body.StartedAt, body.EndedAt, body.Minutes, body.Note)
	if err != nil {
		writeStoreError(w, err, "failed to create time entry")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) patchTimeEntry(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		StartedAt *time.Time `json:"startedAt"`
		EndedAt   *time.Time `json:"endedAt"`
		Minutes   *int       `json:"minutes"`
		Note      *string    `json:"note"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.UpdateTimeEntry(r.Context(), sc, r.PathValue("id"), body.StartedAt, body.EndedAt, body.Minutes, body.Note)
	if err != nil {
		writeStoreError(w, err, "failed to update time entry")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) deleteTimeEntry(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.SoftDeleteTimeEntry(r.Context(), sc, r.PathValue("id")); err != nil {
		writeStoreError(w, err, "failed to delete time entry")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

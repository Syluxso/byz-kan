package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

func jsonDecoder(r *http.Request) *json.Decoder {
	return json.NewDecoder(r.Body)
}

func (a *app) listBoards(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var published *bool
	if v := strings.TrimSpace(r.URL.Query().Get("published")); v != "" {
		b := v == "true" || v == "1"
		published = &b
	}
	out, err := a.store.ListBoards(r.Context(), sc, published)
	if err != nil {
		log.Printf("list boards: %v", err)
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to list boards")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createBoard(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		KeyPrefix   string          `json:"keyPrefix"`
		IsPublished bool            `json:"isPublished"`
		CardSchema  json.RawMessage `json:"cardSchema"`
		Settings    json.RawMessage `json:"settings"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	out, err := a.store.CreateBoard(r.Context(), sc.OrgID, sc.TenantID, sc.ActorID, strings.TrimSpace(body.Name), body.Description, body.KeyPrefix, body.IsPublished, body.CardSchema, body.Settings)
	if err != nil {
		log.Printf("create board: %v", err)
		writeStoreError(w, err, "failed to create board")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) getBoard(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !isUUID(id) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid id")
		return
	}
	out, err := a.store.GetBoard(r.Context(), sc, id)
	if err != nil {
		writeStoreError(w, err, "board not found")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) patchBoard(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var body struct {
		Name        *string         `json:"name"`
		Description *string         `json:"description"`
		KeyPrefix   *string         `json:"keyPrefix"`
		IsPublished *bool           `json:"isPublished"`
		CardSchema  json.RawMessage `json:"cardSchema"`
		Settings    json.RawMessage `json:"settings"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.UpdateBoard(r.Context(), sc, id, body.Name, body.Description, body.IsPublished, body.CardSchema, body.Settings, body.KeyPrefix)
	if err != nil {
		writeStoreError(w, err, "failed to update board")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) deleteBoard(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.SoftDeleteBoard(r.Context(), sc, r.PathValue("id")); err != nil {
		writeStoreError(w, err, "failed to delete board")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) listStates(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListStates(r.Context(), sc, r.PathValue("boardId"))
	if err != nil {
		writeStoreError(w, err, "failed to list states")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createState(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Name      string `json:"name"`
		Position  int    `json:"position"`
		IsDefault bool   `json:"isDefault"`
		WIPLimit  *int   `json:"wipLimit"`
		Color     string `json:"color"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	out, err := a.store.CreateState(r.Context(), sc, r.PathValue("boardId"), strings.TrimSpace(body.Name), body.Color, body.Position, body.IsDefault, body.WIPLimit)
	if err != nil {
		writeStoreError(w, err, "failed to create state")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) patchState(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		Name      *string `json:"name"`
		Position  *int    `json:"position"`
		IsDefault *bool   `json:"isDefault"`
		WIPLimit  *int    `json:"wipLimit"`
		ClearWIP  bool    `json:"clearWipLimit"`
		Color     *string `json:"color"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := a.store.UpdateState(r.Context(), sc, r.PathValue("id"), body.Name, body.Position, body.IsDefault, body.WIPLimit, body.ClearWIP, body.Color)
	if err != nil {
		writeStoreError(w, err, "failed to update state")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) deleteState(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	if err := a.store.SoftDeleteState(r.Context(), sc, r.PathValue("id"), force); err != nil {
		writeStoreError(w, err, "failed to delete state")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) reorderStates(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := a.store.ReorderStates(r.Context(), sc, r.PathValue("boardId"), body.IDs); err != nil {
		writeStoreError(w, err, "failed to reorder states")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *app) listMembers(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListMembers(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "failed to list members")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) addMember(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	var body struct {
		UserID string `json:"userId"`
		Role   string `json:"role"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !isUUID(body.UserID) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "userId required")
		return
	}
	out, err := a.store.AddMember(r.Context(), sc, r.PathValue("id"), body.UserID, body.Role)
	if err != nil {
		writeStoreError(w, err, "failed to add member")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *app) removeMember(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	if err := a.store.RemoveMember(r.Context(), sc, r.PathValue("id"), r.PathValue("userId")); err != nil {
		writeStoreError(w, err, "failed to remove member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) boardActivity(w http.ResponseWriter, r *http.Request) {
	sc, ok := a.sc(w, r)
	if !ok {
		return
	}
	out, err := a.store.ListBoardActivity(r.Context(), sc, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "failed to list activity")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

package main

import (
	"encoding/json"
	"net/http"
)

type problem struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	writeJSON(w, status, problem{Title: title, Detail: detail, Status: status})
}

func writeStoreError(w http.ResponseWriter, err error, fallback string) {
	switch err {
	case errNotFound:
		writeProblem(w, http.StatusNotFound, "Not Found", fallback)
	case errConflict:
		writeProblem(w, http.StatusConflict, "Conflict", fallback)
	case errInvalid:
		writeProblem(w, http.StatusBadRequest, "Bad Request", fallback)
	case errPrefixLocked:
		writeProblem(w, http.StatusConflict, "Conflict", "keyPrefix cannot change after tickets exist")
	case errHasTickets:
		writeProblem(w, http.StatusConflict, "Conflict", "state still has tickets; pass force=1 to move them to default")
	default:
		if isUniqueViolation(err) {
			writeProblem(w, http.StatusConflict, "Conflict", fallback)
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", fallback)
	}
}

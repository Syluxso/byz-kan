package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

func writeUnauthorized(w http.ResponseWriter, publicURL, detail string) {
	meta := strings.TrimRight(publicURL, "/") + "/.well-known/oauth-protected-resource"
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="byz-kan", resource_metadata=%q`, meta))
	writeProblem(w, http.StatusUnauthorized, "Unauthorized", detail)
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

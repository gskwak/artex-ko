package server

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

// blueSearchQuery bounds the literal substring passed to list searches. The
// database performs the search before LIMIT/OFFSET on every page.
func blueSearchQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if !utf8.ValidString(query) {
		writeErr(w, http.StatusBadRequest, "search query must be UTF-8")
		return "", false
	}
	if utf8.RuneCountInString(query) > 200 {
		writeErr(w, http.StatusBadRequest, "search query is too long")
		return "", false
	}
	return query, true
}

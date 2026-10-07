package api

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
)

// safeIDRe validates path parameters that map to filesystem directories.
var safeIDRe = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)

// safeLongIDRe allows the timestamp-prefixed run IDs (e.g. 20240115T143022_kv_main).
var safeLongIDRe = regexp.MustCompile(`^[a-zA-Z0-9\-_T]+$`)

// chiURLParam wraps chi.URLParam for testability.
func chiURLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}

// writeJSON writes v as JSON with status code to w.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON decodes the request body into v. Returns false and writes 400 on error.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

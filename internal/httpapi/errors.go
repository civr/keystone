package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// maxJSONBody bounds request bodies decoded by readJSON.
const maxJSONBody = 64 << 10

// readJSON decodes the request body into v, rejecting unknown fields and
// oversized bodies. On failure it writes a 400 and returns false.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return readJSONLimit(w, r, v, maxJSONBody)
}

// readJSONLimit is readJSON with an explicit body limit in bytes.
func readJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "The request is invalid.")
		return false
	}
	return true
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("http: write response", "err", err)
	}
}

// writeError writes {"error":code,"message":message}; message is English, for the UI.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func errUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "unavailable", "Keystone is temporarily unavailable. Try again later.")
}

func errNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "Not found.")
}

func errMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "This method is not allowed here.")
}

// writeInvalid writes a 400 {"error":"invalid","field":...,"message":...}.
func writeInvalid(w http.ResponseWriter, field, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": field, "message": message})
}

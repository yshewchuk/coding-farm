// Package api implements the HTTP handlers and routing for the Management API.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cloudsandbox/platform/internal/auth"
	"github.com/cloudsandbox/platform/internal/db"
	"github.com/cloudsandbox/platform/internal/service"
)

// Validation errors shared by handlers.
var (
	errNameRequired        = errors.New("name is required")
	errDockerfileRequired  = errors.New("dockerfile_contents is required")
	errTemplateIDRequired  = errors.New("template_id is required")
)

// writeJSON encodes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// errorResponse is the canonical error envelope returned by all handlers.
type errorResponse struct {
	Error string `json:"error"`
}

// writeError writes an error envelope. The HTTP status is derived from the
// concrete error type so handlers can simply propagate domain errors.
func writeError(w http.ResponseWriter, err error) {
	writeErrorStatus(w, mapError(err), err)
}

func writeErrorStatus(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorResponse{Error: err.Error()})
}

// mapError translates domain/repository errors into HTTP status codes.
func mapError(err error) int {
	switch {
	case errors.Is(err, db.ErrNotFound),
		errors.Is(err, service.ErrSessionNotFound),
		errors.Is(err, service.ErrTemplateNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrTemplateNotReady):
		return http.StatusConflict
	case errors.Is(err, auth.ErrInvalidToken):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// decodeJSON decodes a JSON request body into dst. Empty or malformed bodies
// produce a 400.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeErrorStatus(w, http.StatusBadRequest, errors.New("request body required"))
		return false
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, errors.New("invalid json body"))
		return false
	}
	return true
}

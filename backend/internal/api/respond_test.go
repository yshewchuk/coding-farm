package api

import (
	"net/http"
	"testing"

	"github.com/cloudsandbox/platform/internal/auth"
)

func TestErrors_AreJSON(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodGet, "/api/templates/nope", nil, testToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	var e errorResponse
	decode(t, rr, &e)
	if e.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestMapError_ErrInvalidToken(t *testing.T) {
	if got := mapError(auth.ErrInvalidToken); got != http.StatusUnauthorized {
		t.Errorf("mapError(ErrInvalidToken) = %d, want 401", got)
	}
}

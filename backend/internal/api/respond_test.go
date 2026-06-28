package api

import (
	"net/http"
	"testing"
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

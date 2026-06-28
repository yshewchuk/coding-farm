package api

import (
	"net/http"
	"testing"
)

func TestHealth_NoAuth(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodGet, "/health", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

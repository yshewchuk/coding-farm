package api

import (
	"net/http"
	"testing"
)

func TestMeHandler(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodGet, "/api/me", nil, testToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	decode(t, rr, &resp)
	if resp["user_id"] != "user-uuid-1" {
		t.Errorf("user_id = %v", resp["user_id"])
	}
	if resp["org_id"] != "org-1" {
		t.Errorf("org_id = %v", resp["org_id"])
	}
}

func TestMeHandler_NoToken(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodGet, "/api/me", nil, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestMeHandler_BadToken(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodGet, "/api/me", nil, "nope")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

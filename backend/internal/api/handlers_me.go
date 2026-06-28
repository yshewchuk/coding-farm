package api

import (
	"net/http"

	"github.com/cloudsandbox/platform/internal/auth"
)

// me returns the authenticated, provisioned identity. Because the provisioning
// middleware runs before this handler, this is also the implicit "sign-up"
// endpoint: the first authenticated request creates the user + personal org.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	memberships, err := s.orgs.ListMemberships(r.Context(), identity.UserID)
	if err != nil {
		writeError(w, err)
		return
	}
	orgs := make([]struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}, 0, len(memberships))
	for _, m := range memberships {
		orgs = append(orgs, struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		}{ID: m.OrgID, Role: string(m.Role)})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": identity.UserID,
		"email":   identity.Email,
		"org_id":  identity.OrgID,
		"orgs":    orgs,
	})
}

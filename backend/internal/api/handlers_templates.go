package api

import (
	"net/http"

	"github.com/cloudsandbox/platform/internal/auth"
	"github.com/cloudsandbox/platform/internal/models"
	"github.com/go-chi/chi/v5"
)

// listTemplates returns every template owned by the caller's organization.
func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	templates, err := s.templates.List(r.Context(), identity.OrgID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}

// createTemplate stores a new Dockerfile template for the caller's org.
func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	var in models.CreateTemplateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Name == "" {
		writeErrorStatus(w, http.StatusBadRequest, errNameRequired)
		return
	}
	if in.DockerfileContents == "" {
		writeErrorStatus(w, http.StatusBadRequest, errDockerfileRequired)
		return
	}
	t, err := s.templates.Create(r.Context(), identity.OrgID, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// getTemplate returns a single template scoped to the caller's org. A template
// owned by another org surfaces as 404, which is the authorization boundary.
func (s *Server) getTemplate(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	t, err := s.templates.Get(r.Context(), identity.OrgID, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// deleteTemplate removes a template. Sessions referencing it cascade-delete.
func (s *Server) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.templates.Delete(r.Context(), identity.OrgID, id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// buildTemplate triggers an image build for the template via the Fly Apps API.
func (s *Server) buildTemplate(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	t, err := s.orch.BuildTemplate(r.Context(), identity.OrgID, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

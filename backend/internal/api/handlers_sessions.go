package api

import (
	"errors"
	"net/http"

	"github.com/cloudsandbox/platform/internal/auth"
	"github.com/cloudsandbox/platform/internal/models"
	"github.com/cloudsandbox/platform/internal/service"
	"github.com/go-chi/chi/v5"
)

// listSessions returns the caller's sessions within their org.
func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	sessions, err := s.sessions.List(r.Context(), identity.OrgID, identity.UserID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// createSession provisions a new workspace from a template.
func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	var in models.CreateSessionInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.TemplateID == "" {
		writeErrorStatus(w, http.StatusBadRequest, errTemplateIDRequired)
		return
	}
	sess, err := s.orch.CreateSession(r.Context(), identity.OrgID, identity.UserID, in.TemplateID, in.Name)
	if err != nil {
		if errors.Is(err, service.ErrTemplateNotReady) {
			writeErrorStatus(w, http.StatusConflict, err)
			return
		}
		if errors.Is(err, service.ErrTemplateNotFound) {
			writeErrorStatus(w, http.StatusNotFound, err)
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

// getSession returns a single session scoped to the caller's org.
func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	sess, err := s.sessions.Get(r.Context(), identity.OrgID, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// resumeSession starts a suspended workspace.
func (s *Server) resumeSession(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	sess, err := s.orch.ResumeSession(r.Context(), identity.OrgID, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// hibernateSession stops a running workspace, scaling it to zero.
func (s *Server) hibernateSession(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	sess, err := s.orch.HibernateSession(r.Context(), identity.OrgID, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// deleteSession destroys the machine + volume and removes the session record.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.RequireIdentity(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.orch.DeleteSession(r.Context(), identity.OrgID, id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

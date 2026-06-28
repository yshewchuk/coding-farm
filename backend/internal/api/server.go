package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/cloudsandbox/platform/internal/auth"
	"github.com/cloudsandbox/platform/internal/config"
	"github.com/cloudsandbox/platform/internal/models"
	"github.com/cloudsandbox/platform/internal/service"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// templateStore is the subset of db.TemplatesRepo the handlers use. Defining it
// as an interface lets handlers be tested with an in-memory store.
type templateStore interface {
	List(ctx context.Context, orgID string) ([]models.Template, error)
	Create(ctx context.Context, orgID string, in models.CreateTemplateInput) (*models.Template, error)
	Get(ctx context.Context, orgID, id string) (*models.Template, error)
	Delete(ctx context.Context, orgID, id string) error
}

// sessionStore is the subset of db.SessionsRepo the handlers use directly.
type sessionStore interface {
	List(ctx context.Context, orgID, userID string) ([]models.Session, error)
	Get(ctx context.Context, orgID, id string) (*models.Session, error)
}

// Server bundles the dependencies every handler needs. One Server is created
// at process start and shared across all requests.
type Server struct {
	cfg       config.Config
	orgs      auth.Provisioner
	templates templateStore
	sessions  sessionStore
	orch      *service.Orchestrator
	verifier  auth.Verifier
}

// NewServer constructs a Server from its dependencies. In production the concrete
// db repos are passed in; tests pass fakes that satisfy the same interfaces.
func NewServer(cfg config.Config, orgs auth.Provisioner, templates templateStore, sessions sessionStore, orch *service.Orchestrator, verifier auth.Verifier) *Server {
	return &Server{
		cfg:       cfg,
		orgs:      orgs,
		templates: templates,
		sessions:  sessions,
		orch:      orch,
		verifier:  verifier,
	}
}

// Router builds the full HTTP handler tree. Public routes (health) sit outside
// the auth+provisioning group; everything under /api is authenticated.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(chimw.StripSlashes)
	r.Use(corsMiddleware(s.cfg.FrontendURL))

	r.Get("/health", s.health)

	r.Route("/api", func(r chi.Router) {
		r.Use(auth.AuthMiddleware(s.verifier))
		r.Use(auth.ProvisioningMiddleware(s.orgs))

		// Identity / self-hosted admin provisioning.
		r.Get("/me", s.me)

		// Templates (Dockerfile definitions + image builds).
		r.Get("/templates", s.listTemplates)
		r.Post("/templates", s.createTemplate)
		r.Get("/templates/{id}", s.getTemplate)
		r.Delete("/templates/{id}", s.deleteTemplate)
		r.Post("/templates/{id}/build", s.buildTemplate)

		// Sessions (workspace lifecycle).
		r.Get("/sessions", s.listSessions)
		r.Post("/sessions", s.createSession)
		r.Get("/sessions/{id}", s.getSession)
		r.Post("/sessions/{id}/resume", s.resumeSession)
		r.Post("/sessions/{id}/hibernate", s.hibernateSession)
		r.Delete("/sessions/{id}", s.deleteSession)
	})

	return r
}

// health is a liveness probe that does not require authentication.
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// corsMiddleware allows the configured frontend origin to call the API with
// credentials (the Authorization header) from the browser.
func corsMiddleware(frontendURL string) func(http.Handler) http.Handler {
	allowed := strings.TrimSpace(frontendURL)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if allowed != "" && (origin == allowed || allowed == "*") {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

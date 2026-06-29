// Package main is the entry point for the Cloud Sandbox Management API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudsandbox/platform/internal/api"
	"github.com/cloudsandbox/platform/internal/auth"
	"github.com/cloudsandbox/platform/internal/config"
	"github.com/cloudsandbox/platform/internal/db"
	"github.com/cloudsandbox/platform/internal/fly"
	"github.com/cloudsandbox/platform/internal/service"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.Info("config loaded", "region", cfg.FlyRegion, "domain", cfg.LogtoDomain)

	// --- Database (Neon Postgres master pool) ---
	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Migrate(ctx); err != nil {
		return err
	}
	slog.Info("database migrated")

	orgs := db.NewOrgsRepo(pool)
	templates := db.NewTemplatesRepo(pool)
	sessions := db.NewSessionsRepo(pool)

	// --- Fly.io clients ---
	flyClient := fly.NewClient(cfg.FlyAPIBaseURL, "https://api.fly.io", cfg.FlyAPIToken, cfg.FlyOrg, nil)

	// --- Auth (Logto JWKS verifier) ---
	verifier, err := auth.NewJWKSVerifier(ctx, cfg.JWKSURL(), cfg.OIDCIssuer(), cfg.LogtoAudience, cfg.LogtoOrgClaim, cfg.JWKSRefreshInterval)
	if err != nil {
		return err
	}
	slog.Info("jwks fetched", "url", cfg.JWKSURL())

	// --- Orchestration ---
	orch := service.NewOrchestrator(templates, sessions, flyClient, flyClient, cfg.FlyRegion, cfg.WorkspacePort, cfg.DefaultImageRef, "fly.dev")

	// --- HTTP server ---
	srv := api.NewServer(cfg, orgs, templates, sessions, orch, verifier)
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		slog.Info("http server starting", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

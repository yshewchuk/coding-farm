.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Cloud Sandbox — self-hosting tasks\n\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	/^[a-zA-Z0-9_-]+:.*##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ----------------------------------------------------------------------------
# Variables
# ----------------------------------------------------------------------------
GO            ?= go
# Used only by the db-migrate targets below; deploy.sh reads DATABASE_URL itself.
NEON_DB_URL   ?= $(DATABASE_URL)
# Deploy + Fly config (FLY_ORG, LOGTO_*, FRONTEND_URL, FLY_APP, ...) live in the
# environment or .env and are consumed by scripts/deploy.sh — make does NOT load
# .env, so the canonical deploy path is `make fly` (= ./scripts/deploy.sh fly),
# which loads .env, derives a Fly token from `fly auth login`, and is idempotent.

# ----------------------------------------------------------------------------
# Local development
# ----------------------------------------------------------------------------
.PHONY: backend-build
backend-build: ## Build the Go backend binary
	cd backend && $(GO) build -o bin/api ./cmd/api

.PHONY: backend-test
backend-test: ## Run Go unit tests
	cd backend && $(GO) test ./...

.PHONY: backend-vet
backend-vet: ## Vet the Go backend
	cd backend && $(GO) vet ./...

.PHONY: backend-run
backend-run: ## Run the Management API locally (loads backend/.env)
	cd backend && $(GO) run ./cmd/api

.PHONY: frontend-install
frontend-install: ## Install frontend deps
	cd frontend && npm install

.PHONY: frontend-dev
frontend-dev: ## Start the Vite dev server
	cd frontend && npm run dev

.PHONY: frontend-build
frontend-build: ## Build the production frontend bundle
	cd frontend && npm run build

.PHONY: dev
dev: ## Run backend + frontend together (requires tmux or two terminals)
	@echo "In terminal 1: make backend-run"
	@echo "In terminal 2: make frontend-dev"

# ----------------------------------------------------------------------------
# Database (Neon Postgres)
# ----------------------------------------------------------------------------
.PHONY: db-migrate
db-migrate: ## Apply schema migrations to the configured DATABASE_URL
	cd backend && DATABASE_URL="$(NEON_DB_URL)" $(GO) run ./cmd/api --migrate-only

# The migrations also run automatically on API boot, so this target is a
# convenience for applying them without starting the HTTP server. If you prefer
# psql, apply the SQL file directly:
.PHONY: db-migrate-psql
db-migrate-psql: ## Apply schema via psql (requires psql installed)
	PGPASSWORD=$$(echo "$(NEON_DB_URL)" | sed -n 's|.*:\([^@]*\)@.*|\1|p') \
	psql "$(NEON_DB_URL)" -f backend/internal/db/migrations/001_init.sql

# ----------------------------------------------------------------------------
# Logto setup checklist
# ----------------------------------------------------------------------------
.PHONY: logto-checklist
logto-checklist: ## Print the Logto setup steps (M2M seed, then logto-setup)
	@echo "Logto setup:"
	@echo "  1. In the Logto console, create a 'Machine-to-machine' application."
	@echo "  2. Assign it the built-in 'Logto Management API' role (scope: all)."
	@echo "  3. Put its App ID + App Secret in LOGTO_M2M_APP_ID / LOGTO_M2M_APP_SECRET."
	@echo "  4. Run: ./scripts/deploy.sh logto-setup"
	@echo "     (creates the SPA app + API resource, writes LOGTO_APP_ID to .env)."
	@echo "  5. Set the backend env: LOGTO_ISSUER, LOGTO_AUDIENCE."
	@echo "     The frontend build reads LOGTO_APP_ID automatically."
	@echo "  6. (Multi-tenant) Create Organizations + members by hand in the console."

# ----------------------------------------------------------------------------
# Fly.io deployment
# ----------------------------------------------------------------------------
# The canonical, idempotent deploy path is scripts/deploy.sh fly: it loads .env,
# creates the Fly app if missing, derives a Fly API token from `fly auth login`
# when FLY_API_TOKEN is unset, sets all secrets, and deploys (migrations run on
# boot). The old granular fly-app / fly-secrets / fly-deploy targets were a
# thin, non-idempotent, non-.env-loading duplicate of the same flow and failed
# on Windows (native make can't exec the `fly` .cmd shim). They now delegate.
.PHONY: fly
fly: ## Create + secret + deploy the Management API to Fly.io (idempotent)
	./scripts/deploy.sh fly

.PHONY: deploy
deploy: ## Full flow: neon (if needed) -> logto -> fly -> web
	./scripts/deploy.sh all

.PHONY: web
web: ## Deploy the frontend as a static-host Fly app (nginx + SPA fallback)
	./scripts/deploy.sh web

.PHONY: fly-app
fly-app: ## (Deprecated) alias for `make fly` — the script creates the app if missing
	@echo ">> 'make fly-app' is deprecated; use 'make fly' (idempotent, loads .env)."
	./scripts/deploy.sh fly

.PHONY: fly-secrets
fly-secrets: ## (Deprecated) alias for `make fly`
	@echo ">> 'make fly-secrets' is deprecated; use 'make fly'."
	./scripts/deploy.sh fly

.PHONY: fly-deploy
fly-deploy: ## (Deprecated) alias for `make fly`
	@echo ">> 'make fly-deploy' is deprecated; use 'make fly'."
	./scripts/deploy.sh fly

# ----------------------------------------------------------------------------
# Reference workspace image
# ----------------------------------------------------------------------------
.PHONY: docker-build-workspace
docker-build-workspace: ## Build the sample code-server workspace image locally
	docker build -f docker/Dockerfile.codeserver-workspace -t codeserver-workspace .

.PHONY: docker-run-workspace
docker-run-workspace: ## Run the sample workspace image locally on :8080
	docker run --rm -p 8080:8080 -v $(PWD)/workspace:/workspace codeserver-workspace

# ----------------------------------------------------------------------------
# First-time self-host bootstrap (read the README for details)
# ----------------------------------------------------------------------------
.PHONY: bootstrap
bootstrap: ## Print the end-to-end self-host bootstrap steps
	@echo "Self-host bootstrap (one command: ./scripts/deploy.sh all):"
	@echo "  1. fly auth login; fly orgs list  (or: fly orgs create <name>) -> FLY_ORG"
	@echo "  2. cp .env.example .env; fill FLY_ORG, LOGTO_ISSUER, LOGTO_AUDIENCE,"
	@echo "     FRONTEND_URL (leave DATABASE_URL blank to auto-create via neonctl)."
	@echo "  3. Seed one Logto M2M app (console) -> LOGTO_M2M_APP_ID/SECRET in .env."
	@echo "  4. make fly                # = ./scripts/deploy.sh fly: create+secret+deploy API"
	@echo "     make web                  # = ./scripts/deploy.sh web: static-host Fly app"
	@echo "     (or) ./scripts/deploy.sh all   # neon -> logto -> fly -> web"
	@echo "  5. (Optional) point a custom domain at the web app; add to Logto redirects."
	@echo "  6. Sign in, create a template, build it, launch a workspace, Open IDE."

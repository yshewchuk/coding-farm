.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Cloud Sandbox — self-hosting tasks\n\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	/^[a-zA-Z0-9_-]+:.*##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ----------------------------------------------------------------------------
# Variables — override on the command line, e.g. `make fly-deploy FLY_APP=my-api`
# ----------------------------------------------------------------------------
GO            ?= go
FLY_CLI       ?= fly
NEON_DB_URL   ?= $(DATABASE_URL)
FLY_APP       ?= cloudsandbox-api
FLY_ORG       ?= $(FLY_ORG)
FLY_REGION    ?= iad
LOGTO_ISSUER  ?= $(LOGTO_ISSUER)

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
logto-checklist: ## Print the Logto setup checklist
	@echo "Logto setup (perform in the Logto console):"
	@echo "  1. Create an Application of type 'Traditional Web' / 'SPA'."
	@echo "     - Redirect URI:        https://<frontend-origin>/callback"
	@echo "     - Post sign-out URI:    https://<frontend-origin>/"
	@echo "  2. Create an API Resource (audience), e.g. https://api.cloudsandbox"
	@echo "     and grant the application access to it."
	@echo "  3. (Multi-tenant) Create Organizations; add users as members."
	@echo "  4. Record the following for the backend:"
	@echo "       LOGTO_ISSUER=<logto origin>"
	@echo "       LOGTO_AUDIENCE=<resource indicator>"
	@echo "     and for the frontend:"
	@echo "       VITE_LOGTO_ENDPOINT=<logto origin>"
	@echo "       VITE_LOGTO_APP_ID=<app id>"
	@echo "       VITE_LOGTO_RESOURCE=<resource indicator>"

# ----------------------------------------------------------------------------
# Fly.io deployment
# ----------------------------------------------------------------------------
.PHONY: fly-deploy
fly-deploy: ## Deploy the Management API to Fly.io
	$(FLY_CLI) deploy backend --app $(FLY_APP) --remote-only

.PHONY: fly-secrets
fly-secrets: ## Set required secrets on the Fly app (interactive; fill env first)
	$(FLY_CLI) secrets set \
	  --app $(FLY_APP) \
	  DATABASE_URL="$(NEON_DB_URL)" \
	  FLY_API_TOKEN="$(FLY_API_TOKEN)" \
	  FLY_ORG="$(FLY_ORG)" \
	  LOGTO_ISSUER="$(LOGTO_ISSUER)" \
	  FRONTEND_URL="$(FRONTEND_URL)" \
	  WORKSPACE_PORT=8080

.PHONY: fly-app
fly-app: ## Create the Fly app for the Management API (run once)
	$(FLY_CLI) apps create $(FLY_APP) --org $(FLY_ORG)

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
	@echo "Self-host bootstrap:"
	@echo "  1. Provision Neon Postgres; export DATABASE_URL."
	@echo "  2. Deploy Logto (or use Logto Cloud); see: make logto-checklist"
	@echo "  3. make fly-app          # create the API Fly app"
	@echo "  4. make fly-secrets       # set secrets (DATABASE_URL, FLY_API_TOKEN, ...)"
	@echo "  5. make fly-deploy        # deploy the API (runs migrations on boot)"
	@echo "  6. Deploy the frontend (any static host); set VITE_* env vars."
	@echo "  7. Sign in, create a template, build it, launch a workspace, Open IDE."

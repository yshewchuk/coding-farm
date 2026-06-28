# AGENTS.md — build & test commands for the Cloud Sandbox platform

This file documents the canonical commands to build, test, and validate this
repository. Run these before considering any change complete.

## Toolchain

- **Go:** 1.23+ (the CI image and `backend/Dockerfile` pin 1.23).
- **Node:** 20+ (frontend tooling).
- A local Go toolchain is not required for frontend-only work, and vice versa.

## Backend (Go Management API)

All commands run from `backend/` unless noted. The `make` targets wrap these
with the correct working directory.

```bash
# Build everything (compiles cmd/api + all internal packages).
go build ./...

# Run unit tests (mocks the Fly Machines API and Logto JWT verification).
go test ./...

# Static analysis.
go vet ./...

# Run the API locally (loads backend/.env; binds :8080; runs migrations on boot).
go run ./cmd/api
```

### Notes
- Dependencies are vendored on demand via `go mod tidy`; the module is
  `github.com/cloudsandbox/platform`.
- Migrations are embedded in the binary (`internal/db/migrations/*.sql`) and
  applied automatically at startup via `pool.Migrate(ctx)`.
- Tests do **not** require a database or network: the Fly client and JWT
  verifier are exercised against in-process fakes / a test JWKS HTTP server.

## Frontend (React + Vite)

All commands run from `frontend/`.

```bash
npm install        # install dependencies
npm run dev         # Vite dev server on :5173 (proxies /api -> :8080)
npm run build       # production bundle to frontend/dist
npm run lint        # ESLint
```

## Docker (reference workspace image)

```bash
docker build -f docker/Dockerfile.codeserver-workspace -t codeserver-workspace .
docker run --rm -p 8080:8080 -v "$PWD/workspace:/workspace" codeserver-workspace
```

## Deployment (self-hosting)

The platform is deployed with one CLI helper script — no Terraform. The
infra surface is small (one Neon project, one Logto app, one Fly app), and the
official Fly Terraform provider is archived, so a shell script keeps the deploy
story in one auditable, idempotent place. See `docs/DEPLOYMENT.md` for the full
narrative.

### Prerequisites (CLIs only)
- `fly` (flyctl), authenticated: `fly auth login`.
- `jq` (idempotency checks in the script).
- `npm`/Node 20+ (frontend build).
- `neonctl` **optional** — only if you want the script to create the Neon
  project (`npm i -g neonctl && neonctl auth`). Otherwise supply `DATABASE_URL`
  from the Neon console.

### One-time setup
- Create a `.env` at the repo root from `.env.example` and fill in `FLY_ORG`,
  `FLY_API_TOKEN`, `LOGTO_ISSUER`, `LOGTO_AUDIENCE`, `FRONTEND_URL`,
  `LOGTO_APP_ID` (leave `DATABASE_URL` blank to auto-create via `neonctl`).
- Do the one-time Logto console setup (SPA app + API resource) the script
  prints a checklist for (`./scripts/deploy.sh logto`).

### Deploy commands
```bash
./scripts/deploy.sh all           # neon (if needed) -> fly -> frontend build
./scripts/deploy.sh preflight     # validate env + tools
./scripts/deploy.sh neon-create   # create a Neon project (sets DATABASE_URL)
./scripts/deploy.sh logto         # print the one-time Logto console checklist
./scripts/deploy.sh fly            # create/secret/deploy the Management API
./scripts/deploy.sh frontend      # build frontend/dist/ with env baked in
make deploy                         # = ./scripts/deploy.sh all
```

### Notes
- The Management API app is the only Fly app you deploy. Each workspace session
  is a **separate** Fly app/machine provisioned on demand by the API at runtime
  via the Fly Machines REST API.
- Migrations run automatically on boot (`internal/db.Migrate`); the backend logs
  `database migrated` then `http server starting`.
- The script is idempotent: `all` can be re-run safely after fixing a failing
  step; existing resources (Fly app, Neon project) are not recreated.

## Verification checklist before committing

1. `make backend-vet` — passes clean.
2. `make backend-test` — all packages `ok`.
3. `make backend-build` — binary builds.
4. `make frontend-build` — bundle builds.

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

## Verification checklist before committing

1. `make backend-vet` — passes clean.
2. `make backend-test` — all packages `ok`.
3. `make backend-build` — binary builds.
4. `make frontend-build` — bundle builds.

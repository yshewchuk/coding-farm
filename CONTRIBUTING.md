# Contributing to Cloud Sandbox

First off — thank you for considering a contribution. Cloud Sandbox is an
open-source, self-hosted platform for disposable development environments
running on **Fly.io Firecracker microVMs**, with **Neon Serverless Postgres**
for state and **Logto** for authentication. Community contributions help keep it
vendor-neutral, lightweight, and useful across self-hosted infrastructure.

This guide covers:

1. Setting up the project locally.
2. The architecture of the platform and each component.
3. How to contribute to each layer (backend, frontend, workspace images, ops).

> **Direction note:** This project pivoted from an earlier "multi-repo AI agent
> platform with an in-container Workspace Daemon and harness abstraction"
> design to a leaner, concrete implementation on Fly.io + Neon + Logto. The
> documentation below describes what is actually in the repository. The earlier
> daemon/harness ideas are captured as a future direction in
> [`agents.md`](agents.md) under "Future evolution".

---

## Table of Contents
- [Code of Conduct](#code-of-conduct)
- [Repository Layout](#repository-layout)
- [Development Prerequisites](#development-prerequisites)
- [Local Setup](#local-setup)
- [Architecture Overview](#architecture-overview)
- [Contributing by Layer](#contributing-by-layer)
  - [Backend (Go Management API)](#backend-go-management-api)
  - [Frontend (React + Vite)](#frontend-react--vite)
  - [Workspace Images (Docker)](#workspace-images-docker)
  - [Ops & Deployment](#ops--deployment)
- [Agent / IDE Model](#agent--ide-model)
- [Testing](#testing)
- [Coding Standards](#coding-standards)
- [Pull Request Process](#pull-request-process)
- [Release Process](#release-process)

---

## Code of Conduct
Participation in this project is governed by the project's Code of Conduct. By
participating you agree to abide by its terms. Please report unacceptable
behavior to the maintainers via the security contact listed in the repository.

---

## Repository Layout

The repository is organized by the three deployable concerns: the control
plane, its UI, and the workspace image recipes.

```
.
├── README.md                  # Project overview, quick start, self-hosting
├── CONTRIBUTING.md            # This file
├── AGENTS.md                  # Canonical build & test commands (machine + human)
├── agents.md                  # Agent / IDE integration model + future direction
├── Makefile                   # Self-hosting + dev task targets
├── docs/
│   └── ARCHITECTURE.md        # Component & request-flow design doc
├── backend/                   # Go Management API (the control plane)
│   ├── cmd/api/               # HTTP server entry point
│   ├── internal/
│   │   ├── config/            # Env-based configuration
│   │   ├── db/                # pgx pool, embedded migrations, repositories
│   │   ├── auth/              # Logto JWT verification + identity injection
│   │   ├── fly/               # Fly Machines + Apps REST API clients
│   │   ├── service/           # Workspace orchestration (the business logic)
│   │   ├── api/               # HTTP handlers, routing, error mapping
│   │   └── models/            # Shared domain types
│   ├── migrations -> internal/db/migrations  # (migrations live under internal/db)
│   ├── fly.toml               # Fly config for the Management API
│   ├── Dockerfile             # Multi-stage build (distroless runtime)
│   └── go.mod
├── frontend/                  # React + Vite management UI
│   ├── src/
│   │   ├── components/        # AuthScreen, Callback, Dashboard, Templates
│   │   ├── hooks/             # useApiData
│   │   ├── api.js             # Typed Management API client
│   │   ├── logto.js           # Logto OIDC config
│   │   ├── config.js          # import.meta.env wiring
│   │   ├── App.jsx / main.jsx # Routing + providers
│   │   └── styles.css
│   ├── public/
│   ├── vite.config.js
│   └── package.json
└── docker/                    # Reference workspace image Dockerfiles
    └── Dockerfile.codeserver-workspace
```

> Component toolchains: the backend is Go 1.23, the frontend is Node 20 + Vite.
> See each component's section for exact commands; `AGENTS.md` is the canonical,
> copy-pasteable source of truth for build/test commands.

---

## Development Prerequisites
- **Go** 1.23+ (only needed for backend work).
- **Node** 20+ (only needed for frontend work).
- `git`, `make`, and a POSIX shell.
- External accounts for integration testing (optional for unit tests):
  - A **Neon** Postgres database (free tier works) — set `DATABASE_URL`.
  - A **Logto** instance (or Logto Cloud) — set the `LOGTO_*` vars.
  - A **Fly.io** account + API token — set `FLY_API_TOKEN` and `FLY_ORG`.
- A container runtime (Docker) only if you want to build/run the reference
  workspace image locally.

> **Unit tests require no external services.** The Fly Machines client and the
> Logto JWT verifier are exercised against in-process fakes and a test JWKS
> HTTP server, so `go test ./...` runs offline.

---

## Local Setup

### Backend
```bash
cp backend/.env.example backend/.env   # fill in DATABASE_URL, FLY_*, LOGTO_*
make backend-run                        # binds :8080, runs migrations on boot
```

### Frontend
```bash
cp frontend/.env.example frontend/.env.local   # fill in VITE_LOGTO_*
make frontend-install
make frontend-dev                               # binds :5173, proxies /api -> :8080
```

The Vite dev server proxies `/api` and `/health` to the Go backend, so the
browser uses a same-origin origin while the API runs on `:8080`.

### Use it
1. Open http://localhost:5173 → sign in / sign up via Logto.
2. **Templates** → a default code-server Dockerfile is pre-filled → **Save** →
   **Build image** (builds on Fly).
3. **Workspaces** → pick the template → **Create workspace** → **Open IDE** ↗.

---

## Architecture Overview

Cloud Sandbox is a **control plane** (the Go Management API) plus a thin
**management UI**, both talking to external services. There is no in-container
daemon in the MVP — a workspace is simply a Fly Firecracker machine booting a
template image with a code-server IDE, exposed through the Fly Proxy.

```
┌─────────────────┐   JWT (Logto)    ┌──────────────────────┐
│  React + Vite   │ ───────────────▶ │  Go Management API   │
│  (this repo)    │ ◀─── REST ────── │  (sole gatekeeper)   │
└─────────────────┘                  └──────────┬───────────┘
        │                                       │
        │ Logto OIDC                            │ pgx (master pool)
        ▼                                       ▼
┌─────────────────┐                  ┌──────────────────────┐
│   Logto IdP     │                  │  Neon Postgres       │
│  (auth + orgs)  │                  │  (scale-to-zero)     │
└─────────────────┘                  └──────────────────────┘
                                             │
                          Fly Machines/Apps REST API
                                             ▼
                                  ┌──────────────────────┐
                                  │ Per-session Fly App  │
                                  │  ┌────────────────┐  │  autostop=suspend
                                  │  │ Firecracker   │  │  autostart=true
                                  │  │  machine      │  │      (scale-to-zero)
                                  │  │  (code-server) │  │
                                  │  └───────┬───────┘  │
                                  │   NVMe volume @/workspace
                                  └──────────────────────┘
```

Key invariants:
- **No Row-Level Security, no DB-level auth.** The Management API is the sole
  authorization gatekeeper. All ownership checks are application code against a
  master `pgx` connection pool (the anti-lock-in rule).
- **Per-session Fly Apps.** Each workspace becomes its own Fly App so it gets a
  unique scale-to-zero URL for free via the Fly Proxy — no custom routing layer.
- **Scale-to-zero.** Workspace machines use `autostop="suspend"` +
  `autostart=true`, so idle sandboxes cost nothing and wake on the next request
  to their URL. TLS termination and WebSocket passthrough are handled natively
  by the Fly edge proxy.

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the full design and the
end-to-end request flow for "create a workspace".

---

## Contributing by Layer

### Backend (Go Management API)
Location: `backend/` · Module: `github.com/cloudsandbox/platform`

The backend is the brain of the platform. Contributions usually fall into one of
the internal packages:

| Package | What lives here |
| --- | --- |
| `internal/config` | Environment-based config + validation at startup. |
| `internal/db` | `pgxpool` wrapper, embedded SQL migrations (`internal/db/migrations/*.sql`), and the `OrgsRepo` / `TemplatesRepo` / `SessionsRepo` repositories. |
| `internal/auth` | Logto JWT verification against the IdP JWKS (`lestrrat-go/jwx/v3`, cached + auto-refreshed), request-scoped identity injection, and the provisioning middleware that auto-creates the first admin. |
| `internal/fly` | Thin clients for the Fly Machines REST API (machines + volumes) and the Fly Apps REST API (app creation + image build). All behind interfaces for testability. |
| `internal/service` | The orchestrator: maps a session request into Fly App + volume + machine provisioning, writes resource ids back to the DB, handles resume/hibernate/delete + template builds, and enforces org-scoped ownership. |
| `internal/api` | HTTP handlers, routing (`chi`), CORS, and error mapping. |
| `cmd/api` | Entry point: wires dependencies, runs migrations, starts the server. |

When working on the backend:
- **Keep the gatekeeper in the API layer.** Authorization decisions belong in
  `internal/service` and `internal/api` (resource lookups are scoped by
  `org_id`; cross-org access returns 404). Do not push auth logic into the
  database.
- **Keep Fly access behind interfaces.** `fly.MachinesAPI` and
  `fly.BuilderAPI` exist so the orchestrator can be unit-tested with mocks. New
  Fly operations should extend these interfaces and the mock, not call HTTP
  directly from the service layer.
- **Migrations are embedded.** Add new migrations as
  `internal/db/migrations/NNN_name.sql`; they are applied idempotently on boot
  and tracked in `schema_migrations`. Prefer `CREATE TABLE IF NOT EXISTS`-style
  idempotent bodies so migrations are safe to re-run.
- **Add tests.** New orchestration behavior, Fly client operations, and auth
  claims all require accompanying `_test.go` files using the established fakes
  (in-memory stores, `mockFly`, a real-RSA test JWKS server).

### Frontend (React + Vite)
Location: `frontend/`

The UI is intentionally minimal: authenticate with Logto, then manage
templates and workspaces. The API client (`src/api.js`) is a thin, hook-free
module that takes a `getToken` function so it is unit-testable in isolation.

When working on the frontend:
- **No credentials in the browser.** The SPA only ever holds a short-lived
  Logto access token; it never sees passwords. New API calls must go through
  `src/api.js` and forward the bearer token.
- **Keep it lightweight.** Avoid adding heavy state-management or UI-framework
  dependencies; the MVP deliberately uses React state + a small `useApiData`
  hook. If you add a dependency, justify it.
- **Env vars via Vite.** All configuration is read from `import.meta.env` in
  `src/config.js`; do not introduce build-time secrets into the bundle.
- `npm run lint` and `npm run build` must pass.

### Workspace Images (Docker)
Location: `docker/`

Workspace images are what a template's Dockerfile produces. The reference image
(`Dockerfile.codeserver-workspace`) is Ubuntu + Go + `code-server`, running as
a non-root `dev` user out of a mounted `/workspace`, exposing `:8080`.

When contributing workspace images:
- **The Management API mounts an NVMe Fly Volume at `/workspace`.** Ensure the
  image creates `/workspace` and the IDE runs out of it; do not bake state into
  the image.
- **Expose port 8080** (the platform's configured `WORKSPACE_PORT`). The Fly
  Proxy forwards to this port and enables scale-to-zero on it.
- **Run as a non-root user** so files created in the mounted volume are not
  owned by root.
- **Auth is handled by the Fly Proxy / Management API layer.** Disable
  in-editor auth (`code-server --auth none`) so the network boundary controls
  access; the workspace URL is the perimeter.

### Ops & Deployment
Location: `Makefile`, `backend/fly.toml`, `backend/Dockerfile`,
`backend/.env.example`, `frontend/.env.example`

The `Makefile` documents the full self-host sequence. When changing ops:
- Keep the verification checklist in `AGENTS.md` accurate — those are the
  commands CI and contributors rely on.
- Secrets belong in `.env` / `fly secrets`, never in committed files. The
  `.env.example` files list the required variables with safe placeholders.

---

## Agent / IDE Model

The MVP integrates an **IDE**, not an agent harness: each workspace runs
`code-server` (open-source VS Code in the browser), and both humans and AI
coding agents interact through the editor and shell *inside* the workspace. The
fly Proxy provides the unique, scale-to-zero URL.

There is **no in-container daemon or harness abstraction** in the current
implementation. The earlier daemon/harness design is preserved as a clearly
labeled future evolution in [`agents.md`](agents.md). Do not write code that
depends on a daemon process inside workspaces until that direction is adopted.

---

## Testing

Canonical commands (also in `AGENTS.md`):

```bash
# Backend — runs offline (fakes for Fly + JWT verification).
make backend-vet
make backend-test
make backend-build

# Frontend.
make frontend-build
make frontend-dev      # smoke-test in the browser
```

- **Backend unit tests** mock the Fly Machines REST API and the Logto JWT
  verification layer (real RSA keys + a fake JWKS server), so the business logic
  is verified without external dependencies. Current coverage: orchestrator
  ~81%, fly client ~74%, auth ~40%.
- **Frontend** has no dedicated test runner in the MVP; `npm run lint` and a
  successful `npm run build` are the gates. If you add logic worth testing,
  introduce a runner (Vitest) and wire it into the lint/build checks.
- Please add tests alongside any change. New orchestration transitions, Fly
  operations, auth claims, and handlers all require accompanying tests.

---

## Coding Standards
- Follow the existing style in each component; formatting and linting are wired
  into the Makefile / npm scripts.
- Keep functions and modules small and single-purpose. The Go code uses small
  interfaces (`db.Executor`, `fly.MachinesAPI`, `fly.BuilderAPI`,
  `auth.Verifier`, `auth.Provisioner`) — prefer depending on interfaces, not
  concrete types, especially across package boundaries.
- Public API and interface contracts are documented inline.
- No new dependencies without justification. The backend intentionally has a
  tiny dependency surface (`pgx`, `chi`, `jwx`, `godotenv`, `uuid`).
- Never commit secrets, credentials, or organization-specific configuration.

---

## Pull Request Process
1. Open an issue or discussion for anything beyond a small fix, so design can
   be agreed first.
2. Fork and branch from `main`.
3. Keep PRs focused — one layer or one concern per PR where possible.
4. Ensure the `AGENTS.md` verification checklist passes locally
   (`backend-vet`, `backend-test`, `backend-build`, `frontend-build`).
5. Update documentation (`docs/`, `README.md`, `AGENTS.md`, `CONTRIBUTING.md`,
   `agents.md`) for any user- or contributor-visible change.
6. Call out interface or protocol changes explicitly (the Management API's REST
   surface, the Fly client interfaces, the auth claim contract) so maintainers
   can assess compatibility.
7. Mark the PR ready for review.

---

## Release Process
- The project uses semantic versioning.
- Breaking changes to the Management API's REST surface require a major version
  bump and a migration note in `docs/`.
- Each release publishes the Management API container image (via `fly deploy`)
  and a static frontend bundle; the `Makefile` documents the deployment targets.
- See `docs/ARCHITECTURE.md` for the component responsibilities that a release
  must keep coherent.

---

Thank you for helping build a self-hosted, lightweight cloud coding sandbox.

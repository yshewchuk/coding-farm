# Architecture

This document describes the design of the Cloud Sandbox platform and how its
components interact. For build/test commands, see the top-level
[`README.md`](../README.md) and [`AGENTS.md`](../AGENTS.md).

## Components

### 1. Management API (Go, `backend/`)

A single long-lived Fly Machine running a Go HTTP service. It is the **sole
gatekeeper**: all authorization is decided here against a master Postgres
connection pool (no Row-Level Security, no DB-level auth).

Package layout:

| Package | Responsibility |
| --- | --- |
| `internal/config` | Loads env-based config; validates required vars at startup. |
| `internal/db` | `pgxpool` wrapper, embedded SQL migrations, and repositories for orgs/users, templates, and sessions. |
| `internal/auth` | Logto JWT verification against the IdP JWKS (`lestrrat-go/jwx/v3`), request-scoped identity injection, and the provisioning middleware that auto-creates the first admin. |
| `internal/fly` | Thin clients for the Fly Machines REST API (machines + volumes) and the Fly Apps REST API (app creation + image build). Behind interfaces for testability. |
| `internal/service` | The orchestrator: maps a "create session" request into volume + machine provisioning with scale-to-zero services, writes resource ids back to the DB, and handles resume/hibernate/delete + template builds. |
| `internal/api` | HTTP handlers, routing (`chi`), CORS, error mapping. |
| `cmd/api` | Entry point: wires dependencies, runs migrations, starts the server. |

### 2. Frontend (React + Vite, `frontend/`)

A SPA that performs the Logto sign-in flow, obtains an access token, and calls
the Management API. It never handles credentials directly — only the bearer
token that the API validates against Logto's JWKS.

### 3. Database (Neon Serverless Postgres)

Standard PostgreSQL schema (see `backend/internal/db/migrations/001_init.sql`).
Neon's scale-to-zero compute keeps the control plane cheap. Tables:

- `organizations`, `users`, `organization_memberships` — local mirrors of Logto
  identity used for ownership checks.
- `templates` — Dockerfile + build status + image ref.
- `sessions` — workspace lifecycle, scoped Fly resource ids, public URL.

### 4. Fly.io (compute + storage)

Each workspace session becomes its own **Fly App** so it gets a unique
scale-to-zero URL:

- An **NVMe Fly Volume** is provisioned and mounted at `/workspace` for
  persistent, high-speed IDE storage.
- A **Firecracker machine** boots the template image, exposes port `8080`
  through the Fly Proxy with `autostop="suspend"` and `autostart=true`.
- The Fly Proxy handles TLS termination, routing, and WebSocket passthrough
  (code-server), and wakes the machine on the next request to its URL.

### 5. Logto (auth + multi-tenancy)

Logto authenticates users and issues short-lived JWTs. The Management API
validates each request's bearer token against Logto's JWKS, checking signature,
issuer, audience (when configured), and expiration. Organization context flows
from a configurable JWT claim.

## Request lifecycle: "create a workspace"

```
Browser ── POST /api/sessions {template_id} ──▶ Management API
                                                    │
   1. Verify JWT against Logto JWKS (auth.AuthMiddleware)
   2. Provision/resolve identity (auth.ProvisioningMiddleware)
   3. Load template; assert it belongs to the caller's org (authorization)
   4. Ensure a Fly App exists for the session  (fly.EnsureApp)
   5. Provision NVMe Fly Volume                (fly.CreateVolume)
   6. Create Firecracker machine:
        - image = template image_ref
        - mount volume @ /workspace
        - services[0] = {internal_port:8080, autostop:"suspend", autostart:true}
                                                    (fly.CreateMachine)
   7. Write fly_machine_id, fly_volume_id, url back to sessions row
                                                    │
Browser ◀── 201 {status:"running", url:"https://ws-<id>.fly.dev"} ─┘
   │
   └─ user clicks "Open IDE" ──▶ Fly Proxy ──▶ (wakes machine) ──▶ code-server
```

## Security boundaries

- **Unauthenticated requests** → `401` (only `/health` is public).
- **Cross-org access** → `404`: every template/session lookup is scoped by
  `org_id`, so resources owned by another organization simply do not exist
  from the caller's perspective.
- **Templates not built** → `409`: a session cannot be created from a template
  with no image ref (unless a platform default image is configured).
- **Fly resource cleanup** is best-effort on failure and on delete so a stuck
  machine/volume never blocks the database record lifecycle.

## Why these choices

- **pgx + master pool (no RLS):** keeps the authorization logic in one auditable
  place (the Go service layer) and avoids vendor lock-in to Postgres-specific
  RLS, satisfying the anti-lock-in rule.
- **Per-session Fly Apps:** gives each workspace a unique scale-to-zero URL for
  free via the Fly Proxy, with no custom routing layer.
- **`lestrrat-go/jwx/v3`:** the modern, well-maintained Go JWT/JWK library with
  JWKS caching and refresh built in.
- **code-server:** the open-source VS Code Server implementation, runnable in
  any container — no proprietary editor dependency.

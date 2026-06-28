# Cloud Sandbox

A self-hosted, lightweight cloud sandbox environment for disposable development
workspaces (à la Daytona / Cursor Cloud Agents), built on **Fly.io Firecracker
microVMs**, **Neon Serverless Postgres**, and **Logto** authentication with
native scale-to-zero.

> **Status:** MVP. A single administrator can sign up, define a Dockerfile
> template, build it on Fly, launch a sandbox, and connect to a web-based VS
> Code (code-server) running inside it.

---

## Architecture

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
                                  │  ┌────────────────┐  │
                                  │  │ Firecracker   │  │  autostop=suspend
                                  │  │  machine      │  │  autostart=true
                                  │  │  (code-server) │  │      (scale-to-zero)
                                  │  └───────┬───────┘  │
                                  │   NVMe volume @/workspace
                                  └──────────────────────┘
```

### Principles
- **No vendor lock-in for auth/data:** Logto and Neon are swappable; the API
  talks plain Postgres via `pgx` and validates JWTs against any OIDC JWKS.
- **No Row-Level Security:** the Go Management API is the **sole gatekeeper**.
  All authorization is enforced in application code using a master connection
  pool; the database holds no auth logic.
- **Scale-to-zero:** workspace machines use the Fly Proxy's `autostop=suspend`
  + `autostart=true` so idle sandboxes cost nothing and wake on the next
  request to their unique URL.

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the full design.

## Repository layout

```
backend/    Go Management API (cmd/api, internal/{config,db,auth,fly,service,api})
frontend/   React + Vite UI (Logto auth, dashboard, template manager)
docker/     Reference code-server workspace Dockerfile
```

## Quick start (local dev)

### Prerequisites
- Go 1.23+
- Node 20+
- A Neon Postgres database (free tier works) — set `DATABASE_URL`
- A Logto instance — set the `LOGTO_*` vars
- A Fly.io account; `fly auth login` and pick an org (`fly orgs list`, or
  create one with `fly orgs create <name>`) — set `FLY_ORG`. An API token
  (`fly tokens create` → `FLY_API_TOKEN`) is optional for the deploy step
  (it's auto-derived from your login) but required for non-interactive/CI.

### 1. Backend
```bash
cp backend/.env.example backend/.env   # fill in DATABASE_URL, FLY_*, LOGTO_*
make backend-run                        # starts :8080, runs migrations on boot
```

### 2. Frontend
```bash
cp frontend/.env.example frontend/.env.local  # fill in VITE_LOGTO_*
make frontend-install
make frontend-dev                               # starts :5173, proxies /api -> :8080
```

### 3. Use it
1. Open http://localhost:5173 → sign in / sign up via Logto.
2. **Templates** → a default code-server Dockerfile is pre-filled → **Save**
   → **Build image** (builds on Fly).
3. **Workspaces** → pick the template → **Create workspace** → **Open IDE** ↗.

## Self-hosting (production)

The [`Makefile`](Makefile) documents the full sequence. Highlights:

```bash
make bootstrap         # print the end-to-end checklist
make logto-checklist   # Logto OIDC configuration steps
make fly               # create + secret + deploy the API (idempotent; loads .env)
./scripts/deploy.sh web   # deploy the frontend as a static-host Fly app (nginx)
# (the old fly-app / fly-secrets / fly-deploy targets are deprecated aliases)
# Prefer your own static host? `./scripts/deploy.sh frontend` builds dist/ for anywhere.
```

## Testing

```bash
make backend-test      # unit tests: Fly client mock, JWT verification, orchestration
make backend-vet
make frontend-build    # typecheck-free build of the UI bundle
```

The Go unit tests fully mock the Fly Machines REST API and the Logto JWT
verification layer (real RSA keys + a fake JWKS server), so the business logic
is verified without any external dependencies.

## Security model

- Logto authenticates users and issues short-lived access tokens (JWT).
- The Go API validates each request's bearer token against Logto's JWKS
  (`lestrrat-go/jwx/v3`), checking signature, issuer, audience, and expiration.
- The first authenticated request auto-provisions a local user and a personal
  organization (MVP single-admin mode), or an org from a token claim
  (multi-tenant).
- Every resource lookup (template, session) is scoped by `org_id`, so a
  request from another organization gets a `404` — the authorization boundary.

## License

Open-source under the terms in [`LICENSE`](LICENSE).

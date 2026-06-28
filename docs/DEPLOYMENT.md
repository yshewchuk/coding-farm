# Deployment Guide (CLI-only)

This guide describes how to self-host the Cloud Sandbox platform with nothing
but CLIs and one helper script. There is **no Terraform** — the infra surface is
small enough (one Neon project, one Logto app, one Fly app) that a shell script
is clearer and avoids the archived Fly Terraform provider.

Everything is driven by [`scripts/deploy.sh`](../scripts/deploy.sh), which is
idempotent — safe to re-run; each step guards its precondition.

## What gets deployed

| Concern | What | Automation |
| --- | --- | --- |
| **Postgres** | Neon Serverless (scale-to-zero compute) | `neonctl` via the script (optional), or supply `DATABASE_URL` |
| **Identity** | Logto (OIDC IdP) | One-time manual setup in the Logto console (chicken-and-egg) |
| **Control plane** | Go Management API on Fly.io | `flyctl` via the script (create app → set secrets → deploy) |
| **Frontend** | React + Vite static bundle | `npm run build` via the script; deploy `dist/` to any static host |

> The Management API itself calls the Fly Machines REST API at runtime to
> provision per-session workspace machines — that code is the single source of
> truth for Fly integration. The only Fly app you deploy here is the control
> plane.

---

## Prerequisites

- The **`fly` CLI** (`flyctl`), authenticated (`fly auth login`).
- **`jq`** (used by the script for idempotency checks).
- **`npm`/Node 20+** (frontend build).
- A **Neon** account + API key (if you want the script to create the DB; otherwise just grab a connection string from the Neon console).
- A **Logto** instance (Logto Cloud or self-hosted).

Install the optional Neon CLI if you want DB auto-create:
```bash
npm i -g neonctl && neonctl auth
```

---

## One-time bootstrap (manual)

These cannot be automated (Logto needs its own credentials to manage itself):

### Logto (do this once in the Logto console)
1. Create an **Application** of type **SPA**:
   - Redirect URI: `<FRONTEND_URL>/callback`
   - Post sign-out URI: `<FRONTEND_URL>`
   - CORS allowed origins: `<FRONTEND_URL>`
   - → copy its **App ID** (this is `LOGTO_APP_ID` / `VITE_LOGTO_APP_ID`).
2. Create an **API Resource** with indicator = your `LOGTO_AUDIENCE`
   (e.g. `https://api.cloudsandbox.example`) and grant the SPA access to it.
3. *(Multi-tenant only)* Create Organizations + add users as members. The API
   reads the org id from the `organization_id` JWT claim (configurable via
   `LOGTO_ORG_CLAIM`).

### Fly + Neon
```bash
fly auth login
fly tokens create   # if you need a long-lived token for the script
```

---

## Configure

Create a `.env` at the repo root (gitignored) — the script loads it. Copy the
root [`.env.example`](../.env.example) and fill in:

```bash
cp .env.example .env
$EDITOR .env
```

Required values:

| Var | Meaning |
| --- | --- |
| `FLY_ORG` | Your Fly.io org slug. |
| `FLY_API_TOKEN` | A Fly API token (`fly tokens create`). |
| `DATABASE_URL` | Neon Postgres connection string. **Leave blank** to let the script create the project via `neonctl`. |
| `LOGTO_ISSUER` | Logto origin (e.g. `https://your-tenant.logto.app`). |
| `LOGTO_AUDIENCE` | The API resource indicator you created in Logto. |
| `FRONTEND_URL` | The public origin you'll host the frontend at (e.g. `https://app.example.com`). |
| `LOGTO_APP_ID` | The Logto SPA application id. |

Optional: `FLY_APP` (default `cloudsandbox-api`), `NEON_REGION`,
`NEON_PROJECT_NAME`.

---

## Deploy

```bash
# Full flow (creates Neon if DATABASE_URL is unset, prints the Logto checklist,
# deploys the API to Fly, builds the frontend bundle).
./scripts/deploy.sh all
```

Or run individual steps:

```bash
./scripts/deploy.sh preflight     # validate env + tools
./scripts/deploy.sh neon-create   # create a Neon project (sets DATABASE_URL)
./scripts/deploy.sh logto         # print the one-time Logto console checklist
./scripts/deploy.sh fly            # create/secret/deploy the Management API
./scripts/deploy.sh frontend      # build frontend/dist/ with env baked in
```

On success, the Management API is live at `https://<FLY_APP>.fly.dev` (migrations
run automatically on boot — the backend logs `database migrated`). The frontend
bundle is in `frontend/dist/`; deploy it to any static host at `FRONTEND_URL`.

---

## The handoff diagram

```
.env (your values) ──┐
                     ▼
            scripts/deploy.sh all
                     │
   ┌─────────────────┼──────────────────┐
   ▼                 ▼                  ▼
neonctl          flyctl                npm
(creates DB)     (app+secrets+deploy)  (builds bundle)
   │                 │                  │
   ▼                 ▼                  ▼
DATABASE_URL   <app>.fly.dev        frontend/dist/
                                     │
                                     ▼
                          you push dist/ to a static host
```

Logto is the one manual piece: the script prints the checklist and reads the
resulting `LOGTO_*` values from `.env`/env to wire Fly secrets + the frontend
build. After the first setup, re-running `./scripts/deploy.sh all` is fully
automated.

---

## Day-2 operations

| Task | How |
| --- | --- |
| Redeploy the API after a code change | `./scripts/deploy.sh fly` (migrations run on boot) |
| Rotate Neon credentials | update `DATABASE_URL` in `.env`, re-run `./scripts/deploy.sh fly` |
| Rotate the Fly API token | `fly tokens create`, update `.env`, re-run `fly` |
| Add an admin (multi-tenant) | create the Logto org + user by hand; the API resolves the org from the JWT claim |
| Destroy everything | `fly apps destroy <FLY_APP>`; delete the Neon project in the console / `neonctl projects delete` |

---

## Local development (no deploy needed)

For local dev you do not need the deploy script at all — point the backend at any
Postgres and a Logto dev instance:

```bash
cp backend/.env.example backend/.env          # fill DATABASE_URL, FLY_*, LOGTO_*
make backend-run                                # :8080, migrations on boot
cp frontend/.env.example frontend/.env.local   # fill VITE_LOGTO_*
make frontend-install && make frontend-dev      # :5173 proxies /api -> :8080
```

The Go unit tests require no external services — the Fly client and JWT verifier
run against in-process fakes / a test JWKS server.

---

## Verification before merging a change

```bash
make backend-vet
make backend-test
make backend-build
make frontend-build
```

See [`AGENTS.md`](../AGENTS.md) for the canonical commands.

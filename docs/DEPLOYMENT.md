# Deployment Guide (CLI-only)

This guide describes how to self-host the Cloud Sandbox platform with nothing
but CLIs and one helper script. There is **no Terraform** — the infra surface is
small enough (one Neon project, one Logto app, two Fly apps) that a shell script
is clearer and avoids the archived Fly Terraform provider.

Everything is driven by [`scripts/deploy.sh`](../scripts/deploy.sh), which is
idempotent — safe to re-run; each step guards its precondition.

## What gets deployed

| Concern | What | Automation |
| --- | --- | --- |
| **Postgres** | Neon Serverless (scale-to-zero compute) | `neonctl` via the script (optional), or supply `DATABASE_URL` |
| **Identity** | Logto (OIDC IdP) | One M2M app created by hand, then `logto-setup` automates the SPA app + API resource |
| **Control plane** | Go Management API on Fly.io | `flyctl` via the script (create app → set secrets → deploy) |
| **Frontend** | React + Vite static bundle, served by nginx | `flyctl` via the script: builds the bundle and deploys its own static-host Fly app (default `cloudsandbox-web`) |

> The Management API itself calls the Fly Machines REST API at runtime to
> provision per-session workspace machines — that code is the single source of
> truth for Fly integration. You deploy **two** Fly apps: the control plane
> (`cloudsandbox-api`) and the static frontend host (`cloudsandbox-web`).

---

## Prerequisites

- The **`fly` CLI** (`flyctl`), authenticated:
  ```bash
  fly auth login
  ```
  A personal Fly.io organization is auto-created when you sign up. List yours
  with `fly orgs list`, or create a dedicated one (recommended — keeps billing
  isolated) with `fly orgs create <name>`. Use that slug as `FLY_ORG`.
  > A new Fly org needs a payment method attached before it can run machines.
- **`jq`** and **`curl`** (used by the script for Logto setup + idempotency checks).
- **`npm`/Node 20+** (frontend build).
- A **Neon** account + API key (if you want the script to create the DB; otherwise just grab a connection string from the Neon console).
- A **Logto** instance (Logto Cloud or self-hosted).

Install the optional Neon CLI if you want DB auto-create:
```bash
npm i -g neonctl && neonctl auth
```

---

## One-time bootstrap

Only one Logto step must be done by hand: creating the machine-to-machine (M2M)
app that lets the script talk to Logto's own Management API. After that,
`logto-setup` creates the SPA app + API resource for you.

### Logto M2M seed (do this once in the Logto console)
1. Create a **Machine-to-machine** application.
2. Assign it the built-in **"Logto Management API"** role (scope `all`).
   New tenants ship with a pre-configured *"Logto Management API access"* role.
3. Copy its **App ID** + **App Secret** into `LOGTO_M2M_APP_ID` /
   `LOGTO_M2M_APP_SECRET` in your `.env`.

### Automate the rest
```bash
./scripts/deploy.sh logto-setup
```
This is **idempotent**: it creates (or updates) the SPA application — redirect
`<FRONTEND_URL>/callback`, post sign-out `<FRONTEND_URL>`, CORS
`<FRONTEND_URL>` — and the API resource with indicator = your `LOGTO_AUDIENCE`,
then writes `LOGTO_APP_ID` back to `.env`. Re-run it whenever the frontend
origin or audience changes.

> The SPA is a **first-party** app, so Logto grants the API resource's scopes
> automatically — no role/scope wiring is needed for core auth. The backend
> validates only the JWT audience; it does not enforce scopes.

> *(Multi-tenant only)* Creating Organizations and adding members remains a
> manual step in the Logto console; the API reads the org id from the
> `organization_id` JWT claim (configurable via `LOGTO_ORG_CLAIM`).

### Fly + Neon
```bash
fly auth login          # required; flyctl uses this for the deploy step
fly orgs list           # pick an org slug (or create one: fly orgs create <name>)
fly tokens create       # optional: only for non-interactive/CI redeploys (set FLY_API_TOKEN)
```
> The deploy step does **not** require `FLY_API_TOKEN`: flyctl uses its login
> session to create/deploy the app. The script still sets a token as a runtime
> **secret** (the deployed Management API needs one to call the Fly Machines
> REST API) — when `FLY_API_TOKEN` is unset it is derived from `fly auth token`.

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
| `FLY_API_TOKEN` | A Fly API token (`fly tokens create`). **Optional** for the deploy step — flyctl uses its login; the script derives one from `fly auth token` when unset and sets it as a runtime secret. Set it explicitly for non-interactive/CI redeploys. |
| `DATABASE_URL` | Neon Postgres connection string. **Leave blank** to let the script create the project via `neonctl`. |
| `LOGTO_DOMAIN` | Logto origin (e.g. `https://your-tenant.logto.app`). |
| `LOGTO_AUDIENCE` | The API resource indicator (audience) the backend validates. |
| `FRONTEND_URL` | The public origin you'll host the frontend at (e.g. `https://app.example.com`). |
| `LOGTO_M2M_APP_ID` | M2M app id (one-time console seed; required by `logto-setup`). |
| `LOGTO_M2M_APP_SECRET` | M2M app secret (one-time console seed; required by `logto-setup`). |
| `LOGTO_APP_ID` | The Logto SPA application id. **Auto-set** by `logto-setup`; leave blank to create it. |

Optional: `FLY_APP` (default `cloudsandbox-api`), `NEON_REGION`,
`NEON_PROJECT_NAME`, `LOGTO_SPA_APP_NAME` (default `"Cloud Sandbox"`).

---

## Deploy

```bash
# Full flow (creates Neon if DATABASE_URL is unset, runs logto-setup when M2M
# seed vars are present, deploys the API AND the frontend to Fly).
./scripts/deploy.sh all
```

Or run individual steps:

```bash
./scripts/deploy.sh preflight     # validate env + tools
./scripts/deploy.sh neon-create   # create a Neon project (sets DATABASE_URL)
./scripts/deploy.sh logto         # print the one-time Logto M2M seed checklist
./scripts/deploy.sh logto-setup   # create/update the SPA app + API resource (sets LOGTO_APP_ID)
./scripts/deploy.sh fly            # create/secret/deploy the Management API
./scripts/deploy.sh web            # deploy the frontend as a static-host Fly app
./scripts/deploy.sh frontend      # build frontend/dist/ locally (host anywhere; optional)
```

On success, the Management API is live at `https://<FLY_APP>.fly.dev` (migrations
run automatically on boot — the backend logs `database migrated`) and the
frontend is live at `https://<FLY_WEB_APP>.fly.dev` (default
`cloudsandbox-web`). To use a custom domain, point it at the web app and add the
origin to Logto's SPA redirect URIs.

> Prefer your own static host (Cloudflare Pages, Netlify, S3, …)? Run
> `./scripts/deploy.sh frontend` instead of `web` — it builds `frontend/dist/`
> with the Vite env baked in for you to ship anywhere.

---

## The handoff diagram

```
.env (your values) ──┐
                     ▼
            scripts/deploy.sh all
                     │
   ┌─────────────────┼──────────────────┐
   ▼                 ▼                  ▼
neonctl          flyctl                flyctl
(creates DB)    (API: app+secrets     (web: builds bundle +
                  +deploy)             nginx deploy)
   │                 │                  │
   ▼                 ▼                  ▼
DATABASE_URL   <app>.fly.dev        <web-app>.fly.dev
```

Logto is the one manual piece: create the M2M seed app once in the console,
then `./scripts/deploy.sh logto-setup` (run automatically by `all` once the M2M
seed vars are in `.env`) creates the SPA app + API resource and writes
`LOGTO_APP_ID` back to `.env`. After that, re-running
`./scripts/deploy.sh all` is fully automated.

---

## Day-2 operations

| Task | How |
| --- | --- |
| Redeploy the API after a code change | `./scripts/deploy.sh fly` (migrations run on boot) |
| Rotate Neon credentials | update `DATABASE_URL` in `.env`, re-run `./scripts/deploy.sh fly` |
| Rotate the Fly API token | `fly tokens create`, update `.env`, re-run `fly` |
| Change frontend origin / audience | update `FRONTEND_URL`/`LOGTO_AUDIENCE` in `.env`, re-run `./scripts/deploy.sh logto-setup` (idempotent) then `fly` |
| Rotate Logto M2M creds | recreate the M2M app, update `LOGTO_M2M_*` in `.env`, re-run `logto-setup` |
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

#!/usr/bin/env bash
#
# scripts/deploy.sh — one-shot, CLI-only deploy for the Cloud Sandbox platform.
#
# The platform has exactly three external dependencies + one app:
#   1. Neon Serverless Postgres  -> neonctl CLI (optional auto-create) or manual
#   2. Logto (OIDC IdP)          -> manual one-time setup (chicken-and-egg)
#   3. Fly.io Management API app -> flyctl CLI (this script)
#   4. Frontend                  -> static build (this script builds the bundle)
#
# There is intentionally no Terraform: the maintained-provider surface is tiny
# (one Neon project + one Logto app), the Fly.io provider is archived, and a
# shell script keeps the deploy story in one auditable place.
#
# Idempotent: safe to re-run. Each step is guarded so it only acts when needed.
# Every required value can come from the environment OR a .env file loaded first.
#
set -euo pipefail

# -----------------------------------------------------------------------------
# Helpers
# -----------------------------------------------------------------------------
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-$ROOT/.env}"

# ANSI colors, disabled when not a TTY.
if [ -t 1 ]; then C_HEAD='\033[1;36m'; C_OK='\033[32m'; C_WARN='\033[33m'; C_ERR='\033[31m'; C_OFF='\033[0m'; else C_HEAD=''; C_OK=''; C_WARN=''; C_ERR=''; C_OFF=''; fi
head()  { printf "${C_HEAD}==> %s${C_OFF}\n" "$*"; }
ok()    { printf "${C_OK}   ✓ %s${C_OFF}\n" "$*"; }
warn()  { printf "${C_WARN}   ! %s${C_OFF}\n" "$*"; }
err()   { printf "${C_ERR}   ✗ %s${C_OFF}\n" "$*" >&2; }
die()   { err "$*"; exit 1; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || die "required command not found: $1 (install it or add to PATH)"; }

require_env() {
  local name="$1" val="${!1:-}"
  [ -n "$val" ] || die "environment variable $1 is required${2:+ (hint: $2)}"
}

# Load a .env file if present (does not override already-set env vars).
load_env() {
  if [ -f "$ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$ENV_FILE"
    set +a
    ok "loaded env from $ENV_FILE"
  fi
}

# Validate a URL-ish origin (no trailing slash).
validate_origin() {
  case "$1" in
    http://*|https://*) : ;;
    *) die "$1 must be an http(s) origin (got: $1)" ;;
  esac
  case "$1" in
    */) die "$1 must not have a trailing slash" ;;
  esac
}

# -----------------------------------------------------------------------------
# Step 0: preflight
# -----------------------------------------------------------------------------
# Preflight for the Fly deploy step. DATABASE_URL is expected to be set by now
# (created by neon_create, or supplied manually / via .env).
preflight() {
  head "Preflight"
  need_cmd fly
  need_cmd jq
  require_env FLY_ORG "your Fly.io org slug"
  require_env FLY_API_TOKEN "fly auth token; or run 'fly tokens create'"
  require_env LOGTO_ISSUER "e.g. https://your-tenant.logto.app"
  validate_origin "$LOGTO_ISSUER"
  require_env DATABASE_URL "run 'scripts/deploy.sh neon-create' or paste it from the Neon console"
  require_env LOGTO_AUDIENCE "the Logto API resource indicator"
  require_env FRONTEND_URL "the deployed frontend origin, e.g. https://app.example.com"
  validate_origin "$FRONTEND_URL"
  ok "all required env vars present"
}

# -----------------------------------------------------------------------------
# Step 1 (optional): provision a Neon project with neonctl
# -----------------------------------------------------------------------------
neon_create() {
  head "Neon project (neonctl)"
  need_cmd neonctl
  need_cmd jq
  local region="${NEON_REGION:-aws-us-east-1}"
  local name="${NEON_PROJECT_NAME:-cloudsandbox}"
  warn "creating Neon project '$name' in $region (set DATABASE_URL manually to skip this step)"
  local out
  out="$(neonctl projects create --name "$name" --region-id "$region" --output json)"
  # Extract the direct (non-pooled) connection uri.
  local uri
  uri="$(printf '%s' "$out" | jq -r '.connection_uris[0].connection_uri // empty')"
  [ -n "$uri" ] || die "could not parse connection_uri from neonctl output"
  export DATABASE_URL="$uri"
  # Persist it back to .env so subsequent steps + future runs don't need to recreate.
  if [ -f "$ENV_FILE" ]; then
    if grep -q '^DATABASE_URL=' "$ENV_FILE"; then
      warn ".env already has DATABASE_URL; leaving it as-is (exported the new value for this run)"
    else
      printf 'DATABASE_URL=%q\n' "$uri" >> "$ENV_FILE"
      ok "wrote DATABASE_URL to $ENV_FILE"
    fi
  fi
  ok "Neon project created; DATABASE_URL set for this run"
}

# -----------------------------------------------------------------------------
# Step 2: Logto setup (manual; chicken-and-egg)
# -----------------------------------------------------------------------------
logto_checklist() {
  head "Logto (manual one-time setup)"
  cat <<EOF
Logto cannot be created by this script (it needs its own credentials to talk to
its own API). Do this once in the Logto console:

  1. Create an Application of type "SPA":
       - Redirect URI:        ${FRONTEND_URL}/callback
       - Post sign-out URI:    ${FRONTEND_URL}
       - CORS allowed origins: ${FRONTEND_URL}
     -> copy its App ID into LOGTO_APP_ID / VITE_LOGTO_APP_ID.
  2. Create an API Resource with indicator = ${LOGTO_AUDIENCE}
     and grant the SPA access to it.
  3. (Multi-tenant only) Create Organizations; add users as members.
     The API reads the org id from the LOGTO_ORG_CLAIM (default: organization_id).

Then set these env vars (already required by preflight):
  LOGTO_ISSUER=${LOGTO_ISSUER}
  LOGTO_AUDIENCE=${LOGTO_AUDIENCE}
  LOGTO_APP_ID=<SPA app id>
EOF
  ok "Logto checklist printed"
}

# -----------------------------------------------------------------------------
# Step 3: deploy the Management API to Fly.io
# -----------------------------------------------------------------------------
fly_deploy() {
  head "Deploy Management API to Fly.io"
  need_cmd fly
  local app="${FLY_APP:-cloudsandbox-api}"

  # Create the app if it doesn't exist (idempotent). Tolerate either Name/name.
  if ! fly apps list --json 2>/dev/null | jq -e --arg a "$app" \
      '.[] | select((.Name // .name) == $a)' >/dev/null; then
    fly apps create "$app" --org "$FLY_ORG" >/dev/null
    ok "created Fly app '$app'"
  else
    ok "Fly app '$app' already exists"
  fi

  # Set secrets (idempotent: fly secrets set overwrites in place).
  fly secrets set --app "$app" \
    DATABASE_URL="$DATABASE_URL" \
    FLY_API_TOKEN="$FLY_API_TOKEN" \
    FLY_ORG="$FLY_ORG" \
    LOGTO_ISSUER="$LOGTO_ISSUER" \
    LOGTO_AUDIENCE="$LOGTO_AUDIENCE" \
    FRONTEND_URL="$FRONTEND_URL" \
    >/dev/null
  ok "secrets set on '$app'"

  # Deploy. Migrations run automatically on boot (internal/db.Migrate).
  fly deploy "$ROOT/backend" --app "$app" --remote-only
  ok "deployed; migrations run on boot"
  local api_url
  api_url="https://$app.fly.dev"
  ok "Management API: $api_url/health"
  echo "$api_url"
}

# -----------------------------------------------------------------------------
# Step 4: build the frontend bundle
# -----------------------------------------------------------------------------
frontend_build() {
  head "Build frontend bundle"
  need_cmd npm
  require_env LOGTO_APP_ID "the Logto SPA application id (create it once; see 'scripts/deploy.sh logto')"
  local api_url="${1:-https://${FLY_APP:-cloudsandbox-api}.fly.dev}"
  (
    cd "$ROOT/frontend"
    [ -d node_modules ] || npm install >/dev/null 2>&1
    VITE_API_BASE="$api_url" \
    VITE_LOGTO_ENDPOINT="$LOGTO_ISSUER" \
    VITE_LOGTO_APP_ID="$LOGTO_APP_ID" \
    VITE_LOGTO_RESOURCE="$LOGTO_AUDIENCE" \
    npm run build
  )
  ok "frontend bundle built -> frontend/dist/"
  warn "deploy frontend/dist/ to any static host at origin $FRONTEND_URL"
}

# -----------------------------------------------------------------------------
# CLI
# -----------------------------------------------------------------------------
usage() {
  cat <<EOF
Usage: scripts/deploy.sh <command>

Commands:
  all             Run neon (optional) -> logto -> fly -> frontend (default)
  preflight       Check required env vars + tools
  neon-create     Create a Neon project with neonctl (sets DATABASE_URL)
  logto           Print the one-time Logto console checklist
  fly             Create/secret/deploy the Management API app on Fly.io
  frontend        Build the React bundle with env baked in
  help            Show this help

Env (via environment or a .env file at repo root):
  Required: FLY_ORG, FLY_API_TOKEN, LOGTO_ISSUER, DATABASE_URL,
            LOGTO_AUDIENCE, FRONTEND_URL, LOGTO_APP_ID
  Optional: FLY_APP (default cloudsandbox-api), NEON_REGION, NEON_PROJECT_NAME,
            ENV_FILE (default ./backend/.env or ./.env)

The script is idempotent: each step guards its precondition, so 'all' can be
re-run safely after fixing a failing step.
EOF
}

cmd="${1:-all}"
load_env
case "$cmd" in
  all)
    load_env
    # Create the Neon project on the first run if a DATABASE_URL was not supplied.
    if [ -z "${DATABASE_URL:-}" ]; then
      if command -v neonctl >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
        neon_create
      else
        die "DATABASE_URL is not set. Either:
  - run 'scripts/deploy.sh neon-create' (requires neonctl + a NEON_API_KEY), or
  - set DATABASE_URL yourself (from the Neon console) and re-run."
      fi
    fi
    preflight
    logto_checklist
    api_url="$(fly_deploy)"
    frontend_build "$api_url"
    head "Done"
    ok "Next: deploy frontend/dist/ to a static host at $FRONTEND_URL, then sign in."
    ;;
  preflight)    preflight ;;
  neon-create)  neon_create ;;
  logto)        load_env; require_env FRONTEND_URL; require_env LOGTO_AUDIENCE; logto_checklist ;;
  fly)          preflight; fly_deploy ;;
  frontend)     preflight; frontend_build ;;
  help|-h|--help) usage ;;
  *) err "unknown command: $cmd"; usage; exit 1 ;;
esac

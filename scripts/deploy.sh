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
section()  { printf "${C_HEAD}==> %s${C_OFF}\n" "$*"; }
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
# Strips trailing CR so CRLF .env files saved on Windows don't corrupt values.
load_env() {
  [ -f "$ENV_FILE" ] || return 0
  local tmp
  tmp="$(mktemp)"
  sed 's/\r$//' "$ENV_FILE" > "$tmp"
  set -a
  # shellcheck disable=SC1090
  . "$tmp"
  set +a
  rm -f "$tmp"
  ok "loaded env from $ENV_FILE"
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
  section "Preflight"
  need_cmd fly
  need_cmd jq
  require_env FLY_ORG "your Fly.io org slug (run 'fly orgs list'; create one with 'fly orgs create')"
  require_env LOGTO_ISSUER "e.g. https://your-tenant.logto.app"
  validate_origin "$LOGTO_ISSUER"
  ensure_database_url
  require_env LOGTO_AUDIENCE "the Logto API resource indicator"
  require_env FRONTEND_URL "the deployed frontend origin, e.g. https://app.example.com"
  validate_origin "$FRONTEND_URL"
  # FLY_API_TOKEN is optional for the deploy step (flyctl uses its own login),
  # but the deployed Management API needs a long-lived token at runtime to call
  # the Fly Machines REST API. ensure_fly_token derives one when unset.
  ensure_fly_token
  ok "all required env vars present"
}

# Ensure FLY_API_TOKEN is set for the runtime secret. If the operator supplied
# one (recommended for CI / non-interactive deploys) it is used as-is; otherwise
# we derive a token from the logged-in flyctl session (run 'fly auth login').
# The deployed app cannot use an interactive login, so a token is always set.
ensure_fly_token() {
  if [ -n "${FLY_API_TOKEN:-}" ]; then
    ok "FLY_API_TOKEN provided; will be set as a runtime secret"
    return
  fi
  if ! fly auth whoami >/dev/null 2>&1; then
    die "not authenticated to Fly.io. Either run 'fly auth login' or set FLY_API_TOKEN (run 'fly tokens create')."
  fi
  FLY_API_TOKEN="$(fly auth token)"
  export FLY_API_TOKEN
  warn "FLY_API_TOKEN not set; derived from 'fly auth token' (logged-in session). It is set as a runtime secret but not persisted to .env — set it there for non-interactive redeploys."
}

# Ensure DATABASE_URL is set. If already provided (env or .env) it is used
# as-is; otherwise we try to create a Neon project with neonctl and persist it
# back to .env. Called from preflight so every command (all/fly/frontend) gets
# the same auto-create fallback — not just `all`.
#
# When .env was loaded but DATABASE_URL still parsed empty, point the operator
# at the exact line + cause instead of a generic "required" error. The common
# traps are: a blank `DATABASE_URL=`, or an inline `#` comment right after `=`
# that bash parses as empty (e.g. `DATABASE_URL= # from neon console`).
ensure_database_url() {
  [ -n "${DATABASE_URL:-}" ] && return
  if command -v neonctl >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
    neon_create
    return
  fi
  if [ -f "$ENV_FILE" ]; then
    local line ln val
    line="$(grep -nE '^[[:space:]]*DATABASE_URL=' "$ENV_FILE" | head -n1 || true)"
    if [ -n "$line" ]; then
      ln="${line%%:*}"
      val="${line#*DATABASE_URL=}"   # everything after the first DATABASE_URL=
      if printf '%s\n' "$val" | grep -qE '^[[:space:]]*#'; then
        die "DATABASE_URL is blanked by an inline '#' comment in $ENV_FILE (line $ln):
    ${line#*:}
Put the connection string BEFORE any '#', e.g.:
    DATABASE_URL=postgres://user:password@ep-xxx.neon.tech/db?sslmode=require  # optional note
(or run 'scripts/deploy.sh neon-create' with neonctl installed to auto-create one)."
      fi
      if printf '%s\n' "$val" | grep -qE '^[[:space:]]*$'; then
        die "DATABASE_URL is blank in $ENV_FILE (line $ln). Paste your Neon connection string, e.g.:
    DATABASE_URL=postgres://user:password@ep-xxx.neon.tech/db?sslmode=require
(or run 'scripts/deploy.sh neon-create' with neonctl installed to auto-create one)."
      fi
      # The line has a non-empty value but it parsed to empty: usually a '$'
      # in the value referencing an undefined variable.
      die "DATABASE_URL in $ENV_FILE (line $ln) parsed to an empty value:
    ${line#*:}
This usually means the value contains a '\$' referencing an undefined variable.
Single-quote the value (so '\$' is literal) or remove the '\$', e.g.:
    DATABASE_URL='postgres://user:password@ep-xxx.neon.tech/db?sslmode=require'"
    fi
  fi
  die "DATABASE_URL is not set. Either:
  - run 'scripts/deploy.sh neon-create' (requires neonctl + a NEON_API_KEY), or
  - set DATABASE_URL yourself (from the Neon console) and re-run."
}

# -----------------------------------------------------------------------------
# Step 1 (optional): provision a Neon project with neonctl
# -----------------------------------------------------------------------------
neon_create() {
  section "Neon project (neonctl)"
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
# Step 2: Logto setup
# -----------------------------------------------------------------------------
# Logto's Management API authenticates with a machine-to-machine (M2M)
# client_credentials token. The M2M app must be created by hand once (Logto
# cannot manage its own management credentials) and granted the built-in
# "Logto Management API" role (scope `all`). After that seed step, `logto-setup`
# creates/updates the SPA application + API resource idempotently via the API.
logto_checklist() {
  section "Logto (one-time M2M seed)"
  cat <<EOF
Logto's Management API needs machine credentials to manage itself, so create
one M2M app by hand (once). Then 'scripts/deploy.sh logto-setup' automates the
rest (SPA app + API resource).

  1. In the Logto console, create a "Machine-to-machine" application.
  2. Assign it the built-in "Logto Management API" role (scope: all).
     New tenants ship with a pre-configured "Logto Management API access" role.
  3. Copy its App ID + App Secret into .env:
       LOGTO_M2M_APP_ID=<m2m app id>
       LOGTO_M2M_APP_SECRET=<m2m app secret>

Then run:
  ./scripts/deploy.sh logto-setup

This creates the SPA app (redirect ${FRONTEND_URL}/callback, CORS ${FRONTEND_URL})
and the API resource (indicator ${LOGTO_AUDIENCE}), and writes LOGTO_APP_ID back
to .env. Multi-tenant Organizations/members remain a manual step in the console.
EOF
  ok "Logto M2M seed checklist printed"
}

# Fetch an M2M access token for the Logto Management API; export M2M_TOKEN.
logto_m2m_token() {
  require_env LOGTO_M2M_APP_ID "the M2M app id (see 'scripts/deploy.sh logto')"
  require_env LOGTO_M2M_APP_SECRET "the M2M app secret (see 'scripts/deploy.sh logto')"
  local resp token
  resp="$(curl -sS -X POST "${LOGTO_ISSUER}/oidc/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    --data-urlencode "grant_type=client_credentials" \
    --data-urlencode "client_id=${LOGTO_M2M_APP_ID}" \
    --data-urlencode "client_secret=${LOGTO_M2M_APP_SECRET}" \
    --data-urlencode "resource=${LOGTO_ISSUER}/api" \
    --data-urlencode "scope=all")"
  token="$(printf '%s' "$resp" | jq -r '.access_token // empty')"
  [ -n "$token" ] || die "could not fetch M2M token from Logto: ${resp}"
  export M2M_TOKEN="$token"
  ok "M2M access token acquired (valid ~1h)"
}

# logto_api METHOD PATH [BODY] -> prints response body; non-zero on HTTP >= 400.
logto_api() {
  local method="$1" path="$2" body="${3:-}" resp code
  resp="$(curl -sS -w '\n%{http_code}' -X "$method" "${LOGTO_ISSUER}${path}" \
    -H "Authorization: Bearer ${M2M_TOKEN}" \
    -H "Content-Type: application/json" \
    ${body:+-d "$body"})"
  code="$(printf '%s' "$resp" | tail -n1)"
  printf '%s' "$resp" | sed '$d'
  if [ "${code:-500}" -ge 400 ] 2>/dev/null; then
    err "Logto ${method} ${path} -> HTTP ${code}"
    return 1
  fi
  return 0
}

logto_setup() {
  section "Logto setup (SPA app + API resource)"
  need_cmd curl
  need_cmd jq
  require_env LOGTO_ISSUER "e.g. https://your-tenant.logto.app"
  validate_origin "$LOGTO_ISSUER"
  require_env LOGTO_AUDIENCE "the API resource indicator, e.g. https://api.example.com"
  require_env FRONTEND_URL "the deployed frontend origin"
  validate_origin "$FRONTEND_URL"

  logto_m2m_token

  local spa_name="${LOGTO_SPA_APP_NAME:-Cloud Sandbox}" res_id app_id spa_body

  # --- API resource (indicator = LOGTO_AUDIENCE); create if missing ---
  res_id="$(logto_api GET "/api/resources" | jq -r --arg i "$LOGTO_AUDIENCE" \
    '.[] | select(.indicator == $i) | .id' | head -n1 || true)"
  if [ -n "$res_id" ]; then
    ok "API resource '${LOGTO_AUDIENCE}' exists (id ${res_id})"
  else
    res_id="$(logto_api POST "/api/resources" \
      "$(jq -nc --arg n "${spa_name} API" --arg i "$LOGTO_AUDIENCE" '{name:$n,indicator:$i}')" \
      | jq -r '.id' || true)"
    [ -n "$res_id" ] || die "failed to create API resource '${LOGTO_AUDIENCE}'"
    ok "created API resource '${LOGTO_AUDIENCE}' (id ${res_id})"
  fi

  # --- SPA application; create or re-sync redirect/CORS to FRONTEND_URL ---
  app_id="$(logto_api GET "/api/applications" | jq -r --arg n "$spa_name" \
    '.[] | select(.name == $n and .type == "SPA") | .id' | head -n1 || true)"
  spa_body="$(jq -nc \
    --arg n "$spa_name" \
    --arg ru "${FRONTEND_URL}/callback" \
    --arg pr "${FRONTEND_URL}" \
    --arg co "$FRONTEND_URL" \
    '{name:$n,type:"SPA",isThirdParty:false,
      oidcClientMetadata:{redirectUris:[$ru],postLogoutRedirectUris:[$pr]},
      customClientMetadata:{corsAllowedOrigins:[$co]}}')"
  if [ -n "$app_id" ]; then
    logto_api PATCH "/api/applications/${app_id}" "$spa_body" >/dev/null \
      || die "failed to update SPA app '${spa_name}'"
    ok "SPA app '${spa_name}' exists (id ${app_id}); synced redirect/CORS"
  else
    app_id="$(logto_api POST "/api/applications" "$spa_body" | jq -r '.id' || true)"
    [ -n "$app_id" ] || die "failed to create SPA app '${spa_name}'"
    ok "created SPA app '${spa_name}' (id ${app_id})"
  fi

  # Persist + export LOGTO_APP_ID so the frontend build step can use it.
  export LOGTO_APP_ID="$app_id"
  if [ -f "$ENV_FILE" ] && ! grep -q '^LOGTO_APP_ID=' "$ENV_FILE"; then
    printf 'LOGTO_APP_ID=%q\n' "$app_id" >> "$ENV_FILE"
    ok "wrote LOGTO_APP_ID to $ENV_FILE"
  fi
  ok "LOGTO_APP_ID=${app_id}"
}

# -----------------------------------------------------------------------------
# Step 3: deploy the Management API to Fly.io
# -----------------------------------------------------------------------------
fly_deploy() {
  section "Deploy Management API to Fly.io"
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
  section "Build frontend bundle"
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
# Step 5: deploy the frontend as a static-host Fly app (nginx + SPA fallback)
# -----------------------------------------------------------------------------
# The UI is a stateless static bundle served by its own Fly app (default
# cloudsandbox-web), separate from the Management API. The Vite env (VITE_*,
# including VITE_API_BASE pointing at the API app) is baked into the bundle at
# build time via fly deploy --build-arg, so no secrets are needed at runtime.
web_deploy() {
  section "Deploy frontend static host to Fly.io"
  need_cmd fly
  require_env LOGTO_APP_ID "the Logto SPA application id (run 'scripts/deploy.sh logto-setup' first)"
  local web_app="${FLY_WEB_APP:-cloudsandbox-web}"
  local api_url="${1:-https://${FLY_APP:-cloudsandbox-api}.fly.dev}"

  # Create the web app if it doesn't exist (idempotent).
  if ! fly apps list --json 2>/dev/null | jq -e --arg a "$web_app" \
      '.[] | select((.Name // .name) == $a)' >/dev/null; then
    fly apps create "$web_app" --org "$FLY_ORG" >/dev/null
    ok "created Fly app '$web_app'"
  else
    ok "Fly app '$web_app' already exists"
  fi

  # Build + deploy remotely on Fly's builder. VITE_* are baked into the bundle
  # at build time; the runtime container needs no secrets.
  fly deploy "$ROOT" --config "$ROOT/frontend/fly.toml" --app "$web_app" --remote-only \
    --build-arg "VITE_API_BASE=$api_url" \
    --build-arg "VITE_LOGTO_ENDPOINT=$LOGTO_ISSUER" \
    --build-arg "VITE_LOGTO_APP_ID=$LOGTO_APP_ID" \
    --build-arg "VITE_LOGTO_RESOURCE=$LOGTO_AUDIENCE"

  local web_url="https://$web_app.fly.dev"
  ok "Frontend static host: $web_url"
  warn "point your custom domain / FRONTEND_URL ($FRONTEND_URL) at $web_url, and add $web_url (+ $FRONTEND_URL) as a Logto redirect origin."
  echo "$web_url"
}

# -----------------------------------------------------------------------------
# CLI
# -----------------------------------------------------------------------------
usage() {
  cat <<EOF
Usage: scripts/deploy.sh <command>

Commands:
  all             Run neon (optional) -> logto -> fly -> web (default)
  preflight       Check required env vars + tools
  neon-create     Create a Neon project with neonctl (sets DATABASE_URL)
  logto           Print the one-time Logto M2M seed checklist
  logto-setup     Create/update the Logto SPA app + API resource (sets LOGTO_APP_ID)
  fly             Create/secret/deploy the Management API app on Fly.io
  web             Deploy the frontend as a static-host Fly app (nginx + SPA)
  frontend        Build the React bundle locally to frontend/dist/ (host anywhere)
  help            Show this help

Env (via environment or a .env file at repo root):
  Required: FLY_ORG, LOGTO_ISSUER, DATABASE_URL,
            LOGTO_AUDIENCE, FRONTEND_URL
  Fly auth: run 'fly auth login' (recommended). Set FLY_API_TOKEN only for
            non-interactive/CI deploys; otherwise it is derived from the
            logged-in session and still set as a runtime secret.
  Seed (once, for logto-setup): LOGTO_M2M_APP_ID, LOGTO_M2M_APP_SECRET
  Auto-set by logto-setup: LOGTO_APP_ID
  Optional: FLY_APP (default cloudsandbox-api), FLY_WEB_APP (default
            cloudsandbox-web), NEON_REGION, NEON_PROJECT_NAME,
            LOGTO_SPA_APP_NAME (default "Cloud Sandbox"), ENV_FILE

The script is idempotent: each step guards its precondition, so 'all' can be
re-run safely after fixing a failing step.
EOF
}

cmd="${1:-all}"
load_env
case "$cmd" in
  all)
    load_env
    # preflight auto-creates the Neon project when DATABASE_URL is blank, so
    # every command (all/fly/frontend) shares the same fallback.
    preflight
    if [ -n "${LOGTO_M2M_APP_ID:-}" ] && [ -n "${LOGTO_M2M_APP_SECRET:-}" ]; then
      logto_setup
    else
      logto_checklist
    fi
    api_url="$(fly_deploy)"
    web_deploy "$api_url"
    section "Done"
    ok "Sign in at https://${FLY_WEB_APP:-cloudsandbox-web}.fly.dev (or your FRONTEND_URL)."
    ;;
  preflight)    preflight ;;
  neon-create)  neon_create ;;
  logto)        load_env; require_env LOGTO_ISSUER; require_env FRONTEND_URL; require_env LOGTO_AUDIENCE; logto_checklist ;;
  logto-setup)  load_env; logto_setup ;;
  fly)          preflight; fly_deploy ;;
  web)          preflight; web_deploy ;;
  frontend)     preflight; frontend_build ;;
  help|-h|--help) usage ;;
  *) err "unknown command: $cmd"; usage; exit 1 ;;
esac

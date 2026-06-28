# ---------------------------------------------------------------------------
# Neon Serverless Postgres
# ---------------------------------------------------------------------------
# One project with a custom default branch + role + database, and a scale-to-zero
# compute endpoint. The Management API uses the *direct* connection_uri (it runs
# its own pgxpool, so we avoid Neon's pooler to prevent double-pooling).

resource "neon_project" "cloudsandbox" {
  name      = var.neon_project_name
  region_id = var.neon_region
  org_id    = var.neon_org_id

  branch {
    name          = "main"
    database_name = var.neon_db_name
    role_name     = var.neon_db_name
  }

  # Neon scale-to-zero: suspend the compute after 5 minutes of inactivity.
  default_endpoint_settings {
    autoscaling_limit_min_cu = 0.25
    autoscaling_limit_max_cu = 0.5
    suspend_timeout_seconds  = 300
  }
}

# ---------------------------------------------------------------------------
# Logto (OIDC IdP)
# ---------------------------------------------------------------------------
# The API resource = the audience the Management API validates (LOGTO_AUDIENCE).
resource "logto_api_resource" "management_api" {
  name             = "Cloud Sandbox Management API"
  indicator        = var.logto_resource_indicator
  access_token_ttl = 3600
}

# The SPA the React frontend authenticates through. It is a first-party app, so
# it may request tokens for the resource indicator above without an explicit
# resource grant.
resource "logto_application" "frontend" {
  name        = "Cloud Sandbox Web"
  type        = "SPA"
  description = "React + Vite management UI"

  redirect_uris              = ["${var.frontend_origin}/callback"]
  post_logout_redirect_uris  = [var.frontend_origin]
  cors_allowed_origins       = [var.frontend_origin]
}

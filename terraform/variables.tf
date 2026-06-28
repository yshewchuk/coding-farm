# Input variables for the Cloud Sandbox deployment.
# Copy terraform.tfvars.example -> terraform.tfvars and fill in the non-defaults.
# Sensitive values (tokens, M2M secret) should be provided via environment
# variables or `terraform.tfvars` (which is gitignored), never committed.

# ---------------------------------------------------------------------------
# Fly.io (Management API host + workspace provisioner)
# ---------------------------------------------------------------------------
variable "fly_app" {
  description = "Name of the Fly app that runs the Management API. Each workspace session is a *separate* Fly app created on demand by the API; this is only the control-plane app."
  type        = string
}

variable "fly_org" {
  description = "Fly.io organization slug. Workspace apps are provisioned under this org."
  type        = string
}

variable "fly_region" {
  description = "Default Fly region for the Management API and for new workspace machines/volumes."
  type        = string
  default     = "iad"
}

variable "fly_api_token" {
  description = "Fly.io API token. Used (a) by the `fly` CLI for deploy and (b) as a Fly secret so the Management API can provision workspace machines. Set via the FLY_API_TOKEN env var."
  type        = string
  sensitive   = true
}

variable "deploy_sha" {
  description = "Change this (e.g. to a git commit SHA) to force a redeploy of the Management API via Terraform. Empty skips deploy."
  type        = string
  default     = ""
}

# ---------------------------------------------------------------------------
# Neon (Serverless Postgres)
# ---------------------------------------------------------------------------
variable "neon_project_name" {
  description = "Neon project name."
  type        = string
  default     = "cloudsandbox"
}

variable "neon_region" {
  description = "Neon region id, e.g. aws-us-east-1. Should be near var.fly_region for low latency."
  type        = string
  default     = "aws-us-east-1"
}

variable "neon_org_id" {
  description = "Neon organization id. Leave null to use the API key's default org (personal account)."
  type        = string
  default     = null
}

variable "neon_db_name" {
  description = "Name of the default database + role created on the Neon project."
  type        = string
  default     = "cloudsandbox"
}

# ---------------------------------------------------------------------------
# Logto (OIDC IdP)
# ---------------------------------------------------------------------------
variable "logto_hostname" {
  description = "Logto API hostname. Cloud: '<tenantId>.logto.app'. Self-hosted: your Logto origin (no trailing slash). Also used as LOGTO_ISSUER for the Management API."
  type        = string
}

variable "logto_mgmt_application_id" {
  description = "Client id of a Machine-to-Machine application you create once in Logto for Terraform to talk to the Management API. Set via LOGTO_APPLICATION_ID env var or here."
  type        = string
  sensitive   = true
}

variable "logto_mgmt_application_secret" {
  description = "Client secret of the Logto M2M application used by Terraform. Set via LOGTO_APPLICATION_SECRET env var or here."
  type        = string
  sensitive   = true
}

variable "logto_resource_indicator" {
  description = "The API resource indicator (audience) the Management API expects in access tokens. Becomes LOGTO_AUDIENCE and VITE_LOGTO_RESOURCE."
  type        = string
  default     = "https://api.cloudsandbox.example"
}

# ---------------------------------------------------------------------------
# Frontend / CORS
# ---------------------------------------------------------------------------
variable "frontend_origin" {
  description = "Public origin of the deployed React UI (no trailing slash). Used for CORS (FRONTEND_URL) and the SPA redirect URIs."
  type        = string
}

variable "workspace_port" {
  description = "Port exposed by workspace containers (code-server). Must match WORKSPACE_PORT the image listens on."
  type        = number
  default     = 8080
}

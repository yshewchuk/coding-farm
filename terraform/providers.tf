# Terraform providers for the Cloud Sandbox platform.
#
# Provider reality (as of writing):
#   - Neon   (kislerdm/neon): community-maintained, Neon-sponsored. Solid.
#   - Logto  (Lenstra/logto): community, barebones; supports application,
#     api_resource, role, user (NOT organizations).
#   - Fly.io: the official TF provider (fly-apps/fly) is ARCHIVED/unmaintained,
#     and Fly recommends the CLI/API. We therefore drive Fly through the `fly`
#     CLI via null_resource/local-exec (see fly.tf) instead of a dead provider.
#
# All sensitive provider credentials are read from environment variables so
# nothing secret is committed in *.tfvars.

terraform {
  required_version = ">= 1.5.0"

  required_providers {
    neon = {
      source  = "kislerdm/neon"
      version = ">= 0.7.0"
    }
    logto = {
      source  = "Lenstra/logto"
      version = ">= 0.0.15"
    }
    null = {
      source  = "hashicorp/null"
      version = "~> 3.2"
    }
  }
}

# Neon provider: reads NEON_API_KEY from the environment.
provider "neon" {}

# Logto provider: authenticates to the Logto Management API using a
# machine-to-machine application you create ONCE in Logto (see docs/DEPLOYMENT.md
# "Bootstrap"). For Logto Cloud, hostname is "<tenantId>.logto.app"; for
# self-hosted, hostname is your Logto origin.
provider "logto" {
  hostname           = var.logto_hostname
  application_id     = var.logto_mgmt_application_id
  application_secret = var.logto_mgmt_application_secret
}

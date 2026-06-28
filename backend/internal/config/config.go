package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration for the Management API. Every value is
// sourced from environment variables so the same binary can run locally, in CI,
// and on Fly.io without recompilation.
type Config struct {
	// HTTPAddr is the address the HTTP server binds to.
	HTTPAddr string

	// DatabaseURL is the Neon Postgres connection string used by the pgx pool.
	DatabaseURL string

	// FlyAPIToken is the Fly.io API token used to drive the Machines REST API.
	FlyAPIToken string

	// FlyAPIBaseURL is the base URL for the Fly Machines REST API.
	FlyAPIBaseURL string

	// FlyOrg is the Fly.io organization workspaces are provisioned under.
	FlyOrg string

	// FlyRegion is the default Fly region for new machines/volumes.
	FlyRegion string

	// LogtoIssuer is the Logto OIDC issuer URL (e.g. https://logto.example.com).
	LogtoIssuer string

	// LogtoJWKSURL is the full JWKS endpoint. If empty it is derived from the issuer.
	LogtoJWKSURL string

	// LogtoAudience is the expected audience (resource indicator) for access tokens.
	LogtoAudience string

	// LogtoOrgClaim is the JWT claim name carrying the requesting organization id.
	LogtoOrgClaim string

	// JWKSRefreshInterval controls how often the cached JWKS is refreshed.
	JWKSRefreshInterval time.Duration

	// FrontendURL is the public origin of the React UI, used for CORS.
	FrontendURL string

	// DefaultImageRef is used by the MVP provisioner when a template has not yet
	// been built. It allows an operator to ship a pre-built image.
	DefaultImageRef string

	// WorkspacePort is the port exposed by the workspace container (code-server).
	WorkspacePort int
}

// Load reads configuration from the environment, optionally loading a .env file
// when present. Required fields that are missing cause an error so misdeploys
// fail loudly at startup.
func Load() (Config, error) {
	_ = godotenv.Load() // best-effort; ignored when no .env exists

	cfg := Config{
		HTTPAddr:             env("HTTP_ADDR", ":8080"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		FlyAPIToken:          os.Getenv("FLY_API_TOKEN"),
		FlyAPIBaseURL:        env("FLY_API_BASE_URL", "https://api.machines.dev"),
		FlyOrg:               os.Getenv("FLY_ORG"),
		FlyRegion:            env("FLY_REGION", "iad"),
		LogtoIssuer:          strings.TrimRight(os.Getenv("LOGTO_ISSUER"), "/"),
		LogtoJWKSURL:         os.Getenv("LOGTO_JWKS_URL"),
		LogtoAudience:        os.Getenv("LOGTO_AUDIENCE"),
		LogtoOrgClaim:        env("LOGTO_ORG_CLAIM", "organization_id"),
		JWKSRefreshInterval:   envDuration("JWKS_REFRESH_INTERVAL", 15*time.Minute),
		FrontendURL:          env("FRONTEND_URL", "http://localhost:5173"),
		DefaultImageRef:      os.Getenv("DEFAULT_IMAGE_REF"),
		WorkspacePort:        envInt("WORKSPACE_PORT", 8080),
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.FlyAPIToken == "" {
		missing = append(missing, "FLY_API_TOKEN")
	}
	if c.FlyOrg == "" {
		missing = append(missing, "FLY_ORG")
	}
	if c.LogtoIssuer == "" {
		missing = append(missing, "LOGTO_ISSUER")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

// JWKSURL returns the JWKS endpoint, deriving it from the issuer when not set
// explicitly.
func (c Config) JWKSURL() string {
	if c.LogtoJWKSURL != "" {
		return c.LogtoJWKSURL
	}
	return c.LogtoIssuer + "/jwks"
}

// OIDCDiscoveryURL returns the standard OIDC discovery document URL.
func (c Config) OIDCDiscoveryURL() string {
	return c.LogtoIssuer + "/.well-known/openid-configuration"
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

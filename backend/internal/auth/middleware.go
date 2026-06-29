package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cloudsandbox/platform/internal/db"
	"github.com/cloudsandbox/platform/internal/models"
)

// AuthMiddleware intercepts the Authorization header, verifies the Logto JWT,
// and injects the verified Claims into the request context. Missing or invalid
// tokens receive a 401 JSON response. The Logto organization context is extracted but not
// yet resolved to a database row — that happens in ProvisioningMiddleware.
func AuthMiddleware(verifier Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				slog.Info("missing bearer token", "path", r.URL.Path)
				writeJSONError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			claims, err := verifier.Verify(r.Context(), raw)
			if err != nil {
				slog.Info("token verification failed", "path", r.URL.Path, "error", err)
				writeJSONError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
		})
	}
}

// Provisioner is the org/user provisioning capability the ProvisioningMiddleware
// and the identity handlers depend on. It is an interface (backed by
// db.OrgsRepo in production) so the HTTP layer can be tested with a fake.
type Provisioner interface {
	ProvisionSelfHostedAdmin(ctx context.Context, logtoID, email string) (*models.User, *models.Organization, error)
	EnsureOrgForClaim(ctx context.Context, orgLogtoID, userID, name string) (*models.Organization, error)
	EnsureUser(ctx context.Context, logtoID, email string) (*models.User, error)
	ListMemberships(ctx context.Context, userID string) ([]db.Membership, error)
}

// ProvisioningMiddleware resolves the verified Claims to a database Identity. It
// is the "sign-up" step: the first authenticated request auto-provisions a
// local user and a personal organization (MVP single-admin mode) or ensures an
// organization carried in the token claim (multi-tenant mode). The resolved
// internal UserID and OrgID are injected for handlers to enforce ownership.
func ProvisioningMiddleware(orgs Provisioner) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			identity, err := resolveIdentity(r.Context(), orgs, claims)
			if err != nil {
				slog.Error("identity provisioning failed", "error", err, "subject", claims.Subject, "organization_id", claims.OrganizationID)
				writeJSONError(w, http.StatusInternalServerError, "identity provisioning failed")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
		})
	}
}

// resolveIdentity maps verified claims to an internal Identity, provisioning
// database rows as needed. This is the authorization gatekeeper's setup phase:
// after this, handlers only need to check ownership of resources.
func resolveIdentity(ctx context.Context, orgs Provisioner, claims Claims) (Identity, error) {
	if claims.OrganizationID != "" {
		// Multi-tenant: a Logto organization token. Ensure the user + org
		// exist and the user is a member.
		user, err := orgs.EnsureUser(ctx, claims.Subject, claims.Email)
		if err != nil {
			return Identity{}, err
		}
		org, err := orgs.EnsureOrgForClaim(ctx, claims.OrganizationID, user.ID, "")
		if err != nil {
			return Identity{}, err
		}
		return Identity{UserID: user.ID, OrgID: org.ID, Email: user.Email}, nil
	}

	// MVP single-admin: provision a personal organization for the user.
	user, org, err := orgs.ProvisionSelfHostedAdmin(ctx, claims.Subject, claims.Email)
	if err != nil {
		return Identity{}, err
	}
	return Identity{UserID: user.ID, OrgID: org.ID, Email: user.Email}, nil
}

// bearerToken extracts the token from an "Authorization: Bearer <t>" header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, prefix))
}

// Package auth implements Logto JWT verification and request-scoped identity
// injection for the Management API. The Go API is the sole authorization
// gatekeeper: Logto authenticates the user and the API enforces ownership.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// errorResponse is the canonical error envelope returned by all handlers.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSONError writes an error envelope as JSON.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: msg})
}

// ctxKey is an unexported type so callers cannot collide with our context keys.
type ctxKey int

const (
	keyClaims ctxKey = iota
	keyIdentity
)

// Claims are the verified identity fields extracted from a Logto access token.
// They carry only what Logto attests; database identity resolution happens in
// the provisioning middleware.
type Claims struct {
	// Subject is the Logto user id (the JWT `sub`).
	Subject string
	// Issuer is the JWT `iss`.
	Issuer string
	// Audience is the JWT `aud`.
	Audience string
	// Email, when present in a standard `email` claim.
	Email string
	// OrganizationID is the Logto organization id carried in the configured
	// organization claim (multi-tenant tokens). Empty for personal tokens.
	OrganizationID string
	// Raw is the original token string, useful for downstream call forwarding.
	Raw string
}

// Identity is the database-resolved principal injected into request context
// after provisioning. Handlers use this for authorization and ownership checks.
type Identity struct {
	// UserID is the internal users.id (uuid).
	UserID string
	// OrgID is the internal organizations.id (uuid) the request acts within.
	OrgID string
	// Email mirror of the user, for display.
	Email string
}

// WithClaims returns a copy of ctx carrying the verified claims.
func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, keyClaims, c)
}

// ClaimsFromContext retrieves the verified claims, if present.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(keyClaims).(Claims)
	return c, ok
}

// WithIdentity returns a copy of ctx carrying the resolved identity.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, keyIdentity, id)
}

// IdentityFromContext retrieves the resolved identity, if present.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(keyIdentity).(Identity)
	return id, ok
}

// RequireIdentity returns the identity from context or writes a 401 JSON response and returns
// ok=false. This is the helper handlers use at the top of each request.
func RequireIdentity(w http.ResponseWriter, r *http.Request) (Identity, bool) {
	id, ok := IdentityFromContext(r.Context())
	if !ok {
		slog.Info("request missing identity context", "path", r.URL.Path)
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return Identity{}, false
	}
	return id, true
}

// ErrNoClaims is returned when no verified claims are present.
var ErrNoClaims = errors.New("no verified claims in context")

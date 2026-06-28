package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// Verifier validates a raw access token and returns its claims. It is an
// interface so the JWT verification layer can be fully mocked in tests.
type Verifier interface {
	// Verify parses and validates the token, enforcing signature, issuer,
	// audience (when configured), and expiration.
	Verify(ctx context.Context, raw string) (Claims, error)
}

// JWKSVerifier validates Logto access tokens against the IdP's JWKS endpoint.
// The key set is cached in memory and refreshed on a configurable interval so
// key rotation does not require a process restart.
type JWKSVerifier struct {
	jwksURL     string
	issuer      string
	audience    string
	orgClaim    string
	refresh     time.Duration

	mu      sync.RWMutex
	current jwk.Set
	fetch   func(ctx context.Context, url string, options ...jwk.FetchOption) (jwk.Set, error)
}

// NewJWKSVerifier performs an initial JWKS fetch and starts a background
// refresher. The fetch function is parameterized for testability.
func NewJWKSVerifier(ctx context.Context, jwksURL, issuer, audience, orgClaim string, refresh time.Duration) (*JWKSVerifier, error) {
	v := &JWKSVerifier{
		jwksURL:  jwksURL,
		issuer:   issuer,
		audience: audience,
		orgClaim: orgClaim,
		refresh:  refresh,
		fetch:    jwk.Fetch,
	}
	if err := v.refreshKeys(ctx); err != nil {
		return nil, fmt.Errorf("initial jwks fetch: %w", err)
	}
	go v.refreshLoop(context.Background())
	return v, nil
}

func (v *JWKSVerifier) refreshLoop(ctx context.Context) {
	if v.refresh <= 0 {
		return
	}
	t := time.NewTicker(v.refresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = v.refreshKeys(ctx)
		}
	}
}

func (v *JWKSVerifier) refreshKeys(ctx context.Context) error {
	set, err := v.fetch(ctx, v.jwksURL)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.current = set
	v.mu.Unlock()
	return nil
}

func (v *JWKSVerifier) keys() jwk.Set {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.current
}

// Verify implements Verifier.
func (v *JWKSVerifier) Verify(ctx context.Context, raw string) (Claims, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Claims{}, ErrInvalidToken
	}

	opts := []jwt.ParseOption{
		jwt.WithKeySet(v.keys()),
		jwt.WithValidate(true),
	}
	if v.issuer != "" {
		opts = append(opts, jwt.WithIssuer(v.issuer))
	}
	if v.audience != "" {
		opts = append(opts, jwt.WithAudience(v.audience))
	}

	tok, err := jwt.Parse([]byte(raw), opts...)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	c := Claims{Raw: raw}
	c.Subject = stringClaim(tok, "sub")
	c.Issuer = stringClaim(tok, "iss")
	c.Email = stringClaim(tok, "email")
	if v.orgClaim != "" {
		c.OrganizationID = stringClaim(tok, v.orgClaim)
	}
	if c.Subject == "" {
		return Claims{}, fmt.Errorf("%w: missing subject", ErrInvalidToken)
	}
	return c, nil
}

// ErrInvalidToken is the sentinel returned for any token validation failure.
var ErrInvalidToken = errors.New("invalid token")

// stringClaim reads a string-valued claim from a parsed token. jwx v3's
// Get(name, &dst) populates a typed destination and returns an error when the
// claim is absent or of a different type; we fall back to a generic decode so
// non-string claims degrade gracefully instead of failing the whole request.
func stringClaim(tok jwt.Token, name string) string {
	var s string
	if err := tok.Get(name, &s); err == nil {
		return s
	}
	var v any
	if err := tok.Get(name, &v); err == nil {
		return fmt.Sprint(v)
	}
	return ""
}

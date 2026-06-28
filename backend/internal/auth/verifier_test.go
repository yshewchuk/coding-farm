package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// jwkKey / jwksDoc let us hand-build a valid JWKS JSON document using only the
// standard library, so this test does not depend on the jwk key-import API
// surface (which differs across jwx minor versions). The real verifier still
// parses this with jwk.Fetch, exercising the production verification path.
type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid,omitempty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

func rsaPublicJWKS(t *testing.T, pub *rsa.PublicKey, kid string) []byte {
	t.Helper()
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	doc := jwksDoc{Keys: []jwkKey{{
		Kty: "RSA", Kid: kid, Alg: "RS256", Use: "sig", N: n, E: e,
	}}}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}
	return b
}

// setupVerifier provisions an RSA signing key, serves its public JWKS over HTTP,
// and returns a real JWKSVerifier plus a signer that mints tokens for the key.
func setupVerifier(t *testing.T, issuer, audience, orgClaim string) (*JWKSVerifier, func(claims map[string]any, exp time.Time) string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen: %v", err)
	}
	const kid = "test-key-1"
	jwksJSON := rsaPublicJWKS(t, &priv.PublicKey, kid)

	// Import the private key as a jwk.Key and attach the kid so that jwt.Sign
	// emits the kid in the protected header, which jwx v3 requires to match a
	// key in the JWKS during verification.
	privJWK, err := jwk.Import(priv)
	if err != nil {
		t.Fatalf("jwk.Import private key: %v", err)
	}
	if err := privJWK.Set(jwk.KeyIDKey, kid); err != nil {
		t.Fatalf("set kid: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	}))
	t.Cleanup(srv.Close)

	// refresh=0 disables the background refresher; we only need the initial fetch.
	v, err := NewJWKSVerifier(context.Background(), srv.URL, issuer, audience, orgClaim, 0)
	if err != nil {
		t.Fatalf("NewJWKSVerifier: %v", err)
	}

	sign := func(claims map[string]any, exp time.Time) string {
		b := jwt.NewBuilder()
		for k, val := range claims {
			b.Claim(k, val)
		}
		b.Expiration(exp)
		b.IssuedAt(time.Now())
		tok, err := b.Build()
		if err != nil {
			t.Fatalf("build token: %v", err)
		}
		raw, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256(), privJWK))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		return string(raw)
	}
	return v, sign
}

func TestJWKSVerifier_ValidToken(t *testing.T) {
	v, sign := setupVerifier(t, "https://logto.example.com", "", "organization_id")
	raw := sign(map[string]any{
		"sub":             "user-123",
		"iss":             "https://logto.example.com",
		"email":           "admin@example.com",
		"organization_id": "org-logto-abc",
	}, time.Now().Add(1*time.Hour))

	claims, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("subject = %s", claims.Subject)
	}
	if claims.Email != "admin@example.com" {
		t.Errorf("email = %s", claims.Email)
	}
	if claims.OrganizationID != "org-logto-abc" {
		t.Errorf("org = %s", claims.OrganizationID)
	}
	if claims.Issuer != "https://logto.example.com" {
		t.Errorf("issuer = %s", claims.Issuer)
	}
}

func TestJWKSVerifier_OrgClaimConfigurable(t *testing.T) {
	v, sign := setupVerifier(t, "https://logto.example.com", "", "https://acme/claims/org_id")
	raw := sign(map[string]any{
		"sub": "u1",
		"iss": "https://logto.example.com",
		"https://acme/claims/org_id": "tenant-9",
	}, time.Now().Add(time.Hour))
	claims, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.OrganizationID != "tenant-9" {
		t.Errorf("org = %s, want tenant-9", claims.OrganizationID)
	}
}

func TestJWKSVerifier_ExpiredToken(t *testing.T) {
	v, sign := setupVerifier(t, "https://logto.example.com", "", "organization_id")
	raw := sign(map[string]any{"sub": "u1", "iss": "https://logto.example.com"}, time.Now().Add(-1*time.Hour))
	_, err := v.Verify(context.Background(), raw)
	if !strings.Contains(err.Error(), "invalid token") && err != ErrInvalidToken {
		t.Fatalf("err = %v, want invalid token", err)
	}
}

func TestJWKSVerifier_WrongIssuer(t *testing.T) {
	v, sign := setupVerifier(t, "https://logto.example.com", "", "organization_id")
	raw := sign(map[string]any{"sub": "u1", "iss": "https://evil.example.com"}, time.Now().Add(time.Hour))
	if _, err := v.Verify(context.Background(), raw); err == nil {
		t.Fatal("expected issuer validation error")
	}
}

func TestJWKSVerifier_AudienceEnforced(t *testing.T) {
	v, sign := setupVerifier(t, "https://logto.example.com", "api.cloudsandbox.io", "organization_id")
	raw := sign(map[string]any{
		"sub": "u1",
		"iss": "https://logto.example.com",
		"aud": "api.cloudsandbox.io",
	}, time.Now().Add(time.Hour))
	if _, err := v.Verify(context.Background(), raw); err != nil {
		t.Fatalf("Verify with matching audience: %v", err)
	}

	// A token without the expected audience should be rejected.
	bad := sign(map[string]any{"sub": "u1", "iss": "https://logto.example.com"}, time.Now().Add(time.Hour))
	if _, err := v.Verify(context.Background(), bad); err == nil {
		t.Fatal("expected audience mismatch error")
	}
}

func TestJWKSVerifier_TamperedSignature(t *testing.T) {
	v, sign := setupVerifier(t, "https://logto.example.com", "", "organization_id")
	raw := sign(map[string]any{"sub": "u1", "iss": "https://logto.example.com"}, time.Now().Add(time.Hour))

	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 jwt parts, got %d", len(parts))
	}
	// Flip a character in the middle of the signature so the tamper changes
	// actual signature bytes (the trailing base64 char may only hold padding
	// bits, which is not part of the signature).
	sig := parts[2]
	if len(sig) < 4 {
		t.Fatalf("signature too short to tamper")
	}
	idx := len(sig) / 2
	c := sig[idx]
	if c == 'A' {
		c = 'B'
	} else {
		c = 'A'
	}
	parts[2] = sig[:idx] + string(c) + sig[idx+1:]
	tampered := strings.Join(parts, ".")
	if _, err := v.Verify(context.Background(), tampered); err == nil {
		t.Fatal("expected signature verification failure")
	}
}

func TestJWKSVerifier_EmptyToken(t *testing.T) {
	v, _ := setupVerifier(t, "https://logto.example.com", "", "organization_id")
	if _, err := v.Verify(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestJWKSVerifier_MissingSubject(t *testing.T) {
	v, sign := setupVerifier(t, "https://logto.example.com", "", "organization_id")
	raw := sign(map[string]any{"iss": "https://logto.example.com"}, time.Now().Add(time.Hour))
	_, err := v.Verify(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error for missing subject")
	}
	if !strings.Contains(fmt.Sprint(err), "subject") {
		t.Fatalf("err = %v, want subject message", err)
	}
}

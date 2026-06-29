package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cloudsandbox/platform/internal/models"
	"github.com/google/uuid"
)

// Membership links a user to an organization with a role.
type Membership struct {
	OrgID string
	Role  models.Role
	Org   models.Organization
}

// OrgsRepo manages organizations, users, and their memberships.
type OrgsRepo struct {
	ex Executor
}

// NewOrgsRepo returns an OrgsRepo bound to the given executor.
func NewOrgsRepo(ex Executor) *OrgsRepo {
	return &OrgsRepo{ex: ex}
}

// EnsureUser upserts a local user record keyed by Logto sub. This is the
// "sign-up" step for an administrator: the first time a verified JWT is seen,
// the user is provisioned automatically.
func (r *OrgsRepo) EnsureUser(ctx context.Context, logtoID, email string) (*models.User, error) {
	if logtoID == "" {
		return nil, errors.New("logto id required")
	}
	id := uuid.NewString()
	const q = `
		INSERT INTO users (id, logto_id, email)
		VALUES ($1, $2, $3)
		ON CONFLICT (logto_id) DO UPDATE SET email = COALESCE(EXCLUDED.email, users.email)
		RETURNING id, logto_id, email, created_at
	`
	var u models.User
	var emailNull sql.NullString
	err := r.ex.QueryRow(ctx, q, id, logtoID, nullable(email)).Scan(
		&u.ID, &u.LogtoID, &emailNull, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("ensure user: %w", err)
	}
	u.Email = emailNull.String
	return &u, nil
}

// EnsureOrganization upserts an organization keyed by its Logto id.
func (r *OrgsRepo) EnsureOrganization(ctx context.Context, logtoID, name string) (*models.Organization, error) {
	if logtoID == "" {
		return nil, errors.New("organization logto id required")
	}
	id := uuid.NewString()
	const q = `
		INSERT INTO organizations (id, logto_id, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (logto_id) DO UPDATE SET name = COALESCE(EXCLUDED.name, organizations.name)
		RETURNING id, logto_id, name, created_at
	`
	var o models.Organization
	err := r.ex.QueryRow(ctx, q, id, logtoID, name).Scan(
		&o.ID, &o.LogtoID, &o.Name, &o.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("ensure organization: %w", err)
	}
	return &o, nil
}

// EnsureMembership upserts a user's membership in an organization.
func (r *OrgsRepo) EnsureMembership(ctx context.Context, userID, orgID string, role models.Role) error {
	const q = `
		INSERT INTO organization_memberships (user_id, org_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, org_id) DO UPDATE SET role = EXCLUDED.role
	`
	_, err := r.ex.Exec(ctx, q, userID, orgID, string(role))
	if err != nil {
		return fmt.Errorf("ensure membership: %w", err)
	}
	return nil
}

// ProvisionSelfHostedAdmin performs the MVP "sign-up" flow: ensures a user
// exists, ensures a personal organization exists for that user, and grants the
// user an admin role on it. It returns the user and their default organization.
//
// This intentionally runs as a sequence of idempotent upserts rather than a
// single transaction so it remains safe to call on every authenticated request
// without holding locks.
func (r *OrgsRepo) ProvisionSelfHostedAdmin(ctx context.Context, logtoID, email string) (*models.User, *models.Organization, error) {
	user, err := r.EnsureUser(ctx, logtoID, email)
	if err != nil {
		return nil, nil, err
	}
	orgLogtoID := "personal:" + logtoID
	orgName := email
	if orgName == "" {
		orgName = "Personal"
	}
	org, err := r.EnsureOrganization(ctx, orgLogtoID, orgName+"'s workspace")
	if err != nil {
		return nil, nil, err
	}
	if err := r.EnsureMembership(ctx, user.ID, org.ID, models.RoleAdmin); err != nil {
		return nil, nil, err
	}
	return user, org, nil
}

// EnsureOrgForClaim ensures an organization exists for a Logto organization id
// found in a JWT claim (multi-tenant mode). The requesting user is granted
// membership so they can act on that org.
func (r *OrgsRepo) EnsureOrgForClaim(ctx context.Context, orgLogtoID, userID, name string) (*models.Organization, error) {
	orgName := name
	if orgName == "" {
		orgName = "Organization " + orgLogtoID
	}
	org, err := r.EnsureOrganization(ctx, orgLogtoID, orgName)
	if err != nil {
		return nil, err
	}
	if err := r.EnsureMembership(ctx, userID, org.ID, models.RoleMember); err != nil {
		return nil, err
	}
	return org, nil
}

// IsMember reports whether the user belongs to the organization.
func (r *OrgsRepo) IsMember(ctx context.Context, userID, orgID string) (bool, error) {
	var ok bool
	err := r.ex.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM organization_memberships WHERE user_id = $1 AND org_id = $2)`,
		userID, orgID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return ok, nil
}

// ListMemberships returns every organization a user belongs to, with role.
func (r *OrgsRepo) ListMemberships(ctx context.Context, userID string) ([]Membership, error) {
	rows, err := r.ex.Query(ctx, `
		SELECT m.role, o.id, o.logto_id, o.name, o.created_at
		FROM organization_memberships m
		JOIN organizations o ON o.id = m.org_id
		WHERE m.user_id = $1
		ORDER BY o.created_at
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	defer rows.Close()

	var out []Membership
	for rows.Next() {
		var m Membership
		m.Org = models.Organization{}
		if err := rows.Scan(&m.Role, &m.Org.ID, &m.Org.LogtoID, &m.Org.Name, &m.Org.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetOrganization returns an organization by its internal id.
func (r *OrgsRepo) GetOrganization(ctx context.Context, id string) (*models.Organization, error) {
	var o models.Organization
	err := r.ex.QueryRow(ctx,
		`SELECT id, logto_id, name, created_at FROM organizations WHERE id = $1`, id).
		Scan(&o.ID, &o.LogtoID, &o.Name, &o.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}
	return &o, nil
}

// nullable returns a pointer for non-empty strings, else nil (so Postgres
// stores NULL rather than an empty string).
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

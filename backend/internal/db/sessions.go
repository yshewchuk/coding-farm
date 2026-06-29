package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cloudsandbox/platform/internal/models"
	"github.com/google/uuid"
)

// SessionsRepo manages workspace sessions.
type SessionsRepo struct {
	ex Executor
}

// NewSessionsRepo returns a SessionsRepo bound to the given executor.
func NewSessionsRepo(ex Executor) *SessionsRepo {
	return &SessionsRepo{ex: ex}
}

// Create inserts a new pending session owned by a user within an organization.
func (r *SessionsRepo) Create(ctx context.Context, orgID, userID, templateID, name string) (*models.Session, error) {
	id := uuid.NewString()
	if name == "" {
		name = "workspace-" + id[:8]
	}
	const q = `
		INSERT INTO sessions (id, org_id, user_id, template_id, name, status)
		VALUES ($1, $2, $3, $4, $5, 'pending')
		RETURNING id, org_id, user_id, template_id, name, status, fly_machine_id, fly_app_name, fly_volume_id, url, region, created_at, updated_at
	`
	var s models.Session
	var flyMachineID, flyAppName, flyVolumeID, url, region sql.NullString
	err := r.ex.QueryRow(ctx, q, id, orgID, userID, templateID, name).Scan(
		&s.ID, &s.OrgID, &s.UserID, &s.TemplateID, &s.Name, &s.Status,
		&flyMachineID, &flyAppName, &flyVolumeID, &url, &region, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	s.FlyMachineID = flyMachineID.String
	s.FlyAppName = flyAppName.String
	s.FlyVolumeID = flyVolumeID.String
	s.URL = url.String
	s.Region = region.String
	return &s, nil
}

// Get returns a session by id, scoped to the owning organization.
func (r *SessionsRepo) Get(ctx context.Context, orgID, id string) (*models.Session, error) {
	var s models.Session
	var flyMachineID, flyAppName, flyVolumeID, url, region sql.NullString
	err := r.ex.QueryRow(ctx, `
		SELECT id, org_id, user_id, template_id, name, status, fly_machine_id, fly_app_name, fly_volume_id, url, region, created_at, updated_at
		FROM sessions WHERE id = $1 AND org_id = $2
	`, id, orgID).Scan(
		&s.ID, &s.OrgID, &s.UserID, &s.TemplateID, &s.Name, &s.Status,
		&flyMachineID, &flyAppName, &flyVolumeID, &url, &region, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	s.FlyMachineID = flyMachineID.String
	s.FlyAppName = flyAppName.String
	s.FlyVolumeID = flyVolumeID.String
	s.URL = url.String
	s.Region = region.String
	return &s, nil
}

// List returns all sessions visible to a user within an organization.
func (r *SessionsRepo) List(ctx context.Context, orgID, userID string) ([]models.Session, error) {
	rows, err := r.ex.Query(ctx, `
		SELECT id, org_id, user_id, template_id, name, status, fly_machine_id, fly_app_name, fly_volume_id, url, region, created_at, updated_at
		FROM sessions WHERE org_id = $1 AND user_id = $2 ORDER BY created_at DESC
	`, orgID, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []models.Session
	for rows.Next() {
		var s models.Session
		var flyMachineID, flyAppName, flyVolumeID, url, region sql.NullString
		if err := rows.Scan(&s.ID, &s.OrgID, &s.UserID, &s.TemplateID, &s.Name, &s.Status,
			&flyMachineID, &flyAppName, &flyVolumeID, &url, &region, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		s.FlyMachineID = flyMachineID.String
		s.FlyAppName = flyAppName.String
		s.FlyVolumeID = flyVolumeID.String
		s.URL = url.String
		s.Region = region.String
		out = append(out, s)
	}
	return out, rows.Err()
}

// Provisioned updates a session with the Fly resources created for it.
func (r *SessionsRepo) Provisioned(ctx context.Context, id string, p ProvisionedFields) error {
	_, err := r.ex.Exec(ctx, `
		UPDATE sessions
		SET status = $2, fly_machine_id = $3, fly_app_name = $4, fly_volume_id = $5, url = $6, region = $7, updated_at = now()
		WHERE id = $1
	`, id, string(p.Status), nullable(p.MachineID), nullable(p.AppName), nullable(p.VolumeID), nullable(p.URL), nullable(p.Region))
	if err != nil {
		return fmt.Errorf("mark session provisioned: %w", err)
	}
	return nil
}

// ProvisionedFields carries the Fly resource identifiers written back to a
// session after orchestration.
type ProvisionedFields struct {
	Status    models.SessionStatus
	MachineID string
	AppName   string
	VolumeID  string
	URL       string
	Region    string
}

// SetStatus updates only the lifecycle status of a session.
func (r *SessionsRepo) SetStatus(ctx context.Context, id string, status models.SessionStatus) error {
	_, err := r.ex.Exec(ctx, `UPDATE sessions SET status = $2, updated_at = now() WHERE id = $1`, id, string(status))
	if err != nil {
		return fmt.Errorf("set session status: %w", err)
	}
	return nil
}

// Delete removes a session record.
func (r *SessionsRepo) Delete(ctx context.Context, orgID, id string) error {
	tag, err := r.ex.Exec(ctx, `DELETE FROM sessions WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

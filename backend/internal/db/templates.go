package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudsandbox/platform/internal/models"
	"github.com/google/uuid"
)

// ErrNotFound is returned by repositories when a single-row lookup misses.
var ErrNotFound = errors.New("not found")

// TemplatesRepo manages workspace templates.
type TemplatesRepo struct {
	ex Executor
}

// NewTemplatesRepo returns a TemplatesRepo bound to the given executor.
func NewTemplatesRepo(ex Executor) *TemplatesRepo {
	return &TemplatesRepo{ex: ex}
}

// Create inserts a new template owned by an organization.
func (r *TemplatesRepo) Create(ctx context.Context, orgID string, in models.CreateTemplateInput) (*models.Template, error) {
	if in.Name == "" {
		return nil, errors.New("template name required")
	}
	if in.DockerfileContents == "" {
		return nil, errors.New("dockerfile contents required")
	}
	id := uuid.NewString()
	const q = `
		INSERT INTO templates (id, org_id, name, dockerfile_contents, status)
		VALUES ($1, $2, $3, $4, 'draft')
		RETURNING id, org_id, name, dockerfile_contents, status, image_ref, fly_app_name, created_at, updated_at
	`
	var t models.Template
	err := r.ex.QueryRow(ctx, q, id, orgID, in.Name, in.DockerfileContents).Scan(
		&t.ID, &t.OrgID, &t.Name, &t.DockerfileContents, &t.Status, &t.ImageRef, &t.FlyAppName, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	return &t, nil
}

// Get returns a template by id, scoped to the owning organization.
func (r *TemplatesRepo) Get(ctx context.Context, orgID, id string) (*models.Template, error) {
	var t models.Template
	err := r.ex.QueryRow(ctx, `
		SELECT id, org_id, name, dockerfile_contents, status, image_ref, fly_app_name, created_at, updated_at
		FROM templates WHERE id = $1 AND org_id = $2
	`, id, orgID).Scan(
		&t.ID, &t.OrgID, &t.Name, &t.DockerfileContents, &t.Status, &t.ImageRef, &t.FlyAppName, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}
	return &t, nil
}

// List returns all templates owned by an organization.
func (r *TemplatesRepo) List(ctx context.Context, orgID string) ([]models.Template, error) {
	rows, err := r.ex.Query(ctx, `
		SELECT id, org_id, name, dockerfile_contents, status, image_ref, fly_app_name, created_at, updated_at
		FROM templates WHERE org_id = $1 ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	var out []models.Template
	for rows.Next() {
		var t models.Template
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Name, &t.DockerfileContents, &t.Status, &t.ImageRef, &t.FlyAppName, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan template: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpdateBuildStatus updates the build status, image ref, and fly app name for a
// template after a build attempt.
func (r *TemplatesRepo) UpdateBuildStatus(ctx context.Context, id string, status models.TemplateStatus, imageRef, flyAppName string) error {
	_, err := r.ex.Exec(ctx, `
		UPDATE templates
		SET status = $2, image_ref = $3, fly_app_name = $4, updated_at = now()
		WHERE id = $1
	`, id, string(status), nullable(imageRef), nullable(flyAppName))
	if err != nil {
		return fmt.Errorf("update build status: %w", err)
	}
	return nil
}

// Delete removes a template (cascades to sessions).
func (r *TemplatesRepo) Delete(ctx context.Context, orgID, id string) error {
	tag, err := r.ex.Exec(ctx, `DELETE FROM templates WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

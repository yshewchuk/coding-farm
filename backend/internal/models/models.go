// Package models defines the domain types shared across the Management API.
package models

import (
	"time"
)

// TemplateStatus enumerates the lifecycle of a workspace template image.
type TemplateStatus string

const (
	TemplateStatusDraft    TemplateStatus = "draft"
	TemplateStatusBuilding TemplateStatus = "building"
	TemplateStatusReady    TemplateStatus = "ready"
	TemplateStatusFailed   TemplateStatus = "failed"
)

// SessionStatus enumerates the lifecycle of a workspace session.
type SessionStatus string

const (
	SessionStatusPending   SessionStatus = "pending"
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusSuspended SessionStatus = "suspended"
	SessionStatusStopped   SessionStatus = "stopped"
	SessionStatusError     SessionStatus = "error"
)

// Role is the role a user holds within an organization.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleMember Role = "member"
)

// Organization mirrors a Logto organization. The Management API is the
// authorization gatekeeper; this row is a local cache of the IdP's tenant.
type Organization struct {
	ID        string    `json:"id"`
	LogtoID   string    `json:"logto_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// User mirrors a Logto user.
type User struct {
	ID        string    `json:"id"`
	LogtoID   string    `json:"logto_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// Template is a reusable workspace definition backed by a Dockerfile.
type Template struct {
	ID                string         `json:"id"`
	OrgID             string         `json:"org_id"`
	Name              string         `json:"name"`
	DockerfileContents string        `json:"dockerfile_contents"`
	Status            TemplateStatus `json:"status"`
	ImageRef          string         `json:"image_ref,omitempty"`
	FlyAppName        string         `json:"fly_app_name,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// Session is a single workspace instance bound to a Fly Machine.
type Session struct {
	ID            string        `json:"id"`
	OrgID         string        `json:"org_id"`
	UserID        string        `json:"user_id"`
	TemplateID    string        `json:"template_id"`
	Name          string        `json:"name"`
	Status        SessionStatus `json:"status"`
	FlyMachineID  string        `json:"fly_machine_id,omitempty"`
	FlyAppName    string        `json:"fly_app_name,omitempty"`
	FlyVolumeID   string        `json:"fly_volume_id,omitempty"`
	URL           string        `json:"url,omitempty"`
	Region        string        `json:"region,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// CreateTemplateInput is the request body for creating a template.
type CreateTemplateInput struct {
	Name               string `json:"name"`
	DockerfileContents string `json:"dockerfile_contents"`
}

// CreateSessionInput is the request body for creating a session.
type CreateSessionInput struct {
	TemplateID string `json:"template_id"`
	Name       string `json:"name,omitempty"`
}

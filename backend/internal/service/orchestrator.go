// Package service contains the business logic that orchestrates workspace
// lifecycles across the database and the Fly Machines API. It is the place
// where authorization decisions (org ownership) and Fly provisioning meet.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudsandbox/platform/internal/db"
	"github.com/cloudsandbox/platform/internal/fly"
	"github.com/cloudsandbox/platform/internal/models"
)

// templateStore is the subset of the templates repository the orchestrator uses.
type templateStore interface {
	Get(ctx context.Context, orgID, id string) (*models.Template, error)
	UpdateBuildStatus(ctx context.Context, id string, status models.TemplateStatus, imageRef, flyAppName string) error
}

// sessionStore is the subset of the sessions repository the orchestrator uses.
type sessionStore interface {
	Create(ctx context.Context, orgID, userID, templateID, name string) (*models.Session, error)
	Get(ctx context.Context, orgID, id string) (*models.Session, error)
	Provisioned(ctx context.Context, id string, p db.ProvisionedFields) error
	SetStatus(ctx context.Context, id string, status models.SessionStatus) error
	Delete(ctx context.Context, orgID, id string) error
}

// Sentinel errors. Handlers map these to HTTP status codes.
var (
	ErrTemplateNotReady = errors.New("template image is not ready; build it first")
	ErrSessionNotFound  = errors.New("session not found")
	ErrTemplateNotFound = errors.New("template not found")
)

// Orchestrator drives the full workspace lifecycle: it validates ownership,
// provisions Fly Volumes + Machines, and writes the resulting resource ids back
// to the database. Dependencies are interfaces so the whole thing is unit-
// testable with mocks.
type Orchestrator struct {
	templates     templateStore
	sessions      sessionStore
	machines      fly.MachinesAPI
	builder       fly.BuilderAPI
	region        string
	workspacePort int
	defaultImage  string
	appDomain     string
}

// NewOrchestrator wires an Orchestrator with its dependencies.
func NewOrchestrator(tmpl templateStore, sess sessionStore, machines fly.MachinesAPI, builder fly.BuilderAPI, region string, workspacePort int, defaultImage, appDomain string) *Orchestrator {
	if appDomain == "" {
		appDomain = "fly.dev"
	}
	return &Orchestrator{
		templates:     tmpl,
		sessions:      sess,
		machines:      machines,
		builder:       builder,
		region:        region,
		workspacePort: workspacePort,
		defaultImage:  defaultImage,
		appDomain:      appDomain,
	}
}

// appForSession derives a globally-unique, DNS-safe Fly App name from a session
// id. Each session becomes its own Fly App so it gets a unique scale-to-zero URL.
func appForSession(sessionID string) string {
	return "ws-" + slug(sessionID, 12)
}

// appForTemplate derives the Fly App name used only to build a template's image.
func appForTemplate(templateID string) string {
	return "tpl-" + slug(templateID, 12)
}

func volForSession(sessionID string) string {
	return "wsdata-" + slug(sessionID, 8)
}

// slug strips non [a-z0-9] characters from s and returns the first n chars.
func slug(s string, n int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() == n {
				break
			}
		}
	}
	return b.String()
}

func (o *Orchestrator) urlForApp(appName string) string {
	return "https://" + appName + "." + o.appDomain
}

// CreateSession provisions a new workspace from a template. It:
//  1. Verifies the template exists within the caller's org and is ready.
//  2. Creates a dedicated Fly App for the session (unique URL).
//  3. Provisions an NVME Fly Volume mounted at /workspace.
//  4. Creates a Firecracker machine booting the template image, exposing the
//     workspace port with autostop=suspend and autostart=true for scale-to-zero.
//  5. Writes the Fly resource ids back to the session row.
func (o *Orchestrator) CreateSession(ctx context.Context, orgID, userID, templateID, name string) (*models.Session, error) {
	tpl, err := o.templates.Get(ctx, orgID, templateID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrTemplateNotFound
		}
		return nil, fmt.Errorf("load template: %w", err)
	}

	imageRef := tpl.ImageRef
	if imageRef == "" {
		imageRef = o.defaultImage
	}
	if imageRef == "" {
		return nil, ErrTemplateNotReady
	}

	sess, err := o.sessions.Create(ctx, orgID, userID, templateID, name)
	if err != nil {
		return nil, fmt.Errorf("create session record: %w", err)
	}

	appName := appForSession(sess.ID)

	if err := o.builder.EnsureApp(ctx, appName); err != nil {
		o.failSession(ctx, sess.ID, orgID)
		return nil, fmt.Errorf("ensure fly app: %w", err)
	}

	vol, err := o.machines.CreateVolume(ctx, appName, fly.CreateVolumeRequest{
		Name:      volForSession(sess.ID),
		Region:    o.region,
		SizeGB:    1,
		Encrypted: true,
	})
	if err != nil {
		o.failSession(ctx, sess.ID, orgID)
		return nil, fmt.Errorf("create volume: %w", err)
	}

	machine, err := o.machines.CreateMachine(ctx, appName, fly.CreateMachineRequest{
		Name:   "ws-" + slug(sess.ID, 8),
		Region: o.region,
		Config: fly.MachineConfig{
			Image: imageRef,
			Guest: fly.Guest{CPUClass: "shared", CPUs: 1, MemoryMB: 512},
			Mounts: []fly.Mount{{
				Volume: vol.ID,
				Path:   "/workspace",
			}},
			Services: []fly.Service{{
				InternalPort: o.workspacePort,
				Protocol:     "tcp",
				Autostop:      "suspend",
				Autostart:     true,
			}},
			Env: map[string]string{
				"WORKSPACE": "/workspace",
			},
		},
	})
	if err != nil {
		// Best-effort cleanup of the volume we just created.
		_ = o.machines.DestroyVolume(ctx, appName, vol.ID)
		o.failSession(ctx, sess.ID, orgID)
		return nil, fmt.Errorf("create machine: %w", err)
	}

	if err := o.sessions.Provisioned(ctx, sess.ID, db.ProvisionedFields{
		Status:    models.SessionStatusRunning,
		MachineID: machine.ID,
		AppName:   appName,
		VolumeID:  vol.ID,
		URL:       o.urlForApp(appName),
		Region:    o.region,
	}); err != nil {
		return nil, fmt.Errorf("record provisioned session: %w", err)
	}

	return o.sessions.Get(ctx, orgID, sess.ID)
}

// ResumeSession starts a suspended machine.
func (o *Orchestrator) ResumeSession(ctx context.Context, orgID, id string) (*models.Session, error) {
	sess, err := o.sessions.Get(ctx, orgID, id)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	if sess.FlyAppName == "" || sess.FlyMachineID == "" {
		return nil, ErrSessionNotFound
	}
	if err := o.machines.StartMachine(ctx, sess.FlyAppName, sess.FlyMachineID); err != nil {
		return nil, fmt.Errorf("start machine: %w", err)
	}
	_ = o.sessions.SetStatus(ctx, sess.ID, models.SessionStatusRunning)
	return o.sessions.Get(ctx, orgID, sess.ID)
}

// HibernateSession stops a running machine. Combined with autostop=suspend this
// parks the workspace (volume retained) so it scales to zero.
func (o *Orchestrator) HibernateSession(ctx context.Context, orgID, id string) (*models.Session, error) {
	sess, err := o.sessions.Get(ctx, orgID, id)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	if sess.FlyAppName == "" || sess.FlyMachineID == "" {
		return nil, ErrSessionNotFound
	}
	if err := o.machines.StopMachine(ctx, sess.FlyAppName, sess.FlyMachineID); err != nil {
		return nil, fmt.Errorf("stop machine: %w", err)
	}
	_ = o.sessions.SetStatus(ctx, sess.ID, models.SessionStatusSuspended)
	return o.sessions.Get(ctx, orgID, sess.ID)
}

// DeleteSession destroys the machine and volume, then removes the session row.
// Fly resource cleanup is best-effort so a stuck machine never blocks deletion.
func (o *Orchestrator) DeleteSession(ctx context.Context, orgID, id string) error {
	sess, err := o.sessions.Get(ctx, orgID, id)
	if err != nil {
		return ErrSessionNotFound
	}
	if sess.FlyAppName != "" && sess.FlyMachineID != "" {
		_ = o.machines.DestroyMachine(ctx, sess.FlyAppName, sess.FlyMachineID)
	}
	if sess.FlyAppName != "" && sess.FlyVolumeID != "" {
		_ = o.machines.DestroyVolume(ctx, sess.FlyAppName, sess.FlyVolumeID)
	}
	return o.sessions.Delete(ctx, orgID, sess.ID)
}

// BuildTemplate builds the template's Dockerfile into an image in the Fly.io
// internal registry and records the resulting image ref. The template status
// transitions building -> ready (or failed).
func (o *Orchestrator) BuildTemplate(ctx context.Context, orgID, id string) (*models.Template, error) {
	tpl, err := o.templates.Get(ctx, orgID, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrTemplateNotFound
		}
		return nil, fmt.Errorf("load template: %w", err)
	}

	appName := tpl.FlyAppName
	if appName == "" {
		appName = appForTemplate(tpl.ID)
	}

	if err := o.templates.UpdateBuildStatus(ctx, tpl.ID, models.TemplateStatusBuilding, tpl.ImageRef, appName); err != nil {
		return nil, fmt.Errorf("mark building: %w", err)
	}

	if err := o.builder.EnsureApp(ctx, appName); err != nil {
		_ = o.templates.UpdateBuildStatus(ctx, tpl.ID, models.TemplateStatusFailed, "", appName)
		return nil, fmt.Errorf("ensure fly app: %w", err)
	}

	imageRef, err := o.builder.BuildImage(ctx, appName, tpl.DockerfileContents)
	if err != nil {
		_ = o.templates.UpdateBuildStatus(ctx, tpl.ID, models.TemplateStatusFailed, "", appName)
		return nil, fmt.Errorf("build image: %w", err)
	}

	if err := o.templates.UpdateBuildStatus(ctx, tpl.ID, models.TemplateStatusReady, imageRef, appName); err != nil {
		return nil, fmt.Errorf("mark ready: %w", err)
	}
	return o.templates.Get(ctx, orgID, tpl.ID)
}

// failSession marks a session as error after a provisioning failure.
func (o *Orchestrator) failSession(ctx context.Context, id, orgID string) {
	_ = o.sessions.SetStatus(ctx, id, models.SessionStatusError)
}

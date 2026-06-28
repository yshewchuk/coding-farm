package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cloudsandbox/platform/internal/db"
	"github.com/cloudsandbox/platform/internal/fly"
	"github.com/cloudsandbox/platform/internal/models"
	"github.com/google/uuid"
)

// --- Mock Fly Machines + Builder client -------------------------------------

type mockFly struct {
	mu sync.Mutex

	// Configurable return values.
	volID      string
	machineID  string
	imageRef   string
	// errorMode controls per-method failures: method name -> error.
	errs map[string]error

	// Recorded calls for assertions.
	EnsuredApps      []string
	CreateVolumeCalls []fly.CreateVolumeRequest
	CreateMachineCalls []struct {
		App string
		Req fly.CreateMachineRequest
	}
	StartCalls    []struct{ App, ID string }
	StopCalls      []struct{ App, ID string }
	DestroyCalls   []struct{ App, ID string }
	DestroyVolCalls []struct{ App, ID string }
	BuildCalls     []struct{ App, Dockerfile string }
}

func newMockFly() *mockFly {
	return &mockFly{
		volID:     "vol_abc",
		machineID: "mach_xyz",
		imageRef:  "registry.fly.io/tpl-xxxx:latest",
		errs:      map[string]error{},
	}
}

func (m *mockFly) CreateMachine(ctx context.Context, app string, req fly.CreateMachineRequest) (*fly.Machine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateMachineCalls = append(m.CreateMachineCalls, struct {
		App string
		Req fly.CreateMachineRequest
	}{App: app, Req: req})
	if err := m.errs["CreateMachine"]; err != nil {
		return nil, err
	}
	return &fly.Machine{ID: m.machineID, State: "started", Region: req.Region}, nil
}

func (m *mockFly) StartMachine(ctx context.Context, app, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StartCalls = append(m.StartCalls, struct{ App, ID string }{app, id})
	return m.errs["StartMachine"]
}

func (m *mockFly) StopMachine(ctx context.Context, app, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StopCalls = append(m.StopCalls, struct{ App, ID string }{app, id})
	return m.errs["StopMachine"]
}

func (m *mockFly) DestroyMachine(ctx context.Context, app, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DestroyCalls = append(m.DestroyCalls, struct{ App, ID string }{app, id})
	return m.errs["DestroyMachine"]
}

func (m *mockFly) GetMachine(ctx context.Context, app, id string) (*fly.Machine, error) {
	return &fly.Machine{ID: id, State: "started"}, nil
}

func (m *mockFly) CreateVolume(ctx context.Context, app string, req fly.CreateVolumeRequest) (*fly.Volume, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateVolumeCalls = append(m.CreateVolumeCalls, req)
	if err := m.errs["CreateVolume"]; err != nil {
		return nil, err
	}
	return &fly.Volume{ID: m.volID, Name: req.Name, SizeGB: req.SizeGB, Region: req.Region, Encrypted: req.Encrypted}, nil
}

func (m *mockFly) DestroyVolume(ctx context.Context, app, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DestroyVolCalls = append(m.DestroyVolCalls, struct{ App, ID string }{app, id})
	return m.errs["DestroyVolume"]
}

func (m *mockFly) EnsureApp(ctx context.Context, app string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EnsuredApps = append(m.EnsuredApps, app)
	return m.errs["EnsureApp"]
}

func (m *mockFly) BuildImage(ctx context.Context, app, dockerfile string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BuildCalls = append(m.BuildCalls, struct{ App, Dockerfile string }{app, dockerfile})
	if err := m.errs["BuildImage"]; err != nil {
		return "", err
	}
	return m.imageRef, nil
}

// --- In-memory template/session stores --------------------------------------

type memTemplates struct {
	mu   sync.Mutex
	data map[string]*models.Template
}

func newMemTemplates(seed ...*models.Template) *memTemplates {
	m := &memTemplates{data: map[string]*models.Template{}}
	for _, t := range seed {
		cp := *t
		m.data[t.ID] = &cp
	}
	return m
}

func (m *memTemplates) Get(_ context.Context, orgID, id string) (*models.Template, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.data[id]
	if !ok || t.OrgID != orgID {
		return nil, db.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *memTemplates) UpdateBuildStatus(_ context.Context, id string, status models.TemplateStatus, imageRef, flyAppName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.data[id]
	if !ok {
		return db.ErrNotFound
	}
	t.Status = status
	t.ImageRef = imageRef
	t.FlyAppName = flyAppName
	return nil
}

type memSessions struct {
	mu   sync.Mutex
	data map[string]*models.Session
}

func newMemSessions() *memSessions {
	return &memSessions{data: map[string]*models.Session{}}
}

func (m *memSessions) Create(_ context.Context, orgID, userID, templateID, name string) (*models.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uuid.NewString()
	if name == "" {
		name = "workspace-" + id[:8]
	}
	s := &models.Session{
		ID:         id,
		OrgID:      orgID,
		UserID:     userID,
		TemplateID: templateID,
		Name:       name,
		Status:     models.SessionStatusPending,
	}
	m.data[id] = s
	cp := *s
	return &cp, nil
}

func (m *memSessions) Get(_ context.Context, orgID, id string) (*models.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.data[id]
	if !ok || s.OrgID != orgID {
		return nil, db.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *memSessions) Provisioned(_ context.Context, id string, p db.ProvisionedFields) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.data[id]
	if !ok {
		return db.ErrNotFound
	}
	s.Status = p.Status
	s.FlyMachineID = p.MachineID
	s.FlyAppName = p.AppName
	s.FlyVolumeID = p.VolumeID
	s.URL = p.URL
	s.Region = p.Region
	return nil
}

func (m *memSessions) SetStatus(_ context.Context, id string, status models.SessionStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.data[id]
	if !ok {
		return db.ErrNotFound
	}
	s.Status = status
	return nil
}

func (m *memSessions) Delete(_ context.Context, orgID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.data[id]
	if !ok || s.OrgID != orgID {
		return db.ErrNotFound
	}
	delete(m.data, id)
	return nil
}

// --- Test helpers -----------------------------------------------------------

const (
	testOrg = "org-1"
	testUser = "user-1"
)

func readyTemplate(id, image string) *models.Template {
	return &models.Template{
		ID:                 id,
		OrgID:              testOrg,
		Name:               "go-codeserver",
		DockerfileContents: "FROM ubuntu:22.04",
		Status:             models.TemplateStatusReady,
		ImageRef:           image,
	}
}

// --- Tests ------------------------------------------------------------------

func TestCreateSession_Success(t *testing.T) {
	tmpls := newMemTemplates(readyTemplate("tpl-1", "registry.fly.io/tpl-1:latest"))
	sessions := newMemSessions()
	mf := newMockFly()
	orch := NewOrchestrator(tmpls, sessions, mf, mf, "iad", 8080, "", "fly.dev")

	sess, err := orch.CreateSession(context.Background(), testOrg, testUser, "tpl-1", "my-ws")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if sess.Status != models.SessionStatusRunning {
		t.Errorf("status = %s, want running", sess.Status)
	}
	if sess.FlyMachineID != "mach_xyz" {
		t.Errorf("machine id = %s, want mach_xyz", sess.FlyMachineID)
	}
	if sess.FlyVolumeID != "vol_abc" {
		t.Errorf("volume id = %s, want vol_abc", sess.FlyVolumeID)
	}
	if sess.URL == "" {
		t.Fatal("expected url set")
	}
	if got := len(mf.EnsuredApps); got != 1 {
		t.Fatalf("EnsureApp calls = %d, want 1", got)
	}
	if !startsWith(mf.EnsuredApps[0], "ws-") {
		t.Errorf("app name = %s, want ws-* prefix", mf.EnsuredApps[0])
	}

	// Volume provisioned with NVMe semantics.
	if len(mf.CreateVolumeCalls) != 1 {
		t.Fatalf("CreateVolume calls = %d, want 1", len(mf.CreateVolumeCalls))
	}
	vc := mf.CreateVolumeCalls[0]
	if vc.Region != "iad" {
		t.Errorf("volume region = %s, want iad", vc.Region)
	}
	if !vc.Encrypted {
		t.Error("volume should be encrypted")
	}
	if vc.SizeGB != 1 {
		t.Errorf("volume size = %d, want 1", vc.SizeGB)
	}

	// Machine mounts volume at /workspace and exposes port 8080 with scale-to-zero.
	if len(mf.CreateMachineCalls) != 1 {
		t.Fatalf("CreateMachine calls = %d, want 1", len(mf.CreateMachineCalls))
	}
	mc := mf.CreateMachineCalls[0]
	if mc.Req.Config.Image != "registry.fly.io/tpl-1:latest" {
		t.Errorf("image = %s", mc.Req.Config.Image)
	}
	if len(mc.Req.Config.Mounts) != 1 || mc.Req.Config.Mounts[0].Path != "/workspace" {
		t.Errorf("mounts = %+v, want /workspace", mc.Req.Config.Mounts)
	}
	if len(mc.Req.Config.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(mc.Req.Config.Services))
	}
	svc := mc.Req.Config.Services[0]
	if svc.InternalPort != 8080 {
		t.Errorf("internal_port = %d, want 8080", svc.InternalPort)
	}
	if svc.Autostop != "suspend" {
		t.Errorf("autostop = %s, want suspend", svc.Autostop)
	}
	if !svc.Autostart {
		t.Error("autostart should be true")
	}
}

func TestCreateSession_DefaultImageWhenUnbuilt(t *testing.T) {
	tmpl := readyTemplate("tpl-2", "") // not built yet
	tmpl.Status = models.TemplateStatusDraft
	tmpls := newMemTemplates(tmpl)
	sessions := newMemSessions()
	mf := newMockFly()
	orch := NewOrchestrator(tmpls, sessions, mf, mf, "iad", 8080, "registry.fly.io/default:latest", "fly.dev")

	sess, err := orch.CreateSession(context.Background(), testOrg, testUser, "tpl-2", "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.Status != models.SessionStatusRunning {
		t.Errorf("status = %s, want running", sess.Status)
	}
	if mc := mf.CreateMachineCalls[0].Req.Config.Image; mc != "registry.fly.io/default:latest" {
		t.Errorf("image = %s, want default", mc)
	}
}

func TestCreateSession_NotReady(t *testing.T) {
	tmpl := readyTemplate("tpl-3", "")
	tmpl.Status = models.TemplateStatusDraft
	tmpls := newMemTemplates(tmpl)
	orch := NewOrchestrator(tmpls, newMemSessions(), newMockFly(), newMockFly(), "iad", 8080, "", "fly.dev")

	_, err := orch.CreateSession(context.Background(), testOrg, testUser, "tpl-3", "")
	if !errors.Is(err, ErrTemplateNotReady) {
		t.Fatalf("err = %v, want ErrTemplateNotReady", err)
	}
}

func TestCreateSession_TemplateNotFound(t *testing.T) {
	orch := NewOrchestrator(newMemTemplates(), newMemSessions(), newMockFly(), newMockFly(), "iad", 8080, "img", "fly.dev")
	_, err := orch.CreateSession(context.Background(), testOrg, testUser, "nope", "")
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("err = %v, want ErrTemplateNotFound", err)
	}
}

func TestCreateSession_VolumeFailureMarksError(t *testing.T) {
	tmpls := newMemTemplates(readyTemplate("tpl-4", "img"))
	sessions := newMemSessions()
	mf := newMockFly()
	mf.errs["CreateVolume"] = errors.New("boom")
	orch := NewOrchestrator(tmpls, sessions, mf, mf, "iad", 8080, "", "fly.dev")

	_, err := orch.CreateSession(context.Background(), testOrg, testUser, "tpl-4", "")
	if err == nil {
		t.Fatal("expected error")
	}
	// The created pending session should have been marked error.
	for _, s := range sessions.data {
		if s.Status != models.SessionStatusError {
			t.Errorf("status = %s, want error", s.Status)
		}
	}
}

func TestCreateSession_MachineFailureCleansUpVolume(t *testing.T) {
	tmpls := newMemTemplates(readyTemplate("tpl-5", "img"))
	sessions := newMemSessions()
	mf := newMockFly()
	mf.errs["CreateMachine"] = errors.New("boom")
	orch := NewOrchestrator(tmpls, sessions, mf, mf, "iad", 8080, "", "fly.dev")

	_, err := orch.CreateSession(context.Background(), testOrg, testUser, "tpl-5", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(mf.DestroyVolCalls) != 1 {
		t.Errorf("DestroyVolume calls = %d, want 1 (cleanup)", len(mf.DestroyVolCalls))
	}
	if len(mf.DestroyVolCalls) > 0 && mf.DestroyVolCalls[0].ID != "vol_abc" {
		t.Errorf("cleaned volume id = %s, want vol_abc", mf.DestroyVolCalls[0].ID)
	}
}

func TestBuildTemplate_Success(t *testing.T) {
	tmpl := readyTemplate("tpl-6", "")
	tmpl.DockerfileContents = "FROM ubuntu:22.04\nRUN echo hi"
	tmpls := newMemTemplates(tmpl)
	mf := newMockFly()
	orch := NewOrchestrator(tmpls, newMemSessions(), mf, mf, "iad", 8080, "", "fly.dev")

	out, err := orch.BuildTemplate(context.Background(), testOrg, "tpl-6")
	if err != nil {
		t.Fatalf("BuildTemplate: %v", err)
	}
	if out.Status != models.TemplateStatusReady {
		t.Errorf("status = %s, want ready", out.Status)
	}
	if out.ImageRef != mf.imageRef {
		t.Errorf("image_ref = %s, want %s", out.ImageRef, mf.imageRef)
	}
	if len(mf.BuildCalls) != 1 || mf.BuildCalls[0].Dockerfile != tmpl.DockerfileContents {
		t.Errorf("BuildCalls = %+v", mf.BuildCalls)
	}
	if !startsWith(out.FlyAppName, "tpl-") {
		t.Errorf("fly_app_name = %s", out.FlyAppName)
	}
}

func TestBuildTemplate_FailureMarksFailed(t *testing.T) {
	tmpl := readyTemplate("tpl-7", "")
	tmpls := newMemTemplates(tmpl)
	mf := newMockFly()
	mf.errs["BuildImage"] = errors.New("build broke")
	orch := NewOrchestrator(tmpls, newMemSessions(), mf, mf, "iad", 8080, "", "fly.dev")

	_, err := orch.BuildTemplate(context.Background(), testOrg, "tpl-7")
	if err == nil {
		t.Fatal("expected error")
	}
	got, _ := tmpls.Get(context.Background(), testOrg, "tpl-7")
	if got.Status != models.TemplateStatusFailed {
		t.Errorf("status = %s, want failed", got.Status)
	}
}

func TestBuildTemplate_NotFound(t *testing.T) {
	orch := NewOrchestrator(newMemTemplates(), newMemSessions(), newMockFly(), newMockFly(), "iad", 8080, "", "fly.dev")
	_, err := orch.BuildTemplate(context.Background(), testOrg, "missing")
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("err = %v, want ErrTemplateNotFound", err)
	}
}

func TestResumeAndHibernateSession(t *testing.T) {
	tmpls := newMemTemplates(readyTemplate("tpl-r", "img"))
	sessions := newMemSessions()
	mf := newMockFly()
	orch := NewOrchestrator(tmpls, sessions, mf, mf, "iad", 8080, "", "fly.dev")

	sess, err := orch.CreateSession(context.Background(), testOrg, testUser, "tpl-r", "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := orch.HibernateSession(context.Background(), testOrg, sess.ID); err != nil {
		t.Fatalf("Hibernate: %v", err)
	}
	if len(mf.StopCalls) != 1 || mf.StopCalls[0].ID != "mach_xyz" {
		t.Errorf("StopCalls = %+v", mf.StopCalls)
	}
	got, _ := sessions.Get(context.Background(), testOrg, sess.ID)
	if got.Status != models.SessionStatusSuspended {
		t.Errorf("status = %s, want suspended", got.Status)
	}

	if _, err := orch.ResumeSession(context.Background(), testOrg, sess.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(mf.StartCalls) != 1 || mf.StartCalls[0].ID != "mach_xyz" {
		t.Errorf("StartCalls = %+v", mf.StartCalls)
	}
}

func TestDeleteSession_DestroysMachineAndVolume(t *testing.T) {
	tmpls := newMemTemplates(readyTemplate("tpl-d", "img"))
	sessions := newMemSessions()
	mf := newMockFly()
	orch := NewOrchestrator(tmpls, sessions, mf, mf, "iad", 8080, "", "fly.dev")

	sess, err := orch.CreateSession(context.Background(), testOrg, testUser, "tpl-d", "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := orch.DeleteSession(context.Background(), testOrg, sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(mf.DestroyCalls) != 1 || mf.DestroyCalls[0].ID != "mach_xyz" {
		t.Errorf("DestroyCalls = %+v", mf.DestroyCalls)
	}
	if len(mf.DestroyVolCalls) != 1 || mf.DestroyVolCalls[0].ID != "vol_abc" {
		t.Errorf("DestroyVolCalls = %+v", mf.DestroyVolCalls)
	}
	if _, err := sessions.Get(context.Background(), testOrg, sess.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("session should be deleted, got err = %v", err)
	}
}

func TestDeleteSession_NotFound(t *testing.T) {
	orch := NewOrchestrator(newMemTemplates(), newMemSessions(), newMockFly(), newMockFly(), "iad", 8080, "img", "fly.dev")
	if err := orch.DeleteSession(context.Background(), testOrg, "nope"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

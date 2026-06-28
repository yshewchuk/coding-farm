package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/cloudsandbox/platform/internal/auth"
	"github.com/cloudsandbox/platform/internal/config"
	"github.com/cloudsandbox/platform/internal/db"
	"github.com/cloudsandbox/platform/internal/fly"
	"github.com/cloudsandbox/platform/internal/models"
	"github.com/cloudsandbox/platform/internal/service"
	"github.com/google/uuid"
)

// --- Fakes ------------------------------------------------------------------

const testToken = "valid-test-token"

type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, raw string) (auth.Claims, error) {
	if raw != testToken {
		return auth.Claims{}, auth.ErrInvalidToken
	}
	return auth.Claims{Subject: "logto-user-1", Email: "admin@example.com"}, nil
}

type fakeProvisioner struct{ orgID string }

func (f fakeProvisioner) ProvisionSelfHostedAdmin(_ context.Context, _, _ string) (*models.User, *models.Organization, error) {
	return &models.User{ID: "user-uuid-1", Email: "admin@example.com"}, &models.Organization{ID: f.orgID}, nil
}
func (f fakeProvisioner) EnsureOrgForClaim(_ context.Context, _, _, _ string) (*models.Organization, error) {
	return &models.Organization{ID: f.orgID}, nil
}
func (f fakeProvisioner) EnsureUser(_ context.Context, _, _ string) (*models.User, error) {
	return &models.User{ID: "user-uuid-1"}, nil
}
func (f fakeProvisioner) ListMemberships(_ context.Context, _ string) ([]db.Membership, error) {
	return []db.Membership{{OrgID: f.orgID, Role: models.RoleAdmin}}, nil
}

// mockFlyClient implements fly.MachinesAPI + fly.BuilderAPI for handler tests.
type mockFlyClient struct {
	mu                         sync.Mutex
	volID, machineID, imageRef string
	errs                       map[string]error

	EnsuredApps        []string
	CreateVolumeCalls  []fly.CreateVolumeRequest
	CreateMachineCalls []struct {
		App string
		Req fly.CreateMachineRequest
	}
	StartCalls      []struct{ App, ID string }
	StopCalls       []struct{ App, ID string }
	DestroyCalls    []struct{ App, ID string }
	DestroyVolCalls []struct{ App, ID string }
	BuildCalls      []struct{ App, Dockerfile string }
}

func newMockFlyClient() *mockFlyClient {
	return &mockFlyClient{
		volID:     "vol_abc",
		machineID: "mach_xyz",
		imageRef:  "registry.fly.io/tpl-mock:latest",
		errs:      map[string]error{},
	}
}

func (m *mockFlyClient) CreateMachine(_ context.Context, app string, req fly.CreateMachineRequest) (*fly.Machine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateMachineCalls = append(m.CreateMachineCalls, struct {
		App string
		Req fly.CreateMachineRequest
	}{app, req})
	if err := m.errs["CreateMachine"]; err != nil {
		return nil, err
	}
	return &fly.Machine{ID: m.machineID, State: "started", Region: req.Region}, nil
}
func (m *mockFlyClient) StartMachine(_ context.Context, app, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StartCalls = append(m.StartCalls, struct{ App, ID string }{app, id})
	return m.errs["StartMachine"]
}
func (m *mockFlyClient) StopMachine(_ context.Context, app, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StopCalls = append(m.StopCalls, struct{ App, ID string }{app, id})
	return m.errs["StopMachine"]
}
func (m *mockFlyClient) DestroyMachine(_ context.Context, app, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DestroyCalls = append(m.DestroyCalls, struct{ App, ID string }{app, id})
	return m.errs["DestroyMachine"]
}
func (m *mockFlyClient) GetMachine(_ context.Context, _, id string) (*fly.Machine, error) {
	return &fly.Machine{ID: id, State: "started"}, nil
}
func (m *mockFlyClient) CreateVolume(_ context.Context, app string, req fly.CreateVolumeRequest) (*fly.Volume, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateVolumeCalls = append(m.CreateVolumeCalls, req)
	if err := m.errs["CreateVolume"]; err != nil {
		return nil, err
	}
	return &fly.Volume{ID: m.volID, Name: req.Name, SizeGB: req.SizeGB, Region: req.Region, Encrypted: req.Encrypted}, nil
}
func (m *mockFlyClient) DestroyVolume(_ context.Context, app, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DestroyVolCalls = append(m.DestroyVolCalls, struct{ App, ID string }{app, id})
	return m.errs["DestroyVolume"]
}
func (m *mockFlyClient) EnsureApp(_ context.Context, app string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EnsuredApps = append(m.EnsuredApps, app)
	return m.errs["EnsureApp"]
}
func (m *mockFlyClient) BuildImage(_ context.Context, app, dockerfile string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BuildCalls = append(m.BuildCalls, struct{ App, Dockerfile string }{app, dockerfile})
	if err := m.errs["BuildImage"]; err != nil {
		return "", err
	}
	return m.imageRef, nil
}

// memTemplates satisfies api.templateStore AND service.templateStore.
type memTemplates struct {
	data map[string]*models.Template
}

func newMemTemplates() *memTemplates { return &memTemplates{data: map[string]*models.Template{}} }

func (m *memTemplates) List(_ context.Context, orgID string) ([]models.Template, error) {
	var out []models.Template
	for _, t := range m.data {
		if t.OrgID == orgID {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (m *memTemplates) Create(_ context.Context, orgID string, in models.CreateTemplateInput) (*models.Template, error) {
	t := &models.Template{ID: uuid.NewString(), OrgID: orgID, Name: in.Name, DockerfileContents: in.DockerfileContents, Status: models.TemplateStatusDraft}
	m.data[t.ID] = t
	return t, nil
}
func (m *memTemplates) Get(_ context.Context, orgID, id string) (*models.Template, error) {
	t, ok := m.data[id]
	if !ok || t.OrgID != orgID {
		return nil, db.ErrNotFound
	}
	cp := *t
	return &cp, nil
}
func (m *memTemplates) Delete(_ context.Context, orgID, id string) error {
	t, ok := m.data[id]
	if !ok || t.OrgID != orgID {
		return db.ErrNotFound
	}
	delete(m.data, id)
	return nil
}
func (m *memTemplates) UpdateBuildStatus(_ context.Context, id string, status models.TemplateStatus, imageRef, flyAppName string) error {
	t, ok := m.data[id]
	if !ok {
		return db.ErrNotFound
	}
	t.Status = status
	t.ImageRef = imageRef
	t.FlyAppName = flyAppName
	return nil
}

// memSessions satisfies api.sessionStore AND service.sessionStore.
type memSessions struct {
	data map[string]*models.Session
}

func newMemSessions() *memSessions { return &memSessions{data: map[string]*models.Session{}} }

func (m *memSessions) List(_ context.Context, orgID, userID string) ([]models.Session, error) {
	var out []models.Session
	for _, s := range m.data {
		if s.OrgID == orgID && s.UserID == userID {
			out = append(out, *s)
		}
	}
	return out, nil
}
func (m *memSessions) Get(_ context.Context, orgID, id string) (*models.Session, error) {
	s, ok := m.data[id]
	if !ok || s.OrgID != orgID {
		return nil, db.ErrNotFound
	}
	cp := *s
	return &cp, nil
}
func (m *memSessions) Create(_ context.Context, orgID, userID, templateID, name string) (*models.Session, error) {
	id := uuid.NewString()
	if name == "" {
		name = "workspace-" + id[:8]
	}
	s := &models.Session{ID: id, OrgID: orgID, UserID: userID, TemplateID: templateID, Name: name, Status: models.SessionStatusPending}
	m.data[id] = s
	cp := *s
	return &cp, nil
}
func (m *memSessions) Provisioned(_ context.Context, id string, p db.ProvisionedFields) error {
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
	s, ok := m.data[id]
	if !ok {
		return db.ErrNotFound
	}
	s.Status = status
	return nil
}
func (m *memSessions) Delete(_ context.Context, orgID, id string) error {
	s, ok := m.data[id]
	if !ok || s.OrgID != orgID {
		return db.ErrNotFound
	}
	delete(m.data, id)
	return nil
}

// --- Test wiring ------------------------------------------------------------

type testStores struct {
	templates *memTemplates
	sessions  *memSessions
}

func newTestServer(t *testing.T, stores *testStores, mf *mockFlyClient, orgID string) *Server {
	t.Helper()
	if stores == nil {
		stores = &testStores{templates: newMemTemplates(), sessions: newMemSessions()}
	}
	if mf == nil {
		mf = newMockFlyClient()
	}
	orch := service.NewOrchestrator(stores.templates, stores.sessions, mf, mf, "iad", 8080, "", "fly.dev")
	return NewServer(config.Config{}, fakeProvisioner{orgID: orgID}, stores.templates, stores.sessions, orch, fakeVerifier{})
}

// --- HTTP helpers -----------------------------------------------------------

func do(t *testing.T, srv *Server, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	return rr
}

func decode(t *testing.T, rr *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode body %q: %v", rr.Body.String(), err)
	}
}

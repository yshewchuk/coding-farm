package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	mu                       sync.Mutex
	volID, machineID, imageRef string
	errs                     map[string]error

	EnsuredApps       []string
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

// --- Tests ------------------------------------------------------------------

func TestHealth_NoAuth(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodGet, "/health", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestMeHandler(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodGet, "/api/me", nil, testToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	decode(t, rr, &resp)
	if resp["user_id"] != "user-uuid-1" {
		t.Errorf("user_id = %v", resp["user_id"])
	}
	if resp["org_id"] != "org-1" {
		t.Errorf("org_id = %v", resp["org_id"])
	}
}

func TestMeHandler_NoToken(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodGet, "/api/me", nil, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestMeHandler_BadToken(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodGet, "/api/me", nil, "nope")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestCreateTemplateAndList(t *testing.T) {
	stores := &testStores{templates: newMemTemplates(), sessions: newMemSessions()}
	srv := newTestServer(t, stores, nil, "org-1")

	rr := do(t, srv, http.MethodPost, "/api/templates", models.CreateTemplateInput{
		Name:               "go-codeserver",
		DockerfileContents: "FROM ubuntu:22.04",
	}, testToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rr.Code, rr.Body.String())
	}
	var created models.Template
	decode(t, rr, &created)
	if created.Status != models.TemplateStatusDraft {
		t.Errorf("status = %s, want draft", created.Status)
	}

	rr = do(t, srv, http.MethodGet, "/api/templates", nil, testToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d", rr.Code)
	}
	var list struct {
		Templates []models.Template `json:"templates"`
	}
	decode(t, rr, &list)
	if len(list.Templates) != 1 || list.Templates[0].ID != created.ID {
		t.Errorf("templates = %+v", list.Templates)
	}
}

func TestCreateTemplate_Validation(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodPost, "/api/templates", models.CreateTemplateInput{Name: "x"}, testToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestGetTemplate_OrgIsolation(t *testing.T) {
	stores := &testStores{templates: newMemTemplates(), sessions: newMemSessions()}
	// Create a template as org-1.
	srvA := newTestServer(t, stores, nil, "org-1")
	rr := do(t, srvA, http.MethodPost, "/api/templates", models.CreateTemplateInput{
		Name: "t", DockerfileContents: "FROM ubuntu",
	}, testToken)
	var created models.Template
	decode(t, rr, &created)

	// Request it as org-2 with the SAME shared store: must be 404 (authz).
	srvB := newTestServer(t, stores, nil, "org-2")
	rr = do(t, srvB, http.MethodGet, "/api/templates/"+created.ID, nil, testToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (org isolation)", rr.Code)
	}
}

func TestCreateSession_FullFlow(t *testing.T) {
	stores := &testStores{templates: newMemTemplates(), sessions: newMemSessions()}
	stores.templates.data["tpl-1"] = &models.Template{
		ID: "tpl-1", OrgID: "org-1", Name: "ready",
		DockerfileContents: "FROM ubuntu", Status: models.TemplateStatusReady,
		ImageRef: "registry.fly.io/ready:latest",
	}
	mf := newMockFlyClient()
	srv := newTestServer(t, stores, mf, "org-1")

	rr := do(t, srv, http.MethodPost, "/api/sessions", models.CreateSessionInput{
		TemplateID: "tpl-1", Name: "my-sandbox",
	}, testToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rr.Code, rr.Body.String())
	}
	var sess models.Session
	decode(t, rr, &sess)
	if sess.Status != models.SessionStatusRunning {
		t.Errorf("status = %s, want running", sess.Status)
	}
	if sess.FlyMachineID != "mach_xyz" {
		t.Errorf("machine id = %s", sess.FlyMachineID)
	}
	if sess.URL == "" {
		t.Error("expected url")
	}
	if len(mf.CreateVolumeCalls) != 1 {
		t.Errorf("volume calls = %d", len(mf.CreateVolumeCalls))
	}
	if len(mf.CreateMachineCalls) != 1 {
		t.Errorf("machine calls = %d", len(mf.CreateMachineCalls))
	}
}

func TestCreateSession_TemplateNotReady(t *testing.T) {
	stores := &testStores{templates: newMemTemplates(), sessions: newMemSessions()}
	stores.templates.data["tpl-2"] = &models.Template{
		ID: "tpl-2", OrgID: "org-1", Status: models.TemplateStatusDraft,
	}
	srv := newTestServer(t, stores, nil, "org-1")
	rr := do(t, srv, http.MethodPost, "/api/sessions", models.CreateSessionInput{TemplateID: "tpl-2"}, testToken)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rr.Code)
	}
}

func TestDeleteSession_HTTP(t *testing.T) {
	stores := &testStores{templates: newMemTemplates(), sessions: newMemSessions()}
	stores.templates.data["tpl-d"] = &models.Template{ID: "tpl-d", OrgID: "org-1", Status: models.TemplateStatusReady, ImageRef: "img"}
	mf := newMockFlyClient()
	srv := newTestServer(t, stores, mf, "org-1")

	rr := do(t, srv, http.MethodPost, "/api/sessions", models.CreateSessionInput{TemplateID: "tpl-d"}, testToken)
	var sess models.Session
	decode(t, rr, &sess)

	rr = do(t, srv, http.MethodDelete, "/api/sessions/"+sess.ID, nil, testToken)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", rr.Code)
	}
	if len(mf.DestroyCalls) != 1 {
		t.Errorf("destroy machine calls = %d", len(mf.DestroyCalls))
	}
	if len(mf.DestroyVolCalls) != 1 {
		t.Errorf("destroy volume calls = %d", len(mf.DestroyVolCalls))
	}
}

func TestResumeHibernate_HTTP(t *testing.T) {
	stores := &testStores{templates: newMemTemplates(), sessions: newMemSessions()}
	stores.templates.data["tpl-r"] = &models.Template{ID: "tpl-r", OrgID: "org-1", Status: models.TemplateStatusReady, ImageRef: "img"}
	mf := newMockFlyClient()
	srv := newTestServer(t, stores, mf, "org-1")

	rr := do(t, srv, http.MethodPost, "/api/sessions", models.CreateSessionInput{TemplateID: "tpl-r"}, testToken)
	var sess models.Session
	decode(t, rr, &sess)

	rr = do(t, srv, http.MethodPost, "/api/sessions/"+sess.ID+"/hibernate", nil, testToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("hibernate status = %d body=%s", rr.Code, rr.Body.String())
	}
	rr = do(t, srv, http.MethodPost, "/api/sessions/"+sess.ID+"/resume", nil, testToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("resume status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestBuildTemplate_HTTP(t *testing.T) {
	stores := &testStores{templates: newMemTemplates(), sessions: newMemSessions()}
	stores.templates.data["tpl-b"] = &models.Template{
		ID: "tpl-b", OrgID: "org-1", Name: "t",
		DockerfileContents: "FROM ubuntu", Status: models.TemplateStatusDraft,
	}
	mf := newMockFlyClient()
	srv := newTestServer(t, stores, mf, "org-1")

	rr := do(t, srv, http.MethodPost, "/api/templates/tpl-b/build", nil, testToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("build status = %d body=%s", rr.Code, rr.Body.String())
	}
	var tpl models.Template
	decode(t, rr, &tpl)
	if tpl.Status != models.TemplateStatusReady {
		t.Errorf("status = %s, want ready", tpl.Status)
	}
	if len(mf.BuildCalls) != 1 {
		t.Errorf("build calls = %d", len(mf.BuildCalls))
	}
}

func TestErrors_AreJSON(t *testing.T) {
	srv := newTestServer(t, nil, nil, "org-1")
	rr := do(t, srv, http.MethodGet, "/api/templates/nope", nil, testToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	var e errorResponse
	decode(t, rr, &e)
	if e.Error == "" {
		t.Error("expected non-empty error message")
	}
}

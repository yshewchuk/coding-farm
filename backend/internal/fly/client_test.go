package fly

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingServer returns canned JSON per route and captures the last request
// body for assertions, exercising the real HTTP client against a fake Fly API.
type recordingServer struct {
	srv        *httptest.Server
	lastMethod string
	lastPath   string
	lastAuth   string
	lastBody   []byte
}

func newRecordingServer(t *testing.T) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/apps/", func(w http.ResponseWriter, r *http.Request) {
		rs.lastMethod = r.Method
		rs.lastPath = r.URL.Path
		rs.lastAuth = r.Header.Get("Authorization")
		if r.Body != nil {
			rs.lastBody, _ = io.ReadAll(r.Body)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/machines") && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"mach_123","state":"started","region":"iad"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/stop"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/volumes") && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"vol_123","name":"wsdata","size_gb":1,"region":"iad","encrypted":true}`))
		case strings.Contains(r.URL.Path, "/volumes/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/machines/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/builds") && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusAccepted)
		case r.URL.Path == "/v1/apps" && r.Method == http.MethodPost:
			// Simulate "app already exists".
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"app exists"}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	rs.srv = httptest.NewServer(mux)
	t.Cleanup(rs.srv.Close)
	return rs
}

func (rs *recordingServer) client() *Client {
	return NewClient(rs.srv.URL, rs.srv.URL, "test-token", "my-org", nil)
}

func TestClient_CreateMachine(t *testing.T) {
	rs := newRecordingServer(t)
	c := rs.client()

	m, err := c.CreateMachine(context.Background(), "my-app", CreateMachineRequest{
		Region: "iad",
		Config: MachineConfig{
			Image: "registry.fly.io/img:latest",
			Mounts: []Mount{{Volume: "vol_1", Path: "/workspace"}},
			Services: []Service{{InternalPort: 8080, Protocol: "tcp", Autostop: "suspend", Autostart: true}},
		},
	})
	if err != nil {
		t.Fatalf("CreateMachine: %v", err)
	}
	if m.ID != "mach_123" {
		t.Errorf("machine id = %s", m.ID)
	}
	if rs.lastMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", rs.lastMethod)
	}
	if rs.lastPath != "/v1/apps/my-app/machines" {
		t.Errorf("path = %s", rs.lastPath)
	}

	// Validate the on-the-wire body carries image, mounts, and scale-to-zero.
	var body map[string]any
	if err := json.Unmarshal(rs.lastBody, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	cfg, _ := body["config"].(map[string]any)
	if cfg["image"] != "registry.fly.io/img:latest" {
		t.Errorf("image = %v", cfg["image"])
	}
	mounts, _ := cfg["mounts"].([]any)
	if len(mounts) != 1 {
		t.Fatalf("mounts = %v", mounts)
	}
	mount := mounts[0].(map[string]any)
	if mount["path"] != "/workspace" {
		t.Errorf("mount path = %v", mount["path"])
	}
	services, _ := cfg["services"].([]any)
	svc := services[0].(map[string]any)
	if svc["autostop"] != "suspend" {
		t.Errorf("autostop = %v", svc["autostop"])
	}
	if svc["autostart"] != true {
		t.Errorf("autostart = %v", svc["autostart"])
	}
	if int(svc["internal_port"].(float64)) != 8080 {
		t.Errorf("internal_port = %v", svc["internal_port"])
	}

	// Authorization header must carry the bearer token.
	if rs.lastAuth != "Bearer test-token" {
		t.Errorf("authorization = %q, want Bearer test-token", rs.lastAuth)
	}
}

func TestClient_CreateVolume(t *testing.T) {
	rs := newRecordingServer(t)
	c := rs.client()

	v, err := c.CreateVolume(context.Background(), "my-app", CreateVolumeRequest{
		Name: "wsdata-abc", Region: "iad", SizeGB: 1, Encrypted: true,
	})
	if err != nil {
		t.Fatalf("CreateVolume: %v", err)
	}
	if v.ID != "vol_123" {
		t.Errorf("volume id = %s", v.ID)
	}
	var body map[string]any
	_ = json.Unmarshal(rs.lastBody, &body)
	if body["encrypted"] != true {
		t.Errorf("encrypted = %v", body["encrypted"])
	}
	if body["name"] != "wsdata-abc" {
		t.Errorf("name = %v", body["name"])
	}
}

func TestClient_StartStopDestroy(t *testing.T) {
	rs := newRecordingServer(t)
	c := rs.client()

	if err := c.StartMachine(context.Background(), "app", "m1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.HasSuffix(rs.lastPath, "/m1/start") {
		t.Errorf("start path = %s", rs.lastPath)
	}

	if err := c.StopMachine(context.Background(), "app", "m1"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !strings.HasSuffix(rs.lastPath, "/m1/stop") {
		t.Errorf("stop path = %s", rs.lastPath)
	}

	if err := c.DestroyMachine(context.Background(), "app", "m1"); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if rs.lastMethod != http.MethodDelete {
		t.Errorf("destroy method = %s", rs.lastMethod)
	}
}

func TestClient_APIErrorAndIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"machine not found"}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, srv.URL, "tok", "org", nil)

	_, err := c.GetMachine(context.Background(), "app", "m1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Errorf("expected IsNotFound, got %v", err)
	}
	if ae, ok := err.(*APIError); !ok || ae.Message != "machine not found" {
		t.Errorf("error = %+v", err)
	}
}

func TestClient_EnsureApp_ToleratesConflict(t *testing.T) {
	rs := newRecordingServer(t)
	c := rs.client()
	if err := c.EnsureApp(context.Background(), "my-app"); err != nil {
		t.Fatalf("EnsureApp should tolerate 409: %v", err)
	}
}

func TestClient_BuildImage_ReturnsRegistryRef(t *testing.T) {
	rs := newRecordingServer(t)
	c := rs.client()
	ref, err := c.BuildImage(context.Background(), "tpl-abc", "FROM ubuntu")
	if err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
	if ref != "registry.fly.io/tpl-abc:latest" {
		t.Errorf("ref = %s", ref)
	}
	var body map[string]any
	_ = json.Unmarshal(rs.lastBody, &body)
	if body["dockerfile"] != "FROM ubuntu" {
		t.Errorf("dockerfile = %v", body["dockerfile"])
	}
}

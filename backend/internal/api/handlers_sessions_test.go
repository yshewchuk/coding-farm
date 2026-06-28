package api

import (
	"net/http"
	"testing"

	"github.com/cloudsandbox/platform/internal/models"
)

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

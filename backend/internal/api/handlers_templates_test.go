package api

import (
	"net/http"
	"testing"

	"github.com/cloudsandbox/platform/internal/models"
)

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

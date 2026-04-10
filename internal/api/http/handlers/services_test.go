package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/plusclouds/ubuntu-agent/internal/api/http/handlers"
	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
)

// fakeServices is a test double for ServicesProvider.
type fakeServices struct {
	list        []services.ServiceInfo
	info        *services.ServiceInfo
	actionResult *services.ActionResult
	err         error
}

func (f *fakeServices) List(_ context.Context) ([]services.ServiceInfo, error) {
	return f.list, f.err
}
func (f *fakeServices) Get(_ context.Context, _ string) (*services.ServiceInfo, error) {
	return f.info, f.err
}
func (f *fakeServices) Start(_ context.Context, _ string) (*services.ActionResult, error) {
	return f.actionResult, f.err
}
func (f *fakeServices) Stop(_ context.Context, _ string) (*services.ActionResult, error) {
	return f.actionResult, f.err
}
func (f *fakeServices) Restart(_ context.Context, _ string) (*services.ActionResult, error) {
	return f.actionResult, f.err
}
func (f *fakeServices) Reload(_ context.Context, _ string) (*services.ActionResult, error) {
	return f.actionResult, f.err
}
func (f *fakeServices) Enable(_ context.Context, _ string) (*services.ActionResult, error) {
	return f.actionResult, f.err
}
func (f *fakeServices) Disable(_ context.Context, _ string) (*services.ActionResult, error) {
	return f.actionResult, f.err
}

// chiRequest builds a request with a Chi URL param pre-set.
func chiRequest(method, path, paramName, paramValue string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramValue)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// --- List ---

func TestServicesList_Success(t *testing.T) {
	fake := &fakeServices{
		list: []services.ServiceInfo{
			{Name: "nginx.service", State: services.StateActive},
			{Name: "ssh.service", State: services.StateActive},
		},
	}
	h := handlers.NewServicesHandler(fake)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/services/", nil)
	rec := httptest.NewRecorder()
	h.List(rec, r)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if !env.Success {
		t.Error("expected success=true")
	}
	var list []services.ServiceInfo
	if err := json.Unmarshal(env.Data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 services, got %d", len(list))
	}
}

func TestServicesList_Error_Returns500(t *testing.T) {
	fake := &fakeServices{err: errors.New("dbus error")}
	h := handlers.NewServicesHandler(fake)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/services/", nil)
	rec := httptest.NewRecorder()
	h.List(rec, r)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// --- Get ---

func TestServicesGet_Success(t *testing.T) {
	fake := &fakeServices{
		info: &services.ServiceInfo{
			Name:     "nginx.service",
			State:    services.StateActive,
			SubState: "running",
		},
	}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodGet, "/api/v1/services/nginx", "name", "nginx")
	rec := httptest.NewRecorder()
	h.Get(rec, r)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	var info services.ServiceInfo
	if err := json.Unmarshal(env.Data, &info); err != nil {
		t.Fatal(err)
	}
	if info.Name != "nginx.service" {
		t.Errorf("name: got %q, want nginx.service", info.Name)
	}
}

func TestServicesGet_NotFound_Returns404(t *testing.T) {
	fake := &fakeServices{err: errors.New("unit not found: ghost.service")}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodGet, "/api/v1/services/ghost", "name", "ghost")
	rec := httptest.NewRecorder()
	h.Get(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Action helpers ---

func successResult(action services.ServiceAction) *services.ActionResult {
	return &services.ActionResult{
		Service: "nginx.service",
		Action:  action,
		Success: true,
		Message: "ok",
	}
}

func failResult(action services.ServiceAction) *services.ActionResult {
	return &services.ActionResult{
		Service: "nginx.service",
		Action:  action,
		Success: false,
		Message: "failed",
	}
}

// --- Status ---

func TestServicesStatus_Success(t *testing.T) {
	fake := &fakeServices{
		info: &services.ServiceInfo{
			Name:     "rsyslog.service",
			State:    services.StateActive,
			SubState: "running",
			PID:      1234,
		},
	}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodGet, "/api/v1/services/rsyslog/status", "name", "rsyslog")
	rec := httptest.NewRecorder()
	h.Status(rec, r)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	var obj map[string]interface{}
	if err := json.Unmarshal(env.Data, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["state"] != "active" {
		t.Errorf("state: got %v, want active", obj["state"])
	}
	if obj["sub_state"] != "running" {
		t.Errorf("sub_state: got %v, want running", obj["sub_state"])
	}
	if _, ok := obj["description"]; ok {
		t.Error("description should not be in status response")
	}
}

func TestServicesStatus_NotFound_Returns404(t *testing.T) {
	fake := &fakeServices{err: errors.New("unit not found")}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodGet, "/api/v1/services/ghost/status", "name", "ghost")
	rec := httptest.NewRecorder()
	h.Status(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Start ---

func TestServicesStart_Success(t *testing.T) {
	fake := &fakeServices{actionResult: successResult(services.ActionStart)}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodPost, "/api/v1/services/nginx/start", "name", "nginx")
	rec := httptest.NewRecorder()
	h.Start(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestServicesStart_ActionFailure_Returns422(t *testing.T) {
	fake := &fakeServices{actionResult: failResult(services.ActionStart)}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodPost, "/api/v1/services/nginx/start", "name", "nginx")
	rec := httptest.NewRecorder()
	h.Start(rec, r)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rec.Code)
	}
}

func TestServicesStart_Error_Returns500(t *testing.T) {
	fake := &fakeServices{err: errors.New("dbus gone")}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodPost, "/api/v1/services/nginx/start", "name", "nginx")
	rec := httptest.NewRecorder()
	h.Start(rec, r)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// --- Stop ---

func TestServicesStop_Success(t *testing.T) {
	fake := &fakeServices{actionResult: successResult(services.ActionStop)}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodPost, "/api/v1/services/nginx/stop", "name", "nginx")
	rec := httptest.NewRecorder()
	h.Stop(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// --- Restart ---

func TestServicesRestart_Success(t *testing.T) {
	fake := &fakeServices{actionResult: successResult(services.ActionRestart)}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodPost, "/api/v1/services/nginx/restart", "name", "nginx")
	rec := httptest.NewRecorder()
	h.Restart(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// --- Reload ---

func TestServicesReload_Success(t *testing.T) {
	fake := &fakeServices{actionResult: successResult(services.ActionReload)}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodPost, "/api/v1/services/nginx/reload", "name", "nginx")
	rec := httptest.NewRecorder()
	h.Reload(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// --- Enable ---

func TestServicesEnable_Success(t *testing.T) {
	fake := &fakeServices{actionResult: successResult(services.ActionEnable)}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodPost, "/api/v1/services/nginx/enable", "name", "nginx")
	rec := httptest.NewRecorder()
	h.Enable(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// --- Disable ---

func TestServicesDisable_Success(t *testing.T) {
	fake := &fakeServices{actionResult: successResult(services.ActionDisable)}
	h := handlers.NewServicesHandler(fake)
	r := chiRequest(http.MethodPost, "/api/v1/services/nginx/disable", "name", "nginx")
	rec := httptest.NewRecorder()
	h.Disable(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/plusclouds/ubuntu-agent/internal/api/http/response"
	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
)

// ServicesProvider is the interface the services handler depends on.
// *services.Module satisfies this interface.
type ServicesProvider interface {
	List(ctx context.Context) ([]services.ServiceInfo, error)
	Get(ctx context.Context, name string) (*services.ServiceInfo, error)
	Start(ctx context.Context, name string) (*services.ActionResult, error)
	Stop(ctx context.Context, name string) (*services.ActionResult, error)
	Restart(ctx context.Context, name string) (*services.ActionResult, error)
	Reload(ctx context.Context, name string) (*services.ActionResult, error)
	Enable(ctx context.Context, name string) (*services.ActionResult, error)
	Disable(ctx context.Context, name string) (*services.ActionResult, error)
}

// ServicesHandler wires the services module to HTTP endpoints.
type ServicesHandler struct {
	svc ServicesProvider
}

// NewServicesHandler creates a ServicesHandler backed by the given services provider.
func NewServicesHandler(svc ServicesProvider) *ServicesHandler {
	return &ServicesHandler{svc: svc}
}

// List handles GET /api/v1/services.
// Returns information about all loaded systemd service units.
func (h *ServicesHandler) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.List(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "SERVICE_LIST_ERROR", err.Error())
		return
	}
	response.Success(w, list)
}

// Get handles GET /api/v1/services/{name}.
// Returns detailed information for a single named service.
func (h *ServicesHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	info, err := h.svc.Get(r.Context(), name)
	if err != nil {
		response.Error(w, http.StatusNotFound, "SERVICE_NOT_FOUND", err.Error())
		return
	}
	response.Success(w, info)
}

// Status handles GET /api/v1/services/{name}/status.
// Returns the current state of a single named service.
func (h *ServicesHandler) Status(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	info, err := h.svc.Get(r.Context(), name)
	if err != nil {
		response.Error(w, http.StatusNotFound, "SERVICE_NOT_FOUND", err.Error())
		return
	}
	response.Success(w, struct {
		Name     string               `json:"name"`
		State    services.ServiceState `json:"state"`
		SubState string               `json:"sub_state"`
		Enabled  bool                 `json:"enabled"`
		PID      uint32               `json:"pid,omitempty"`
		Since    int64                `json:"since,omitempty"`
	}{
		Name:     info.Name,
		State:    info.State,
		SubState: info.SubState,
		Enabled:  info.Enabled,
		PID:      info.PID,
		Since:    info.Since,
	})
}

// Start handles POST /api/v1/services/{name}/start.
func (h *ServicesHandler) Start(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	result, err := h.svc.Start(r.Context(), name)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "SERVICE_START_ERROR", err.Error())
		return
	}
	if !result.Success {
		response.JSON(w, http.StatusUnprocessableEntity, result)
		return
	}
	response.Success(w, result)
}

// Stop handles POST /api/v1/services/{name}/stop.
func (h *ServicesHandler) Stop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	result, err := h.svc.Stop(r.Context(), name)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "SERVICE_STOP_ERROR", err.Error())
		return
	}
	if !result.Success {
		response.JSON(w, http.StatusUnprocessableEntity, result)
		return
	}
	response.Success(w, result)
}

// Restart handles POST /api/v1/services/{name}/restart.
func (h *ServicesHandler) Restart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	result, err := h.svc.Restart(r.Context(), name)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "SERVICE_RESTART_ERROR", err.Error())
		return
	}
	if !result.Success {
		response.JSON(w, http.StatusUnprocessableEntity, result)
		return
	}
	response.Success(w, result)
}

// Reload handles POST /api/v1/services/{name}/reload.
func (h *ServicesHandler) Reload(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	result, err := h.svc.Reload(r.Context(), name)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "SERVICE_RELOAD_ERROR", err.Error())
		return
	}
	if !result.Success {
		response.JSON(w, http.StatusUnprocessableEntity, result)
		return
	}
	response.Success(w, result)
}

// Enable handles POST /api/v1/services/{name}/enable.
func (h *ServicesHandler) Enable(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	result, err := h.svc.Enable(r.Context(), name)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "SERVICE_ENABLE_ERROR", err.Error())
		return
	}
	if !result.Success {
		response.JSON(w, http.StatusUnprocessableEntity, result)
		return
	}
	response.Success(w, result)
}

// Disable handles POST /api/v1/services/{name}/disable.
func (h *ServicesHandler) Disable(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	result, err := h.svc.Disable(r.Context(), name)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "SERVICE_DISABLE_ERROR", err.Error())
		return
	}
	if !result.Success {
		response.JSON(w, http.StatusUnprocessableEntity, result)
		return
	}
	response.Success(w, result)
}

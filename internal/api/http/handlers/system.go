// Package handlers contains the HTTP handler implementations for the agent API.
package handlers

import (
	"context"
	"net/http"

	"github.com/plusclouds/ubuntu-agent/internal/api/http/response"
	"github.com/plusclouds/ubuntu-agent/internal/config"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
)

// SystemProvider is the interface the system handler depends on.
// *system.Module satisfies this interface.
type SystemProvider interface {
	GetInfo(ctx context.Context) (*system.SystemInfo, error)
	GetMetrics(ctx context.Context) (*system.SystemMetrics, error)
	GetCPU(ctx context.Context) (*system.CPUStats, error)
	GetMemory(ctx context.Context) (*system.MemoryStats, error)
	GetDisk(ctx context.Context) (*system.DiskStats, error)
	GetNetwork(ctx context.Context) (*system.NetworkStats, error)
}

// SystemHandler wires the system module to HTTP endpoints.
type SystemHandler struct {
	sys SystemProvider
}

// NewSystemHandler creates a SystemHandler backed by the given system provider.
func NewSystemHandler(sys SystemProvider) *SystemHandler {
	return &SystemHandler{sys: sys}
}

// Healthz handles GET /healthz.
// Returns a simple health payload without authentication. Used by load balancers
// and systemd watchdog checks.
func (h *SystemHandler) Healthz(w http.ResponseWriter, r *http.Request) {
	response.Success(w, map[string]string{
		"status":  "ok",
		"version": config.AgentVersion,
	})
}

// GetInfo handles GET /api/v1/system/info.
// Returns static/semi-static information about the host VM.
func (h *SystemHandler) GetInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.sys.GetInfo(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "SYSTEM_INFO_ERROR", err.Error())
		return
	}
	response.Success(w, info)
}

// GetMetrics handles GET /api/v1/system/metrics.
// Returns a full snapshot of all resource metrics (CPU, memory, disk, network).
func (h *SystemHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := h.sys.GetMetrics(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "METRICS_ERROR", err.Error())
		return
	}
	response.Success(w, metrics)
}

// GetCPU handles GET /api/v1/system/cpu.
// Returns current CPU utilisation and load averages.
func (h *SystemHandler) GetCPU(w http.ResponseWriter, r *http.Request) {
	stats, err := h.sys.GetCPU(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "CPU_ERROR", err.Error())
		return
	}
	response.Success(w, stats)
}

// GetMemory handles GET /api/v1/system/memory.
// Returns current RAM and swap utilisation.
func (h *SystemHandler) GetMemory(w http.ResponseWriter, r *http.Request) {
	stats, err := h.sys.GetMemory(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "MEMORY_ERROR", err.Error())
		return
	}
	response.Success(w, stats)
}

// GetDisk handles GET /api/v1/system/disk.
// Returns usage data for all mounted partitions.
func (h *SystemHandler) GetDisk(w http.ResponseWriter, r *http.Request) {
	stats, err := h.sys.GetDisk(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "DISK_ERROR", err.Error())
		return
	}
	response.Success(w, stats)
}

// GetNetwork handles GET /api/v1/system/network.
// Returns I/O counters for all network interfaces.
func (h *SystemHandler) GetNetwork(w http.ResponseWriter, r *http.Request) {
	stats, err := h.sys.GetNetwork(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "NETWORK_ERROR", err.Error())
		return
	}
	response.Success(w, stats)
}

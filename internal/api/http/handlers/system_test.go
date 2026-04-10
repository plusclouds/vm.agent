package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/plusclouds/ubuntu-agent/internal/api/http/handlers"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
)

// fakeSystem is a test double for SystemProvider.
type fakeSystem struct {
	info    *system.SystemInfo
	metrics *system.SystemMetrics
	cpu     *system.CPUStats
	memory  *system.MemoryStats
	disk    *system.DiskStats
	network *system.NetworkStats
	err     error
}

func (f *fakeSystem) GetInfo(_ context.Context) (*system.SystemInfo, error) {
	return f.info, f.err
}
func (f *fakeSystem) GetMetrics(_ context.Context) (*system.SystemMetrics, error) {
	return f.metrics, f.err
}
func (f *fakeSystem) GetCPU(_ context.Context) (*system.CPUStats, error) {
	return f.cpu, f.err
}
func (f *fakeSystem) GetMemory(_ context.Context) (*system.MemoryStats, error) {
	return f.memory, f.err
}
func (f *fakeSystem) GetDisk(_ context.Context) (*system.DiskStats, error) {
	return f.disk, f.err
}
func (f *fakeSystem) GetNetwork(_ context.Context) (*system.NetworkStats, error) {
	return f.network, f.err
}

// --- helpers ---

type respEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func doGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) respEnvelope {
	t.Helper()
	var env respEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env
}

// --- Healthz ---

func TestHealthz_Returns200(t *testing.T) {
	h := handlers.NewSystemHandler(&fakeSystem{})
	rec := doGet(t, http.HandlerFunc(h.Healthz), "/healthz")
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if !env.Success {
		t.Error("expected success=true")
	}
}

// --- GetInfo ---

func TestGetInfo_Success(t *testing.T) {
	fake := &fakeSystem{
		info: &system.SystemInfo{Hostname: "test-host", VMID: "vm-001"},
	}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetInfo), "/api/v1/system/info")

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if !env.Success {
		t.Error("expected success=true")
	}

	var info system.SystemInfo
	if err := json.Unmarshal(env.Data, &info); err != nil {
		t.Fatal(err)
	}
	if info.Hostname != "test-host" {
		t.Errorf("hostname: got %q, want test-host", info.Hostname)
	}
	if info.VMID != "vm-001" {
		t.Errorf("vm_id: got %q, want vm-001", info.VMID)
	}
}

func TestGetInfo_Error_Returns500(t *testing.T) {
	fake := &fakeSystem{err: errors.New("gopsutil boom")}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetInfo), "/api/v1/system/info")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Success {
		t.Error("expected success=false on error")
	}
	if env.Error == nil || env.Error.Code != "SYSTEM_INFO_ERROR" {
		t.Errorf("expected SYSTEM_INFO_ERROR, got %v", env.Error)
	}
}

// --- GetCPU ---

func TestGetCPU_Success(t *testing.T) {
	fake := &fakeSystem{
		cpu: &system.CPUStats{CoreCount: 4, UsagePercent: 12.5},
	}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetCPU), "/api/v1/system/cpu")

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var stats system.CPUStats
	env := decodeEnvelope(t, rec)
	if err := json.Unmarshal(env.Data, &stats); err != nil {
		t.Fatal(err)
	}
	if stats.CoreCount != 4 {
		t.Errorf("core_count: got %d, want 4", stats.CoreCount)
	}
}

func TestGetCPU_Error_Returns500(t *testing.T) {
	fake := &fakeSystem{err: errors.New("cpu error")}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetCPU), "/api/v1/system/cpu")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// --- GetMemory ---

func TestGetMemory_Success(t *testing.T) {
	fake := &fakeSystem{
		memory: &system.MemoryStats{TotalBytes: 8 * 1024 * 1024 * 1024},
	}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetMemory), "/api/v1/system/memory")
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestGetMemory_Error_Returns500(t *testing.T) {
	fake := &fakeSystem{err: errors.New("mem error")}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetMemory), "/api/v1/system/memory")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// --- GetDisk ---

func TestGetDisk_Success(t *testing.T) {
	fake := &fakeSystem{
		disk: &system.DiskStats{
			Partitions: []system.PartitionStats{
				{Device: "/dev/sda1", Mountpoint: "/", TotalBytes: 100},
			},
		},
	}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetDisk), "/api/v1/system/disk")
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var disk system.DiskStats
	env := decodeEnvelope(t, rec)
	if err := json.Unmarshal(env.Data, &disk); err != nil {
		t.Fatal(err)
	}
	if len(disk.Partitions) != 1 {
		t.Errorf("expected 1 partition, got %d", len(disk.Partitions))
	}
}

func TestGetDisk_Error_Returns500(t *testing.T) {
	fake := &fakeSystem{err: errors.New("disk error")}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetDisk), "/api/v1/system/disk")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// --- GetNetwork ---

func TestGetNetwork_Success(t *testing.T) {
	fake := &fakeSystem{
		network: &system.NetworkStats{
			Interfaces: []system.InterfaceStats{
				{Name: "eth0", IsUp: true},
			},
		},
	}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetNetwork), "/api/v1/system/network")
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestGetNetwork_Error_Returns500(t *testing.T) {
	fake := &fakeSystem{err: errors.New("net error")}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetNetwork), "/api/v1/system/network")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// --- GetMetrics ---

func TestGetMetrics_Success(t *testing.T) {
	fake := &fakeSystem{
		metrics: &system.SystemMetrics{CollectedAt: 1234567890},
	}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetMetrics), "/api/v1/system/metrics")
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestGetMetrics_Error_Returns500(t *testing.T) {
	fake := &fakeSystem{err: errors.New("metrics error")}
	h := handlers.NewSystemHandler(fake)
	rec := doGet(t, http.HandlerFunc(h.GetMetrics), "/api/v1/system/metrics")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

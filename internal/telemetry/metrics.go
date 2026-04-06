// Package telemetry provides Prometheus metrics for the PlusClouds agent.
// All metrics use the "plusclouds_agent_" prefix for easy identification
// in a multi-exporter Prometheus setup.
package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
)

// Metric names.
const (
	namespace = "plusclouds_agent"
)

// nolint: gochecknoglobals
var (
	// CPUUsageGauge tracks the current overall CPU usage percentage.
	CPUUsageGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "cpu_usage_percent",
		Help:      "Current CPU usage as a percentage (0-100).",
	})

	// MemUsageGauge tracks the current memory (RAM) usage percentage.
	MemUsageGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "memory_usage_percent",
		Help:      "Current memory usage as a percentage (0-100).",
	})

	// DiskUsageGauge tracks disk usage per mountpoint.
	DiskUsageGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "disk_usage_percent",
		Help:      "Current disk usage as a percentage (0-100) per mountpoint.",
	}, []string{"mountpoint"})

	// ServiceStateGauge tracks the state of individual systemd services.
	// The value is 1 for the service's current state, 0 for others.
	// Labels: name (unit name), state (active|inactive|failed|unknown).
	ServiceStateGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "service_state",
		Help:      "Current state of a systemd service (1 = current state, 0 = not in this state).",
	}, []string{"name", "state"})

	// APIRequestsTotal counts HTTP requests processed by the agent API.
	APIRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests processed by the agent API.",
	}, []string{"method", "path", "status"})

	// APIRequestDuration records the latency distribution of HTTP requests.
	APIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request duration in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	// HeartbeatTotal counts the total number of heartbeats successfully sent
	// to the PlusClouds control plane.
	HeartbeatTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "heartbeats_total",
		Help:      "Total number of heartbeats successfully sent to the control plane.",
	})
)

// Register is a no-op when using promauto (metrics register themselves).
// It is provided as a named hook for explicit registration workflows or
// testing with a custom registry.
func Register() {
	// promauto registers all vars above with prometheus.DefaultRegisterer
	// at init time. This function exists as a clear initialisation point.
}

// UpdateSystemMetrics updates the CPU, memory, and disk Prometheus gauges
// from a freshly collected SystemMetrics snapshot.
func UpdateSystemMetrics(m *system.SystemMetrics) {
	CPUUsageGauge.Set(m.CPU.UsagePercent)
	MemUsageGauge.Set(m.Memory.UsagePercent)

	for _, p := range m.Disk.Partitions {
		DiskUsageGauge.With(prometheus.Labels{
			"mountpoint": p.Mountpoint,
		}).Set(p.UsagePercent)
	}
}

// UpdateServiceMetrics updates the service state gauge for all services
// in the provided slice. Each service gets a label set for each possible
// state value; only the current state is set to 1.
func UpdateServiceMetrics(svcs []services.ServiceInfo) {
	allStates := []services.ServiceState{
		services.StateActive,
		services.StateInactive,
		services.StateFailed,
		services.StateUnknown,
	}

	for _, svc := range svcs {
		for _, s := range allStates {
			val := 0.0
			if svc.State == s {
				val = 1.0
			}
			ServiceStateGauge.With(prometheus.Labels{
				"name":  svc.Name,
				"state": string(s),
			}).Set(val)
		}
	}
}

// Package http wires together the Chi router, middleware, and all handler groups.
package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/api/http/handlers"
	"github.com/plusclouds/ubuntu-agent/internal/api/http/middleware"
	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// NewRouter constructs and returns the fully configured Chi router.
//
// All routes require API key authentication. The agent may be deployed
// in a DMZ or otherwise exposed network, so /healthz and /metrics are
// not exempt — callers (orchestrator, Prometheus, load balancer probes)
// must supply a valid Bearer token or X-API-Key header.
//
// Route layout:
//
//	GET  /healthz                        — liveness probe (auth required)
//	GET  /metrics                        — Prometheus scrape target (auth required)
//	/api/v1/
//	  GET  /system/info
//	  GET  /system/metrics
//	  GET  /system/cpu
//	  GET  /system/memory
//	  GET  /system/disk
//	  GET  /system/network
//	  GET  /services
//	  GET  /services/{name}
//	  POST /services/{name}/start
//	  POST /services/{name}/stop
//	  POST /services/{name}/restart
//	  POST /services/{name}/reload
//	  POST /services/{name}/enable
//	  POST /services/{name}/disable
//	  GET  /metadata
//	  GET  /metadata/instance
//	  GET  /metadata/network
//	  GET  /metadata/services
func NewRouter(
	apiKey string,
	sysMod *system.Module,
	svcMod *services.Module,
	iso *isoconfig.ISOMetadata,
	logger *zap.Logger,
) http.Handler {
	r := chi.NewRouter()

	// Global middleware: recover from panics, set request ID, log, authenticate.
	// Auth is applied globally — no endpoint is exempt.
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.RequestLogger(logger))
	r.Use(middleware.Auth(apiKey))

	// Health and metrics endpoints (auth enforced by global middleware).
	sysHandler := handlers.NewSystemHandler(sysMod)
	r.Get("/healthz", sysHandler.Healthz)
	r.Handle("/metrics", promhttp.Handler())

	// API routes.
	r.Route("/api/v1", func(r chi.Router) {

		// System resource endpoints.
		r.Route("/system", func(r chi.Router) {
			r.Get("/info", sysHandler.GetInfo)
			r.Get("/metrics", sysHandler.GetMetrics)
			r.Get("/cpu", sysHandler.GetCPU)
			r.Get("/memory", sysHandler.GetMemory)
			r.Get("/disk", sysHandler.GetDisk)
			r.Get("/network", sysHandler.GetNetwork)
		})

		// Systemd service management endpoints.
		svcHandler := handlers.NewServicesHandler(svcMod)
		r.Route("/services", func(r chi.Router) {
			r.Get("/", svcHandler.List)
			r.Route("/{name}", func(r chi.Router) {
				r.Get("/", svcHandler.Get)
				r.Post("/start", svcHandler.Start)
				r.Post("/stop", svcHandler.Stop)
				r.Post("/restart", svcHandler.Restart)
				r.Post("/reload", svcHandler.Reload)
				r.Post("/enable", svcHandler.Enable)
				r.Post("/disable", svcHandler.Disable)
			})
		})

		// ISO metadata endpoints.
		metaHandler := handlers.NewMetadataHandler(iso)
		r.Route("/metadata", func(r chi.Router) {
			r.Get("/", metaHandler.Show)
			r.Get("/instance", metaHandler.GetInstance)
			r.Get("/network", metaHandler.GetNetwork)
			r.Get("/services", metaHandler.GetServices)
		})
	})

	return r
}

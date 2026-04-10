// Command plusclouds-agent is the PlusClouds VM agent daemon.
// It collects system metrics, manages systemd services, exposes a REST and
// gRPC API, and maintains a heartbeat connection with the PlusClouds control
// plane.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/plusclouds/ubuntu-agent/internal/api/grpc"
	agenthttp "github.com/plusclouds/ubuntu-agent/internal/api/http"
	"github.com/plusclouds/ubuntu-agent/internal/api/http/response"
	"github.com/plusclouds/ubuntu-agent/internal/config"
	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
	"github.com/plusclouds/ubuntu-agent/internal/registry"
	"github.com/plusclouds/ubuntu-agent/internal/telemetry"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

var cfgFile string

func main() {
	root := &cobra.Command{
		Use:     "plusclouds-agent",
		Short:   "PlusClouds Ubuntu VM Agent",
		Version: config.AgentVersion,
		RunE:    run,
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "",
		"Path to config file (default: /etc/plusclouds/agent.yaml)")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, _ []string) error {
	// ------------------------------------------------------------------ //
	// 1. Load configuration
	// ------------------------------------------------------------------ //
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// ------------------------------------------------------------------ //
	// 2. Initialise zap logger
	// ------------------------------------------------------------------ //
	logger, err := buildLogger(cfg)
	if err != nil {
		return fmt.Errorf("building logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	logger.Info("PlusClouds agent starting",
		zap.String("version", config.AgentVersion),
		zap.String("config_file", cfgFile),
	)

	// ------------------------------------------------------------------ //
	// 3. Read ISO metadata
	// ------------------------------------------------------------------ //
	isoReader := isoconfig.NewReader(cfg.ISO.MountPath)
	iso, err := isoReader.Read()
	if err != nil {
		logger.Warn("could not read ISO config drive; running in local-only mode",
			zap.String("mount_path", cfg.ISO.MountPath),
			zap.Error(err),
		)
		iso = &isoconfig.ISOMetadata{}
	} else {
		logger.Info("ISO metadata loaded",
			zap.String("vm_id", iso.VMID()),
			zap.String("tenant_id", iso.TenantID()),
		)
	}

	// Merge ISO credentials into config (ISO takes precedence over config file).
	if iso.APIKey() != "" {
		cfg.Auth.APIKey = iso.APIKey()
	}
	if iso.ControlPlaneURL() != "" {
		cfg.Registry.Endpoint = iso.ControlPlaneURL()
	}

	// Set agent ID for API response metadata.
	response.SetAgentID(iso.VMID())

	// ------------------------------------------------------------------ //
	// 4. Connect to systemd D-Bus
	// ------------------------------------------------------------------ //
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var dbusConn *dbus.Conn
	dbusConn, err = dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		logger.Warn("could not connect to systemd D-Bus; service management will be unavailable",
			zap.Error(err),
		)
	} else {
		defer dbusConn.Close()
		logger.Info("connected to systemd D-Bus")
	}

	// ------------------------------------------------------------------ //
	// 5. Initialise modules
	// ------------------------------------------------------------------ //
	sysMod := system.New(iso)
	svcMod := services.New(dbusConn, logger) // dbusConn may be nil in dev/non-root mode
	logger.Info("system and services modules initialised")

	// ------------------------------------------------------------------ //
	// 6. Register with the PlusClouds orchestrator  ← MANDATORY GATE
	//
	// The agent reads the AgentToken from the ISO config drive and presents
	// it to the orchestrator. The orchestrator validates the token, records
	// the agent in its inventory, and returns a SessionToken.
	//
	// No HTTP or gRPC server is started until this succeeds. If the control
	// plane endpoint is not configured the agent runs in local-only mode
	// (ErrNoControlPlane). Any other error is fatal.
	// ------------------------------------------------------------------ //
	reg := registry.New(cfg, iso, sysMod, logger)
	if err := reg.Register(ctx); err != nil {
		if errors.Is(err, registry.ErrNoControlPlane) {
			logger.Warn("no orchestrator endpoint configured — starting in local-only mode")
		} else {
			// Hard failure: stop here, do not expose any API.
			return fmt.Errorf("orchestrator registration failed: %w", err)
		}
	}
	reg.StartHeartbeat(ctx)

	// ------------------------------------------------------------------ //
	// 7. Initialise telemetry
	// ------------------------------------------------------------------ //
	telemetry.Register()
	logger.Info("Prometheus metrics registered")

	// ------------------------------------------------------------------ //
	// 8. Build HTTP router and start HTTP server
	// ------------------------------------------------------------------ //
	router := agenthttp.NewRouter(cfg.Auth.APIKey, sysMod, svcMod, iso, logger)
	httpServer := agenthttp.New(cfg, router, logger)

	httpErrCh := make(chan error, 1)
	go func() {
		if err := httpServer.Start(ctx); err != nil {
			httpErrCh <- err
		}
		close(httpErrCh)
	}()

	// ------------------------------------------------------------------ //
	// 9. Start gRPC server
	// ------------------------------------------------------------------ //
	grpcServer := grpc.New(cfg, logger)
	grpcErrCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Start(ctx); err != nil {
			grpcErrCh <- err
		}
		close(grpcErrCh)
	}()

	logger.Info("agent started",
		zap.Int("http_port", cfg.Server.HTTP.Port),
		zap.Int("grpc_port", cfg.Server.GRPC.Port),
	)

	// ------------------------------------------------------------------ //
	// 10. Wait for OS signal
	// ------------------------------------------------------------------ //
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	case err := <-httpErrCh:
		if err != nil {
			logger.Error("HTTP server error", zap.Error(err))
		}
	case err := <-grpcErrCh:
		if err != nil {
			logger.Error("gRPC server error", zap.Error(err))
		}
	}

	// ------------------------------------------------------------------ //
	// 11. Graceful shutdown
	// ------------------------------------------------------------------ //
	logger.Info("initiating graceful shutdown")
	cancel() // signal all goroutines to stop

	// Wait for servers to drain.
	if err := <-httpErrCh; err != nil {
		logger.Warn("HTTP server shutdown error", zap.Error(err))
	}
	if err := <-grpcErrCh; err != nil {
		logger.Warn("gRPC server shutdown error", zap.Error(err))
	}

	// D-Bus connection is closed by the deferred dbusConn.Close() above.
	logger.Info("agent stopped cleanly")
	return nil
}

// buildLogger constructs a zap logger from the configuration.
func buildLogger(cfg *config.Config) (*zap.Logger, error) {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		level = zapcore.InfoLevel
	}

	var zapCfg zap.Config
	if cfg.Log.Format == "console" {
		zapCfg = zap.NewDevelopmentConfig()
	} else {
		zapCfg = zap.NewProductionConfig()
	}
	zapCfg.Level = zap.NewAtomicLevelAt(level)

	return zapCfg.Build()
}

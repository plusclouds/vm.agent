// Command plusclouds-agent is the PlusClouds VM agent daemon.
// It collects system metrics, manages services, and communicates with the
// PlusClouds platform exclusively via NATS. Supports Linux and Windows.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/plusclouds/ubuntu-agent/internal/config"
	"github.com/plusclouds/ubuntu-agent/internal/dispatcher"
	"github.com/plusclouds/ubuntu-agent/internal/executor"
	"github.com/plusclouds/ubuntu-agent/internal/modules/autoheal"
	"github.com/plusclouds/ubuntu-agent/internal/modules/diskresize"
	"github.com/plusclouds/ubuntu-agent/internal/modules/envvars"
	"github.com/plusclouds/ubuntu-agent/internal/modules/identity"
	"github.com/plusclouds/ubuntu-agent/internal/modules/netconfig"
	"github.com/plusclouds/ubuntu-agent/internal/modules/sshkeys"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
	natsclient "github.com/plusclouds/ubuntu-agent/internal/nats"
	"github.com/plusclouds/ubuntu-agent/internal/protocol"
	"github.com/plusclouds/ubuntu-agent/internal/publisher"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

func main() {
	// On Windows, when launched by the Service Control Manager rather than
	// interactively, run() must be driven by svc.Run instead of a plain CLI
	// invocation — see service_windows.go. maybeRunAsService is a no-op
	// stub on other platforms (service_linux.go).
	if handled, err := maybeRunAsService(); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	root := &cobra.Command{
		Use:     "plusclouds-agent",
		Short:   "PlusClouds VM Agent",
		Version: config.AgentVersion,
		RunE:    runCLI,
	}

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runCLI runs the agent in the foreground, shutting down cleanly on
// SIGINT/SIGTERM — the path used when running interactively or under
// systemd (which sends SIGTERM to stop the unit).
func runCLI(_ *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return run(ctx, cancel)
}

// run drives the agent's full startup sequence and blocks until ctx is
// cancelled — by an OS signal (runCLI) or by the Windows service control
// handler responding to a Stop/Shutdown request (service_windows.go).
func run(ctx context.Context, cancel context.CancelFunc) error {
	// ------------------------------------------------------------------ //
	// 1. Bootstrap: built-in defaults only, just enough to locate the
	//    config-drive (or its local cache).
	// ------------------------------------------------------------------ //
	bootCfg, err := config.Load(nil)
	if err != nil {
		return fmt.Errorf("loading bootstrap config: %w", err)
	}
	bootLogger, err := buildLogger(bootCfg)
	if err != nil {
		return fmt.Errorf("building bootstrap logger: %w", err)
	}

	bootExec := executor.New(bootLogger)

	// ------------------------------------------------------------------ //
	// 2. Locate and read the config-drive (or its local cache), then build
	//    the real configuration by merging its "agent" settings on top of
	//    defaults. There is no config file anymore — the platform writes
	//    everything (identity, NATS, allowed operations, logging, ...)
	//    into pc-meta-data.json at provisioning time.
	// ------------------------------------------------------------------ //
	locator := isoconfig.NewLocator(bootExec, bootLogger)
	iso, err := isoconfig.Load(ctx, locator, bootCfg.ISO.MountPath, bootCfg.ISO.CachePath, bootCfg.ISO.Label, bootLogger)
	if err != nil {
		bootLogger.Warn("could not resolve config-drive metadata, running on built-in defaults",
			zap.Error(err),
		)
		iso = &isoconfig.ISOMetadata{}
	}

	cfg, err := config.Load(iso.AgentSettings())
	if err != nil {
		return fmt.Errorf("building config from config-drive metadata: %w", err)
	}

	// ------------------------------------------------------------------ //
	// 3. Initialise the real logger, now that log config may have been
	//    overridden by the config-drive metadata.
	// ------------------------------------------------------------------ //
	logger, err := buildLogger(cfg)
	if err != nil {
		return fmt.Errorf("building logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	logger.Info("PlusClouds agent starting",
		zap.String("version", config.AgentVersion),
	)

	exec := executor.New(logger)

	// ------------------------------------------------------------------ //
	// 4. Resolve identity. The top-level config-drive identity fields
	//    (virtual_machine_id, agent_api_key) always take precedence over
	//    whatever the merged agent.nats settings say.
	// ------------------------------------------------------------------ //
	agentUUID := cfg.NATS.AgentUUID
	agentAPIKey := cfg.NATS.APIKey

	if iso.VMID() != "" {
		agentUUID = iso.VMID()
		agentAPIKey = iso.AgentAPIKey()
	}

	if agentUUID == "" {
		logger.Warn("agent_uuid is not set — NATS auth will fail; check the config-drive metadata")
	}
	if agentAPIKey == "" {
		logger.Warn("api_key is not set — NATS auth will fail; check the config-drive metadata")
	}

	logger.Info("agent identity resolved", zap.String("agent_uuid", agentUUID))

	// ------------------------------------------------------------------ //
	// 5. Construct the config-drive-driven modules once, so the same
	//    instances back both the boot-time apply below and the live
	//    dispatcher operations wired up later.
	// ------------------------------------------------------------------ //
	sshProvisioner := sshkeys.New()
	identitySetter := identity.New(exec)
	netCfg := netconfig.New(exec, logger)
	envMgr := envvars.New(logger)
	resizer := diskresize.New(exec, logger)

	// ------------------------------------------------------------------ //
	// 6. Apply config-drive desired state at boot: password, network,
	//    disk resize, SSH keys, env vars. Every step here is best-effort
	//    and non-fatal (log a warning and continue) — a bad NIC config or
	//    unresolvable user must never block the agent from starting and
	//    reaching NATS, which is the actual recovery path if boot-time
	//    apply fails.
	//
	//    Note: unlike SSH keys (purely additive), password and network
	//    config are declarative — they re-assert the config-drive's value
	//    on every boot. If the platform pushes a live identity.set_password
	//    or network.configure command over NATS, a later reboot will
	//    re-apply whatever is cached in the config-drive metadata unless
	//    the platform also keeps pc-meta-data.json in sync with any live
	//    change it issues.
	// ------------------------------------------------------------------ //
	if username, password := iso.Username(), iso.Password(); username != "" && password != "" {
		if _, err := identitySetter.SetPassword(ctx, username, password); err != nil {
			logger.Warn("could not set password from config-drive metadata",
				zap.String("username", username),
				zap.Error(err),
			)
		} else {
			logger.Info("set password from config-drive metadata", zap.String("username", username))
		}
	}

	for _, nic := range iso.VirtualNetworkCards() {
		if _, err := netCfg.Configure(ctx, nic); err != nil {
			logger.Warn("could not apply network configuration from config-drive metadata",
				zap.String("mac_addr", nic.MACAddr),
				zap.Error(err),
			)
		} else {
			logger.Info("applied network configuration from config-drive metadata",
				zap.String("mac_addr", nic.MACAddr),
			)
		}
	}

	if result, err := resizer.Resize(ctx, diskresize.DefaultTarget()); err != nil {
		logger.Warn("could not resize disk at boot", zap.Error(err))
	} else {
		logger.Info("resized disk at boot",
			zap.String("target", result.Target),
			zap.Int64("grown_bytes", result.GrownBytes),
		)
	}

	if username, keys := iso.Username(), iso.SSHKeys(); username != "" && len(keys) > 0 {
		result, err := sshProvisioner.Provision(username, keys)
		if err != nil {
			logger.Warn("could not provision SSH keys from config-drive metadata",
				zap.String("username", username),
				zap.Error(err),
			)
		} else if len(result.AddedKeys) > 0 {
			logger.Info("provisioned SSH keys from config-drive metadata",
				zap.String("username", username),
				zap.String("path", result.Path),
				zap.Int("added", len(result.AddedKeys)),
				zap.Int("total", result.TotalKeys),
			)
		}
	}

	for _, ev := range iso.EnvVars() {
		if _, err := envMgr.Set(ctx, ev.Key, ev.Value); err != nil {
			logger.Warn("could not set environment variable from config-drive metadata",
				zap.String("key", ev.Key),
				zap.Error(err),
			)
		}
	}

	// ------------------------------------------------------------------ //
	// 7. Initialise platform-specific service manager
	// ------------------------------------------------------------------ //
	svcMgr, svcCleanup := newServiceManager(ctx, logger)
	defer svcCleanup()

	cronLister, cronCleanup := newCronLister(ctx, logger)
	defer cronCleanup()

	// ------------------------------------------------------------------ //
	// 8. Initialise remaining modules
	// ------------------------------------------------------------------ //
	sysMod := system.New(iso)
	logger.Info("modules initialised",
		zap.Int("allowed_operations", len(cfg.Agent.AllowedOperations)),
		zap.Int("allowed_commands", len(cfg.Agent.AllowedCommands)),
		zap.Duration("telemetry_interval", cfg.Agent.TelemetryInterval),
		zap.Duration("heartbeat_interval", cfg.Agent.HeartbeatInterval),
	)

	if cfg.Autoheal.Enabled {
		autoheal.New(svcMgr, cfg.Autoheal.Services, cfg.Autoheal.RestartDelay, logger).Start(ctx)
	}

	// ------------------------------------------------------------------ //
	// 9. Connect to NATS
	// ------------------------------------------------------------------ //
	nc, err := natsclient.Connect(cfg.NATS, agentUUID, agentAPIKey, logger)
	if err != nil {
		return fmt.Errorf("NATS connection failed: %w", err)
	}
	defer nc.Drain()

	// ------------------------------------------------------------------ //
	// 10. Create publisher and dispatcher
	// ------------------------------------------------------------------ //
	pub := publisher.New(nc, sysMod, agentUUID, cfg.Agent, logger)

	disp := dispatcher.New(
		sysMod, svcMgr, exec, resizer, cronLister,
		identitySetter, netCfg, sshProvisioner, envMgr,
		pub,
		agentUUID,
		cfg.Agent.AllowedOperations,
		cfg.Agent.AllowedCommands,
		logger,
	)

	// ------------------------------------------------------------------ //
	// 11. Subscribe to cmd subject
	// ------------------------------------------------------------------ //
	if err := nc.Subscribe(func(env protocol.Envelope) {
		result := disp.Dispatch(ctx, env)

		if env.ReplyTo != "" {
			// Synchronous caller is blocking on this inbox — reply directly.
			if err := nc.PublishToSubject(env.ReplyTo, result); err != nil {
				logger.Error("could not publish sync result",
					zap.String("command_id", env.ID),
					zap.String("reply_to", env.ReplyTo),
					zap.Error(err),
				)
			}
		} else {
			// Async path — result goes to the evt subject as usual.
			if err := nc.Publish(result); err != nil {
				logger.Error("could not publish result to evt subject",
					zap.String("command_id", env.ID),
					zap.Error(err),
				)
			}
		}
	}); err != nil {
		return fmt.Errorf("subscribing to NATS cmd subject: %w", err)
	}

	// ------------------------------------------------------------------ //
	// 12. Start heartbeat and telemetry publisher
	// ------------------------------------------------------------------ //
	pub.Start(ctx)

	logger.Info("agent started",
		zap.String("nats_url", cfg.NATS.ActiveURL()),
		zap.String("cmd_subject", nc.CmdSubject()),
		zap.String("evt_subject", nc.EvtSubject()),
	)

	// ------------------------------------------------------------------ //
	// 13. Wait for shutdown — ctx is cancelled either by an OS signal
	//    (runCLI) or by the Windows service control handler responding to
	//    a Stop/Shutdown request (service_windows.go).
	// ------------------------------------------------------------------ //
	<-ctx.Done()
	logger.Info("received shutdown signal")

	logger.Info("initiating graceful shutdown")
	cancel()
	logger.Info("agent stopped cleanly")
	return nil
}

func buildLogger(cfg *config.Config) (*zap.Logger, error) {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		level = zapcore.InfoLevel
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	var stdoutEnc zapcore.Encoder
	if cfg.Log.Format == "console" {
		consoleEncCfg := zap.NewDevelopmentEncoderConfig()
		consoleEncCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		stdoutEnc = zapcore.NewConsoleEncoder(consoleEncCfg)
	} else {
		stdoutEnc = zapcore.NewJSONEncoder(encCfg)
	}
	stdoutCore := zapcore.NewCore(stdoutEnc, zapcore.AddSync(os.Stdout), level)
	cores := []zapcore.Core{stdoutCore}

	if cfg.Log.File != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.Log.File), 0755); err != nil {
			return nil, fmt.Errorf("creating log directory: %w", err)
		}
		f, err := os.OpenFile(cfg.Log.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("opening log file %s: %w", cfg.Log.File, err)
		}
		fileCore := zapcore.NewCore(zapcore.NewJSONEncoder(encCfg), zapcore.AddSync(f), level)
		cores = append(cores, fileCore)
	}

	return zap.New(zapcore.NewTee(cores...), zap.AddCaller()), nil
}

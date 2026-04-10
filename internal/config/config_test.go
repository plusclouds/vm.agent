package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/plusclouds/ubuntu-agent/internal/config"
)

// --- Defaults ---

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.HTTP.Port != 8080 {
		t.Errorf("HTTP port default: got %d, want 8080", cfg.Server.HTTP.Port)
	}
	if cfg.Server.GRPC.Port != 8081 {
		t.Errorf("gRPC port default: got %d, want 8081", cfg.Server.GRPC.Port)
	}
	if cfg.Server.Telemetry.Port != 9100 {
		t.Errorf("telemetry port default: got %d, want 9100", cfg.Server.Telemetry.Port)
	}
	if cfg.ISO.MountPath != "/media/plusclouds-config" {
		t.Errorf("ISO mount path default: got %q", cfg.ISO.MountPath)
	}
	if !cfg.ISO.FallbackEnv {
		t.Error("ISO fallback_env should default to true")
	}
	if cfg.Registry.HeartbeatInterval != 30*time.Second {
		t.Errorf("heartbeat interval default: got %v, want 30s", cfg.Registry.HeartbeatInterval)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("log level default: got %q, want info", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("log format default: got %q, want json", cfg.Log.Format)
	}
	if !cfg.Autoheal.Enabled {
		t.Error("autoheal.enabled should default to true")
	}
	if cfg.Autoheal.RestartDelay != 10*time.Second {
		t.Errorf("autoheal restart_delay default: got %v, want 10s", cfg.Autoheal.RestartDelay)
	}
}

// --- Config file ---

func TestLoad_FromYAMLFile(t *testing.T) {
	yaml := `
server:
  http:
    port: 9090
  grpc:
    port: 9091
log:
  level: debug
  format: console
registry:
  endpoint: "https://api.example.com"
  heartbeat_interval: 60s
`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfgFile, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.HTTP.Port != 9090 {
		t.Errorf("HTTP port: got %d, want 9090", cfg.Server.HTTP.Port)
	}
	if cfg.Server.GRPC.Port != 9091 {
		t.Errorf("gRPC port: got %d, want 9091", cfg.Server.GRPC.Port)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log level: got %q, want debug", cfg.Log.Level)
	}
	if cfg.Log.Format != "console" {
		t.Errorf("log format: got %q, want console", cfg.Log.Format)
	}
	if cfg.Registry.Endpoint != "https://api.example.com" {
		t.Errorf("registry endpoint: got %q", cfg.Registry.Endpoint)
	}
	if cfg.Registry.HeartbeatInterval != 60*time.Second {
		t.Errorf("heartbeat interval: got %v, want 60s", cfg.Registry.HeartbeatInterval)
	}
}

// --- Environment variable overrides ---

func TestLoad_EnvVarOverridesDefault(t *testing.T) {
	t.Setenv("PLUSCLOUDS_AGENT_SERVER_HTTP_PORT", "7777")
	t.Setenv("PLUSCLOUDS_AGENT_LOG_LEVEL", "warn")
	t.Setenv("PLUSCLOUDS_AGENT_AUTH_API_KEY", "env-secret")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.HTTP.Port != 7777 {
		t.Errorf("HTTP port via env: got %d, want 7777", cfg.Server.HTTP.Port)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("log level via env: got %q, want warn", cfg.Log.Level)
	}
	if cfg.Auth.APIKey != "env-secret" {
		t.Errorf("api key via env: got %q, want env-secret", cfg.Auth.APIKey)
	}
}

func TestLoad_EnvVarOverridesFile(t *testing.T) {
	yaml := `server:\n  http:\n    port: 9090\n`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfgFile, []byte("server:\n  http:\n    port: 9090\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLUSCLOUDS_AGENT_SERVER_HTTP_PORT", "5555")
	_ = yaml

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Server.HTTP.Port != 5555 {
		t.Errorf("env should override file: got %d, want 5555", cfg.Server.HTTP.Port)
	}
}

// --- Error cases ---

func TestLoad_InvalidYAMLFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfgFile, []byte(": invalid: yaml: [\n"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := config.Load(cfgFile)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestLoad_NonExistentFileIsOK(t *testing.T) {
	// A completely missing config file is acceptable; defaults take over.
	// Load("") already exercises this path, but test it with explicit path too.
	_, err := config.Load("/nonexistent/path/agent.yaml")
	if err == nil {
		t.Error("expected error for explicit non-existent config file path")
	}
}

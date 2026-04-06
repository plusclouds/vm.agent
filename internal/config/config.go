// Package config handles loading and validation of the agent configuration.
// Configuration is layered: defaults → config file → environment variables.
// Environment variables use the prefix PLUSCLOUDS_AGENT_ and dot-separated
// keys map to underscores (e.g. server.http.port → PLUSCLOUDS_AGENT_SERVER_HTTP_PORT).
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// AgentVersion is set at build time via -ldflags.
const AgentVersion = "0.1.0"

// Config is the top-level configuration structure for the agent.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Auth     AuthConfig     `mapstructure:"auth"`
	ISO      ISOConfig      `mapstructure:"iso"`
	Registry RegistryConfig `mapstructure:"registry"`
	Log      LogConfig      `mapstructure:"log"`
	Autoheal AutohealConfig `mapstructure:"autoheal"`
}

// ServerConfig holds network listener configuration.
type ServerConfig struct {
	HTTP      HTTPConfig      `mapstructure:"http"`
	GRPC      GRPCConfig      `mapstructure:"grpc"`
	Telemetry TelemetryConfig `mapstructure:"telemetry"`
}

// HTTPConfig configures the REST API listener.
type HTTPConfig struct {
	Port int `mapstructure:"port"`
}

// GRPCConfig configures the gRPC listener.
type GRPCConfig struct {
	Port int `mapstructure:"port"`
}

// TelemetryConfig configures the Prometheus metrics listener.
type TelemetryConfig struct {
	Port int `mapstructure:"port"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	// APIKey is the shared secret used to authenticate API requests.
	// It is populated from the ISO config drive credentials.json.
	APIKey string `mapstructure:"api_key"`
}

// ISOConfig holds ISO/config-drive settings.
type ISOConfig struct {
	// MountPath is the filesystem path where the config drive ISO is mounted.
	MountPath string `mapstructure:"mount_path"`
	// FallbackEnv enables falling back to environment variables when ISO files
	// are absent. Useful for development and testing without a real ISO.
	FallbackEnv bool `mapstructure:"fallback_env"`
}

// RegistryConfig holds control plane registration settings.
type RegistryConfig struct {
	// Endpoint is the base URL of the PlusClouds control plane.
	// e.g. https://api.plusclouds.com
	Endpoint string `mapstructure:"endpoint"`
	// HeartbeatInterval is how often the agent sends a heartbeat.
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	// Level is the minimum log level: debug, info, warn, error.
	Level string `mapstructure:"level"`
	// Format controls log output: json or console.
	Format string `mapstructure:"format"`
}

// AutohealConfig holds automatic service recovery settings.
type AutohealConfig struct {
	// Enabled activates automatic restart of failed services.
	Enabled bool `mapstructure:"enabled"`
	// RestartDelay is how long to wait before restarting a failed service.
	RestartDelay time.Duration `mapstructure:"restart_delay"`
}

// Load reads the configuration from the given file path, then overlays
// environment variables (prefix PLUSCLOUDS_AGENT_), and finally applies
// built-in defaults for any missing values.
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	// --- Defaults ---
	setDefaults(v)

	// --- Config file ---
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("agent")
		v.SetConfigType("yaml")
		v.AddConfigPath("/etc/plusclouds")
		v.AddConfigPath("$HOME/.plusclouds")
		v.AddConfigPath("./configs")
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		// It's acceptable to run without a config file (defaults + env vars).
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	// --- Environment variables ---
	v.SetEnvPrefix("PLUSCLOUDS_AGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	return &cfg, nil
}

// setDefaults registers built-in default values.
func setDefaults(v *viper.Viper) {
	v.SetDefault("server.http.port", 8080)
	v.SetDefault("server.grpc.port", 8081)
	v.SetDefault("server.telemetry.port", 9100)

	v.SetDefault("auth.api_key", "")

	v.SetDefault("iso.mount_path", "/media/plusclouds-config")
	v.SetDefault("iso.fallback_env", true)

	v.SetDefault("registry.endpoint", "")
	v.SetDefault("registry.heartbeat_interval", 30*time.Second)

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")

	v.SetDefault("autoheal.enabled", true)
	v.SetDefault("autoheal.restart_delay", 10*time.Second)
}

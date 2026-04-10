package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/config"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
	"github.com/plusclouds/ubuntu-agent/internal/registry"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func nopLogger() *zap.Logger { return zap.NewNop() }

// sysMod returns a real system.Module backed by the local OS.
// Safe for tests — reads /proc only, no side effects.
func sysMod(iso *isoconfig.ISOMetadata) *system.Module { return system.New(iso) }

// cfg builds a minimal config with the given control-plane endpoint and
// auth key (used as the agent token fallback in the registry).
func cfg(endpoint, authKey string) *config.Config {
	return &config.Config{
		Registry: config.RegistryConfig{
			Endpoint:          endpoint,
			HeartbeatInterval: 100 * time.Millisecond,
		},
		Auth: config.AuthConfig{
			APIKey: authKey,
		},
	}
}

// emptyISO is an ISOMetadata with no underlying data — all accessors return "".
func emptyISO() *isoconfig.ISOMetadata { return &isoconfig.ISOMetadata{} }

// vmISO returns an ISOMetadata with minimal VM identity.
func vmISO() *isoconfig.ISOMetadata {
	return isoconfig.New(&isoconfig.VirtualMachineMetadata{
		Hostname:         "vm-test",
		VirtualMachineID: "vm-test-id",
	})
}

// orchestratorOK is an httptest server that accepts registration and heartbeats.
func orchestratorOK(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agents/register":
			resp := map[string]interface{}{
				"agent_id":      "vm-test-id",
				"session_token": "sess-abc-123",
				"expires_at":    time.Now().Add(time.Hour).Unix(),
				"message":       "registered",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
		case "/agents/heartbeat":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestRegister_NoControlPlane_ReturnsErrNoControlPlane(t *testing.T) {
	// Neither ISO nor config carry a control-plane endpoint.
	iso := emptyISO()
	c := cfg("", "")
	reg := registry.New(c, iso, sysMod(iso), nopLogger())

	err := reg.Register(context.Background())
	if !errors.Is(err, registry.ErrNoControlPlane) {
		t.Errorf("expected ErrNoControlPlane, got %v", err)
	}
}

func TestRegister_NoAgentToken_ReturnsErrNoAgentToken(t *testing.T) {
	// Endpoint is set but both ISO and cfg carry no token.
	iso := vmISO()
	c := cfg("https://api.example.com", "" /* no token */)
	reg := registry.New(c, iso, sysMod(iso), nopLogger())

	err := reg.Register(context.Background())
	if !errors.Is(err, registry.ErrNoAgentToken) {
		t.Errorf("expected ErrNoAgentToken, got %v", err)
	}
}

func TestRegister_OrchestratorAccepts(t *testing.T) {
	srv := orchestratorOK(t)
	defer srv.Close()

	iso := vmISO()
	c := cfg(srv.URL, "agent-token-xyz")
	reg := registry.New(c, iso, sysMod(iso), nopLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := reg.Register(ctx); err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}
	if reg.SessionToken() == "" {
		t.Error("expected non-empty session token after registration")
	}
	if reg.SessionToken() != "sess-abc-123" {
		t.Errorf("session token: got %q, want sess-abc-123", reg.SessionToken())
	}
}

func TestRegister_OrchestratorRejects_Returns4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	iso := vmISO()
	c := cfg(srv.URL, "bad-token")
	reg := registry.New(c, iso, sysMod(iso), nopLogger())

	err := reg.Register(context.Background())
	if !errors.Is(err, registry.ErrRegistrationRejected) {
		t.Errorf("expected ErrRegistrationRejected, got %v", err)
	}
}

func TestRegister_OrchestratorServerError_RetriesAndFails(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	iso := vmISO()
	c := cfg(srv.URL, "token")
	reg := registry.New(c, iso, sysMod(iso), nopLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := reg.Register(ctx); err == nil {
		t.Error("expected error after all retries exhausted")
	}
	if attempts < 1 {
		t.Errorf("expected at least 1 attempt, got %d", attempts)
	}
}

func TestRegister_MissingSessionToken_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Valid 200 but session_token is absent.
		resp := map[string]interface{}{
			"agent_id":   "vm-test-id",
			"expires_at": time.Now().Add(time.Hour).Unix(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	iso := vmISO()
	c := cfg(srv.URL, "token")
	reg := registry.New(c, iso, sysMod(iso), nopLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := reg.Register(ctx); err == nil {
		t.Error("expected error when session_token is missing from response")
	}
}

// ---------------------------------------------------------------------------
// SessionToken
// ---------------------------------------------------------------------------

func TestSessionToken_BeforeRegistration_IsEmpty(t *testing.T) {
	iso := emptyISO()
	reg := registry.New(cfg("", ""), iso, sysMod(iso), nopLogger())
	if got := reg.SessionToken(); got != "" {
		t.Errorf("expected empty session token before registration, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Heartbeat
// ---------------------------------------------------------------------------

func TestStartHeartbeat_SendsHeartbeats(t *testing.T) {
	heartbeats := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agents/register":
			resp := map[string]interface{}{
				"agent_id":      "vm-test-id",
				"session_token": "sess-xyz",
				"expires_at":    time.Now().Add(time.Hour).Unix(),
				"message":       "ok",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
		case "/agents/heartbeat":
			heartbeats++
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	iso := vmISO()
	c := &config.Config{
		Registry: config.RegistryConfig{
			Endpoint:          srv.URL,
			HeartbeatInterval: 50 * time.Millisecond,
		},
		Auth: config.AuthConfig{APIKey: "token"},
	}
	reg := registry.New(c, iso, sysMod(iso), nopLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := reg.Register(ctx); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	reg.StartHeartbeat(ctx)

	// GetCPU samples for 500 ms each call; two heartbeats = ~1200 ms minimum.
	time.Sleep(1500 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond) // let the goroutine notice cancellation

	if heartbeats < 2 {
		t.Errorf("expected at least 2 heartbeats, got %d", heartbeats)
	}
}

func TestStartHeartbeat_StopsOnContextCancel(t *testing.T) {
	srv := orchestratorOK(t)
	defer srv.Close()

	iso := vmISO()
	c := &config.Config{
		Registry: config.RegistryConfig{
			Endpoint:          srv.URL,
			HeartbeatInterval: 10 * time.Millisecond,
		},
		Auth: config.AuthConfig{APIKey: "token"},
	}
	reg := registry.New(c, iso, sysMod(iso), nopLogger())

	ctx, cancel := context.WithCancel(context.Background())
	if err := reg.Register(ctx); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	reg.StartHeartbeat(ctx)
	cancel() // immediate cancellation — goroutine should exit cleanly

	time.Sleep(50 * time.Millisecond)
}

// Package registry handles mandatory agent registration and periodic heartbeats
// with the PlusClouds orchestration layer.
//
// Startup flow:
//  1. Read AgentToken from ISO config drive (credentials.json).
//  2. POST to orchestrator /api/v1/agents/register with AgentToken in the
//     Authorization header.
//  3. Orchestrator validates the token, records the agent, and responds with a
//     SessionToken the agent uses for all subsequent communication.
//  4. Only after a successful registration response does the agent start its
//     HTTP and gRPC servers.
//
// If the control plane endpoint is not configured the agent runs in local-only
// mode (no registration, no heartbeat). This is intended for development and
// testing only — production VMs always have a ControlPlaneURL in the ISO.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
	"unsafe"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/config"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// ErrNoControlPlane is returned when no control plane endpoint is configured.
// The agent treats this as local-only mode.
var ErrNoControlPlane = errors.New("control plane endpoint is not configured")

// ErrNoAgentToken is returned when the ISO provides a control plane URL but
// no AgentToken to authenticate with. This is always a hard failure.
var ErrNoAgentToken = errors.New("control plane endpoint is configured but no agent_token found in ISO credentials")

// ErrRegistrationRejected is returned when the orchestrator explicitly rejects
// the registration (4xx response). Retrying will not help.
var ErrRegistrationRejected = errors.New("orchestrator rejected agent registration")

const (
	maxRetries    = 5
	baseBackoff   = 5 * time.Second
	maxBackoff    = 60 * time.Second
	requestTimeout = 15 * time.Second
)

// registrationPayload is the body sent to the orchestrator on startup.
type registrationPayload struct {
	VMID         string            `json:"vm_id"`
	TenantID     string            `json:"tenant_id"`
	Hostname     string            `json:"hostname"`
	IPAddress    string            `json:"ip_address"`
	AgentVersion string            `json:"agent_version"`
	Capabilities []string          `json:"capabilities"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// RegistrationResponse is what the orchestrator returns on a successful
// registration. The SessionToken must be stored and used for all subsequent
// orchestrator calls (heartbeats, config pushes, etc.).
type RegistrationResponse struct {
	// AgentID is the canonical identifier the orchestrator assigned (should
	// match the VMID from the ISO).
	AgentID string `json:"agent_id"`
	// SessionToken is a short-lived token the agent presents on heartbeats and
	// other outbound orchestrator calls. It is separate from the ISO AgentToken
	// (one-time provisioning credential) and from the HTTP API key (inbound
	// auth for callers hitting the agent).
	SessionToken string `json:"session_token"`
	// ExpiresAt is the Unix timestamp at which the SessionToken expires. The
	// agent re-registers automatically before this deadline.
	ExpiresAt int64 `json:"expires_at"`
	// Message is a human-readable confirmation string from the orchestrator.
	Message string `json:"message"`
}

// heartbeatPayload is sent on every heartbeat tick.
type heartbeatPayload struct {
	VMID          string  `json:"vm_id"`
	TenantID      string  `json:"tenant_id"`
	Timestamp     int64   `json:"timestamp"`
	UptimeSeconds int64   `json:"uptime_seconds"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemPercent    float64 `json:"memory_percent"`
	AgentVersion  string  `json:"agent_version"`
}

// Registry manages the agent's connection to the PlusClouds orchestration layer.
type Registry struct {
	cfg          *config.Config
	iso          *isoconfig.ISOMetadata
	sys          *system.Module
	logger       *zap.Logger
	client       *http.Client
	// sessionToken is set atomically after a successful registration.
	// Using unsafe.Pointer so we can atomically swap the string pointer.
	sessionToken unsafe.Pointer // *string
}

// New creates a Registry.
func New(
	cfg *config.Config,
	iso *isoconfig.ISOMetadata,
	sys *system.Module,
	logger *zap.Logger,
) *Registry {
	return &Registry{
		cfg:    cfg,
		iso:    iso,
		sys:    sys,
		logger: logger,
		client: &http.Client{Timeout: requestTimeout},
	}
}

// Register performs mandatory pre-flight registration with the PlusClouds
// orchestrator. It reads the AgentToken from the ISO, presents it to the
// orchestrator, and stores the returned SessionToken for use in heartbeats.
//
// Registration is retried up to maxRetries times with exponential backoff.
// A client-side rejection (4xx) is not retried.
//
// Returns ErrNoControlPlane when running in local-only mode (no endpoint
// configured). All other errors are hard failures — the caller must not
// start any servers until this returns nil.
func (r *Registry) Register(ctx context.Context) error {
	endpoint := r.endpoint()
	if endpoint == "" {
		r.logger.Warn("no control plane endpoint configured; running in local-only mode (no registration)")
		return ErrNoControlPlane
	}

	agentToken := r.agentToken()
	if agentToken == "" {
		return ErrNoAgentToken
	}

	sysInfo, err := r.sys.GetInfo(ctx)
	if err != nil {
		return fmt.Errorf("gathering system info for registration: %w", err)
	}

	ip := ""
	if r.iso != nil && r.iso.Network != nil {
		ip = r.iso.Network.IPAddress
	}

	payload := registrationPayload{
		VMID:         r.vmID(),
		TenantID:     r.tenantID(),
		Hostname:     sysInfo.Hostname,
		IPAddress:    ip,
		AgentVersion: config.AgentVersion,
		Capabilities: agentCapabilities(),
	}
	if r.iso != nil && r.iso.Instance != nil {
		payload.Labels = r.iso.Instance.Tags
	}

	url := endpoint + "/agents/register"

	var lastErr error
	backoff := baseBackoff

	for attempt := 1; attempt <= maxRetries; attempt++ {
		r.logger.Info("registering with orchestrator",
			zap.String("url", url),
			zap.String("vm_id", payload.VMID),
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", maxRetries),
		)

		regResp, err := r.postRegistration(ctx, url, agentToken, payload)
		if err != nil {
			// Client-side rejection: no point retrying.
			if errors.Is(err, ErrRegistrationRejected) {
				return fmt.Errorf("orchestrator rejected registration for vm_id=%s: %w", payload.VMID, err)
			}

			lastErr = err
			r.logger.Warn("registration attempt failed",
				zap.Int("attempt", attempt),
				zap.Duration("retry_in", backoff),
				zap.Error(err),
			)

			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return fmt.Errorf("context cancelled during registration: %w", ctx.Err())
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, maxBackoff)
			}
			continue
		}

		// Store session token atomically.
		r.storeSessionToken(regResp.SessionToken)

		r.logger.Info("agent registered and authorised by orchestrator",
			zap.String("agent_id", regResp.AgentID),
			zap.String("vm_id", payload.VMID),
			zap.Time("session_expires_at", time.Unix(regResp.ExpiresAt, 0)),
			zap.String("message", regResp.Message),
		)
		return nil
	}

	return fmt.Errorf("registration failed after %d attempts: %w", maxRetries, lastErr)
}

// StartHeartbeat starts a background goroutine that sends a heartbeat to the
// orchestrator every cfg.Registry.HeartbeatInterval. It uses the SessionToken
// obtained during registration. The goroutine exits when ctx is cancelled.
//
// Must only be called after a successful Register().
func (r *Registry) StartHeartbeat(ctx context.Context) {
	interval := r.cfg.Registry.HeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		r.logger.Info("heartbeat loop started", zap.Duration("interval", interval))

		for {
			select {
			case <-ctx.Done():
				r.logger.Info("heartbeat loop stopped")
				return
			case <-ticker.C:
				if err := r.sendHeartbeat(ctx); err != nil {
					r.logger.Warn("heartbeat failed", zap.Error(err))
				}
			}
		}
	}()
}

// SessionToken returns the session token issued by the orchestrator after
// a successful registration. Returns "" before registration completes.
func (r *Registry) SessionToken() string {
	p := atomic.LoadPointer(&r.sessionToken)
	if p == nil {
		return ""
	}
	return *(*string)(p)
}

// --- internal helpers -------------------------------------------------------

// postRegistration POSTs the registration payload and parses the orchestrator
// response into a RegistrationResponse.
func (r *Registry) postRegistration(
	ctx context.Context,
	url string,
	agentToken string,
	payload registrationPayload,
) (*RegistrationResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshalling registration payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentToken)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading registration response body: %w", err)
	}

	// 4xx = orchestrator explicitly rejected us (bad token, unknown VM, etc.)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		r.logger.Error("orchestrator rejected registration",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(respBody)),
		)
		return nil, ErrRegistrationRejected
	}

	// 5xx or other unexpected status = transient, worth retrying.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("orchestrator returned unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var regResp RegistrationResponse
	if err := json.Unmarshal(respBody, &regResp); err != nil {
		return nil, fmt.Errorf("parsing registration response: %w", err)
	}
	if regResp.SessionToken == "" {
		return nil, fmt.Errorf("orchestrator response missing session_token")
	}

	return &regResp, nil
}

// sendHeartbeat collects current metrics and POSTs them to the orchestrator
// using the SessionToken obtained at registration.
func (r *Registry) sendHeartbeat(ctx context.Context) error {
	endpoint := r.endpoint()
	if endpoint == "" {
		return nil // local-only mode
	}

	payload, err := r.buildHeartbeatPayload(ctx)
	if err != nil {
		return fmt.Errorf("building heartbeat payload: %w", err)
	}

	url := endpoint + "/agents/heartbeat"
	if err := r.postJSON(ctx, url, r.SessionToken(), payload); err != nil {
		return fmt.Errorf("posting heartbeat: %w", err)
	}

	r.logger.Debug("heartbeat sent", zap.String("vm_id", payload.VMID))
	return nil
}

// buildHeartbeatPayload constructs the heartbeat body from current metrics.
func (r *Registry) buildHeartbeatPayload(ctx context.Context) (*heartbeatPayload, error) {
	info, err := r.sys.GetInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("system info: %w", err)
	}
	cpuStats, err := r.sys.GetCPU(ctx)
	if err != nil {
		return nil, fmt.Errorf("cpu stats: %w", err)
	}
	memStats, err := r.sys.GetMemory(ctx)
	if err != nil {
		return nil, fmt.Errorf("memory stats: %w", err)
	}

	return &heartbeatPayload{
		VMID:          r.vmID(),
		TenantID:      r.tenantID(),
		Timestamp:     time.Now().Unix(),
		UptimeSeconds: info.Uptime,
		CPUPercent:    cpuStats.UsagePercent,
		MemPercent:    memStats.UsagePercent,
		AgentVersion:  config.AgentVersion,
	}, nil
}

// postJSON marshals v to JSON and POSTs it to url, setting the given token as
// the Bearer authorization header.
func (r *Registry) postJSON(ctx context.Context, url string, token string, v interface{}) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshalling payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("orchestrator returned %d for %s", resp.StatusCode, url)
	}
	return nil
}

// storeSessionToken atomically stores the session token.
func (r *Registry) storeSessionToken(token string) {
	p := unsafe.Pointer(&token)
	atomic.StorePointer(&r.sessionToken, p)
}

// agentCapabilities returns the list of capabilities this agent supports.
func agentCapabilities() []string {
	return []string{
		"system.info",
		"system.metrics",
		"services.manage",
		"metadata.read",
		"grpc.v1",
	}
}

func (r *Registry) vmID() string {
	if r.iso != nil {
		return r.iso.VMID()
	}
	return ""
}

func (r *Registry) tenantID() string {
	if r.iso != nil {
		return r.iso.TenantID()
	}
	return ""
}

func (r *Registry) agentToken() string {
	if r.iso != nil {
		return r.iso.AgentToken()
	}
	return ""
}

func (r *Registry) endpoint() string {
	if r.iso != nil && r.iso.ControlPlaneURL() != "" {
		return r.iso.ControlPlaneURL()
	}
	return r.cfg.Registry.Endpoint
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

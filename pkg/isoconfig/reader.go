// Package isoconfig reads VM metadata from the PlusClouds config-drive ISO.
// The ISO is a vFAT/ISO9660 volume that contains several JSON files and a
// cloud-init compatible user-data file. It is typically mounted at
// /media/plusclouds-config by a udev rule or cloud-init datasource.
package isoconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// InstanceMetadata describes the virtual machine identity on the PlusClouds platform.
type InstanceMetadata struct {
	// VMID is the unique identifier for this virtual machine.
	VMID string `json:"vm_id"`
	// TenantID is the PlusClouds tenant/customer identifier.
	TenantID string `json:"tenant_id"`
	// TenantName is the human-readable tenant name.
	TenantName string `json:"tenant_name"`
	// Datacenter is the datacenter slug (e.g. "ist-1").
	Datacenter string `json:"datacenter"`
	// Region is the geographic region slug (e.g. "eu-west").
	Region string `json:"region"`
	// PlanTier is the compute plan identifier (e.g. "standard-2vcpu-4gb").
	PlanTier string `json:"plan_tier"`
	// Tags holds arbitrary key/value labels applied to the VM.
	Tags map[string]string `json:"tags,omitempty"`
}

// NetworkMetadata describes the primary network configuration for the VM.
type NetworkMetadata struct {
	// IPAddress is the primary IPv4 address assigned to the VM.
	IPAddress string `json:"ip_address"`
	// Gateway is the default gateway IPv4 address.
	Gateway string `json:"gateway"`
	// DNS holds the list of nameserver IP addresses.
	DNS []string `json:"dns,omitempty"`
	// Hostname is the short hostname for the VM.
	Hostname string `json:"hostname"`
	// Domain is the DNS search domain (e.g. "tenant.plusclouds.net").
	Domain string `json:"domain,omitempty"`
}

// ServiceEntry describes a single service that the orchestrator wants managed.
type ServiceEntry struct {
	// Name is the systemd unit name (e.g. "nginx.service").
	Name string `json:"name"`
	// Enabled indicates whether this service should be enabled on boot.
	Enabled bool `json:"enabled"`
	// Config holds optional service-specific configuration key/value pairs.
	Config map[string]string `json:"config,omitempty"`
}

// ServicesManifest lists the services that the control plane wants the agent
// to manage on this VM.
type ServicesManifest struct {
	Services []ServiceEntry `json:"services"`
}

// Credentials holds the secrets injected by the PlusClouds provisioner.
// These are kept in memory only and never persisted to disk by the agent.
type Credentials struct {
	// APIKey is the shared secret for authenticating agent API requests.
	APIKey string `json:"api_key"`
	// ControlPlaneURL is the base URL of the PlusClouds control plane.
	ControlPlaneURL string `json:"control_plane_url"`
	// AgentToken is a per-VM JWT used when calling the control plane.
	AgentToken string `json:"agent_token"`
}

// ISOMetadata is the aggregated metadata read from all ISO files.
type ISOMetadata struct {
	Instance    *InstanceMetadata
	Network     *NetworkMetadata
	Services    *ServicesManifest
	Credentials *Credentials
	// UserData contains the raw cloud-init user-data string (may be empty).
	UserData string
}

// Reader reads metadata from a mounted config-drive ISO.
type Reader struct {
	mountPath string
}

// NewReader creates a Reader that reads from the given mount path.
func NewReader(mountPath string) *Reader {
	return &Reader{mountPath: mountPath}
}

// Read attempts to read all metadata files from the ISO mount point.
// Missing files are silently skipped; only JSON parse errors are returned.
// Returns an empty but non-nil ISOMetadata if no files are present.
func (r *Reader) Read() (*ISOMetadata, error) {
	meta := &ISOMetadata{}

	instance, err := readJSON[InstanceMetadata](filepath.Join(r.mountPath, "instance.json"))
	if err != nil {
		return nil, fmt.Errorf("reading instance.json: %w", err)
	}
	meta.Instance = instance

	network, err := readJSON[NetworkMetadata](filepath.Join(r.mountPath, "network.json"))
	if err != nil {
		return nil, fmt.Errorf("reading network.json: %w", err)
	}
	meta.Network = network

	services, err := readJSON[ServicesManifest](filepath.Join(r.mountPath, "services.json"))
	if err != nil {
		return nil, fmt.Errorf("reading services.json: %w", err)
	}
	meta.Services = services

	creds, err := readJSON[Credentials](filepath.Join(r.mountPath, "credentials.json"))
	if err != nil {
		return nil, fmt.Errorf("reading credentials.json: %w", err)
	}
	meta.Credentials = creds

	userData, err := readRawFile(filepath.Join(r.mountPath, "user-data"))
	if err != nil {
		return nil, fmt.Errorf("reading user-data: %w", err)
	}
	meta.UserData = userData

	return meta, nil
}

// readJSON reads a JSON file at the given path and decodes it into T.
// If the file does not exist, readJSON returns (nil, nil) — callers should
// check for a nil pointer before using the result.
func readJSON[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &out, nil
}

// readRawFile reads a plain text file at the given path.
// Returns an empty string without error if the file does not exist.
func readRawFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), nil
}

// VMID returns the VM identifier from instance metadata, or "" if unavailable.
func (m *ISOMetadata) VMID() string {
	if m.Instance == nil {
		return ""
	}
	return m.Instance.VMID
}

// TenantID returns the tenant identifier from instance metadata, or "" if unavailable.
func (m *ISOMetadata) TenantID() string {
	if m.Instance == nil {
		return ""
	}
	return m.Instance.TenantID
}

// APIKey returns the API key from credentials, or "" if unavailable.
func (m *ISOMetadata) APIKey() string {
	if m.Credentials == nil {
		return ""
	}
	return m.Credentials.APIKey
}

// ControlPlaneURL returns the control plane URL from credentials, or "" if unavailable.
func (m *ISOMetadata) ControlPlaneURL() string {
	if m.Credentials == nil {
		return ""
	}
	return m.Credentials.ControlPlaneURL
}

// AgentToken returns the per-VM provisioning token from credentials, or "" if unavailable.
// This token is sent to the orchestrator during registration and is distinct from the
// SessionToken the orchestrator issues in response.
func (m *ISOMetadata) AgentToken() string {
	if m.Credentials == nil {
		return ""
	}
	return m.Credentials.AgentToken
}

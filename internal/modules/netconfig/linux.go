//go:build linux

package netconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

const netplanDir = "/etc/netplan"

// linuxConfigurer generates a per-NIC netplan YAML file (matched by MAC
// address, since the metadata carries no interface-name field) and applies
// it via "netplan apply".
type linuxConfigurer struct {
	runner CommandRunner
	logger *zap.Logger
	dir    string // overridable in tests
}

// New returns a Configurer backed by netplan.
func New(runner CommandRunner, logger *zap.Logger) Configurer {
	return &linuxConfigurer{runner: runner, logger: logger, dir: netplanDir}
}

// netplan YAML structure (v2). Fields use slices (never maps with >1 entry)
// so rendering is deterministic across calls — required for the
// diff-before-write idempotency check below.
type netplanRoot struct {
	Network netplanNetwork `yaml:"network"`
}

type netplanNetwork struct {
	Version   int                        `yaml:"version"`
	Ethernets map[string]netplanEthernet `yaml:"ethernets"`
}

type netplanEthernet struct {
	Match       netplanMatch        `yaml:"match"`
	Addresses   []string            `yaml:"addresses,omitempty"`
	Routes      []netplanRoute      `yaml:"routes,omitempty"`
	Nameservers *netplanNameservers `yaml:"nameservers,omitempty"`
	MTU         int                 `yaml:"mtu,omitempty"`
}

type netplanMatch struct {
	MACAddress string `yaml:"macaddress"`
}

type netplanRoute struct {
	To  string `yaml:"to"`
	Via string `yaml:"via"`
}

type netplanNameservers struct {
	Addresses []string `yaml:"addresses,omitempty"`
}

// renderNetplanYAML builds the netplan YAML document for one NIC. Only one
// ethernet key is ever present, so the underlying map's iteration order
// (Go maps are unordered) has no effect on output determinism.
func renderNetplanYAML(nic isoconfig.VirtualNetworkCard) ([]byte, error) {
	name := fmt.Sprintf("plusclouds-nic%d", nic.DeviceNumber)

	eth := netplanEthernet{
		Match:     netplanMatch{MACAddress: nic.MACAddr},
		Addresses: buildAddresses(nic),
		MTU:       nic.Network.MTU,
	}

	if nic.Network.Gateway != nil && *nic.Network.Gateway != "" {
		eth.Routes = []netplanRoute{{To: "default", Via: *nic.Network.Gateway}}
	}

	if dns := stripDNSCIDR(nic.Network.DNSNameservers); len(dns) > 0 {
		eth.Nameservers = &netplanNameservers{Addresses: dns}
	}

	root := netplanRoot{
		Network: netplanNetwork{
			Version:   2,
			Ethernets: map[string]netplanEthernet{name: eth},
		},
	}

	return yaml.Marshal(root)
}

// netplanFilePath returns the per-MAC config path for nic. Using one file
// per MAC means reconfiguring one NIC never touches another NIC's file, and
// reapplying the same NIC's config just overwrites its own file. The "90-"
// prefix runs after distro-shipped files like 50-cloud-init.yaml.
func netplanFilePath(dir, mac string) string {
	safe := strings.ReplaceAll(mac, ":", "-")
	return filepath.Join(dir, fmt.Sprintf("90-plusclouds-%s.yaml", safe))
}

func (c *linuxConfigurer) Configure(ctx context.Context, nic isoconfig.VirtualNetworkCard) (*Result, error) {
	if nic.MACAddr == "" {
		return nil, fmt.Errorf("nic.mac_addr is required")
	}

	rendered, err := renderNetplanYAML(nic)
	if err != nil {
		return nil, fmt.Errorf("rendering netplan config: %w", err)
	}

	path := netplanFilePath(c.dir, nic.MACAddr)
	addrs := buildAddresses(nic)

	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == string(rendered) {
		return &Result{
			MACAddr:   nic.MACAddr,
			Addresses: addrs,
			Message:   "no change — configuration already applied",
		}, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading existing %q: %w", path, err)
	}

	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return nil, fmt.Errorf("creating %q: %w", c.dir, err)
	}
	if err := os.WriteFile(path, rendered, 0600); err != nil {
		return nil, fmt.Errorf("writing %q: %w", path, err)
	}

	if _, stderr, err := c.runner.Execute(ctx, "netplan", "apply"); err != nil {
		return nil, fmt.Errorf("netplan apply failed: %w (stderr: %s)", err, stderr)
	}

	ifaceName, _ := findInterfaceByMAC(nic.MACAddr)

	return &Result{
		MACAddr:   nic.MACAddr,
		Interface: ifaceName,
		Addresses: addrs,
		Message:   fmt.Sprintf("applied static configuration to %s", path),
	}, nil
}

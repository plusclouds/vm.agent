// Package netconfig statically applies IP/netmask/gateway/DNS/MTU
// configuration to a network interface identified by MAC address. Platform-
// specific implementations are selected at compile time via build tags,
// mirroring internal/modules/diskresize's Resizer pattern.
package netconfig

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// Configurer statically applies IP/netmask/gateway/DNS/MTU configuration to
// a network interface identified by MAC address.
type Configurer interface {
	Configure(ctx context.Context, nic isoconfig.VirtualNetworkCard) (*Result, error)
}

// CommandRunner matches *executor.Executor.Execute's signature, letting
// tests inject a fake instead of shelling out for real.
type CommandRunner interface {
	Execute(ctx context.Context, command string, args ...string) (stdout, stderr string, err error)
}

// Result reports the outcome of a Configure call.
type Result struct {
	MACAddr   string   `json:"mac_addr"`
	Interface string   `json:"interface,omitempty"`
	Addresses []string `json:"addresses"`
	Message   string   `json:"message"`
}

// netInterfaces is overridable in tests (net.Interfaces can't be faked
// directly).
var netInterfaces = net.Interfaces

// findInterfaceByMAC returns the OS interface name whose hardware address
// matches mac (case-insensitive), or an error if none is found.
func findInterfaceByMAC(mac string) (string, error) {
	ifaces, err := netInterfaces()
	if err != nil {
		return "", fmt.Errorf("listing network interfaces: %w", err)
	}
	for _, i := range ifaces {
		if strings.EqualFold(i.HardwareAddr.String(), mac) {
			return i.Name, nil
		}
	}
	return "", fmt.Errorf("no interface with MAC %q found", mac)
}

// buildAddresses converts a NIC's ip_list into CIDR address strings. Every
// entry is included, including ones marked is_reserved — the metadata's
// ip_list is treated as the full set of addresses to statically assign to
// this interface.
func buildAddresses(nic isoconfig.VirtualNetworkCard) []string {
	addrs := make([]string, 0, len(nic.IPList.Data))
	for _, ip := range nic.IPList.Data {
		if ip.IPAddr == "" {
			continue
		}
		addrs = append(addrs, ip.IPAddr)
	}
	return addrs
}

// stripDNSCIDR removes a CIDR suffix (e.g. "/32") from each DNS nameserver
// entry — dns_nameservers entries in the metadata are CIDR-suffixed, but
// nameserver configuration (netplan, Windows DNS client) wants bare
// addresses.
func stripDNSCIDR(nameservers []string) []string {
	out := make([]string, 0, len(nameservers))
	for _, ns := range nameservers {
		if idx := strings.IndexByte(ns, '/'); idx >= 0 {
			ns = ns[:idx]
		}
		if ns != "" {
			out = append(out, ns)
		}
	}
	return out
}

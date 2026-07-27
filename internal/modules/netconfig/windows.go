//go:build windows

package netconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// windowsConfigurer applies static IP/gateway/DNS/MTU configuration via
// PowerShell's NetTCPIP/DnsClient cmdlets, matched to an adapter by MAC
// address. It queries current state first and only runs the mutating
// cmdlets if the desired state differs, to avoid an unnecessary link
// reconfiguration on every call.
type windowsConfigurer struct {
	runner CommandRunner
	logger *zap.Logger
}

// New returns a Configurer backed by PowerShell.
func New(runner CommandRunner, logger *zap.Logger) Configurer {
	return &windowsConfigurer{runner: runner, logger: logger}
}

type psCurrentState struct {
	AdapterName string   `json:"adapterName"`
	IfIndex     int      `json:"ifIndex"`
	Addresses   []string `json:"addresses"`
	DNS         []string `json:"dns"`
	MTU         int      `json:"mtu"`
}

func (c *windowsConfigurer) Configure(ctx context.Context, nic isoconfig.VirtualNetworkCard) (*Result, error) {
	if nic.MACAddr == "" {
		return nil, fmt.Errorf("nic.mac_addr is required")
	}

	queryScript := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$adapter = Get-NetAdapter | Where-Object { ($_.MacAddress -replace '-',':') -ieq "%s" }
if (-not $adapter) { throw "no adapter with MAC %s found" }
$addrs = @(Get-NetIPAddress -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
    Where-Object { $_.PrefixOrigin -eq 'Manual' } |
    ForEach-Object { "$($_.IPAddress)/$($_.PrefixLength)" })
$dns = @((Get-DnsClientServerAddress -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4).ServerAddresses)
$mtu = (Get-NetIPInterface -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4).NlMtuBytes
@{ adapterName = $adapter.Name; ifIndex = $adapter.ifIndex; addresses = $addrs; dns = $dns; mtu = $mtu } | ConvertTo-Json -Compress
`, nic.MACAddr, nic.MACAddr)

	stdout, stderr, err := c.runner.Execute(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", queryScript)
	if err != nil {
		return nil, fmt.Errorf("querying adapter state: %w (stderr: %s)", err, stderr)
	}

	var current psCurrentState
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &current); err != nil {
		return nil, fmt.Errorf("parsing adapter state: %w (stdout: %s)", err, stdout)
	}

	wantAddrs := buildAddresses(nic)
	wantDNS := stripDNSCIDR(nic.Network.DNSNameservers)
	wantMTU := nic.Network.MTU

	if sameStringSet(current.Addresses, wantAddrs) && sameStringSet(current.DNS, wantDNS) && current.MTU == wantMTU {
		return &Result{
			MACAddr:   nic.MACAddr,
			Interface: current.AdapterName,
			Addresses: wantAddrs,
			Message:   "no change — configuration already applied",
		}, nil
	}

	mutateScript := buildMutateScript(current.IfIndex, nic, wantAddrs, wantDNS, wantMTU)
	if _, stderr, err := c.runner.Execute(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", mutateScript); err != nil {
		return nil, fmt.Errorf("applying adapter configuration: %w (stderr: %s)", err, stderr)
	}

	return &Result{
		MACAddr:   nic.MACAddr,
		Interface: current.AdapterName,
		Addresses: wantAddrs,
		Message:   fmt.Sprintf("applied static configuration to interface index %d", current.IfIndex),
	}, nil
}

// buildMutateScript renders the PowerShell that removes any existing
// manually-assigned IPv4 addresses on ifIndex and reapplies the desired
// state. Only the first address gets -DefaultGateway, to avoid a "multiple
// default gateways" error.
func buildMutateScript(ifIndex int, nic isoconfig.VirtualNetworkCard, addrs, dns []string, mtu int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "$ErrorActionPreference = \"Stop\"\n")
	fmt.Fprintf(&b, "Get-NetIPAddress -InterfaceIndex %d -AddressFamily IPv4 -ErrorAction SilentlyContinue | "+
		"Where-Object { $_.PrefixOrigin -eq 'Manual' } | Remove-NetIPAddress -Confirm:$false\n", ifIndex)

	for i, addr := range addrs {
		ip, prefix := splitCIDR(addr)
		if ip == "" {
			continue
		}
		if i == 0 && nic.Network.Gateway != nil && *nic.Network.Gateway != "" {
			fmt.Fprintf(&b, "New-NetIPAddress -InterfaceIndex %d -IPAddress \"%s\" -PrefixLength %s -DefaultGateway \"%s\" | Out-Null\n",
				ifIndex, ip, prefix, *nic.Network.Gateway)
		} else {
			fmt.Fprintf(&b, "New-NetIPAddress -InterfaceIndex %d -IPAddress \"%s\" -PrefixLength %s | Out-Null\n",
				ifIndex, ip, prefix)
		}
	}

	if len(dns) > 0 {
		fmt.Fprintf(&b, "Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses (%s)\n", ifIndex, quotedList(dns))
	}
	if mtu > 0 {
		fmt.Fprintf(&b, "Set-NetIPInterface -InterfaceIndex %d -NlMtuBytes %d\n", ifIndex, mtu)
	}

	return b.String()
}

// splitCIDR splits "10.0.0.1/24" into ("10.0.0.1", "24"). Returns "" for ip
// if addr doesn't look like a CIDR address.
func splitCIDR(addr string) (ip, prefix string) {
	idx := strings.IndexByte(addr, '/')
	if idx < 0 {
		return "", ""
	}
	return addr[:idx], addr[idx+1:]
}

func quotedList(vals []string) string {
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = fmt.Sprintf("\"%s\"", v)
	}
	return strings.Join(quoted, ",")
}

// sameStringSet reports whether a and b contain the same elements,
// ignoring order and duplicates.
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if !set[v] {
			return false
		}
	}
	return true
}

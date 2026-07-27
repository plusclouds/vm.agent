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
	Gateway     string   `json:"gateway"`
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
$defaultRoute = Get-NetRoute -InterfaceIndex $adapter.ifIndex -DestinationPrefix "0.0.0.0/0" -AddressFamily IPv4 -ErrorAction SilentlyContinue | Select-Object -First 1
$gateway = if ($defaultRoute) { $defaultRoute.NextHop } else { "" }
@{ adapterName = $adapter.Name; ifIndex = $adapter.ifIndex; addresses = $addrs; dns = $dns; mtu = $mtu; gateway = $gateway } | ConvertTo-Json -Compress
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
	wantGateway := ""
	if nic.Network.Gateway != nil {
		wantGateway = *nic.Network.Gateway
	}

	if sameStringSet(current.Addresses, wantAddrs) && sameStringSet(current.DNS, wantDNS) &&
		current.MTU == wantMTU && current.Gateway == wantGateway {
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

// buildMutateScript renders the PowerShell that disables DHCP, clears
// whatever IPv4 addresses/routes ifIndex currently has, and reapplies the
// desired state from scratch.
//
// DHCP must be explicitly disabled first: cloud hypervisors commonly hand
// out the VM's fixed IP via DHCP too, purely for guest-OS compatibility
// (this metadata schema's dhcp_server field), so the interface can already
// hold that exact address with PrefixOrigin "Dhcp" before this ever runs.
// Only clearing "Manual"-origin addresses left that DHCP-leased address in
// place, and New-NetIPAddress then failed to (re-)assign the same address
// with "Instance MSFT_NetIPAddress already exists". Disabling DHCP and then
// removing every IPv4 address on the interface — regardless of origin —
// guarantees a clean slate before assigning exactly the desired addresses.
//
// The gateway is deliberately never passed via New-NetIPAddress's
// -DefaultGateway parameter: that parameter requires the gateway to be
// within the subnet implied by the address's own prefix length, which a
// /32 host address (the norm for this metadata schema — see
// pkg/isoconfig's ip_list entries) never satisfies, since a /32 has no
// "local subnet" other than itself. Setting a gateway outside the
// interface's implied subnet fails the entire New-NetIPAddress call with
// "DefaultGateway ... is not on the same network segment", which also
// aborts the address assignment itself under $ErrorActionPreference =
// "Stop". Instead, this mirrors the technique cloud-init-style Windows
// provisioning tools (e.g. cloudbase-init) use for exactly this case: add
// an explicit on-link host route to the gateway address (NextHop
// "0.0.0.0" marks it directly reachable regardless of subnet membership),
// then the default route via that gateway, as separate New-NetRoute calls.
func buildMutateScript(ifIndex int, nic isoconfig.VirtualNetworkCard, addrs, dns []string, mtu int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "$ErrorActionPreference = \"Stop\"\n")
	fmt.Fprintf(&b, "Set-NetIPInterface -InterfaceIndex %d -AddressFamily IPv4 -Dhcp Disabled -ErrorAction SilentlyContinue\n", ifIndex)
	fmt.Fprintf(&b, "Get-NetIPAddress -InterfaceIndex %d -AddressFamily IPv4 -ErrorAction SilentlyContinue | "+
		"Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue\n", ifIndex)
	fmt.Fprintf(&b, "Get-NetRoute -InterfaceIndex %d -AddressFamily IPv4 -DestinationPrefix \"0.0.0.0/0\" -ErrorAction SilentlyContinue | "+
		"Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue\n", ifIndex)

	for _, addr := range addrs {
		ip, prefix := splitCIDR(addr)
		if ip == "" {
			continue
		}
		fmt.Fprintf(&b, "New-NetIPAddress -InterfaceIndex %d -IPAddress \"%s\" -PrefixLength %s | Out-Null\n",
			ifIndex, ip, prefix)
	}

	if nic.Network.Gateway != nil && *nic.Network.Gateway != "" {
		gw := *nic.Network.Gateway
		fmt.Fprintf(&b, "New-NetRoute -DestinationPrefix \"%s/32\" -InterfaceIndex %d -NextHop \"0.0.0.0\" -ErrorAction SilentlyContinue | Out-Null\n", gw, ifIndex)
		fmt.Fprintf(&b, "New-NetRoute -DestinationPrefix \"0.0.0.0/0\" -InterfaceIndex %d -NextHop \"%s\" | Out-Null\n", ifIndex, gw)
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

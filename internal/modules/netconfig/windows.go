//go:build windows

package netconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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

// buildMutateScript renders a script that converts the interface from
// DHCP (or whatever it currently has) to the desired static configuration
// using netsh, not the newer NetTCPIP PowerShell cmdlets (New-NetIPAddress/
// New-NetRoute).
//
// Two independent problems with the NetTCPIP cmdlets made them unusable
// here:
//
//  1. New-NetIPAddress -DefaultGateway requires the gateway to be within
//     the subnet implied by the address's own prefix length, which a /32
//     host address (the norm for this metadata schema — see
//     pkg/isoconfig's ip_list entries) never satisfies, since a /32 has no
//     "local subnet" other than itself. This failed the entire
//     New-NetIPAddress call (including the address assignment) with
//     "DefaultGateway ... is not on the same network segment".
//
//  2. Cloud hypervisors commonly hand the VM's fixed IP out via DHCP too,
//     purely for guest-OS compatibility (this schema's dhcp_server
//     field), so the interface can already hold the exact address we want
//     to assign, with PrefixOrigin "Dhcp". Explicitly disabling DHCP and
//     removing every address before re-adding it (tried first) still hit
//     "Instance MSFT_NetIPAddress already exists" on this NIC (a XenServer
//     PV virtual adapter) — the disable/remove/re-add sequence across
//     three separate CIM-backed cmdlet calls isn't atomic and isn't
//     reliable on every driver.
//
// `netsh interface ipv4 set address ... source=static` sidesteps both:
// it performs the DHCP-to-static conversion as a single, decades-old,
// broadly-compatible operation, and it does not enforce the same
// gateway/subnet-membership validation as New-NetIPAddress — this is the
// same reason cloud-init-style Windows provisioning tools (e.g.
// cloudbase-init) use netsh for exactly this case.
func buildMutateScript(ifIndex int, nic isoconfig.VirtualNetworkCard, addrs, dns []string, mtu int) string {
	gateway := ""
	if nic.Network.Gateway != nil {
		gateway = *nic.Network.Gateway
	}

	var b strings.Builder
	fmt.Fprintf(&b, "$ErrorActionPreference = \"Stop\"\n")
	fmt.Fprintf(&b, "$ifAlias = (Get-NetAdapter -InterfaceIndex %d).Name\n", ifIndex)

	for i, addr := range addrs {
		ip, prefix := splitCIDR(addr)
		if ip == "" {
			continue
		}
		mask := prefixToMask(prefix)
		if i == 0 {
			if gateway != "" {
				fmt.Fprintf(&b, "netsh interface ipv4 set address name=\"$ifAlias\" source=static addr=%s mask=%s gateway=%s gwmetric=1\n",
					ip, mask, gateway)
			} else {
				fmt.Fprintf(&b, "netsh interface ipv4 set address name=\"$ifAlias\" source=static addr=%s mask=%s\n", ip, mask)
			}
			fmt.Fprintf(&b, "if ($LASTEXITCODE -ne 0) { throw \"netsh set address failed with exit code $LASTEXITCODE\" }\n")
		} else {
			// Additional addresses beyond the first (e.g. reserved/floating
			// IPs from ip_list) are added on top of the primary static
			// address — netsh's "set address" only replaces the primary.
			// Note: unlike "set address" (addr=), "add address" takes
			// address= — a real inconsistency between the two netsh
			// subcommands, not a typo.
			fmt.Fprintf(&b, "netsh interface ipv4 add address name=\"$ifAlias\" address=%s mask=%s\n", ip, mask)
		}
	}
	// Drop any stale secondary manual addresses from a previous run that
	// assigned more addresses than the current desired state has (the
	// primary swap above already handles addrs[0]).
	fmt.Fprintf(&b, "Get-NetIPAddress -InterfaceIndex %d -AddressFamily IPv4 -ErrorAction SilentlyContinue | "+
		"Where-Object { $_.PrefixOrigin -eq 'Manual' } | Select-Object -Skip %d | Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue\n",
		ifIndex, len(addrs))

	for i, ns := range dns {
		if i == 0 {
			// Unlike "set/add address" (parameter is addr=), "set/add
			// dnsservers" takes address=.
			fmt.Fprintf(&b, "netsh interface ipv4 set dnsservers name=\"$ifAlias\" source=static address=%s register=none\n", ns)
		} else {
			fmt.Fprintf(&b, "netsh interface ipv4 add dnsservers name=\"$ifAlias\" address=%s index=%d\n", ns, i+1)
		}
	}

	if mtu > 0 {
		fmt.Fprintf(&b, "netsh interface ipv4 set subinterface \"$ifAlias\" mtu=%d store=persistent\n", mtu)
	}

	return b.String()
}

// prefixToMask converts a CIDR prefix length (e.g. "32") to a dotted-decimal
// subnet mask (e.g. "255.255.255.255"), which is what netsh expects rather
// than a bare prefix length. Falls back to /32 for anything unparseable.
func prefixToMask(prefixLen string) string {
	n, err := strconv.Atoi(prefixLen)
	if err != nil || n < 0 || n > 32 {
		n = 32
	}
	mask := uint32(0xffffffff) << uint32(32-n)
	return fmt.Sprintf("%d.%d.%d.%d", byte(mask>>24), byte(mask>>16), byte(mask>>8), byte(mask))
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

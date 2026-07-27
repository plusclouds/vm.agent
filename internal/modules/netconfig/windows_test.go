//go:build windows

package netconfig

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

type call struct {
	command string
	args    []string
}

type fakeRunner struct {
	calls     []call
	responses []response
}

type response struct {
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) Execute(_ context.Context, command string, args ...string) (string, string, error) {
	f.calls = append(f.calls, call{command: command, args: args})
	idx := len(f.calls) - 1
	if idx < len(f.responses) {
		r := f.responses[idx]
		return r.stdout, r.stderr, r.err
	}
	return "", "", nil
}

func TestConfigure_NoChange_SkipsMutateScript(t *testing.T) {
	nic := privateNIC()
	queryOut := `{"adapterName":"Ethernet","ifIndex":5,"addresses":["10.128.0.1/32"],"dns":["198.51.100.53"],"mtu":1500}`

	fr := &fakeRunner{responses: []response{{stdout: queryOut}}}
	c := New(fr, zap.NewNop())

	result, err := c.Configure(context.Background(), nic)
	if err != nil {
		t.Fatalf("Configure() error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Errorf("expected only the query call, got %d calls", len(fr.calls))
	}
	if result.Interface != "Ethernet" {
		t.Errorf("Interface = %q, want Ethernet", result.Interface)
	}
}

func TestConfigure_ChangeDetected_RunsMutateScript(t *testing.T) {
	nic := privateNIC()
	queryOut := `{"adapterName":"Ethernet","ifIndex":5,"addresses":[],"dns":[],"mtu":1500}`

	fr := &fakeRunner{responses: []response{{stdout: queryOut}, {stdout: ""}}}
	c := New(fr, zap.NewNop())

	if _, err := c.Configure(context.Background(), nic); err != nil {
		t.Fatalf("Configure() error: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("expected query + mutate calls, got %d", len(fr.calls))
	}
	if fr.calls[1].command != "powershell" {
		t.Errorf("mutate call command = %q, want powershell", fr.calls[1].command)
	}
}

func TestConfigure_GatewayChange_RunsMutateScript(t *testing.T) {
	nic := publicNIC()
	gw := "203.0.113.1"
	nic.Network.Gateway = &gw
	// Current state matches everything except the gateway.
	queryOut := `{"adapterName":"Ethernet","ifIndex":5,"addresses":["203.0.113.16/32"],"dns":["198.51.100.53","198.51.100.54"],"mtu":1500,"gateway":""}`

	fr := &fakeRunner{responses: []response{{stdout: queryOut}, {stdout: ""}}}
	c := New(fr, zap.NewNop())

	if _, err := c.Configure(context.Background(), nic); err != nil {
		t.Fatalf("Configure() error: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("expected query + mutate calls when only the gateway differs, got %d", len(fr.calls))
	}
}

func TestConfigure_GatewayUnchanged_NoChange(t *testing.T) {
	nic := publicNIC()
	gw := "203.0.113.1"
	nic.Network.Gateway = &gw
	queryOut := `{"adapterName":"Ethernet","ifIndex":5,"addresses":["203.0.113.16/32"],"dns":["198.51.100.53","198.51.100.54"],"mtu":1500,"gateway":"203.0.113.1"}`

	fr := &fakeRunner{responses: []response{{stdout: queryOut}}}
	c := New(fr, zap.NewNop())

	if _, err := c.Configure(context.Background(), nic); err != nil {
		t.Fatalf("Configure() error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Errorf("expected only the query call when gateway already matches, got %d calls", len(fr.calls))
	}
}

// TestBuildMutateScript_UsesNetshNotNewNetIPAddress guards against
// regressing back to the NetTCPIP cmdlets (New-NetIPAddress/New-NetRoute),
// which hit two separate bugs on real hardware: -DefaultGateway rejects any
// gateway outside a /32 address's own (nonexistent) subnet, and the
// disable/remove/re-add cmdlet sequence raced with a DHCP-leased address
// on at least one XenServer PV NIC ("Instance MSFT_NetIPAddress already
// exists"). netsh performs the DHCP-to-static conversion as a single
// operation and doesn't enforce the same gateway/subnet validation.
func TestBuildMutateScript_UsesNetshNotNewNetIPAddress(t *testing.T) {
	nic := publicNIC()
	gw := "203.0.113.1"
	nic.Network.Gateway = &gw

	script := buildMutateScript(5, nic, buildAddresses(nic), stripDNSCIDR(nic.Network.DNSNameservers), nic.Network.MTU)

	if strings.Contains(script, "New-NetIPAddress") || strings.Contains(script, "New-NetRoute") {
		t.Fatalf("script must not use the NetTCPIP cmdlets, got:\n%s", script)
	}
	want := `netsh interface ipv4 set address name="$ifAlias" source=static addr=203.0.113.16 mask=255.255.255.255 gateway=203.0.113.1 gwmetric=1`
	if !strings.Contains(script, want) {
		t.Errorf("expected netsh set address with gateway, got:\n%s", script)
	}
}

func TestBuildMutateScript_NoGateway_OmitsGatewayArg(t *testing.T) {
	nic := privateNIC() // Gateway is nil
	script := buildMutateScript(5, nic, buildAddresses(nic), stripDNSCIDR(nic.Network.DNSNameservers), nic.Network.MTU)

	if strings.Contains(script, "gateway=") {
		t.Errorf("expected no gateway argument when no gateway is set, got:\n%s", script)
	}
	if !strings.Contains(script, `netsh interface ipv4 set address name="$ifAlias" source=static addr=10.128.0.1 mask=255.255.255.255`) {
		t.Errorf("expected a static address set without a gateway, got:\n%s", script)
	}
}

func TestBuildMutateScript_SecondaryAddress_UsesAddNotSet(t *testing.T) {
	nic := publicNIC()
	nic.IPList.Data = append(nic.IPList.Data, isoconfig.IPEntry{ID: 1012, IPAddr: "203.0.113.17/32", IsReserved: true})

	script := buildMutateScript(5, nic, buildAddresses(nic), stripDNSCIDR(nic.Network.DNSNameservers), nic.Network.MTU)

	if !strings.Contains(script, `netsh interface ipv4 add address name="$ifAlias" address=203.0.113.17 mask=255.255.255.255`) {
		t.Errorf("expected the second address to be added via 'add address', got:\n%s", script)
	}
}

func TestPrefixToMask(t *testing.T) {
	cases := map[string]string{
		"32": "255.255.255.255",
		"24": "255.255.255.0",
		"16": "255.255.0.0",
		"22": "255.255.252.0",
	}
	for prefix, want := range cases {
		if got := prefixToMask(prefix); got != want {
			t.Errorf("prefixToMask(%q) = %q, want %q", prefix, got, want)
		}
	}
}

func TestSameStringSet(t *testing.T) {
	if !sameStringSet([]string{"a", "b"}, []string{"b", "a"}) {
		t.Error("expected sets with same elements in different order to match")
	}
	if sameStringSet([]string{"a"}, []string{"a", "b"}) {
		t.Error("expected different-length sets to not match")
	}
}

func TestSplitCIDR(t *testing.T) {
	ip, prefix := splitCIDR("10.0.0.1/24")
	if ip != "10.0.0.1" || prefix != "24" {
		t.Errorf("splitCIDR() = (%q, %q)", ip, prefix)
	}
	if ip, _ := splitCIDR("not-cidr"); ip != "" {
		t.Errorf("splitCIDR(non-CIDR) should return empty ip, got %q", ip)
	}
}

//go:build windows

package netconfig

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
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

// TestBuildMutateScript_GatewayNeverPassedToNewNetIPAddress guards against
// regressing the bug where -DefaultGateway on New-NetIPAddress fails for
// /32 addresses whose gateway isn't in the implied subnet — the norm for
// this metadata schema. The gateway must only ever be applied via
// New-NetRoute.
func TestBuildMutateScript_GatewayNeverPassedToNewNetIPAddress(t *testing.T) {
	nic := publicNIC()
	gw := "203.0.113.1"
	nic.Network.Gateway = &gw

	script := buildMutateScript(5, nic, buildAddresses(nic), stripDNSCIDR(nic.Network.DNSNameservers), nic.Network.MTU)

	if strings.Contains(script, "-DefaultGateway") {
		t.Fatalf("script must never use -DefaultGateway on New-NetIPAddress, got:\n%s", script)
	}
	if !strings.Contains(script, `New-NetRoute -DestinationPrefix "203.0.113.1/32" -InterfaceIndex 5 -NextHop "0.0.0.0"`) {
		t.Errorf("expected an on-link host route to the gateway, got:\n%s", script)
	}
	if !strings.Contains(script, `New-NetRoute -DestinationPrefix "0.0.0.0/0" -InterfaceIndex 5 -NextHop "203.0.113.1"`) {
		t.Errorf("expected a default route via the gateway, got:\n%s", script)
	}
}

func TestBuildMutateScript_NoGateway_NoRoutesAdded(t *testing.T) {
	nic := privateNIC() // Gateway is nil
	script := buildMutateScript(5, nic, buildAddresses(nic), stripDNSCIDR(nic.Network.DNSNameservers), nic.Network.MTU)

	if strings.Contains(script, "New-NetRoute") {
		t.Errorf("expected no route manipulation when no gateway is set, got:\n%s", script)
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

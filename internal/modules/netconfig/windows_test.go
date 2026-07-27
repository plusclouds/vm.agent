//go:build windows

package netconfig

import (
	"context"
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

//go:build linux

package netconfig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

type call struct {
	command string
	args    []string
}

type fakeRunner struct {
	calls []call
	err   error
}

func (f *fakeRunner) Execute(_ context.Context, command string, args ...string) (string, string, error) {
	f.calls = append(f.calls, call{command: command, args: args})
	return "", "", f.err
}

func newTestConfigurer(dir string, runner CommandRunner) *linuxConfigurer {
	return &linuxConfigurer{runner: runner, logger: zap.NewNop(), dir: dir}
}

func TestRenderNetplanYAML_PrivateNIC_NoGatewayNoRoutes(t *testing.T) {
	yamlBytes, err := renderNetplanYAML(privateNIC())
	if err != nil {
		t.Fatalf("renderNetplanYAML() error: %v", err)
	}
	want := `network:
    version: 2
    ethernets:
        plusclouds-nic1:
            match:
                macaddress: "02:00:00:00:00:01"
            addresses:
                - 10.128.0.1/32
            nameservers:
                addresses:
                    - 198.51.100.53
            mtu: 1500
`
	if string(yamlBytes) != want {
		t.Errorf("rendered YAML =\n%s\nwant:\n%s", yamlBytes, want)
	}
}

func TestRenderNetplanYAML_Deterministic(t *testing.T) {
	nic := publicNIC()
	first, err := renderNetplanYAML(nic)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := renderNetplanYAML(nic)
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(first) {
			t.Fatalf("renderNetplanYAML() is not deterministic across calls")
		}
	}
}

func TestRenderNetplanYAML_WithGateway_EmitsDefaultRoute(t *testing.T) {
	nic := publicNIC()
	gw := "203.0.113.1"
	nic.Network.Gateway = &gw

	yamlBytes, err := renderNetplanYAML(nic)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(yamlBytes), "to: default") || !strings.Contains(string(yamlBytes), "via: 203.0.113.1") {
		t.Errorf("expected a default route via gateway, got:\n%s", yamlBytes)
	}
}

func TestConfigure_Idempotent_NoChangeSkipsApply(t *testing.T) {
	dir := t.TempDir()
	fr := &fakeRunner{}
	c := newTestConfigurer(dir, fr)

	if _, err := c.Configure(context.Background(), privateNIC()); err != nil {
		t.Fatalf("first Configure() error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected exactly 1 netplan apply call, got %d", len(fr.calls))
	}

	result, err := c.Configure(context.Background(), privateNIC())
	if err != nil {
		t.Fatalf("second Configure() error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Errorf("expected no additional netplan apply calls on identical re-apply, got %d total", len(fr.calls))
	}
	if result.Message == "" {
		t.Error("expected a message describing the no-op")
	}
}

func TestConfigure_ChangeDetected_CallsApplyAgain(t *testing.T) {
	dir := t.TempDir()
	fr := &fakeRunner{}
	c := newTestConfigurer(dir, fr)

	if _, err := c.Configure(context.Background(), privateNIC()); err != nil {
		t.Fatalf("first Configure() error: %v", err)
	}

	changed := privateNIC()
	changed.Network.MTU = 9000
	if _, err := c.Configure(context.Background(), changed); err != nil {
		t.Fatalf("second Configure() error: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Errorf("expected netplan apply to run again after a real change, got %d calls", len(fr.calls))
	}
}

func TestConfigure_WritesOneFilePerMAC(t *testing.T) {
	dir := t.TempDir()
	fr := &fakeRunner{}
	c := newTestConfigurer(dir, fr)

	if _, err := c.Configure(context.Background(), privateNIC()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Configure(context.Background(), publicNIC()); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 netplan files (one per MAC), got %d", len(entries))
	}

	privatePath := netplanFilePath(dir, privateNIC().MACAddr)
	if _, err := os.Stat(privatePath); err != nil {
		t.Errorf("expected %s to exist: %v", privatePath, err)
	}
}

func TestNetplanFilePath_SanitizesMAC(t *testing.T) {
	got := netplanFilePath("/etc/netplan", "02:00:00:00:00:01")
	want := filepath.Join("/etc/netplan", "90-plusclouds-02-00-00-00-00-01.yaml")
	if got != want {
		t.Errorf("netplanFilePath() = %q, want %q", got, want)
	}
}

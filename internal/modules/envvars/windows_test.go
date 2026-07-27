//go:build windows

package envvars

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"golang.org/x/sys/windows/registry"
)

// newTestManager targets a throwaway HKEY_CURRENT_USER test subkey — never
// the real machine Environment key — so tests don't mutate the host.
func newTestManager(t *testing.T) *windowsManager {
	t.Helper()
	const testKeyPath = `Software\PlusCloudsAgentTest\envvars`

	k, _, err := registry.CreateKey(registry.CURRENT_USER, testKeyPath, registry.ALL_ACCESS)
	if err != nil {
		t.Fatalf("creating test registry key: %v", err)
	}
	k.Close()

	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, testKeyPath)
	})

	return &windowsManager{keyPath: testKeyPath, root: registry.CURRENT_USER, logger: zap.NewNop()}
}

func TestSetUnsetList_RoundTrip(t *testing.T) {
	m := newTestManager(t)

	if _, err := m.Set(context.Background(), "FOO", "bar"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	got, err := m.List(context.Background())
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if got["FOO"] != "bar" {
		t.Errorf("List() = %v, want FOO=bar", got)
	}

	if _, err := m.Unset(context.Background(), "FOO"); err != nil {
		t.Fatalf("Unset() error: %v", err)
	}
	got, err = m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["FOO"]; ok {
		t.Error("FOO should have been removed")
	}
}

func TestUnset_Nonexistent_IsNoop(t *testing.T) {
	m := newTestManager(t)

	result, err := m.Unset(context.Background(), "NEVER_SET")
	if err != nil {
		t.Fatalf("Unset() error: %v", err)
	}
	if result.Message != "already unset" {
		t.Errorf("Message = %q, want %q", result.Message, "already unset")
	}
}

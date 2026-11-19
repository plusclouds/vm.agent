//go:build windows

package hostname

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
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
	if f.err != nil {
		return "", "access denied", f.err
	}
	return "", "", nil
}

func newTestSetter(t *testing.T, fr *fakeRunner, current, pending string) (*windowsSetter, string) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "hostname-applied")
	return &windowsSetter{
		runner:    fr,
		statePath: statePath,
		names:     func() (string, string, error) { return current, pending, nil },
	}, statePath
}

func TestApply_RenamesFreshMachine(t *testing.T) {
	fr := &fakeRunner{}
	s, statePath := newTestSetter(t, fr, "WIN-B38G6ED0CB8", "WIN-B38G6ED0CB8")

	result, err := s.Apply(context.Background(), "backup-test")
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if !result.Changed || !result.RebootRequired {
		t.Errorf("result = %+v, want Changed and RebootRequired", result)
	}
	if result.Previous != "WIN-B38G6ED0CB8" {
		t.Errorf("Previous = %q", result.Previous)
	}

	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 command, got %d", len(fr.calls))
	}
	c := fr.calls[0]
	if c.command != "powershell" {
		t.Errorf("command = %q, want powershell", c.command)
	}
	script := c.args[len(c.args)-1]
	if !strings.Contains(script, "Rename-Computer -NewName 'backup-test' -Force") {
		t.Errorf("script = %q", script)
	}
	if strings.Contains(script, "Restart") {
		t.Errorf("script must never restart the machine: %q", script)
	}
	if got := readRecorded(statePath); got != "backup-test" {
		t.Errorf("recorded = %q, want backup-test", got)
	}
}

func TestApply_KeepsMachineNamedByItsOwner(t *testing.T) {
	fr := &fakeRunner{}
	s, statePath := newTestSetter(t, fr, "SQLPROD", "SQLPROD")

	result, err := s.Apply(context.Background(), "backup-test")
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if result.Changed {
		t.Errorf("result = %+v, want no change", result)
	}
	if len(fr.calls) != 0 {
		t.Errorf("expected no command, got %d", len(fr.calls))
	}
	if got := readRecorded(statePath); got != "backup-test" {
		t.Errorf("recorded = %q, want backup-test", got)
	}

	// The panel hostname changes later: now it is the platform's to apply.
	result, err = s.Apply(context.Background(), "backup-new")
	if err != nil {
		t.Fatalf("second Apply() error: %v", err)
	}
	if !result.Changed || len(fr.calls) != 1 {
		t.Errorf("after a panel change: result = %+v, calls = %d", result, len(fr.calls))
	}
}

func TestApply_SecondBootDoesNothing(t *testing.T) {
	fr := &fakeRunner{}
	s, _ := newTestSetter(t, fr, "WIN-B38G6ED0CB8", "WIN-B38G6ED0CB8")

	if _, err := s.Apply(context.Background(), "backup-test"); err != nil {
		t.Fatalf("first Apply() error: %v", err)
	}
	// Agent restarts before the machine does: the rename is still waiting.
	s.names = func() (string, string, error) { return "WIN-B38G6ED0CB8", "BACKUP-TEST", nil }
	if _, err := s.Apply(context.Background(), "backup-test"); err != nil {
		t.Fatalf("second Apply() error: %v", err)
	}
	// After the restart.
	s.names = func() (string, string, error) { return "BACKUP-TEST", "BACKUP-TEST", nil }
	if _, err := s.Apply(context.Background(), "backup-test"); err != nil {
		t.Fatalf("third Apply() error: %v", err)
	}

	if len(fr.calls) != 1 {
		t.Errorf("expected exactly 1 rename across three starts, got %d", len(fr.calls))
	}
}

func TestApply_FailedRenameIsNotRecorded(t *testing.T) {
	fr := &fakeRunner{err: errors.New("exit status 1")}
	s, statePath := newTestSetter(t, fr, "WIN-B38G6ED0CB8", "WIN-B38G6ED0CB8")

	if _, err := s.Apply(context.Background(), "backup-test"); err == nil {
		t.Fatal("Apply() = nil error, want the rename failure")
	}
	if got := readRecorded(statePath); got != "" {
		t.Errorf("recorded = %q, want empty so the next boot retries", got)
	}
}

func TestApply_RejectsInvalidNameWithoutRunningAnything(t *testing.T) {
	fr := &fakeRunner{}
	s, _ := newTestSetter(t, fr, "WIN-B38G6ED0CB8", "WIN-B38G6ED0CB8")

	if _, err := s.Apply(context.Background(), "web'; Restart-Computer; '"); err == nil {
		t.Fatal("Apply() = nil error, want a validation error")
	}
	if len(fr.calls) != 0 {
		t.Errorf("expected no command, got %d", len(fr.calls))
	}
}

func TestApply_EmptyHostnameDoesNothing(t *testing.T) {
	fr := &fakeRunner{}
	s, _ := newTestSetter(t, fr, "WIN-B38G6ED0CB8", "WIN-B38G6ED0CB8")

	if _, err := s.Apply(context.Background(), ""); err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(fr.calls) != 0 {
		t.Errorf("expected no command, got %d", len(fr.calls))
	}
}

// Reads the real registry: read-only, and proves the two value names exist.
func TestRegistryNames(t *testing.T) {
	current, pending, err := registryNames()
	if err != nil {
		t.Fatalf("registryNames() error: %v", err)
	}
	if current == "" || pending == "" {
		t.Errorf("registryNames() = %q, %q, want both non-empty", current, pending)
	}
}

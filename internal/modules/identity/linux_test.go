//go:build linux

package identity

import (
	"context"
	"errors"
	"os/user"
	"testing"
)

type call struct {
	command string
	stdin   []byte
	args    []string
}

type fakeRunner struct {
	calls    []call
	execErr  error
	execOut  string
	execErr2 string
}

func (f *fakeRunner) Execute(_ context.Context, command string, args ...string) (string, string, error) {
	f.calls = append(f.calls, call{command: command, args: args})
	return f.execOut, f.execErr2, f.execErr
}

func (f *fakeRunner) ExecuteWithStdin(_ context.Context, stdin []byte, command string, args ...string) (string, string, error) {
	f.calls = append(f.calls, call{command: command, stdin: stdin, args: args})
	return f.execOut, f.execErr2, f.execErr
}

// currentUsername returns a real, resolvable username on this system so
// SetPassword's user.Lookup check succeeds without needing root.
func currentUsername(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Skipf("could not determine current user: %v", err)
	}
	return u.Username
}

func TestSetPassword_PipesPasswordViaStdinNeverArgs(t *testing.T) {
	username := currentUsername(t)
	fr := &fakeRunner{}
	s := New(fr)

	if _, err := s.SetPassword(context.Background(), username, "s3cret"); err != nil {
		t.Fatalf("SetPassword() error: %v", err)
	}

	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.calls))
	}
	c := fr.calls[0]
	if c.command != "chpasswd" {
		t.Errorf("command = %q, want chpasswd", c.command)
	}
	wantStdin := username + ":s3cret\n"
	if string(c.stdin) != wantStdin {
		t.Errorf("stdin = %q, want %q", string(c.stdin), wantStdin)
	}
	for _, a := range c.args {
		if a == "s3cret" {
			t.Fatal("password must never appear in args")
		}
	}
}

func TestSetPassword_UnknownUser_ReturnsErrorWithoutRunning(t *testing.T) {
	fr := &fakeRunner{}
	s := New(fr)

	_, err := s.SetPassword(context.Background(), "definitely-not-a-real-user-12345", "s3cret")
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
	if len(fr.calls) != 0 {
		t.Errorf("expected no commands to run for unknown user, got %d", len(fr.calls))
	}
}

func TestSetPassword_MissingUsernameOrPassword(t *testing.T) {
	fr := &fakeRunner{}
	s := New(fr)

	if _, err := s.SetPassword(context.Background(), "", "s3cret"); err == nil {
		t.Error("expected error for empty username")
	}
	if _, err := s.SetPassword(context.Background(), "alice", ""); err == nil {
		t.Error("expected error for empty password")
	}
}

func TestSetPassword_ChpasswdFailure_PropagatesError(t *testing.T) {
	username := currentUsername(t)
	fr := &fakeRunner{execErr: errors.New("boom")}
	s := New(fr)

	if _, err := s.SetPassword(context.Background(), username, "s3cret"); err == nil {
		t.Fatal("expected error when chpasswd fails")
	}
}

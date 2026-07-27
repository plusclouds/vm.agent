//go:build windows

package identity

import (
	"context"
	"errors"
	"os/user"
	"testing"
)

type call struct {
	command string
	args    []string
}

type fakeRunner struct {
	calls   []call
	execErr error
}

func (f *fakeRunner) Execute(_ context.Context, command string, args ...string) (string, string, error) {
	f.calls = append(f.calls, call{command: command, args: args})
	return "", "", f.execErr
}

func (f *fakeRunner) ExecuteWithStdin(_ context.Context, _ []byte, command string, args ...string) (string, string, error) {
	f.calls = append(f.calls, call{command: command, args: args})
	return "", "", f.execErr
}

func currentUsername(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Skipf("could not determine current user: %v", err)
	}
	return u.Username
}

// TestSetPassword_UsesNetUser documents (rather than hides) the argv
// exposure: the password is a plain command-line argument to net.exe.
func TestSetPassword_UsesNetUser(t *testing.T) {
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
	if c.command != "net" {
		t.Errorf("command = %q, want net", c.command)
	}
	want := []string{"user", username, "s3cret"}
	if len(c.args) != len(want) {
		t.Fatalf("args = %v, want %v", c.args, want)
	}
	for i := range want {
		if c.args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, c.args[i], want[i])
		}
	}
}

func TestSetPassword_NetUserFailure_PropagatesError(t *testing.T) {
	username := currentUsername(t)
	fr := &fakeRunner{execErr: errors.New("boom")}
	s := New(fr)

	if _, err := s.SetPassword(context.Background(), username, "s3cret"); err == nil {
		t.Fatal("expected error when net user fails")
	}
}

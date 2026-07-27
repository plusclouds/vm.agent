//go:build linux

package identity

import (
	"context"
	"fmt"
	"os/user"
)

// linuxSetter changes a user's password via chpasswd, piping "user:password"
// on stdin so the plaintext password never appears in argv or in the
// executor's audit log (which only ever records command+args).
type linuxSetter struct {
	runner CommandRunner
}

// New returns a PasswordSetter backed by chpasswd.
func New(runner CommandRunner) PasswordSetter {
	return &linuxSetter{runner: runner}
}

func (s *linuxSetter) SetPassword(ctx context.Context, username, password string) (*Result, error) {
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	if _, err := user.Lookup(username); err != nil {
		return nil, fmt.Errorf("looking up user %q: %w", username, err)
	}

	stdin := []byte(username + ":" + password + "\n")
	_, stderr, err := s.runner.ExecuteWithStdin(ctx, stdin, "chpasswd")
	if err != nil {
		return nil, fmt.Errorf("chpasswd failed: %w (stderr: %s)", err, stderr)
	}

	return &Result{Username: username, Message: "password updated"}, nil
}

//go:build windows

package identity

import (
	"context"
	"fmt"
	"os/user"
)

// windowsSetter changes a user's password via net.exe.
//
// KNOWN LIMITATION: net.exe requires the new password as a plain
// command-line argument — there is no stdin-based equivalent on Windows for
// local accounts short of calling NetUserSetInfo directly via syscalls
// (a meaningfully larger amount of code: marshaling a USER_INFO_1003
// struct, UTF16 pointers, and a direct netapi32.dll call). This means the
// password is briefly visible to anything enumerating process command
// lines (e.g. Get-Process, Task Manager's "Command line" column, Sysmon)
// for the duration of the net.exe call. This is a known, accepted exposure,
// not mitigated by this implementation.
type windowsSetter struct {
	runner CommandRunner
}

// New returns a PasswordSetter backed by "net user".
func New(runner CommandRunner) PasswordSetter {
	return &windowsSetter{runner: runner}
}

func (s *windowsSetter) SetPassword(ctx context.Context, username, password string) (*Result, error) {
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	if _, err := user.Lookup(username); err != nil {
		return nil, fmt.Errorf("looking up user %q: %w", username, err)
	}

	_, stderr, err := s.runner.Execute(ctx, "net", "user", username, password)
	if err != nil {
		return nil, fmt.Errorf("net user failed: %w (stderr: %s)", err, stderr)
	}

	return &Result{Username: username, Message: "password updated"}, nil
}

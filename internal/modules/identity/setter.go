// Package identity changes local OS user account passwords. Platform-specific
// implementations are selected at compile time via build tags, mirroring
// internal/modules/diskresize's Resizer pattern.
package identity

import "context"

// PasswordSetter changes the login password for a local OS user.
type PasswordSetter interface {
	SetPassword(ctx context.Context, username, password string) (*Result, error)
}

// CommandRunner matches *executor.Executor's Execute/ExecuteWithStdin
// signatures, letting tests inject a fake instead of shelling out for real.
type CommandRunner interface {
	Execute(ctx context.Context, command string, args ...string) (stdout, stderr string, err error)
	ExecuteWithStdin(ctx context.Context, stdin []byte, command string, args ...string) (stdout, stderr string, err error)
}

// Result reports the outcome of a SetPassword call.
type Result struct {
	Username string `json:"username"`
	Message  string `json:"message"`
}

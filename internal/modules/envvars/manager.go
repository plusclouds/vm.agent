// Package envvars sets, unsets, and lists system-wide persistent
// environment variables. Platform-specific implementations are selected at
// compile time via build tags, mirroring internal/modules/diskresize's
// Resizer pattern.
package envvars

import "context"

// Manager sets, unsets, and lists system-wide persistent environment
// variables (surviving reboot, visible to new processes).
type Manager interface {
	Set(ctx context.Context, key, value string) (*Result, error)
	Unset(ctx context.Context, key string) (*Result, error)
	List(ctx context.Context) (map[string]string, error)
}

// Result reports the outcome of a Set or Unset call.
type Result struct {
	Key     string `json:"key"`
	Value   string `json:"value,omitempty"`
	Message string `json:"message"`
}

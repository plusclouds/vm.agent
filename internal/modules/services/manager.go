// Package services provides service management across operating systems.
// The Manager interface is the single entry point used by the dispatcher.
// Platform-specific implementations are selected at compile time via build tags.
package services

import "context"

// Manager is the OS-agnostic interface for service lifecycle operations.
type Manager interface {
	List(ctx context.Context) ([]ServiceInfo, error)
	Get(ctx context.Context, name string) (*ServiceInfo, error)
	Start(ctx context.Context, name string) (*ActionResult, error)
	Stop(ctx context.Context, name string) (*ActionResult, error)
	Restart(ctx context.Context, name string) (*ActionResult, error)
	Reload(ctx context.Context, name string) (*ActionResult, error)
	Enable(ctx context.Context, name string) (*ActionResult, error)
	Disable(ctx context.Context, name string) (*ActionResult, error)
}

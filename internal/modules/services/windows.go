//go:build windows

package services

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// windowsManager is a stub that reports all service operations as unsupported.
// Full Windows SCM integration (golang.org/x/sys/windows/svc) can replace
// this stub when Windows service management is required.
type windowsManager struct {
	logger *zap.Logger
}

// New returns a stub Manager for Windows.
func New(logger *zap.Logger) Manager {
	logger.Info("service management: Windows SCM not yet implemented — service operations will return unsupported")
	return &windowsManager{logger: logger}
}

var errNotSupported = fmt.Errorf("service management is not yet supported on Windows")

func (m *windowsManager) List(_ context.Context) ([]ServiceInfo, error) {
	return nil, errNotSupported
}
func (m *windowsManager) Get(_ context.Context, _ string) (*ServiceInfo, error) {
	return nil, errNotSupported
}
func (m *windowsManager) Start(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionStart, Success: false, Message: errNotSupported.Error()}, nil
}
func (m *windowsManager) Stop(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionStop, Success: false, Message: errNotSupported.Error()}, nil
}
func (m *windowsManager) Restart(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionRestart, Success: false, Message: errNotSupported.Error()}, nil
}
func (m *windowsManager) Reload(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionReload, Success: false, Message: errNotSupported.Error()}, nil
}
func (m *windowsManager) Enable(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionEnable, Success: false, Message: errNotSupported.Error()}, nil
}
func (m *windowsManager) Disable(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionDisable, Success: false, Message: errNotSupported.Error()}, nil
}

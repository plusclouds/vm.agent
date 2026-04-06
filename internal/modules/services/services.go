package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"
	"go.uber.org/zap"
)

// Module manages systemd services via the D-Bus system bus.
type Module struct {
	bus    *dbus.Conn
	logger *zap.Logger
}

// New creates a new services Module.
// bus should be a connection obtained via dbus.NewSystemdConnectionContext.
func New(bus *dbus.Conn, logger *zap.Logger) *Module {
	return &Module{
		bus:    bus,
		logger: logger,
	}
}

// ensureServiceSuffix appends ".service" to a unit name if it has no suffix.
func ensureServiceSuffix(name string) string {
	if !strings.Contains(name, ".") {
		return name + ".service"
	}
	return name
}

// List returns information about all currently loaded systemd units.
func (m *Module) List(ctx context.Context) ([]ServiceInfo, error) {
	units, err := m.bus.ListUnitsContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing systemd units: %w", err)
	}

	services := make([]ServiceInfo, 0, len(units))
	for _, u := range units {
		// Only include .service units to keep the list manageable.
		if !strings.HasSuffix(u.Name, ".service") {
			continue
		}
		services = append(services, m.unitToServiceInfo(u))
	}
	return services, nil
}

// Get returns detailed information for a single named unit.
func (m *Module) Get(ctx context.Context, name string) (*ServiceInfo, error) {
	name = ensureServiceSuffix(name)

	units, err := m.bus.ListUnitsByNamesContext(ctx, []string{name})
	if err != nil {
		return nil, fmt.Errorf("querying unit %s: %w", name, err)
	}
	if len(units) == 0 {
		return nil, fmt.Errorf("unit not found: %s", name)
	}

	info := m.unitToServiceInfo(units[0])
	return &info, nil
}

// Start starts the named unit and waits for the job to complete.
func (m *Module) Start(ctx context.Context, name string) (*ActionResult, error) {
	name = ensureServiceSuffix(name)
	m.logger.Info("starting service", zap.String("unit", name))

	ch := make(chan string, 1)
	if _, err := m.bus.StartUnitContext(ctx, name, "replace", ch); err != nil {
		return failResult(name, ActionStart, fmt.Errorf("start failed: %w", err)), nil
	}

	result := <-ch
	if result != "done" {
		return failResult(name, ActionStart, fmt.Errorf("job result: %s", result)), nil
	}

	return &ActionResult{
		Service: name,
		Action:  ActionStart,
		Success: true,
		Message: fmt.Sprintf("Service %s started successfully.", name),
	}, nil
}

// Stop stops the named unit and waits for the job to complete.
func (m *Module) Stop(ctx context.Context, name string) (*ActionResult, error) {
	name = ensureServiceSuffix(name)
	m.logger.Info("stopping service", zap.String("unit", name))

	ch := make(chan string, 1)
	if _, err := m.bus.StopUnitContext(ctx, name, "replace", ch); err != nil {
		return failResult(name, ActionStop, fmt.Errorf("stop failed: %w", err)), nil
	}

	result := <-ch
	if result != "done" {
		return failResult(name, ActionStop, fmt.Errorf("job result: %s", result)), nil
	}

	return &ActionResult{
		Service: name,
		Action:  ActionStop,
		Success: true,
		Message: fmt.Sprintf("Service %s stopped successfully.", name),
	}, nil
}

// Restart restarts the named unit and waits for the job to complete.
func (m *Module) Restart(ctx context.Context, name string) (*ActionResult, error) {
	name = ensureServiceSuffix(name)
	m.logger.Info("restarting service", zap.String("unit", name))

	ch := make(chan string, 1)
	if _, err := m.bus.RestartUnitContext(ctx, name, "replace", ch); err != nil {
		return failResult(name, ActionRestart, fmt.Errorf("restart failed: %w", err)), nil
	}

	result := <-ch
	if result != "done" {
		return failResult(name, ActionRestart, fmt.Errorf("job result: %s", result)), nil
	}

	return &ActionResult{
		Service: name,
		Action:  ActionRestart,
		Success: true,
		Message: fmt.Sprintf("Service %s restarted successfully.", name),
	}, nil
}

// Enable enables the named unit to start on boot.
func (m *Module) Enable(ctx context.Context, name string) (*ActionResult, error) {
	name = ensureServiceSuffix(name)
	m.logger.Info("enabling service", zap.String("unit", name))

	if _, _, err := m.bus.EnableUnitFilesContext(ctx, []string{name}, false, true); err != nil {
		return failResult(name, ActionEnable, fmt.Errorf("enable failed: %w", err)), nil
	}

	if err := m.bus.ReloadContext(ctx); err != nil {
		m.logger.Warn("daemon-reload after enable failed", zap.Error(err))
	}

	return &ActionResult{
		Service: name,
		Action:  ActionEnable,
		Success: true,
		Message: fmt.Sprintf("Service %s enabled successfully.", name),
	}, nil
}

// Disable disables the named unit from starting on boot.
func (m *Module) Disable(ctx context.Context, name string) (*ActionResult, error) {
	name = ensureServiceSuffix(name)
	m.logger.Info("disabling service", zap.String("unit", name))

	if _, err := m.bus.DisableUnitFilesContext(ctx, []string{name}, false); err != nil {
		return failResult(name, ActionDisable, fmt.Errorf("disable failed: %w", err)), nil
	}

	if err := m.bus.ReloadContext(ctx); err != nil {
		m.logger.Warn("daemon-reload after disable failed", zap.Error(err))
	}

	return &ActionResult{
		Service: name,
		Action:  ActionDisable,
		Success: true,
		Message: fmt.Sprintf("Service %s disabled successfully.", name),
	}, nil
}

// Reload sends a reload signal to the named unit.
func (m *Module) Reload(ctx context.Context, name string) (*ActionResult, error) {
	name = ensureServiceSuffix(name)
	m.logger.Info("reloading service", zap.String("unit", name))

	ch := make(chan string, 1)
	if _, err := m.bus.ReloadUnitContext(ctx, name, "replace", ch); err != nil {
		return failResult(name, ActionReload, fmt.Errorf("reload failed: %w", err)), nil
	}

	result := <-ch
	if result != "done" {
		return failResult(name, ActionReload, fmt.Errorf("job result: %s", result)), nil
	}

	return &ActionResult{
		Service: name,
		Action:  ActionReload,
		Success: true,
		Message: fmt.Sprintf("Service %s reloaded successfully.", name),
	}, nil
}

// unitToServiceInfo converts a dbus.UnitStatus to a ServiceInfo.
func (m *Module) unitToServiceInfo(unit dbus.UnitStatus) ServiceInfo {
	state := StateUnknown
	switch unit.ActiveState {
	case "active":
		state = StateActive
	case "inactive":
		state = StateInactive
	case "failed":
		state = StateFailed
	}

	return ServiceInfo{
		Name:        unit.Name,
		Description: unit.Description,
		State:       state,
		SubState:    unit.SubState,
		// Enabled status is not available in UnitStatus; set false as default.
		// To get accurate enabled status, a separate GetUnitFileState call is needed.
		Enabled: false,
		PID:     0,
	}
}

// failResult is a helper that constructs a failed ActionResult.
func failResult(name string, action ServiceAction, err error) *ActionResult {
	return &ActionResult{
		Service: name,
		Action:  action,
		Success: false,
		Message: err.Error(),
	}
}

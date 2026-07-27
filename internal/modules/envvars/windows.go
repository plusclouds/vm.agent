//go:build windows

package envvars

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"

	"go.uber.org/zap"
	"golang.org/x/sys/windows/registry"
)

const environmentKeyPath = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`

// windowsManager sets machine-wide environment variables in the registry
// and broadcasts WM_SETTINGCHANGE so already-running processes (notably
// Explorer and its child shells) pick up the change without a reboot.
// Processes started after the registry write pick it up regardless, via
// normal registry-read-at-process-creation-time behavior.
type windowsManager struct {
	keyPath string // overridable in tests
	root    registry.Key
	logger  *zap.Logger
}

// New returns a Manager backed by the machine environment registry key.
func New(logger *zap.Logger) Manager {
	return &windowsManager{keyPath: environmentKeyPath, root: registry.LOCAL_MACHINE, logger: logger}
}

func (m *windowsManager) Set(ctx context.Context, key, value string) (*Result, error) {
	k, err := registry.OpenKey(m.root, m.keyPath, registry.SET_VALUE)
	if err != nil {
		return nil, fmt.Errorf("opening environment key: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(key, value); err != nil {
		return nil, fmt.Errorf("setting %q: %w", key, err)
	}
	m.broadcast()

	return &Result{Key: key, Value: value, Message: "environment variable set"}, nil
}

func (m *windowsManager) Unset(ctx context.Context, key string) (*Result, error) {
	k, err := registry.OpenKey(m.root, m.keyPath, registry.SET_VALUE)
	if err != nil {
		return nil, fmt.Errorf("opening environment key: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(key); err != nil {
		if err == registry.ErrNotExist {
			return &Result{Key: key, Message: "already unset"}, nil
		}
		return nil, fmt.Errorf("deleting %q: %w", key, err)
	}
	m.broadcast()

	return &Result{Key: key, Message: "environment variable unset"}, nil
}

func (m *windowsManager) List(ctx context.Context) (map[string]string, error) {
	k, err := registry.OpenKey(m.root, m.keyPath, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("opening environment key: %w", err)
	}
	defer k.Close()

	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil, fmt.Errorf("reading value names: %w", err)
	}

	out := make(map[string]string, len(names))
	for _, n := range names {
		v, _, err := k.GetStringValue(n)
		if err == nil {
			out[n] = v
		}
	}
	return out, nil
}

// broadcast sends WM_SETTINGCHANGE to all top-level windows. Best-effort:
// the registry write has already succeeded regardless of whether this
// notifies anyone, so failures here are logged, not returned as errors.
func (m *windowsManager) broadcast() {
	const (
		hwndBroadcast    = 0xffff
		wmSettingChange  = 0x001A
		smtoAbortIfHung  = 0x0002
		broadcastTimeout = 5000
	)

	user32 := syscall.NewLazyDLL("user32.dll")
	sendMessageTimeout := user32.NewProc("SendMessageTimeoutW")

	param, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("could not broadcast environment change", zap.Error(err))
		}
		return
	}

	ret, _, callErr := sendMessageTimeout.Call(
		hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(param)),
		smtoAbortIfHung, broadcastTimeout, 0,
	)
	if ret == 0 && m.logger != nil {
		m.logger.Warn("WM_SETTINGCHANGE broadcast did not complete", zap.Error(callErr))
	}
}

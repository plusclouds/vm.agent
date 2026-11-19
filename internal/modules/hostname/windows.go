//go:build windows

package hostname

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const computerNameKey = `SYSTEM\CurrentControlSet\Control\ComputerName\`

// namesFunc returns the computer name in use now and the one that takes
// effect after the next restart. They differ only while a rename is waiting.
type namesFunc func() (current, pending string, err error)

type windowsSetter struct {
	runner    CommandRunner
	statePath string
	names     namesFunc
}

// New returns a Setter backed by Rename-Computer. statePath is the file where
// the last hostname taken from the platform is remembered.
func New(runner CommandRunner, statePath string) Setter {
	return &windowsSetter{runner: runner, statePath: statePath, names: registryNames}
}

// registryNames reads both names straight from the registry: no PowerShell
// start-up cost, which at boot is long enough to hit command timeouts.
func registryNames() (string, string, error) {
	read := func(sub string) (string, error) {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, computerNameKey+sub, registry.QUERY_VALUE)
		if err != nil {
			return "", fmt.Errorf("opening %s: %w", sub, err)
		}
		defer k.Close()
		v, _, err := k.GetStringValue("ComputerName")
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", sub, err)
		}
		return v, nil
	}

	current, err := read("ActiveComputerName")
	if err != nil {
		return "", "", err
	}
	pending, err := read("ComputerName")
	if err != nil {
		return "", "", err
	}
	return current, pending, nil
}

func (s *windowsSetter) Apply(ctx context.Context, desired string) (*Result, error) {
	if desired == "" {
		return &Result{Message: "no hostname in metadata"}, nil
	}
	if err := validate(desired); err != nil {
		return nil, err
	}

	current, pending, err := s.names()
	if err != nil {
		return nil, fmt.Errorf("reading computer name: %w", err)
	}

	result := &Result{Hostname: desired, Previous: current}

	switch decide(desired, current, pending, readRecorded(s.statePath)) {
	case actionNone:
		result.Message = "hostname left as it is"
		return result, nil

	case actionRecord:
		if err := writeRecorded(s.statePath, desired); err != nil {
			return nil, err
		}
		result.Message = "hostname left as it is"
		return result, nil
	}

	// desired passed validate, so it holds only letters, digits and hyphens
	// and cannot break out of the quoted argument.
	script := fmt.Sprintf("Rename-Computer -NewName '%s' -Force -ErrorAction Stop", desired)
	_, stderr, err := s.runner.Execute(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return nil, fmt.Errorf("Rename-Computer failed: %w (stderr: %s)", err, stderr)
	}

	if err := writeRecorded(s.statePath, desired); err != nil {
		return nil, err
	}

	result.Changed = true
	result.RebootRequired = true
	result.Message = "hostname changed, takes effect after the next restart"
	return result, nil
}

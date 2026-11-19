//go:build !windows

package hostname

import "context"

// On Linux the hostname is set by the first-boot playbook the platform ships
// on the config-drive, so there is nothing for the agent to do.
type noopSetter struct{}

// New returns a Setter that does nothing. The arguments are accepted so the
// call site is the same on every platform.
func New(_ CommandRunner, _ string) Setter {
	return noopSetter{}
}

func (noopSetter) Apply(_ context.Context, desired string) (*Result, error) {
	return &Result{Hostname: desired, Message: "hostname is handled outside the agent on this platform"}, nil
}

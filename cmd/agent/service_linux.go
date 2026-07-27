//go:build linux

package main

// maybeRunAsService is a no-op on Linux — the agent always runs as a plain
// foreground process, managed by systemd (see systemd/plusclouds-agent.service).
func maybeRunAsService() (handled bool, err error) {
	return false, nil
}

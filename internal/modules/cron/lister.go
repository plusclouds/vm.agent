// Package cron provides read-only visibility into scheduled tasks: cron-style
// jobs (system crontab, /etc/cron.d, per-user crontabs) and systemd timers.
// It only lists what's already scheduled — creating, editing, or removing a
// job is out of scope, since a scheduled task is effectively unsupervised
// remote code execution and deserves its own, more carefully-gated design.
package cron

import "context"

// Lister reports the scheduled tasks currently configured on the host.
type Lister interface {
	List(ctx context.Context) ([]Job, error)
}

// Job is one scheduled task, from either a cron-style source or a systemd
// timer. Only the fields relevant to its Source are populated.
type Job struct {
	// Source is "cron" or "systemd_timer".
	Source string `json:"source"`

	// -- cron fields --
	// User is the account the job runs as.
	User string `json:"user,omitempty"`
	// Schedule is the raw schedule expression (e.g. "*/5 * * * *" or "@daily").
	Schedule string `json:"schedule,omitempty"`
	// Command is the command line the job runs.
	Command string `json:"command,omitempty"`
	// File is the crontab-style file the entry was read from.
	File string `json:"file,omitempty"`

	// -- systemd_timer fields --
	// Unit is the timer's own unit name (e.g. "logrotate.timer").
	Unit string `json:"unit,omitempty"`
	// Activates is the unit the timer triggers (e.g. "logrotate.service").
	Activates string `json:"activates,omitempty"`
	// NextRun is the next scheduled trigger, Unix seconds, if known.
	NextRun *int64 `json:"next_run,omitempty"`
	// LastRun is the last trigger time, Unix seconds, if known.
	LastRun *int64 `json:"last_run,omitempty"`

	// Enabled reports whether the job is currently active (a cron entry
	// present in a file is always considered enabled; a systemd timer is
	// enabled if its unit is active).
	Enabled bool `json:"enabled"`
}

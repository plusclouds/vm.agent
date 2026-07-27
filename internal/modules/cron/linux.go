//go:build linux

package cron

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"
	"go.uber.org/zap"
)

// perUserCrontabDirs are checked in order; only the first one that exists
// is read, since Debian/Ubuntu and RHEL-family systems each use one of
// these but not both for the same purpose.
var perUserCrontabDirs = []string{
	"/var/spool/cron/crontabs", // Debian/Ubuntu
	"/var/spool/cron",          // RHEL/CentOS/AlmaLinux
}

// linuxLister reads cron-style files directly (no dependency on a `crontab`
// binary being installed) and lists systemd timers over D-Bus.
type linuxLister struct {
	bus    *dbus.Conn
	logger *zap.Logger
}

// New returns a Lister backed by cron-style files and, if bus is non-nil,
// systemd timers. bus may be nil — in that case only cron-style files are
// reported (graceful degradation, matching services.New's contract).
func New(bus *dbus.Conn, logger *zap.Logger) Lister {
	return &linuxLister{bus: bus, logger: logger}
}

func (l *linuxLister) List(ctx context.Context) ([]Job, error) {
	var jobs []Job

	if data, err := os.ReadFile("/etc/crontab"); err == nil {
		jobs = append(jobs, parseCrontabContent(string(data), "/etc/crontab", "", true)...)
	}

	if entries, err := os.ReadDir("/etc/cron.d"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join("/etc/cron.d", e.Name())
			if data, err := os.ReadFile(path); err == nil {
				jobs = append(jobs, parseCrontabContent(string(data), path, "", true)...)
			}
		}
	}

	for _, dir := range perUserCrontabDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			jobs = append(jobs, parseCrontabContent(string(data), path, e.Name(), false)...)
		}
		break // only the first matching convention, to avoid double-counting
	}

	if l.bus != nil {
		timers, err := l.listTimers(ctx)
		if err != nil {
			l.logger.Warn("cron: could not list systemd timers", zap.Error(err))
		} else {
			jobs = append(jobs, timers...)
		}
	}

	return jobs, nil
}

func (l *linuxLister) listTimers(ctx context.Context) ([]Job, error) {
	units, err := l.bus.ListUnitsContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing systemd units: %w", err)
	}

	var jobs []Job
	for _, u := range units {
		if !strings.HasSuffix(u.Name, ".timer") {
			continue
		}
		job := Job{
			Source:  "systemd_timer",
			Unit:    u.Name,
			Enabled: u.ActiveState == "active",
		}

		props, err := l.bus.GetUnitTypePropertiesContext(ctx, u.Name, "Timer")
		if err != nil {
			l.logger.Debug("cron: could not read timer properties", zap.String("unit", u.Name), zap.Error(err))
			jobs = append(jobs, job)
			continue
		}
		if v, ok := props["Unit"].(string); ok {
			job.Activates = v
		}
		if t := usecToUnixSeconds(props["NextElapseUSecRealtime"]); t != nil {
			job.NextRun = t
		}
		if t := usecToUnixSeconds(props["LastTriggerUSec"]); t != nil {
			job.LastRun = t
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// usecInfinity is systemd's USEC_INFINITY sentinel (UINT64_MAX), used by
// NextElapseUSecRealtime to mean "not scheduled" (e.g. a disabled timer) —
// as distinct from 0, which LastTriggerUSec uses to mean "never triggered".
const usecInfinity = ^uint64(0)

// usecToUnixSeconds converts a systemd D-Bus CALENDAR_USEC property (a
// uint64 count of microseconds since the Unix epoch) into Unix seconds, or
// nil if the property is unset (0) or the "never scheduled" sentinel.
func usecToUnixSeconds(v interface{}) *int64 {
	usec, ok := v.(uint64)
	if !ok || usec == 0 || usec == usecInfinity {
		return nil
	}
	sec := int64(usec / 1_000_000)
	return &sec
}

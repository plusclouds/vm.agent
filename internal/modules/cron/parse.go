package cron

import (
	"regexp"
	"strings"
)

var envAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*=`)

// parseCrontabContent parses the entries of a crontab-style file's content.
// hasUserField is true for system-wide files (/etc/crontab, /etc/cron.d/*),
// whose lines carry an explicit user field between the schedule and the
// command; it's false for per-user crontabs (/var/spool/cron/...), where
// the user is implied by the file itself (impliedUser).
func parseCrontabContent(content, file, impliedUser string, hasUserField bool) []Job {
	var jobs []Job
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || envAssignment.MatchString(trimmed) {
			continue
		}

		fields := strings.Fields(trimmed)
		var schedule string
		var rest []string

		if strings.HasPrefix(fields[0], "@") {
			schedule = fields[0]
			rest = fields[1:]
		} else {
			if len(fields) < 6 {
				continue // not enough fields for 5 schedule tokens + a command
			}
			schedule = strings.Join(fields[:5], " ")
			rest = fields[5:]
		}

		var user, command string
		if hasUserField {
			if len(rest) < 2 {
				continue // missing the user field or the command
			}
			user = rest[0]
			command = strings.Join(rest[1:], " ")
		} else {
			if len(rest) < 1 {
				continue
			}
			user = impliedUser
			command = strings.Join(rest, " ")
		}

		jobs = append(jobs, Job{
			Source:   "cron",
			User:     user,
			Schedule: schedule,
			Command:  command,
			File:     file,
			Enabled:  true,
		})
	}
	return jobs
}

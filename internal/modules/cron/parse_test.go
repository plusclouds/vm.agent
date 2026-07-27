package cron

import (
	"reflect"
	"testing"
)

func TestParseCrontabContent_SystemWideWithUserField(t *testing.T) {
	content := `# comment line, should be skipped
PATH=/usr/bin:/bin
SHELL=/bin/sh

*/5 * * * * root /usr/local/bin/backup.sh
0 3 * * 0 www-data /usr/bin/find /tmp -mtime +7 -delete
`
	jobs := parseCrontabContent(content, "/etc/crontab", "", true)

	want := []Job{
		{Source: "cron", User: "root", Schedule: "*/5 * * * *", Command: "/usr/local/bin/backup.sh", File: "/etc/crontab", Enabled: true},
		{Source: "cron", User: "www-data", Schedule: "0 3 * * 0", Command: "/usr/bin/find /tmp -mtime +7 -delete", File: "/etc/crontab", Enabled: true},
	}
	if !reflect.DeepEqual(jobs, want) {
		t.Errorf("got %+v, want %+v", jobs, want)
	}
}

func TestParseCrontabContent_PerUserNoUserField(t *testing.T) {
	content := "30 2 * * * /home/deploy/bin/nightly-sync.sh --quiet\n"
	jobs := parseCrontabContent(content, "/var/spool/cron/crontabs/deploy", "deploy", false)

	want := []Job{
		{Source: "cron", User: "deploy", Schedule: "30 2 * * *", Command: "/home/deploy/bin/nightly-sync.sh --quiet", File: "/var/spool/cron/crontabs/deploy", Enabled: true},
	}
	if !reflect.DeepEqual(jobs, want) {
		t.Errorf("got %+v, want %+v", jobs, want)
	}
}

func TestParseCrontabContent_Nicknames(t *testing.T) {
	content := "@daily root /usr/local/sbin/logrotate-extra\n@reboot www-data /opt/app/on-boot.sh\n"
	jobs := parseCrontabContent(content, "/etc/cron.d/myapp", "", true)

	want := []Job{
		{Source: "cron", User: "root", Schedule: "@daily", Command: "/usr/local/sbin/logrotate-extra", File: "/etc/cron.d/myapp", Enabled: true},
		{Source: "cron", User: "www-data", Schedule: "@reboot", Command: "/opt/app/on-boot.sh", File: "/etc/cron.d/myapp", Enabled: true},
	}
	if !reflect.DeepEqual(jobs, want) {
		t.Errorf("got %+v, want %+v", jobs, want)
	}
}

func TestParseCrontabContent_PerUserNickname(t *testing.T) {
	content := "@hourly /home/deploy/bin/heartbeat.sh\n"
	jobs := parseCrontabContent(content, "/var/spool/cron/crontabs/deploy", "deploy", false)

	want := []Job{
		{Source: "cron", User: "deploy", Schedule: "@hourly", Command: "/home/deploy/bin/heartbeat.sh", File: "/var/spool/cron/crontabs/deploy", Enabled: true},
	}
	if !reflect.DeepEqual(jobs, want) {
		t.Errorf("got %+v, want %+v", jobs, want)
	}
}

func TestParseCrontabContent_SkipsMalformedLines(t *testing.T) {
	content := "* * * * incomplete\nnot a cron line at all\nroot missing-schedule-fields\n"
	jobs := parseCrontabContent(content, "/etc/crontab", "", true)
	if len(jobs) != 0 {
		t.Errorf("expected no jobs from malformed input, got %+v", jobs)
	}
}

func TestParseCrontabContent_EmptyContent(t *testing.T) {
	if jobs := parseCrontabContent("", "/etc/crontab", "", true); jobs != nil {
		t.Errorf("expected nil for empty content, got %+v", jobs)
	}
}

func TestParseCrontabContent_IgnoresBlankAndCommentOnlyContent(t *testing.T) {
	content := "\n\n# nothing here\n   \n# another comment\n"
	if jobs := parseCrontabContent(content, "/etc/crontab", "", true); jobs != nil {
		t.Errorf("expected nil, got %+v", jobs)
	}
}

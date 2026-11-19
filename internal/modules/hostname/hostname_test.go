package hostname

import (
	"path/filepath"
	"testing"
)

func TestDecide(t *testing.T) {
	tests := []struct {
		name                                string
		desired, current, pending, recorded string
		want                                action
	}{
		{"fresh machine with a generated name is renamed", "web-1", "WIN-B38G6ED0CB8", "WIN-B38G6ED0CB8", "", actionRename},
		{"fresh desktop edition name is renamed", "web-1", "DESKTOP-A1B2C3D", "DESKTOP-A1B2C3D", "", actionRename},
		{"generated name check ignores case", "web-1", "win-b38g6ed0cb8", "win-b38g6ed0cb8", "", actionRename},
		{"machine named by its owner before this version is kept", "web-1", "SQLPROD", "SQLPROD", "", actionRecord},
		{"generated name with another rename already waiting is kept", "web-1", "WIN-B38G6ED0CB8", "SQLPROD", "", actionRecord},
		{"already the desired name, not recorded yet", "web-1", "WEB-1", "WEB-1", "", actionRecord},
		{"already the desired name and recorded", "web-1", "WEB-1", "WEB-1", "web-1", actionNone},
		{"rename to the desired name is waiting for a restart", "web-1", "WIN-B38G6ED0CB8", "WEB-1", "web-1", actionNone},
		{"hostname changed in the panel", "web-2", "WEB-1", "WEB-1", "web-1", actionRename},
		{"renamed inside the guest after we applied it", "web-1", "SQLPROD", "SQLPROD", "web-1", actionNone},
		{"renamed inside the guest back to a generated-looking name", "web-1", "WIN-B38G6ED0CB8", "WIN-B38G6ED0CB8", "web-1", actionNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decide(tt.desired, tt.current, tt.pending, tt.recorded); got != tt.want {
				t.Errorf("decide() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	valid := []string{"a", "web-1", "backup-test", "A1", "abcdefghijklmno"}
	for _, name := range valid {
		if err := validate(name); err != nil {
			t.Errorf("validate(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"",
		"abcdefghijklmnop",          // 16 characters
		"-web",                      // leading hyphen
		"web-",                      // trailing hyphen
		"web.example.com",           // dots
		"web 1",                     // space
		"12345",                     // all digits
		"web'; Restart-Computer; '", // would break out of the quoted argument
		"web$(whoami)",
		"web`n",
	}
	for _, name := range invalid {
		if err := validate(name); err == nil {
			t.Errorf("validate(%q) = nil, want an error", name)
		}
	}
}

func TestRecordedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "hostname-applied")

	if got := readRecorded(path); got != "" {
		t.Fatalf("readRecorded() on a missing file = %q, want empty", got)
	}
	if err := writeRecorded(path, "web-1"); err != nil {
		t.Fatalf("writeRecorded() error: %v", err)
	}
	if got := readRecorded(path); got != "web-1" {
		t.Errorf("readRecorded() = %q, want web-1", got)
	}
	if err := writeRecorded(path, "web-2"); err != nil {
		t.Fatalf("writeRecorded() overwrite error: %v", err)
	}
	if got := readRecorded(path); got != "web-2" {
		t.Errorf("readRecorded() after overwrite = %q, want web-2", got)
	}
}

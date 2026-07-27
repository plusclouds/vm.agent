//go:build linux

package envvars

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.uber.org/zap"
)

const environmentFilePath = "/etc/environment"

// validEnvKeyRe enforces POSIX shell/env-var name rules — the one
// injection point into a systemwide file every process reads, so this is
// checked before the file is ever touched.
var validEnvKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// linuxManager rewrites /etc/environment, the file PAM's pam_env module
// reads at login. Changes here are not visible to already-running
// processes — only to new login sessions — which is inherent to this
// mechanism, not a gap in this implementation.
type linuxManager struct {
	path   string // overridable in tests
	logger *zap.Logger
}

// New returns a Manager backed by /etc/environment. logger is accepted (but
// currently unused on Linux) for signature symmetry with the Windows
// implementation, which needs it to log broadcast failures.
func New(logger *zap.Logger) Manager {
	return &linuxManager{path: environmentFilePath, logger: logger}
}

func (m *linuxManager) Set(ctx context.Context, key, value string) (*Result, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	changed, err := m.rewrite(key, &value)
	if err != nil {
		return nil, err
	}
	msg := "environment variable set"
	if !changed {
		msg = "no change — already set to this value"
	}
	return &Result{Key: key, Value: value, Message: msg}, nil
}

func (m *linuxManager) Unset(ctx context.Context, key string) (*Result, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	changed, err := m.rewrite(key, nil)
	if err != nil {
		return nil, err
	}
	msg := "environment variable unset"
	if !changed {
		msg = "already unset"
	}
	return &Result{Key: key, Message: msg}, nil
}

func (m *linuxManager) List(ctx context.Context) (map[string]string, error) {
	existing, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("reading %q: %w", m.path, err)
	}

	out := make(map[string]string)
	for _, line := range strings.Split(string(existing), "\n") {
		trimmed := strings.TrimSpace(line)
		if k, v, ok := parseEnvLine(trimmed); ok {
			out[k] = v
		}
	}
	return out, nil
}

func validateKey(key string) error {
	if !validEnvKeyRe.MatchString(key) {
		return fmt.Errorf("invalid environment variable name %q", key)
	}
	return nil
}

// rewrite parses the existing file, sets/removes key (value == nil means
// unset), and atomically rewrites the file only if content actually
// changed. Comment lines and lines for other keys are preserved verbatim.
func (m *linuxManager) rewrite(key string, value *string) (changed bool, err error) {
	existing, err := os.ReadFile(m.path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("reading %q: %w", m.path, err)
	}

	var out []string
	found := false
	for _, line := range strings.Split(string(existing), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if k, _, ok := parseEnvLine(trimmed); ok && k == key {
			found = true
			if value != nil {
				out = append(out, formatEnvLine(key, *value))
			}
			continue
		}
		out = append(out, trimmed)
	}
	if value != nil && !found {
		out = append(out, formatEnvLine(key, *value))
	}

	rendered := ""
	if len(out) > 0 {
		rendered = strings.Join(out, "\n") + "\n"
	}

	if rendered == string(existing) {
		return false, nil
	}

	if err := atomicWrite(m.path, []byte(rendered), 0644); err != nil {
		return false, err
	}
	return true, nil
}

// parseEnvLine parses a KEY=VALUE or KEY="VALUE" line. Comment lines
// (starting with #) and anything else that doesn't parse are reported as
// not-ok and left untouched by callers.
func parseEnvLine(line string) (key, value string, ok bool) {
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	idx := strings.IndexByte(line, '=')
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	if !validEnvKeyRe.MatchString(key) {
		return "", "", false
	}
	value = strings.TrimSpace(line[idx+1:])
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return key, value, true
}

// formatEnvLine always writes the quoted KEY="VALUE" form, regardless of
// how the value was originally read, so values containing spaces are
// handled safely.
func formatEnvLine(key, value string) string {
	return fmt.Sprintf("%s=%q", key, value)
}

// atomicWrite writes data to a temp file in path's directory then renames
// it into place, mirroring pkg/isoconfig/load.go's cacheCopy pattern.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating %q: %w", dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming temp file into place: %w", err)
	}
	return nil
}

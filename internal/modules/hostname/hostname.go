// Package hostname applies the hostname the platform put in the config-drive
// metadata. On Windows this replaces the manual step of running the toolkit's
// register-startup-task.ps1: the agent already applies the password, network,
// SSH keys and environment variables at boot, and the hostname was the one
// thing left to that script.
//
// Renaming a machine is not harmless (SQL Server, domain membership and
// anything else that remembers the computer name notice it), so Apply only
// renames when the name is clearly still the platform's to set. See decide.
package hostname

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Setter applies the desired hostname to the machine.
type Setter interface {
	Apply(ctx context.Context, desired string) (*Result, error)
}

// CommandRunner matches *executor.Executor's Execute signature, letting tests
// inject a fake instead of shelling out for real.
type CommandRunner interface {
	Execute(ctx context.Context, command string, args ...string) (stdout, stderr string, err error)
}

// Result reports the outcome of an Apply call.
type Result struct {
	Hostname       string `json:"hostname"`
	Previous       string `json:"previous"`
	Changed        bool   `json:"changed"`
	RebootRequired bool   `json:"reboot_required"`
	Message        string `json:"message"`
}

type action int

const (
	// actionNone leaves the machine alone and records nothing.
	actionNone action = iota
	// actionRecord leaves the machine alone and remembers the desired name,
	// so only a later change of that name in the panel triggers a rename.
	actionRecord
	// actionRename renames the machine and remembers the desired name.
	actionRename
)

// A Windows computer name is a NetBIOS name: at most 15 characters, letters,
// digits and hyphens, not starting or ending with a hyphen. Anything else is
// refused rather than passed to Rename-Computer, which would truncate or
// reject it. The same pattern is what makes it safe to place the name in the
// PowerShell command.
var validName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,13}[A-Za-z0-9])?$`)

// Names Windows setup generates for a machine nobody has named yet.
var generatedName = regexp.MustCompile(`(?i)^(WIN-[A-Z0-9]{11}|DESKTOP-[A-Z0-9]{7})$`)

var allDigits = regexp.MustCompile(`^[0-9]+$`)

func validate(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("hostname %q is not a valid computer name (1-15 letters, digits or hyphens)", name)
	}
	if allDigits.MatchString(name) {
		return fmt.Errorf("hostname %q is not a valid computer name (it cannot be all digits)", name)
	}
	return nil
}

// decide says what to do, given the name the platform wants, the name the
// machine has now, the name it will have after the next restart (equal to
// current unless a rename is waiting) and the name this agent last recorded.
//
//   - Already the desired name, now or after the pending restart: nothing to
//     rename, just record it.
//   - Nothing recorded yet and the machine still has a setup-generated name:
//     it is a fresh machine, rename it.
//   - Nothing recorded yet and the machine has a name someone chose: it was
//     configured before this agent version arrived. Keep the name, record the
//     desired one.
//   - Recorded name differs from the desired one: the hostname was changed in
//     the panel since, rename.
//   - Recorded name equals the desired one but the machine is called something
//     else: it was renamed from inside the guest afterwards. Keep it.
func decide(desired, current, pending, recorded string) action {
	if strings.EqualFold(desired, current) || strings.EqualFold(desired, pending) {
		if strings.EqualFold(desired, recorded) {
			return actionNone
		}
		return actionRecord
	}

	if recorded == "" {
		if strings.EqualFold(current, pending) && generatedName.MatchString(current) {
			return actionRename
		}
		return actionRecord
	}

	if !strings.EqualFold(recorded, desired) {
		return actionRename
	}
	return actionNone
}

func readRecorded(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeRecorded(path, name string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(name+"\n"), 0600); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming state file: %w", err)
	}
	return nil
}

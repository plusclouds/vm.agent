//go:build windows

package sshkeys

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// windowsProvisioner writes to the target user's per-user authorized_keys
// file (%USERPROFILE%\.ssh\authorized_keys), as Win32-OpenSSH uses for
// non-administrator accounts.
//
// It does not handle the separate administrators_authorized_keys file
// Win32-OpenSSH requires for members of the Administrators group, and does
// not set the strict ACLs OpenSSH enforces on that file (owner- and
// SYSTEM-only, no inherited permissions) — using this for an administrator
// account requires tightening the file's ACL separately (e.g. via icacls)
// or OpenSSH will refuse to honor it.
type windowsProvisioner struct{}

// New returns a Provisioner backed by the local user database and
// %USERPROFILE%\.ssh\authorized_keys.
func New() Provisioner { return &windowsProvisioner{} }

func (p *windowsProvisioner) Provision(username string, keys []string) (*Result, error) {
	if len(keys) == 0 {
		return &Result{Username: username}, nil
	}

	u, err := user.Lookup(username)
	if err != nil {
		return nil, fmt.Errorf("looking up user %q: %w", username, err)
	}

	sshDir := filepath.Join(u.HomeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return nil, fmt.Errorf("creating %q: %w", sshDir, err)
	}

	path := filepath.Join(sshDir, "authorized_keys")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}

	merged, added, total := mergeAuthorizedKeys(string(existing), keys)

	if len(added) > 0 {
		if err := os.WriteFile(path, []byte(merged), 0600); err != nil {
			return nil, fmt.Errorf("writing %q: %w", path, err)
		}
	}

	return &Result{Username: username, Path: path, AddedKeys: added, TotalKeys: total}, nil
}

//go:build linux

package sshkeys

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// linuxProvisioner writes to the target user's ~/.ssh/authorized_keys,
// creating the .ssh directory (0700) and file (0600) if needed and
// chown-ing both to that user — the agent runs as root, so without this
// the user would be unable to read their own authorized_keys.
type linuxProvisioner struct{}

// New returns a Provisioner backed by the local user database and
// ~/.ssh/authorized_keys.
func New() Provisioner { return &linuxProvisioner{} }

func (p *linuxProvisioner) Provision(username string, keys []string) (*Result, error) {
	if len(keys) == 0 {
		return &Result{Username: username}, nil
	}

	u, uid, gid, err := lookupUser(username)
	if err != nil {
		return nil, err
	}

	result, err := writeAuthorizedKeys(filepath.Join(u.HomeDir, ".ssh"), keys, uid, gid)
	if err != nil {
		return nil, err
	}
	result.Username = username
	return result, nil
}

func (p *linuxProvisioner) Remove(username string, keys []string) (*RemoveResult, error) {
	u, uid, gid, err := lookupUser(username)
	if err != nil {
		return nil, err
	}

	result, err := removeAuthorizedKeysFile(filepath.Join(u.HomeDir, ".ssh"), keys, uid, gid)
	if err != nil {
		return nil, err
	}
	result.Username = username
	return result, nil
}

func (p *linuxProvisioner) List(username string) ([]string, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, fmt.Errorf("looking up user %q: %w", username, err)
	}
	return listAuthorizedKeysFile(filepath.Join(u.HomeDir, ".ssh"))
}

// listAuthorizedKeysFile reads sshDir/authorized_keys and returns its key
// identities. Split out from List so it can be exercised directly in tests
// against a temp directory, mirroring writeAuthorizedKeys/
// removeAuthorizedKeysFile.
func listAuthorizedKeysFile(sshDir string) ([]string, error) {
	path := filepath.Join(sshDir, "authorized_keys")
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}
	return listAuthorizedKeys(string(existing)), nil
}

// lookupUser resolves username to its numeric uid/gid.
func lookupUser(username string) (*user.User, int, int, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("looking up user %q: %w", username, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("parsing uid for %q: %w", username, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("parsing gid for %q: %w", username, err)
	}
	return u, uid, gid, nil
}

// writeAuthorizedKeys ensures sshDir exists (0700, chowned to uid/gid) and
// merges keys into sshDir/authorized_keys (0600, chowned to uid/gid),
// creating it if needed. Split out from Provision so it can be exercised
// directly in tests against a temp directory, without needing a real
// system user or root (chowning to the test process's own uid/gid is
// always permitted).
func writeAuthorizedKeys(sshDir string, keys []string, uid, gid int) (*Result, error) {
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return nil, fmt.Errorf("creating %q: %w", sshDir, err)
	}
	if err := os.Chown(sshDir, uid, gid); err != nil {
		return nil, fmt.Errorf("chowning %q: %w", sshDir, err)
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
		if err := os.Chown(path, uid, gid); err != nil {
			return nil, fmt.Errorf("chowning %q: %w", path, err)
		}
	}

	return &Result{Path: path, AddedKeys: added, TotalKeys: total}, nil
}

// removeAuthorizedKeysFile mirrors writeAuthorizedKeys: reads sshDir/
// authorized_keys, drops any matching keys, and only rewrites (+ chowns)
// the file if something was actually removed.
func removeAuthorizedKeysFile(sshDir string, keys []string, uid, gid int) (*RemoveResult, error) {
	path := filepath.Join(sshDir, "authorized_keys")
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &RemoveResult{Path: path}, nil
		}
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}

	merged, removed, total := removeAuthorizedKeys(string(existing), keys)

	if len(removed) > 0 {
		if err := os.WriteFile(path, []byte(merged), 0600); err != nil {
			return nil, fmt.Errorf("writing %q: %w", path, err)
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return nil, fmt.Errorf("chowning %q: %w", path, err)
		}
	}

	return &RemoveResult{Path: path, RemovedKeys: removed, TotalKeys: total}, nil
}

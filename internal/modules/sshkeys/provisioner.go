// Package sshkeys provisions SSH public keys supplied by the config-drive
// metadata into a user's authorized_keys file. Platform-specific
// implementations are selected at compile time via build tags, mirroring
// internal/modules/diskresize's Resizer pattern.
package sshkeys

import "strings"

// Provisioner ensures a set of SSH public keys are present in a user's
// authorized_keys file.
type Provisioner interface {
	// Provision appends any of keys not already present in username's
	// authorized_keys file. Existing keys are never removed or reordered —
	// this only ever adds what the config-drive supplies. A nil/empty keys
	// list is a no-op.
	Provision(username string, keys []string) (*Result, error)

	// Remove deletes any of keys currently present in username's
	// authorized_keys file, matched by key identity (type + base64 blob),
	// ignoring comments/options. Removing a key that isn't present is a
	// no-op, not an error. There is no protection against removing the
	// last key and locking someone out — the agent has no way to know
	// which key the caller depends on, and the NATS control channel this
	// runs over doesn't depend on SSH access anyway.
	Remove(username string, keys []string) (*RemoveResult, error)

	// List returns the key identities ("type base64blob") currently in
	// username's authorized_keys file, in file order. Malformed/
	// unrecognized lines are skipped, same as Provision's merge behavior.
	// Returns nil (not an error) if the file doesn't exist.
	List(username string) ([]string, error)
}

// Result reports the outcome of a Provision call.
type Result struct {
	Username  string   `json:"username"`
	Path      string   `json:"path"`
	AddedKeys []string `json:"added_keys,omitempty"`
	TotalKeys int      `json:"total_keys"`
}

// RemoveResult reports the outcome of a Remove call.
type RemoveResult struct {
	Username    string   `json:"username"`
	Path        string   `json:"path"`
	RemovedKeys []string `json:"removed_keys,omitempty"`
	TotalKeys   int      `json:"total_keys"`
}

// knownKeyTypes are the authorized_keys key-type tokens this package
// recognizes. A line must contain one of these as a whole field to be
// treated as a valid key — anything else is skipped rather than written
// verbatim, so malformed config-drive data can't corrupt authorized_keys.
var knownKeyTypes = map[string]bool{
	"ssh-ed25519":                        true,
	"ssh-rsa":                            true,
	"ssh-dss":                            true,
	"ecdsa-sha2-nistp256":                true,
	"ecdsa-sha2-nistp384":                true,
	"ecdsa-sha2-nistp521":                true,
	"sk-ssh-ed25519@openssh.com":         true,
	"sk-ecdsa-sha2-nistp256@openssh.com": true,
}

// keyIdentity extracts the "<type> <base64-blob>" portion of an
// authorized_keys line — the part that actually identifies the key,
// ignoring any leading options and trailing comment — and reports whether
// line looks like a recognized key at all.
func keyIdentity(line string) (identity string, ok bool) {
	fields := strings.Fields(line)
	for i, f := range fields {
		if knownKeyTypes[f] && i+1 < len(fields) {
			return f + " " + fields[i+1], true
		}
	}
	return "", false
}

// mergeAuthorizedKeys appends any of incoming not already present in
// existing (an authorized_keys file's current contents) and reports the
// keys that were newly added (as "type base64blob" identities) and the
// total number of valid keys in the result.
func mergeAuthorizedKeys(existing string, incoming []string) (merged string, added []string, total int) {
	have := make(map[string]bool)
	var lines []string

	for _, l := range strings.Split(existing, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
		if id, ok := keyIdentity(trimmed); ok {
			have[id] = true
		}
	}

	for _, k := range incoming {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		id, ok := keyIdentity(k)
		if !ok || have[id] {
			continue
		}
		have[id] = true
		lines = append(lines, k)
		added = append(added, id)
	}

	if len(lines) == 0 {
		return "", added, 0
	}
	return strings.Join(lines, "\n") + "\n", added, len(have)
}

// removeAuthorizedKeys drops any line from existing whose key identity
// matches one of toRemove (matched via keyIdentity, so comments/options on
// either side are ignored). Lines that don't parse as a recognized key
// (blank lines, comments, malformed lines) are always preserved untouched,
// mirroring mergeAuthorizedKeys' "never corrupt what's already there"
// stance. If every key is removed, merged is "" (an empty file is written,
// not deleted).
func removeAuthorizedKeys(existing string, toRemove []string) (merged string, removed []string, total int) {
	want := make(map[string]bool)
	for _, k := range toRemove {
		if id, ok := keyIdentity(k); ok {
			want[id] = true
		}
	}

	removedSet := make(map[string]bool)
	var lines []string
	have := 0
	for _, l := range strings.Split(existing, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if id, ok := keyIdentity(trimmed); ok {
			if want[id] {
				if !removedSet[id] {
					removed = append(removed, id)
					removedSet[id] = true
				}
				continue
			}
			have++
		}
		lines = append(lines, trimmed)
	}

	if len(lines) == 0 {
		return "", removed, 0
	}
	return strings.Join(lines, "\n") + "\n", removed, have
}

// listAuthorizedKeys extracts recognized key identities from existing file
// contents, in file order, skipping malformed/comment/blank lines.
func listAuthorizedKeys(existing string) []string {
	var keys []string
	for _, l := range strings.Split(existing, "\n") {
		trimmed := strings.TrimSpace(l)
		if id, ok := keyIdentity(trimmed); ok {
			keys = append(keys, id)
		}
	}
	return keys
}

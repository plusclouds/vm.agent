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
}

// Result reports the outcome of a Provision call.
type Result struct {
	Username  string   `json:"username"`
	Path      string   `json:"path"`
	AddedKeys []string `json:"added_keys,omitempty"`
	TotalKeys int      `json:"total_keys"`
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

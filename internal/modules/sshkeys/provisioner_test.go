package sshkeys

import (
	"reflect"
	"testing"
)

func TestKeyIdentity(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		wantID string
		wantOK bool
	}{
		{
			name:   "plain ed25519",
			line:   "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... deploy@example.com",
			wantID: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI...",
			wantOK: true,
		},
		{
			name:   "rsa with no comment",
			line:   "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAB...",
			wantID: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAB...",
			wantOK: true,
		},
		{
			name:   "with leading options",
			line:   `command="/usr/bin/foo" ssh-ed25519 AAAAC3abc== comment here`,
			wantID: "ssh-ed25519 AAAAC3abc==",
			wantOK: true,
		},
		{
			name:   "unrecognized garbage",
			line:   "not a key at all",
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
		{
			name:   "comment line",
			line:   "# just a comment",
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, ok := keyIdentity(c.line)
			if ok != c.wantOK {
				t.Fatalf("keyIdentity(%q) ok = %v, want %v", c.line, ok, c.wantOK)
			}
			if ok && id != c.wantID {
				t.Errorf("keyIdentity(%q) = %q, want %q", c.line, id, c.wantID)
			}
		})
	}
}

func TestMergeAuthorizedKeys_EmptyExisting_AddsAll(t *testing.T) {
	merged, added, total := mergeAuthorizedKeys("", []string{
		"ssh-ed25519 AAAAKEY1 alice@example.com",
		"ssh-rsa AAAAKEY2 bob@example.com",
	})

	wantMerged := "ssh-ed25519 AAAAKEY1 alice@example.com\nssh-rsa AAAAKEY2 bob@example.com\n"
	if merged != wantMerged {
		t.Errorf("merged = %q, want %q", merged, wantMerged)
	}
	if len(added) != 2 {
		t.Errorf("added = %v, want 2 entries", added)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
}

func TestMergeAuthorizedKeys_SkipsAlreadyPresent(t *testing.T) {
	existing := "ssh-ed25519 AAAAKEY1 alice@example.com\n"
	merged, added, total := mergeAuthorizedKeys(existing, []string{
		"ssh-ed25519 AAAAKEY1 alice-renamed@example.com", // same identity, different comment
		"ssh-rsa AAAAKEY2 bob@example.com",               // new
	})

	if len(added) != 1 || added[0] != "ssh-rsa AAAAKEY2" {
		t.Errorf("added = %v, want exactly [\"ssh-rsa AAAAKEY2\"]", added)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	wantMerged := "ssh-ed25519 AAAAKEY1 alice@example.com\nssh-rsa AAAAKEY2 bob@example.com\n"
	if merged != wantMerged {
		t.Errorf("merged = %q, want %q", merged, wantMerged)
	}
}

func TestMergeAuthorizedKeys_NeverRemovesExistingUnrecognizedLines(t *testing.T) {
	existing := "# a hand-written comment\nssh-ed25519 AAAAKEY1 alice@example.com\n"
	merged, _, _ := mergeAuthorizedKeys(existing, nil)

	if merged != "# a hand-written comment\nssh-ed25519 AAAAKEY1 alice@example.com\n" {
		t.Errorf("existing content should be preserved verbatim, got %q", merged)
	}
}

func TestMergeAuthorizedKeys_SkipsMalformedIncomingKeys(t *testing.T) {
	merged, added, total := mergeAuthorizedKeys("", []string{
		"not a valid key",
		"ssh-ed25519 AAAAKEY1 alice@example.com",
	})

	if !reflect.DeepEqual(added, []string{"ssh-ed25519 AAAAKEY1"}) {
		t.Errorf("added = %v, want only the valid key", added)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if merged != "ssh-ed25519 AAAAKEY1 alice@example.com\n" {
		t.Errorf("merged = %q, malformed line should not be written", merged)
	}
}

func TestMergeAuthorizedKeys_NoValidKeysAtAll_ReturnsEmpty(t *testing.T) {
	merged, added, total := mergeAuthorizedKeys("", []string{"garbage", ""})
	if merged != "" || added != nil || total != 0 {
		t.Errorf("got merged=%q added=%v total=%d, want all zero values", merged, added, total)
	}
}

func TestRemoveAuthorizedKeys_RemovesByIdentityIgnoringComment(t *testing.T) {
	existing := "ssh-ed25519 AAAAKEY1 alice@example.com\nssh-rsa AAAAKEY2 bob@example.com\n"
	merged, removed, total := removeAuthorizedKeys(existing, []string{
		"ssh-ed25519 AAAAKEY1 a-completely-different-comment",
	})

	if !reflect.DeepEqual(removed, []string{"ssh-ed25519 AAAAKEY1"}) {
		t.Errorf("removed = %v, want [\"ssh-ed25519 AAAAKEY1\"]", removed)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if merged != "ssh-rsa AAAAKEY2 bob@example.com\n" {
		t.Errorf("merged = %q", merged)
	}
}

func TestRemoveAuthorizedKeys_RemoveNonexistent_IsNoop(t *testing.T) {
	existing := "ssh-ed25519 AAAAKEY1 alice@example.com\n"
	merged, removed, total := removeAuthorizedKeys(existing, []string{"ssh-rsa AAAANOTPRESENT nobody"})

	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if merged != existing {
		t.Errorf("merged = %q, want unchanged %q", merged, existing)
	}
}

func TestRemoveAuthorizedKeys_RemoveAll_LeavesEmptyResult(t *testing.T) {
	existing := "ssh-ed25519 AAAAKEY1 alice@example.com\n"
	merged, removed, total := removeAuthorizedKeys(existing, []string{"ssh-ed25519 AAAAKEY1 alice@example.com"})

	if len(removed) != 1 {
		t.Errorf("removed = %v, want 1 entry", removed)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if merged != "" {
		t.Errorf("merged = %q, want empty", merged)
	}
}

func TestRemoveAuthorizedKeys_PreservesNonMatchingAndUnrecognizedLines(t *testing.T) {
	existing := "# a hand-written comment\nssh-ed25519 AAAAKEY1 alice@example.com\nssh-rsa AAAAKEY2 bob@example.com\n"
	merged, removed, total := removeAuthorizedKeys(existing, []string{"ssh-rsa AAAAKEY2 bob@example.com"})

	if !reflect.DeepEqual(removed, []string{"ssh-rsa AAAAKEY2"}) {
		t.Errorf("removed = %v", removed)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	want := "# a hand-written comment\nssh-ed25519 AAAAKEY1 alice@example.com\n"
	if merged != want {
		t.Errorf("merged = %q, want %q", merged, want)
	}
}

func TestRemoveAuthorizedKeys_MalformedRemovalRequestsIgnored(t *testing.T) {
	existing := "ssh-ed25519 AAAAKEY1 alice@example.com\n"
	merged, removed, total := removeAuthorizedKeys(existing, []string{"not a valid key", ""})

	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if merged != existing {
		t.Errorf("merged = %q, want unchanged %q", merged, existing)
	}
}

func TestListAuthorizedKeys(t *testing.T) {
	existing := "# a comment\nssh-ed25519 AAAAKEY1 alice@example.com\nnot a key\nssh-rsa AAAAKEY2 bob@example.com\n"
	got := listAuthorizedKeys(existing)
	want := []string{"ssh-ed25519 AAAAKEY1", "ssh-rsa AAAAKEY2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listAuthorizedKeys() = %v, want %v", got, want)
	}
}

func TestListAuthorizedKeys_Empty(t *testing.T) {
	if got := listAuthorizedKeys(""); got != nil {
		t.Errorf("listAuthorizedKeys(\"\") = %v, want nil", got)
	}
}

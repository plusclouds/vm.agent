//go:build linux

package sshkeys

import (
	"os"
	"path/filepath"
	"testing"
)

// writeAuthorizedKeys is exercised directly against a temp directory,
// chowning to the test process's own uid/gid (always permitted, unlike
// chowning to an arbitrary system user, which would need root) — this
// covers the real file-creation/merge/permission logic without touching
// any real user's home directory.

func TestWriteAuthorizedKeys_CreatesDirAndFile(t *testing.T) {
	base := t.TempDir()
	sshDir := filepath.Join(base, ".ssh")
	uid, gid := os.Getuid(), os.Getgid()

	result, err := writeAuthorizedKeys(sshDir, []string{"ssh-ed25519 AAAAKEY1 alice@example.com"}, uid, gid)
	if err != nil {
		t.Fatalf("writeAuthorizedKeys() error: %v", err)
	}

	info, err := os.Stat(sshDir)
	if err != nil {
		t.Fatalf("stat %q: %v", sshDir, err)
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf(".ssh perm = %o, want 0700", perm)
	}

	keysPath := filepath.Join(sshDir, "authorized_keys")
	fi, err := os.Stat(keysPath)
	if err != nil {
		t.Fatalf("stat %q: %v", keysPath, err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("authorized_keys perm = %o, want 0600", perm)
	}

	data, err := os.ReadFile(keysPath)
	if err != nil {
		t.Fatalf("reading authorized_keys: %v", err)
	}
	if string(data) != "ssh-ed25519 AAAAKEY1 alice@example.com\n" {
		t.Errorf("authorized_keys content = %q", string(data))
	}
	if result.Path != keysPath {
		t.Errorf("result.Path = %q, want %q", result.Path, keysPath)
	}
	if len(result.AddedKeys) != 1 || result.TotalKeys != 1 {
		t.Errorf("result = %+v, want 1 added, 1 total", result)
	}
}

func TestWriteAuthorizedKeys_IdempotentAcrossCalls(t *testing.T) {
	base := t.TempDir()
	sshDir := filepath.Join(base, ".ssh")
	uid, gid := os.Getuid(), os.Getgid()
	key := "ssh-ed25519 AAAAKEY1 alice@example.com"

	if _, err := writeAuthorizedKeys(sshDir, []string{key}, uid, gid); err != nil {
		t.Fatalf("first call: %v", err)
	}
	result, err := writeAuthorizedKeys(sshDir, []string{key}, uid, gid)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(result.AddedKeys) != 0 {
		t.Errorf("second call should add nothing, got %v", result.AddedKeys)
	}
	if result.TotalKeys != 1 {
		t.Errorf("total should stay 1, got %d", result.TotalKeys)
	}

	data, _ := os.ReadFile(filepath.Join(sshDir, "authorized_keys"))
	if string(data) != key+"\n" {
		t.Errorf("key should not be duplicated, got %q", string(data))
	}
}

func TestWriteAuthorizedKeys_PreservesHandWrittenEntries(t *testing.T) {
	base := t.TempDir()
	sshDir := filepath.Join(base, ".ssh")
	uid, gid := os.Getuid(), os.Getgid()

	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	handWritten := "ssh-rsa AAAAHANDWRITTEN admin@example.com\n"
	if err := os.WriteFile(filepath.Join(sshDir, "authorized_keys"), []byte(handWritten), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := writeAuthorizedKeys(sshDir, []string{"ssh-ed25519 AAAANEW alice@example.com"}, uid, gid)
	if err != nil {
		t.Fatalf("writeAuthorizedKeys() error: %v", err)
	}
	if len(result.AddedKeys) != 1 {
		t.Fatalf("expected exactly 1 new key added, got %v", result.AddedKeys)
	}
	if result.TotalKeys != 2 {
		t.Errorf("total = %d, want 2 (hand-written + new)", result.TotalKeys)
	}

	data, _ := os.ReadFile(filepath.Join(sshDir, "authorized_keys"))
	want := "ssh-rsa AAAAHANDWRITTEN admin@example.com\nssh-ed25519 AAAANEW alice@example.com\n"
	if string(data) != want {
		t.Errorf("authorized_keys = %q, want %q", string(data), want)
	}
}

func TestWriteAuthorizedKeys_NoNewKeys_DoesNotRewriteFile(t *testing.T) {
	base := t.TempDir()
	sshDir := filepath.Join(base, ".ssh")
	uid, gid := os.Getuid(), os.Getgid()
	key := "ssh-ed25519 AAAAKEY1 alice@example.com"

	if _, err := writeAuthorizedKeys(sshDir, []string{key}, uid, gid); err != nil {
		t.Fatalf("first call: %v", err)
	}
	keysPath := filepath.Join(sshDir, "authorized_keys")
	before, err := os.Stat(keysPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := writeAuthorizedKeys(sshDir, []string{key}, uid, gid); err != nil {
		t.Fatalf("second call: %v", err)
	}
	after, err := os.Stat(keysPath)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("file was rewritten even though no new keys were added")
	}
}

func TestRemoveAuthorizedKeysFile_RemovesKeyAndRewrites(t *testing.T) {
	base := t.TempDir()
	sshDir := filepath.Join(base, ".ssh")
	uid, gid := os.Getuid(), os.Getgid()
	key1 := "ssh-ed25519 AAAAKEY1 alice@example.com"
	key2 := "ssh-rsa AAAAKEY2 bob@example.com"

	if _, err := writeAuthorizedKeys(sshDir, []string{key1, key2}, uid, gid); err != nil {
		t.Fatalf("seeding keys: %v", err)
	}

	result, err := removeAuthorizedKeysFile(sshDir, []string{key1}, uid, gid)
	if err != nil {
		t.Fatalf("removeAuthorizedKeysFile() error: %v", err)
	}
	if len(result.RemovedKeys) != 1 || result.TotalKeys != 1 {
		t.Errorf("result = %+v, want 1 removed, 1 remaining", result)
	}

	data, _ := os.ReadFile(filepath.Join(sshDir, "authorized_keys"))
	if string(data) != key2+"\n" {
		t.Errorf("authorized_keys = %q, want only key2", string(data))
	}
}

func TestRemoveAuthorizedKeysFile_NonexistentFile_IsNoop(t *testing.T) {
	base := t.TempDir()
	sshDir := filepath.Join(base, ".ssh")
	uid, gid := os.Getuid(), os.Getgid()

	result, err := removeAuthorizedKeysFile(sshDir, []string{"ssh-ed25519 AAAAKEY1 alice"}, uid, gid)
	if err != nil {
		t.Fatalf("removeAuthorizedKeysFile() error: %v", err)
	}
	if len(result.RemovedKeys) != 0 {
		t.Errorf("removed = %v, want none", result.RemovedKeys)
	}
}

func TestRemoveAuthorizedKeysFile_RemoveAll_LeavesEmptyFileNotDeleted(t *testing.T) {
	base := t.TempDir()
	sshDir := filepath.Join(base, ".ssh")
	uid, gid := os.Getuid(), os.Getgid()
	key := "ssh-ed25519 AAAAKEY1 alice@example.com"

	if _, err := writeAuthorizedKeys(sshDir, []string{key}, uid, gid); err != nil {
		t.Fatalf("seeding key: %v", err)
	}
	if _, err := removeAuthorizedKeysFile(sshDir, []string{key}, uid, gid); err != nil {
		t.Fatalf("removeAuthorizedKeysFile() error: %v", err)
	}

	path := filepath.Join(sshDir, "authorized_keys")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected authorized_keys to still exist (empty), got: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("expected empty file, size = %d", info.Size())
	}
}

func TestListAuthorizedKeysFile(t *testing.T) {
	base := t.TempDir()
	sshDir := filepath.Join(base, ".ssh")
	uid, gid := os.Getuid(), os.Getgid()
	key1 := "ssh-ed25519 AAAAKEY1 alice@example.com"
	key2 := "ssh-rsa AAAAKEY2 bob@example.com"

	if _, err := writeAuthorizedKeys(sshDir, []string{key1, key2}, uid, gid); err != nil {
		t.Fatalf("seeding keys: %v", err)
	}

	keys, err := listAuthorizedKeysFile(sshDir)
	if err != nil {
		t.Fatalf("listAuthorizedKeysFile() error: %v", err)
	}
	want := []string{"ssh-ed25519 AAAAKEY1", "ssh-rsa AAAAKEY2"}
	if len(keys) != len(want) || keys[0] != want[0] || keys[1] != want[1] {
		t.Errorf("listAuthorizedKeysFile() = %v, want %v", keys, want)
	}
}

func TestListAuthorizedKeysFile_NonexistentFile_ReturnsNilNoError(t *testing.T) {
	base := t.TempDir()
	sshDir := filepath.Join(base, ".ssh")

	keys, err := listAuthorizedKeysFile(sshDir)
	if err != nil {
		t.Fatalf("listAuthorizedKeysFile() error: %v", err)
	}
	if keys != nil {
		t.Errorf("keys = %v, want nil", keys)
	}
}

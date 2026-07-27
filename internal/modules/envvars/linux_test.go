//go:build linux

package envvars

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func newTestManager(path string) *linuxManager {
	return &linuxManager{path: path, logger: zap.NewNop()}
}

func TestSet_CreatesFileWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment")
	m := newTestManager(path)

	if _, err := m.Set(context.Background(), "FOO", "bar"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(data) != `FOO="bar"`+"\n" {
		t.Errorf("file content = %q", string(data))
	}
}

func TestSet_OverwritesExistingKeyPreservesOthers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment")
	m := newTestManager(path)

	if _, err := m.Set(context.Background(), "FOO", "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Set(context.Background(), "BAR", "two"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Set(context.Background(), "FOO", "updated"); err != nil {
		t.Fatal(err)
	}

	got, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["FOO"] != "updated" || got["BAR"] != "two" {
		t.Errorf("List() = %v, want FOO=updated BAR=two", got)
	}
}

func TestSet_PreservesCommentsAndUnrelatedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment")
	if err := os.WriteFile(path, []byte("# a comment\nPATH=/usr/bin\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := newTestManager(path)

	if _, err := m.Set(context.Background(), "FOO", "bar"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	want := "# a comment\nPATH=/usr/bin\nFOO=\"bar\"\n"
	if string(data) != want {
		t.Errorf("file content = %q, want %q", string(data), want)
	}
}

func TestSet_Idempotent_NoRewriteWhenValueUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment")
	m := newTestManager(path)

	if _, err := m.Set(context.Background(), "FOO", "bar"); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	result, err := m.Set(context.Background(), "FOO", "bar")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("file was rewritten even though value did not change")
	}
	if result.Message == "environment variable set" {
		t.Errorf("expected a no-change message, got %q", result.Message)
	}
}

func TestUnset_RemovesKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment")
	m := newTestManager(path)

	if _, err := m.Set(context.Background(), "FOO", "bar"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Unset(context.Background(), "FOO"); err != nil {
		t.Fatal(err)
	}

	got, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["FOO"]; ok {
		t.Error("FOO should have been removed")
	}
}

func TestUnset_Nonexistent_IsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment")
	m := newTestManager(path)

	result, err := m.Unset(context.Background(), "NEVER_SET")
	if err != nil {
		t.Fatalf("Unset() error: %v", err)
	}
	if result.Message != "already unset" {
		t.Errorf("Message = %q, want %q", result.Message, "already unset")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected no file to be created for unsetting a nonexistent key")
	}
}

func TestSet_QuotedAndUnquotedParsingOnRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment")
	if err := os.WriteFile(path, []byte("FOO=bar\nBAZ=\"qux\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := newTestManager(path)

	got, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["FOO"] != "bar" || got["BAZ"] != "qux" {
		t.Errorf("List() = %v, want FOO=bar BAZ=qux", got)
	}
}

func TestValidateKey_RejectsInvalidNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment")
	m := newTestManager(path)

	for _, bad := range []string{"", "1FOO", "FOO-BAR", "FOO BAR", "FOO=BAR"} {
		if _, err := m.Set(context.Background(), bad, "x"); err == nil {
			t.Errorf("Set(%q) should have been rejected", bad)
		}
	}
}

func TestList_NonexistentFile_ReturnsEmptyMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment")
	m := newTestManager(path)

	got, err := m.List(context.Background())
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List() = %v, want empty", got)
	}
}

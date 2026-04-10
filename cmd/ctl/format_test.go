package main

import (
	"testing"
)

// --- formatBytes ---

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		input uint64
		want  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TB"},
	}

	for _, c := range cases {
		got := formatBytes(c.input)
		if got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.input, got, c.want)
		}
	}
}

// --- formatSeconds ---

func TestFormatSeconds(t *testing.T) {
	cases := []struct {
		input int64
		want  string
	}{
		{0, "0h 0m"},
		{60, "0h 1m"},
		{3600, "1h 0m"},
		{3661, "1h 1m"},
		{86400, "1d 0h 0m"},
		{90061, "1d 1h 1m"},
		{172800, "2d 0h 0m"},
	}

	for _, c := range cases {
		got := formatSeconds(c.input)
		if got != c.want {
			t.Errorf("formatSeconds(%d) = %q, want %q", c.input, got, c.want)
		}
	}
}

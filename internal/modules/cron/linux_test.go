//go:build linux

package cron

import "testing"

func TestUsecToUnixSeconds(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want *int64
	}{
		{"zero means unset", uint64(0), nil},
		{"usec infinity sentinel means never scheduled", usecInfinity, nil},
		{"wrong type", "not a uint64", nil},
		{"nil", nil, nil},
		{"one second", uint64(1_000_000), int64Ptr(1)},
		{"real timestamp", uint64(1_800_000_000_000_000), int64Ptr(1_800_000_000_000_000 / 1_000_000)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := usecToUnixSeconds(c.in)
			if (got == nil) != (c.want == nil) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			if got != nil && *got != *c.want {
				t.Errorf("got %d, want %d", *got, *c.want)
			}
		})
	}
}

func int64Ptr(v int64) *int64 { return &v }

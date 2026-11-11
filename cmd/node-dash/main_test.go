package main

import (
	"strings"
	"testing"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/websettings"
)

func TestAppName(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "Node",
		"   ":              "Node",
		" Satoshi’s node ": "Satoshi’s node",
		strings.Repeat("ä", websettings.MaxNameLen): strings.Repeat("ä", websettings.MaxNameLen),
	} {
		if got, err := appName(in); err != nil || got != want {
			t.Errorf("appName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{strings.Repeat("x", websettings.MaxNameLen+1), "two\nlines", "bad \xff"} {
		if got, err := appName(in); err == nil {
			t.Errorf("appName(%q) = %q, want an error", in, got)
		}
	}
}

func TestMockDashboard(t *testing.T) {
	for _, tc := range []struct{ scenario, version, wantVersion, wantLatest string }{
		{"outdated", "dev", "v0.5.0", "v0.6.0"},
		{"outdated", "v0.7.2", "v0.7.2", "v0.8.0"},
		{"healthy", "dev", "v0.5.0", "v0.5.0"},
	} {
		v, latest := mockDashboard(tc.scenario, tc.version)
		r, _ := latest.Latest()
		if v != tc.wantVersion || r.Tag() != tc.wantLatest {
			t.Errorf("%s %s: got %s, latest %s; want %s, %s", tc.scenario, tc.version, v, r.Tag(), tc.wantVersion, tc.wantLatest)
		}
	}
}

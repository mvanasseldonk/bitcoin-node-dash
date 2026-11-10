package main

import (
	"strings"
	"testing"
)

func TestAppName(t *testing.T) {
	for in, want := range map[string]string{
		"":                              "Node",
		"   ":                           "Node",
		" Satoshi’s node ":              "Satoshi’s node",
		strings.Repeat("ä", maxNameLen): strings.Repeat("ä", maxNameLen),
	} {
		if got, err := appName(in); err != nil || got != want {
			t.Errorf("appName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{strings.Repeat("x", maxNameLen+1), "two\nlines", "bad \xff"} {
		if got, err := appName(in); err == nil {
			t.Errorf("appName(%q) = %q, want an error", in, got)
		}
	}
}

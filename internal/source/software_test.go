package source

import "testing"

func TestParseSoftware(t *testing.T) {
	for _, tc := range []struct {
		version    int
		subversion string
		label      string
		core       bool
	}{
		{320100, "/Satoshi:32.1.0/", "Bitcoin Core 32.1", true},
		{300000, "/Satoshi:30.0.0/", "Bitcoin Core 30.0", true},
		{290201, "/Satoshi:29.2.1(my node)/", "Bitcoin Core 29.2.1", true},
		{280100, "/Satoshi:28.1.0/Knots:20250305/", "Bitcoin Knots 28.1.knots20250305", false},
		{240001, "/btcwire:0.5.0/btcd:0.24.2/", "btcwire:0.5.0/btcd:0.24.2", false},
	} {
		s := ParseSoftware(tc.version, tc.subversion)
		if s.Label != tc.label || s.IsCore() != tc.core {
			t.Errorf("ParseSoftware(%d, %q) = %q (core %v), want %q (core %v)", tc.version, tc.subversion, s.Label, s.IsCore(), tc.label, tc.core)
		}
	}
	if s := ParseSoftware(320100, "/Satoshi:32.1.0/"); s.Major != 32 || s.Minor != 1 || s.Patch != 0 {
		t.Errorf("version parts = %d.%d.%d, want 32.1.0", s.Major, s.Minor, s.Patch)
	}
}

package minisign

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// testdata was made with minisign 0.12: key a signs legacy.minisig and
// prehashed.minisig, key b signs otherkey.minisig.
func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func key(t *testing.T, name string) PublicKey {
	t.Helper()
	k, err := ParsePublicKey(string(read(t, name)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestVerify(t *testing.T) {
	a := key(t, "a.pub")
	if a.ID() != "AE7E94AA10573294" {
		t.Fatalf("key ID = %s", a.ID())
	}
	msg := read(t, "checksums.txt")
	trusted, err := Verify(a, msg, read(t, "legacy.minisig"))
	if err != nil {
		t.Fatal(err)
	}
	if trusted != "node-dash v0.7.0" {
		t.Fatalf("trusted comment = %q", trusted)
	}
}

func TestVerifyRejects(t *testing.T) {
	a := key(t, "a.pub")
	msg := read(t, "checksums.txt")
	sig := string(read(t, "legacy.minisig"))
	lines := strings.Split(sig, "\n")

	tampered := append([]byte{}, msg...)
	tampered[0] ^= 1
	changedComment := strings.Replace(sig, "trusted comment: node-dash v0.7.0", "trusted comment: node-dash v9.9.9", 1)

	for _, tc := range []struct {
		name string
		msg  []byte
		sig  string
		want error
	}{
		{"a tampered file", tampered, sig, ErrSignature},
		{"a changed trusted comment", msg, changedComment, ErrComment},
		{"another key", msg, string(read(t, "otherkey.minisig")), ErrKey},
		{"the prehashed format", msg, string(read(t, "prehashed.minisig")), ErrPrehashed},
		{"an empty file", msg, "", ErrFormat},
		{"a missing line", msg, strings.Join(lines[:3], "\n"), ErrFormat},
		{"garbage", msg, "untrusted comment: x\nnot base64\ntrusted comment: y\nzzz\n", ErrFormat},
	} {
		if _, err := Verify(a, tc.msg, []byte(tc.sig)); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestParsePublicKey(t *testing.T) {
	for _, s := range []string{"", "RWQ=", "untrusted comment: x\nnot a key"} {
		if _, err := ParsePublicKey(s); err == nil {
			t.Errorf("ParsePublicKey(%q): want an error", s)
		}
	}
}

// The key in the binary is the one in minisign.pub, which people use to
// check a release by hand.
func TestReleaseKeyMatchesRepository(t *testing.T) {
	pub, err := os.ReadFile("../../minisign.pub")
	if err != nil {
		t.Fatal(err)
	}
	k, err := ParsePublicKey(string(pub))
	if err != nil {
		t.Fatal(err)
	}
	if k.ID() != ReleaseKey().ID() || !k.key.Equal(ReleaseKey().key) {
		t.Fatalf("minisign.pub has key %s, the binary %s", k.ID(), ReleaseKey().ID())
	}
	if !strings.Contains(string(pub), k.ID()) {
		t.Fatalf("minisign.pub's comment doesn't name key %s", k.ID())
	}
}

func TestVerifyRelease(t *testing.T) {
	a := key(t, "a.pub")
	msg, sig := read(t, "checksums.txt"), read(t, "legacy.minisig")
	if err := VerifyRelease(a, "v0.7.0", msg, sig); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRelease(a, "v0.8.0", msg, sig); err == nil {
		t.Fatal("a signature for v0.7.0 must not pass for v0.8.0")
	}
}

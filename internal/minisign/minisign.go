// Package minisign verifies minisign signatures (https://jedisct1.github.io/minisign/)
// with Ed25519 from the standard library, so a server needs no minisign
// package to check a release (story #50).
//
// Only the legacy format is accepted: a signature over the file itself
// ("Ed"). The default format of minisign 0.10 and newer signs a BLAKE2b
// hash of the file ("ED"), which the standard library can't compute; releases
// are signed with minisign -l.
package minisign

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// PublicKey is a minisign public key.
type PublicKey struct {
	id  [8]byte
	key ed25519.PublicKey
}

// ID is the key ID as minisign prints it, e.g. "241DA55A465F44FF".
func (k PublicKey) ID() string { return keyID(k.id) }

// keyID formats a key number the way minisign prints it: little-endian.
func keyID(n [8]byte) string {
	var b [8]byte
	for i := range n {
		b[i] = n[7-i]
	}
	return strings.ToUpper(hex.EncodeToString(b[:]))
}

// ParsePublicKey parses a public key: its base64 line, or the whole
// minisign.pub file with its comment line.
func ParsePublicKey(s string) (PublicKey, error) {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[len(lines)-1]))
	if err != nil || len(raw) != 2+8+ed25519.PublicKeySize || string(raw[:2]) != "Ed" {
		return PublicKey{}, errors.New("not a minisign public key")
	}
	var k PublicKey
	copy(k.id[:], raw[2:10])
	k.key = ed25519.PublicKey(raw[10:])
	return k, nil
}

// Errors from Verify.
var (
	ErrFormat    = errors.New("not a minisign signature file")
	ErrPrehashed = errors.New("signed in the prehashed format; sign with minisign -l")
	ErrKey       = errors.New("signed with another key")
	ErrSignature = errors.New("the signature doesn't match the file")
	ErrComment   = errors.New("the trusted comment was changed")
)

// Verify checks sigFile (the contents of a .minisig file) for msg against
// key, and returns the trusted comment, which the signature also covers.
func Verify(key PublicKey, msg, sigFile []byte) (trusted string, err error) {
	// Four lines: untrusted comment, signature, trusted comment, global signature.
	lines := strings.Split(strings.TrimRight(string(sigFile), "\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "untrusted comment:") {
		return "", ErrFormat
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(sig) != 2+8+ed25519.SignatureSize {
		return "", ErrFormat
	}
	trusted, ok := strings.CutPrefix(strings.TrimRight(lines[2], "\r"), "trusted comment: ")
	if !ok {
		return "", ErrFormat
	}
	global, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || len(global) != ed25519.SignatureSize {
		return "", ErrFormat
	}

	switch string(sig[:2]) {
	case "Ed":
	case "ED":
		return "", ErrPrehashed
	default:
		return "", ErrFormat
	}
	var id [8]byte
	copy(id[:], sig[2:10])
	if id != key.id {
		return "", fmt.Errorf("%w (key %s, expected %s)", ErrKey, keyID(id), key.ID())
	}
	sig = sig[10:]
	if !ed25519.Verify(key.key, msg, sig) {
		return "", ErrSignature
	}
	// The global signature covers the signature and the trusted comment.
	if !ed25519.Verify(key.key, bytes.Join([][]byte{sig, []byte(trusted)}, nil), global) {
		return "", ErrComment
	}
	return trusted, nil
}

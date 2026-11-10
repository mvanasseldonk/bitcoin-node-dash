package minisign

import "fmt"

// releaseKey signs node-dash releases: the second line of minisign.pub at the
// repository root. The CI test of the updater builds with a key of its own:
// -ldflags "-X github.com/mvanasseldonk/bitcoin-node-dash/internal/minisign.releaseKey=…".
var releaseKey = "RWT/RF9GWqUdJBDwNWdvqdV0U+5eFTghUZVmWognxJTlffqeKREbSObV"

// ReleaseKey returns the public key that signs node-dash releases.
func ReleaseKey() PublicKey {
	k, err := ParsePublicKey(releaseKey)
	if err != nil {
		panic(fmt.Sprintf("minisign: bad release key %q: %v", releaseKey, err))
	}
	return k
}

// VerifyRelease checks a release's checksums.txt against its signature,
// made with key (ReleaseKey for real releases), and that the signature is
// for that release: the trusted comment must be "node-dash <tag>". This stops
// an old, validly signed checksums.txt from passing for a newer release.
func VerifyRelease(key PublicKey, tag string, checksums, sigFile []byte) error {
	trusted, err := Verify(key, checksums, sigFile)
	if err != nil {
		return err
	}
	if want := "node-dash " + tag; trusted != want {
		return fmt.Errorf("signed for %q, not %q", trusted, want)
	}
	return nil
}

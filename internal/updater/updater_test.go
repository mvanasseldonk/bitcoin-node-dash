package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/minisign"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// signer makes minisign signatures in the legacy format, like minisign -l.
type signer struct {
	id   [8]byte
	priv ed25519.PrivateKey
}

func newSigner(t *testing.T, id byte) (signer, minisign.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := signer{id: [8]byte{id, 1, 2, 3, 4, 5, 6, 7}, priv: priv}
	k, err := minisign.ParsePublicKey(base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), s.id[:]...), pub...)))
	if err != nil {
		t.Fatal(err)
	}
	return s, k
}

func (s signer) sign(msg []byte, trusted string) []byte {
	sig := ed25519.Sign(s.priv, msg)
	global := ed25519.Sign(s.priv, append(append([]byte{}, sig...), trusted...))
	return []byte(fmt.Sprintf("untrusted comment: test\n%s\ntrusted comment: %s\n%s\n",
		base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), s.id[:]...), sig...)),
		trusted, base64.StdEncoding.EncodeToString(global)))
}

// fakeRelease is a release server: the latest-release API and the files.
type fakeRelease struct {
	latest string
	files  map[string][]byte // path under the downloads, e.g. "v0.8.0/checksums.txt"
}

func (f *fakeRelease) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/latest" {
		fmt.Fprintf(w, `{"tag_name":%q}`, f.latest)
		return
	}
	b, ok := f.files[strings.TrimPrefix(r.URL.Path, "/download/")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Write(b)
}

func tarball(t *testing.T, path, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: path, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	io.WriteString(tw, content)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// publish adds a signed release with a binary whose content is "binary <tag>".
func (f *fakeRelease) publish(t *testing.T, s signer, tag string) {
	t.Helper()
	name := fmt.Sprintf("node-dash_%s_linux_amd64", tag)
	archive := tarball(t, name+"/node-dash", "binary "+tag)
	sum := sha256.Sum256(archive)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + name + ".tar.gz\n")
	f.files[tag+"/"+name+".tar.gz"] = archive
	f.files[tag+"/checksums.txt"] = sums
	f.files[tag+"/checksums.txt.minisig"] = s.sign(sums, "node-dash "+tag)
	f.latest = tag
}

// host is a fake server: the installed binary, systemctl and the dashboard.
type host struct {
	t        *testing.T
	cfg      Config
	restarts int
	// healthy tells whether a version answers after a restart.
	healthy func(version string) bool
}

func newHost(t *testing.T, key minisign.PublicKey, rel *fakeRelease, installed string) *host {
	t.Helper()
	srv := httptest.NewServer(rel)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	h := &host{t: t, healthy: func(string) bool { return true }}
	bin := filepath.Join(dir, "bin", "node-dash")
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("binary "+installed), 0o755)
	h.cfg = Config{
		Version:       installed,
		API:           srv.URL + "/api/latest",
		Downloads:     srv.URL + "/download",
		Arch:          "amd64",
		Key:           key,
		Binary:        bin,
		StateDir:      filepath.Join(dir, "state"),
		Service:       "node-dash.service",
		HealthTimeout: 50 * time.Millisecond,
		Client:        srv.Client(),
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:           func() time.Time { return time.Date(2026, 11, 10, 3, 12, 0, 0, time.UTC) },
		Systemctl: func(_ context.Context, args ...string) error {
			if args[0] == "restart" {
				h.restarts++
			}
			return nil
		},
		RunVersion: func(_ context.Context, binary string) (string, error) {
			b, err := os.ReadFile(binary)
			return strings.TrimPrefix(string(b), "binary "), err
		},
		Running: func(_ context.Context, tag string) error {
			if h.installed() != tag || !h.healthy(tag) {
				return errors.New("not answering")
			}
			return nil
		},
	}
	return h
}

func (h *host) installed() string {
	b, err := os.ReadFile(h.cfg.Binary)
	if err != nil {
		h.t.Fatal(err)
	}
	return strings.TrimPrefix(string(b), "binary ")
}

func (h *host) run() model.AutoUpdate {
	st := Run(context.Background(), h.cfg)
	// The status file holds the same.
	got := ReadStatus(filepath.Join(h.cfg.StateDir, StatusFile))
	if got == nil || *got != st {
		h.t.Fatalf("status file = %+v, want %+v", got, st)
	}
	return st
}

func TestUpdates(t *testing.T) {
	s, key := newSigner(t, 1)
	rel := &fakeRelease{files: map[string][]byte{}}
	rel.publish(t, s, "v0.8.0")
	h := newHost(t, key, rel, "v0.7.0")

	st := h.run()
	if st.Result != model.UpdateUpdated || st.From != "v0.7.0" || st.To != "v0.8.0" || st.Message != "" {
		t.Fatalf("got %+v", st)
	}
	if h.installed() != "v0.8.0" || h.restarts != 1 {
		t.Fatalf("installed %s after %d restarts", h.installed(), h.restarts)
	}
	if !st.LastRun.Equal(h.cfg.Now()) {
		t.Fatalf("last run = %v", st.LastRun)
	}
	prev, _ := os.ReadFile(filepath.Join(h.cfg.StateDir, previousFile))
	if string(prev) != "binary v0.7.0" {
		t.Fatalf("previous binary = %q", prev)
	}

	// The next day: nothing newer.
	h.cfg.Version = "v0.8.0"
	if st := h.run(); st.Result != model.UpdateCurrent || h.restarts != 1 {
		t.Fatalf("got %+v after %d restarts", st, h.restarts)
	}
}

func TestRollsBack(t *testing.T) {
	s, key := newSigner(t, 1)
	rel := &fakeRelease{files: map[string][]byte{}}
	rel.publish(t, s, "v0.8.0")
	h := newHost(t, key, rel, "v0.7.0")
	h.healthy = func(v string) bool { return v != "v0.8.0" }

	st := h.run()
	if st.Result != model.UpdateFailed || !strings.Contains(st.Message, "v0.8.0 didn't start") || !strings.Contains(st.Message, "back on v0.7.0") {
		t.Fatalf("got %+v", st)
	}
	if h.installed() != "v0.7.0" || h.restarts != 2 {
		t.Fatalf("installed %s after %d restarts, want v0.7.0 back", h.installed(), h.restarts)
	}
}

func TestRefuses(t *testing.T) {
	s, key := newSigner(t, 1)
	other, _ := newSigner(t, 2)
	for _, tc := range []struct {
		name   string
		break_ func(rel *fakeRelease)
		reason string
	}{
		{"unsigned", func(rel *fakeRelease) { delete(rel.files, "v0.8.0/checksums.txt.minisig") }, "isn't signed"},
		{"signed with another key", func(rel *fakeRelease) {
			rel.files["v0.8.0/checksums.txt.minisig"] = other.sign(rel.files["v0.8.0/checksums.txt"], "node-dash v0.8.0")
		}, "another key"},
		{"an old signature for another release", func(rel *fakeRelease) {
			rel.files["v0.8.0/checksums.txt.minisig"] = s.sign(rel.files["v0.8.0/checksums.txt"], "node-dash v0.7.0")
		}, `signed for "node-dash v0.7.0"`},
		{"a changed checksums.txt", func(rel *fakeRelease) {
			rel.files["v0.8.0/checksums.txt"] = append(rel.files["v0.8.0/checksums.txt"], "# extra\n"...)
		}, "doesn't match the file"},
		{"a changed archive", func(rel *fakeRelease) {
			rel.files["v0.8.0/node-dash_v0.8.0_linux_amd64.tar.gz"] = tarball(t, "node-dash_v0.8.0_linux_amd64/node-dash", "binary evil")
		}, "doesn't match checksums.txt"},
		{"a binary of another version", func(rel *fakeRelease) {
			archive := tarball(t, "node-dash_v0.8.0_linux_amd64/node-dash", "binary v0.9.9")
			sum := sha256.Sum256(archive)
			sums := []byte(hex.EncodeToString(sum[:]) + "  node-dash_v0.8.0_linux_amd64.tar.gz\n")
			rel.files["v0.8.0/node-dash_v0.8.0_linux_amd64.tar.gz"] = archive
			rel.files["v0.8.0/checksums.txt"] = sums
			rel.files["v0.8.0/checksums.txt.minisig"] = s.sign(sums, "node-dash v0.8.0")
		}, `says it is "v0.9.9"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rel := &fakeRelease{files: map[string][]byte{}}
			rel.publish(t, s, "v0.8.0")
			tc.break_(rel)
			h := newHost(t, key, rel, "v0.7.0")
			st := h.run()
			if st.Result != model.UpdateRefused || !strings.Contains(st.Message, tc.reason) {
				t.Fatalf("got %+v, want refused: %s", st, tc.reason)
			}
			if h.installed() != "v0.7.0" || h.restarts != 0 {
				t.Fatalf("installed %s after %d restarts, want v0.7.0 untouched", h.installed(), h.restarts)
			}
		})
	}
}

func TestSkips(t *testing.T) {
	s, key := newSigner(t, 1)
	rel := &fakeRelease{files: map[string][]byte{}}
	rel.publish(t, s, "v1.0.0")

	h := newHost(t, key, rel, "v0.7.0")
	if st := h.run(); st.Result != model.UpdateSkipped || st.To != "v1.0.0" || !strings.Contains(st.Message, "major version") {
		t.Fatalf("major version: got %+v", st)
	}
	h = newHost(t, key, rel, "dev")
	if st := h.run(); st.Result != model.UpdateSkipped || !strings.Contains(st.Message, "local build") {
		t.Fatalf("local build: got %+v", st)
	}
	if h.installed() != "dev" || h.restarts != 0 {
		t.Fatal("a skipped update must not touch anything")
	}
}

func TestLookupFails(t *testing.T) {
	_, key := newSigner(t, 1)
	h := newHost(t, key, &fakeRelease{latest: "garbage", files: map[string][]byte{}}, "v0.7.0")
	if st := h.run(); st.Result != model.UpdateFailed || !strings.Contains(st.Message, "can't look up") {
		t.Fatalf("got %+v", st)
	}
}

func TestReadStatus(t *testing.T) {
	dir := t.TempDir()
	if ReadStatus(filepath.Join(dir, "missing.json")) != nil {
		t.Fatal("no file: want nil")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{"), 0o644)
	if ReadStatus(bad) != nil {
		t.Fatal("broken file: want nil")
	}
}

func TestHealthHost(t *testing.T) {
	for listen, want := range map[string]string{
		":8080":              "127.0.0.1:8080",
		"0.0.0.0:9000":       "127.0.0.1:9000",
		"[::]:8080":          "[::1]:8080",
		"192.168.1.119:8080": "192.168.1.119:8080",
		"nonsense":           "127.0.0.1:8080",
	} {
		if got := HealthHost(listen); got != want {
			t.Errorf("HealthHost(%q) = %s, want %s", listen, got, want)
		}
	}
}

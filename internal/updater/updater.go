// Package updater installs new node-dash releases by itself (story #50).
//
// It runs as root, once a day, from node-dash-update.service: the command
// "node-dash update" of the binary that is installed. The dashboard itself
// can't update: its service may not write /usr/local/bin and can't gain
// privileges. The updater:
//
//  1. asks GitHub for the latest release, and stops unless it is newer and
//     of the same major version, or the step from 0.x to 1.x (story #74);
//  2. downloads checksums.txt, its minisign signature and the archive, and
//     checks the signature with the key built into this binary, never the new
//     one, and the archive against checksums.txt;
//  3. keeps the installed binary, puts the new one in place and restarts
//     node-dash.service;
//  4. puts the old binary back when the new version doesn't answer within
//     30 seconds;
//  5. writes the result to a status file, which the dashboard shows.
package updater

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/minisign"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/release"
)

// Where the updater works on a real server.
const (
	DefaultBinary    = "/usr/local/bin/node-dash"
	DefaultStateDir  = "/var/lib/node-dash" // StateDirectory= of node-dash-update.service
	DefaultService   = "node-dash.service"
	DefaultDownloads = "https://github.com/mvanasseldonk/bitcoin-node-dash/releases/download"

	// StatusFile is the name of the status file in the state directory.
	StatusFile = "update.json"
	// previousFile keeps the binary that ran before the last update.
	previousFile = "node-dash.previous"

	healthTimeout = 30 * time.Second
	maxArchive    = 100 << 20 // a release archive is about 5 MB
	maxSmallFile  = 64 << 10  // checksums.txt and its signature
)

// Config says what to update and how. Defaults fills it in for a server.
type Config struct {
	Version   string // the installed version, e.g. "v0.7.0"
	API       string // GitHub's latest-release API
	Downloads string // release files are at <Downloads>/<tag>/<file>
	Arch      string // amd64 or arm64
	Key       minisign.PublicKey

	Binary   string // the installed binary
	StateDir string // the status file and the previous binary
	Service  string // the dashboard's systemd service

	HealthTimeout time.Duration
	Client        *http.Client
	Log           *slog.Logger
	Now           func() time.Time

	// Systemctl runs systemctl. RunVersion runs a binary's version command.
	// Running checks that the dashboard answers as version tag.
	Systemctl  func(ctx context.Context, args ...string) error
	RunVersion func(ctx context.Context, binary string) (string, error)
	Running    func(ctx context.Context, tag string) error
}

// Defaults returns the configuration for this server. NODEDASH_UPDATE_API
// and NODEDASH_UPDATE_DOWNLOADS point it at another release server, for the
// CI test; a release must still be signed with the key in this binary.
func Defaults(version string, log *slog.Logger) Config {
	c := Config{
		Version:       version,
		API:           envOr("NODEDASH_UPDATE_API", release.DashboardLatestURL),
		Downloads:     envOr("NODEDASH_UPDATE_DOWNLOADS", DefaultDownloads),
		Arch:          runtime.GOARCH,
		Key:           minisign.ReleaseKey(),
		Binary:        DefaultBinary,
		StateDir:      DefaultStateDir,
		Service:       DefaultService,
		HealthTimeout: healthTimeout,
		Client:        &http.Client{Timeout: 2 * time.Minute},
		Log:           log,
		Now:           time.Now,
	}
	c.Systemctl = systemctl
	c.RunVersion = runVersion
	c.Running = func(ctx context.Context, tag string) error { return running(ctx, c.Client, c.Service, tag) }
	return c
}

// Run updates once, writes the result to the status file and returns it.
func Run(ctx context.Context, c Config) model.AutoUpdate {
	st := update(ctx, c)
	st.LastRun = c.Now().UTC()
	switch st.Result {
	case model.UpdateRefused, model.UpdateFailed:
		c.Log.Error("update "+st.Result, "from", st.From, "to", st.To, "reason", st.Message)
	default:
		c.Log.Info("update "+st.Result, "from", st.From, "to", st.To, "message", st.Message)
	}
	if err := WriteStatus(filepath.Join(c.StateDir, StatusFile), st); err != nil {
		c.Log.Error("can't write the status file", "err", err)
	}
	return st
}

func update(ctx context.Context, c Config) model.AutoUpdate {
	own, ok := release.ParseVersion(c.Version)
	if !ok {
		return model.AutoUpdate{Result: model.UpdateSkipped, From: c.Version,
			Message: "a local build isn't updated automatically"}
	}
	st := model.AutoUpdate{From: own.Tag()}

	latest, err := release.FetchLatest(ctx, c.Client, c.API, "bitcoin-node-dash/"+own.Tag(), release.ParseVersion)
	if err != nil {
		st.Result, st.Message = model.UpdateFailed, "can't look up the latest release: "+err.Error()
		return st
	}
	tag := latest.Tag()
	if !latest.Newer(own) {
		st.Result = model.UpdateCurrent
		return st
	}
	st.To = tag
	if !installable(own, latest) {
		st.Result, st.Message = model.UpdateSkipped, tag+" is a new major version: update with the install command"
		return st
	}

	dir, err := os.MkdirTemp("", "node-dash-update-")
	if err != nil {
		st.Result, st.Message = model.UpdateFailed, err.Error()
		return st
	}
	defer os.RemoveAll(dir)

	newBin, result, err := fetchVerified(ctx, c, tag, dir)
	if err != nil {
		st.Result, st.Message = result, err.Error()
		return st
	}
	if err := install(ctx, c, newBin, tag); err != nil {
		st.Result, st.Message = model.UpdateFailed, err.Error()
		return st
	}
	st.Result = model.UpdateUpdated
	return st
}

// installable tells whether the updater may go from own to the newer latest
// by itself: within the same major version, or the one step from 0.x to 1.x
// (story #74). v1.0.0 changes nothing that needs the install command: it is
// where the promise of the README's "Versioning" section starts. Any other
// new major version may change the unit, the settings or the data, so it
// needs the install command (decision 0002).
func installable(own, latest release.Release) bool {
	return latest.Major == own.Major || own.Major == 0 && latest.Major == 1
}

// fetchVerified downloads release tag into dir and checks it. It returns the
// new binary, or why not: refused when a check fails, failed when a download
// fails.
func fetchVerified(ctx context.Context, c Config, tag, dir string) (string, string, error) {
	base := strings.TrimRight(c.Downloads, "/") + "/" + tag + "/"
	name := fmt.Sprintf("node-dash_%s_linux_%s", tag, c.Arch)

	sums, err := download(ctx, c.Client, base+"checksums.txt", maxSmallFile)
	if err != nil {
		return "", model.UpdateFailed, err
	}
	sig, err := download(ctx, c.Client, base+"checksums.txt.minisig", maxSmallFile)
	if errors.Is(err, errNotFound) {
		return "", model.UpdateRefused, fmt.Errorf("%s isn't signed", tag)
	}
	if err != nil {
		return "", model.UpdateFailed, err
	}
	if err := minisign.VerifyRelease(c.Key, tag, sums, sig); err != nil {
		return "", model.UpdateRefused, fmt.Errorf("the signature of %s doesn't check out: %w", tag, err)
	}
	want, ok := checksum(sums, name+".tar.gz")
	if !ok {
		return "", model.UpdateRefused, fmt.Errorf("checksums.txt of %s has no %s.tar.gz", tag, name)
	}

	archive, err := download(ctx, c.Client, base+name+".tar.gz", maxArchive)
	if err != nil {
		return "", model.UpdateFailed, err
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return "", model.UpdateRefused, fmt.Errorf("%s.tar.gz doesn't match checksums.txt", name)
	}
	bin := filepath.Join(dir, "node-dash")
	if err := extract(archive, name+"/node-dash", bin); err != nil {
		return "", model.UpdateRefused, err
	}
	v, err := c.RunVersion(ctx, bin)
	if err != nil || v != tag {
		return "", model.UpdateRefused, fmt.Errorf("the new binary says it is %q, not %s (%v)", v, tag, err)
	}
	return bin, "", nil
}

// install puts newBin in place of the installed binary, restarts the
// dashboard, and puts the old binary back when the new one doesn't answer.
func install(ctx context.Context, c Config, newBin, tag string) error {
	previous := filepath.Join(c.StateDir, previousFile)
	if err := os.MkdirAll(c.StateDir, 0o755); err != nil {
		return err
	}
	if err := copyFile(c.Binary, previous); err != nil {
		return fmt.Errorf("can't keep the installed binary: %w", err)
	}
	if err := replace(c.Binary, newBin); err != nil {
		return fmt.Errorf("can't install %s: %w", tag, err)
	}
	if err := c.Systemctl(ctx, "restart", c.Service); err == nil {
		if err = waitRunning(ctx, c, tag); err == nil {
			return nil
		}
	}

	// Roll back.
	from := c.Version
	if own, ok := release.ParseVersion(c.Version); ok {
		from = own.Tag()
	}
	reason := fmt.Sprintf("%s didn't start within %s; back on %s", tag, c.HealthTimeout.Round(time.Second), from)
	if err := replace(c.Binary, previous); err != nil {
		return fmt.Errorf("%s didn't start, and putting %s back failed: %w", tag, from, err)
	}
	_ = c.Systemctl(ctx, "reset-failed", c.Service)
	if err := c.Systemctl(ctx, "restart", c.Service); err != nil {
		return fmt.Errorf("%s; restarting it failed: %w", reason, err)
	}
	if err := waitRunning(ctx, c, from); err != nil {
		return fmt.Errorf("%s, but it doesn't answer either; see journalctl -u %s", reason, c.Service)
	}
	return errors.New(reason)
}

// waitRunning waits until the dashboard answers as version tag.
func waitRunning(ctx context.Context, c Config, tag string) error {
	ctx, cancel := context.WithTimeout(ctx, c.HealthTimeout)
	defer cancel()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		err := c.Running(ctx, tag)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-t.C:
		}
	}
}

var errNotFound = errors.New("not found")

func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("download %s: %w", url, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("download %s: larger than %d bytes", url, limit)
	}
	return b, nil
}

// checksum finds file's SHA-256 in a sha256sum listing.
func checksum(sums []byte, file string) (string, bool) {
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == file && len(f[0]) == 64 {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// extract writes the file at path inside a .tar.gz archive to dst.
func extract(archive []byte, path, dst string) error {
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return fmt.Errorf("the archive isn't gzip: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("the archive has no %s", path)
		}
		if err != nil {
			return fmt.Errorf("the archive is damaged: %w", err)
		}
		if h.Name != path || h.Typeflag != tar.TypeReg {
			continue
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(tr, maxArchive))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}
}

// replace puts a copy of src at dst in one step: a copy next to dst, then a
// rename, so dst is never half written.
func replace(dst, src string) error {
	tmp := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".new")
	if err := copyFile(src, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// WriteStatus writes the status file in one step, readable by the dashboard.
func WriteStatus(path string, st model.AutoUpdate) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadStatus reads the status file; nil when there is none, so automatic
// updates are off, or it can't be read.
func ReadStatus(path string) *model.AutoUpdate {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var st model.AutoUpdate
	if err := json.Unmarshal(b, &st); err != nil || st.Result == "" {
		return nil
	}
	return &st
}

// --- On a real server ---------------------------------------------------------

func systemctl(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "version").Output()
	return strings.TrimSpace(string(out)), err
}

// running asks the dashboard of service whether it answers as version tag:
// /healthz, then the version in /api/status.
func running(ctx context.Context, client *http.Client, service, tag string) error {
	host := HealthHost(serviceListen(ctx, service))
	get := func(path string) (*http.Response, error) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+path, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
		}
		return resp, nil
	}
	resp, err := get("/healthz")
	if err != nil {
		return err
	}
	resp.Body.Close()
	resp, err = get("/api/status")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var snap struct {
		Dashboard struct {
			Version string `json:"version"`
		} `json:"dashboard"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&snap); err != nil {
		return fmt.Errorf("/api/status: %w", err)
	}
	if snap.Dashboard.Version != tag {
		return fmt.Errorf("the dashboard runs %q, not %s", snap.Dashboard.Version, tag)
	}
	return nil
}

// serviceListen finds the dashboard's NODEDASH_LISTEN in the environment of
// its running process, which includes an EnvironmentFile. Root may read it.
func serviceListen(ctx context.Context, service string) string {
	out, err := exec.CommandContext(ctx, "systemctl", "show", "-p", "MainPID", "--value", service).Output()
	if err == nil {
		if env, err := os.ReadFile("/proc/" + strings.TrimSpace(string(out)) + "/environ"); err == nil {
			for _, kv := range strings.Split(string(env), "\x00") {
				if v, ok := strings.CutPrefix(kv, "NODEDASH_LISTEN="); ok && v != "" {
					return v
				}
			}
		}
	}
	return ":8080"
}

// HealthHost turns a listen address into one to connect to: a wildcard
// address becomes the loopback address.
func HealthHost(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "127.0.0.1:8080"
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return net.JoinHostPort(host, port)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

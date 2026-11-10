package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func reader(conf, dir string, args ...string) *Reader {
	return &Reader{ConfPath: conf, DataDir: dir, args: func() []string { return args }}
}

func item(t *testing.T, s *model.Settings, key string) model.Setting {
	t.Helper()
	for _, it := range s.Items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("no %s", key)
	return model.Setting{}
}

func value(it model.Setting) string {
	if it.Value == nil {
		return "nil"
	}
	b, _ := json.Marshal(*it.Value)
	return string(b)
}

func check(t *testing.T, s *model.Settings, key, wantValue, wantSource, wantDetail string) {
	t.Helper()
	it := item(t, s, key)
	if value(it) != wantValue || it.Source != wantSource || it.SourceDetail != wantDetail {
		t.Errorf("%s = %s from %s %q, want %s from %s %q", key, value(it), it.Source, it.SourceDetail, wantValue, wantSource, wantDetail)
	}
}

const sample = `# my node
rpcuser=alice
rpcpassword=hunter2
dbcache=2048
txindex=1
nolisten=1
maxconnections=40
maxconnections=80   # the first one wins
includeconf=extra.conf

[main]
maxuploadtarget=5G
dbcache=4096

[test]
prune=550
`

func TestSourcesAndPrecedence(t *testing.T) {
	dir := t.TempDir()
	conf := write(t, dir, "bitcoin.conf", sample)
	write(t, dir, "extra.conf", "maxmempool=500\ndbcache=1\n")
	s := reader(conf, dir, "-datadir="+dir, "-prune=550", "-rpcpassword=secret").ReadSettings(nil)

	if s.Path != conf || s.FileError != "" {
		t.Fatalf("path %q, error %q", s.Path, s.FileError)
	}
	check(t, s, "dbcache", "4096", model.FromConf, "[main]") // the section wins over the top
	check(t, s, "maxconnections", "40", model.FromConf, "")  // the first value wins
	check(t, s, "maxmempool", "500", model.FromInclude, "extra.conf")
	check(t, s, "prune", "550", model.FromCmdline, "-prune=550") // [test] doesn't count on mainnet
	check(t, s, "txindex", "1", model.FromConf, "")
	check(t, s, "listen", "0", model.FromConf, "") // nolisten=1
	check(t, s, "blocksonly", "0", model.FromDefault, "")
	check(t, s, "maxuploadtarget", "5120", model.FromConf, "[main]") // 5G = 5 GiB

	// Nothing else leaves the package: no secrets from the file or the command line.
	b, _ := json.Marshal(s)
	for _, secret := range []string{"hunter2", "secret", "alice", "rpc"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("%q leaked: %s", secret, b)
		}
	}
	if len(s.Items) != len(Keys) {
		t.Fatalf("%d items, want %d", len(s.Items), len(Keys))
	}
}

func TestDefaults(t *testing.T) {
	dir := t.TempDir()
	s := reader(write(t, dir, "bitcoin.conf", ""), dir).ReadSettings(nil)
	check(t, s, "dbcache", "nil", model.FromDefault, "") // depends on the version: no number
	check(t, s, "maxconnections", "125", model.FromDefault, "")
	check(t, s, "maxmempool", "300", model.FromDefault, "")
	check(t, s, "listen", "1", model.FromDefault, "")

	// -connect implies -listen=0.
	s = reader(write(t, dir, "bitcoin.conf", "connect=192.0.2.1\n"), dir).ReadSettings(nil)
	check(t, s, "listen", "0", model.FromDefault, "")
}

func TestRunningValues(t *testing.T) {
	dir := t.TempDir()
	conf := write(t, dir, "bitcoin.conf", "maxmempool=500\ntxindex=1\n")
	running := &model.RunningSettings{Chain: "main", MaxMempool: val(300), TxIndex: val(1), Prune: val(0), MaxUploadTarget: val(2048)}
	s := reader(conf, dir).ReadSettings(running)

	// bitcoin.conf was changed, but bitcoind wasn't restarted.
	mp := item(t, s, "maxmempool")
	if value(mp) != "300" || mp.Source != model.FromRunning || mp.ConfValue == nil || *mp.ConfValue != 500 {
		t.Fatalf("maxmempool = %+v", mp)
	}
	check(t, s, "txindex", "1", model.FromConf, "")
	check(t, s, "prune", "0", model.FromDefault, "")
	// Set somewhere the dashboard can't see, e.g. a command line in another container.
	check(t, s, "maxuploadtarget", "2048", model.FromRunning, "")
}

func TestChainSection(t *testing.T) {
	dir := t.TempDir()
	conf := write(t, dir, "bitcoin.conf", "prune=1000\n[signet]\nprune=550\n")
	s := reader(conf, dir, "-signet").ReadSettings(nil)
	check(t, s, "prune", "550", model.FromConf, "[signet]")
	s = reader(conf, dir).ReadSettings(&model.RunningSettings{Chain: "signet"})
	check(t, s, "prune", "550", model.FromConf, "[signet]")
}

func TestConfFromCommandLine(t *testing.T) {
	dir := t.TempDir()
	other := write(t, dir, "other.conf", "dbcache=300\n")
	s := reader(filepath.Join(dir, "bitcoin.conf"), dir, "-conf=other.conf").ReadSettings(nil)
	if s.Path != other {
		t.Fatalf("path = %s, want %s", s.Path, other)
	}
	check(t, s, "dbcache", "300", model.FromConf, "")
}

func TestInvalidValues(t *testing.T) {
	dir := t.TempDir()
	s := reader(write(t, dir, "bitcoin.conf", "dbcache=lots\nmaxconnections=40x\ntxindex=true\n"), dir).ReadSettings(nil)
	if it := item(t, s, "dbcache"); it.Invalid != "lots" || value(it) != "0" {
		t.Errorf("dbcache = %+v", it)
	}
	if it := item(t, s, "maxconnections"); it.Invalid != "40x" || value(it) != "40" {
		t.Errorf("maxconnections = %+v", it)
	}
	// Like Bitcoin Core: "true" isn't a number, so txindex is off.
	if it := item(t, s, "txindex"); it.Invalid != "true" || value(it) != "0" {
		t.Errorf("txindex = %+v", it)
	}
}

func TestMissingFile(t *testing.T) {
	dir := t.TempDir()
	s := reader(filepath.Join(dir, "bitcoin.conf"), dir).ReadSettings(&model.RunningSettings{MaxMempool: val(300)})
	if !strings.HasPrefix(s.FileError, "No ") {
		t.Fatalf("file error = %q", s.FileError)
	}
	check(t, s, "maxmempool", "300", model.FromDefault, "")

	if os.Getuid() != 0 {
		conf := write(t, dir, "locked.conf", "dbcache=1\n")
		_ = os.Chmod(conf, 0)
		s = reader(conf, dir).ReadSettings(nil)
		if !strings.Contains(s.FileError, "permission denied") {
			t.Fatalf("file error = %q", s.FileError)
		}
	}
}

func TestByteSizes(t *testing.T) {
	for in, want := range map[string]int64{"0": 0, "5000": 5000, "5G": 5120, "5g": 4768, "500M": 500, "1T": 1 << 20} {
		if got, ok := parseByteSize(in); !ok || got != want {
			t.Errorf("parseByteSize(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
}

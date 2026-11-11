package settings

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

func TestEditInPlace(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		changes  map[string]int64
		want     string
	}{
		{
			name:    "the effective line, comment kept, rest byte for byte",
			in:      "# my node\r\nrpcpassword=hunter2\r\ndbcache=2048   # big\r\ndbcache=1\r\nweird line\r\n",
			changes: map[string]int64{"dbcache": 4096},
			want:    "# my node\r\nrpcpassword=hunter2\r\ndbcache=4096 # big\r\ndbcache=1\r\nweird line\r\n",
		},
		{
			name:    "the chain's section wins over the top",
			in:      "maxmempool=300\n[main]\nmaxmempool=400\n[test]\nmaxmempool=50\n",
			changes: map[string]int64{"maxmempool": 500},
			want:    "maxmempool=300\n[main]\nmaxmempool=500\n[test]\nmaxmempool=50\n",
		},
		{
			name:    "a chain prefix counts as the section",
			in:      "  main.maxuploadtarget=5G\nmaxuploadtarget=1\n",
			changes: map[string]int64{"maxuploadtarget": 0},
			want:    "  main.maxuploadtarget=0\nmaxuploadtarget=1\n",
		},
		{
			name:    "a negation becomes the setting",
			in:      "nolisten=1\n",
			changes: map[string]int64{"listen": 1},
			want:    "listen=1\n",
		},
		{
			name:    "a new setting goes before the first section",
			in:      "server=1\n[test]\nprune=550\n",
			changes: map[string]int64{"txindex": 1, "dbcache": 1000},
			want:    "server=1\n\n" + Marker + "\ndbcache=1000\ntxindex=1\n\n[test]\nprune=550\n",
		},
		{
			name:    "and under the marker next time",
			in:      "server=1\n\n" + Marker + "\ndbcache=1000\n\n[test]\nprune=550\n",
			changes: map[string]int64{"maxmempool": 500},
			want:    "server=1\n\n" + Marker + "\ndbcache=1000\nmaxmempool=500\n\n[test]\nprune=550\n",
		},
		{
			name:    "at the end of a file without a final newline",
			in:      "server=1",
			changes: map[string]int64{"blocksonly": 1},
			want:    "server=1\n\n" + Marker + "\nblocksonly=1\n",
		},
		{
			name:    "an empty file",
			in:      "",
			changes: map[string]int64{"prune": 550},
			want:    Marker + "\nprune=550\n",
		},
		{
			name:    "a setting only in another chain's section is added at the top",
			in:      "[test]\nprune=550\n",
			changes: map[string]int64{"prune": 1000},
			want:    Marker + "\nprune=1000\n\n[test]\nprune=550\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Edit([]byte(tc.in), "main", tc.changes)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
	if _, err := Edit([]byte(""), "main", map[string]int64{"rpcpassword": 1}); err == nil {
		t.Error("an unknown key must be refused")
	}
}

// form sets up a bitcoin.conf with editing on.
func form(t *testing.T, conf string, args ...string) (*Reader, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := write(t, dir, "bitcoin.conf", conf)
	state := filepath.Join(t.TempDir(), "state")
	r := reader(path, dir, args...)
	if err := r.EnableEditing(state, "bitcoind.service", slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	return r, path, state
}

func details(started time.Time, pruned bool) *model.Details {
	return &model.Details{
		Blockchain: &model.BlockchainDetails{Pruned: pruned},
		Server:     model.ServerDetails{MemTotalBytes: 8 << 30, DiskFreeBytes: 500 << 30},
		Running:    &model.RunningSettings{Chain: "main", Started: started},
	}
}

func field(t *testing.T, f *model.SettingsForm, key string) model.SettingField {
	t.Helper()
	for _, x := range f.Fields {
		if x.Key == key {
			return x
		}
	}
	t.Fatalf("no field %s", key)
	return model.SettingField{}
}

func TestSave(t *testing.T) {
	const conf = "rpcuser=alice\nrpcpassword=hunter2\ndbcache=450\n[main]\nmaxmempool=300\n"
	r, path, state := form(t, conf)
	started := time.Now().Add(-time.Hour)
	d := details(started, false)

	f := r.Form(d)
	if f.Error != "" || f.Hash == "" || f.Saved != nil {
		t.Fatalf("form: %+v", f)
	}
	if v := field(t, f, "dbcache").Value; v == nil || *v != 450 {
		t.Errorf("dbcache = %v", v)
	}
	if fd := field(t, f, "listen"); fd.Value != nil || fd.Default == nil || *fd.Default != 1 || fd.Source != model.FromDefault {
		t.Errorf("listen: %+v", fd)
	}

	out, err := r.Save(d, f.Hash, map[string]int64{"dbcache": 2048, "maxmempool": 500, "txindex": 1, "listen": 1})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	// listen had no value: setting it to its default is still written.
	want := "rpcuser=alice\nrpcpassword=hunter2\ndbcache=2048\n\n" + Marker + "\ntxindex=1\nlisten=1\n\n[main]\nmaxmempool=500\n"
	if string(b) != want {
		t.Errorf("bitcoin.conf:\n%s\nwant\n%s", b, want)
	}
	if out.Saved == nil || !out.Saved.RestartNeeded || out.Hash == f.Hash {
		t.Fatalf("after saving: %+v", out)
	}
	keys := []string{}
	for _, c := range out.Saved.Changes {
		keys = append(keys, c.Key)
	}
	if strings.Join(keys, ",") != "dbcache,maxmempool,txindex,listen" {
		t.Errorf("changes %v", keys)
	}

	// The backup is the previous file, readable only by the dashboard's user.
	backups, _ := os.ReadDir(filepath.Join(state, "backups"))
	if len(backups) != 1 {
		t.Fatalf("%d backups", len(backups))
	}
	bp := filepath.Join(state, "backups", backups[0].Name())
	if b, _ := os.ReadFile(bp); string(b) != conf {
		t.Errorf("backup: %q", b)
	}
	if fi, _ := os.Stat(bp); fi.Mode().Perm() != 0o600 {
		t.Errorf("backup mode %v", fi.Mode().Perm())
	}

	// The settings section shows it too, until bitcoind restarts.
	if s := r.ReadSettings(d.Running); !s.Editable || !s.RestartNeeded {
		t.Errorf("section: editable %v, restart needed %v", s.Editable, s.RestartNeeded)
	}
	// It survives a restart of the dashboard.
	r2 := reader(path, filepath.Dir(path))
	if err := r2.EnableEditing(state, "bitcoind.service", slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	if f := r2.Form(d); f.Saved == nil || !f.Saved.RestartNeeded {
		t.Error("restart needed must survive a restart of the dashboard")
	}
	// Once bitcoind has started after the save, it's gone.
	d.Running.Started = time.Now().Add(time.Second)
	if f := r.Form(d); f.Saved != nil {
		t.Errorf("after the restart: %+v", f.Saved)
	}
}

func TestSaveConflict(t *testing.T) {
	r, path, state := form(t, "dbcache=450\n")
	d := details(time.Time{}, false)
	f := r.Form(d)
	if err := os.WriteFile(path, []byte("dbcache=450\n# edited over SSH\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Save(d, f.Hash, map[string]int64{"dbcache": 1000}); !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want a conflict", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "dbcache=450\n# edited over SSH\n" {
		t.Errorf("the file must stay as it is: %q", b)
	}
	if backups, _ := os.ReadDir(filepath.Join(state, "backups")); len(backups) != 0 {
		t.Error("no backup without a save")
	}
}

func TestSaveRefused(t *testing.T) {
	for _, tc := range []struct {
		name    string
		conf    string
		args    []string
		pruned  bool
		changes map[string]int64
		key     string
		want    string
	}{
		{name: "dbcache too small", changes: map[string]int64{"dbcache": 2}, key: "dbcache", want: "At least 4 MiB"},
		{name: "dbcache above memory", changes: map[string]int64{"dbcache": 9000}, key: "dbcache", want: "8192 MiB"},
		{name: "mempool too small", changes: map[string]int64{"maxmempool": 4}, key: "maxmempool", want: "5 MB"},
		{name: "negative connections", changes: map[string]int64{"maxconnections": -1}, key: "maxconnections", want: "0 or more"},
		{name: "negative upload", changes: map[string]int64{"maxuploadtarget": -1}, key: "maxuploadtarget", want: "unlimited"},
		{name: "a switch", changes: map[string]int64{"listen": 2}, key: "listen", want: "On (1) or off (0)"},
		{name: "prune too small", changes: map[string]int64{"prune": 100}, key: "prune", want: "550"},
		{name: "manual pruning", changes: map[string]int64{"prune": 1}, key: "prune", want: "Manual"},
		{name: "prune with txindex", conf: "txindex=1\n", changes: map[string]int64{"prune": 550}, key: "prune", want: "transaction index"},
		{name: "txindex on a pruned node", conf: "prune=550\n", pruned: true, changes: map[string]int64{"txindex": 1}, key: "txindex", want: "transaction index"},
		{name: "pruning off on a pruned node", conf: "prune=550\n", pruned: true, changes: map[string]int64{"prune": 0}, key: "prune", want: "whole blockchain"},
		{name: "from the command line", args: []string{"-dbcache=1000"}, changes: map[string]int64{"dbcache": 2000}, key: "dbcache", want: "command line (-dbcache=1000)"},
		{name: "not a setting", changes: map[string]int64{"rpcpassword": 1}, key: "rpcpassword", want: "Not a setting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := tc.conf + "rpcpassword=hunter2\n"
			r, path, _ := form(t, conf, tc.args...)
			d := details(time.Time{}, tc.pruned)
			_, err := r.Save(d, r.Form(d).Hash, tc.changes)
			var fe FieldErrors
			if !errors.As(err, &fe) {
				t.Fatalf("got %v, want field errors", err)
			}
			if !strings.Contains(fe[tc.key], tc.want) {
				t.Errorf("%s: %q, want %q", tc.key, fe[tc.key], tc.want)
			}
			if b, _ := os.ReadFile(path); string(b) != conf {
				t.Errorf("the file must stay as it is: %q", b)
			}
		})
	}
}

func TestSaveAllowed(t *testing.T) {
	// Turning pruning on and the index off together, and a new target size.
	r, path, _ := form(t, "txindex=1\n")
	d := details(time.Time{}, false)
	if _, err := r.Save(d, r.Form(d).Hash, map[string]int64{"prune": 550, "txindex": 0}); err != nil {
		t.Fatal(err)
	}
	d = details(time.Time{}, true)
	if _, err := r.Save(d, r.Form(d).Hash, map[string]int64{"prune": 1000}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "txindex=0\n\n"+Marker+"\nprune=1000\n" {
		t.Errorf("bitcoin.conf: %q", b)
	}
	// Nothing changes: refused, nothing written.
	if _, err := r.Save(d, r.Form(d).Hash, map[string]int64{"prune": 1000}); !errors.Is(err, ErrNothingToSave) {
		t.Errorf("got %v", err)
	}
}

func TestBackupsKept(t *testing.T) {
	r, _, state := form(t, "dbcache=100\n")
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	r.edit.now = func() time.Time { return now }
	d := details(time.Time{}, false)
	for i := range KeepBackups + 3 {
		now = now.Add(time.Second)
		if _, err := r.Save(d, r.Form(d).Hash, map[string]int64{"dbcache": int64(200 + i)}); err != nil {
			t.Fatal(err)
		}
	}
	backups, _ := os.ReadDir(filepath.Join(state, "backups"))
	if len(backups) != KeepBackups {
		t.Fatalf("%d backups, want %d", len(backups), KeepBackups)
	}
	// The oldest are gone: the first kept one has dbcache=202.
	if b, _ := os.ReadFile(filepath.Join(state, "backups", backups[0].Name())); string(b) != "dbcache=202\n" {
		t.Errorf("oldest kept backup: %q", b)
	}
}

func TestFormLocks(t *testing.T) {
	r, _, _ := form(t, "includeconf=extra.conf\n", "-prune=550")
	write(t, r.DataDir, "extra.conf", "maxmempool=500\n")
	write(t, r.DataDir, "settings.json", `{"_warning_": "do not edit", "dbcache": 1000, "wallet": ["a"]}`)
	f := r.Form(details(time.Time{}, true))
	for key, want := range map[string]string{
		"prune":      "command line (-prune=550)",
		"maxmempool": "extra.conf",
		"dbcache":    "settings.json",
	} {
		if got := field(t, f, key).Locked; !strings.Contains(got, want) {
			t.Errorf("%s locked: %q, want %q", key, got, want)
		}
	}
	if fd := field(t, f, "dbcache"); fd.Source != model.FromSettingsFile || *fd.Value != 1000 {
		t.Errorf("dbcache: %+v", fd)
	}
	if got := field(t, f, "txindex").Locked; got != "" {
		t.Errorf("txindex must be editable: %q", got)
	}
}

func TestFormReadOnlyFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anything")
	}
	r, path, _ := form(t, "dbcache=100\n")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	d := details(time.Time{}, false)
	f := r.Form(d)
	if !strings.Contains(f.Error, "may not write") {
		t.Errorf("error: %q", f.Error)
	}
	var cant *CantSaveError
	if _, err := r.Save(d, f.Hash, map[string]int64{"dbcache": 200}); !errors.As(err, &cant) {
		t.Errorf("got %v", err)
	}
}

func TestStartTime(t *testing.T) {
	stat := []byte("4242 (bitcoind (x)) S 1 4242 4242 0 -1 4194560 100 0 0 0 5 3 0 0 20 0 9 0 123456 1000 200 18446744073709551615")
	sys := []byte("cpu  1 2 3\nbtime 1794300000\nprocesses 5\n")
	want := time.Unix(1794300000, 0).Add(1234560 * time.Millisecond)
	if got := startTime(stat, sys); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := startTime([]byte("garbage"), sys); !got.IsZero() {
		t.Errorf("garbage: %v", got)
	}
	if got := processID("4242", stat, sys); got != "1794300000/4242/123456" {
		t.Errorf("process: %q", got)
	}
	if got := processID("4242", []byte("garbage"), sys); got != "" {
		t.Errorf("process of garbage: %q", got)
	}
}

// TestRestartSeen: the start time from /proc is off by up to a second, so
// a restart right after a save is told by the process, not the time.
func TestRestartSeen(t *testing.T) {
	r, path, state := form(t, "dbcache=450\n")
	process := "1794300000/4242/123456"
	r.process = func() string { return process }
	saveAt := time.Now()
	d := details(saveAt.Add(-time.Hour), false)
	f := r.Form(d)
	if _, err := r.Save(d, f.Hash, map[string]int64{"dbcache": 1000}); err != nil {
		t.Fatal(err)
	}

	// The same bitcoind, though its start time seems later: not restarted.
	d.Running.Started = saveAt.Add(time.Minute)
	if f := r.Form(d); f.Saved == nil {
		t.Error("the same bitcoind must still need a restart")
	}

	// Restarted right away: its start time seems before the save.
	process = "1794300000/4250/123500"
	d.Running.Started = saveAt.Add(-500 * time.Millisecond)
	if f := r.Form(d); f.Saved != nil {
		t.Errorf("after the restart: %+v", f.Saved)
	}
	if s := r.ReadSettings(d.Running); s.RestartNeeded {
		t.Error("the settings section must not ask for a restart either")
	}

	// The process of the save survives a restart of the dashboard.
	r2 := reader(path, filepath.Dir(path))
	r2.process = func() string { return process }
	if err := r2.EnableEditing(state, "bitcoind.service", slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	if f := r2.Form(d); f.Saved != nil {
		t.Errorf("after restarting the dashboard: %+v", f.Saved)
	}
	process = "1794300000/4242/123456"
	if f := r2.Form(d); f.Saved == nil {
		t.Error("with the bitcoind of the save, a restart is still needed")
	}
}

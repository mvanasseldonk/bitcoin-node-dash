package settings

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// The settings form (story #55, decision 0003). Only the eight settings in
// Keys can be changed, only to integers the editor writes itself, so no
// other text can get into bitcoin.conf.

const (
	// KeepBackups is how many backups of bitcoin.conf are kept.
	KeepBackups = 10
	backupDir   = "backups"
	backupName  = "bitcoin.conf."
	lastSave    = "last-save.json"
)

// ErrConflict is returned when bitcoin.conf changed since the form read it.
var ErrConflict = errors.New("bitcoin.conf changed since you opened this page")

// ErrNothingToSave is returned for a save without changes.
var ErrNothingToSave = errors.New("nothing to change")

// FieldErrors maps a setting to why its new value is refused.
type FieldErrors map[string]string

func (e FieldErrors) Error() string {
	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + e[k]
	}
	return "not saved: " + strings.Join(parts, "; ")
}

// CantSaveError says why bitcoin.conf can't be saved, in plain language.
type CantSaveError struct{ Reason string }

func (e *CantSaveError) Error() string { return e.Reason }

// editor is the state of the settings form.
type editor struct {
	// key makes the file's fingerprint an HMAC: a plain hash of a file with
	// rpcpassword in it could be guessed offline.
	key      []byte
	stateDir string
	service  string
	log      *slog.Logger
	now      func() time.Time

	// saving allows one save at a time.
	saving sync.Mutex
	// mu guards last and process.
	mu   sync.Mutex
	last *model.SavedSettings
	// process is the bitcoind that ran during the last save ("" unknown).
	process string
}

// lastSaveFile is what last-save.json holds.
type lastSaveFile struct {
	model.SavedSettings
	Process string `json:"process,omitempty"`
}

// EnableEditing turns the settings form on (NODEDASH_SETTINGS=1). stateDir
// holds the backups and the time of the last save; service is bitcoind's
// systemd service, for the restart command.
func (r *Reader) EnableEditing(stateDir, service string, log *slog.Logger) error {
	if err := os.MkdirAll(filepath.Join(stateDir, backupDir), 0o700); err != nil {
		return fmt.Errorf("settings state directory: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	e := &editor{key: key, stateDir: stateDir, service: service, log: log, now: time.Now}
	if b, err := os.ReadFile(filepath.Join(stateDir, lastSave)); err == nil {
		var s lastSaveFile
		if json.Unmarshal(b, &s) == nil && !s.At.IsZero() {
			e.last = &s.SavedSettings
			e.process = s.Process
		}
	}
	r.edit = e
	return nil
}

// Editable reports whether the settings form is on.
func (r *Reader) Editable() bool { return r.edit != nil && (r.allowed == nil || r.allowed()) }

// saved is the last save while bitcoind hasn't restarted since; nil after.
// process is the running bitcoind: another one than during the save has
// restarted. Without both, started tells: a start time is off by up to a
// second, so it misses a restart right after the save. With neither known,
// a restart is still needed.
func (e *editor) saved(started time.Time, process string) *model.SavedSettings {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last == nil {
		return nil
	}
	if e.process != "" && process != "" {
		if process != e.process {
			return nil
		}
	} else if !started.IsZero() && started.After(e.last.At) {
		return nil
	}
	s := *e.last
	s.RestartNeeded = true
	return &s
}

// Form returns what the settings form shows. d are the current details,
// for what the node runs with and the server's memory and disk.
func (r *Reader) Form(d *model.Details) *model.SettingsForm {
	return r.form(r.read(d.Running), d)
}

func (r *Reader) form(st *state, d *model.Details) *model.SettingsForm {
	f := &model.SettingsForm{Path: st.path, MemTotalBytes: d.Server.MemTotalBytes, DiskFreeBytes: d.Server.DiskFreeBytes}
	if d.Blockchain != nil {
		pruned := d.Blockchain.Pruned
		f.Pruned = &pruned
	}
	if r.edit != nil {
		f.Service = r.edit.service
		f.Saved = r.edit.saved(r.bitcoindStarted(d.Running), r.bitcoindProcess())
	}
	switch {
	case st.readErr != nil:
		f.Error = fileError(st.path, st.readErr)
	default:
		if r.edit != nil {
			f.Hash = r.edit.fingerprint(st.raw)
		}
		if err := writable(st.path); err != nil {
			f.Error = writeError(st.path, err)
		}
	}
	for _, s := range specs {
		v, found, _ := st.configured(s)
		field := model.SettingField{Key: s.key, Value: v, Default: st.def(s), Source: model.FromDefault}
		if v != nil {
			field.Source, field.SourceDetail = found.source, found.detail
			field.Locked = lockReason(found)
		}
		if d.Running != nil && s.running != nil {
			field.Running = s.running(d.Running)
		}
		f.Fields = append(f.Fields, field)
	}
	return f
}

// lockReason says why the form can't change a setting set here; "" when it
// can.
func lockReason(f found) string {
	switch f.source {
	case model.FromCmdline:
		return fmt.Sprintf("Set on bitcoind's command line (%s), which wins over bitcoin.conf: change it there.", f.detail)
	case model.FromInclude:
		return fmt.Sprintf("Set in %s, a file included by bitcoin.conf: change it there.", f.detail)
	case model.FromSettingsFile:
		return "Set in settings.json in the data directory, which wins over bitcoin.conf: change it there, or in bitcoin-qt."
	}
	return ""
}

// Save changes the settings in bitcoin.conf (decision 0003, "A save, step
// by step"). sum is the hash the form read; d are the current details.
// It returns the form as it is after saving.
func (r *Reader) Save(d *model.Details, sum string, changes map[string]int64) (*model.SettingsForm, error) {
	e := r.edit
	if e == nil {
		return nil, &CantSaveError{Reason: "changing settings is off (NODEDASH_SETTINGS)"}
	}
	e.saving.Lock()
	defer e.saving.Unlock()

	// 1. Read the file again; refuse when it changed.
	st := r.read(d.Running)
	if st.readErr != nil {
		return nil, &CantSaveError{Reason: fileError(st.path, st.readErr)}
	}
	if !hmac.Equal([]byte(e.fingerprint(st.raw)), []byte(sum)) {
		return nil, ErrConflict
	}
	if err := writable(st.path); err != nil {
		return nil, &CantSaveError{Reason: writeError(st.path, err)}
	}

	// 2. Validate, without the values that don't change.
	form := r.form(st, d)
	fields := map[string]model.SettingField{}
	for _, f := range form.Fields {
		fields[f.Key] = f
	}
	real := map[string]int64{}
	errs := FieldErrors{}
	for key, v := range changes {
		f, ok := fields[key]
		if !ok {
			errs[key] = "Not a setting the form can change."
			continue
		}
		if f.Value != nil && *f.Value == v {
			continue
		}
		real[key] = v
	}
	if len(errs) > 0 {
		return nil, errs
	}
	if len(real) == 0 {
		return nil, ErrNothingToSave
	}
	if errs := validate(fields, real, form.Pruned, form.MemTotalBytes); len(errs) > 0 {
		return nil, errs
	}

	// 3. Edit the text.
	edited, err := Edit(st.raw, st.chain, real)
	if err != nil {
		return nil, err
	}

	// 4. Back up the file as it is.
	backup, err := e.backup(st.raw)
	if err != nil {
		e.log.Error("settings not saved: can't back up bitcoin.conf", "err", err)
		return nil, &CantSaveError{Reason: "Not saved: the backup of bitcoin.conf failed"}
	}

	// 5. Write in place, and 6. check.
	if err := writeInPlace(st.path, edited); err != nil {
		e.log.Error("settings not saved: can't write bitcoin.conf", "path", st.path, "err", err)
		restore(st.path, st.raw, e.log)
		return nil, &CantSaveError{Reason: writeError(st.path, err)}
	}
	if err := verify(st, edited, real); err != nil {
		e.log.Error("settings not saved: bitcoin.conf isn't as written; restored the backup", "path", st.path, "err", err)
		restore(st.path, st.raw, e.log)
		return nil, &CantSaveError{Reason: "Not saved: bitcoin.conf wasn't as written, so the previous version is back"}
	}

	// 7. Log only the keys that changed.
	var list []model.SettingChange
	var logged []string
	for _, key := range Keys {
		v, ok := real[key]
		if !ok {
			continue
		}
		from := fields[key].Value
		list = append(list, model.SettingChange{Key: key, From: from, To: v})
		was := "unset"
		if from != nil {
			was = fmt.Sprint(*from)
		}
		logged = append(logged, fmt.Sprintf("%s %s → %d", key, was, v))
	}
	e.log.Info("settings saved: "+strings.Join(logged, ", "), "path", st.path, "backup", backup)

	process := r.bitcoindProcess()
	e.mu.Lock()
	e.last = &model.SavedSettings{At: e.now().UTC(), Changes: list}
	e.process = process
	e.persist()
	e.mu.Unlock()
	return r.Form(d), nil
}

// validate checks new values and combinations (decision 0003, "What can be
// changed"). fields are the form's; pruned is whether the node reports it
// is pruned (nil: unknown).
func validate(fields map[string]model.SettingField, changes map[string]int64, pruned *bool, memTotal uint64) FieldErrors {
	errs := FieldErrors{}
	final := func(key string) int64 {
		if v, ok := changes[key]; ok {
			return v
		}
		f := fields[key]
		switch {
		case f.Value != nil:
			return *f.Value
		case f.Default != nil:
			return *f.Default
		}
		return 0
	}
	for key, v := range changes {
		f := fields[key]
		if f.Locked != "" {
			errs[key] = f.Locked
			continue
		}
		switch key {
		case "dbcache":
			memMiB := int64(memTotal >> 20)
			switch {
			case v < 4:
				errs[key] = "At least 4 MiB."
			case memMiB > 0 && v > memMiB:
				errs[key] = fmt.Sprintf("More than this server's memory (%d MiB).", memMiB)
			}
		case "maxmempool":
			if v < 5 {
				errs[key] = "At least 5 MB."
			}
		case "maxconnections":
			if v < 0 {
				errs[key] = "0 or more."
			}
		case "maxuploadtarget":
			if v < 0 {
				errs[key] = "0 (unlimited) or more."
			}
		case "txindex", "listen", "blocksonly":
			if v != 0 && v != 1 {
				errs[key] = "On (1) or off (0)."
			}
		case "prune":
			wasPruned := (pruned != nil && *pruned) || (pruned == nil && f.Value != nil && *f.Value > 0)
			switch {
			case v == 1 && (f.Value == nil || *f.Value != 1):
				errs[key] = "Manual pruning (1) can't be set here."
			case v < 0 || (v > 1 && v < 550):
				errs[key] = "Off (0), or at least 550 MiB."
			case v == 0 && wasPruned:
				errs[key] = "This node is pruned. Keeping all blocks again means downloading the whole blockchain (bitcoind -reindex); that isn't possible from here."
			}
		}
	}
	if final("prune") > 0 && final("txindex") == 1 {
		key := "prune"
		if _, ok := changes["txindex"]; ok {
			key = "txindex"
		}
		if _, taken := errs[key]; !taken {
			errs[key] = "A pruned node can't keep a transaction index. Turn one of them off."
		}
	}
	return errs
}

// verify reads bitcoin.conf again: it must be exactly what was written, and
// set the changed settings to their new values.
func verify(st *state, edited []byte, changes map[string]int64) error {
	b, err := os.ReadFile(st.path)
	if err != nil {
		return err
	}
	if !bytes.Equal(b, edited) {
		return errors.New("the file differs from what was written")
	}
	conf := readConf(b, st.dataDir, st.chain)
	for key, want := range changes {
		f, ok := conf[key]
		if !ok || f.source != model.FromConf {
			return fmt.Errorf("%s isn't set in the file", key)
		}
		if got, _ := parse(specFor(key).kind, f); got != want {
			return fmt.Errorf("%s is %d, not %d", key, got, want)
		}
	}
	return nil
}

// backup writes content to the backup directory, readable only by the
// dashboard's user (it holds rpcpassword), and keeps the last KeepBackups.
func (e *editor) backup(content []byte) (string, error) {
	dir := filepath.Join(e.stateDir, backupDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := e.now().UTC().Format("20060102T150405Z")
	var f *os.File
	var path string
	for i := 1; ; i++ {
		path = filepath.Join(dir, backupName+stamp)
		if i > 1 {
			path = fmt.Sprintf("%s-%d", path, i)
		}
		var err error
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || i > 100 {
			return "", err
		}
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	syncDir(dir)

	entries, err := os.ReadDir(dir)
	if err == nil {
		var names []string
		for _, en := range entries {
			if strings.HasPrefix(en.Name(), backupName) {
				names = append(names, en.Name())
			}
		}
		sort.Strings(names)
		for len(names) > KeepBackups {
			_ = os.Remove(filepath.Join(dir, names[0]))
			names = names[1:]
		}
	}
	return path, nil
}

// persist stores the last save, so "restart needed" survives a restart of
// the dashboard.
func (e *editor) persist() {
	b, err := json.Marshal(lastSaveFile{SavedSettings: *e.last, Process: e.process})
	if err != nil {
		return
	}
	tmp := filepath.Join(e.stateDir, lastSave+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		e.log.Warn("can't store the time of the last save", "err", err)
		return
	}
	if err := os.Rename(tmp, filepath.Join(e.stateDir, lastSave)); err != nil {
		e.log.Warn("can't store the time of the last save", "err", err)
	}
}

// writeInPlace writes content into the file itself: with write access to
// this one file only (ReadWritePaths=), it can't be replaced by a rename.
// Owner and mode stay as they are.
func writeInPlace(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteAt(content, 0); err != nil {
		f.Close()
		return err
	}
	if err := f.Truncate(int64(len(content))); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// restore writes the previous content back after a failed save.
func restore(path string, content []byte, log *slog.Logger) {
	if err := writeInPlace(path, content); err != nil {
		log.Error("can't restore bitcoin.conf; the backup is in the settings state directory", "path", path, "err", err)
	}
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}

// fingerprint identifies a version of bitcoin.conf, for the conflict check.
func (e *editor) fingerprint(b []byte) string {
	m := hmac.New(sha256.New, e.key)
	m.Write(b)
	return hex.EncodeToString(m.Sum(nil))
}

// writable reports whether the dashboard may write path. A read-only bind
// mount (no ReadWritePaths=, or the file was replaced since) gives EROFS.
func writable(path string) error {
	return syscall.Access(path, 2 /* W_OK */)
}

// writeError says why bitcoin.conf can't be written, in plain language.
func writeError(path string, err error) string {
	switch {
	case errors.Is(err, syscall.EROFS):
		return fmt.Sprintf("%s is read-only for node-dash. Turn on changing settings with the installer (NODEDASH_SETTINGS=1). If it is on and bitcoin.conf was edited on the server since, restart node-dash: sudo systemctl restart node-dash", path)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Sprintf("node-dash may not write %s: it must be writable by the user node-dash runs as", path)
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("No %s", path)
	}
	return fmt.Sprintf("node-dash can't write %s", path)
}

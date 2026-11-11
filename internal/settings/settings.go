// Package settings reads the main settings of the node (story #13): the
// value it runs with and where it comes from. It only reads. Of bitcoin.conf
// and bitcoind's command line, only the settings in Keys leave this package:
// never secrets such as rpcpassword.
package settings

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

type kind int

const (
	number kind = iota
	boolean
	byteSize // MiB, or a number with a unit such as "5G" (Bitcoin Core 26+)
)

type spec struct {
	key  string
	kind kind
	// def is Bitcoin Core's default; nil when it depends on the version or
	// the machine (dbcache).
	def *int64
	// running is the value the node reports, if it does.
	running func(*model.RunningSettings) *int64
}

func val(v int64) *int64 { return &v }

var specs = []spec{
	{key: "dbcache", kind: number},
	{key: "maxconnections", kind: number, def: val(125)},
	{key: "maxmempool", kind: number, def: val(300), running: func(r *model.RunningSettings) *int64 { return r.MaxMempool }},
	{key: "prune", kind: number, def: val(0), running: func(r *model.RunningSettings) *int64 { return r.Prune }},
	{key: "txindex", kind: boolean, def: val(0), running: func(r *model.RunningSettings) *int64 { return r.TxIndex }},
	{key: "listen", kind: boolean, def: val(1)},
	{key: "blocksonly", kind: boolean, def: val(0), running: func(r *model.RunningSettings) *int64 { return r.BlocksOnly }},
	{key: "maxuploadtarget", kind: byteSize, def: val(0), running: func(r *model.RunningSettings) *int64 { return r.MaxUploadTarget }},
}

// Keys are the settings that are shown, in order.
var Keys = func() []string {
	k := make([]string, len(specs))
	for i, s := range specs {
		k[i] = s.key
	}
	return k
}()

// Reader reads the settings from bitcoin.conf, settings.json and bitcoind's
// command line. With editing on (EnableEditing), it also serves the
// settings form (story #55).
type Reader struct {
	// ConfPath is bitcoin.conf, unless bitcoind's command line names another
	// with -conf.
	ConfPath string
	// DataDir is where relative -conf and includeconf paths start.
	DataDir string
	// args returns bitcoind's command-line arguments, nil when unknown.
	args func() []string
	// started returns when bitcoind started, from /proc; zero when unknown.
	started func() time.Time
	// process identifies the running bitcoind, from /proc; "" when unknown.
	process func() string

	// edit is nil while editing is off.
	edit *editor
}

// New returns a Reader that finds bitcoind's command line in /proc.
func New(confPath, dataDir string) *Reader {
	return &Reader{ConfPath: confPath, DataDir: dataDir, args: bitcoindArgs, started: bitcoindStarted, process: bitcoindProcess}
}

// WithArgs replaces bitcoind's command line, for the mock mode.
func (r *Reader) WithArgs(args []string) *Reader {
	r.args = func() []string { return args }
	r.started = func() time.Time { return time.Time{} }
	r.process = func() string { return "" }
	return r
}

// found is a configured value and where it is set.
type found struct {
	raw     string
	source  string
	detail  string
	negated bool
}

// state is one reading of everything that configures the node.
type state struct {
	path    string
	dataDir string
	chain   string
	// raw is bitcoin.conf as read; nil when it couldn't be.
	raw     []byte
	readErr error
	// conf has the values in bitcoin.conf and its included files; cmd those
	// on bitcoind's command line; file those in settings.json.
	conf, cmd, file map[string]found
}

// lookup returns where key is set, in Bitcoin Core's order: the command
// line, then settings.json, then bitcoin.conf.
func (st *state) lookup(key string) (found, bool) {
	for _, m := range []map[string]found{st.cmd, st.file, st.conf} {
		if f, ok := m[key]; ok {
			return f, true
		}
	}
	return found{}, false
}

func (r *Reader) read(running *model.RunningSettings) *state {
	var args []string
	if r.args != nil {
		args = r.args()
	}
	cmd := parseArgs(args)
	st := &state{dataDir: r.DataDir, path: r.ConfPath, chain: "main", cmd: cmd}
	if d, ok := cmd["datadir"]; ok && d.raw != "" {
		st.dataDir = d.raw
	}
	if c, ok := cmd["conf"]; ok && c.raw != "" {
		st.path = c.raw
		if !filepath.IsAbs(st.path) {
			st.path = filepath.Join(st.dataDir, st.path)
		}
	}
	if running != nil && running.Chain != "" {
		st.chain = running.Chain
	} else if c := chainFromArgs(cmd); c != "" {
		st.chain = c
	}
	st.raw, st.readErr = os.ReadFile(st.path)
	if st.readErr != nil {
		st.raw = nil
	}
	st.conf = readConf(st.raw, st.dataDir, st.chain)
	st.file = readSettingsFile(st.dataDir, st.chain)
	return st
}

// configured is the value of s where it is set, if it is.
func (st *state) configured(s spec) (v *int64, f found, invalid string) {
	f, ok := st.lookup(s.key)
	if !ok {
		return nil, f, ""
	}
	n, valid := parse(s.kind, f)
	if !valid {
		invalid = f.raw
	}
	return &n, f, invalid
}

// def is Bitcoin Core's default for s, given the rest of the configuration.
func (st *state) def(s spec) *int64 {
	if s.key == "listen" {
		// -connect implies -listen=0.
		if c, ok := st.lookup("connect"); ok && !c.negated && c.raw != "0" {
			return val(0)
		}
	}
	return s.def
}

// ReadSettings implements source.SettingsReader. running is nil while the
// node can't be reached.
func (r *Reader) ReadSettings(running *model.RunningSettings) *model.Settings {
	st := r.read(running)
	out := &model.Settings{Path: st.path}
	if st.readErr != nil {
		out.FileError = fileError(st.path, st.readErr)
	}
	for _, s := range specs {
		item := model.Setting{Key: s.key}
		configured, f, invalid := st.configured(s)
		item.Invalid = invalid
		def := st.def(s)
		var run *int64
		if running != nil && s.running != nil {
			run = s.running(running)
		}
		switch {
		case run != nil && configured != nil:
			item.Value = run
			if *run == *configured {
				item.Source, item.SourceDetail = f.source, f.detail
			} else {
				item.Source = model.FromRunning
				item.ConfValue = configured
			}
		case run != nil:
			item.Value = run
			item.Source = model.FromRunning
			if def != nil && *def == *run {
				item.Source = model.FromDefault
			}
		case configured != nil:
			item.Value = configured
			item.Source, item.SourceDetail = f.source, f.detail
		default:
			item.Value = def
			item.Source = model.FromDefault
		}
		out.Items = append(out.Items, item)
	}
	if r.edit != nil {
		out.Editable = true
		if saved := r.edit.saved(r.bitcoindStarted(running), r.bitcoindProcess()); saved != nil {
			out.RestartNeeded = saved.RestartNeeded
		}
	}
	return out
}

// bitcoindStarted is when bitcoind started: from /proc, else from its uptime
// over RPC. Zero when unknown.
func (r *Reader) bitcoindStarted(running *model.RunningSettings) time.Time {
	if r.started != nil {
		if t := r.started(); !t.IsZero() {
			return t
		}
	}
	if running != nil {
		return running.Started
	}
	return time.Time{}
}

// bitcoindProcess identifies the running bitcoind; "" when unknown.
func (r *Reader) bitcoindProcess() string {
	if r.process == nil {
		return ""
	}
	return r.process()
}

// parse reads a configured value the way Bitcoin Core does. valid is false
// for a value that isn't a clean number; Core then reads its leading digits.
func parse(k kind, f found) (v int64, valid bool) {
	raw := strings.TrimSpace(f.raw)
	switch k {
	case boolean:
		// Like Core's InterpretBool: empty is true, otherwise a number.
		on := true
		if raw != "" {
			n, ok := atoi(raw)
			on, valid = n != 0, ok
		} else {
			valid = true
		}
		if f.negated {
			on = !on
		}
		if on {
			return 1, valid
		}
		return 0, valid
	case byteSize:
		if f.negated {
			return 0, true
		}
		return parseByteSize(raw)
	default:
		if f.negated {
			return 0, true
		}
		return atoi(raw)
	}
}

// atoi reads an integer like Core's LocaleIndependentAtoi: leading digits,
// 0 when there are none. ok is false when anything else followed.
func atoi(s string) (int64, bool) {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, true
	}
	end := 0
	if end < len(s) && (s[end] == '-' || s[end] == '+') {
		end++
	}
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.ParseInt(s[:end], 10, 64)
	return n, false
}

// parseByteSize reads maxuploadtarget in MiB: a number of MiB, or a number
// with a unit (Core's ParseByteUnits: k, m, g, t are powers of 1000; K, M,
// G, T powers of 1024).
func parseByteSize(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	mult := float64(1 << 20)
	if u := s[len(s)-1]; strings.ContainsRune("kmgtKMGT", rune(u)) {
		s = s[:len(s)-1]
		switch u {
		case 'k':
			mult = 1e3
		case 'm':
			mult = 1e6
		case 'g':
			mult = 1e9
		case 't':
			mult = 1e12
		case 'K':
			mult = 1 << 10
		case 'M':
			mult = 1 << 20
		case 'G':
			mult = 1 << 30
		case 'T':
			mult = 1 << 40
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		v, _ := atoi(s)
		return v, false
	}
	return int64(float64(n) * mult / (1 << 20)), true
}

// readConf reads bitcoin.conf (raw, nil when unreadable) and its
// includeconf files for one chain. Within a file the first value of a key
// wins; a value in the chain's section wins over one at the top; the main
// file wins over included files.
func readConf(raw []byte, dataDir, chain string) map[string]found {
	if raw == nil {
		return map[string]found{}
	}
	top, section, includes := parseFile(bytes.NewReader(raw), chain, model.FromConf, "")

	var incTop, incSection []map[string]found
	for _, inc := range includes {
		p := inc
		if !filepath.IsAbs(p) {
			p = filepath.Join(dataDir, p)
		}
		g, err := os.Open(p)
		if err != nil {
			continue // Bitcoin Core refuses to start then; the main file still says enough
		}
		t, s, _ := parseFile(g, chain, model.FromInclude, filepath.Base(inc))
		g.Close()
		incTop, incSection = append(incTop, t), append(incSection, s)
	}

	out := map[string]found{}
	for _, m := range append(append([]map[string]found{section}, incSection...), append([]map[string]found{top}, incTop...)...) {
		for k, v := range m {
			if _, seen := out[k]; !seen {
				out[k] = v
			}
		}
	}
	return out
}

// chainDirs are the chains' subdirectories of the data directory.
var chainDirs = map[string]string{"main": "", "test": "testnet3", "testnet4": "testnet4", "signet": "signet", "regtest": "regtest"}

// readSettingsFile reads the settings this package knows from Bitcoin
// Core's settings.json, in the chain's data directory. Its values win over
// bitcoin.conf. Everything else in it, such as wallet names, is ignored.
func readSettingsFile(dataDir, chain string) map[string]found {
	out := map[string]found{}
	b, err := os.ReadFile(filepath.Join(dataDir, chainDirs[chain], "settings.json"))
	if err != nil {
		return out
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return out
	}
	for key, v := range m {
		if !known(key) {
			continue
		}
		var list []json.RawMessage
		if json.Unmarshal(v, &list) == nil {
			if len(list) == 0 {
				continue
			}
			v = list[0]
		}
		var raw string
		var x any
		if json.Unmarshal(v, &x) != nil {
			continue
		}
		switch t := x.(type) {
		case string:
			raw = t
		case float64:
			raw = strconv.FormatInt(int64(t), 10)
		case bool:
			raw = "0"
			if t {
				raw = "1"
			}
		default:
			continue
		}
		out[key] = found{raw: raw, source: model.FromSettingsFile, detail: "settings.json"}
	}
	return out
}

// parseFile reads one config file: "key=value" lines, "#" comments,
// [section] headers, "chain.key" prefixes and "nokey" negations.
func parseFile(r io.Reader, chain, source, detail string) (top, section map[string]found, includes []string) {
	top, section = map[string]found{}, map[string]found{}
	current := ""
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		target := current
		if net, k, ok := strings.Cut(key, "."); ok {
			target, key = net, k
		}
		if key == "includeconf" && (target == "" || target == chain) {
			includes = append(includes, value)
			continue
		}
		negated := false
		if name, ok := strings.CutPrefix(key, "no"); ok && known(name) {
			key, negated = name, true
		}
		var m map[string]found
		d := detail
		switch target {
		case "":
			m = top
		case chain:
			m = section
			if d == "" {
				d = "[" + chain + "]"
			}
		default:
			continue // another chain
		}
		if _, seen := m[key]; !seen {
			m[key] = found{raw: value, source: source, detail: d, negated: negated}
		}
	}
	return top, section, includes
}

func known(key string) bool {
	for _, s := range specs {
		if s.key == key {
			return true
		}
	}
	return key == "connect"
}

// parseArgs reads bitcoind's command line: "-key=value", "--key", "-nokey".
// The last value of a key wins, as in Bitcoin Core. Only the keys this
// package needs are kept; the flag itself is the source detail.
func parseArgs(args []string) map[string]found {
	out := map[string]found{}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue
		}
		flag := a
		a = strings.TrimLeft(a, "-")
		key, value, _ := strings.Cut(a, "=")
		negated := false
		if name, ok := strings.CutPrefix(key, "no"); ok && known(name) {
			key, negated = name, true
		}
		switch {
		case known(key):
			out[key] = found{raw: value, source: model.FromCmdline, detail: flag, negated: negated}
		case key == "conf" || key == "datadir" || key == "chain" || key == "testnet" || key == "testnet4" || key == "signet" || key == "regtest":
			out[key] = found{raw: value, negated: negated}
		}
	}
	return out
}

func chainFromArgs(cmd map[string]found) string {
	if c, ok := cmd["chain"]; ok {
		return c.raw
	}
	for _, c := range []string{"testnet4", "signet", "regtest"} {
		if f, ok := cmd[c]; ok && !f.negated && f.raw != "0" {
			return c
		}
	}
	if f, ok := cmd["testnet"]; ok && !f.negated && f.raw != "0" {
		return "test"
	}
	return ""
}

// fileError says why bitcoin.conf couldn't be read, without the error's
// internals.
func fileError(path string, err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("No %s: Bitcoin Core uses its defaults, or the file is elsewhere (set BITCOIN_CONF)", path)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Sprintf("Can't read %s: permission denied", path)
	}
	return fmt.Sprintf("Can't read %s", path)
}

// bitcoindPID returns the process directory of the running bitcoind in
// /proc; "" when there is no /proc (macOS) or no bitcoind visible (another
// container).
func bitcoindPID() string {
	dirs, err := os.ReadDir("/proc")
	if err != nil {
		return ""
	}
	for _, d := range dirs {
		if _, err := strconv.Atoi(d.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", d.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(comm)) == "bitcoind" {
			return filepath.Join("/proc", d.Name())
		}
	}
	return ""
}

// bitcoindArgs returns the arguments of the running bitcoind, from /proc.
// Nil when it can't be found or read.
func bitcoindArgs() []string {
	dir := bitcoindPID()
	if dir == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if err != nil {
		return nil
	}
	parts := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	if len(parts) > 0 {
		return parts[1:]
	}
	return nil
}

// clockTicks is the kernel's USER_HZ, the unit of start times in /proc. It
// is 100 on every Linux architecture node-dash is built for.
const clockTicks = 100

// bitcoindStarted returns when the running bitcoind started, from
// /proc/<pid>/stat and the boot time in /proc/stat. Zero when unknown.
func bitcoindStarted() time.Time {
	_, stat, sys := bitcoindStat()
	return startTime(stat, sys)
}

// bitcoindProcess identifies the running bitcoind by boot, process ID and
// start time in clock ticks; "" when unknown. Unlike bitcoindStarted, which
// is off by up to a second (the boot time is in whole seconds), it is exact:
// a restart right after a save is seen.
func bitcoindProcess() string {
	dir, stat, sys := bitcoindStat()
	return processID(filepath.Base(dir), stat, sys)
}

// bitcoindStat reads the running bitcoind's /proc/<pid>/stat and /proc/stat.
func bitcoindStat() (dir string, stat, sys []byte) {
	dir = bitcoindPID()
	if dir == "" {
		return "", nil, nil
	}
	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return "", nil, nil
	}
	sys, err = os.ReadFile("/proc/stat")
	if err != nil {
		return "", nil, nil
	}
	return dir, stat, sys
}

// startTime reads a process's start time from its /proc/<pid>/stat and the
// boot time in /proc/stat. Zero when they can't be read.
func startTime(stat, sys []byte) time.Time {
	boot, ticks, ok := procTimes(stat, sys)
	if !ok {
		return time.Time{}
	}
	return time.Unix(boot, 0).Add(time.Duration(ticks) * time.Second / clockTicks)
}

// processID identifies process pid by boot time, pid and start time in
// clock ticks, which no other process has. "" when they can't be read.
func processID(pid string, stat, sys []byte) string {
	boot, ticks, ok := procTimes(stat, sys)
	if !ok || pid == "" {
		return ""
	}
	return fmt.Sprintf("%d/%s/%d", boot, pid, ticks)
}

// procTimes reads the boot time from /proc/stat and a process's start time
// in clock ticks since boot from its /proc/<pid>/stat.
func procTimes(stat, sys []byte) (boot, ticks int64, ok bool) {
	// The command name, in parentheses, may contain spaces: count from its end.
	i := bytes.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, 0, false
	}
	fields := strings.Fields(string(stat[i+1:]))
	// Field 22 of stat is the start time; fields[0] is field 3.
	if len(fields) < 20 {
		return 0, 0, false
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(sys), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			boot, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return 0, 0, false
			}
			return boot, ticks, true
		}
	}
	return 0, 0, false
}

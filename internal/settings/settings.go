// Package settings reads the main settings of the node (story #13): the
// value it runs with and where it comes from. It only reads. Of bitcoin.conf
// and bitcoind's command line, only the settings in Keys leave this package:
// never secrets such as rpcpassword.
package settings

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

// Reader reads the settings from bitcoin.conf and bitcoind's command line.
type Reader struct {
	// ConfPath is bitcoin.conf, unless bitcoind's command line names another
	// with -conf.
	ConfPath string
	// DataDir is where relative -conf and includeconf paths start.
	DataDir string
	// args returns bitcoind's command-line arguments, nil when unknown.
	args func() []string
}

// New returns a Reader that finds bitcoind's command line in /proc.
func New(confPath, dataDir string) *Reader {
	return &Reader{ConfPath: confPath, DataDir: dataDir, args: bitcoindArgs}
}

// found is a configured value and where it is set.
type found struct {
	raw     string
	source  string
	detail  string
	negated bool
}

// ReadSettings implements source.SettingsReader. running is nil while the
// node can't be reached.
func (r *Reader) ReadSettings(running *model.RunningSettings) *model.Settings {
	args := r.args()
	cmd := parseArgs(args)

	dataDir := r.DataDir
	if d, ok := cmd["datadir"]; ok && d.raw != "" {
		dataDir = d.raw
	}
	path := r.ConfPath
	if c, ok := cmd["conf"]; ok && c.raw != "" {
		path = c.raw
		if !filepath.IsAbs(path) {
			path = filepath.Join(dataDir, path)
		}
	}
	chain := "main"
	if running != nil && running.Chain != "" {
		chain = running.Chain
	} else if c := chainFromArgs(cmd); c != "" {
		chain = c
	}

	out := &model.Settings{Path: path}
	conf, err := readConf(path, dataDir, chain)
	if err != nil {
		out.FileError = fileError(path, err)
	}
	lookup := func(key string) (found, bool) {
		if f, ok := cmd[key]; ok {
			return f, true
		}
		f, ok := conf[key]
		return f, ok
	}

	for _, s := range specs {
		item := model.Setting{Key: s.key}
		def := s.def
		if s.key == "listen" {
			// -connect implies -listen=0.
			if c, ok := lookup("connect"); ok && !c.negated && c.raw != "0" {
				def = val(0)
			}
		}
		var configured *int64
		f, ok := lookup(s.key)
		if ok {
			v, valid := parse(s.kind, f)
			configured = &v
			if !valid {
				item.Invalid = f.raw
			}
		}
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
	return out
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

// readConf reads bitcoin.conf and its includeconf files for one chain.
// Within a file the first value of a key wins; a value in the chain's
// section wins over one at the top; the main file wins over included files.
func readConf(path, dataDir, chain string) (map[string]found, error) {
	f, err := os.Open(path)
	if err != nil {
		return map[string]found{}, err
	}
	defer f.Close()
	top, section, includes := parseFile(f, chain, model.FromConf, "")

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
	return out, nil
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

// bitcoindArgs returns the arguments of the running bitcoind, from /proc.
// Nil when there is no /proc (macOS), no bitcoind visible (another
// container), or it can't be read.
func bitcoindArgs() []string {
	dirs, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, d := range dirs {
		if _, err := strconv.Atoi(d.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", d.Name(), "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "bitcoind" {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", d.Name(), "cmdline"))
		if err != nil {
			return nil
		}
		parts := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		if len(parts) > 0 {
			return parts[1:]
		}
	}
	return nil
}

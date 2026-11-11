// Package websettings keeps the dashboard's own settings changed on its
// settings page (story #61, decision 0004): the display settings in
// display.json, and one file per access switch turned off, in off/.
//
// A value set here wins over the environment, which wins over the default.
// An access switch is on only when the environment turns it on and it wasn't
// turned off here; nothing here can turn one on.
package websettings

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// The access switches, as named in off/ and in the API.
const (
	Control       = "control"
	Settings      = "settings"
	PeerAddresses = "peer-addresses"
)

// Switches lists the access switches in the order the page shows them.
var Switches = []string{Control, Settings, PeerAddresses}

// labels name the settings in the log and in what a save reports.
var labels = map[string]string{
	"name":        "Name",
	"ancoro":      "Ancoro link",
	"updateCheck": "Update check",
	Control:       "Node control",
	Settings:      "Changing settings",
	PeerAddresses: "Peer addresses",
}

// DefaultDir is the state directory without $STATE_DIRECTORY.
const DefaultDir = "/var/lib/node-dash-web"

const (
	displayFile = "display.json"
	offDir      = "off"
	// MaxNameLen is the longest name: enough for "Satoshi's node in the meter
	// cupboard", short enough for a header and a browser tab.
	MaxNameLen = 40
)

// Env is what the environment says, read once at startup.
type Env struct {
	// Name is NODEDASH_NAME, already checked; "" when not set.
	Name string
	// Ancoro and UpdateCheck are NODEDASH_ANCORO and NODEDASH_UPDATE_CHECK;
	// nil when not set (both are on by default).
	Ancoro, UpdateCheck *bool
	// On lists the switches the environment turns on.
	On map[string]bool
	// Listen is NODEDASH_LISTEN, "" when not set; Hosts is NODEDASH_HOSTS.
	Listen string
	Hosts  []string
	// Docker: node control and changing settings aren't available.
	Docker bool
}

// DefaultListen is the address without NODEDASH_LISTEN.
const DefaultListen = ":8080"

// display is display.json. Only what was changed here is in it.
type display struct {
	V           int     `json:"v"`
	Name        *string `json:"name,omitempty"`
	Ancoro      *bool   `json:"ancoro,omitempty"`
	UpdateCheck *bool   `json:"updateCheck,omitempty"`
}

// Store reads and writes the settings in its directory.
type Store struct {
	dir string
	env Env
	log *slog.Logger
	now func() time.Time

	mu      sync.Mutex
	disp    display
	dispErr string
	off     map[string]time.Time
	offErr  string
	version string
}

// New returns a Store for dir, and reads it. Problems with the files are
// logged, never fatal.
func New(dir string, env Env, log *slog.Logger) *Store {
	s := &Store{dir: dir, env: env, log: log, now: time.Now}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if s.dispErr != "" {
		log.Warn("dashboard settings: " + s.dispErr)
	}
	if s.offErr != "" {
		log.Warn("dashboard settings: " + s.offErr)
	}
	for _, sw := range Switches {
		if t, ok := s.off[sw]; ok && env.On[sw] {
			log.Info("dashboard settings: "+strings.ToLower(labels[sw])+" is off, turned off on the page", "since", t.Format(time.RFC3339))
		}
	}
	return s
}

// load reads the files. Call with mu held.
func (s *Store) load() {
	h := sha256.New()
	s.disp, s.dispErr = display{}, ""
	path := filepath.Join(s.dir, displayFile)
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		s.dispErr = fmt.Sprintf("can't read %s, so it is ignored: %v", path, err)
	default:
		h.Write(b)
		var d display
		if err := json.Unmarshal(b, &d); err != nil {
			s.dispErr = fmt.Sprintf("%s is broken, so it is ignored: %v", path, err)
		} else {
			if d.Name != nil && NameProblem(*d.Name) != "" {
				s.dispErr = fmt.Sprintf("the name in %s can't be used, so it is ignored", path)
				d.Name = nil
			}
			s.disp = d
		}
	}

	// The switches fail closed: an off/ that can't be read turns all off.
	s.off, s.offErr = map[string]time.Time{}, ""
	for _, sw := range Switches {
		p := filepath.Join(s.dir, offDir, sw)
		b, err := os.ReadFile(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			s.offErr = fmt.Sprintf("can't read %s, so node control, changing settings and peer addresses are all off: %v", filepath.Join(s.dir, offDir), err)
			continue
		}
		h.Write([]byte("\x00" + sw + "\x00"))
		h.Write(b)
		t, _ := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
		s.off[sw] = t
	}
	s.version = hex.EncodeToString(h.Sum(nil))[:16]
}

// Name is the dashboard's name: set here, else NODEDASH_NAME, else "Node".
func (s *Store) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name().Value
}

// Ancoro is whether the link to Ancoro shows.
func (s *Store) Ancoro() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ancoro().Value
}

// UpdateCheck is whether the dashboard looks up new releases.
func (s *Store) UpdateCheck() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateCheck().Value
}

// On is whether switch sw is on: the environment turns it on, it wasn't
// turned off here, and off/ could be read.
func (s *Store) On(sw string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.on(sw)
}

func (s *Store) on(sw string) bool {
	_, off := s.off[sw]
	return s.available(sw) && s.env.On[sw] && !off && s.offErr == ""
}

func (s *Store) available(sw string) bool {
	return !s.env.Docker || sw == PeerAddresses
}

func (s *Store) name() model.WebText {
	v := model.WebText{Value: model.DefaultName, Source: model.FromDefaultValue, Fallback: model.DefaultName, FallbackSource: model.FromDefaultValue}
	if s.env.Name != "" {
		v.Fallback, v.FallbackSource = s.env.Name, model.FromEnvironment
		v.Value, v.Source = v.Fallback, v.FallbackSource
	}
	if s.disp.Name != nil {
		v.Value, v.Source = strings.TrimSpace(*s.disp.Name), model.FromWeb
		if v.Value == "" {
			v.Value = model.DefaultName
		}
	}
	return v
}

func boolSetting(env *bool, web *bool) model.WebBool {
	v := model.WebBool{Value: true, Source: model.FromDefaultValue, Fallback: true, FallbackSource: model.FromDefaultValue}
	if env != nil {
		v.Fallback, v.FallbackSource = *env, model.FromEnvironment
		v.Value, v.Source = v.Fallback, v.FallbackSource
	}
	if web != nil {
		v.Value, v.Source = *web, model.FromWeb
	}
	return v
}

func (s *Store) ancoro() model.WebBool { return boolSetting(s.env.Ancoro, s.disp.Ancoro) }
func (s *Store) updateCheck() model.WebBool {
	return boolSetting(s.env.UpdateCheck, s.disp.UpdateCheck)
}

// View reads the files again and returns the page.
func (s *Store) View() model.WebSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return s.view()
}

func (s *Store) view() model.WebSettings {
	out := model.WebSettings{
		Version:     s.version,
		Docker:      s.env.Docker,
		Name:        s.name(),
		Ancoro:      s.ancoro(),
		UpdateCheck: s.updateCheck(),
		Listen:      model.WebText{Value: DefaultListen, Source: model.FromDefaultValue},
		Hosts:       model.WebHosts{Value: []string{}, Source: model.FromDefaultValue},
	}
	if s.env.Listen != "" {
		out.Listen = model.WebText{Value: s.env.Listen, Source: model.FromEnvironment}
	}
	if len(s.env.Hosts) > 0 {
		out.Hosts = model.WebHosts{Value: s.env.Hosts, Source: model.FromEnvironment}
	}
	if why := s.cantSave(); why != "" {
		out.CantSave = why
	} else {
		out.CanSave = true
	}
	for _, w := range []string{s.dispErr, s.offErr} {
		if w != "" {
			out.Warnings = append(out.Warnings, w)
		}
	}
	for _, sw := range Switches {
		x := model.WebSwitch{Key: sw, On: s.on(sw), EnvOn: s.env.On[sw], Available: s.available(sw)}
		if t, ok := s.off[sw]; ok {
			t := t
			x.OffSince = &t
		}
		out.Switches = append(out.Switches, x)
	}
	return out
}

// cantSave says why the directory can't be written, or "".
func (s *Store) cantSave() string {
	why := "node-dash can’t write its settings in " + s.dir
	st, err := os.Stat(s.dir)
	if err != nil || !st.IsDir() || syscall.Access(s.dir, 2 /* W_OK */) != nil {
		if s.env.Docker {
			return why + ": the container has no volume for it (see the README)"
		}
		return why
	}
	return ""
}

// Change is a save from the page.
type Change struct {
	Version string
	// Set holds "name" (a string), "ancoro" and "updateCheck" (true or false).
	Name        *string
	Ancoro      *bool
	UpdateCheck *bool
	// Reset drops values set here: "name", "ancoro", "updateCheck".
	Reset []string
	// Off turns switches off.
	Off []string
}

var (
	// ErrConflict: the files changed since the page read them.
	ErrConflict = errors.New("the dashboard settings were changed elsewhere; reload to see them")
	// ErrNothing: the save changes nothing.
	ErrNothing = errors.New("nothing to save")
	// ErrTurnOn: a switch can't be turned on here (decision 0004).
	ErrTurnOn = errors.New("access can only be turned on on the server, with the installer")
)

// BadRequestError is a body that isn't a Change.
type BadRequestError struct{ Msg string }

func (e *BadRequestError) Error() string { return e.Msg }

// FieldErrors maps a field to why its value is refused.
type FieldErrors map[string]string

func (f FieldErrors) Error() string { return fmt.Sprintf("%d field(s) refused", len(f)) }

// CantSaveError: the files couldn't be written. Nothing changed.
type CantSaveError struct{ Msg string }

func (e *CantSaveError) Error() string { return e.Msg }

// ParseChange reads a save: {"version", "set", "reset", "off"}. Any way of
// turning a switch on is ErrTurnOn; anything else unknown a BadRequestError.
func ParseChange(body []byte) (Change, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return Change{}, &BadRequestError{`expected {"version": "…", "set": {…}, "reset": […], "off": […]}`}
	}
	var c Change
	for k, v := range raw {
		switch k {
		case "version":
			if json.Unmarshal(v, &c.Version) != nil {
				return Change{}, &BadRequestError{"version must be a string"}
			}
		case "set":
			var set map[string]json.RawMessage
			if json.Unmarshal(v, &set) != nil {
				return Change{}, &BadRequestError{"set must be an object"}
			}
			for sk, sv := range set {
				var err error
				switch sk {
				case "name":
					c.Name = new(string)
					err = json.Unmarshal(sv, c.Name)
				case "ancoro":
					c.Ancoro = new(bool)
					err = json.Unmarshal(sv, c.Ancoro)
				case "updateCheck":
					c.UpdateCheck = new(bool)
					err = json.Unmarshal(sv, c.UpdateCheck)
				case Control, Settings, PeerAddresses, "peerAddresses":
					return Change{}, ErrTurnOn
				default:
					return Change{}, &BadRequestError{"unknown setting: " + sk}
				}
				if err != nil {
					return Change{}, &BadRequestError{"wrong type for " + sk}
				}
			}
		case "reset":
			if json.Unmarshal(v, &c.Reset) != nil {
				return Change{}, &BadRequestError{"reset must be a list of names"}
			}
			for _, r := range c.Reset {
				if r != "name" && r != "ancoro" && r != "updateCheck" {
					if _, isSwitch := labels[r]; isSwitch {
						return Change{}, ErrTurnOn
					}
					return Change{}, &BadRequestError{"unknown setting: " + r}
				}
			}
		case "off":
			if json.Unmarshal(v, &c.Off) != nil {
				return Change{}, &BadRequestError{"off must be a list of switches"}
			}
			for _, o := range c.Off {
				if o != Control && o != Settings && o != PeerAddresses {
					return Change{}, &BadRequestError{"unknown switch: " + o}
				}
			}
		case "on":
			return Change{}, ErrTurnOn
		default:
			return Change{}, &BadRequestError{"unknown field: " + k}
		}
	}
	if c.Version == "" {
		return Change{}, &BadRequestError{"version is missing"}
	}
	return c, nil
}

// Apply saves c and returns the page as it is now, with what changed.
func (s *Store) Apply(c Change) (model.WebSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if c.Version != s.version {
		return model.WebSettings{}, ErrConflict
	}
	if c.Name != nil {
		if p := NameProblem(*c.Name); p != "" {
			return model.WebSettings{}, FieldErrors{"name": p}
		}
	}

	before := s.view()
	next := s.disp
	next.V = 1
	// A value that is already in effect isn't a change: it would only pin
	// it here, and say "Node" → "Node".
	if c.Name != nil {
		n := strings.TrimSpace(*c.Name)
		effective := n
		if effective == "" {
			effective = model.DefaultName
		}
		if effective != before.Name.Value {
			next.Name = &n
		}
	}
	if c.Ancoro != nil && *c.Ancoro != before.Ancoro.Value {
		next.Ancoro = c.Ancoro
	}
	if c.UpdateCheck != nil && *c.UpdateCheck != before.UpdateCheck.Value {
		next.UpdateCheck = c.UpdateCheck
	}
	for _, r := range c.Reset {
		switch r {
		case "name":
			next.Name = nil
		case "ancoro":
			next.Ancoro = nil
		case "updateCheck":
			next.UpdateCheck = nil
		}
	}
	var off []string
	for _, sw := range c.Off {
		if s.on(sw) {
			off = append(off, sw)
		}
	}
	dispChanged := !sameDisplay(s.disp, next)
	if !dispChanged && len(off) == 0 {
		return model.WebSettings{}, ErrNothing
	}
	if why := s.cantSave(); why != "" {
		return model.WebSettings{}, &CantSaveError{why}
	}

	if dispChanged {
		b, _ := json.Marshal(next)
		if err := writeAtomic(s.dir, displayFile, append(b, '\n')); err != nil {
			return model.WebSettings{}, &CantSaveError{"node-dash can’t write " + filepath.Join(s.dir, displayFile) + ": " + err.Error()}
		}
	}
	if len(off) > 0 {
		dir := filepath.Join(s.dir, offDir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return model.WebSettings{}, &CantSaveError{"node-dash can’t write " + dir + ": " + err.Error()}
		}
		stamp := []byte(s.now().UTC().Format(time.RFC3339) + "\n")
		for _, sw := range off {
			if err := writeAtomic(dir, sw, stamp); err != nil {
				return model.WebSettings{}, &CantSaveError{"node-dash can’t write " + filepath.Join(dir, sw) + ": " + err.Error()}
			}
		}
	}

	s.load()
	after := s.view()
	saved := describe(before, after, off)
	for _, line := range saved {
		s.log.Info("dashboard settings: " + line)
	}
	after.Saved = saved
	return after, nil
}

func sameDisplay(a, b display) bool {
	ja, _ := json.Marshal(display{Name: a.Name, Ancoro: a.Ancoro, UpdateCheck: a.UpdateCheck})
	jb, _ := json.Marshal(display{Name: b.Name, Ancoro: b.Ancoro, UpdateCheck: b.UpdateCheck})
	return bytes.Equal(ja, jb)
}

// describe says what changed: `Name "Node" → "My node"`, `Ancoro link on →
// off (from the environment)`, `Node control turned off`.
func describe(before, after model.WebSettings, off []string) []string {
	var out []string
	from := func(source string) string {
		if source == model.FromWeb {
			return ""
		}
		return " (" + map[string]string{model.FromDefaultValue: "the default", model.FromEnvironment: "the environment"}[source] + ")"
	}
	if before.Name.Value != after.Name.Value || before.Name.Source != after.Name.Source {
		out = append(out, fmt.Sprintf("%s %q → %q%s", labels["name"], before.Name.Value, after.Name.Value, from(after.Name.Source)))
	}
	onOff := map[bool]string{true: "on", false: "off"}
	for _, x := range []struct {
		key           string
		before, after model.WebBool
	}{{"ancoro", before.Ancoro, after.Ancoro}, {"updateCheck", before.UpdateCheck, after.UpdateCheck}} {
		if x.before.Value != x.after.Value || x.before.Source != x.after.Source {
			out = append(out, fmt.Sprintf("%s %s → %s%s", labels[x.key], onOff[x.before.Value], onOff[x.after.Value], from(x.after.Source)))
		}
	}
	for _, sw := range off {
		out = append(out, labels[sw]+" turned off")
	}
	return out
}

// writeAtomic writes dir/name through a temporary file in dir, fsync and
// rename, mode 0600: a reader sees the old file or the new one.
func writeAtomic(dir, name string, b []byte) error {
	f, err := os.CreateTemp(dir, "."+name+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// NameProblem says why v can't be the dashboard's name, or "". An empty
// name is fine: it means "Node".
func NameProblem(v string) string {
	v = strings.TrimSpace(v)
	if !utf8.ValidString(v) {
		return "Not valid UTF-8."
	}
	if n := utf8.RuneCountInString(v); n > MaxNameLen {
		return fmt.Sprintf("At most %d characters; this is %d.", MaxNameLen, n)
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return "No control characters, such as a newline."
		}
	}
	return ""
}

// Dir picks this package's directory from $STATE_DIRECTORY, which systemd
// fills with one entry per StateDirectory= line, separated by ":", in no
// promised order. Each is chosen by its name (decision 0004).
func Dir(stateDirectory, name, fallback string) string {
	for _, d := range strings.Split(stateDirectory, ":") {
		if d != "" && filepath.Base(d) == name {
			return d
		}
	}
	return fallback
}

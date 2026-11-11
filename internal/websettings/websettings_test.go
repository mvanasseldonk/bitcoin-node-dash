package websettings

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

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func ptr[T any](v T) *T { return &v }

func allOn() map[string]bool {
	return map[string]bool{Control: true, Settings: true, PeerAddresses: true}
}

func newStore(t *testing.T, env Env) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	return New(dir, env, quiet()), dir
}

func apply(t *testing.T, s *Store, body string) (model.WebSettings, error) {
	t.Helper()
	c, err := ParseChange([]byte(body))
	if err != nil {
		return model.WebSettings{}, err
	}
	return s.Apply(c)
}

// A value set here wins over the environment, which wins over the default;
// "reset" goes back to the environment.
func TestPrecedence(t *testing.T) {
	s, _ := newStore(t, Env{Name: "From env", Ancoro: ptr(false)})
	v := s.View()
	if v.Name.Value != "From env" || v.Name.Source != model.FromEnvironment {
		t.Fatalf("name: %+v", v.Name)
	}
	if v.Ancoro.Value || v.Ancoro.Source != model.FromEnvironment {
		t.Fatalf("ancoro: %+v", v.Ancoro)
	}
	if !v.UpdateCheck.Value || v.UpdateCheck.Source != model.FromDefaultValue {
		t.Fatalf("update check: %+v", v.UpdateCheck)
	}

	v, err := apply(t, s, `{"version":"`+v.Version+`","set":{"name":"  Satoshi’s node ","ancoro":true}}`)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "Satoshi’s node" || v.Name.Source != model.FromWeb || v.Name.Fallback != "From env" {
		t.Fatalf("after set: %q %+v", s.Name(), v.Name)
	}
	if !s.Ancoro() || v.Ancoro.Source != model.FromWeb {
		t.Fatalf("ancoro after set: %+v", v.Ancoro)
	}
	if len(v.Saved) != 2 || !strings.Contains(v.Saved[0], `"From env" → "Satoshi’s node"`) {
		t.Fatalf("saved: %q", v.Saved)
	}

	v, err = apply(t, s, `{"version":"`+v.Version+`","reset":["name","ancoro"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "From env" || s.Ancoro() || v.Name.Source != model.FromEnvironment {
		t.Fatalf("after reset: %q %v %+v", s.Name(), s.Ancoro(), v.Name)
	}

	// An empty name is the default name.
	v, err = apply(t, s, `{"version":"`+v.Version+`","set":{"name":""}}`)
	if err != nil || s.Name() != model.DefaultName {
		t.Fatalf("empty name: %q, %v", s.Name(), err)
	}
}

func TestNameValidation(t *testing.T) {
	for _, ok := range []string{"", "Node", strings.Repeat("ä", MaxNameLen), " padded "} {
		if p := NameProblem(ok); p != "" {
			t.Errorf("NameProblem(%q) = %q, want none", ok, p)
		}
	}
	for _, bad := range []string{strings.Repeat("x", MaxNameLen+1), "two\nlines", "tab\there", "bad \xff"} {
		if NameProblem(bad) == "" {
			t.Errorf("NameProblem(%q): want a problem", bad)
		}
	}
	s, _ := newStore(t, Env{})
	_, err := apply(t, s, `{"version":"`+s.View().Version+`","set":{"name":"`+strings.Repeat("x", 41)+`"}}`)
	var fields FieldErrors
	if !errors.As(err, &fields) || fields["name"] == "" {
		t.Fatalf("want a field error, got %v", err)
	}
	if s.Name() != model.DefaultName {
		t.Fatalf("a refused name was saved: %q", s.Name())
	}
}

// A switch can be turned off and stays off, also for a new Store (a
// restart); nothing turns it on again.
func TestSwitchesOnlyOff(t *testing.T) {
	env := Env{On: allOn()}
	s, dir := newStore(t, env)
	if !s.On(Control) || !s.On(PeerAddresses) {
		t.Fatal("switches the environment turns on must be on")
	}
	s.now = func() time.Time { return time.Date(2026, 11, 11, 13, 0, 0, 0, time.UTC) }
	v, err := apply(t, s, `{"version":"`+s.View().Version+`","off":["control"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if s.On(Control) || !s.On(Settings) {
		t.Fatal("only node control must be off")
	}
	if v.Switches[0].OffSince == nil || !v.Switches[0].OffSince.Equal(s.now()) || v.Saved[0] != "Node control turned off" {
		t.Fatalf("switch: %+v, saved %q", v.Switches[0], v.Saved)
	}
	st, err := os.Stat(filepath.Join(dir, "off", Control))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("off file: %v %v", st, err)
	}

	if New(dir, env, quiet()).On(Control) {
		t.Fatal("node control must stay off after a restart")
	}

	for _, body := range []string{
		`{"version":"x","on":["control"]}`,
		`{"version":"x","set":{"control":true}}`,
		`{"version":"x","set":{"peerAddresses":true}}`,
		`{"version":"x","reset":["control"]}`,
	} {
		if _, err := ParseChange([]byte(body)); !errors.Is(err, ErrTurnOn) {
			t.Errorf("%s: want ErrTurnOn, got %v", body, err)
		}
	}

	// The installer removes the file to turn it on again.
	if err := os.Remove(filepath.Join(dir, "off", Control)); err != nil {
		t.Fatal(err)
	}
	if !New(dir, env, quiet()).On(Control) {
		t.Fatal("without the off file, node control is on again")
	}
}

// A switch the environment doesn't turn on stays off, and isn't recorded.
func TestSwitchOffOnServer(t *testing.T) {
	s, dir := newStore(t, Env{On: map[string]bool{Settings: true}})
	if s.On(Control) {
		t.Fatal("node control isn't on in the environment")
	}
	if _, err := apply(t, s, `{"version":"`+s.View().Version+`","off":["control"]}`); !errors.Is(err, ErrNothing) {
		t.Fatalf("want ErrNothing, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "off")); err == nil {
		t.Fatal("nothing must be written")
	}
}

// display.json fails open: ignored, with a warning, and repaired by a save.
// off/ fails closed: all switches off.
func TestBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "display.json"), []byte(`{"name": "x"`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(dir, Env{Name: "From env", On: allOn()}, quiet())
	v := s.View()
	if s.Name() != "From env" || len(v.Warnings) != 1 || !strings.Contains(v.Warnings[0], "ignored") {
		t.Fatalf("broken display.json: %q %q", s.Name(), v.Warnings)
	}
	if _, err := apply(t, s, `{"version":"`+v.Version+`","set":{"name":"Fixed"}}`); err != nil || s.Name() != "Fixed" {
		t.Fatalf("a save must repair it: %q %v", s.Name(), err)
	}

	if os.Getuid() == 0 {
		t.Skip("root reads any file")
	}
	off := filepath.Join(dir, "off")
	if err := os.MkdirAll(off, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(off, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(off, 0o700)
	s = New(dir, Env{On: allOn()}, quiet())
	for _, sw := range Switches {
		if s.On(sw) {
			t.Errorf("%s must be off when off/ can't be read", sw)
		}
	}
	if w := s.View().Warnings; len(w) != 1 || !strings.Contains(w[0], "all off") {
		t.Errorf("warnings: %q", w)
	}
}

func TestConflict(t *testing.T) {
	s, _ := newStore(t, Env{})
	old := s.View().Version
	if _, err := apply(t, s, `{"version":"`+old+`","set":{"ancoro":false}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := apply(t, s, `{"version":"`+old+`","set":{"name":"Other tab"}}`); !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestCantSave(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "missing"), Env{Docker: true, On: allOn()}, quiet())
	v := s.View()
	if v.CanSave || !strings.Contains(v.CantSave, "no volume") {
		t.Fatalf("view: %+v", v)
	}
	if v.Switches[0].Available || !v.Switches[2].Available || s.On(Control) || !s.On(PeerAddresses) {
		t.Fatalf("in Docker only peer addresses is available: %+v", v.Switches)
	}
	var cant *CantSaveError
	if _, err := apply(t, s, `{"version":"`+v.Version+`","set":{"name":"x"}}`); !errors.As(err, &cant) {
		t.Fatalf("want CantSaveError, got %v", err)
	}
}

func TestParseChangeRefusesUnknown(t *testing.T) {
	var bad *BadRequestError
	for _, body := range []string{`{"set":{"name":"x"}}`, `{"version":"v","colour":"red"}`, `{"version":"v","set":{"listen":":80"}}`, `{"version":"v","off":["hosts"]}`, `not json`} {
		if _, err := ParseChange([]byte(body)); !errors.As(err, &bad) {
			t.Errorf("%s: want a BadRequestError, got %v", body, err)
		}
	}
}

func TestDir(t *testing.T) {
	for _, tc := range []struct{ env, name, want string }{
		{"/var/lib/node-dash-settings:/var/lib/node-dash-web", "node-dash-web", "/var/lib/node-dash-web"},
		{"/var/lib/node-dash-web:/var/lib/node-dash-settings", "node-dash-settings", "/var/lib/node-dash-settings"},
		{"/var/lib/node-dash-web", "node-dash-settings", "fallback"},
		{"", "node-dash-web", "fallback"},
	} {
		if got := Dir(tc.env, tc.name, "fallback"); got != tc.want {
			t.Errorf("Dir(%q, %q) = %q, want %q", tc.env, tc.name, got, tc.want)
		}
	}
}

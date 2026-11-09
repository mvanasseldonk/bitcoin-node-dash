package control

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeSystemd answers "systemctl show" with its state and records actions.
type fakeSystemd struct {
	load, active, sub string
	timeout           string
	since             int64 // ActiveEnterTimestamp, unix seconds
	actionErr         error
	actions           []string
}

func (f *fakeSystemd) run(_ context.Context, args ...string) (string, error) {
	if args[0] == "show" {
		return "LoadState=" + f.load + "\nActiveState=" + f.active + "\nSubState=" + f.sub +
			"\nActiveEnterTimestamp=@" + strconv.FormatInt(f.since, 10) + "\nTimeoutStopUSec=" + f.timeout + "\n", nil
	}
	f.actions = append(f.actions, strings.Join(args, " "))
	return "", f.actionErr
}

func newTest(f *fakeSystemd, ready *bool) (*Controller, *time.Time) {
	now := time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC)
	c := New("bitcoind.service", func() bool { return *ready }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.run = f.run
	c.now = func() time.Time { return now }
	return c, &now
}

func TestState(t *testing.T) {
	ready := true
	c, _ := newTest(&fakeSystemd{load: "loaded", active: "active", sub: "running", timeout: "10min", since: 1731170000}, &ready)
	s := c.State(context.Background())
	if !s.Available || s.ActiveState != "active" || s.StopTimeoutSec != 600 || s.Since.Unix() != 1731170000 {
		t.Fatalf("unexpected state: %+v", s)
	}
	if !slices.Equal(s.Actions, []string{Restart, Stop}) {
		t.Fatalf("actions = %v, want restart, stop", s.Actions)
	}
}

func TestActionsFitTheState(t *testing.T) {
	for active, want := range map[string][]string{
		"active":       {Restart, Stop},
		"inactive":     {Start},
		"failed":       {Start},
		"activating":   {},
		"deactivating": {},
	} {
		ready := true
		c, _ := newTest(&fakeSystemd{load: "loaded", active: active, timeout: "10min"}, &ready)
		if got := c.State(context.Background()).Actions; !slices.Equal(got, want) {
			t.Errorf("%s: actions = %v, want %v", active, got, want)
		}
	}
}

func TestUnknownUnit(t *testing.T) {
	ready := true
	c, _ := newTest(&fakeSystemd{load: "not-found", active: "inactive"}, &ready)
	s := c.State(context.Background())
	if s.Available || len(s.Actions) != 0 || !strings.Contains(s.Error, "BITCOIN_SERVICE") {
		t.Fatalf("unexpected state for a missing unit: %+v", s)
	}
}

func TestRestartProgress(t *testing.T) {
	f := &fakeSystemd{load: "loaded", active: "active", sub: "running", timeout: "10min"}
	ready := true
	c, now := newTest(f, &ready)

	s, err := c.Do(context.Background(), Restart)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.actions, []string{"--no-ask-password --no-block restart bitcoind.service"}) {
		t.Fatalf("systemctl calls = %v", f.actions)
	}
	if s.Pending == nil || s.Pending.Action != Restart || len(s.Actions) != 0 {
		t.Fatalf("want a pending restart and no actions, got %+v", s)
	}

	// systemd hasn't picked up the job yet: still pending.
	*now = now.Add(cacheTTL)
	if s := c.State(context.Background()); s.Pending == nil {
		t.Fatal("pending ended before the node went down")
	}

	// Stopping, then starting: still pending.
	f.active, ready = "deactivating", false
	*now = now.Add(cacheTTL)
	if s := c.State(context.Background()); s.Pending == nil {
		t.Fatal("pending ended while stopping")
	}
	f.active, f.since = "active", now.Add(cacheTTL).Unix()
	*now = now.Add(cacheTTL)
	if s := c.State(context.Background()); s.Pending == nil {
		t.Fatal("pending ended while the node is still warming up")
	}

	// Active and answering RPC: done.
	ready = true
	*now = now.Add(cacheTTL)
	if s := c.State(context.Background()); s.Pending != nil || !slices.Equal(s.Actions, []string{Restart, Stop}) {
		t.Fatalf("want restart finished, got %+v", s)
	}
}

func TestStopEndsWhenInactive(t *testing.T) {
	f := &fakeSystemd{load: "loaded", active: "active", timeout: "10min"}
	ready := true
	c, now := newTest(f, &ready)
	if _, err := c.Do(context.Background(), Stop); err != nil {
		t.Fatal(err)
	}
	f.active = "inactive"
	*now = now.Add(cacheTTL)
	if s := c.State(context.Background()); s.Pending != nil || !slices.Equal(s.Actions, []string{Start}) {
		t.Fatalf("want stop finished with start available, got %+v", s)
	}
}

func TestActionNotAllowedNow(t *testing.T) {
	f := &fakeSystemd{load: "loaded", active: "active", timeout: "10min"}
	ready := true
	c, _ := newTest(f, &ready)
	if _, err := c.Do(context.Background(), Start); !errors.Is(err, ErrNotAllowedNow) {
		t.Fatalf("start while running: got %v, want ErrNotAllowedNow", err)
	}
	if _, err := c.Do(context.Background(), "reboot"); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("unknown action: got %v", err)
	}
	if len(f.actions) != 0 {
		t.Fatalf("systemctl was called: %v", f.actions)
	}
}

func TestPermissionDenied(t *testing.T) {
	f := &fakeSystemd{load: "loaded", active: "active", timeout: "10min",
		actionErr: errors.New("Failed to restart bitcoind.service: Access denied")}
	ready := true
	c, _ := newTest(f, &ready)
	s, err := c.Do(context.Background(), Restart)
	if !errors.Is(err, ErrNotPermitted) || !s.PermissionDenied || s.Pending != nil {
		t.Fatalf("want permission denied, got %v %+v", err, s)
	}
}

func TestParseTimeoutSec(t *testing.T) {
	for in, want := range map[string]int64{
		"1min 30s": 90, "10min": 600, "1h": 3600, "45s": 45, "500ms": 0, "infinity": -1, "": 0, "garbage": 0,
	} {
		if got := parseTimeoutSec(in); got != want {
			t.Errorf("parseTimeoutSec(%q) = %d, want %d", in, got, want)
		}
	}
}

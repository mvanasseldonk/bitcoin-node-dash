// Package control starts, stops and restarts Bitcoin Core through systemd
// (story #10, decision docs/decisions/0001-controlling-bitcoind.md).
//
// It runs `systemctl` as the dashboard's own user; a polkit rule must allow
// that user to manage the unit. Stopping always goes through systemd, never
// RPC "stop": with Restart=always, systemd would start the node again.
package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Actions the dashboard can run.
const (
	Start   = "start"
	Stop    = "stop"
	Restart = "restart"
)

// MinStopTimeout is the shortest stop timeout considered safe: Bitcoin Core
// may need minutes to flush its cache before it exits.
const MinStopTimeout = 5 * time.Minute

// pendingTimeout ends the progress of an action that never finishes.
const pendingTimeout = 20 * time.Minute

// cacheTTL limits how often systemctl is run, however many tabs are open.
const cacheTTL = 2 * time.Second

var (
	// ErrNotPermitted means systemd (polkit) refused the action.
	ErrNotPermitted = errors.New("not permitted to control the node: install the polkit rule (see the README)")
	// ErrNotAllowedNow means the action doesn't fit the node's current state.
	ErrNotAllowedNow = errors.New("this action isn't possible right now")
	// ErrUnknownAction is returned for anything but start, stop and restart.
	ErrUnknownAction = errors.New("unknown action")
)

// Pending is an action that was started and hasn't reached its goal yet.
type Pending struct {
	Action    string    `json:"action"`
	StartedAt time.Time `json:"startedAt"`
}

// State is what the dashboard shows about the node's service.
type State struct {
	Unit string `json:"unit"`
	// Available is false when the unit doesn't exist or systemd can't be asked.
	Available bool `json:"available"`
	// ActiveState and SubState are systemd's, e.g. "active"/"running",
	// "deactivating"/"stop-sigterm", "inactive"/"dead", "failed"/"failed".
	ActiveState string `json:"activeState"`
	SubState    string `json:"subState"`
	// Since is when the service last became active (zero when unknown).
	Since time.Time `json:"since"`
	// StopTimeoutSec is how long systemd waits for a clean stop (0 = unknown, -1 = infinite).
	StopTimeoutSec int64 `json:"stopTimeoutSec"`
	// Actions that fit the current state.
	Actions []string `json:"actions"`
	Pending *Pending `json:"pending"`
	// PermissionDenied is set after systemd refused an action.
	PermissionDenied bool   `json:"permissionDenied"`
	Error            string `json:"error"`
}

// Runner runs systemctl with the given arguments and returns its stdout.
type Runner func(ctx context.Context, args ...string) (string, error)

// Controller controls one systemd unit.
type Controller struct {
	unit  string
	run   Runner
	ready func() bool
	log   *slog.Logger
	now   func() time.Time

	mu        sync.Mutex
	cached    State
	cachedAt  time.Time
	pending   *Pending
	denied    bool
	lastError string
}

// New returns a Controller for unit. ready reports whether the node answers
// RPC normally (not starting); it ends the progress of start and restart.
func New(unit string, ready func() bool, log *slog.Logger) *Controller {
	return NewWithRunner(unit, systemctl, ready, log)
}

// NewWithRunner is New with another way to run systemctl, for tests.
func NewWithRunner(unit string, run Runner, ready func() bool, log *slog.Logger) *Controller {
	return &Controller{unit: unit, run: run, ready: ready, log: log, now: time.Now}
}

// systemctl runs the real systemctl, with a timeout and stable messages.
func systemctl(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	cmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C", "SYSTEMD_PAGER=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), errors.New(msg)
	}
	return stdout.String(), nil
}

// State returns the unit's state, at most cacheTTL old.
func (c *Controller) State(ctx context.Context) State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now().Sub(c.cachedAt) < cacheTTL {
		return c.cached
	}
	return c.refreshLocked(ctx)
}

func (c *Controller) refreshLocked(ctx context.Context) State {
	s := State{Unit: c.unit, Actions: []string{}}
	out, err := c.run(ctx, "show", "--timestamp=unix",
		"-p", "LoadState,ActiveState,SubState,ActiveEnterTimestamp,TimeoutStopUSec", c.unit)
	props := parseProps(out)
	switch {
	case err != nil:
		s.Error = "can't ask systemd about " + c.unit + ": " + err.Error()
	case props["LoadState"] != "loaded":
		s.Error = "no systemd service " + c.unit + " found; set BITCOIN_SERVICE to the right one"
	default:
		s.Available = true
		s.ActiveState, s.SubState = props["ActiveState"], props["SubState"]
		s.Since = parseUnixTimestamp(props["ActiveEnterTimestamp"])
		s.StopTimeoutSec = parseTimeoutSec(props["TimeoutStopUSec"])
	}

	// Progress of a running action ends when its goal is reached, or on timeout.
	if p := c.pending; p != nil {
		done := false
		switch p.Action {
		case Stop:
			done = s.ActiveState == "inactive" || s.ActiveState == "failed"
		default:
			// Became active again after the action (ActiveEnterTimestamp has
			// a resolution of a second), and answers RPC normally.
			restarted := !s.Since.Before(p.StartedAt.Truncate(time.Second))
			done = (s.ActiveState == "active" && restarted && c.ready()) || s.ActiveState == "failed"
		}
		if s.ActiveState == "failed" {
			c.lastError = "the node stopped with an error; see: journalctl -u " + c.unit + " -n 50"
		}
		if done || c.now().Sub(p.StartedAt) > pendingTimeout {
			c.pending = nil
		}
	}

	s.Pending = c.pending
	s.PermissionDenied = c.denied
	if s.Error == "" {
		s.Error = c.lastError
	}
	if s.Available && s.Pending == nil {
		s.Actions = allowedActions(s.ActiveState)
	}
	c.cached, c.cachedAt = s, c.now()
	return s
}

// allowedActions lists the actions that make sense in a systemd ActiveState.
func allowedActions(active string) []string {
	switch active {
	case "active":
		return []string{Restart, Stop}
	case "inactive", "failed":
		return []string{Start}
	default: // activating, deactivating, reloading: wait
		return []string{}
	}
}

// Do starts an action. It returns as soon as systemd has accepted the job;
// State shows the progress.
func (c *Controller) Do(ctx context.Context, action string) (State, error) {
	if action != Start && action != Stop && action != Restart {
		return State{}, ErrUnknownAction
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	s := c.refreshLocked(ctx)
	allowed := false
	for _, a := range s.Actions {
		allowed = allowed || a == action
	}
	if !allowed {
		return s, ErrNotAllowedNow
	}

	_, err := c.run(ctx, "--no-ask-password", "--no-block", action, c.unit)
	if err != nil {
		if isPermissionError(err) {
			c.denied = true
			c.log.Warn("node control not permitted", "action", action, "unit", c.unit, "err", err)
			err = ErrNotPermitted
		} else {
			c.log.Warn("node control failed", "action", action, "unit", c.unit, "err", err)
			err = fmt.Errorf("systemctl %s %s: %w", action, c.unit, err)
		}
		c.lastError = err.Error()
		c.cachedAt = time.Time{}
		return c.refreshLocked(ctx), err
	}

	c.log.Info("node control", "action", action, "unit", c.unit)
	c.denied, c.lastError = false, ""
	c.pending = &Pending{Action: action, StartedAt: c.now()}
	c.cachedAt = time.Time{}
	return c.refreshLocked(ctx), nil
}

func isPermissionError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Access denied") ||
		strings.Contains(msg, "Interactive authentication required") ||
		strings.Contains(msg, "not authorized")
}

// parseProps parses systemctl show's "Key=value" lines.
func parseProps(out string) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[k] = v
		}
	}
	return props
}

// parseUnixTimestamp parses "@1731170000" (systemctl --timestamp=unix).
func parseUnixTimestamp(v string) time.Time {
	sec, err := strconv.ParseInt(strings.TrimPrefix(v, "@"), 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// parseTimeoutSec parses systemd's human durations, e.g. "1min 30s", "10min",
// "infinity". It returns 0 when unknown and -1 for infinity.
func parseTimeoutSec(v string) int64 {
	v = strings.TrimSpace(v)
	switch v {
	case "":
		return 0
	case "infinity":
		return -1
	}
	units := []struct {
		suffix string
		d      time.Duration
	}{{"min", time.Minute}, {"ms", time.Millisecond}, {"us", time.Microsecond}, {"h", time.Hour}, {"d", 24 * time.Hour}, {"s", time.Second}}
	var total time.Duration
	for _, part := range strings.Fields(v) {
		matched := false
		for _, u := range units {
			if n, ok := strings.CutSuffix(part, u.suffix); ok {
				f, err := strconv.ParseFloat(n, 64)
				if err != nil {
					return 0
				}
				total += time.Duration(f * float64(u.d))
				matched = true
				break
			}
		}
		if !matched {
			return 0
		}
	}
	return int64(total / time.Second)
}

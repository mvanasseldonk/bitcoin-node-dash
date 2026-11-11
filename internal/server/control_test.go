package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/control"
)

// fakeSystemctl reports a running unit and counts actions.
type fakeSystemctl struct{ actions int }

func (f *fakeSystemctl) run(_ context.Context, args ...string) (string, error) {
	if args[0] == "show" {
		return "LoadState=loaded\nActiveState=active\nSubState=running\nTimeoutStopUSec=10min\n", nil
	}
	f.actions++
	return "", nil
}

func controlHandler(t *testing.T) (http.Handler, *fakeSystemctl) {
	t.Helper()
	f := &fakeSystemctl{}
	c := control.NewWithRunner("bitcoind.service", f.run, func() bool { return true }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return New(fixedSnapshot{}, fstest.MapFS{}, WithControl(c, []string{"bitcoin-node.lan"}, func() bool { return true })), f
}

func post(h http.Handler, host, contentType, origin, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/control", strings.NewReader(body))
	req.Host = host
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestControlAccepted(t *testing.T) {
	for _, tc := range []struct{ host, origin string }{
		{"192.168.1.119:8080", "http://192.168.1.119:8080"}, // the browser on the LAN
		{"localhost:8080", ""},                              // curl on the server, no Origin
		{"bitcoin-node.lan:8080", "http://bitcoin-node.lan:8080"},
		{"[fd00::1]:8080", "http://[fd00::1]:8080"},
	} {
		h, f := controlHandler(t) // fresh: a pending restart blocks the next action
		rec := post(h, tc.host, "application/json", tc.origin, `{"action":"restart"}`)
		if rec.Code != http.StatusAccepted || f.actions != 1 {
			t.Errorf("%s: got %d (%s), %d actions; want 202 and 1 action", tc.host, rec.Code, rec.Body.String(), f.actions)
		}
	}
}

func TestControlRefused(t *testing.T) {
	h, f := controlHandler(t)
	for _, tc := range []struct {
		name, host, contentType, origin, body string
		want                                  int
	}{
		{"form post from a web page", "192.168.1.119:8080", "application/x-www-form-urlencoded", "", "action=restart", http.StatusUnsupportedMediaType},
		{"text/plain from a web page", "192.168.1.119:8080", "text/plain", "", `{"action":"restart"}`, http.StatusUnsupportedMediaType},
		{"other site's origin", "192.168.1.119:8080", "application/json", "http://evil.example", `{"action":"restart"}`, http.StatusForbidden},
		{"null origin", "192.168.1.119:8080", "application/json", "null", `{"action":"restart"}`, http.StatusForbidden},
		{"DNS rebinding", "evil.example:8080", "application/json", "http://evil.example:8080", `{"action":"restart"}`, http.StatusForbidden},
		{"unknown action", "192.168.1.119:8080", "application/json", "", `{"action":"reboot"}`, http.StatusBadRequest},
		{"start while running", "192.168.1.119:8080", "application/json", "", `{"action":"start"}`, http.StatusConflict},
	} {
		if rec := post(h, tc.host, tc.contentType, tc.origin, tc.body); rec.Code != tc.want {
			t.Errorf("%s: got %d (%s), want %d", tc.name, rec.Code, rec.Body.String(), tc.want)
		}
	}
	if f.actions != 0 {
		t.Fatalf("systemctl actions ran %d times; want none", f.actions)
	}

	// GET never acts.
	req := httptest.NewRequest(http.MethodGet, "http://192.168.1.119:8080/api/control?action=restart", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if f.actions != 0 {
		t.Fatal("GET ran an action")
	}
}

func TestControlOffByDefault(t *testing.T) {
	h := New(fixedSnapshot{}, fstest.MapFS{"index.html": {Data: []byte("x")}})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/api/control", strings.NewReader(`{"action":"stop"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s /api/control without control: got %d, want 404", method, rec.Code)
		}
	}
}

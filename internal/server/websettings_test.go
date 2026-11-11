package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/websettings"
)

func webSettingsHandler(t *testing.T) (http.Handler, *websettings.Store) {
	t.Helper()
	store := websettings.New(t.TempDir(), websettings.Env{On: map[string]bool{websettings.Control: true}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return New(fixedSnapshot{}, fstest.MapFS{}, WithWebSettings(store, []string{"bitcoin-node.lan"})), store
}

func dashboardRequest(h http.Handler, method, origin, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://192.168.1.119:8080/api/dashboard", strings.NewReader(body))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWebSettingsSave(t *testing.T) {
	h, store := webSettingsHandler(t)
	rec := dashboardRequest(h, http.MethodGet, "", "")
	var v model.WebSettings
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	rec = dashboardRequest(h, http.MethodPost, "http://192.168.1.119:8080", `{"version":"`+v.Version+`","set":{"name":"Living room"}}`)
	if rec.Code != http.StatusOK || store.Name() != "Living room" {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body)
	}
	// The same version again is a conflict: another tab saved first.
	rec = dashboardRequest(h, http.MethodPost, "", `{"version":"`+v.Version+`","set":{"name":"Kitchen"}}`)
	if rec.Code != http.StatusConflict || store.Name() != "Living room" {
		t.Fatalf("stale version: %d %s", rec.Code, rec.Body)
	}
}

func TestWebSettingsRefused(t *testing.T) {
	h, store := webSettingsHandler(t)
	version := store.View().Version
	for _, tc := range []struct {
		name, origin, body string
		want               int
	}{
		{"other site's origin", "http://evil.example", `{"version":"` + version + `","set":{"name":"x"}}`, http.StatusForbidden},
		{"turn a switch on", "", `{"version":"` + version + `","on":["control"]}`, http.StatusForbidden},
		{"a name too long", "", `{"version":"` + version + `","set":{"name":"` + strings.Repeat("x", 41) + `"}}`, http.StatusUnprocessableEntity},
		{"an unknown field", "", `{"version":"` + version + `","set":{"listen":":80"}}`, http.StatusBadRequest},
		{"nothing changes", "", `{"version":"` + version + `","set":{"name":"Node"}}`, http.StatusBadRequest},
	} {
		if rec := dashboardRequest(h, http.MethodPost, tc.origin, tc.body); rec.Code != tc.want {
			t.Errorf("%s: got %d (%s), want %d", tc.name, rec.Code, rec.Body, tc.want)
		}
	}
	if store.Name() != model.DefaultName || !store.On(websettings.Control) {
		t.Fatal("a refused request changed something")
	}
}

// With NODEDASH_WEB_SETTINGS=0 there is no API and no button.
func TestWebSettingsOff(t *testing.T) {
	h := New(fixedSnapshot{}, fstest.MapFS{})
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		if rec := dashboardRequest(h, m, "", `{}`); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: got %d, want 404", m, rec.Code)
		}
	}
}

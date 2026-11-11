package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/settings"
)

type fixedDetails struct{ invalidated int }

func (f *fixedDetails) Get(context.Context) *model.Details {
	return &model.Details{
		Blockchain: &model.BlockchainDetails{},
		Running:    &model.RunningSettings{Chain: "main"},
		Server:     model.ServerDetails{MemTotalBytes: 8 << 30},
	}
}

func (f *fixedDetails) Invalidate() { f.invalidated++ }

const testConf = "rpcuser=alice\nrpcpassword=hunter2\ndbcache=450\n"

func settingsHandler(t *testing.T) (http.Handler, string, *fixedDetails) {
	t.Helper()
	dir := t.TempDir()
	conf := filepath.Join(dir, "bitcoin.conf")
	if err := os.WriteFile(conf, []byte(testConf), 0o600); err != nil {
		t.Fatal(err)
	}
	r := settings.New(conf, dir).WithArgs(nil)
	if err := r.EnableEditing(filepath.Join(t.TempDir(), "state"), "bitcoind.service", slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	det := &fixedDetails{}
	return New(fixedSnapshot{}, fstest.MapFS{}, WithSettings(det, r, nil, func() bool { return true })), conf, det
}

func getForm(t *testing.T, h http.Handler) model.SettingsForm {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://192.168.1.119:8080/api/settings", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings: %d %s", rec.Code, rec.Body.String())
	}
	var f model.SettingsForm
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func postSettings(h http.Handler, host, contentType, origin, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/settings", strings.NewReader(body))
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

func TestSettingsSaved(t *testing.T) {
	h, conf, det := settingsHandler(t)
	f := getForm(t, h)
	body := `{"hash":"` + f.Hash + `","changes":{"dbcache":2048}}`
	rec := postSettings(h, "192.168.1.119:8080", "application/json", "http://192.168.1.119:8080", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if b, _ := os.ReadFile(conf); !strings.Contains(string(b), "dbcache=2048\n") || !strings.Contains(string(b), "rpcpassword=hunter2\n") {
		t.Errorf("bitcoin.conf: %q", b)
	}
	if det.invalidated != 1 {
		t.Error("the details must be read again after a save")
	}
	// The same hash again: the file changed since.
	rec = postSettings(h, "192.168.1.119:8080", "application/json", "", `{"hash":"`+f.Hash+`","changes":{"dbcache":4096}}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"conflict":true`) {
		t.Errorf("stale hash: got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSettingsRefused(t *testing.T) {
	h, conf, _ := settingsHandler(t)
	f := getForm(t, h)
	ok := `{"hash":"` + f.Hash + `","changes":{"dbcache":2048}}`
	for _, tc := range []struct {
		name, host, contentType, origin, body string
		want                                  int
	}{
		{"form post from a web page", "192.168.1.119:8080", "application/x-www-form-urlencoded", "", "dbcache=2048", http.StatusUnsupportedMediaType},
		{"other site's origin", "192.168.1.119:8080", "application/json", "http://evil.example", ok, http.StatusForbidden},
		{"DNS rebinding", "evil.example:8080", "application/json", "http://evil.example:8080", ok, http.StatusForbidden},
		{"another key", "192.168.1.119:8080", "application/json", "", `{"hash":"` + f.Hash + `","changes":{"rpcpassword":1}}`, http.StatusUnprocessableEntity},
		{"text as a value", "192.168.1.119:8080", "application/json", "", `{"hash":"` + f.Hash + `","changes":{"dbcache":"1\nrpcallowip=0.0.0.0/0"}}`, http.StatusBadRequest},
		{"an extra field", "192.168.1.119:8080", "application/json", "", `{"hash":"` + f.Hash + `","changes":{},"path":"/etc/passwd"}`, http.StatusBadRequest},
		{"too small", "192.168.1.119:8080", "application/json", "", `{"hash":"` + f.Hash + `","changes":{"dbcache":1}}`, http.StatusUnprocessableEntity},
		{"no change", "192.168.1.119:8080", "application/json", "", `{"hash":"` + f.Hash + `","changes":{"dbcache":450}}`, http.StatusBadRequest},
	} {
		if rec := postSettings(h, tc.host, tc.contentType, tc.origin, tc.body); rec.Code != tc.want {
			t.Errorf("%s: got %d (%s), want %d", tc.name, rec.Code, rec.Body.String(), tc.want)
		}
	}
	if b, _ := os.ReadFile(conf); string(b) != testConf {
		t.Errorf("bitcoin.conf changed: %q", b)
	}

	// The form never contains other lines of the file.
	req := httptest.NewRequest(http.MethodGet, "http://192.168.1.119:8080/api/settings", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), "rpcuser") {
		t.Errorf("secrets in the form: %s", rec.Body.String())
	}
}

func TestSettingsOffByDefault(t *testing.T) {
	h := New(fixedSnapshot{}, fstest.MapFS{"index.html": {Data: []byte("x")}})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/api/settings", strings.NewReader(`{"hash":"x","changes":{"dbcache":2048}}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s /api/settings without settings: got %d, want 404", method, rec.Code)
		}
	}
}

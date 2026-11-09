package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

type fixedSnapshot model.Snapshot

func (f fixedSnapshot) Snapshot() model.Snapshot { return model.Snapshot(f) }

func TestStatusEndpoint(t *testing.T) {
	snap := fixedSnapshot{Reachable: true, Health: model.Health{Level: model.Healthy, Reasons: []string{}}}
	h := New(snap, fstest.MapFS{})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got model.Snapshot
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Reachable || got.Health.Level != model.Healthy {
		t.Fatalf("unexpected body: %+v", got)
	}
	if d := time.Since(got.ServerTime); d < 0 || d > time.Minute {
		t.Fatalf("serverTime = %v, want about now", got.ServerTime)
	}
}

func TestHealthz(t *testing.T) {
	h := New(fixedSnapshot{}, fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestServesFrontend(t *testing.T) {
	static := fstest.MapFS{"index.html": {Data: []byte("<h1>dash</h1>")}}
	h := New(fixedSnapshot{}, static)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "<h1>dash</h1>" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestMissingFrontend(t *testing.T) {
	h := New(fixedSnapshot{}, fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

type fakeDetails struct{ d *model.Details }

func (f fakeDetails) Get(context.Context) *model.Details { return f.d }

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestDetailsEndpoint(t *testing.T) {
	d := &model.Details{Blockchain: &model.BlockchainDetails{UptimeSec: 42}}
	h := New(fixedSnapshot{}, fstest.MapFS{}, WithDetails(fakeDetails{d}))

	rec := get(h, http.MethodGet, "/api/details")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got model.Details
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil || got.Blockchain.UptimeSec != 42 {
		t.Fatalf("body = %+v, %v", got, err)
	}
	if rec := get(h, http.MethodPost, "/api/details"); rec.Code != http.StatusNotFound {
		t.Fatalf("POST status = %d, want 404", rec.Code)
	}
}

func TestDetailsNodeDown(t *testing.T) {
	d := &model.Details{NodeError: "can't reach bitcoin node", Server: model.ServerDetails{MemTotalBytes: 16e9}}
	h := New(fixedSnapshot{}, fstest.MapFS{}, WithDetails(fakeDetails{d}))
	rec := get(h, http.MethodGet, "/api/details")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"nodeError":"can't reach bitcoin node"`) || !strings.Contains(body, `"blockchain":null`) {
		t.Fatalf("got %d %s", rec.Code, body)
	}
	// The raw CPU reading is internal.
	if strings.Contains(body, "Idle") {
		t.Fatalf("raw CPU reading in the JSON: %s", body)
	}
}

func TestDetailsOffWithoutSource(t *testing.T) {
	h := New(fixedSnapshot{}, fstest.MapFS{})
	if rec := get(h, http.MethodGet, "/api/details"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

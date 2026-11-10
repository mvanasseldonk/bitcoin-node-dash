package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
)

type fakeDetails struct {
	d   *model.Details
	err error
}

func (f fakeDetails) Get(context.Context) (*model.Details, error) { return f.d, f.err }

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestDetailsEndpoint(t *testing.T) {
	d := &model.Details{Blockchain: model.BlockchainDetails{UptimeSec: 42}}
	h := New(fixedSnapshot{}, fstest.MapFS{}, WithDetails(fakeDetails{d: d}))

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

func TestDetailsUnreachable(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errors.New("dial tcp 127.0.0.1:8332: connection refused"), "can't reach bitcoin node"},
		{&source.StartingError{Status: "Loading block index…"}, "starting: Loading block index…"},
	} {
		h := New(fixedSnapshot{}, fstest.MapFS{}, WithDetails(fakeDetails{err: tc.err}))
		rec := get(h, http.MethodGet, "/api/details")
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%v: got %d %s, want 503 with %q", tc.err, rec.Code, rec.Body, tc.want)
		}
		// Internal details such as addresses stay in the log.
		if strings.Contains(rec.Body.String(), "127.0.0.1") {
			t.Errorf("error leaks an address: %s", rec.Body)
		}
	}
}

func TestDetailsOffWithoutSource(t *testing.T) {
	h := New(fixedSnapshot{}, fstest.MapFS{})
	if rec := get(h, http.MethodGet, "/api/details"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

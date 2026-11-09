// Package server exposes the dashboard UI and its JSON API over HTTP.
package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// SnapshotProvider returns the latest cached node snapshot.
type SnapshotProvider interface {
	Snapshot() model.Snapshot
}

// Option adds optional endpoints to the dashboard.
type Option func(mux *http.ServeMux)

// New returns the HTTP handler for the dashboard.
// static holds the built frontend (index.html and assets).
func New(snaps SnapshotProvider, static fs.FS, opts ...Option) http.Handler {
	mux := http.NewServeMux()
	// Unknown API paths, and the control endpoints when control is off, are 404.
	mux.HandleFunc("GET /api/", http.NotFound)
	mux.HandleFunc("POST /api/", http.NotFound)
	for _, opt := range opts {
		opt(mux)
	}

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		snap := snaps.Snapshot()
		snap.ServerTime = time.Now()
		_ = json.NewEncoder(w).Encode(snap)
	})

	// healthz reports whether the dashboard itself is running,
	// independent of the state of the Bitcoin node.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	if _, err := fs.Stat(static, "index.html"); err != nil {
		mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Frontend not built. Run `make web` (or use the Vite dev server) and restart.", http.StatusServiceUnavailable)
		})
	} else {
		mux.Handle("GET /", http.FileServerFS(static))
	}

	return mux
}

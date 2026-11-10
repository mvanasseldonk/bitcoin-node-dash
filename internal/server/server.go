// Package server exposes the dashboard UI and its JSON API over HTTP.
package server

import (
	"bytes"
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

// DefaultName is the dashboard's name when the operator gives none.
const DefaultName = "Node"

// config is what the options change.
type config struct {
	mux       *http.ServeMux
	name      string
	noAncoro  bool
	dashboard func() model.Dashboard
}

// Option adds optional endpoints to the dashboard, or changes its settings.
type Option func(c *config)

// WithName sets the name the operator gave their node (NODEDASH_NAME, story
// #35). It is shown in the header, the page title and on the home screen.
func WithName(name string) Option {
	return func(c *config) { c.name = name }
}

// WithoutAncoro hides the link to Ancoro (NODEDASH_ANCORO=0, story #44).
func WithoutAncoro() Option {
	return func(c *config) { c.noAncoro = true }
}

// WithDashboard adds the dashboard's own version and latest release to every
// status snapshot (story #39).
func WithDashboard(info func() model.Dashboard) Option {
	return func(c *config) { c.dashboard = info }
}

// New returns the HTTP handler for the dashboard.
// static holds the built frontend (index.html and assets).
func New(snaps SnapshotProvider, static fs.FS, opts ...Option) http.Handler {
	mux := http.NewServeMux()
	// Unknown API paths, and the control endpoints when control is off, are 404.
	mux.HandleFunc("GET /api/", http.NotFound)
	mux.HandleFunc("POST /api/", http.NotFound)
	cfg := &config{mux: mux, name: DefaultName}
	for _, opt := range opts {
		opt(cfg)
	}

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		snap := snaps.Snapshot()
		snap.ServerTime = time.Now()
		if cfg.dashboard != nil {
			d := cfg.dashboard()
			snap.Dashboard = &d
		}
		_ = json.NewEncoder(w).Encode(snap)
	})

	// healthz reports whether the dashboard itself is running,
	// independent of the state of the Bitcoin node.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /manifest.webmanifest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		_ = json.NewEncoder(w).Encode(newManifest(cfg.name))
	})

	index, err := fs.ReadFile(static, "index.html")
	if err != nil {
		mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Frontend not built. Run `make web` (or use the Vite dev server) and restart.", http.StatusServiceUnavailable)
		})
	} else {
		page := withName(index, cfg.name)
		if cfg.noAncoro {
			page = bytes.Replace(page, []byte(ancoroOn), []byte(ancoroOff), 1)
		}
		// The page itself carries the name; the file server would serve the
		// file as built, and redirects /index.html here.
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(page)
		})
		mux.Handle("GET /", http.FileServerFS(static))
	}

	return mux
}

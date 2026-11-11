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
const DefaultName = model.DefaultName

// config is what the options change.
type config struct {
	mux       *http.ServeMux
	name      func() string
	ancoro    func() bool
	dashboard func() model.Dashboard
	// webSettings is whether the dashboard settings page is on (#61).
	webSettings bool
}

// Option adds optional endpoints to the dashboard, or changes its settings.
type Option func(c *config)

// WithName sets the name the operator gave their node (NODEDASH_NAME, story
// #35). It is shown in the header, the page title and on the home screen.
func WithName(name string) Option {
	return func(c *config) { c.name = func() string { return name } }
}

// WithoutAncoro hides the link to Ancoro (NODEDASH_ANCORO=0, story #44).
func WithoutAncoro() Option {
	return func(c *config) { c.ancoro = func() bool { return false } }
}

// WithBrand reads the name and whether the Ancoro link shows on every
// request, so a change on the settings page applies at once (story #61).
func WithBrand(name func() string, ancoro func() bool) Option {
	return func(c *config) { c.name, c.ancoro = name, ancoro }
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
	cfg := &config{mux: mux, name: func() string { return DefaultName }, ancoro: func() bool { return true }}
	for _, opt := range opts {
		opt(cfg)
	}

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		snap := snaps.Snapshot()
		snap.ServerTime = time.Now()
		snap.WebSettings = cfg.webSettings
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
		_ = json.NewEncoder(w).Encode(newManifest(cfg.name()))
	})

	index, err := fs.ReadFile(static, "index.html")
	if err != nil {
		mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Frontend not built. Run `make web` (or use the Vite dev server) and restart.", http.StatusServiceUnavailable)
		})
	} else {
		// The page itself carries the name; the file server would serve the
		// file as built, and redirects /index.html here.
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
			page := withName(index, cfg.name())
			if !cfg.ancoro() {
				page = bytes.Replace(page, []byte(ancoroOn), []byte(ancoroOff), 1)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(page)
		})
		mux.Handle("GET /", http.FileServerFS(static))
	}

	return mux
}

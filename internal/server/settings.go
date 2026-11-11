package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/settings"
)

// SettingsEditor is the settings form (story #55).
type SettingsEditor interface {
	Form(d *model.Details) *model.SettingsForm
	Save(d *model.Details, hash string, changes map[string]int64) (*model.SettingsForm, error)
}

// invalidator is a DetailsProvider that can drop its cached reading, so the
// details page shows a save right away.
type invalidator interface {
	Invalidate()
}

// WithSettings adds the settings form (story #55): GET and POST
// /api/settings, with the protections of node control (decision 0003).
// Without it, both are 404. hosts and on are as for WithControl.
func WithSettings(det DetailsProvider, ed SettingsEditor, hosts []string, on func() bool) Option {
	return func(cfg *config) {
		g := newGuard(hosts)

		cfg.mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
			if !on() {
				http.NotFound(w, r)
				return
			}
			if err := g.checkHost(r); err != nil {
				writeError(w, http.StatusForbidden, err)
				return
			}
			writeJSON(w, http.StatusOK, ed.Form(det.Get(r.Context())))
		})

		cfg.mux.HandleFunc("POST /api/settings", func(w http.ResponseWriter, r *http.Request) {
			if !on() {
				http.NotFound(w, r)
				return
			}
			if status, err := g.checkPost(r); err != nil {
				writeError(w, status, err)
				return
			}
			var req struct {
				Hash    string           `json:"hash"`
				Changes map[string]int64 `json:"changes"`
			}
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil || req.Hash == "" {
				writeError(w, http.StatusBadRequest, errors.New(`expected {"hash": "…", "changes": {"setting": number}}`))
				return
			}
			form, err := ed.Save(det.Get(r.Context()), req.Hash, req.Changes)
			var fields settings.FieldErrors
			var cant *settings.CantSaveError
			switch {
			case err == nil:
				if inv, ok := det.(invalidator); ok {
					inv.Invalidate()
				}
				writeJSON(w, http.StatusOK, form)
			case errors.As(err, &fields):
				n := "1 field"
				if len(fields) != 1 {
					n = fmt.Sprintf("%d fields", len(fields))
				}
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "Fix " + n + " to save", "fields": fields})
			case errors.Is(err, settings.ErrConflict):
				writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "conflict": true})
			case errors.Is(err, settings.ErrNothingToSave):
				writeError(w, http.StatusBadRequest, err)
			case errors.As(err, &cant):
				writeError(w, http.StatusInternalServerError, err)
			default:
				writeError(w, http.StatusInternalServerError, err)
			}
		})
	}
}

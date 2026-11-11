package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/websettings"
)

// WebSettingsStore is the dashboard's own settings (story #61).
type WebSettingsStore interface {
	View() model.WebSettings
	Apply(c websettings.Change) (model.WebSettings, error)
}

// WithWebSettings adds the dashboard settings page: GET and POST
// /api/dashboard, with the protections of node control (decision 0004).
// Without it (NODEDASH_WEB_SETTINGS=0), both are 404 and there is no button.
func WithWebSettings(store WebSettingsStore, hosts []string) Option {
	return func(cfg *config) {
		cfg.webSettings = true
		g := newGuard(hosts)

		cfg.mux.HandleFunc("GET /api/dashboard", func(w http.ResponseWriter, r *http.Request) {
			if err := g.checkHost(r); err != nil {
				writeError(w, http.StatusForbidden, err)
				return
			}
			writeJSON(w, http.StatusOK, store.View())
		})

		cfg.mux.HandleFunc("POST /api/dashboard", func(w http.ResponseWriter, r *http.Request) {
			if status, err := g.checkPost(r); err != nil {
				writeError(w, status, err)
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
			if err != nil {
				writeError(w, http.StatusBadRequest, errors.New("request too large"))
				return
			}
			c, err := websettings.ParseChange(body)
			if err == nil {
				var v model.WebSettings
				if v, err = store.Apply(c); err == nil {
					writeJSON(w, http.StatusOK, v)
					return
				}
			}
			var bad *websettings.BadRequestError
			var fields websettings.FieldErrors
			switch {
			case errors.Is(err, websettings.ErrTurnOn):
				writeError(w, http.StatusForbidden, err)
			case errors.As(err, &bad), errors.Is(err, websettings.ErrNothing):
				writeError(w, http.StatusBadRequest, err)
			case errors.As(err, &fields):
				msg := "Fix 1 field to save"
				if len(fields) > 1 {
					msg = fmt.Sprintf("Fix %d fields to save", len(fields))
				}
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": msg, "fields": fields})
			case errors.Is(err, websettings.ErrConflict):
				writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "conflict": true})
			default: // a CantSaveError or a failed write
				writeError(w, http.StatusInternalServerError, err)
			}
		})
	}
}

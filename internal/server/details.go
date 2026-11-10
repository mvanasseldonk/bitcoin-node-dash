package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
)

// DetailsProvider returns the numbers for the details page.
type DetailsProvider interface {
	Get(ctx context.Context) (*model.Details, error)
}

// WithDetails adds GET /api/details (story #14). While the node can't be
// reached it answers 503 with the reason.
func WithDetails(d DetailsProvider) Option {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/details", func(w http.ResponseWriter, r *http.Request) {
			details, err := d.Get(r.Context())
			if err != nil {
				msg := "can't reach bitcoin node"
				var starting *source.StartingError
				if errors.As(err, &starting) {
					msg = "bitcoin node is starting: " + starting.Status
				}
				writeError(w, http.StatusServiceUnavailable, errors.New(msg))
				return
			}
			writeJSON(w, http.StatusOK, details)
		})
	}
}

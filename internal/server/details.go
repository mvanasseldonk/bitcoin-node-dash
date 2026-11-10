package server

import (
	"context"
	"net/http"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// DetailsProvider returns the numbers for the details page.
type DetailsProvider interface {
	Get(ctx context.Context) *model.Details
}

// WithDetails adds GET /api/details (stories #14 and #36). It always answers
// 200: when the node can't be reached, nodeError says why and the server
// figures are still there.
func WithDetails(d DetailsProvider) Option {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/details", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, d.Get(r.Context()))
		})
	}
}

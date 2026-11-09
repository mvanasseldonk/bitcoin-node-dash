// Package source provides the data sources the dashboard reads node data from.
package source

import (
	"context"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// Source fetches the current state of the node.
//
// Fetch must respect ctx cancellation, so a slow node never blocks the poller.
type Source interface {
	Fetch(ctx context.Context) (*model.NodeData, error)
}

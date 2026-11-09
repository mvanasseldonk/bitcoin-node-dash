// Package health turns node data into the overall status shown in the circle.
package health

import (
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// Evaluate returns the health of a reachable node.
//
// This is a placeholder for the project skeleton: every reachable node is
// healthy. The real rules (sync, peers, disk) are added with story #1.
func Evaluate(_ *model.NodeData, _ time.Time) model.Health {
	return model.Health{Level: model.Healthy, Reasons: []string{}}
}

// Unreachable is the health when the node cannot be reached.
func Unreachable() model.Health {
	return model.Health{Level: model.Unreachable, Reasons: []string{"Can't reach bitcoind"}}
}

package source

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// Scenarios supported by the mock source. They match the states in the design.
var Scenarios = []string{"healthy", "warning", "error", "unreachable"}

// ErrMockUnreachable is returned by the mock source in the "unreachable" scenario.
var ErrMockUnreachable = errors.New("mock: node unreachable")

const (
	gb = 1_000_000_000
	tb = 1_000 * gb
)

// Mock is a data source with fixed, realistic data for one scenario.
// It is used for development and demos, without a running Bitcoin node.
type Mock struct {
	scenario string
	now      func() time.Time
}

// NewMock returns a mock source for the given scenario.
func NewMock(scenario string) (*Mock, error) {
	for _, s := range Scenarios {
		if s == scenario {
			return &Mock{scenario: scenario, now: time.Now}, nil
		}
	}
	return nil, fmt.Errorf("unknown mock scenario %q (valid: %v)", scenario, Scenarios)
}

// Fetch implements Source.
func (m *Mock) Fetch(ctx context.Context) (*model.NodeData, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.scenario == "unreachable" {
		return nil, ErrMockUnreachable
	}

	now := m.now()
	d := &model.NodeData{
		Blockchain: model.Blockchain{
			Chain:                "main",
			Blocks:               977_412,
			Headers:              977_412,
			VerificationProgress: 0.99999,
			LastBlockTime:        now.Add(-4 * time.Minute),
		},
		Network: model.Network{Outbound: 10, Inbound: 6},
		Server:  model.Server{DiskFreeBytes: 812 * gb, DiskTotalBytes: 2 * tb},
	}

	switch m.scenario {
	case "warning":
		d.Blockchain.LastBlockTime = now.Add(-74 * time.Minute)
		d.Server.DiskFreeBytes = 152 * gb
	case "error":
		d.Network = model.Network{Outbound: 0, Inbound: 0}
	}
	return d, nil
}

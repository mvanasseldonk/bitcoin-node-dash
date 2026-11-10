package source

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// Scenarios supported by the mock source. They match the states in the design.
// "stale" answers once and then hangs, like a stuck poller: the dashboard
// keeps serving the first snapshot while it grows older.
// "outdated" runs a Bitcoin Core version that no longer gets security fixes;
// in mock mode, the latest release is 32.1.
var Scenarios = []string{"healthy", "syncing", "warning", "error", "unreachable", "starting", "stale", "outdated"}

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
	fetched  atomic.Bool

	startOnce sync.Once
	started   time.Time
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
	if m.scenario == "starting" {
		return nil, &StartingError{Status: "Loading block index…"}
	}
	if m.scenario == "stale" && m.fetched.Swap(true) {
		select {} // hang forever, ignoring ctx, to simulate a stuck source
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
		Network:  model.Network{Outbound: 10, Inbound: 6},
		Server:   model.Server{DiskFreeBytes: 812 * gb, DiskTotalBytes: 2 * tb},
		Software: ParseSoftware(320100, "/Satoshi:32.1.0/"),
	}

	switch m.scenario {
	case "syncing":
		d.Blockchain.Blocks = 850_000
		d.Blockchain.VerificationProgress = 0.87
		d.Blockchain.InitialBlockDownload = true
		d.Blockchain.LastBlockTime = now.Add(-2 * 365 * 24 * time.Hour)
		d.Server.DiskFreeBytes = 1_340 * gb
	case "warning":
		d.Blockchain.LastBlockTime = now.Add(-74 * time.Minute)
		d.Server.DiskFreeBytes = 38 * gb
		// A newer release is available: information, no attention.
		d.Software = ParseSoftware(320000, "/Satoshi:32.0.0/")
	case "error":
		d.Network = model.Network{Outbound: 0, Inbound: 0}
	case "outdated":
		d.Software = ParseSoftware(280100, "/Satoshi:28.1.0/")
	}
	return d, nil
}

// FetchDetails implements DetailSource with numbers that grow like a real
// node's: about 120 kB/s up and 45 kB/s down.
func (m *Mock) FetchDetails(ctx context.Context) (*model.Details, error) {
	switch m.scenario {
	case "unreachable":
		return nil, ErrMockUnreachable
	case "starting":
		return nil, &StartingError{Status: "Loading block index…"}
	case "stale":
		<-ctx.Done() // a stuck node: the details request times out
		return nil, ctx.Err()
	}
	now := m.now()
	m.startOnce.Do(func() { m.started = now })
	elapsed := now.Sub(m.started).Seconds()

	d := &model.Details{
		FetchedAt: now,
		Blockchain: model.BlockchainDetails{
			UptimeSec:       int64((12*24+4)*3600 + elapsed),
			SizeOnDiskBytes: 742 * gb,
		},
		Network: model.NetworkDetails{
			Networks: []model.NetworkType{
				{Name: "ipv4", Enabled: true, Peers: 8},
				{Name: "ipv6", Enabled: true, Peers: 2},
				{Name: "onion", Enabled: true, Peers: 6},
				{Name: "i2p"},
				{Name: "cjdns"},
			},
			TotalSentBytes: uint64(48.2*gb + 120_000*elapsed),
			TotalRecvBytes: uint64(17.9*gb + 45_000*elapsed),
		},
	}
	switch m.scenario {
	case "syncing":
		d.Blockchain.SizeOnDiskBytes = 610 * gb
	case "error":
		d.Network.Networks = []model.NetworkType{{Name: "ipv4", Enabled: true}, {Name: "ipv6", Enabled: true}, {Name: "onion"}, {Name: "i2p"}, {Name: "cjdns"}}
	}
	return d, nil
}

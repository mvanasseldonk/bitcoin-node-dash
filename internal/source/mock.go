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
// in mock mode, the latest release is 32.1, and a newer dashboard release
// exists (story #39).
// "live" is healthy, with a new block every 30 seconds, to see the dashboard
// react to new data (story #41).
// "busy" is healthy on a busy network: a full mempool and high fees (#11).
// "restarted" is healthy right after bitcoind restarted: the mempool is
// still loading and there are no fee estimates yet (#11).
var Scenarios = []string{"healthy", "syncing", "warning", "error", "unreachable", "starting", "stale", "outdated", "live", "busy", "restarted"}

// liveBlockEvery is how often a block arrives in the "live" scenario.
const liveBlockEvery = 30 * time.Second

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
		Mempool: &model.Mempool{
			Loaded:       true,
			Transactions: 18_412,
			VBytes:       42_000_000,
			UsageBytes:   142_000_000,
			MaxBytes:     300_000_000,
			MinFee:       0.1,
			MinRelayFee:  0.1,
			Fees:         mockFees(4, 2.4, 1.1),
		},
	}

	switch m.scenario {
	case "busy":
		d.Mempool = &model.Mempool{
			Loaded:       true,
			Transactions: 142_005,
			VBytes:       298_000_000,
			UsageBytes:   296_000_000,
			MaxBytes:     300_000_000,
			MinFee:       3.2,
			MinRelayFee:  0.1,
			Fees:         mockFees(86, 41, 6.5),
		}
	case "restarted":
		d.Mempool.Loaded = false
		d.Mempool.Transactions = 3_120
		d.Mempool.VBytes = 7_400_000
		d.Mempool.UsageBytes = 24_000_000
		d.Mempool.Fees = mockFees()
	case "syncing":
		d.Mempool.Transactions = 1_204
		d.Mempool.VBytes = 2_100_000
		d.Mempool.UsageBytes = 9_000_000
		d.Mempool.Fees = mockFees()
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
	case "live":
		m.startOnce.Do(func() { m.started = now })
		n := int64(now.Sub(m.started) / liveBlockEvery)
		d.Blockchain.Blocks += n
		d.Blockchain.Headers = d.Blockchain.Blocks
		d.Blockchain.LastBlockTime = m.started.Add(time.Duration(n) * liveBlockEvery)
	}
	return d, nil
}

// mockFees returns estimates for FeeTargets with these rates; without
// rates, the node has not enough data yet.
func mockFees(rates ...float64) []model.FeeEstimate {
	fees := make([]model.FeeEstimate, len(FeeTargets))
	for i, target := range FeeTargets {
		fees[i] = model.FeeEstimate{Blocks: target, Missing: FeeNoData}
		if i < len(rates) {
			fees[i] = model.FeeEstimate{Blocks: target, Rate: &rates[i]}
		}
	}
	return fees
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

	uptime := int64((12*24+4)*3600 + elapsed)
	d := &model.Details{
		FetchedAt: now,
		Running:   &model.RunningSettings{Chain: "main", Started: now.Add(-time.Duration(uptime) * time.Second)},
		Blockchain: &model.BlockchainDetails{
			UptimeSec:       uptime,
			SizeOnDiskBytes: 742 * gb,
		},
		Network: &model.NetworkDetails{
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

// ReadServer implements ServerReader: a small home server at about 7% CPU.
func (m *Mock) ReadServer() model.ServerDetails {
	m.startOnce.Do(func() { m.started = m.now() })
	ticks := uint64(m.now().Sub(m.started).Seconds() * 400) // 4 cores, 100 ticks/s
	temp := 47.0
	d := model.ServerDetails{
		MemUsedBytes:   5_800_000_000,
		MemTotalBytes:  16_000_000_000,
		DiskFreeBytes:  812 * gb,
		DiskUsedBytes:  1188 * gb,
		DiskTotalBytes: 2 * tb,
		CPUTempC:       &temp,
		UptimeSec:      int64((41*24+7)*3600 + m.now().Sub(m.started).Seconds()),
		Missing:        map[string]string{},
		CPU:            model.CPUSample{Total: 9_000_000 + ticks, Idle: 8_100_000 + ticks*93/100},
	}
	if m.scenario == "warning" {
		d.DiskFreeBytes, d.DiskUsedBytes = 38*gb, 1962*gb
	}
	return d
}

// mockPeer is a peer of the mock node: connected for age, with a ping in ms
// (0: not measured yet), and MB sent and received.
type mockPeer struct {
	network, typ, subver, addr string
	inbound, encrypted         bool
	age                        time.Duration
	ping                       float64
	sentMB, recvMB             float64
}

// mockPeers matches the Network tile: 10 outgoing, 6 incoming.
var mockPeers = []mockPeer{
	{"onion", "outbound-full-relay", "/Satoshi:28.1.0/", "vww6ybal4bd7szmgncyruucpgfkqahzddi37ktceo3ah7ngmcopnpyyd.onion:8333", false, true, 6*24*time.Hour + 3*time.Hour, 182, 1200, 310},
	{"ipv4", "outbound-full-relay", "/Satoshi:30.0.0/", "203.0.113.24:8333", false, true, 6*24*time.Hour + time.Hour, 38, 2400, 420},
	{"ipv4", "block-relay-only", "/Satoshi:29.2.0/Knots:20251010/", "198.51.100.7:8333", false, false, 5*24*time.Hour + 7*time.Hour, 41, 88, 1100},
	{"ipv6", "inbound", "/Satoshi:27.0.0/", "[2001:db8::4a1]:51234", true, true, 2*24*time.Hour + 5*time.Hour, 64, 640, 96},
	{"onion", "outbound-full-relay", "/Satoshi:29.0.0/", "kdt2yb7lmq4f6nxw2rvcz5oe3wq4hjuadt5y7cbmxkqu2hwvs3z6ryad.onion:8333", false, true, 2 * 24 * time.Hour, 205, 480, 160},
	{"ipv4", "outbound-full-relay", "/Satoshi:30.0.0/", "192.0.2.44:8333", false, true, 30 * time.Hour, 29, 720, 190},
	{"onion", "manual", "/Satoshi:30.0.0/", "p53lf57qovyuvwsc6xnrppyply3vtqm7l6pcobkmyqsiofyeznfu5uqd.onion:8333", false, true, 9 * time.Hour, 240, 210, 58},
	{"ipv6", "inbound", "/Satoshi:30.0.0/", "[2001:db8:5::17]:40332", true, true, 7 * time.Hour, 52, 180, 22},
	{"ipv4", "outbound-full-relay", "/Satoshi:28.1.0/", "198.51.100.140:8333", false, false, 5 * time.Hour, 74, 95, 44},
	{"onion", "inbound", "/Satoshi:30.0.0/", "127.0.0.1:43122", true, true, 3*time.Hour + 12*time.Minute, 412, 14, 2.1},
	{"i2p", "block-relay-only", "/Satoshi:29.0.0/", "ukeu3k5oycgaauneqgtnvselmt4yemvoilkln7jpvamvfx7dnkdq.b32.i2p:0", false, false, 2*time.Hour + 10*time.Minute, 1400, 3, 41},
	{"ipv4", "outbound-full-relay", "/Satoshi:30.0.0/", "203.0.113.201:8333", false, true, 95 * time.Minute, 33, 22, 9},
	{"ipv4", "inbound", "/btcwire:0.5.0/btcd:0.24.2/", "192.0.2.181:40112", true, false, 41 * time.Minute, 96, 12, 1.8},
	{"ipv4", "inbound", "/Satoshi:29.1.0/", "198.51.100.33:52210", true, false, 18 * time.Minute, 120, 4, 0.9},
	{"onion", "inbound", "/Satoshi:30.0.0/", "127.0.0.1:50918", true, true, 6 * time.Minute, 380, 1.2, 0.4},
	{"ipv4", "feeler", "/Satoshi:30.0.0/", "198.51.100.92:8333", false, true, 12 * time.Second, 0, 0.001, 0.002},
}

// FetchPeers implements PeerSource. The error scenario has no peers.
func (m *Mock) FetchPeers(ctx context.Context) ([]model.Peer, error) {
	switch m.scenario {
	case "unreachable":
		return nil, ErrMockUnreachable
	case "starting":
		return nil, &StartingError{Status: "Loading block index…"}
	case "stale":
		<-ctx.Done()
		return nil, ctx.Err()
	case "error":
		return []model.Peer{}, nil
	}
	now := m.now()
	peers := make([]model.Peer, len(mockPeers))
	for i, p := range mockPeers {
		peers[i] = model.Peer{
			ID:          int64(i + 1),
			Network:     p.network,
			Inbound:     p.inbound,
			Type:        ConnectionType(p.typ),
			Client:      PeerClient(p.subver),
			ConnectedAt: now.Add(-p.age).Truncate(time.Second).UTC(),
			SentBytes:   uint64(p.sentMB * 1e6),
			RecvBytes:   uint64(p.recvMB * 1e6),
			Encrypted:   p.encrypted,
			Address:     p.addr,
		}
		if p.ping > 0 {
			ping := p.ping
			peers[i].PingMs = &ping
		}
	}
	SortPeers(peers)
	return peers, nil
}

// ReadSettings implements SettingsReader: a mix of sources. In "warning",
// bitcoin.conf was changed but bitcoind wasn't restarted (story #13).
func (m *Mock) ReadSettings(running *model.RunningSettings) *model.Settings {
	v := func(n int64) *int64 { return &n }
	s := &model.Settings{
		Path: "/home/satoshi/.bitcoin/bitcoin.conf",
		Items: []model.Setting{
			{Key: "dbcache", Value: v(2048), Source: model.FromConf},
			{Key: "maxconnections", Value: v(125), Source: model.FromDefault},
			{Key: "maxmempool", Value: v(300), Source: model.FromDefault},
			{Key: "prune", Value: v(0), Source: model.FromDefault},
			{Key: "txindex", Value: v(1), Source: model.FromConf},
			{Key: "listen", Value: v(1), Source: model.FromDefault},
			{Key: "blocksonly", Value: v(0), Source: model.FromDefault},
			{Key: "maxuploadtarget", Value: v(5000), Source: model.FromConf, SourceDetail: "[main]"},
		},
	}
	switch m.scenario {
	case "warning":
		s.Items[2] = model.Setting{Key: "maxmempool", Value: v(300), Source: model.FromRunning, ConfValue: v(500)}
	case "outdated":
		s.Path = "/etc/bitcoin/bitcoin.conf"
		s.Items[3] = model.Setting{Key: "prune", Value: v(550), Source: model.FromCmdline, SourceDetail: "-prune=550"}
		s.Items[4] = model.Setting{Key: "txindex", Value: v(0), Source: model.FromDefault}
	}
	if running == nil {
		// Unreachable: only what bitcoin.conf says; the rest is unknown.
		for i, it := range s.Items {
			if it.Source == model.FromRunning {
				s.Items[i] = model.Setting{Key: it.Key, Value: it.ConfValue, Source: model.FromConf}
			}
		}
	}
	return s
}

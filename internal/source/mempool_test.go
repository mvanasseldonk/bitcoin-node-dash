package source

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSatPerVB(t *testing.T) {
	for _, c := range []struct {
		btcPerKvB float64
		want      float64
	}{
		{0.000001, 0.1},   // the default minimum relay fee of recent Bitcoin Core
		{0.00001, 1},      // 1 sat/vB
		{0.00002437, 2.4}, // one decimal under 10
		{0.00009949, 9.9}, // still under 10
		{0.0000996, 10},   // rounds up to 10
		{0.00018234, 18},  // whole numbers from 10
		{0.00086512, 87},  // 86.5 rounds up
		{0.0000004, 0},    // under 0.05 sat/vB
	} {
		if got := satPerVB(c.btcPerKvB); got != c.want {
			t.Errorf("satPerVB(%v) = %v, want %v", c.btcPerKvB, got, c.want)
		}
	}
}

// mempoolCore is a fake Bitcoin Core that answers the mempool calls and
// counts the fee estimates it is asked for.
type mempoolCore struct {
	mu        sync.Mutex
	height    int64
	mempool   string // the result of getmempoolinfo, or "" for an error
	forbid    bool   // answer getmempoolinfo with 403, like rpcwhitelist
	fees      map[int]string
	estimates int
}

func (c *mempoolCore) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		result := ""
		switch req.Method {
		case "getblockchaininfo":
			result = `{"chain":"main","blocks":` + itoa(c.height) + `,"headers":` + itoa(c.height) + `,"time":1794200000}`
		case "getnetworkinfo":
			result = okResults["getnetworkinfo"]
		case "getmempoolinfo":
			if c.forbid {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			result = c.mempool
		case "estimatesmartfee":
			c.estimates++
			if len(req.Params) != 2 || req.Params[1] != "economical" {
				t.Errorf("estimatesmartfee params = %v, want [target economical]", req.Params)
			}
			target := int(req.Params[0].(float64))
			result = c.fees[target]
			if result == "" {
				result = `{"errors":["Insufficient data or no feerate found"],"blocks":0}`
			}
		}
		if result == "" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"result":null,"error":{"code":-32601,"message":"Method not found"},"id":"node-dash"}`)
			return
		}
		_, _ = io.WriteString(w, `{"result":`+result+`,"error":null,"id":"node-dash"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

const mempoolInfo = `{"loaded":true,"size":18412,"bytes":42000000,"usage":142000000,"maxmempool":300000000,"mempoolminfee":0.000001,"minrelaytxfee":0.000001}`

func TestRPCMempool(t *testing.T) {
	core := &mempoolCore{height: 900_000, mempool: mempoolInfo, fees: map[int]string{
		2:   `{"feerate":0.00004,"blocks":2}`,
		6:   `{"feerate":0.00002437,"blocks":6}`,
		144: `{"feerate":0.0000112,"blocks":144}`,
	}}
	r := NewRPC(RPCConfig{URL: core.server(t).URL, User: "alice", Password: "pw"})
	d, err := r.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := d.Mempool
	if m == nil || m.Error != "" || !m.Loaded || m.Transactions != 18412 || m.VBytes != 42_000_000 ||
		m.UsageBytes != 142_000_000 || m.MaxBytes != 300_000_000 || m.MinFee != 0.1 || m.MinRelayFee != 0.1 {
		t.Fatalf("mempool = %+v", m)
	}
	var got []string
	for _, f := range m.Fees {
		if f.Rate == nil {
			t.Fatalf("estimate for %d blocks missing: %q", f.Blocks, f.Missing)
		}
		b, _ := json.Marshal(*f.Rate)
		got = append(got, itoa(int64(f.Blocks))+":"+string(b))
	}
	if s := strings.Join(got, " "); s != "2:4 6:2.4 144:1.1" {
		t.Fatalf("fees = %s, want 2:4 6:2.4 144:1.1", s)
	}
}

func TestRPCMempoolMissingEstimates(t *testing.T) {
	core := &mempoolCore{height: 1, mempool: mempoolInfo, fees: map[int]string{2: `{"feerate":0.00004,"blocks":2}`}}
	r := NewRPC(RPCConfig{URL: core.server(t).URL, User: "alice", Password: "pw"})
	d, err := r.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range d.Mempool.Fees[1:] {
		if f.Rate != nil || f.Missing != FeeNoData {
			t.Fatalf("estimate for %d blocks = %v %q, want missing with %q", f.Blocks, f.Rate, f.Missing, FeeNoData)
		}
	}
}

func TestRPCMempoolLoading(t *testing.T) {
	core := &mempoolCore{height: 1, mempool: strings.Replace(mempoolInfo, `"loaded":true`, `"loaded":false`, 1)}
	r := NewRPC(RPCConfig{URL: core.server(t).URL, User: "alice", Password: "pw"})
	d, err := r.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Mempool.Loaded || d.Mempool.Error != "" {
		t.Fatalf("mempool = %+v, want loading without error", d.Mempool)
	}
}

// A failing mempool call leaves the node reachable (story #11).
func TestRPCMempoolFails(t *testing.T) {
	for name, core := range map[string]*mempoolCore{
		"method missing": {height: 1},
		"rpcwhitelist":   {height: 1, forbid: true},
	} {
		r := NewRPC(RPCConfig{URL: core.server(t).URL, User: "alice", Password: "pw"})
		d, err := r.Fetch(context.Background())
		if err != nil {
			t.Fatalf("%s: the node became unreachable: %v", name, err)
		}
		want := FeeUnavailable
		if core.forbid {
			want = MempoolNotAllow
		}
		if d.Mempool == nil || d.Mempool.Error != want || d.Mempool.Fees != nil {
			t.Fatalf("%s: mempool = %+v, want error %q", name, d.Mempool, want)
		}
		if d.Blockchain.Blocks != 1 {
			t.Fatalf("%s: the rest of the data is gone: %+v", name, d)
		}
	}
}

// The estimates are asked again only on a new block, and at most once a minute.
func TestRPCFeesOncePerBlock(t *testing.T) {
	core := &mempoolCore{height: 100, mempool: mempoolInfo}
	r := NewRPC(RPCConfig{URL: core.server(t).URL, User: "alice", Password: "pw"})
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	fetch := func() {
		t.Helper()
		if _, err := r.Fetch(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	rounds := func() int { core.mu.Lock(); defer core.mu.Unlock(); return core.estimates / len(FeeTargets) }

	fetch()
	if rounds() != 1 {
		t.Fatalf("first fetch: %d rounds of estimates, want 1", rounds())
	}
	now = now.Add(10 * time.Minute) // the same block, much later
	fetch()
	if rounds() != 1 {
		t.Fatalf("same block: %d rounds, want 1", rounds())
	}
	core.mu.Lock()
	core.height++
	core.mu.Unlock()
	fetch() // a new block, 10 minutes after the last round
	if rounds() != 2 {
		t.Fatalf("new block: %d rounds, want 2", rounds())
	}
	core.mu.Lock()
	core.height++
	core.mu.Unlock()
	now = now.Add(30 * time.Second)
	fetch() // another block, but within a minute
	if rounds() != 2 {
		t.Fatalf("new block within a minute: %d rounds, want 2", rounds())
	}
	now = now.Add(31 * time.Second)
	fetch()
	if rounds() != 3 {
		t.Fatalf("a minute later: %d rounds, want 3", rounds())
	}
}

package source

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// fakeCore is a minimal Bitcoin Core JSON-RPC server for tests.
func fakeCore(t *testing.T, user, pass string, results map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct {
			Method string `json:"method"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		res, ok := results[req.Method]
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"result":null,"error":{"code":-32601,"message":"Method not found"},"id":"node-dash"}`)
			return
		}
		if strings.HasPrefix(res, "ERR:") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"result":null,"error":{"code":-28,"message":"`+strings.TrimPrefix(res, "ERR:")+`"},"id":"node-dash"}`)
			return
		}
		_, _ = io.WriteString(w, `{"result":`+res+`,"error":null,"id":"node-dash"}`)
	}))
}

var okResults = map[string]string{
	"getblockchaininfo": `{"chain":"main","blocks":977412,"headers":977412,"verificationprogress":0.99999,"initialblockdownload":false,"time":1794200000}`,
	"getnetworkinfo":    `{"version":320100,"subversion":"/Satoshi:32.1.0/","connections":16,"connections_in":6,"connections_out":10}`,
	"getmempoolinfo":    mempoolInfo,
	"estimatesmartfee":  `{"feerate":0.00004,"blocks":2}`,
}

func TestRPCFetchWithCookie(t *testing.T) {
	srv := fakeCore(t, "__cookie__", "s3cret", okResults)
	defer srv.Close()

	dir := t.TempDir()
	cookie := filepath.Join(dir, ".cookie")
	if err := os.WriteFile(cookie, []byte("__cookie__:s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := NewRPC(RPCConfig{URL: srv.URL, CookiePath: cookie, DataDir: dir})
	r.disk = func(string) (uint64, uint64, error) { return 812, 2000, nil }

	d, err := r.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Blockchain.Blocks != 977412 || d.Blockchain.Chain != "main" {
		t.Errorf("blockchain = %+v", d.Blockchain)
	}
	if !d.Blockchain.LastBlockTime.Equal(time.Unix(1794200000, 0)) {
		t.Errorf("last block time = %v", d.Blockchain.LastBlockTime)
	}
	if d.Network.Outbound != 10 || d.Network.Inbound != 6 {
		t.Errorf("network = %+v", d.Network)
	}
	if d.Server.DiskFreeBytes != 812 || d.Server.DiskTotalBytes != 2000 {
		t.Errorf("server = %+v", d.Server)
	}
	if d.Software.Label != "Bitcoin Core 32.1" {
		t.Errorf("software = %+v", d.Software)
	}
}

func TestRPCFetchWithUserPassword(t *testing.T) {
	srv := fakeCore(t, "alice", "pw", okResults)
	defer srv.Close()

	r := NewRPC(RPCConfig{URL: srv.URL, User: "alice", Password: "pw"})
	d, err := r.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Server.DiskTotalBytes != 0 {
		t.Errorf("expected unknown disk usage without data dir, got %+v", d.Server)
	}
}

func TestRPCUnauthorized(t *testing.T) {
	srv := fakeCore(t, "alice", "pw", okResults)
	defer srv.Close()

	r := NewRPC(RPCConfig{URL: srv.URL, User: "alice", Password: "wrong"})
	if _, err := r.Fetch(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestRPCErrorIsReported(t *testing.T) {
	srv := fakeCore(t, "alice", "pw", map[string]string{"getblockchaininfo": "ERR:Loading block index…"})
	defer srv.Close()

	r := NewRPC(RPCConfig{URL: srv.URL, User: "alice", Password: "pw"})
	_, err := r.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Loading block index") {
		t.Fatalf("want warm-up error, got %v", err)
	}
}

func TestRPCMissingCookie(t *testing.T) {
	r := NewRPC(RPCConfig{URL: "http://127.0.0.1:1", CookiePath: filepath.Join(t.TempDir(), "nope")})
	if _, err := r.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "cookie") {
		t.Fatalf("want cookie error, got %v", err)
	}
}

func TestDiskUsageOfTempDir(t *testing.T) {
	free, total, err := diskUsage(t.TempDir())
	if err != nil {
		t.Skipf("disk usage not supported: %v", err)
	}
	if total == 0 || free > total {
		t.Fatalf("free=%d total=%d", free, total)
	}
}

func TestRPCStarting(t *testing.T) {
	srv := fakeCore(t, "alice", "pw", map[string]string{"getblockchaininfo": "ERR:Loading block index…"})
	defer srv.Close()
	r := NewRPC(RPCConfig{URL: srv.URL, User: "alice", Password: "pw"})
	_, err := r.Fetch(context.Background())
	var se *StartingError
	if !errors.As(err, &se) || se.Status != "Loading block index…" {
		t.Fatalf("want StartingError with the status, got %v", err)
	}
}

// TestRPCOnlyCheapCalls guards the polling loop against expensive RPC calls
// (story #7): one fetch makes exactly these lightweight calls, and the fee
// estimates only on the first fetch and after a new block (story #11).
func TestRPCOnlyCheapCalls(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		methods = append(methods, req.Method)
		_, _ = io.WriteString(w, `{"result":`+okResults[req.Method]+`,"error":null,"id":"node-dash"}`)
	}))
	defer srv.Close()

	r := NewRPC(RPCConfig{URL: srv.URL, User: "alice", Password: "pw"})
	for range 2 {
		if _, err := r.Fetch(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	const poll = "getblockchaininfo,getnetworkinfo,getmempoolinfo"
	want := poll + ",estimatesmartfee,estimatesmartfee,estimatesmartfee," + poll
	if got := strings.Join(methods, ","); got != want {
		t.Fatalf("RPC calls for two fetches = %s, want %s", got, want)
	}
}

func TestRPCFetchDetails(t *testing.T) {
	results := map[string]string{
		"getblockchaininfo": `{"chain":"main","size_on_disk":742000000000,"pruned":false}`,
		"uptime":            `1054800`,
		"getnetworkinfo": `{"networks":[{"name":"ipv4","reachable":true,"proxy":"10.0.0.5:9050"},{"name":"ipv6","reachable":true,"proxy":""},` +
			`{"name":"onion","reachable":true,"proxy":"127.0.0.1:9050"},{"name":"i2p","reachable":false,"proxy":""},{"name":"cjdns","reachable":false,"proxy":""}],` +
			`"localaddresses":[{"address":"203.0.113.7","port":8333,"score":4},{"address":"abcdefghijklmnop.onion","port":8333,"score":4}]}`,
		"getnettotals": `{"totalbytesrecv":17900000000,"totalbytessent":48200000000,"timemillis":1794300000000}`,
		"getpeerinfo": `[{"addr":"198.51.100.1:8333","network":"ipv4","inbound":false},{"addr":"198.51.100.2:8333","network":"ipv4","inbound":true},` +
			`{"addr":"xyz.onion:8333","network":"onion","inbound":false},{"addr":"10.1.2.3:8333","network":"not_publicly_routable","inbound":true}]`,
	}
	srv := fakeCore(t, "alice", "pw", results)
	defer srv.Close()

	r := NewRPC(RPCConfig{URL: srv.URL, User: "alice", Password: "pw"})
	d, err := r.FetchDetails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Blockchain.UptimeSec != 1054800 || d.Blockchain.SizeOnDiskBytes != 742_000_000_000 || d.Blockchain.Pruned {
		t.Errorf("blockchain details = %+v", d.Blockchain)
	}
	if d.Network.TotalSentBytes != 48_200_000_000 || d.Network.TotalRecvBytes != 17_900_000_000 || !d.FetchedAt.Equal(time.UnixMilli(1794300000000)) {
		t.Errorf("totals = %+v at %v", d.Network, d.FetchedAt)
	}
	got := map[string]model.NetworkType{}
	var names []string
	for _, n := range d.Network.Networks {
		got[n.Name] = n
		names = append(names, n.Name)
	}
	if strings.Join(names, ",") != "ipv4,ipv6,onion,i2p,cjdns,not_publicly_routable" {
		t.Errorf("networks in order: %v", names)
	}
	if got["ipv4"].Peers != 2 || got["onion"].Peers != 1 || !got["onion"].Enabled || got["i2p"].Enabled || got["not_publicly_routable"].Peers != 1 {
		t.Errorf("networks = %+v", d.Network.Networks)
	}

	// Privacy: no address of the node, its peers or its proxy leaves the server.
	b, _ := json.Marshal(d)
	for _, secret := range []string{"203.0.113.7", "onion:", ".onion", "198.51.100", "10.1.2.3", "10.0.0.5", "9050"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("details contain %q: %s", secret, b)
		}
	}
}

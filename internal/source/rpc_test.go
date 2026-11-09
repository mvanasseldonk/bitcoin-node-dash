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
// (story #7): one fetch makes exactly these two lightweight calls.
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
	if _, err := r.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(methods, ","); got != "getblockchaininfo,getnetworkinfo" {
		t.Fatalf("RPC calls per fetch = %s, want getblockchaininfo,getnetworkinfo", got)
	}
}

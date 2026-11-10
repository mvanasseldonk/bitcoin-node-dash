package source

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPeerClient(t *testing.T) {
	for subver, want := range map[string]string{
		"/Satoshi:27.0.0/":                   "Bitcoin Core 27.0",
		"/Satoshi:29.2.1/":                   "Bitcoin Core 29.2.1",
		"/Satoshi:30.0.0(my comment)/":       "Bitcoin Core 30.0",
		"/Satoshi:28.1.0/Knots:20250305/":    "Bitcoin Knots 28.1",
		"/btcwire:0.5.0/btcd:0.24.2/":        "btcwire:0.5.0/btcd:0.24.2",
		"/Satoshi:25.0.0/Somethingelse:1.0/": "Satoshi:25.0.0/Somethingelse:1.0",
		"":                                   "Unknown",
	} {
		if got := PeerClient(subver); got != want {
			t.Errorf("PeerClient(%q) = %q, want %q", subver, got, want)
		}
	}
}

func TestConnectionType(t *testing.T) {
	for typ, want := range map[string]string{
		"outbound-full-relay": "Full relay",
		"block-relay-only":    "Blocks only",
		"manual":              "Manual",
		"feeler":              "Feeler",
		"addr-fetch":          "Address fetch",
		"inbound":             "",
		"something-new":       "something-new",
	} {
		if got := ConnectionType(typ); got != want {
			t.Errorf("ConnectionType(%q) = %q, want %q", typ, got, want)
		}
	}
}

func TestRPCFetchPeers(t *testing.T) {
	srv := fakeCore(t, "alice", "pw", map[string]string{
		"getpeerinfo": `[
			{"id":7,"addr":"192.0.2.1:8333","addrlocal":"203.0.113.9:8333","network":"ipv4","inbound":false,
			 "connection_type":"block-relay-only","subver":"/Satoshi:30.0.0/","pingtime":0.0412,"conntime":1794300000,
			 "bytessent":1000,"bytesrecv":2000,"transport_protocol_type":"v2"},
			{"id":3,"addr":"127.0.0.1:41234","network":"onion","inbound":true,"connection_type":"inbound",
			 "subver":"/Satoshi:28.1.0/Knots:20250305/","conntime":1794200000,"bytessent":5,"bytesrecv":6,
			 "transport_protocol_type":"v1"}
		]`,
	})
	defer srv.Close()
	r := NewRPC(RPCConfig{URL: srv.URL, User: "alice", Password: "pw"})
	peers, err := r.FetchPeers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 {
		t.Fatalf("got %d peers", len(peers))
	}
	// Connected longest first.
	in, out := peers[0], peers[1]
	if in.ID != 3 || !in.Inbound || in.Type != "" || in.Client != "Bitcoin Knots 28.1" || in.PingMs != nil || in.Encrypted {
		t.Fatalf("inbound peer = %+v", in)
	}
	if out.ID != 7 || out.Type != "Blocks only" || out.Client != "Bitcoin Core 30.0" || out.PingMs == nil ||
		*out.PingMs != 41.2 || !out.Encrypted || out.SentBytes != 1000 || out.RecvBytes != 2000 ||
		!out.ConnectedAt.Equal(time.Unix(1794300000, 0)) || out.Address != "192.0.2.1:8333" {
		t.Fatalf("outbound peer = %+v", out)
	}
	// The node's own address (addrlocal) never leaves the source.
	b, _ := json.Marshal(peers)
	if strings.Contains(string(b), "203.0.113.9") {
		t.Fatalf("addrlocal leaked: %s", b)
	}
}

func TestMockPeers(t *testing.T) {
	m, _ := NewMock("healthy")
	peers, err := m.FetchPeers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out, in, enc := 0, 0, 0
	for i, p := range peers {
		if p.Inbound {
			in++
		} else {
			out++
		}
		if p.Encrypted {
			enc++
		}
		if i > 0 && p.ConnectedAt.Before(peers[i-1].ConnectedAt) {
			t.Fatal("mock peers aren't connected longest first")
		}
	}
	if out != 10 || in != 6 || enc == 0 || enc == len(peers) {
		t.Fatalf("mock peers: %d out, %d in, %d encrypted", out, in, enc)
	}
	m, _ = NewMock("error")
	if peers, err := m.FetchPeers(context.Background()); err != nil || len(peers) != 0 {
		t.Fatalf("error scenario: %d peers, %v; want none", len(peers), err)
	}
}

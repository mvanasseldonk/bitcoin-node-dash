package peers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

type fakeNode struct {
	calls int
	err   error
}

func (f *fakeNode) FetchPeers(context.Context) ([]model.Peer, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return []model.Peer{{ID: 1, Network: "ipv4", Address: "192.0.2.1:8333"}}, nil
}

func newService(node *fakeNode, addresses bool) *Service {
	return New(node, addresses, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// Without NODEDASH_PEER_ADDRESSES=1, the API has no addresses at all.
func TestAddressesOffByDefault(t *testing.T) {
	p := newService(&fakeNode{}, false).Get(context.Background())
	b, _ := json.Marshal(p)
	if p.Addresses || strings.Contains(string(b), "192.0.2.1") || strings.Contains(string(b), `"address"`) {
		t.Fatalf("addresses leaked: %s", b)
	}
	p = newService(&fakeNode{}, true).Get(context.Background())
	if !p.Addresses || p.Peers[0].Address != "192.0.2.1:8333" {
		t.Fatalf("with addresses on: %+v", p)
	}
}

// The node is only asked while the page asks, and once per CacheTTL.
func TestCached(t *testing.T) {
	node := &fakeNode{}
	s := newService(node, false)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if node.calls != 0 {
		t.Fatal("fetched before anyone asked")
	}
	s.Get(context.Background())
	s.Get(context.Background())
	if node.calls != 1 {
		t.Fatalf("calls = %d within the cache time, want 1", node.calls)
	}
	now = now.Add(CacheTTL)
	s.Get(context.Background())
	if node.calls != 2 {
		t.Fatalf("calls = %d after the cache time, want 2", node.calls)
	}
}

func TestUnreachable(t *testing.T) {
	p := newService(&fakeNode{err: errors.New("dial tcp 10.0.0.5:8332: connection refused")}, true).Get(context.Background())
	if p.Peers != nil || p.NodeError != "can't reach bitcoin node" {
		t.Fatalf("got %+v", p)
	}
}

package details

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
)

type fakeSource struct {
	mu    sync.Mutex
	calls int
	next  *model.Details
	err   error
}

func (f *fakeSource) FetchDetails(context.Context) (*model.Details, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	d := *f.next
	return &d, nil
}

func reading(at time.Time, uptime int64, sent, recv uint64) *model.Details {
	return &model.Details{
		FetchedAt:  at,
		Blockchain: &model.BlockchainDetails{UptimeSec: uptime},
		Network:    &model.NetworkDetails{TotalSentBytes: sent, TotalRecvBytes: recv},
	}
}

// fakeServer returns CPU readings that advance by 100 ticks, 20 of them busy.
type fakeServer struct{ reads uint64 }

func (f *fakeServer) ReadServer() model.ServerDetails {
	f.reads++
	return model.ServerDetails{MemTotalBytes: 16e9, CPU: model.CPUSample{Total: 1000 + 100*f.reads, Idle: 500 + 80*f.reads}}
}

func newTest(src *fakeSource) (*Service, *time.Time) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	s := New(src, &fakeServer{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = func() time.Time { return now }
	return s, &now
}

func TestRates(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	src := &fakeSource{next: reading(t0, 1000, 1_000_000, 500_000)}
	s, now := newTest(src)

	d := s.Get(context.Background())
	if d.Network.SendRate != nil || d.Network.RecvRate != nil {
		t.Fatal("the first reading must not have rates")
	}

	// 5 s later: 600 kB sent, 250 kB received.
	*now = now.Add(5 * time.Second)
	src.next = reading(t0.Add(5*time.Second), 1005, 1_600_000, 750_000)
	d = s.Get(context.Background())
	if d.Network.SendRate == nil || *d.Network.SendRate != 120_000 || *d.Network.RecvRate != 50_000 {
		t.Fatalf("rates = %v / %v, want 120000 / 50000", d.Network.SendRate, d.Network.RecvRate)
	}

	// bitcoind restarted: its counters started over. No rate rather than a wrong one.
	*now = now.Add(5 * time.Second)
	src.next = reading(t0.Add(10*time.Second), 3, 20_000, 10_000)
	d = s.Get(context.Background())
	if d.Network.SendRate != nil || d.Network.RecvRate != nil {
		t.Fatalf("after a restart: rates = %v / %v, want none", d.Network.SendRate, d.Network.RecvRate)
	}

	// And from the next reading on, rates again.
	*now = now.Add(5 * time.Second)
	src.next = reading(t0.Add(15*time.Second), 8, 120_000, 60_000)
	if d = s.Get(context.Background()); d.Network.SendRate == nil || *d.Network.SendRate != 20_000 {
		t.Fatalf("rates after the restart = %v, want 20000", d.Network.SendRate)
	}
}

func TestSharedCache(t *testing.T) {
	src := &fakeSource{next: reading(time.Now(), 1, 1, 1)}
	s, now := newTest(src)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if d := s.Get(context.Background()); d.NodeError != "" {
				t.Error(d.NodeError)
			}
		}()
	}
	wg.Wait()
	if src.calls != 1 {
		t.Fatalf("50 visitors caused %d fetches, want 1", src.calls)
	}

	*now = now.Add(CacheTTL)
	s.Get(context.Background())
	if src.calls != 2 {
		t.Fatalf("after the cache expired: %d fetches, want 2", src.calls)
	}
}

func TestNodeUnreachable(t *testing.T) {
	src := &fakeSource{err: errors.New("dial tcp 127.0.0.1:8332: connection refused")}
	s, _ := newTest(src)
	for range 5 {
		d := s.Get(context.Background())
		if d.NodeError != "can't reach bitcoin node" || d.Blockchain != nil || d.Network != nil {
			t.Fatalf("got %+v, want only a plain node error", d)
		}
		// The server figures don't depend on the node.
		if d.Server.MemTotalBytes == 0 {
			t.Fatal("server figures missing while the node is down")
		}
	}
	if src.calls != 1 {
		t.Fatalf("an unreachable node was asked %d times, want 1", src.calls)
	}
}

func TestCancelledVisitorDoesNotFailOthers(t *testing.T) {
	src := &fakeSource{next: reading(time.Now(), 1, 1, 1)}
	s, _ := newTest(src)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d := s.Get(ctx); d.NodeError != "" {
		t.Fatalf("a closed page failed the shared fetch: %s", d.NodeError)
	}
}

func TestStartingNode(t *testing.T) {
	s, _ := newTest(&fakeSource{err: &source.StartingError{Status: "Loading block index…"}})
	if d := s.Get(context.Background()); d.NodeError != "bitcoin node is starting: Loading block index…" {
		t.Fatalf("node error = %q", d.NodeError)
	}
}

func TestCPUUsage(t *testing.T) {
	src := &fakeSource{next: reading(time.Now(), 1, 1, 1)}
	s, now := newTest(src)
	if d := s.Get(context.Background()); d.Server.CPUPercent != nil {
		t.Fatal("the first reading must not have a CPU usage")
	}
	*now = now.Add(CacheTTL)
	d := s.Get(context.Background())
	if d.Server.CPUPercent == nil || *d.Server.CPUPercent != 20 {
		t.Fatalf("CPU usage = %v, want 20%%", d.Server.CPUPercent)
	}
}

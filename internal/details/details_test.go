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
		Blockchain: model.BlockchainDetails{UptimeSec: uptime},
		Network:    model.NetworkDetails{TotalSentBytes: sent, TotalRecvBytes: recv},
	}
}

func newTest(src *fakeSource) (*Service, *time.Time) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	s := New(src, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = func() time.Time { return now }
	return s, &now
}

func TestRates(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	src := &fakeSource{next: reading(t0, 1000, 1_000_000, 500_000)}
	s, now := newTest(src)

	d, err := s.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Network.SendRate != nil || d.Network.RecvRate != nil {
		t.Fatal("the first reading must not have rates")
	}

	// 5 s later: 600 kB sent, 250 kB received.
	*now = now.Add(5 * time.Second)
	src.next = reading(t0.Add(5*time.Second), 1005, 1_600_000, 750_000)
	d, _ = s.Get(context.Background())
	if d.Network.SendRate == nil || *d.Network.SendRate != 120_000 || *d.Network.RecvRate != 50_000 {
		t.Fatalf("rates = %v / %v, want 120000 / 50000", d.Network.SendRate, d.Network.RecvRate)
	}

	// bitcoind restarted: its counters started over. No rate rather than a wrong one.
	*now = now.Add(5 * time.Second)
	src.next = reading(t0.Add(10*time.Second), 3, 20_000, 10_000)
	d, _ = s.Get(context.Background())
	if d.Network.SendRate != nil || d.Network.RecvRate != nil {
		t.Fatalf("after a restart: rates = %v / %v, want none", d.Network.SendRate, d.Network.RecvRate)
	}

	// And from the next reading on, rates again.
	*now = now.Add(5 * time.Second)
	src.next = reading(t0.Add(15*time.Second), 8, 120_000, 60_000)
	if d, _ = s.Get(context.Background()); d.Network.SendRate == nil || *d.Network.SendRate != 20_000 {
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
			if _, err := s.Get(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if src.calls != 1 {
		t.Fatalf("50 visitors caused %d fetches, want 1", src.calls)
	}

	*now = now.Add(CacheTTL)
	_, _ = s.Get(context.Background())
	if src.calls != 2 {
		t.Fatalf("after the cache expired: %d fetches, want 2", src.calls)
	}
}

func TestErrorIsCachedToo(t *testing.T) {
	src := &fakeSource{err: errors.New("connection refused")}
	s, _ := newTest(src)
	for range 5 {
		if _, err := s.Get(context.Background()); err == nil {
			t.Fatal("want an error")
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
	if _, err := s.Get(ctx); err != nil {
		t.Fatalf("a closed page failed the shared fetch: %v", err)
	}
}

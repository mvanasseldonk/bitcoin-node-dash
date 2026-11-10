package source

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

func TestMockScenarios(t *testing.T) {
	for _, s := range Scenarios {
		m, err := NewMock(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		d, err := m.Fetch(context.Background())
		if s == "starting" {
			var se *StartingError
			if !errors.As(err, &se) || se.Status == "" {
				t.Fatalf("starting: want a StartingError, got %v", err)
			}
			continue
		}
		if s == "unreachable" {
			if !errors.Is(err, ErrMockUnreachable) {
				t.Fatalf("unreachable: want ErrMockUnreachable, got %v", err)
			}
			continue
		}
		if err != nil || d == nil {
			t.Fatalf("%s: got %v, %v", s, d, err)
		}
		if m := d.Mempool; m == nil || m.Transactions == 0 || len(m.Fees) != len(FeeTargets) {
			t.Fatalf("%s: mempool = %+v", s, d.Mempool)
		}
	}
}

func TestMockMempool(t *testing.T) {
	fetch := func(s string) *model.Mempool {
		m, _ := NewMock(s)
		d, err := m.Fetch(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return d.Mempool
	}
	if m := fetch("restarted"); m.Loaded || m.Fees[0].Rate != nil {
		t.Fatalf("restarted: want a loading mempool without estimates, got %+v", m)
	}
	if m := fetch("syncing"); !m.Loaded || m.Fees[0].Missing != FeeNoData {
		t.Fatalf("syncing: want no estimates, got %+v", m)
	}
	if m := fetch("busy"); m.UsageBytes*100/m.MaxBytes < 95 || *m.Fees[0].Rate < 50 {
		t.Fatalf("busy: want a full mempool and high fees, got %+v", m)
	}
}

func TestMockUnknownScenario(t *testing.T) {
	if _, err := NewMock("sideways"); err == nil {
		t.Fatal("expected error for unknown scenario")
	}
}

func TestMockStaleHangsAfterFirstFetch(t *testing.T) {
	m, err := NewMock("stale")
	if err != nil {
		t.Fatal(err)
	}
	if d, err := m.Fetch(context.Background()); err != nil || d == nil {
		t.Fatalf("first fetch: got %v, %v", d, err)
	}
	done := make(chan struct{})
	go func() {
		_, _ = m.Fetch(context.Background())
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("second fetch returned; want it to hang")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMockDetails(t *testing.T) {
	for _, s := range Scenarios {
		m, _ := NewMock(s)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		d, err := m.FetchDetails(ctx)
		cancel()
		switch s {
		case "unreachable", "starting", "stale":
			if err == nil {
				t.Errorf("%s: want an error", s)
			}
		default:
			if err != nil || d.Blockchain.UptimeSec == 0 || len(d.Network.Networks) == 0 {
				t.Errorf("%s: got %+v, %v", s, d, err)
			}
		}
	}
}

func TestMockLiveBlocks(t *testing.T) {
	m, _ := NewMock("live")
	start := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return start }
	first, _ := m.Fetch(context.Background())

	m.now = func() time.Time { return start.Add(70 * time.Second) }
	later, _ := m.Fetch(context.Background())
	if got := later.Blockchain.Blocks - first.Blockchain.Blocks; got != 2 {
		t.Fatalf("after 70 s: %d new blocks, want 2", got)
	}
	if want := start.Add(60 * time.Second); !later.Blockchain.LastBlockTime.Equal(want) {
		t.Fatalf("last block at %v, want %v", later.Blockchain.LastBlockTime, want)
	}
}

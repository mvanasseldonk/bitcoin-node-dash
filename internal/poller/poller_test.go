package poller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

type fakeSource struct {
	data  *model.NodeData
	err   error
	panic bool
	block bool
}

func (f *fakeSource) Fetch(ctx context.Context) (*model.NodeData, error) {
	if f.panic {
		panic("boom")
	}
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.data, f.err
}

func newTestPoller(src *fakeSource) *Poller {
	return New(src, time.Second, 50*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestInitialSnapshotIsUnreachable(t *testing.T) {
	p := newTestPoller(&fakeSource{})
	s := p.Snapshot()
	if s.Reachable || s.Health.Level != model.Unreachable || s.Data != nil {
		t.Fatalf("unexpected initial snapshot: %+v", s)
	}
}

func TestSuccessfulPoll(t *testing.T) {
	data := &model.NodeData{
		Blockchain: model.Blockchain{Blocks: 42, Headers: 42, LastBlockTime: time.Now()},
		Network:    model.Network{Outbound: 10},
	}
	p := newTestPoller(&fakeSource{data: data})
	p.Poll(context.Background())

	s := p.Snapshot()
	if !s.Reachable || s.Data != data || s.UpdatedAt.IsZero() || s.Health.Level != model.Healthy {
		t.Fatalf("unexpected snapshot: %+v", s)
	}
}

func TestFailureKeepsLastKnownData(t *testing.T) {
	data := &model.NodeData{Blockchain: model.Blockchain{Blocks: 42}}
	src := &fakeSource{data: data}
	p := newTestPoller(src)
	p.Poll(context.Background())
	updated := p.Snapshot().UpdatedAt

	src.err = errors.New("connection refused")
	p.Poll(context.Background())

	s := p.Snapshot()
	if s.Reachable || s.Health.Level != model.Unreachable {
		t.Fatalf("expected unreachable, got %+v", s)
	}
	if s.Data != data || !s.UpdatedAt.Equal(updated) {
		t.Fatalf("expected last known data to be kept, got %+v", s)
	}
}

func TestPanickingSourceDoesNotCrash(t *testing.T) {
	p := newTestPoller(&fakeSource{panic: true})
	p.Poll(context.Background())
	if s := p.Snapshot(); s.Reachable {
		t.Fatalf("expected unreachable after panic, got %+v", s)
	}
}

func TestSlowSourceTimesOut(t *testing.T) {
	p := newTestPoller(&fakeSource{block: true})
	start := time.Now()
	p.Poll(context.Background())
	if time.Since(start) > time.Second {
		t.Fatal("poll did not respect the fetch timeout")
	}
	if s := p.Snapshot(); s.Reachable {
		t.Fatalf("expected unreachable after timeout, got %+v", s)
	}
}

func TestLogsEachProblemOnce(t *testing.T) {
	var buf bytes.Buffer
	src := &fakeSource{err: errors.New("bitcoin core rejected the RPC credentials")}
	p := New(src, time.Second, 50*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	count := func(s string) int { return strings.Count(buf.String(), s) }
	poll := func(n int) {
		for range n {
			p.Poll(context.Background())
		}
	}

	// Unreachable from the start: one warning, not one per poll.
	poll(3)
	if got := count("node unreachable"); got != 1 {
		t.Fatalf("after 3 failed polls: %d warnings, want 1\n%s", got, buf.String())
	}

	// A different cause is logged once.
	src.err = errors.New("connection refused")
	poll(3)
	if got := count("connection refused"); got != 1 {
		t.Fatalf("new cause logged %d times, want 1\n%s", got, buf.String())
	}

	// Back online: logged once.
	src.err, src.data = nil, &model.NodeData{}
	poll(3)
	if got := count("node reachable again"); got != 1 {
		t.Fatalf("recovery logged %d times, want 1\n%s", got, buf.String())
	}

	// Lost again after being reachable: one new warning.
	src.err = errors.New("connection refused")
	poll(3)
	if got := count("node unreachable"); got != 3 {
		t.Fatalf("%d warnings in total, want 3\n%s", got, buf.String())
	}
}

func TestReachableFromStartLogsNoRecovery(t *testing.T) {
	var buf bytes.Buffer
	p := New(&fakeSource{data: &model.NodeData{}}, time.Second, 50*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	p.Poll(context.Background())
	if strings.Contains(buf.String(), "reachable again") {
		t.Fatalf("unexpected recovery message:\n%s", buf.String())
	}
}

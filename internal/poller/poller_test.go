package poller

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
	data := &model.NodeData{Blockchain: model.Blockchain{Blocks: 42}}
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

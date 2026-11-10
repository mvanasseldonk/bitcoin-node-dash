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
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/release"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
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

func TestStartingNode(t *testing.T) {
	data := &model.NodeData{Blockchain: model.Blockchain{Blocks: 42}}
	src := &fakeSource{data: data}
	p := newTestPoller(src)
	p.Poll(context.Background())

	src.err = &source.StartingError{Status: "Verifying blocks…"}
	p.Poll(context.Background())
	p.Poll(context.Background())

	s := p.Snapshot()
	if s.Health.Level != model.Starting || len(s.Health.Reasons) != 1 || s.Health.Reasons[0] != "Verifying blocks…" {
		t.Fatalf("want starting with the status as reason, got %+v", s.Health)
	}
	if s.Reachable || s.Data != data {
		t.Fatalf("want last known data kept and not reachable, got %+v", s)
	}
	if d := p.NextDelay(); d != time.Second {
		t.Fatalf("NextDelay while starting = %v, want the normal interval", d)
	}
}

func TestBackoffWhileUnreachable(t *testing.T) {
	src := &fakeSource{err: errors.New("connection refused")}
	p := newTestPoller(src) // interval 1s, so backoff caps at 3s
	if d := p.NextDelay(); d != time.Second {
		t.Fatalf("before any poll: %v, want 1s", d)
	}
	var got []time.Duration
	for range 5 {
		p.Poll(context.Background())
		got = append(got, p.NextDelay())
	}
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second, 3 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delays = %v, want %v", got, want)
		}
	}

	// Back to the normal pace as soon as the node answers.
	src.err, src.data = nil, &model.NodeData{}
	p.Poll(context.Background())
	if d := p.NextDelay(); d != time.Second {
		t.Fatalf("after recovery: %v, want 1s", d)
	}
}

func TestUpgradeAdvice(t *testing.T) {
	data := &model.NodeData{
		Blockchain: model.Blockchain{Blocks: 42, Headers: 42, LastBlockTime: time.Now()},
		Network:    model.Network{Outbound: 10},
		Software:   source.ParseSoftware(280100, "/Satoshi:28.1.0/"),
	}
	p := newTestPoller(&fakeSource{data: data})

	// Update check off: no advice, healthy.
	p.Poll(context.Background())
	if s := p.Snapshot(); s.Data.Upgrade != nil || s.Health.Level != model.Healthy {
		t.Fatalf("without releases: got %+v, %s", s.Data.Upgrade, s.Health.Level)
	}

	p.UseReleases(release.Fixed{Major: 32, Minor: 1})
	p.Poll(context.Background())
	s := p.Snapshot()
	if u := s.Data.Upgrade; u == nil || !u.EndOfLife || u.Latest != "32.1" {
		t.Fatalf("want end-of-life advice, got %+v", u)
	}
	if s.Health.Level != model.Warning {
		t.Fatalf("level = %s, want warning", s.Health.Level)
	}
}

// The mempool never changes the health, and a problem with it is logged once.
func TestMempoolProblemKeepsHealth(t *testing.T) {
	var logs bytes.Buffer
	data := &model.NodeData{
		Blockchain: model.Blockchain{Blocks: 42, Headers: 42, LastBlockTime: time.Now()},
		Network:    model.Network{Outbound: 10},
		Mempool:    &model.Mempool{Error: source.MempoolNotAllow},
	}
	p := New(&fakeSource{data: data}, time.Second, 50*time.Millisecond, slog.New(slog.NewTextHandler(&logs, nil)))
	p.Poll(context.Background())
	p.Poll(context.Background())
	s := p.Snapshot()
	if !s.Reachable || s.Health.Level != model.Healthy {
		t.Fatalf("a mempool problem changed the status: %+v", s.Health)
	}
	if n := strings.Count(logs.String(), "mempool unavailable"); n != 1 {
		t.Fatalf("logged the mempool problem %d times, want once:\n%s", n, logs.String())
	}
}

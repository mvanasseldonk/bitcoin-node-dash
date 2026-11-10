// Package poller fetches node data on a fixed interval and caches the result,
// so every browser tab reads the same snapshot instead of hitting the node.
package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/health"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/release"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
)

// Poller periodically fetches node data from a Source.
//
// While the node can't be reached it backs off: the delay between attempts
// doubles from interval up to maxBackoff, so a node that is down isn't
// hammered while recovery is still noticed quickly. While the node is
// starting up it keeps polling at interval, to show progress and the moment
// it is ready.
type Poller struct {
	src        source.Source
	interval   time.Duration
	maxBackoff time.Duration
	timeout    time.Duration
	log        *slog.Logger
	now        func() time.Time
	// releases tells the latest Bitcoin Core release, for upgrade advice;
	// nil when the update check is off.
	releases release.Latest

	mu   sync.RWMutex
	snap model.Snapshot
	// lastErr is the message of the error that was logged last, or "" while
	// the node is reachable. Used to log each problem once instead of on every poll.
	lastErr string
	// failures counts consecutive failed polls, for the backoff.
	failures int
	// mempoolErr is the mempool problem that was logged last, or "".
	mempoolErr string
}

// New returns a Poller. Call Run to start polling.
func New(src source.Source, interval, timeout time.Duration, log *slog.Logger) *Poller {
	return &Poller{
		src:        src,
		interval:   interval,
		maxBackoff: 3 * interval,
		timeout:    timeout,
		log:        log,
		now:        time.Now,
		snap:       model.Snapshot{Health: health.Unreachable()},
	}
}

// UseReleases sets where the latest Bitcoin Core release comes from, for the
// upgrade advice. Call it before Run.
func (p *Poller) UseReleases(l release.Latest) {
	p.releases = l
}

// Run polls immediately and then every interval (longer while the node is
// unreachable, see Poller), until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	for {
		p.Poll(ctx)
		t := time.NewTimer(p.NextDelay())
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// NextDelay returns how long to wait before the next poll.
func (p *Poller) NextDelay() time.Duration {
	p.mu.RLock()
	n := p.failures
	p.mu.RUnlock()
	d := p.interval
	for i := 1; i < n && d < p.maxBackoff; i++ {
		d *= 2
	}
	return min(d, p.maxBackoff)
}

// Poll performs a single fetch and updates the cached snapshot.
// It never panics: a failing or panicking source marks the node unreachable.
func (p *Poller) Poll(ctx context.Context) {
	data, err := p.fetch(ctx)
	now := p.now()

	p.mu.Lock()
	defer p.mu.Unlock()
	p.snap.CheckedAt = now
	if err != nil {
		p.snap.Reachable = false
		// Log a problem once, and again only when its cause changes.
		logNew := err.Error() != p.lastErr
		p.lastErr = err.Error()
		var starting *source.StartingError
		if errors.As(err, &starting) {
			p.failures = 0 // starting is progress: keep polling at the normal pace
			p.snap.Health = health.Starting(starting.Status)
			if logNew {
				p.log.Info("node is starting", "status", starting.Status)
			}
			return // keep the last known data
		}
		p.failures++
		p.snap.Health = health.Unreachable()
		if logNew {
			p.log.Warn("node unreachable", "err", err)
		}
		return // keep the last known data
	}
	p.failures = 0
	if p.lastErr != "" {
		p.log.Info("node reachable again")
		p.lastErr = ""
	}
	p.logMempool(data.Mempool)
	p.snap.Reachable = true
	p.snap.UpdatedAt = now
	data.Upgrade = release.Advise(data.Software, p.releases)
	p.snap.Data = data
	p.snap.Health = health.Evaluate(data, now)
}

// logMempool logs a mempool problem once, and when it is solved. The
// mempool never changes the health (story #11).
func (p *Poller) logMempool(m *model.Mempool) {
	msg := ""
	if m != nil {
		msg = m.Error
	}
	if msg == p.mempoolErr {
		return
	}
	if msg != "" {
		p.log.Warn("mempool unavailable", "reason", msg)
	} else if p.mempoolErr != "" {
		p.log.Info("mempool available again")
	}
	p.mempoolErr = msg
}

func (p *Poller) fetch(ctx context.Context) (data *model.NodeData, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("source panicked: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return p.src.Fetch(ctx)
}

// Snapshot returns a copy of the latest snapshot.
func (p *Poller) Snapshot() model.Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snap
}

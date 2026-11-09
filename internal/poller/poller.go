// Package poller fetches node data on a fixed interval and caches the result,
// so every browser tab reads the same snapshot instead of hitting the node.
package poller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/health"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
)

// Poller periodically fetches node data from a Source.
type Poller struct {
	src      source.Source
	interval time.Duration
	timeout  time.Duration
	log      *slog.Logger
	now      func() time.Time

	mu   sync.RWMutex
	snap model.Snapshot
	// lastErr is the message of the error that was logged last, or "" while
	// the node is reachable. Used to log each problem once instead of on every poll.
	lastErr string
}

// New returns a Poller. Call Run to start polling.
func New(src source.Source, interval, timeout time.Duration, log *slog.Logger) *Poller {
	return &Poller{
		src:      src,
		interval: interval,
		timeout:  timeout,
		log:      log,
		now:      time.Now,
		snap:     model.Snapshot{Health: health.Unreachable()},
	}
}

// Run polls immediately and then every interval, until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.Poll(ctx)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Poll(ctx)
		}
	}
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
		// Log a problem once, and again only when its cause changes.
		if msg := err.Error(); msg != p.lastErr {
			p.log.Warn("node unreachable", "err", err)
			p.lastErr = msg
		}
		p.snap.Reachable = false
		p.snap.Health = health.Unreachable()
		return // keep the last known data
	}
	if p.lastErr != "" {
		p.log.Info("node reachable again")
		p.lastErr = ""
	}
	p.snap.Reachable = true
	p.snap.UpdatedAt = now
	p.snap.Data = data
	p.snap.Health = health.Evaluate(data, now)
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

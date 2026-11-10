// Package details serves the extra numbers of the details page (story #14).
// They are fetched on demand, only while someone has the page open, and
// shared by all visitors for a few seconds.
package details

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
)

// CacheTTL is how long one reading is shared. The page refreshes every 5 s.
const CacheTTL = 4 * time.Second

const fetchTimeout = 5 * time.Second

// Service fetches details from a source, caches them briefly, and computes
// the transfer rates from two readings.
type Service struct {
	src source.DetailSource
	log *slog.Logger
	now func() time.Time

	mu        sync.Mutex
	cached    *model.Details
	cachedErr error
	cachedAt  time.Time
	// prev is the previous successful reading, for the rates.
	prev    *model.Details
	lastErr string
}

// New returns a Service for src.
func New(src source.DetailSource, log *slog.Logger) *Service {
	return &Service{src: src, log: log, now: time.Now}
}

// Get returns the details, at most CacheTTL old. Concurrent callers wait for
// one fetch instead of each asking the node.
func (s *Service) Get(ctx context.Context) (*model.Details, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.now().Sub(s.cachedAt) < CacheTTL {
		return s.cached, s.cachedErr
	}

	// A visitor closing the page must not fail the fetch for the others.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	d, err := s.src.FetchDetails(ctx)
	s.cachedAt = s.now()
	if err != nil {
		if err.Error() != s.lastErr {
			s.log.Warn("can't fetch details", "err", err)
		}
		s.lastErr = err.Error()
		s.cached, s.cachedErr = nil, err
		return nil, err
	}
	s.lastErr = ""
	withRates(d, s.prev)
	s.prev = d
	s.cached, s.cachedErr = d, nil
	return d, nil
}

// withRates sets the transfer rates of cur from the change since prev.
// They stay nil without a usable previous reading: the first one, or after
// bitcoind restarted and its counters started over.
func withRates(cur, prev *model.Details) {
	if prev == nil {
		return
	}
	dt := cur.FetchedAt.Sub(prev.FetchedAt).Seconds()
	c, p := cur.Network, prev.Network
	restarted := cur.Blockchain.UptimeSec < prev.Blockchain.UptimeSec ||
		c.TotalSentBytes < p.TotalSentBytes || c.TotalRecvBytes < p.TotalRecvBytes
	if dt <= 0 || restarted {
		return
	}
	send := float64(c.TotalSentBytes-p.TotalSentBytes) / dt
	recv := float64(c.TotalRecvBytes-p.TotalRecvBytes) / dt
	cur.Network.SendRate, cur.Network.RecvRate = &send, &recv
}

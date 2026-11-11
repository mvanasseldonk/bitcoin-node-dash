// Package details serves the extra numbers of the details page (stories #14
// and #36). They are fetched on demand, only while someone has the page
// open, and shared by all visitors for a few seconds.
package details

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
)

// CacheTTL is how long one reading is shared. The page refreshes every 5 s.
const CacheTTL = 4 * time.Second

const fetchTimeout = 5 * time.Second

// Service reads details from the node and the server, caches them briefly,
// and computes rates and CPU usage from two readings.
type Service struct {
	node     source.DetailSource
	server   source.ServerReader
	settings source.SettingsReader
	log      *slog.Logger
	now      func() time.Time

	mu       sync.Mutex
	cached   *model.Details
	cachedAt time.Time
	// prev is the previous reading, for the rates and CPU usage.
	prev    *model.Details
	lastErr string
}

// New returns a Service. server and settings may be nil when they can't be read.
func New(node source.DetailSource, server source.ServerReader, settings source.SettingsReader, log *slog.Logger) *Service {
	return &Service{node: node, server: server, settings: settings, log: log, now: time.Now}
}

// Get returns the details, at most CacheTTL old. Concurrent callers wait for
// one reading instead of each asking the node. When the node can't be
// reached, the server figures are still there and NodeError says why.
func (s *Service) Get(ctx context.Context) *model.Details {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && s.now().Sub(s.cachedAt) < CacheTTL {
		return s.cached
	}

	// A visitor closing the page must not fail the reading for the others.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	d, err := s.node.FetchDetails(ctx)
	if err != nil {
		if err.Error() != s.lastErr {
			s.log.Warn("can't fetch details", "err", err)
		}
		s.lastErr = err.Error()
		d = &model.Details{FetchedAt: s.now(), NodeError: nodeError(err)}
	} else {
		s.lastErr = ""
	}
	if s.server != nil {
		d.Server = s.server.ReadServer()
	}
	if s.settings != nil {
		d.Settings = s.settings.ReadSettings(d.Running)
	}

	withRates(d, s.prev)
	withCPU(d, s.prev)
	s.prev = d
	s.cached, s.cachedAt = d, s.now()
	return d
}

// Invalidate drops the cached reading, e.g. after the settings form saved.
func (s *Service) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = nil
}

// nodeError says in plain language why the node's numbers are missing.
// Technical details, such as addresses, stay in the log.
func nodeError(err error) string {
	var starting *source.StartingError
	if errors.As(err, &starting) {
		return "bitcoin node is starting: " + starting.Status
	}
	return "can't reach bitcoin node"
}

// withRates sets the transfer rates of cur from the change since prev.
// They stay nil without a usable previous reading: the first one, or after
// bitcoind restarted and its counters started over.
func withRates(cur, prev *model.Details) {
	if prev == nil || cur.Network == nil || prev.Network == nil || cur.Blockchain == nil || prev.Blockchain == nil {
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
	c.SendRate, c.RecvRate = &send, &recv
}

// withCPU sets the CPU usage of cur from the change in CPU time since prev.
func withCPU(cur, prev *model.Details) {
	if prev == nil {
		return
	}
	if busy, ok := model.CPUBusy(prev.Server.CPU, cur.Server.CPU); ok {
		cur.Server.CPUPercent = &busy
	}
}

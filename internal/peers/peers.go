// Package peers serves the list of the Peers page (story #12). It is fetched
// on demand, only while someone has the page open, and shared by all
// visitors for a few seconds.
package peers

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

// Service reads the peers from the node and caches them briefly.
type Service struct {
	node source.PeerSource
	// addresses is NODEDASH_PEER_ADDRESSES=1. Without it, the addresses
	// never leave this package: the dashboard has no login.
	addresses bool
	log       *slog.Logger
	now       func() time.Time

	mu       sync.Mutex
	cached   *model.Peers
	cachedAt time.Time
	lastErr  string
}

// New returns a Service.
func New(node source.PeerSource, addresses bool, log *slog.Logger) *Service {
	return &Service{node: node, addresses: addresses, log: log, now: time.Now}
}

// Get returns the peers, at most CacheTTL old. Concurrent callers wait for
// one reading instead of each asking the node.
func (s *Service) Get(ctx context.Context) *model.Peers {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && s.now().Sub(s.cachedAt) < CacheTTL {
		return s.cached
	}

	// A visitor closing the page must not fail the reading for the others.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	out := &model.Peers{FetchedAt: s.now(), Addresses: s.addresses}
	list, err := s.node.FetchPeers(ctx)
	if err != nil {
		if err.Error() != s.lastErr {
			s.log.Warn("can't fetch peers", "err", err)
		}
		s.lastErr = err.Error()
		out.NodeError = nodeError(err)
	} else {
		s.lastErr = ""
		if !s.addresses {
			for i := range list {
				list[i].Address = ""
			}
		}
		out.Peers = list
	}
	s.cached, s.cachedAt = out, s.now()
	return out
}

// nodeError says in plain language why the list is missing.
func nodeError(err error) string {
	var starting *source.StartingError
	if errors.As(err, &starting) {
		return "bitcoin node is starting: " + starting.Status
	}
	return "can't reach bitcoin node"
}

package control

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MockRunner simulates systemd for a unit, for development and demos in mock
// mode (NODEDASH_MOCK with NODEDASH_CONTROL=1): stop, start and restart go
// through deactivating / activating phases of a few seconds each.
func MockRunner() Runner {
	m := &mockSystemd{active: "active", since: time.Now().Add(-(12*24 + 4) * time.Hour)}
	return m.run
}

type phase struct {
	state string
	until time.Time
}

type mockSystemd struct {
	mu     sync.Mutex
	active string
	since  time.Time
	queue  []phase // upcoming states; each lasts until its time
}

const mockStep = 3 * time.Second

func (m *mockSystemd) run(_ context.Context, args ...string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.advance(time.Now())

	if args[0] == "show" {
		return fmt.Sprintf("LoadState=loaded\nActiveState=%s\nSubState=%s\nActiveEnterTimestamp=@%d\nTimeoutStopUSec=10min\n",
			m.active, map[string]string{"active": "running", "inactive": "dead"}[m.active], m.since.Unix()), nil
	}

	now := time.Now()
	switch action := args[len(args)-2]; action {
	case Stop:
		m.queue = []phase{{"deactivating", now.Add(mockStep)}, {"inactive", time.Time{}}}
	case Start:
		m.queue = []phase{{"activating", now.Add(mockStep)}, {"active", time.Time{}}}
	case Restart:
		m.queue = []phase{{"deactivating", now.Add(mockStep)}, {"activating", now.Add(2 * mockStep)}, {"active", time.Time{}}}
	default:
		return "", fmt.Errorf("unknown command %s", strings.Join(args, " "))
	}
	m.advance(now)
	return "", nil
}

// advance moves through the queued phases that are due.
func (m *mockSystemd) advance(now time.Time) {
	for len(m.queue) > 0 {
		p := m.queue[0]
		if m.active != p.state {
			m.active = p.state
			if p.state == "active" {
				m.since = now
			}
		}
		if p.until.IsZero() || now.Before(p.until) {
			if p.until.IsZero() {
				m.queue = nil
			}
			return
		}
		m.queue = m.queue[1:]
	}
}

package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/poller"
)

type countingSource struct{ fetches atomic.Int64 }

func (c *countingSource) Fetch(context.Context) (*model.NodeData, error) {
	c.fetches.Add(1)
	return &model.NodeData{}, nil
}

// TestVisitorsDontReachTheNode checks story #7: browser tabs read the cached
// snapshot, so any number of visitors causes no extra calls to the node.
func TestVisitorsDontReachTheNode(t *testing.T) {
	src := &countingSource{}
	p := poller.New(src, time.Hour, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.Poll(context.Background())
	srv := httptest.NewServer(New(p, fstest.MapFS{}))
	defer srv.Close()

	var wg sync.WaitGroup
	for range 200 {
		wg.Go(func() {
			resp, err := http.Get(srv.URL + "/api/status")
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		})
	}
	wg.Wait()

	if n := src.fetches.Load(); n != 1 {
		t.Fatalf("node was fetched %d times for 200 visitors, want 1 (the poll)", n)
	}
}

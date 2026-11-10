package source

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMockScenarios(t *testing.T) {
	for _, s := range Scenarios {
		m, err := NewMock(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		d, err := m.Fetch(context.Background())
		if s == "starting" {
			var se *StartingError
			if !errors.As(err, &se) || se.Status == "" {
				t.Fatalf("starting: want a StartingError, got %v", err)
			}
			continue
		}
		if s == "unreachable" {
			if !errors.Is(err, ErrMockUnreachable) {
				t.Fatalf("unreachable: want ErrMockUnreachable, got %v", err)
			}
			continue
		}
		if err != nil || d == nil {
			t.Fatalf("%s: got %v, %v", s, d, err)
		}
	}
}

func TestMockUnknownScenario(t *testing.T) {
	if _, err := NewMock("sideways"); err == nil {
		t.Fatal("expected error for unknown scenario")
	}
}

func TestMockStaleHangsAfterFirstFetch(t *testing.T) {
	m, err := NewMock("stale")
	if err != nil {
		t.Fatal(err)
	}
	if d, err := m.Fetch(context.Background()); err != nil || d == nil {
		t.Fatalf("first fetch: got %v, %v", d, err)
	}
	done := make(chan struct{})
	go func() {
		_, _ = m.Fetch(context.Background())
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("second fetch returned; want it to hang")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMockDetails(t *testing.T) {
	for _, s := range Scenarios {
		m, _ := NewMock(s)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		d, err := m.FetchDetails(ctx)
		cancel()
		switch s {
		case "unreachable", "starting", "stale":
			if err == nil {
				t.Errorf("%s: want an error", s)
			}
		default:
			if err != nil || d.Blockchain.UptimeSec == 0 || len(d.Network.Networks) == 0 {
				t.Errorf("%s: got %+v, %v", s, d, err)
			}
		}
	}
}

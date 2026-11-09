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

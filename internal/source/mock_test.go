package source

import (
	"context"
	"errors"
	"testing"
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

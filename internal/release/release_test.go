package release

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

func core(major, minor, patch int) model.Software {
	return model.Software{Name: "Bitcoin Core", Major: major, Minor: minor, Patch: patch}
}

func TestParseTag(t *testing.T) {
	for tag, want := range map[string]string{"v32.1": "32.1", "v29.2.1": "29.2.1", "v30.0": "30.0"} {
		r, ok := ParseTag(tag)
		if !ok || r.String() != want {
			t.Errorf("ParseTag(%q) = %v %v, want %s", tag, r, ok, want)
		}
	}
	for _, tag := range []string{"v32.0rc1", "v32", "latest", "v32.1.0.1", ""} {
		if r, ok := ParseTag(tag); ok {
			t.Errorf("ParseTag(%q) = %v, want rejected", tag, r)
		}
	}
}

func TestAdvise(t *testing.T) {
	latest := Fixed{32, 1, 0}
	for _, tc := range []struct {
		name           string
		running        model.Software
		available, eol bool
	}{
		{"up to date", core(32, 1, 0), false, false},
		{"maintenance release available", core(32, 0, 0), true, false},
		{"oldest maintained major", core(30, 4, 0), true, false},
		{"end of life", core(29, 5, 0), true, true},
		{"long end of life", core(25, 0, 0), true, true},
		{"newer than the latest release", core(33, 99, 0), false, false},
	} {
		u := Advise(tc.running, latest)
		if u == nil || u.Latest != "32.1" || u.Available != tc.available || u.EndOfLife != tc.eol {
			t.Errorf("%s: got %+v, want available %v, end of life %v", tc.name, u, tc.available, tc.eol)
		}
	}
}

func TestNoAdvice(t *testing.T) {
	knots := model.Software{Name: "Bitcoin Knots", Major: 25}
	if u := Advise(knots, Fixed{32, 1, 0}); u != nil {
		t.Errorf("Knots: got %+v, want no advice", u)
	}
	if u := Advise(core(25, 0, 0), nil); u != nil {
		t.Errorf("update check off: got %+v, want no advice", u)
	}
	if u := Advise(core(25, 0, 0), &Checker{}); u != nil {
		t.Errorf("latest unknown: got %+v, want no advice", u)
	}
}

func newTestChecker(url string) *Checker {
	c := NewChecker("test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.url = url
	return c
}

func TestCheck(t *testing.T) {
	answer := `{"tag_name":"v32.1","prerelease":false,"draft":false}`
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.UserAgent(), "bitcoin-node-dash/") {
			t.Errorf("User-Agent = %q", r.UserAgent())
		}
		w.WriteHeader(status)
		io.WriteString(w, answer)
	}))
	defer srv.Close()

	c := newTestChecker(srv.URL)
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r, ok := c.Latest(); !ok || r.String() != "32.1" {
		t.Fatalf("Latest() = %v %v, want 32.1", r, ok)
	}

	// A failure keeps the last known release: no advice disappears or flips.
	status, answer = http.StatusForbidden, `{"message":"rate limited"}`
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("want an error for HTTP 403")
	}
	if r, ok := c.Latest(); !ok || r.String() != "32.1" {
		t.Fatalf("after a failure: Latest() = %v %v, want 32.1 still", r, ok)
	}
}

func TestCheckRejectsPrerelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"tag_name":"v33.0rc1","prerelease":true}`)
	}))
	defer srv.Close()
	c := newTestChecker(srv.URL)
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("want an error for a pre-release")
	}
	if _, ok := c.Latest(); ok {
		t.Fatal("a pre-release must not become the latest release")
	}
}

func TestCheckUnreachable(t *testing.T) {
	c := newTestChecker("http://127.0.0.1:1")
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("want an error without a connection")
	}
	if u := Advise(core(25, 0, 0), c); u != nil {
		t.Fatalf("without a connection: got %+v, want no advice", u)
	}
}

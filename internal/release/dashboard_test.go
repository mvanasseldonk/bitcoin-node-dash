package release

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

func TestParseVersion(t *testing.T) {
	for v, want := range map[string]string{"v0.5.0": "v0.5.0", "0.5.0": "v0.5.0", "v1.12.3": "v1.12.3"} {
		r, ok := ParseVersion(v)
		if !ok || r.Tag() != want {
			t.Errorf("ParseVersion(%q) = %v %v, want %s", v, r, ok, want)
		}
	}
	for _, v := range []string{"dev", "ci", "v0.5", "v0.6.0-rc1", "v0.6.0rc1", "v0.5.0.1", ""} {
		if r, ok := ParseVersion(v); ok {
			t.Errorf("ParseVersion(%q) = %v, want rejected", v, r)
		}
	}
}

func TestDashboard(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		latest  Latest
		want    model.Dashboard
	}{
		{"newer release", "v0.5.0", Fixed{0, 6, 0}, model.Dashboard{
			Version: "v0.5.0", Check: model.CheckOn, Latest: "v0.6.0", Available: true,
			URL: "https://github.com/mvanasseldonk/bitcoin-node-dash/releases/tag/v0.6.0",
		}},
		{"patch release", "v0.5.0", Fixed{0, 5, 1}, model.Dashboard{
			Version: "v0.5.0", Check: model.CheckOn, Latest: "v0.5.1", Available: true,
			URL: "https://github.com/mvanasseldonk/bitcoin-node-dash/releases/tag/v0.5.1",
		}},
		{"up to date", "v0.5.0", Fixed{0, 5, 0}, model.Dashboard{Version: "v0.5.0", Check: model.CheckOn, Latest: "v0.5.0"}},
		{"never shows an older version as newer", "v0.6.0", Fixed{0, 5, 9}, model.Dashboard{Version: "v0.6.0", Check: model.CheckOn, Latest: "v0.5.9"}},
		{"Docker image version", "0.5.0", Fixed{0, 6, 0}, model.Dashboard{
			Version: "v0.5.0", Check: model.CheckOn, Latest: "v0.6.0", Available: true,
			URL: "https://github.com/mvanasseldonk/bitcoin-node-dash/releases/tag/v0.6.0",
		}},
		{"latest not known yet", "v0.5.0", &Checker{}, model.Dashboard{Version: "v0.5.0", Check: model.CheckOn}},
		{"update check off", "v0.5.0", nil, model.Dashboard{Version: "v0.5.0", Check: model.CheckOff}},
		{"local build", "dev", Fixed{0, 6, 0}, model.Dashboard{Version: "dev", Check: model.CheckLocal}},
	} {
		if got := Dashboard(tc.version, tc.latest, false); got != tc.want {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
	if d := Dashboard("v0.5.0", nil, true); !d.Docker {
		t.Error("Docker isn't passed on")
	}
}

func TestDashboardChecker(t *testing.T) {
	answer := `{"tag_name":"v0.6.0"}`
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, answer)
	}))
	defer srv.Close()

	c := NewDashboardChecker("v0.5.0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if c.url != DashboardLatestURL {
		t.Fatalf("url = %s", c.url)
	}
	c.url = srv.URL + "/repos/mvanasseldonk/bitcoin-node-dash/releases/latest"
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r, ok := c.Latest(); !ok || r.Tag() != "v0.6.0" {
		t.Fatalf("Latest() = %v %v, want v0.6.0", r, ok)
	}
	if path != "/repos/mvanasseldonk/bitcoin-node-dash/releases/latest" {
		t.Errorf("asked %s", path)
	}

	// Only vX.Y.Z counts: an odd tag keeps the last known release.
	answer = `{"tag_name":"v0.7"}`
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("want an error for tag v0.7")
	}
	if r, _ := c.Latest(); r.Tag() != "v0.6.0" {
		t.Fatalf("after an odd tag: Latest() = %v, want v0.6.0 still", r.Tag())
	}
}

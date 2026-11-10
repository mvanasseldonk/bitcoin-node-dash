package release

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// DashboardLatestURL is GitHub's API for the latest node-dash release
// (story #39). Like LatestURL, it skips pre-releases and drafts.
const DashboardLatestURL = "https://api.github.com/repos/mvanasseldonk/bitcoin-node-dash/releases/latest"

// dashboardReleaseURL is the page of one node-dash release, for "What's new".
const dashboardReleaseURL = "https://github.com/mvanasseldonk/bitcoin-node-dash/releases/tag/"

// NewDashboardChecker returns a Checker for the dashboard's own releases.
func NewDashboardChecker(version string, log *slog.Logger) *Checker {
	c := NewChecker(version, log)
	c.url = DashboardLatestURL
	c.what = "node-dash"
	c.parse = ParseVersion
	return c
}

// ParseVersion parses a node-dash version: "v0.5.0", or "0.5.0" as the
// Docker image has it. Anything else isn't a release: "dev" (a local build),
// "ci", "v0.5" or a release candidate.
func ParseVersion(v string) (Release, bool) {
	if strings.Count(v, ".") != 2 {
		return Release{}, false
	}
	return ParseTag(v)
}

// Tag formats a node-dash version as its release tag: "v0.5.0".
func (r Release) Tag() string {
	return fmt.Sprintf("v%d.%d.%d", r.Major, r.Minor, r.Patch)
}

// Dashboard tells which version of the dashboard runs and whether a newer
// release exists. latest is nil when the update check is off; docker tells
// which update command to show.
func Dashboard(version string, latest Latest, docker bool) model.Dashboard {
	d := model.Dashboard{Version: version, Check: model.CheckLocal, Docker: docker}
	own, ok := ParseVersion(version)
	if !ok {
		return d // a local build: no release to compare with
	}
	d.Version = own.Tag()
	if latest == nil {
		d.Check = model.CheckOff
		return d
	}
	d.Check = model.CheckOn
	r, ok := latest.Latest()
	if !ok {
		return d // not looked up yet, or GitHub couldn't be reached
	}
	d.Latest = r.Tag()
	if r.Newer(own) {
		d.Available = true
		d.URL = dashboardReleaseURL + d.Latest
	}
	return d
}

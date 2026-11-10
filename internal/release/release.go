// Package release looks up the latest Bitcoin Core release and turns it into
// upgrade advice (story #24), and the latest release of the dashboard
// itself (story #39).
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// LatestURL is GitHub's API for the latest Bitcoin Core release. It skips
// release candidates (pre-releases) and drafts.
const LatestURL = "https://api.github.com/repos/bitcoin/bitcoin/releases/latest"

// MaintainedMajors is how many major versions of Bitcoin Core get security
// fixes: the latest three (https://bitcoincore.org/en/lifecycle/).
const MaintainedMajors = 3

const (
	checkEvery = 24 * time.Hour
	retryAfter = time.Hour
)

// Release is a Bitcoin Core version.
type Release struct {
	Major, Minor, Patch int
}

// String formats it as people write it: "32.1", "29.2.1".
func (r Release) String() string {
	s := fmt.Sprintf("%d.%d", r.Major, r.Minor)
	if r.Patch != 0 {
		s += fmt.Sprintf(".%d", r.Patch)
	}
	return s
}

func (r Release) newerThan(s model.Software) bool {
	return r.Newer(Release{s.Major, s.Minor, s.Patch})
}

// Newer tells whether r is a later version than o.
func (r Release) Newer(o Release) bool {
	if r.Major != o.Major {
		return r.Major > o.Major
	}
	if r.Minor != o.Minor {
		return r.Minor > o.Minor
	}
	return r.Patch > o.Patch
}

// ParseTag parses a release tag such as "v32.1" or "v29.2.1". Release
// candidates ("v32.0rc1") and anything else are rejected.
func ParseTag(tag string) (Release, bool) {
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Release{}, false
	}
	n := make([]int, 3)
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Release{}, false
		}
		n[i] = v
	}
	return Release{n[0], n[1], n[2]}, true
}

// Latest tells the latest Bitcoin Core release, when known.
type Latest interface {
	Latest() (Release, bool)
}

// Fixed is a Latest that always returns the same release (mock mode).
type Fixed Release

// Latest implements Latest.
func (f Fixed) Latest() (Release, bool) { return Release(f), true }

// Advise returns the upgrade advice for the running software, or nil when
// there is none: not Bitcoin Core, or the latest release is unknown.
func Advise(s model.Software, latest Latest) *model.Upgrade {
	if latest == nil || !s.IsCore() {
		return nil
	}
	r, ok := latest.Latest()
	if !ok {
		return nil
	}
	return &model.Upgrade{
		Latest:    r.String(),
		Available: r.newerThan(s),
		EndOfLife: s.Major <= r.Major-MaintainedMajors,
	}
}

// Checker looks up the latest release on GitHub once a day, and an hour
// after a failure.
type Checker struct {
	url       string
	what      string                           // whose releases, for the log
	parse     func(tag string) (Release, bool) // which tags count
	userAgent string
	client    *http.Client
	log       *slog.Logger

	mu      sync.RWMutex
	latest  Release
	known   bool
	lastErr string
}

// NewChecker returns a Checker; version is the dashboard's own, sent in the
// User-Agent as GitHub asks.
func NewChecker(version string, log *slog.Logger) *Checker {
	return &Checker{
		url:       LatestURL,
		what:      "Bitcoin Core",
		parse:     ParseTag,
		userAgent: "bitcoin-node-dash/" + version,
		client:    &http.Client{Timeout: 15 * time.Second},
		log:       log,
	}
}

// Latest implements Latest.
func (c *Checker) Latest() (Release, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest, c.known
}

// Run checks now and then periodically, until ctx is cancelled.
func (c *Checker) Run(ctx context.Context) {
	for {
		wait := checkEvery
		if err := c.Check(ctx); err != nil {
			wait = retryAfter
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// Check looks up the latest release once. A failure keeps the last known
// release, and is logged once until the cause changes.
func (c *Checker) Check(ctx context.Context) error {
	r, err := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		if err.Error() != c.lastErr && ctx.Err() == nil {
			c.log.Warn("can't look up the latest "+c.what+" release; retrying in an hour", "err", err)
		}
		c.lastErr = err.Error()
		return err
	}
	if !c.known || r != c.latest {
		c.log.Info("latest "+c.what+" release", "version", r.String())
	}
	c.latest, c.known, c.lastErr = r, true, ""
	return nil
}

func (c *Checker) fetch(ctx context.Context) (Release, error) {
	return FetchLatest(ctx, c.client, c.url, c.userAgent, c.parse)
}

// FetchLatest asks a GitHub releases/latest API once for the latest
// release; parse decides which tags count.
func FetchLatest(ctx context.Context, client *http.Client, url, userAgent string, parse func(tag string) (Release, bool)) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered HTTP %d", resp.StatusCode)
	}
	var body struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("unexpected answer from GitHub: %w", err)
	}
	if body.Prerelease || body.Draft {
		return Release{}, errors.New("GitHub's latest release is a pre-release")
	}
	r, ok := parse(body.TagName)
	if !ok {
		return Release{}, fmt.Errorf("unexpected release tag %q", body.TagName)
	}
	return r, nil
}

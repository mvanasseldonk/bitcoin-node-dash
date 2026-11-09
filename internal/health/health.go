// Package health turns node data into the overall status shown in the circle.
package health

import (
	"fmt"
	"math"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// Thresholds for the health rules (story #1). The tiles in the frontend
// (web/src/tiles.tsx) use the same values; keep them in sync.
const (
	// LastBlockWarn is how long without a new block before the node needs attention.
	// A block arrives about every 10 minutes; an hour without one is unusual.
	LastBlockWarn = 60 * time.Minute
	// HeadersBehindWarn is how many blocks the node may lag behind the headers
	// it knows of before it counts as catching up (outside initial sync).
	HeadersBehindWarn = 2
	// FewOutbound is the highest number of outgoing connections that counts as "few".
	FewOutbound = 3
	// DiskFreeWarn and DiskFreeError are the fractions of free disk space
	// below which the node needs attention or has a problem.
	DiskFreeWarn  = 0.10
	DiskFreeError = 0.05
)

type finding struct {
	level  model.Level
	reason string
}

// Evaluate returns the health of a reachable node: the most severe level of
// all rules, with the reasons in plain language, most severe first.
func Evaluate(d *model.NodeData, now time.Time) model.Health {
	var errs, warns []string
	add := func(f finding) {
		if f.level == model.Error {
			errs = append(errs, f.reason)
		} else {
			warns = append(warns, f.reason)
		}
	}
	for _, f := range findings(d, now) {
		add(f)
	}

	reasons := append(errs, warns...)
	switch {
	case len(errs) > 0:
		return model.Health{Level: model.Error, Reasons: reasons}
	case len(warns) > 0:
		return model.Health{Level: model.Warning, Reasons: reasons}
	default:
		return model.Health{Level: model.Healthy, Reasons: []string{}}
	}
}

func findings(d *model.NodeData, now time.Time) []finding {
	var fs []finding

	// Connections: outgoing connections keep the node in sync. Incoming
	// connections are optional (they need an open port), so they never count.
	switch out, in := d.Network.Outbound, d.Network.Inbound; {
	case out == 0 && in == 0:
		fs = append(fs, finding{model.Error, "No peers connected"})
	case out == 0:
		fs = append(fs, finding{model.Error, "No outgoing connections"})
	case out <= FewOutbound:
		fs = append(fs, finding{model.Warning, fmt.Sprintf("Only %d outgoing %s", out, plural(out, "connection", "connections"))})
	}

	// Sync.
	bc := d.Blockchain
	switch {
	case bc.InitialBlockDownload:
		fs = append(fs, finding{model.Warning, fmt.Sprintf("Syncing with the network (%d%%)", int(math.Floor(bc.VerificationProgress*100)))})
	case bc.Headers-bc.Blocks > HeadersBehindWarn:
		behind := bc.Headers - bc.Blocks
		fs = append(fs, finding{model.Warning, fmt.Sprintf("Catching up: %d blocks behind", behind)})
	case !bc.LastBlockTime.IsZero() && now.Sub(bc.LastBlockTime) > LastBlockWarn:
		fs = append(fs, finding{model.Warning, "No new block for " + Duration(now.Sub(bc.LastBlockTime))})
	}

	// Disk space of the volume with the blockchain data (unknown when total is 0).
	if s := d.Server; s.DiskTotalBytes > 0 {
		free := float64(s.DiskFreeBytes) / float64(s.DiskTotalBytes)
		full := fmt.Sprintf("Disk %d%% full", int(math.Round((1-free)*100)))
		switch {
		case free < DiskFreeError:
			fs = append(fs, finding{model.Error, full})
		case free < DiskFreeWarn:
			fs = append(fs, finding{model.Warning, full})
		}
	}

	return fs
}

// Duration formats a duration in plain language: "74 minutes", "3 hours", "2 days".
// Minutes are used up to two hours, so short delays stay precise.
func Duration(d time.Duration) string {
	m := int(d.Minutes())
	switch {
	case m < 120:
		return fmt.Sprintf("%d %s", m, plural(m, "minute", "minutes"))
	case m < 48*60:
		h := m / 60
		return fmt.Sprintf("%d %s", h, plural(h, "hour", "hours"))
	default:
		days := m / (24 * 60)
		return fmt.Sprintf("%d %s", days, plural(days, "day", "days"))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Unreachable is the health when the node cannot be reached.
func Unreachable() model.Health {
	return model.Health{Level: model.Unreachable, Reasons: []string{"Can't reach bitcoin node"}}
}

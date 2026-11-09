package health

import (
	"reflect"
	"testing"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

const gb = 1_000_000_000

var now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

// healthy returns node data that passes every rule.
func healthy() *model.NodeData {
	return &model.NodeData{
		Blockchain: model.Blockchain{
			Blocks: 977_412, Headers: 977_412, VerificationProgress: 0.99999,
			LastBlockTime: now.Add(-4 * time.Minute),
		},
		Network: model.Network{Outbound: 10, Inbound: 6},
		Server:  model.Server{DiskFreeBytes: 812 * gb, DiskTotalBytes: 2000 * gb},
	}
}

func TestBytes(t *testing.T) {
	for in, want := range map[uint64]string{
		450_000_000:       "450 MB",
		47 * gb:           "47 GB",
		812 * gb:          "812 GB",
		1_340_000_000_000: "1.3 TB",
	} {
		if got := Bytes(in); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(d *model.NodeData)
		level   model.Level
		reasons []string
	}{
		{"healthy", func(*model.NodeData) {}, model.Healthy, []string{}},
		{"no inbound is fine", func(d *model.NodeData) { d.Network.Inbound = 0 }, model.Healthy, []string{}},
		{"unknown disk is fine", func(d *model.NodeData) { d.Server = model.Server{} }, model.Healthy, []string{}},

		{"no peers", func(d *model.NodeData) { d.Network = model.Network{} }, model.Error, []string{"No peers connected"}},
		{"only inbound", func(d *model.NodeData) { d.Network = model.Network{Outbound: 0, Inbound: 4} }, model.Error, []string{"No outgoing connections"}},
		{"one outbound", func(d *model.NodeData) { d.Network.Outbound = 1 }, model.Warning, []string{"Only 1 outgoing connection"}},
		{"three outbound", func(d *model.NodeData) { d.Network.Outbound = 3 }, model.Warning, []string{"Only 3 outgoing connections"}},
		{"four outbound", func(d *model.NodeData) { d.Network.Outbound = 4 }, model.Healthy, []string{}},

		{"initial sync", func(d *model.NodeData) {
			d.Blockchain.InitialBlockDownload = true
			d.Blockchain.VerificationProgress = 0.876
			d.Blockchain.LastBlockTime = now.Add(-700 * 24 * time.Hour)
		}, model.Warning, []string{"Syncing with the network (87%)"}},
		{"behind on headers", func(d *model.NodeData) { d.Blockchain.Blocks -= 5 }, model.Warning, []string{"Catching up: 5 blocks behind"}},
		{"two behind is fine", func(d *model.NodeData) { d.Blockchain.Blocks -= 2 }, model.Healthy, []string{}},
		{"no block for 74 min", func(d *model.NodeData) { d.Blockchain.LastBlockTime = now.Add(-74 * time.Minute) }, model.Warning, []string{"No new block for 74 minutes"}},
		{"no block for 60 min", func(d *model.NodeData) { d.Blockchain.LastBlockTime = now.Add(-60 * time.Minute) }, model.Healthy, []string{}},

		// Large disk (2 TB): the absolute limits count (50 GB / 10 GB).
		{"160 GB free is fine", func(d *model.NodeData) { d.Server.DiskFreeBytes = 160 * gb }, model.Healthy, []string{}},
		{"50 GB free is fine", func(d *model.NodeData) { d.Server.DiskFreeBytes = 50 * gb }, model.Healthy, []string{}},
		{"47 GB free", func(d *model.NodeData) { d.Server.DiskFreeBytes = 47 * gb }, model.Warning, []string{"Low disk space: 47 GB free"}},
		{"10 GB free", func(d *model.NodeData) { d.Server.DiskFreeBytes = 10 * gb }, model.Warning, []string{"Low disk space: 10 GB free"}},
		{"8 GB free", func(d *model.NodeData) { d.Server.DiskFreeBytes = 8 * gb }, model.Error, []string{"Disk almost full: 8 GB free"}},
		{"450 MB free", func(d *model.NodeData) { d.Server.DiskFreeBytes = 450_000_000 }, model.Error, []string{"Disk almost full: 450 MB free"}},
		// Small disk (64 GB, e.g. a pruned node): the fractions count (6.4 GB / 3.2 GB).
		{"small disk, 20 GB free is fine", func(d *model.NodeData) { d.Server = model.Server{DiskFreeBytes: 20 * gb, DiskTotalBytes: 64 * gb} }, model.Healthy, []string{}},
		{"small disk, 5 GB free", func(d *model.NodeData) { d.Server = model.Server{DiskFreeBytes: 5 * gb, DiskTotalBytes: 64 * gb} }, model.Warning, []string{"Low disk space: 5 GB free"}},
		{"small disk, 3 GB free", func(d *model.NodeData) { d.Server = model.Server{DiskFreeBytes: 3 * gb, DiskTotalBytes: 64 * gb} }, model.Error, []string{"Disk almost full: 3 GB free"}},

		// Software (story #24): only end of life needs attention.
		{"newer release available is fine", func(d *model.NodeData) {
			d.Software = model.Software{Name: "Bitcoin Core", Label: "Bitcoin Core 32.0"}
			d.Upgrade = &model.Upgrade{Latest: "32.1", Available: true}
		}, model.Healthy, []string{}},
		{"end of life", func(d *model.NodeData) {
			d.Software = model.Software{Name: "Bitcoin Core", Label: "Bitcoin Core 28.1"}
			d.Upgrade = &model.Upgrade{Latest: "32.1", Available: true, EndOfLife: true}
		}, model.Warning, []string{"Bitcoin Core 28.1 no longer gets security fixes · upgrade to 32.1"}},

		{"errors before warnings", func(d *model.NodeData) {
			d.Blockchain.LastBlockTime = now.Add(-74 * time.Minute)
			d.Network = model.Network{}
		}, model.Error, []string{"No peers connected", "No new block for 74 minutes"}},
		{"several warnings", func(d *model.NodeData) {
			d.Blockchain.LastBlockTime = now.Add(-74 * time.Minute)
			d.Server.DiskFreeBytes = 38 * gb
		}, model.Warning, []string{"No new block for 74 minutes", "Low disk space: 38 GB free"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := healthy()
			tt.edit(d)
			got := Evaluate(d, now)
			if got.Level != tt.level || !reflect.DeepEqual(got.Reasons, tt.reasons) {
				t.Fatalf("got %s %q, want %s %q", got.Level, got.Reasons, tt.level, tt.reasons)
			}
		})
	}
}

func TestDuration(t *testing.T) {
	tests := map[time.Duration]string{
		1 * time.Minute:  "1 minute",
		74 * time.Minute: "74 minutes",
		3 * time.Hour:    "3 hours",
		50 * time.Hour:   "2 days",
	}
	for d, want := range tests {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
}

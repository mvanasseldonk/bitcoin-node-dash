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

		{"disk 92% full", func(d *model.NodeData) { d.Server.DiskFreeBytes = 160 * gb }, model.Warning, []string{"Disk 92% full"}},
		{"disk 90% full", func(d *model.NodeData) { d.Server.DiskFreeBytes = 200 * gb }, model.Healthy, []string{}},
		{"disk 97% full", func(d *model.NodeData) { d.Server.DiskFreeBytes = 60 * gb }, model.Error, []string{"Disk 97% full"}},

		{"errors before warnings", func(d *model.NodeData) {
			d.Blockchain.LastBlockTime = now.Add(-74 * time.Minute)
			d.Network = model.Network{}
		}, model.Error, []string{"No peers connected", "No new block for 74 minutes"}},
		{"several warnings", func(d *model.NodeData) {
			d.Blockchain.LastBlockTime = now.Add(-74 * time.Minute)
			d.Server.DiskFreeBytes = 152 * gb
		}, model.Warning, []string{"No new block for 74 minutes", "Disk 92% full"}},
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

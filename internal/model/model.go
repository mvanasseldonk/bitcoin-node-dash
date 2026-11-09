// Package model holds the data types shared between the data sources,
// the poller and the HTTP API.
package model

import "time"

// Level is the overall health level of the node, shown as the status circle.
type Level string

const (
	Healthy     Level = "healthy"
	Warning     Level = "warning"
	Error       Level = "error"
	Unreachable Level = "unreachable"
	// Starting means Bitcoin Core answers but is still starting up
	// (RPC warm-up: loading the block index, verifying blocks, ...).
	Starting Level = "starting"
)

// NodeData is everything a data source returns on one successful fetch.
type NodeData struct {
	Blockchain Blockchain `json:"blockchain"`
	Network    Network    `json:"network"`
	Server     Server     `json:"server"`
	Software   Software   `json:"software"`
	// Upgrade is the advice about the node software, or nil when there is
	// none to give (not Bitcoin Core, or the latest release is unknown).
	Upgrade *Upgrade `json:"upgrade"`
}

// Software is the node software and its version (story #24).
type Software struct {
	// Name is "Bitcoin Core", "Bitcoin Knots", or "" when unknown.
	Name string `json:"name"`
	// Version as people write it: "32.1", "30.0", "29.2.1".
	Version string `json:"version"`
	// Label is what the dashboard shows, e.g. "Bitcoin Core 32.1", or the
	// raw user agent (subversion) for software it doesn't know.
	Label string `json:"label"`
	// Major, Minor and Patch of the Bitcoin Core code base.
	Major int `json:"-"`
	Minor int `json:"-"`
	Patch int `json:"-"`
}

// IsCore reports whether this is Bitcoin Core itself, whose release
// schedule the upgrade advice is based on.
func (s Software) IsCore() bool { return s.Name == "Bitcoin Core" }

// Upgrade is the advice to upgrade the node software.
type Upgrade struct {
	// Latest is the latest Bitcoin Core release, e.g. "32.1".
	Latest string `json:"latest"`
	// Available is true when Latest is newer than the running version.
	Available bool `json:"available"`
	// EndOfLife is true when the running major version no longer gets
	// security fixes.
	EndOfLife bool `json:"endOfLife"`
}

// Blockchain describes the sync state of the node.
type Blockchain struct {
	Chain                string    `json:"chain"`
	Blocks               int64     `json:"blocks"`
	Headers              int64     `json:"headers"`
	VerificationProgress float64   `json:"verificationProgress"`
	InitialBlockDownload bool      `json:"initialBlockDownload"`
	LastBlockTime        time.Time `json:"lastBlockTime"`
}

// Network describes how the node is connected to its peers.
type Network struct {
	Outbound int `json:"outbound"`
	Inbound  int `json:"inbound"`
}

// Server describes the resources of the host the node runs on.
type Server struct {
	DiskFreeBytes  uint64 `json:"diskFreeBytes"`
	DiskTotalBytes uint64 `json:"diskTotalBytes"`
}

// Health is the evaluated status of the node with human-readable reasons.
type Health struct {
	Level   Level    `json:"level"`
	Reasons []string `json:"reasons"`
}

// Snapshot is what the API serves to the dashboard.
//
// Data holds the last successfully fetched values. When the node becomes
// unreachable, Data is kept so the dashboard can show last known values.
type Snapshot struct {
	// CheckedAt is the time of the last fetch attempt.
	CheckedAt time.Time `json:"checkedAt"`
	// UpdatedAt is the time of the last successful fetch (zero if none yet).
	UpdatedAt time.Time `json:"updatedAt"`
	Reachable bool      `json:"reachable"`
	Health    Health    `json:"health"`
	Data      *NodeData `json:"data"`
	// ServerTime is the dashboard server's clock when the snapshot was served.
	// Browsers use it to measure data age without trusting their own clock.
	ServerTime time.Time `json:"serverTime"`
}

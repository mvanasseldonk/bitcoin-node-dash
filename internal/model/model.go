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
)

// NodeData is everything a data source returns on one successful fetch.
type NodeData struct {
	Blockchain Blockchain `json:"blockchain"`
	Network    Network    `json:"network"`
	Server     Server     `json:"server"`
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

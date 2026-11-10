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
	// Dashboard is about the dashboard itself: its version and whether a
	// newer release exists (story #39). Set when the snapshot is served.
	Dashboard *Dashboard `json:"dashboard,omitempty"`
}

// Whether the dashboard looks up its own latest release.
const (
	CheckOn    = "on"
	CheckOff   = "off"   // NODEDASH_UPDATE_CHECK=0
	CheckLocal = "local" // a local build ("dev"): there is no release to compare with
)

// Dashboard is the version of the dashboard and its latest release.
type Dashboard struct {
	// Version is the running version, e.g. "v0.5.0", or "dev" for a local build.
	Version string `json:"version"`
	// Check is CheckOn, CheckOff or CheckLocal.
	Check string `json:"check"`
	// Latest is the latest release, e.g. "v0.6.0"; empty while unknown.
	Latest string `json:"latest,omitempty"`
	// Available is true when Latest is newer than Version.
	Available bool `json:"available"`
	// URL is the page of the latest release, when it is newer.
	URL string `json:"url,omitempty"`
	// Docker is true in the Docker image, which is updated another way.
	Docker bool `json:"docker"`
}

// Details are the extra numbers for the details page (story #14). They are
// only fetched while someone has that page open. They never include
// addresses of the node or its peers: the dashboard has no login.
type Details struct {
	// FetchedAt is when the numbers were read from the node.
	FetchedAt time.Time `json:"fetchedAt"`
	// Blockchain and Network are nil while the node can't be reached;
	// NodeError then says why, in plain language.
	Blockchain *BlockchainDetails `json:"blockchain"`
	Network    *NetworkDetails    `json:"network"`
	NodeError  string             `json:"nodeError"`
	// Server is read from the operating system, also when the node is down.
	Server ServerDetails `json:"server"`
}

// ServerDetails are the figures of the machine the dashboard runs on
// (story #36). Zero or nil values are unknown; Missing says why.
type ServerDetails struct {
	// CPUPercent is the CPU usage since the previous reading (nil on the first).
	CPUPercent    *float64 `json:"cpuPercent"`
	MemUsedBytes  uint64   `json:"memUsedBytes"`
	MemTotalBytes uint64   `json:"memTotalBytes"`
	// Disk is the filesystem with the Bitcoin data directory.
	DiskFreeBytes  uint64 `json:"diskFreeBytes"`
	DiskUsedBytes  uint64 `json:"diskUsedBytes"`
	DiskTotalBytes uint64 `json:"diskTotalBytes"`
	// CPUTempC is the CPU temperature in °C, when the server exposes it.
	CPUTempC  *float64 `json:"cpuTempC"`
	UptimeSec int64    `json:"uptimeSec"`
	// Missing maps a figure ("cpu", "memory", "disk", "temperature",
	// "uptime") to the reason it is unknown, e.g. "only available on Linux".
	Missing map[string]string `json:"missing"`
	// CPU is the raw reading for CPUPercent.
	CPU CPUSample `json:"-"`
}

// CPUSample is a reading of the CPU time counters (/proc/stat), in ticks.
type CPUSample struct {
	Total, Idle uint64
}

// BlockchainDetails adds to Blockchain.
type BlockchainDetails struct {
	// UptimeSec is how long bitcoind has been running.
	UptimeSec int64 `json:"uptimeSec"`
	// SizeOnDiskBytes is the size of the block and undo files.
	SizeOnDiskBytes uint64 `json:"sizeOnDiskBytes"`
	Pruned          bool   `json:"pruned"`
}

// NetworkDetails adds to Network.
type NetworkDetails struct {
	// Networks lists each network type and whether it is enabled.
	Networks []NetworkType `json:"networks"`
	// TotalSentBytes and TotalRecvBytes count since bitcoind started.
	TotalSentBytes uint64 `json:"totalSentBytes"`
	TotalRecvBytes uint64 `json:"totalRecvBytes"`
	// SendRate and RecvRate are in bytes per second, nil while unknown
	// (the first reading, or right after bitcoind restarted).
	SendRate *float64 `json:"sendRate"`
	RecvRate *float64 `json:"recvRate"`
}

// NetworkType is one kind of network: ipv4, ipv6, onion (Tor), i2p, cjdns.
type NetworkType struct {
	Name string `json:"name"`
	// Enabled is getnetworkinfo's "reachable": the node may connect over it.
	Enabled bool `json:"enabled"`
	// Peers is the number of connected peers on this network.
	Peers int `json:"peers"`
}

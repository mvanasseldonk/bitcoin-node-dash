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
	// Mempool is nil when the source doesn't read it.
	Mempool *Mempool `json:"mempool"`
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

// Mempool is the node's mempool and its fee estimates (story #11). It never
// changes the health: a busy network says nothing about the node.
type Mempool struct {
	// Error says in plain language why the mempool is unknown; the other
	// fields are then zero. The node stays reachable.
	Error string `json:"error,omitempty"`
	// Loaded is false while Bitcoin Core loads mempool.dat after a start:
	// the numbers are too low until then.
	Loaded       bool  `json:"loaded"`
	Transactions int64 `json:"transactions"`
	// VBytes is the sum of the transactions' virtual sizes.
	VBytes uint64 `json:"vbytes"`
	// UsageBytes is the memory in use, of at most MaxBytes (maxmempool).
	UsageBytes uint64 `json:"usageBytes"`
	MaxBytes   uint64 `json:"maxBytes"`
	// MinFee and MinRelayFee are in sat/vB, rounded like Fees.
	MinFee      float64 `json:"minFee"`
	MinRelayFee float64 `json:"minRelayFee"`
	// Fees are the estimates for 2, 6 and 144 blocks (economical).
	Fees []FeeEstimate `json:"fees"`
}

// FeeEstimate is the fee rate to be confirmed within Blocks blocks.
type FeeEstimate struct {
	Blocks int `json:"blocks"`
	// Rate is in sat/vB: one decimal under 10, whole numbers above. Nil
	// when unknown; Missing then says why.
	Rate    *float64 `json:"rate"`
	Missing string   `json:"missing,omitempty"`
}

// Server describes the resources of the host the node runs on. The disk
// comes from the source; the rest from the poller's ServerReader, for the
// meters on the home page (story #65).
type Server struct {
	DiskFreeBytes  uint64 `json:"diskFreeBytes"`
	DiskTotalBytes uint64 `json:"diskTotalBytes"`
	// CPUPercent is the CPU usage since the previous poll, nil on the first.
	CPUPercent    *float64 `json:"cpuPercent"`
	MemUsedBytes  uint64   `json:"memUsedBytes"`
	MemTotalBytes uint64   `json:"memTotalBytes"`
	// CPUTempC is nil when the server doesn't expose a CPU sensor.
	CPUTempC  *float64 `json:"cpuTempC"`
	UptimeSec int64    `json:"uptimeSec"`
	// Missing maps "cpu", "memory", "temperature" or "uptime" to the reason
	// it is unknown, like ServerDetails.Missing.
	Missing map[string]string `json:"missing,omitempty"`
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
	// WebSettings is true when the dashboard settings page is on (#61).
	WebSettings bool `json:"webSettings,omitempty"`
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
	// AutoUpdate is the last run of the automatic updates (story #50); nil
	// when they are off.
	AutoUpdate *AutoUpdate `json:"autoUpdate,omitempty"`
}

// Results of an automatic update.
const (
	UpdateUpdated = "updated" // a newer release was installed
	UpdateCurrent = "current" // already the latest release
	UpdateSkipped = "skipped" // a new major version, or a local build: needs the install command
	UpdateRefused = "refused" // the signature or checksum didn't check out: nothing was installed
	UpdateFailed  = "failed"  // couldn't look up or download, or the new version didn't start
)

// AutoUpdate is what the updater (node-dash update, run by root once a day)
// writes to its status file, and the dashboard shows.
type AutoUpdate struct {
	LastRun time.Time `json:"lastRun"`
	Result  string    `json:"result"`
	// From is the version that ran; To the release it was about.
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Message string `json:"message,omitempty"`
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
	// Settings are the node's main settings (story #13), from bitcoin.conf,
	// its command line and the node; nil when they can't be read at all.
	Settings *Settings `json:"settings"`
	// Running are the settings the node reports over RPC, for Settings.
	Running *RunningSettings `json:"-"`
}

// Sources of a setting's value (story #13).
const (
	FromDefault = "default" // Bitcoin Core's default
	FromConf    = "conf"    // bitcoin.conf; SourceDetail is the section, e.g. "[main]"
	FromInclude = "include" // a file named by includeconf; SourceDetail is its name
	FromCmdline = "cmdline" // bitcoind's command line; SourceDetail is the flag
	FromRunning = "running" // the node reports it; where it is set is unknown
	FromUnknown = "unknown"
	// FromSettingsFile is Bitcoin Core's settings.json, which wins over
	// bitcoin.conf (story #55).
	FromSettingsFile = "settingsfile"
)

// Settings are the main settings of the node, read-only (story #13). Only
// these settings leave the server: never other lines of bitcoin.conf.
type Settings struct {
	// Path is the bitcoin.conf that was read.
	Path string `json:"path"`
	// FileError says why bitcoin.conf couldn't be read, in plain language.
	FileError string    `json:"fileError,omitempty"`
	Items     []Setting `json:"items"`
	// Editable is true when the settings form is on (NODEDASH_SETTINGS=1,
	// story #55).
	Editable bool `json:"editable,omitempty"`
	// RestartNeeded is true when the form saved a change and bitcoind
	// hasn't restarted since.
	RestartNeeded bool `json:"restartNeeded,omitempty"`
}

// Setting is one setting and where its value comes from.
type Setting struct {
	Key string `json:"key"`
	// Value is in the setting's unit: MiB for dbcache, prune and
	// maxuploadtarget, MB for maxmempool, a count for maxconnections, 0 or 1
	// for txindex, listen and blocksonly. For prune, 1 is manual pruning.
	// Nil when unknown.
	Value        *int64 `json:"value"`
	Source       string `json:"source"`
	SourceDetail string `json:"sourceDetail,omitempty"`
	// ConfValue is what bitcoin.conf or the command line says, when the node
	// runs with something else: it hasn't been restarted since.
	ConfValue *int64 `json:"confValue,omitempty"`
	// Invalid is a configured value that isn't a valid number.
	Invalid string `json:"invalid,omitempty"`
}

// RunningSettings are the settings the node reports over RPC. Nil fields
// are unknown.
type RunningSettings struct {
	Chain           string
	Prune           *int64 // 0 off, 1 manual, else the target in MiB
	TxIndex         *int64
	MaxMempool      *int64 // MB
	MaxUploadTarget *int64 // MiB, 0 unlimited
	BlocksOnly      *int64
	// Started is when bitcoind started, from its uptime; zero when unknown.
	Started time.Time
}

// SettingsForm is what the settings form shows and edits (story #55). Like
// Settings, it holds only the main settings: never other lines of
// bitcoin.conf.
type SettingsForm struct {
	// Path is the bitcoin.conf the form saves to.
	Path string `json:"path"`
	// Hash identifies bitcoin.conf as read (an HMAC with a key of this
	// process). A save sends it back, and is refused when the file changed
	// since.
	Hash string `json:"hash"`
	// Error says why the form can't save, e.g. a file that isn't writable.
	// The fields are then shown, read-only.
	Error  string         `json:"error,omitempty"`
	Fields []SettingField `json:"fields"`
	// Pruned is whether the node reports it is pruned; nil when unknown.
	Pruned *bool `json:"pruned"`
	// MemTotalBytes and DiskFreeBytes are the server's, for the hints; 0
	// when unknown.
	MemTotalBytes uint64 `json:"memTotalBytes"`
	DiskFreeBytes uint64 `json:"diskFreeBytes"`
	// Service is bitcoind's systemd service, for the restart command.
	Service string `json:"service"`
	// Saved is the last save, while bitcoind hasn't restarted since.
	Saved *SavedSettings `json:"saved"`
}

// SettingField is one setting in the form.
type SettingField struct {
	Key string `json:"key"`
	// Value is what is configured: in bitcoin.conf, or where Source says.
	// Nil when it isn't set anywhere, so Default applies.
	Value *int64 `json:"value"`
	// Default is Bitcoin Core's default; nil when it depends on the version.
	Default *int64 `json:"default"`
	// Running is what the node runs with, when it reports it.
	Running      *int64 `json:"running"`
	Source       string `json:"source"`
	SourceDetail string `json:"sourceDetail,omitempty"`
	// Locked says why the form can't change this setting, e.g. it is set on
	// the command line.
	Locked string `json:"locked,omitempty"`
}

// SavedSettings is a save of the settings form.
type SavedSettings struct {
	At      time.Time       `json:"at"`
	Changes []SettingChange `json:"changes"`
	// RestartNeeded is true until bitcoind has started after At.
	RestartNeeded bool `json:"restartNeeded"`
}

// SettingChange is one changed setting. From is nil when it wasn't set.
type SettingChange struct {
	Key  string `json:"key"`
	From *int64 `json:"from"`
	To   int64  `json:"to"`
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

// CPUBusy returns the CPU usage in percent between two readings, and false
// when they can't be compared: no previous reading, or the counters went
// back (a reboot).
func CPUBusy(prev, cur CPUSample) (float64, bool) {
	if prev.Total == 0 || cur.Total <= prev.Total || cur.Idle < prev.Idle {
		return 0, false
	}
	busy := float64((cur.Total-prev.Total)-(cur.Idle-prev.Idle)) / float64(cur.Total-prev.Total) * 100
	return min(max(busy, 0), 100), true
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

// Peers is the list for the Peers page (story #12). It is only fetched while
// someone has that page open.
type Peers struct {
	// FetchedAt is when the list was read from the node.
	FetchedAt time.Time `json:"fetchedAt"`
	// Peers is nil while the node can't be reached; NodeError then says why.
	Peers     []Peer `json:"peers"`
	NodeError string `json:"nodeError"`
	// Addresses is true with NODEDASH_PEER_ADDRESSES=1: only then do peers
	// carry their address.
	Addresses bool `json:"addresses"`
}

// Peer is one connected peer. Never the node's own address.
type Peer struct {
	ID int64 `json:"id"`
	// Network is ipv4, ipv6, onion, i2p, cjdns or not_publicly_routable.
	Network string `json:"network"`
	Inbound bool   `json:"inbound"`
	// Type is the connection type in plain language: "Full relay",
	// "Blocks only", "Manual", "Feeler", "Address fetch"; "" for inbound.
	Type string `json:"type"`
	// Client is e.g. "Bitcoin Core 27.0", or the raw user agent.
	Client string `json:"client"`
	// PingMs is nil until the node measured it.
	PingMs      *float64  `json:"pingMs"`
	ConnectedAt time.Time `json:"connectedAt"`
	SentBytes   uint64    `json:"sentBytes"`
	RecvBytes   uint64    `json:"recvBytes"`
	// Encrypted is true for the v2 transport (BIP324).
	Encrypted bool `json:"encrypted"`
	// Address is only set with NODEDASH_PEER_ADDRESSES=1.
	Address string `json:"address,omitempty"`
}

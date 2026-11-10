package source

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// PeerSource fetches the connected peers for the Peers page (story #12).
// Peers carry their address; the caller drops it unless it is asked for.
type PeerSource interface {
	FetchPeers(ctx context.Context) ([]model.Peer, error)
}

// connectionTypes are getpeerinfo's connection types in plain language.
var connectionTypes = map[string]string{
	"outbound-full-relay": "Full relay",
	"block-relay-only":    "Blocks only",
	"manual":              "Manual",
	"feeler":              "Feeler",
	"addr-fetch":          "Address fetch",
	"inbound":             "",
}

// ConnectionType returns the connection type in plain language.
func ConnectionType(t string) string {
	if label, ok := connectionTypes[t]; ok {
		return label
	}
	return t
}

// PeerClient names the software of a peer from its user agent, e.g.
// "/Satoshi:27.0.0/" is "Bitcoin Core 27.0", and
// "/Satoshi:28.1.0/Knots:20250305/" is "Bitcoin Knots 28.1". Software it
// doesn't know keeps its user agent; an empty one is "Unknown".
func PeerClient(subver string) string {
	var parts []string
	for _, p := range strings.Split(strings.Trim(subver, "/"), "/") {
		name, _, _ := strings.Cut(p, "(") // drop uacomment
		if name != "" {
			parts = append(parts, name)
		}
	}
	if len(parts) == 0 {
		return "Unknown"
	}
	core, ok := strings.CutPrefix(parts[0], "Satoshi:")
	switch {
	case ok && len(parts) == 1:
		return "Bitcoin Core " + shortVersion(core)
	case ok && len(parts) == 2 && strings.HasPrefix(parts[1], "Knots:"):
		return "Bitcoin Knots " + shortVersion(core)
	}
	return strings.Trim(subver, "/")
}

// shortVersion drops a zero patch number: "27.0.0" is "27.0", "29.2.1" stays.
func shortVersion(v string) string {
	if s, ok := strings.CutSuffix(v, ".0"); ok && strings.Count(v, ".") == 2 {
		return s
	}
	return v
}

// SortPeers puts the peers in the default order: connected longest first,
// so the list stays put between refreshes.
func SortPeers(peers []model.Peer) {
	sort.SliceStable(peers, func(i, j int) bool {
		if !peers[i].ConnectedAt.Equal(peers[j].ConnectedAt) {
			return peers[i].ConnectedAt.Before(peers[j].ConnectedAt)
		}
		return peers[i].ID < peers[j].ID
	})
}

// FetchPeers implements PeerSource. addrlocal, the node's own address as a
// peer sees it, is never read.
func (r *RPC) FetchPeers(ctx context.Context) ([]model.Peer, error) {
	var raw []struct {
		ID             int64    `json:"id"`
		Addr           string   `json:"addr"`
		Network        string   `json:"network"`
		Inbound        bool     `json:"inbound"`
		ConnectionType string   `json:"connection_type"`
		Subver         string   `json:"subver"`
		PingTime       *float64 `json:"pingtime"`
		ConnTime       int64    `json:"conntime"`
		BytesSent      uint64   `json:"bytessent"`
		BytesRecv      uint64   `json:"bytesrecv"`
		Transport      string   `json:"transport_protocol_type"`
	}
	if err := r.call(ctx, "getpeerinfo", &raw); err != nil {
		return nil, err
	}
	peers := make([]model.Peer, 0, len(raw))
	for _, p := range raw {
		peer := model.Peer{
			ID:          p.ID,
			Network:     p.Network,
			Inbound:     p.Inbound,
			Type:        ConnectionType(p.ConnectionType),
			Client:      PeerClient(p.Subver),
			ConnectedAt: time.Unix(p.ConnTime, 0).UTC(),
			SentBytes:   p.BytesSent,
			RecvBytes:   p.BytesRecv,
			Encrypted:   p.Transport == "v2",
			Address:     p.Addr,
		}
		if p.Inbound {
			peer.Type = ""
		}
		if p.PingTime != nil {
			ms := *p.PingTime * 1000
			peer.PingMs = &ms
		}
		peers = append(peers, peer)
	}
	SortPeers(peers)
	return peers, nil
}

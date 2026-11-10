package source

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// DetailSource fetches the extra numbers for the details page (story #14).
// Rates are left nil: the caller computes them from two readings.
type DetailSource interface {
	FetchDetails(ctx context.Context) (*model.Details, error)
}

// networkOrder is the order networks are shown in; others follow.
var networkOrder = []string{"ipv4", "ipv6", "onion", "i2p", "cjdns"}

// FetchDetails implements DetailSource. Of the peers, only the count per
// network leaves this function: never addresses.
func (r *RPC) FetchDetails(ctx context.Context) (*model.Details, error) {
	var bc struct {
		Chain           string `json:"chain"`
		SizeOnDisk      uint64 `json:"size_on_disk"`
		Pruned          bool   `json:"pruned"`
		PruneTargetSize *int64 `json:"prune_target_size"`
	}
	if err := r.call(ctx, "getblockchaininfo", &bc); err != nil {
		return nil, err
	}
	var uptime int64
	if err := r.call(ctx, "uptime", &uptime); err != nil {
		return nil, err
	}
	var ni struct {
		Networks []struct {
			Name      string `json:"name"`
			Reachable bool   `json:"reachable"`
		} `json:"networks"`
		LocalRelay *bool `json:"localrelay"`
	}
	if err := r.call(ctx, "getnetworkinfo", &ni); err != nil {
		return nil, err
	}
	var totals struct {
		TotalBytesRecv uint64 `json:"totalbytesrecv"`
		TotalBytesSent uint64 `json:"totalbytessent"`
		TimeMillis     int64  `json:"timemillis"`
		UploadTarget   struct {
			Target *int64 `json:"target"`
		} `json:"uploadtarget"`
	}
	if err := r.call(ctx, "getnettotals", &totals); err != nil {
		return nil, err
	}
	var peers []struct {
		Network string `json:"network"`
	}
	if err := r.call(ctx, "getpeerinfo", &peers); err != nil {
		return nil, err
	}

	perNetwork := map[string]int{}
	for _, p := range peers {
		perNetwork[p.Network]++
	}
	enabled := map[string]bool{}
	for _, n := range ni.Networks {
		enabled[n.Name] = n.Reachable
	}

	d := &model.Details{
		FetchedAt: time.UnixMilli(totals.TimeMillis).UTC(),
		Blockchain: &model.BlockchainDetails{
			UptimeSec:       uptime,
			SizeOnDiskBytes: bc.SizeOnDisk,
			Pruned:          bc.Pruned,
		},
		Network: &model.NetworkDetails{
			Networks:       networkTypes(enabled, perNetwork),
			TotalSentBytes: totals.TotalBytesSent,
			TotalRecvBytes: totals.TotalBytesRecv,
		},
	}
	if totals.TimeMillis == 0 {
		d.FetchedAt = time.Now().UTC()
	}
	d.Running = r.runningSettings(ctx, bc.Chain, bc.Pruned, bc.PruneTargetSize, ni.LocalRelay, totals.UploadTarget.Target)
	d.Running.Started = d.FetchedAt.Add(-time.Duration(uptime) * time.Second)
	return d, nil
}

const mib = 1 << 20

// runningSettings collects the settings the node reports (story #13). A
// call that fails leaves its setting unknown; it never fails the details.
func (r *RPC) runningSettings(ctx context.Context, chain string, pruned bool, pruneTarget *int64, localRelay *bool, uploadTarget *int64) *model.RunningSettings {
	val := func(v int64) *int64 { return &v }
	rs := &model.RunningSettings{Chain: chain}
	switch {
	case !pruned:
		rs.Prune = val(0)
	case pruneTarget != nil && *pruneTarget > 0:
		rs.Prune = val(*pruneTarget / mib)
	default:
		rs.Prune = val(1) // manual pruning (prune=1)
	}
	if localRelay != nil {
		rs.BlocksOnly = val(0)
		if !*localRelay {
			rs.BlocksOnly = val(1)
		}
	}
	if uploadTarget != nil {
		rs.MaxUploadTarget = val(*uploadTarget / mib)
	}
	var idx map[string]json.RawMessage
	if err := r.call(ctx, "getindexinfo", &idx); err == nil {
		rs.TxIndex = val(0)
		if _, ok := idx["txindex"]; ok {
			rs.TxIndex = val(1)
		}
	}
	var mp struct {
		MaxMempool int64 `json:"maxmempool"`
	}
	if err := r.call(ctx, "getmempoolinfo", &mp); err == nil && mp.MaxMempool > 0 {
		rs.MaxMempool = val(mp.MaxMempool / 1_000_000)
	}
	return rs
}

// networkTypes lists the known networks in a fixed order, plus any other
// network that has peers (e.g. "not_publicly_routable").
func networkTypes(enabled map[string]bool, peers map[string]int) []model.NetworkType {
	out := []model.NetworkType{}
	seen := map[string]bool{}
	for _, name := range networkOrder {
		out = append(out, model.NetworkType{Name: name, Enabled: enabled[name], Peers: peers[name]})
		seen[name] = true
	}
	var others []string
	for name, n := range peers {
		if !seen[name] && n > 0 {
			others = append(others, name)
		}
	}
	sort.Strings(others)
	for _, name := range others {
		out = append(out, model.NetworkType{Name: name, Enabled: true, Peers: peers[name]})
	}
	return out
}

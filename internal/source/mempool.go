package source

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// FeeTargets are the confirmation targets of the fee estimates, in blocks:
// about 20 minutes, an hour and a day (story #11).
var FeeTargets = []int{2, 6, 144}

// feeRefresh is the shortest time between two rounds of fee estimates. They
// are asked again only when a new block arrived, and at most this often.
const feeRefresh = time.Minute

// Reasons shown when a figure is unknown.
const (
	FeeNoData       = "Not enough data yet"
	FeeUnavailable  = "The node didn't answer"
	MempoolNotAllow = "Not allowed for this RPC user (rpcwhitelist)"
)

// satPerVB converts a fee rate in BTC/kvB, as Bitcoin Core reports it, to
// sat/vB, rounded the way the dashboard shows it: one decimal under 10
// sat/vB ("0.1", "2.4"), whole numbers from there ("18").
func satPerVB(btcPerKvB float64) float64 {
	v := btcPerKvB * 100_000
	if v < 10 {
		return math.Round(v*10) / 10
	}
	return math.Round(v)
}

// feeCache holds the last round of fee estimates, so they are asked again
// only when a new block arrives.
type feeCache struct {
	height int64
	at     time.Time
	fees   []model.FeeEstimate
}

// mempool reads getmempoolinfo, and the fee estimates when they are due.
// It never fails: a failing call is reported in the result, so the node
// doesn't become unreachable because of the mempool.
func (r *RPC) mempool(ctx context.Context, height int64) *model.Mempool {
	var mi struct {
		Loaded        bool    `json:"loaded"`
		Size          int64   `json:"size"`
		Bytes         uint64  `json:"bytes"`
		Usage         uint64  `json:"usage"`
		MaxMempool    uint64  `json:"maxmempool"`
		MempoolMinFee float64 `json:"mempoolminfee"`
		MinRelayTxFee float64 `json:"minrelaytxfee"`
	}
	if err := r.call(ctx, "getmempoolinfo", &mi); err != nil {
		return &model.Mempool{Error: mempoolReason(err)}
	}
	return &model.Mempool{
		Loaded:       mi.Loaded,
		Transactions: mi.Size,
		VBytes:       mi.Bytes,
		UsageBytes:   mi.Usage,
		MaxBytes:     mi.MaxMempool,
		MinFee:       satPerVB(mi.MempoolMinFee),
		MinRelayFee:  satPerVB(mi.MinRelayTxFee),
		Fees:         r.fees(ctx, height),
	}
}

// fees returns the fee estimates, from the cache unless a new block arrived
// and the last round is at least feeRefresh old.
func (r *RPC) fees(ctx context.Context, height int64) []model.FeeEstimate {
	r.feeMu.Lock()
	defer r.feeMu.Unlock()
	now := r.now()
	if c := r.feeCache; c != nil && (c.height == height || now.Sub(c.at) < feeRefresh) {
		return c.fees
	}
	fees := make([]model.FeeEstimate, 0, len(FeeTargets))
	for _, target := range FeeTargets {
		fees = append(fees, r.estimate(ctx, target))
	}
	r.feeCache = &feeCache{height: height, at: now, fees: fees}
	return fees
}

func (r *RPC) estimate(ctx context.Context, target int) model.FeeEstimate {
	e := model.FeeEstimate{Blocks: target}
	var res struct {
		FeeRate *float64 `json:"feerate"`
		Errors  []string `json:"errors"`
	}
	if err := r.callParams(ctx, "estimatesmartfee", []any{target, "economical"}, &res); err != nil {
		e.Missing = mempoolReason(err)
		return e
	}
	if res.FeeRate == nil || *res.FeeRate <= 0 {
		// Bitcoin Core answers with "errors" when it has too little data:
		// right after a start, during the initial sync, with blocksonly=1.
		e.Missing = FeeNoData
		return e
	}
	rate := satPerVB(*res.FeeRate)
	e.Rate = &rate
	return e
}

// mempoolReason says in plain language why a mempool call failed. The
// node itself answered the other calls, so a refusal means this method
// isn't allowed (rpcwhitelist), not that the credentials are wrong.
func mempoolReason(err error) string {
	if errors.Is(err, ErrUnauthorized) {
		return MempoolNotAllow
	}
	return FeeUnavailable
}

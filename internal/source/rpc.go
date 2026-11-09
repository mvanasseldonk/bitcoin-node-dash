package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// ErrUnauthorized is returned when Bitcoin Core rejects the RPC credentials.
var ErrUnauthorized = errors.New("bitcoin core rejected the RPC credentials (check the cookie file or RPC user/password)")

// RPCConfig configures the connection to Bitcoin Core.
type RPCConfig struct {
	// URL of the JSON-RPC server, e.g. http://127.0.0.1:8332.
	URL string
	// User and Password, when User is set, are used as is.
	User     string
	Password string
	// ConfPath is Bitcoin Core's bitcoin.conf. When User is empty and the
	// file sets rpcuser and rpcpassword, those are used. Re-read on every call.
	ConfPath string
	// CookiePath is the .cookie file Bitcoin Core writes on start, used when
	// neither of the above gives credentials. It is re-read on every call,
	// because the cookie changes each time bitcoind restarts.
	CookiePath string
	// DataDir is the Bitcoin data directory; its volume is used for disk usage.
	// Optional: when empty or unreadable, disk usage is reported as unknown.
	DataDir string
}

// RPC reads node data from Bitcoin Core over JSON-RPC.
//
// Requires Bitcoin Core 23 or newer (for the "time" field of getblockchaininfo
// and connections_in/out of getnetworkinfo).
type RPC struct {
	cfg    RPCConfig
	client *http.Client
	disk   func(path string) (free, total uint64, err error)
}

// NewRPC returns an RPC source. Timeouts are controlled by the caller's context.
func NewRPC(cfg RPCConfig) *RPC {
	return &RPC{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		disk:   diskUsage,
	}
}

// Fetch implements Source.
func (r *RPC) Fetch(ctx context.Context) (*model.NodeData, error) {
	var bc struct {
		Chain                string  `json:"chain"`
		Blocks               int64   `json:"blocks"`
		Headers              int64   `json:"headers"`
		VerificationProgress float64 `json:"verificationprogress"`
		InitialBlockDownload bool    `json:"initialblockdownload"`
		Time                 int64   `json:"time"`
	}
	if err := r.call(ctx, "getblockchaininfo", &bc); err != nil {
		return nil, err
	}

	var ni struct {
		ConnectionsIn  int `json:"connections_in"`
		ConnectionsOut int `json:"connections_out"`
	}
	if err := r.call(ctx, "getnetworkinfo", &ni); err != nil {
		return nil, err
	}

	d := &model.NodeData{
		Blockchain: model.Blockchain{
			Chain:                bc.Chain,
			Blocks:               bc.Blocks,
			Headers:              bc.Headers,
			VerificationProgress: bc.VerificationProgress,
			InitialBlockDownload: bc.InitialBlockDownload,
		},
		Network: model.Network{Outbound: ni.ConnectionsOut, Inbound: ni.ConnectionsIn},
	}
	if bc.Time > 0 {
		d.Blockchain.LastBlockTime = time.Unix(bc.Time, 0).UTC()
	}
	if r.cfg.DataDir != "" {
		if free, total, err := r.disk(r.cfg.DataDir); err == nil {
			d.Server = model.Server{DiskFreeBytes: free, DiskTotalBytes: total}
		}
	}
	return d, nil
}

// rpcInWarmup is the error code Bitcoin Core returns while it is starting up.
const rpcInWarmup = -28

// StartingError is returned while Bitcoin Core is starting up and doesn't
// answer RPC calls yet. Status is its progress, e.g. "Loading block index…".
type StartingError struct {
	Status string
}

func (e *StartingError) Error() string { return "bitcoin core is starting: " + e.Status }

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (r *RPC) call(ctx context.Context, method string, out any) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "1.0",
		"id":      "node-dash",
		"method":  method,
		"params":  []any{},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	user, pass, err := r.credentials()
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrUnauthorized
	}

	// Bitcoin Core answers RPC errors with HTTP 500 and a JSON body, so decode first.
	var rr struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rr); err != nil {
		return fmt.Errorf("%s: unexpected response (HTTP %d): %w", method, resp.StatusCode, err)
	}
	if rr.Error != nil {
		if rr.Error.Code == rpcInWarmup {
			return &StartingError{Status: rr.Error.Message}
		}
		return fmt.Errorf("%s: %s (code %d)", method, rr.Error.Message, rr.Error.Code)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", method, resp.StatusCode)
	}
	if err := json.Unmarshal(rr.Result, out); err != nil {
		return fmt.Errorf("%s: decode result: %w", method, err)
	}
	return nil
}

// credentials returns the RPC credentials, in order of precedence: the
// configured user, rpcuser/rpcpassword from bitcoin.conf, the cookie file.
// bitcoin.conf comes before the cookie because Bitcoin Core writes no cookie
// when rpcpassword is set, so a cookie next to it is a stale leftover.
// AuthSource describes where the credentials currently come from, for the
// log. It never includes the password.
func (r *RPC) AuthSource() string {
	if r.cfg.User != "" {
		return "user " + r.cfg.User
	}
	if r.cfg.ConfPath != "" {
		if user, _, ok := confCredentials(r.cfg.ConfPath); ok {
			return "user " + user + " (" + r.cfg.ConfPath + ")"
		}
	}
	return "cookie " + r.cfg.CookiePath
}

func (r *RPC) credentials() (user, pass string, err error) {
	if r.cfg.User != "" {
		return r.cfg.User, r.cfg.Password, nil
	}
	if r.cfg.ConfPath != "" {
		if user, pass, ok := confCredentials(r.cfg.ConfPath); ok {
			return user, pass, nil
		}
	}
	b, err := os.ReadFile(r.cfg.CookiePath)
	if err != nil {
		return "", "", fmt.Errorf("read RPC cookie: %w", err)
	}
	user, pass, ok := strings.Cut(strings.TrimSpace(string(b)), ":")
	if !ok {
		return "", "", fmt.Errorf("read RPC cookie: unexpected format in %s", r.cfg.CookiePath)
	}
	return user, pass, nil
}

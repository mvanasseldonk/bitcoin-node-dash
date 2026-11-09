# bitcoin-node-dash

A minimal, calm dashboard for your Bitcoin Core node. One look tells you whether
your node is healthy; it stays up when your node doesn't, and it barely uses any
resources.

> **Status:** early development. Installation via a ready-made Docker image and
> release binaries will follow. Requires Bitcoin Core 23 or newer.

## Development

Requirements: Go 1.25+ and Node.js 26 (see `.nvmrc`; run `nvm use`).

```bash
make run                 # build everything and run on mock data at http://localhost:8080
make run MOCK=warning    # other scenarios: healthy, syncing, warning, error, unreachable
make run MOCK=           # connect to Bitcoin Core instead (see Configuration)
make test                # Go tests + frontend type check
make lint                # gofmt + go vet
```

For frontend work with hot reload, run the Go server and the Vite dev server side by side:

```bash
NODEDASH_MOCK=healthy go run ./cmd/node-dash   # API on :8080
cd web && npm run dev                          # UI on :5173, proxies /api to :8080
```

### Configuration

| Variable               | Default                  | Description                                                                            |
| ---------------------- | ------------------------ | -------------------------------------------------------------------------------------- |
| `NODEDASH_LISTEN`      | `:8080`                  | Address the dashboard listens on                                                       |
| `NODEDASH_MOCK`        | _(empty)_                | Run on mock data: `healthy`, `syncing`, `warning`, `error` or `unreachable`            |
| `BITCOIN_RPC_URL`      | `http://127.0.0.1:8332`  | Bitcoin Core JSON-RPC address                                                          |
| `BITCOIN_DATADIR`      | `~/.bitcoin`             | Bitcoin data directory; used for the cookie file and to measure free disk space        |
| `BITCOIN_RPC_COOKIE`   | `$BITCOIN_DATADIR/.cookie` | Cookie file for RPC authentication (re-read on every call, so restarts are handled)  |
| `BITCOIN_RPC_USER`     | _(empty)_                | RPC user; when set, used instead of the cookie file                                    |
| `BITCOIN_RPC_PASSWORD` | _(empty)_                | RPC password for `BITCOIN_RPC_USER`                                                    |

The dashboard only uses the read-only RPC calls `getblockchaininfo` and `getnetworkinfo`.

### Status rules

The circle shows the most severe of these rules, with the reasons listed underneath:

| Status          | When                                                                                   |
| --------------- | -------------------------------------------------------------------------------------- |
| **Problem**     | No outgoing connections · disk with blockchain data more than 95% full                |
| **Attention**   | 1–3 outgoing connections · initial sync · more than 2 blocks behind the known headers · no new block for more than 60 minutes · disk more than 90% full |
| **Unreachable** | The dashboard can't reach Bitcoin Core (last known values stay visible, dimmed)        |
| **Healthy**     | None of the above                                                                      |

Incoming connections never affect the status: they need an open port and are optional.
The thresholds live in [`internal/health/health.go`](internal/health/health.go).

### How it works

- `cmd/node-dash` is the single binary; the built frontend (`web/dist`) is embedded into it.
- `internal/poller` fetches node data every 10 seconds with a 5 second timeout and caches it,
  so every open browser tab reads the same snapshot (`GET /api/status`) instead of hitting the node.
- When the node can't be reached, the last known data is kept and the status becomes *Unreachable*.
- `GET /healthz` reports whether the dashboard itself is running.

## License

MIT

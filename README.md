# bitcoin-node-dash

A minimal, calm dashboard for your Bitcoin Core node. One look tells you whether
your node is healthy; it stays up when your node doesn't, and it barely uses any
resources.

> **Status:** early development. The dashboard currently runs on mock data;
> connecting to Bitcoin Core over RPC is next. Installation via a ready-made
> Docker image and release binaries will follow.

## Development

Requirements: Go 1.25+ and Node.js 22+.

```bash
make run                 # build everything and run on mock data at http://localhost:8080
make run MOCK=warning    # other scenarios: healthy, warning, error, unreachable
make test                # Go tests + frontend type check
make lint                # gofmt + go vet
```

For frontend work with hot reload, run the Go server and the Vite dev server side by side:

```bash
NODEDASH_MOCK=healthy go run ./cmd/node-dash   # API on :8080
cd web && npm run dev                          # UI on :5173, proxies /api to :8080
```

### Configuration

| Variable          | Default   | Description                                                            |
| ----------------- | --------- | ---------------------------------------------------------------------- |
| `NODEDASH_LISTEN` | `:8080`   | Address the dashboard listens on                                       |
| `NODEDASH_MOCK`   | `healthy` | Mock scenario: `healthy`, `warning`, `error` or `unreachable`          |

### How it works

- `cmd/node-dash` is the single binary; the built frontend (`web/dist`) is embedded into it.
- `internal/poller` fetches node data every 10 seconds with a 5 second timeout and caches it,
  so every open browser tab reads the same snapshot (`GET /api/status`) instead of hitting the node.
- When the node can't be reached, the last known data is kept and the status becomes *Unreachable*.
- `GET /healthz` reports whether the dashboard itself is running.

## License

MIT

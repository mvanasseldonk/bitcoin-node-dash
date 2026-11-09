# bitcoin-node-dash

A minimal, calm dashboard for your Bitcoin Core node. One look tells you whether
your node is healthy; it stays up when your node doesn't, and it barely uses any
resources.

- One status circle: **Healthy**, **Attention**, **Problem** or **Unreachable**, with the reasons in plain language
- Three tiles: is the node in sync, how is it connected, how much disk space is left
- Read-only: it only uses the RPC calls `getblockchaininfo` and `getnetworkinfo`
- Runs on your LAN only; no accounts, no tracking, no requests to the internet

Requires Bitcoin Core 23 or newer.

## Install

### Docker (recommended)

Run this on the machine where Bitcoin Core runs:

```bash
docker run -d --name node-dash --restart unless-stopped \
  --network host \
  --user "$(stat -c '%u:%g' ~/.bitcoin)" \
  -v ~/.bitcoin:/bitcoin:ro \
  ghcr.io/mvanasseldonk/bitcoin-node-dash:latest
```

Then open `http://<your-server>:8080` in a browser on your network.

- `--network host` lets the dashboard reach Bitcoin Core's RPC on `127.0.0.1:8332`, without opening RPC to other machines.
- `--user` runs the dashboard as the owner of your Bitcoin data directory, so it can read the RPC cookie file (or `bitcoin.conf`).
- `-v ~/.bitcoin:/bitcoin:ro` mounts your data directory read-only. Use your own path if your data lives elsewhere.

Prefer Docker Compose? Download the example, check the `user` and volume path, and start it:

```bash
curl -fsSLO https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/docker-compose.yml
docker compose up -d
```

**Update** to the latest version:

```bash
docker compose pull && docker compose up -d
```

(or `docker pull ghcr.io/mvanasseldonk/bitcoin-node-dash:latest` and re-create the container.)

Images are published for `linux/amd64` and `linux/arm64` (e.g. Raspberry Pi 4/5).
Tags: `latest` (newest release), `0.1.0` / `0.1` (a specific release) and `edge` (the main branch).

#### Bitcoin Core in Docker

If Bitcoin Core itself runs in a container, put the dashboard on the same Docker network instead
of using host networking, point it at the Bitcoin container, and mount the same data volume:

```yaml
services:
  node-dash:
    image: ghcr.io/mvanasseldonk/bitcoin-node-dash:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    user: "1000:1000"          # the user that owns the Bitcoin data
    volumes:
      - bitcoin-data:/bitcoin:ro
    environment:
      BITCOIN_RPC_URL: http://bitcoind:8332
```

Bitcoin Core must accept RPC from the dashboard container (`rpcbind` / `rpcallowip`).

### Binary (without Docker)

Download the archive for your platform from the [latest release](https://github.com/mvanasseldonk/bitcoin-node-dash/releases/latest),
for example `node-dash_v0.1.0_linux_amd64.tar.gz`, and run it as the user that runs Bitcoin Core:

```bash
tar -xzf node-dash_*_linux_amd64.tar.gz
cd node-dash_*_linux_amd64
./node-dash
```

To run it as a service, install the binary and the included `node-dash.service` systemd unit
(adjust `User`, `Group` and `BITCOIN_DATADIR` first):

```bash
sudo install -m 0755 node-dash /usr/local/bin/node-dash
sudo cp node-dash.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now node-dash
```

Check the archive against `checksums.txt` from the release with `sha256sum -c --ignore-missing checksums.txt`.

## Configuration

All settings are environment variables. The defaults work for a local Bitcoin Core, both with cookie
authentication and with `rpcuser`/`rpcpassword` in `bitcoin.conf`.

The dashboard looks for RPC credentials in this order:

1. `BITCOIN_RPC_USER` and `BITCOIN_RPC_PASSWORD`, when set;
2. `rpcuser` and `rpcpassword` in `bitcoin.conf` (the top of the file or its `[main]` section);
3. the cookie file Bitcoin Core writes when no `rpcpassword` is set.

`bitcoin.conf` and the cookie are re-read on every call, so changes and restarts of Bitcoin Core are picked up.
The password is never logged.

| Variable               | Default                    | Description                                                                         |
| ---------------------- | -------------------------- | ----------------------------------------------------------------------------------- |
| `NODEDASH_LISTEN`      | `:8080`                    | Address the dashboard listens on                                                    |
| `BITCOIN_RPC_URL`      | `http://127.0.0.1:8332`    | Bitcoin Core JSON-RPC address                                                       |
| `BITCOIN_DATADIR`      | `~/.bitcoin` (`/bitcoin` in Docker) | Bitcoin data directory; used for `bitcoin.conf`, the cookie file and to measure free disk space |
| `BITCOIN_CONF`         | `$BITCOIN_DATADIR/bitcoin.conf` | Bitcoin Core config file to read `rpcuser`/`rpcpassword` from                 |
| `BITCOIN_RPC_COOKIE`   | `$BITCOIN_DATADIR/.cookie` | Cookie file for RPC authentication                                                  |
| `BITCOIN_RPC_USER`     | _(empty)_                  | RPC user; when set, used instead of `bitcoin.conf` and the cookie file              |
| `BITCOIN_RPC_PASSWORD` | _(empty)_                  | RPC password for `BITCOIN_RPC_USER`                                                 |
| `NODEDASH_MOCK`        | _(empty)_                  | Run on mock data: `healthy`, `syncing`, `warning`, `error`, `unreachable` or `stale` |

## Troubleshooting

The dashboard shows **Unreachable** and its log (`docker logs node-dash`) tells you why:

- **`read RPC cookie: permission denied`**: the dashboard runs as a user that can't read `.cookie`.
  Run it as the owner of the data directory (`--user` / `user:`), or, with Bitcoin Core 28+,
  add `rpccookieperms=group` to `bitcoin.conf` and run the dashboard in that group.
- **`read RPC cookie: no such file`**: wrong data directory, or a test network. For testnet/signet
  the cookie lives in a subdirectory: set `BITCOIN_RPC_COOKIE`, e.g. `/bitcoin/signet/.cookie`.
- **`connection refused`**: Bitcoin Core isn't running, or listens elsewhere. Check `BITCOIN_RPC_URL`;
  in Docker without host networking, `127.0.0.1` is the container itself.
- **`rejected the RPC credentials`**: check where the credentials come from: the `auth=` field in the
  startup log says it. Wrong `BITCOIN_RPC_USER` / `BITCOIN_RPC_PASSWORD`; or a stale `.cookie` left
  behind after you set `rpcpassword` (delete it, or make sure `bitcoin.conf` is readable for the dashboard);
  or Bitcoin Core uses a `bitcoin.conf` elsewhere (set `BITCOIN_CONF`).

Run `docker exec node-dash /usr/local/bin/node-dash version` to see which version you're running.

## Status rules

The circle shows the most severe of these rules, with the reasons listed underneath:

| Status          | When                                                                                   |
| --------------- | -------------------------------------------------------------------------------------- |
| **Problem**     | No outgoing connections · less than 10 GB free on the disk with blockchain data        |
| **Attention**   | 1–3 outgoing connections · initial sync · more than 2 blocks behind the known headers · no new block for more than 60 minutes · less than 50 GB free on the disk |
| **Unreachable** | The dashboard can't reach Bitcoin Core (last known values stay visible, dimmed)        |
| **Healthy**     | None of the above                                                                      |

Incoming connections never affect the status: they need an open port and are optional.
Disk limits are in GB because the blockchain grows by a fixed amount (roughly 60–90 GB a year), whatever
the size of your disk: 50 GB free lasts months, 10 GB weeks. On small disks, such as pruned nodes, the
limits are 10% and 5% of the disk instead, if that is less.
The thresholds live in [`internal/health/health.go`](internal/health/health.go).

## Development

Requirements: Go 1.26+ and Node.js 26 (see `.nvmrc`; run `nvm use`).

```bash
make run                 # build everything and run on mock data at http://localhost:8080
make run MOCK=warning    # other scenarios: healthy, syncing, warning, error, unreachable, stale
make run MOCK=           # connect to Bitcoin Core instead (see Configuration)
make test                # Go tests + frontend type check
make lint                # gofmt + go vet
```

For frontend work with hot reload, run the Go server and the Vite dev server side by side:

```bash
NODEDASH_MOCK=healthy go run ./cmd/node-dash   # API on :8080
cd web && npm run dev                          # UI on :5173, proxies /api to :8080
```

### How it works

- `cmd/node-dash` is the single binary; the built frontend (`web/dist`) is embedded into it.
- `internal/poller` fetches node data every 10 seconds with a 5 second timeout and caches it,
  so every open browser tab reads the same snapshot (`GET /api/status`) instead of hitting the node.
- When the node can't be reached, the last known data is kept and the status becomes *Unreachable*.
- The browser refreshes every 10 seconds, and right away when you return to the tab. Data older than
  30 seconds is dimmed and marked as last known values. Ages are measured on the server's clock
  (`serverTime` in `/api/status`), so a browser with a wrong clock still shows them correctly.
- `GET /healthz` reports whether the dashboard itself is running; `node-dash healthcheck` calls it
  (used by the Docker `HEALTHCHECK`).

### Resource usage

The dashboard is built to stay out of your node's way:

- It polls Bitcoin Core once every 10 seconds with two lightweight calls (`getblockchaininfo` and
  `getnetworkinfo`), however many browser tabs are open. Tests guard both.
- Measured idle: about **11 MB** memory and **0.0% CPU** (0.01 s of CPU time per minute); about 14 MB after
  300 simultaneous requests. The Docker image is about 10 MB.
- CI fails if the container uses 100 MB or more. The systemd unit and the Compose example cap memory at 128 MB.

On your own node, the service's memory and average CPU use since it started:

```bash
pid=$(systemctl show -p MainPID --value node-dash); echo "Memory: $(( $(ps -o rss= -p $pid) / 1024 )) MB"; ps -o etimes=,cputimes= -p $pid | awk '{printf "CPU: %.3f%% on average over %d minutes\n", 100*$2/$1, $1/60}'
```

## License

MIT

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

### Without Docker (Linux with systemd)

Run this on the machine where Bitcoin Core runs:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh
```

The script:

- finds the user that runs `bitcoind` and its data directory, and shows what it found;
- downloads the latest release for your CPU (x86-64 or ARM64) and checks it against `checksums.txt`;
- installs `/usr/local/bin/node-dash` and a systemd service that starts with your server;
- prints the dashboard's address, and the command to open the port if the ufw firewall is active.

It uses `sudo` for the install steps. Settings, put before `sh`:
`VERSION=v0.2.0` for a specific release, `NODEDASH_USER=bitcoin` and `BITCOIN_DATADIR=/path/to/.bitcoin`
when the script can't find `bitcoind`.

**Update:** run the same command again. It does nothing when you are up to date. If you changed
`/etc/systemd/system/node-dash.service` (or installed it by hand), it keeps your version.

**Uninstall** (your Bitcoin data is never touched):

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --uninstall
```

Prefer to read a script before running it?

```bash
curl -fsSLO https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh
less install.sh
sh install.sh
```

#### By hand

Download the archive for your platform from the [latest release](https://github.com/mvanasseldonk/bitcoin-node-dash/releases/latest),
for example `node-dash_v0.2.0_linux_amd64.tar.gz`, check it with `sha256sum -c --ignore-missing checksums.txt`,
and run it as the user that runs Bitcoin Core:

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
| `NODEDASH_NAME`        | `Node`                     | Your name for the node, at most 40 characters (see [Name and icon](#name-and-icon)) |
| `BITCOIN_RPC_URL`      | `http://127.0.0.1:8332`    | Bitcoin Core JSON-RPC address                                                       |
| `BITCOIN_DATADIR`      | `~/.bitcoin` (`/bitcoin` in Docker) | Bitcoin data directory; used for `bitcoin.conf`, the cookie file and to measure free disk space |
| `BITCOIN_CONF`         | `$BITCOIN_DATADIR/bitcoin.conf` | Bitcoin Core config file to read `rpcuser`/`rpcpassword` from                 |
| `BITCOIN_RPC_COOKIE`   | `$BITCOIN_DATADIR/.cookie` | Cookie file for RPC authentication                                                  |
| `BITCOIN_RPC_USER`     | _(empty)_                  | RPC user; when set, used instead of `bitcoin.conf` and the cookie file              |
| `BITCOIN_RPC_PASSWORD` | _(empty)_                  | RPC password for `BITCOIN_RPC_USER`                                                 |
| `NODEDASH_MOCK`        | _(empty)_                  | Run on mock data: `healthy`, `syncing`, `warning`, `error`, `unreachable`, `starting`, `stale` or `outdated` |
| `NODEDASH_UPDATE_CHECK` | _(empty)_                 | `0` turns off the daily lookup of the latest Bitcoin Core release (see [Upgrade advice](#upgrade-advice)) |
| `NODEDASH_CONTROL`     | _(empty)_                  | `1` turns on [node control](#node-control): stop, start and restart Bitcoin Core    |
| `BITCOIN_SERVICE`      | `bitcoind.service`         | The systemd service of Bitcoin Core, for node control                               |
| `NODEDASH_HOSTS`       | _(empty)_                  | Extra host names, comma separated, on which node control may be used (see below)    |

## Node control

The dashboard can stop, start and restart Bitcoin Core: the power button at the top right. It's **off by default**,
because the dashboard has no login.

> **Anyone who can open the dashboard can stop your node.** Only turn this on when the dashboard is reachable
> from your own home network, never from the internet.

It needs Bitcoin Core as a systemd service and polkit (installed on Ubuntu Server; on Debian:
`sudo apt install polkitd`). Turn it on with the installer:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | NODEDASH_CONTROL=1 sh
```

`NODEDASH_CONTROL=0` turns it off again. Use `BITCOIN_SERVICE=…` when your service has another name.

What the installer does:

- **A polkit rule** in `/etc/polkit-1/rules.d/50-node-dash.rules` lets the dashboard's user start, stop and restart
  that one service, and nothing else.
- **A drop-in** in `/etc/systemd/system/node-dash.service.d/control.conf` sets `NODEDASH_CONTROL=1`. It also works
  when you wrote `node-dash.service` yourself.
- **A check** with `pkcheck` that polkit really allows it.

How it works and why: [decision 0001](docs/decisions/0001-controlling-bitcoind.md).

- **A clean shutdown:** stop and restart go through systemd, which sends Bitcoin Core SIGTERM. Never RPC `stop`:
  with `Restart=always`, systemd would start it again.
- **Stop timeout:** systemd force-stops a service after 90 seconds by default. Bitcoin Core may need longer to
  write its cache to disk, and a forced stop can corrupt the chainstate. The dashboard and the installer warn when
  the timeout is under 5 minutes. To allow 10 minutes:

  ```bash
  sudo systemctl edit bitcoind.service
  ```

  and add:

  ```ini
  [Service]
  TimeoutStopSec=600
  ```

- **Protection against web pages:** a malicious site in a browser on your network can't use the controls. The
  dashboard only accepts JSON requests from its own page, on an IP address or `localhost`. When you open the
  dashboard by a host name, such as `http://bitcoin-node.lan:8080`, add that name to `NODEDASH_HOSTS`
  (`sudo systemctl edit node-dash`, `Environment=NODEDASH_HOSTS=bitcoin-node.lan`).
- **Not supported yet:** Bitcoin Core in Docker, and systems without polkit.

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

## Details page

The **Details** link on each tile opens a page with the numbers behind it:

- **Blockchain:** block and header height, verification progress, the node software, bitcoind uptime, and the size of
  the blockchain data.
- **Network:** peers per network (IPv4, IPv6, Tor, I2P, CJDNS), data sent and received, and the current upload and
  download rate.
- **Server:** CPU usage, memory, the disk with the blockchain data, CPU temperature and uptime. They come from Linux
  (`/proc` and `/sys`), so they also show while Bitcoin Core is down.
  - **In Docker:** the figures are the host's, because a container shares its kernel. The CPU temperature is often
    missing in Docker.
  - **In a virtual machine:** often no CPU temperature either.
  - **On macOS:** only the disk.

  A figure that isn't available shows "—", with the reason.

These extra numbers are only fetched while the page is open, at most once every 4 seconds, however many people look.
The page never shows addresses: not your node's, and not those of its peers.

## Name and icon

The browser tab shows your node's status: the favicon takes the colour of the status circle, and the title reads
e.g. "Attention · Node". In a background tab you can see that the node needs attention.

- **Your own name:** set `NODEDASH_NAME`, e.g. `NODEDASH_NAME="Satoshi's node"`. It shows in the header, the tab title
  and under the icon on your phone's home screen.
  - **Without Docker:** `sudo systemctl edit node-dash`, `Environment="NODEDASH_NAME=Satoshi's node"`.
  - **With Docker:** under `environment:` in `docker-compose.yml`.

  A name longer than 40 characters is refused at startup.
- **On your phone:** "Add to Home Screen" gives the dashboard its own icon and name. The icon stays the same: a phone
  doesn't let a web app change it, so the status shows in the app.

## Status rules

The circle shows the most severe of these rules, with the reasons listed underneath:

| Status          | When                                                                                   |
| --------------- | -------------------------------------------------------------------------------------- |
| **Problem**     | No outgoing connections · less than 10 GB free on the disk with blockchain data        |
| **Attention**   | 1–3 outgoing connections · initial sync · more than 2 blocks behind the known headers · no new block for more than 60 minutes · less than 50 GB free on the disk · a Bitcoin Core version that no longer gets security fixes |
| **Starting**    | Bitcoin Core is starting up (loading the block index, verifying blocks, …); its own progress message is shown |
| **Unreachable** | The dashboard can't reach Bitcoin Core (last known values stay visible, dimmed)        |
| **Healthy**     | None of the above                                                                      |

Incoming connections never affect the status: they need an open port and are optional.
Disk limits are in GB because the blockchain grows by a fixed amount (roughly 60–90 GB a year), whatever
the size of your disk: 50 GB free lasts months, 10 GB weeks. On small disks, such as pruned nodes, the
limits are 10% and 5% of the disk instead, if that is less.
The thresholds live in [`internal/health/health.go`](internal/health/health.go).

### Upgrade advice

The Blockchain tile shows which node software runs, e.g. "Bitcoin Core 32.1". For Bitcoin Core, the dashboard
also says when to upgrade:

- **A newer release exists:** the tile says so, e.g. "Bitcoin Core 32.0 · 32.1 available". The status doesn't
  change.
- **Your version no longer gets security fixes:** the status becomes **Attention**. Bitcoin Core maintains the
  latest three major versions ([lifecycle](https://bitcoincore.org/en/lifecycle/)): with 32 as the latest, 30, 31
  and 32 get fixes, and 29 and older don't.

To know the latest release, the dashboard server asks GitHub once a day
(`api.github.com/repos/bitcoin/bitcoin/releases/latest`), and an hour later when that fails.

- **What it reveals:** GitHub sees your IP address asking for the latest Bitcoin Core release. Nothing about your
  node is sent.
- **Without internet:** you see the version without advice.
- **Turning it off:** set `NODEDASH_UPDATE_CHECK=0` (`sudo systemctl edit node-dash`,
  `Environment=NODEDASH_UPDATE_CHECK=0`).

Bitcoin Knots and other node software are shown by name, without upgrade advice.

## Development

Requirements: Go 1.26+ and Node.js 26 (see `.nvmrc`; run `nvm use`).

```bash
make run                 # build everything and run on mock data at http://localhost:8080
make run MOCK=warning    # other scenarios: healthy, syncing, warning, error, unreachable, starting, stale, outdated
make run CONTROL=1       # with node control on a simulated systemd
make run MOCK=           # connect to Bitcoin Core instead (see Configuration)
make test                # Go tests + frontend type check
make lint                # gofmt + go vet
go run ./tools/icons     # redraw the favicons and home screen icons in web/public
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
  Attempts then slow down (10, 20, then every 30 seconds), and return to every 10 seconds once the node answers.
  While Bitcoin Core starts up the status is *Starting*, with its progress message, and polling stays at 10 seconds.
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

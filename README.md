# bitcoin-node-dash

A minimal, calm dashboard for your Bitcoin Core node. One look tells you whether
your node is healthy; it stays up when your node doesn't, and it barely uses any
resources.

- One status circle: **Healthy**, **Attention**, **Problem** or **Unreachable**, with the reasons in plain language
- Four tiles: is the node in sync, how is it connected, how much disk space is left, and which fee gets a
  transaction in within about 20 minutes
- Read-only: it only uses read-only RPC calls, such as `getblockchaininfo`, `getnetworkinfo` and `getmempoolinfo`
- Runs on your LAN only; no accounts, no tracking, no requests to the internet

Requires Bitcoin Core 23 or newer.

## Install

**Use the installer** when Bitcoin Core runs on Linux with systemd, as on most servers and VMs. It is one command,
asks a few questions, and supports every option. [Docker](#docker) works too, with fewer options.

**Using ChatGPT, Claude or another assistant?** Give it this link, and it can guide you step by step:
[docs/install-with-ai.md](https://github.com/mvanasseldonk/bitcoin-node-dash/blob/main/docs/install-with-ai.md).

### With the installer (recommended)

Run this on the machine where Bitcoin Core runs, as a user that can use `sudo`:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh
```

The first time, in a terminal, it asks for a name for your node and whether to turn on three options:
[node control](#node-control), [changing settings](#changing-settings) and [automatic updates](#automatic-updates).
Enter keeps the default, which is "no" for all three. Then it:

- finds the user that runs `bitcoind` and its data directory, and shows what it found;
- downloads the latest release for your CPU (x86-64 or ARM64), checks it against `checksums.txt` and checks the
  signature of `checksums.txt` (see [Signed releases](#signed-releases));
- installs `/usr/local/bin/node-dash` and a systemd service that starts with your server;
- prints the dashboard's address, the command to open the port if the ufw firewall is active, and which options
  are on.

Then open that address, e.g. `http://192.168.1.20:8080`, in a browser on your network.

**Options** go after `sh -s --`. With options, it asks nothing:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --name "Satoshi's node" --control
```

| Option | What it does |
|---|---|
| `--name "My node"` | Your name for the node, at most 40 characters. `--name ""` goes back to "Node" |
| `--control`, `--no-control` | [Node control](#node-control) on or off |
| `--settings`, `--no-settings` | [Changing settings](#changing-settings) on or off |
| `--auto-update`, `--no-auto-update` | [Automatic updates](#automatic-updates) on or off |
| `--yes` | Ask nothing; what isn't given stays as it is |
| `--check` | Change nothing; print a report to share when asking for help (see below) |
| `--uninstall` | Remove node-dash |
| `--help` | All options |

Run the installer again with an option to change it later; the others stay as they are. Every option also works as
an environment variable **before `sh`**, e.g. `| NODEDASH_CONTROL=1 sh` (not before `curl`: then `sh` doesn't get
it). For special cases, also before `sh`: `VERSION=v0.2.0` for a specific release, and `NODEDASH_USER=bitcoin` and
`BITCOIN_DATADIR=/path/to/.bitcoin` when the installer can't find `bitcoind`.

**Something not working?** `--check` changes nothing and prints what the installer and a helper need to know: your
system, where Bitcoin Core runs and whether its RPC answers, whether the dashboard runs, the options, and the
firewall. It never shows passwords, keys or lines of `bitcoin.conf`, so you can paste it in an issue or a chat:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --check
```

**Update:** run the same command again. It does nothing when you are up to date. If you changed
`/etc/systemd/system/node-dash.service` (or installed it by hand), it keeps your version. Or let it update itself:
see [Automatic updates](#automatic-updates).

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

### By hand

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

### Docker

For when you already run your services in Docker. The dashboard itself works the same, but some options don't
work in a container:

| | Installer | Docker |
|---|---|---|
| Status, tiles, details, peers, mempool | ✓ | ✓ |
| [Node control](#node-control): stop, start and restart Bitcoin Core | ✓ | – |
| [Changing settings](#changing-settings) of `bitcoin.conf` | ✓ | – (the data directory is mounted read-only) |
| [Automatic updates](#automatic-updates) | ✓ | – (`docker compose pull`) |
| Questions on the first install, `--check` | ✓ | – |
| Your [own name](#name-and-icon) | `--name` | `NODEDASH_NAME` in `docker-compose.yml` |

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
| `BITCOIN_CONF`         | `$BITCOIN_DATADIR/bitcoin.conf` | Bitcoin Core config file to read `rpcuser`/`rpcpassword` and the [node settings](#node-settings) from |
| `BITCOIN_RPC_COOKIE`   | `$BITCOIN_DATADIR/.cookie` | Cookie file for RPC authentication                                                  |
| `BITCOIN_RPC_USER`     | _(empty)_                  | RPC user; when set, used instead of `bitcoin.conf` and the cookie file              |
| `BITCOIN_RPC_PASSWORD` | _(empty)_                  | RPC password for `BITCOIN_RPC_USER`                                                 |
| `NODEDASH_MOCK`        | _(empty)_                  | Run on mock data: `healthy`, `syncing`, `warning`, `error`, `unreachable`, `starting`, `stale`, `outdated`, `live` (a new block every 30 s), `busy` (a full mempool and high fees) or `restarted` (the mempool is still loading). With `NODEDASH_SETTINGS=1`, the settings form edits a demo `bitcoin.conf` in a temporary directory |
| `NODEDASH_UPDATE_CHECK` | _(empty)_                 | `0` turns off the daily lookup of the latest Bitcoin Core and node-dash releases (see [Upgrade advice](#upgrade-advice) and [New dashboard versions](#new-dashboard-versions)) |
| `NODEDASH_CONTROL`     | _(empty)_                  | `1` turns on [node control](#node-control): stop, start and restart Bitcoin Core    |
| `BITCOIN_SERVICE`      | `bitcoind.service`         | The systemd service of Bitcoin Core, for node control                               |
| `NODEDASH_HOSTS`       | _(empty)_                  | Extra host names, comma separated, on which node control may be used (see below)    |
| `NODEDASH_ANCORO`      | _(empty)_                  | `0` hides the link to Ancoro under the tiles (see [Checking a transaction](#checking-a-transaction)) |
| `NODEDASH_PEER_ADDRESSES` | _(empty)_               | `1` shows the addresses of your peers on the Peers page (see [Peers page](#peers-page)) |
| `NODEDASH_SETTINGS`    | _(empty)_                  | `1` turns on [changing settings](#changing-settings); set by the installer        |

`NODEDASH_AUTO_UPDATE` is a setting of the installer, not of the dashboard: see [Automatic updates](#automatic-updates).

## Node control

The dashboard can stop, start and restart Bitcoin Core: the power button at the top right. It's **off by default**,
because the dashboard has no login.

> **Anyone who can open the dashboard can stop your node.** Only turn this on when the dashboard is reachable
> from your own home network, never from the internet.

It needs Bitcoin Core as a systemd service and polkit (installed on Ubuntu Server; on Debian:
`sudo apt install polkitd`). Turn it on with the installer:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --control
```

`--no-control` turns it off again. Use `BITCOIN_SERVICE=…` before `sh` when your service has another name.

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

Installed with the installer? Start with `--check` (see [Install](#install)); it shows most problems at a glance.

The dashboard shows **Unreachable** and its log (`journalctl -u node-dash`, or `docker logs node-dash`) tells you
why:

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
- **Server:** CPU usage, memory, the disk with the blockchain data, CPU temperature and uptime, and the dashboard's
  own version (see [New dashboard versions](#new-dashboard-versions)). The figures come from Linux
  (`/proc` and `/sys`), so they also show while Bitcoin Core is down.
  - **In Docker:** the figures are the host's, because a container shares its kernel. The CPU temperature is often
    missing in Docker.
  - **In a virtual machine:** often no CPU temperature either.
  - **On macOS:** only the disk.

  A figure that isn't available shows "—", with the reason.
- **Mempool:** see [Mempool and fees](#mempool-and-fees).
- **Node settings:** see [Node settings](#node-settings).

These extra numbers are only fetched while the page is open, at most once every 4 seconds, however many people look.
The page never shows addresses: not your node's, and not those of its peers.

## Peers page

**See all** next to the number of peers on the details page opens a list of every connected peer:

- **Per peer:** the network (IPv4, IPv6, Tor, I2P, CJDNS), incoming or outgoing, the connection type ("Full relay",
  "Blocks only", "Manual", "Feeler", "Address fetch"), the client ("Bitcoin Core 30.0"), the ping, how long it has
  been connected, the data sent and received, and whether the connection is encrypted (BIP324).
- **Sorting:** click a column header; click it again to turn the order around. At phone width, choose with
  **Sort by**. By default, the peers connected longest come first, so the list stays put.
- **Load:** everything comes from your node's `getpeerinfo`, only while the page is open, at most once every
  4 seconds, however many people look.
- **No status colours:** a slow peer shows by sorting on ping. Bitcoin Core doesn't say which peers misbehave.

**Addresses are off by default.** The dashboard has no login, so anyone who can open it would see the IP and Tor
addresses of your peers; for incoming peers, those are other people's. Set `NODEDASH_PEER_ADDRESSES=1` to show them,
as a second line under each peer. Without it, they aren't in the API either. Your node's own address is never shown.

## Mempool and fees

The **Mempool** tile shows the fee for a transaction to be confirmed within about 20 minutes (2 blocks), in sat/vB,
with the number of transactions waiting and their size. Its details section adds the memory the mempool uses (of
`maxmempool`), the minimum mempool and relay fees, and the estimates for ~20 minutes, ~1 hour (6 blocks) and ~1 day
(144 blocks).

- **Your own node:** everything comes from your node's `getmempoolinfo` and `estimatesmartfee` (economical mode).
  No fee website is asked.
- **Rounding:** fee rates under 10 sat/vB get one decimal ("0.1", "2.4"); higher ones are whole numbers.
- **Missing estimates:** right after a start, during the initial sync, or with `blocksonly=1`, Bitcoin Core has no
  estimates yet; they show "—" with "Not enough data yet". While the mempool loads after a start, it says
  "Loading mempool…".
- **Status:** the mempool never changes the status circle: a busy network says nothing about your node. If your
  `rpcwhitelist` doesn't allow these calls, only the mempool shows "—"; the node stays reachable.
- **Load:** `getmempoolinfo` runs with each poll. The fee estimates are asked again only after a new block, and at
  most once a minute.

## Node settings

The **Node settings** section on the details page shows eight common settings of Bitcoin Core: the database cache
(`dbcache`), connections (`maxconnections`), mempool size (`maxmempool`), pruning (`prune`), the transaction index
(`txindex`), incoming peers (`listen`), blocks only (`blocksonly`) and the upload limit (`maxuploadtarget`).

- **The value:** what the node runs with, with its unit ("2,048 MiB", "300 MB", "Off").
- **Where it comes from**, under the value:
  - "bitcoin.conf", "bitcoin.conf · [main]" (a network section) or the name of an `includeconf` file;
  - "Command line": a flag of bitcoind itself, e.g. `-prune=550` in `ExecStart=` of `bitcoind.service`, read from
    `/proc`. In Docker, the dashboard can't see the bitcoind process, so this source doesn't show there;
  - "settings.json": Bitcoin Core's own settings file in the data directory, which wins over `bitcoin.conf`. bitcoind
    usually only keeps wallet names there; bitcoin-qt also stores settings such as `dbcache` there;
  - "Default": not set anywhere, so Bitcoin Core's default;
  - "Running": the node reports it, but it isn't set where the dashboard can see it.
- **Restart needed:** for pruning, the transaction index, the mempool size, the upload limit and blocks only, the node
  also reports the value it runs with. If `bitcoin.conf` says something else, the section shows both, e.g.
  "bitcoin.conf says 500 MB · restart to apply". This is information only: it never changes the status circle.
- **The file:** the section says which `bitcoin.conf` it read (`BITCOIN_CONF`, or `bitcoin.conf` in the data
  directory; a `-conf=` or `-datadir=` on bitcoind's command line wins). If the file is missing or can't be read, it
  says why, and shows the defaults and what the node reports.
- **Nothing else leaves the server:** only these eight settings are in the API, never `rpcpassword`, `rpcauth` or
  any other line of the file. Like the other details, they are only read while the page is open.
- **Read-only by default.** To change them from the dashboard, see below.

### Changing settings

**Change settings** in the Node settings section opens a form for the same eight settings. It's **off by default**,
because the dashboard has no login.

> **Anyone who can open the dashboard can change these settings**, including turning on pruning, which deletes old
> blocks. Only turn this on when the dashboard is reachable from your own home network, never from the internet.

Turn it on with the installer:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --settings
```

`--no-settings` turns it off again. Without it, there is no form, and the API refuses changes.

What the installer does:

- **A drop-in** in `/etc/systemd/system/node-dash.service.d/settings.conf`:
  - it sets `NODEDASH_SETTINGS=1`;
  - it gives the dashboard write access to `bitcoin.conf`, that one file, with `ReadWritePaths=`. Never to the data
    directory;
  - it creates `/var/lib/node-dash-settings` for the backups, readable only by the dashboard's user.
- **It never changes an owner or a mode.** `bitcoin.conf` must already exist and be writable by the user the
  dashboard runs as; otherwise the installer says why and changes nothing. A `bitcoin.conf` owned by root, like
  `/etc/bitcoin/bitcoin.conf` of Bitcoin Core's own systemd unit, isn't supported yet.

How a save works:

- **Only these eight settings, only numbers.** Every other line, comment and section stays exactly as it is. A
  setting is changed where it takes effect, e.g. in `[main]`. New ones go at the top, under `# Set by node-dash`.
- **A backup first**, in `/var/lib/node-dash-settings/backups`. The last 10 are kept.
- **Changed meanwhile?** If `bitcoin.conf` changed since you opened the form, e.g. over SSH, nothing is saved: reload
  and make your change again.
- **Logged:** every save is in `journalctl -u node-dash`, with only the settings that changed, from what to what.
- **Restart:** a change applies when bitcoind restarts. "Saved · restart bitcoind to apply" shows until it has. With
  [node control](#node-control) on, the form offers **Restart now**; otherwise it shows the command.
- **Edited `bitcoin.conf` over SSH?** An editor such as vim replaces the file, and the dashboard's write access goes
  with it. The form then asks you to restart node-dash: `sudo systemctl restart node-dash`.

Not every setting can be changed here:

- **Set elsewhere:** a setting from bitcoind's command line, an `includeconf` file or `settings.json` wins over
  `bitcoin.conf`. The form shows it with a lock and where to change it.
- **Pruning** deletes old blocks when bitcoind restarts, so the form asks first. Turning it off again isn't possible
  here: that means downloading the whole blockchain again (`-reindex`). The target size can be changed.
- **The transaction index** takes hours to build and about 60 GB of disk; the form asks first. A pruned node can't
  keep one.
- **Ranges:** the database cache is at least 4 MiB and no more than the server's memory; the mempool at least 5 MB.

Not supported: Bitcoin Core in Docker, where `bitcoin.conf` is mounted read-only. How it works and why:
[decision 0003](docs/decisions/0003-changing-settings.md).

## Name and icon

The browser tab shows your node's status: the favicon takes the colour of the status circle, and the title reads
e.g. "Attention · Node". In a background tab you can see that the node needs attention.

- **Your own name:** set `NODEDASH_NAME`, e.g. `NODEDASH_NAME="Satoshi's node"`. It shows in the header, the tab title
  and under the icon on your phone's home screen.
  - **Without Docker:** with the installer, `sh -s -- --name "Satoshi's node"` (see [Install](#install)).
  - **With Docker:** under `environment:` in `docker-compose.yml`.

  A name longer than 40 characters is refused at startup.
- **On your phone:** "Add to Home Screen" gives the dashboard its own icon and name. The icon stays the same: a phone
  doesn't let a web app change it, so the status shows in the app.

## Dark mode

The dashboard follows your device's light or dark setting. To choose yourself, use the theme button at the top
right: **System**, **Light** or **Dark**. The choice is kept in that browser only, so your phone and laptop each have
their own.

## Checking a transaction

Under the tiles, a line links to [Ancoro](https://www.ancoro.site), where you can check whether a transaction went
through. The dashboard itself never shows transactions, and peer addresses only with `NODEDASH_PEER_ADDRESSES=1`.

- **Nothing is loaded from Ancoro** until you click.
- **The link doesn't tell Ancoro where your dashboard runs** (`noreferrer`).
- **To hide it,** set `NODEDASH_ANCORO=0`.

## New dashboard versions

The **Dashboard** row on the details page (under Server) shows which version of the dashboard runs. When a newer
release exists, a quiet line under the tiles says so, e.g. "node-dash v0.6.0 is available", with links to what's
new and to the update command. The dashboard can't update itself; you run the command:

- **Without Docker:** the install command again, on the server. It keeps your settings.
- **With Docker Compose:** `docker compose pull && docker compose up -d`, next to your `docker-compose.yml`.

The details page shows the command with a copy button. To know the latest release, the dashboard server asks
GitHub once a day (`api.github.com/repos/mvanasseldonk/bitcoin-node-dash/releases/latest`), together with the
[Bitcoin Core lookup](#upgrade-advice). `NODEDASH_UPDATE_CHECK=0` turns off both. A build of your own (version `dev`)
never asks.

### Automatic updates

Without Docker, node-dash can install new releases by itself. It's **off by default**. Turn it on with the
installer (v0.7.0 or newer):

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --auto-update
```

`--no-auto-update` turns it off again; `--uninstall` removes it too. The installer adds a systemd timer,
`node-dash-update.timer`, that runs `node-dash update` as root once a day, at a random time that stays the same on
your server. It checks once right away. Each run:

- **Only the same major version:** it installs v0.8.0 over v0.7.0, but not v1.0.0. A new major version needs the
  install command, so you can read what changed first.
- **Checks the signature** of the release with the key built into the node-dash that is installed, never the new
  one, and the archive against `checksums.txt`. An unsigned or wrongly signed release is refused.
- **Keeps the previous binary** in `/var/lib/node-dash`. When the new version doesn't answer within 30 seconds, it puts
  the previous one back.
- **Writes the result** to `/var/lib/node-dash/update.json`. The details page shows it in the **Automatic updates** row:
  when it last ran, and whether it updated, was up to date, or what went wrong.

It only replaces the binary. When a release changes `node-dash.service`, its release notes say so; run the install
command for that. Logs: `journalctl -u node-dash-update`. To check right away: `sudo systemctl start node-dash-update`.

**With Docker** it doesn't apply: `docker compose pull && docker compose up -d`, or a tool such as
[Watchtower](https://containrrr.dev/watchtower/), updates the container.

**The risk:** an update is installed without anyone looking at it first. The signature makes sure it was signed with
the release key, which only the release workflow can use; it doesn't help when that key or the GitHub account is
stolen. How and why: [decision 0002](docs/decisions/0002-automatic-updates.md).

### Signed releases

From v0.7.0, `checksums.txt` of each release is signed with [minisign](https://jedisct1.github.io/minisign/), key
`241DA55A465F44FF` ([`minisign.pub`](minisign.pub)). The installer and the updater check it. To check a download
yourself:

```bash
minisign -V -p minisign.pub -m checksums.txt
sha256sum -c --ignore-missing checksums.txt
```

The trusted comment names the release, e.g. "node-dash v0.7.0".

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

- **What it reveals:** GitHub sees your IP address asking for the latest Bitcoin Core release, and for the latest
  node-dash release. Nothing about your node is sent.
- **Without internet:** you see the version without advice.
- **Turning it off:** set `NODEDASH_UPDATE_CHECK=0` (`sudo systemctl edit node-dash`,
  `Environment=NODEDASH_UPDATE_CHECK=0`).

Bitcoin Knots and other node software are shown by name, without upgrade advice.

## Development

Requirements: Go 1.26+ and Node.js 26 (see `.nvmrc`; run `nvm use`).

```bash
make run                 # build everything and run on mock data at http://localhost:8080
make run MOCK=warning    # other scenarios: healthy, syncing, warning, error, unreachable, starting, stale, outdated, live, busy, restarted
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

- It polls Bitcoin Core once every 10 seconds with three lightweight calls (`getblockchaininfo`,
  `getnetworkinfo` and `getmempoolinfo`), however many browser tabs are open, and asks for the three fee
  estimates only after a new block. Tests guard this.
- Measured idle: about **11 MB** memory and **0.0% CPU** (0.01 s of CPU time per minute); about 14 MB after
  300 simultaneous requests. The Docker image is about 10 MB.
- CI fails if the container uses 100 MB or more. The systemd unit and the Compose example cap memory at 128 MB.

On your own node, the service's memory and average CPU use since it started:

```bash
pid=$(systemctl show -p MainPID --value node-dash); echo "Memory: $(( $(ps -o rss= -p $pid) / 1024 )) MB"; ps -o etimes=,cputimes= -p $pid | awk '{printf "CPU: %.3f%% on average over %d minutes\n", 100*$2/$1, $1/60}'
```

## License

MIT

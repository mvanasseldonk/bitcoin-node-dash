# 1. How the dashboard controls bitcoind

- **Status:** accepted. Verified on the first node, see [Findings](#findings-on-the-first-node-2026-11-09).
- **Date:** 2026-11-09
- **Issue:** #9, for #10 (stop / start / restart) and #13 (settings form)

## Context

The dashboard should stop, start and restart Bitcoin Core (#10), and later change its settings (#13).
It has no login: it is meant for a trusted home network (product decision in #10).

What we know about the first real node:

- **Where it runs:** a Proxmox VM, running Bitcoin Core as the systemd service `bitcoind.service`, under user `martijn`,
  with `-datadir=/home/martijn/.bitcoin`.
- **The dashboard:** runs on the same VM as the systemd service `node-dash.service`, also under user `martijn`.
- **Hardening:** that service has `NoNewPrivileges=true`, `ProtectSystem=strict` and `ProtectHome=read-only`.
- **RPC:** works with `rpcuser`/`rpcpassword`, with full RPC rights (no `rpcwhitelist`).

Other setups people will use: Bitcoin Core from a distribution package or the official
[contrib unit](https://github.com/bitcoin/bitcoin/blob/master/contrib/init/bitcoind.service) (user `bitcoin`,
`Type=notify`, `Restart=on-failure`), Bitcoin Core in Docker, and node distributions such as Umbrel or Start9.

## Options

### A. RPC `stop`

The dashboard already talks RPC. `stop` shuts Bitcoin Core down cleanly.

- **Start:** impossible. Once bitcoind is down, there is no RPC.
- **Restart:** only works if a supervisor starts it again. That differs per setup:
  - with `Restart=on-failure` (the contrib unit), a clean stop stays stopped;
  - with `Restart=always`, "stop" turns into "restart".
- **Verdict:** useful for a clean stop, but it can't do start, and stop/restart depend on how the unit is written.

### B. systemd over D-Bus, allowed by a polkit rule

The dashboard asks systemd to start, stop or restart `bitcoind.service`. It can call the D-Bus API, or simply run
`systemctl`, which uses that API. As a normal user, systemd checks with polkit whether the user may manage the unit.
A small polkit rule grants exactly that, for one unit and one user:

```js
// /etc/polkit-1/rules.d/50-node-dash.rules
polkit.addRule(function (action, subject) {
  if (action.id === "org.freedesktop.systemd1.manage-units" &&
      action.lookup("unit") === "bitcoind.service" &&
      ["start", "stop", "restart"].indexOf(action.lookup("verb")) >= 0 &&
      subject.user === "martijn") {
    return polkit.Result.YES;
  }
});
```

- **Start, stop and restart all work.** systemd stops bitcoind with SIGTERM, which is a clean shutdown in Bitcoin Core.
  The contrib unit allows 10 minutes for it.
- **Status:** `systemctl is-active` / `show` tell the state (`activating`, `active`, `deactivating`, `inactive`,
  `failed`) without any rights. That is exactly the progress #10 asks for. Combined with the existing *Starting*
  state from RPC warm-up (#6), it reads: stopping → stopped → starting → syncing.
- **Hardening:** works with `NoNewPrivileges=true`, because nothing changes user. The dashboard only talks to the
  system bus.
- **Blast radius:** only `bitcoind.service`, only these three verbs. It can't touch other units or files.
- **Requires polkit (`polkitd`).** It is installed on most desktops and on Ubuntu Server, but often not on a minimal
  Debian server. Polkit's [`action.lookup("unit")`/`"verb"`](https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.systemd1.html#Security)
  details need systemd 226 or newer, which every current distribution has.
- **Go:** run `systemctl` with a timeout. That needs no new dependency (`go.mod` has none today). A D-Bus library
  (`go-systemd`) could replace it later.

### C. A sudo rule

`martijn ALL=(root) NOPASSWD: /usr/bin/systemctl start bitcoind.service, …`

- **Works without polkit.** But sudo switches user, which `NoNewPrivileges=true` forbids, so the dashboard's
  hardening would have to go.
- **Sudo rules with arguments are easy to get subtly wrong.**
- **Verdict:** worse than B on both counts.

### D. A systemd path unit as a "doorbell"

A root-owned `node-dash-control.path` watches a request file, for example `/run/node-dash/request`. The dashboard
writes `restart` into it; the matching `.service` runs `systemctl restart bitcoind.service`.

- **Works without polkit and without sudo**, and keeps all hardening.
- **The dashboard can only ring the bell:** it gets no direct answer. Progress still comes from
  `systemctl is-active`, which needs no rights.
- **Verdict:** the fallback when polkit isn't available. Two more units to install, which `install.sh` can do.

### E. The Docker socket

For Bitcoin Core in Docker, the dashboard could start and stop the container through `/var/run/docker.sock`.

- **Root on the host:** mounting the socket gives that. Anyone who can use the dashboard could then start any
  container, which is unacceptable on a dashboard without login.
- **A socket proxy doesn't make it narrow enough:** a proxy such as `docker-socket-proxy`, limited to `containers`
  and `POST`, still allows every container.
- **Not needed for the first node.**
- **Verdict:** out of scope until someone needs it; then a separate decision. Option A (RPC `stop`) does work for
  Docker, because Docker's restart policy then decides.

## Decision

1. **Mechanism:** use systemd for start, stop and restart (option B), by running
   `systemctl start|stop|restart bitcoind.service`.
   - **Unit name:** detected, and configurable with `BITCOIN_SERVICE`.
   - **Permission:** a polkit rule scoped to that one unit and the dashboard's user.
2. **Read state:** from `systemctl show -p ActiveState,SubState bitcoind.service`, combined with RPC.
   Only offer actions that fit the state.
3. **Fallback when polkit is missing:** the path unit (option D).
   - `install.sh` detects polkit and installs the rule or the path units. It asks first, because it grants the
     dashboard power over the node.
   - Without either, the buttons don't appear, and the dashboard explains how to enable them.
4. **Docker:** no Docker socket (option E is rejected). For a containerised Bitcoin Core, only a clean
   "stop" via RPC may be offered later.
5. **Off by default:** node control is opt-in, behind `NODEDASH_CONTROL=1`.
   - **Why:** a read-only dashboard is the safe default, because there is no login.
   - **When it's off:** the API endpoints for control return 404.

## Security

There is no login, so anything that can send an HTTP request to the dashboard can stop the node. Besides people
on the LAN, that includes **web pages in a browser on the LAN**.

- **Cross-site requests:** a malicious site can send a request to `http://192.168.1.119:8080` from your browser.
- **DNS rebinding:** a site can make its own domain point to your server's IP, so the browser treats the dashboard
  as that site.

So the control endpoints (#10) must:

- **Only accept `POST` with `Content-Type: application/json`.** Browsers send such requests cross-site only after
  a CORS preflight, which the dashboard doesn't answer. The request never arrives.
- **Check `Origin`:** reject requests whose `Origin` header doesn't match the dashboard's own address.
- **Check `Host`:** only accept an IP address, `localhost`, or a host name the user configures
  (`NODEDASH_HOSTS`). This blocks DNS rebinding.
- **Never** do anything on `GET`.

The polkit rule limits what a successful attacker could do: stopping or restarting Bitcoin Core, nothing else on
the server.

## Reading and writing bitcoin.conf (#13)

- **Reading already works** (#25): the dashboard runs as the user that owns the data directory.
- **Writing is blocked today** by `ProtectHome=read-only`. It would need `ReadWritePaths=` for that one file in the
  unit. The file also holds secrets (`rpcpassword`) and settings that can break the node or its privacy
  (`rpcallowip`, `proxy`, `prune`).
- **Proposal for #13:** a separate decision.
  - **Scope:** only a short allow-list of safe settings, never the RPC or network-binding ones.
  - **Safety:** always a backup copy, and a restart via the mechanism above to apply changes.

## Verification

Before #10 is built, check on the node:

```bash
systemctl cat bitcoind.service | grep -E '^(Type|User|Restart|ExecStart|ExecStop|TimeoutStopSec)='; systemctl --version | head -n 1; command -v pkaction >/dev/null && echo "polkit: installed" || echo "polkit: not installed"
```

- **polkit installed:** add the rule above, then as `martijn`, without sudo, run
  `systemctl restart bitcoind.service`. It must work without a password prompt. Then the decision holds.
- **polkit not installed:** decide between installing it (`sudo apt install polkitd`) and the path unit fallback.
- **`Restart=always` in the unit:** that's fine with option B, because systemd's own stop doesn't trigger a restart.

### Findings on the first node (2026-11-09)

- **Versions:** Ubuntu 24.04 (systemd 255), and polkit is installed. That version reads JavaScript rules from
  `/etc/polkit-1/rules.d/`, so option B applies.
- **`bitcoind.service`:** `Type=forking`, `User=martijn`, `Restart=always`.
  - **What `Restart=always` means here:** `systemctl stop` stops for good.
  - **Pitfall:** an RPC `stop` or `bitcoin-cli stop` makes systemd start bitcoind again. The dashboard must
    therefore always stop through systemd, never through RPC.
- **No `TimeoutStopSec`:** systemd's default of 90 seconds applies. Bitcoin Core can need longer to shut down
  cleanly, because it first writes its cache to disk. After 90 seconds systemd kills it, which can corrupt the
  chainstate and force a long reindex. That risk exists today on every reboot, regardless of the dashboard.
  - **Fix:** add a drop-in with `TimeoutStopSec=600`, the value the contrib unit uses.
  - **For #10:** the dashboard must check this timeout, and warn about it before it offers stop or restart.
- **Permission check:** `pkcheck` tests the rule without restarting the node. Passing details such as `unit`
  and `verb` is only allowed for root, so root asks on behalf of the user's own shell process:

  ```bash
  for unit in bitcoind.service ssh.service; do sudo pkcheck --action-id org.freedesktop.systemd1.manage-units --process $$ --detail unit $unit --detail verb restart >/dev/null 2>&1; echo "$unit: exit=$?"; done
  ```

  The result: `bitcoind.service: exit=0` (allowed by the rule) and `ssh.service: exit=2` (only with an admin
  password). The rule is scoped as intended. `install.sh` can use the same check after installing the rule.

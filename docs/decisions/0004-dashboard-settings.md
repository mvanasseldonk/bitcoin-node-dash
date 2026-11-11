# 4. How the dashboard changes its own settings

- **Status:** proposed
- **Date:** 2026-11-11
- **Issue:** #61

## Context

#61 lets people change the dashboard's own settings on a page in the dashboard, reached by a button in the header.
The design is in the canvas.

- **Today every `NODEDASH_*` setting is an environment variable,** read once at startup. With systemd it is in the
  unit or a drop-in; with Docker it is in `docker-compose.yml`.
- **The dashboard can't write its own unit,** and shouldn't: `/etc` is read-only (`ProtectSystem=strict`, see
  [decision 0001](0001-controlling-bitcoind.md)). The unit is root's.
- **No login:** what the page can do, anyone on the home network can do, and so can web pages in their browser (0001,
  *Security*). #61 therefore splits the settings:
  - **display:** `NODEDASH_NAME`, `NODEDASH_ANCORO`, `NODEDASH_UPDATE_CHECK`, which can be changed;
  - **access switches:** `NODEDASH_CONTROL`, `NODEDASH_SETTINGS`, `NODEDASH_PEER_ADDRESSES`, which can only be turned
    **off** here;
  - **fixed at startup:** `NODEDASH_LISTEN` and `NODEDASH_HOSTS`, which are only shown.
- **On by default** (#61, decision 7). `NODEDASH_WEB_SETTINGS=0` turns the page off.
- **The state directories that exist:**
  - `/var/lib/node-dash` belongs to root and the updater ([decision 0002](0002-automatic-updates.md));
  - `/var/lib/node-dash-settings` holds the backups of `bitcoin.conf`, and only exists with `NODEDASH_SETTINGS=1`
    ([decision 0003](0003-changing-settings.md)).
- **In Docker,** the container has no writable volume: the data directory is mounted read-only.

## Options

### Where the values are kept

- **A. A state directory of its own,** `/var/lib/node-dash-web`, from `StateDirectory=` in the unit. systemd creates
  it, owned by the dashboard's user. The rest of the hardening stays.
- **B. In `/var/lib/node-dash-settings`.** Rejected: it only exists with `NODEDASH_SETTINGS=1`, and its job is the
  backups of `bitcoin.conf`.
- **C. A drop-in in `/etc`,** written by a root helper, like the path unit of 0001, option D. Then `systemctl cat`
  would show everything. Rejected: two more units, and root would act on what the dashboard asks. That is what 0003
  avoided for `bitcoin.conf`.

### How a switch turned off here is turned on again

- **A. The installer edits the dashboard's file.** Rejected: root would parse JSON in shell (no `jq` on a fresh
  server), and a mistake there breaks the file.
- **B. One file per switch turned off,** next to the values. Root only has to delete a file: `rm -f`.
- **C. A time stamp in the drop-in,** newer than the "off". Rejected: the installer would rewrite the drop-in, and
  restart the dashboard, on every run.

## Decision

### 1. Own state directory, two kinds of file (options A and B)

The unit that `install.sh` writes, and `deploy/node-dash.service`, get:

```ini
StateDirectory=node-dash-web
StateDirectoryMode=0700
```

In `/var/lib/node-dash-web`:

- **`display.json`:** the display settings changed here. Only what was changed is in it; a key that isn't there
  comes from the environment.

  ```json
  {"v": 1, "name": "Satoshi's node", "ancoro": false}
  ```

- **`off/control`, `off/settings`, `off/peer-addresses`:** one file per switch turned off here. Its content is the
  time it was turned off (RFC 3339). The page shows that time. If the file exists, the switch is off.

Both are mode `0600`, owned by the dashboard's user. `display.json` is written atomically: a temporary file in the
same directory, `fsync`, then `rename()`. Unlike `bitcoin.conf` in 0003, the directory is writable, so this works.

- **Finding the directory:** from `$STATE_DIRECTORY`. With two `StateDirectory=` lines (this one, and
  `node-dash-settings` from the settings drop-in), systemd puts both in it, separated by `:`, in no promised order.
  So each one is chosen **by its name**, also for the existing `settingsStateDir()` of 0003, which now takes the
  first entry. Without `$STATE_DIRECTORY` (a unit made by hand, `go run`), the fallback is `/var/lib/node-dash-web`.
- **No directory, or not writable:** the page shows every value with its source, but can't save. It says why, and
  that the environment applies: "Not saved: node-dash can't write its settings in /var/lib/node-dash-web".
- **A unit changed by hand,** which the installer keeps as it is: the installer says the page needs
  `StateDirectory=node-dash-web` and prints the two lines to add.

### 2. Which value wins

- **Display settings:** the value in `display.json`, then the environment variable, then the default. "Use that"
  on the page removes the key from `display.json`, so the environment applies again.
- **Access switches:** on only if the environment turns it on **and** there is no `off/<switch>` file. The page
  can create that file; nothing can create the opposite.
- **The page shows the source of each value:** "Default", "Environment", or "Set here".

### 3. When something is wrong with the files

- **The values fail open; the switches fail closed.**
  - **`display.json` can't be read or parsed:** it is ignored, with a warning in the log and on the page. The
    environment applies, and saving overwrites it, so it can be repaired from the page.
  - **`off/` can't be read** (a permission or I/O error, not "doesn't exist"): all three switches are treated as
    **off**, with a warning in the log and on the page. Otherwise one broken directory would turn node control back
    on.
- **This differs from #61,** which says a broken file is ignored and the environment applies. That still holds for
  the display settings. For the switches, failing closed is safer.

### 4. Changes apply without a restart

- **Name:** the server puts it in the page and in `manifest.webmanifest` on every request, instead of once at
  startup. After a save, the frontend changes the header and the tab title at once.
  - **A home screen icon** that is already there keeps its old name. That is how browsers handle the manifest.
- **Ancoro link and update check:** read on every request or daily lookup, not once.
  - **Update check turned on again:** the daily lookup runs within a minute.
  - **Turned off:** the next lookup is skipped, and what was already found stays shown until it is old.
- **Switches turned off:** `/api/control` and `POST /api/settings` check the switch on every request and answer 404
  once it is off. The Peers page stops getting addresses from the next poll.

### 5. The API

Only on `GET` and `POST /api/dashboard`. With `NODEDASH_WEB_SETTINGS=0`, both answer 404 and there is no button.

- **`GET`:** each value with its source, the environment's value, and for each switch whether it is on and since
  when it is off. Never other environment variables.
- **`POST`:** the protections of `/api/control` (0001): only `Content-Type: application/json`, and the `Origin` and
  `Host` checks.
  - **Body:**
    - `{"version": n, "set": {"name": "…", "ancoro": false, "updateCheck": true}, "reset": ["name"], "off": ["control"]}`;
    - every part is optional, and unknown keys are refused (400);
    - `version` is the one the page got from `GET`. If the files changed since, the save is refused (409), as for
      `bitcoin.conf` in 0003.
  - **There is no "on":** `"on"` in the body, or a switch in `set`, is answered with **403**, so anyone reading the
    API sees it is refused on purpose.
  - **Validation:**
    - the name is checked by the same function as `NODEDASH_NAME` (at most 40 characters, valid UTF-8, no control
      characters);
    - the two others are `true` or `false`;
    - a refused field gives 422 with the reason, as in 0003.
- **Log:** one line per save, with what changed: `dashboard settings: name "Node" → "Satoshi's node"`, or
  `dashboard settings: node control turned off`.

### 6. Turning a switch on again

- **The installer, when a switch is turned on:** `install.sh` with `NODEDASH_CONTROL=1`, `NODEDASH_SETTINGS=1` or
  `NODEDASH_PEER_ADDRESSES=1` removes `off/<switch>` and restarts the dashboard if the file was there. It only
  removes; it never writes there.
- **`NODEDASH_PEER_ADDRESSES`** becomes an installer option, like the other two, with a drop-in
  (`node-dash.service.d/peers.conf`). That way all three are turned on the same way.
- **By hand:** `sudo rm /var/lib/node-dash-web/off/control` and `sudo systemctl restart node-dash`. The README says
  so. The page and the dialog show the installer command.

### 7. Docker

- **The page works without a volume,** read-only: the values with their source, and why it can't save.
- **With a volume it can save:**
  - `docker-compose.yml` gets a commented-out volume, `./node-dash:/var/lib/node-dash-web`, plus how to make the
    directory for the container's user (`mkdir node-dash && chown 1000:1000 node-dash`);
  - the same path is used inside the container, so the code doesn't need to know about Docker.
- **Of the switches, only peer addresses applies in Docker.** Node control and changing settings aren't supported
  there (0001, 0003). The page shows them as "Not available in Docker".

## Security

- **An attacker who gets past the `Origin` and `Host` checks can:**
  - rename the node;
  - hide the Ancoro link;
  - turn off the update check, so the page no longer says an upgrade is out;
  - turn off any of the three switches.

  That is all. None of it adds access, and all of it can be undone on the server. It is also why the page is on by
  default.
- **They can't:**
  - turn anything on;
  - change the address, the host names or the RPC settings;
  - read or write any other file.

  The dashboard only writes its own state directory, and only with values it checked.
- **Off here is off in the dashboard, not on the server.** Turning node control off here leaves the polkit rule in
  place, and turning changing settings off leaves the drop-in that lets the dashboard write `bitcoin.conf`. The
  dashboard simply stops doing it. To remove the access itself, run the installer with `NODEDASH_CONTROL=0` or
  `NODEDASH_SETTINGS=0`. The dialog and the README say so.
- **The update check** is turned off only for the dashboard's own lookup. The updater of 0002 (`node-dash update`,
  run by root) doesn't read `display.json`, so automatic updates keep working.

## Verification

Before #61 is built, check on the node that two state directories work side by side. Nothing here changes the
dashboard or `bitcoin.conf`:

```bash
sudo systemd-run --pty --wait --uid=martijn -p StateDirectory=node-dash-test-a -p StateDirectory=node-dash-test-b -p ProtectSystem=strict sh -c 'echo "$STATE_DIRECTORY"; touch /var/lib/node-dash-test-a/x && echo "a: writable"'; sudo rm -rf /var/lib/node-dash-test-a /var/lib/node-dash-test-b
```

It should print both directories, separated by `:`, and `a: writable`.

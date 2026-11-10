# 3. How the dashboard changes bitcoin.conf

- **Status:** proposed. To verify on the first node, see [Verification](#verification).
- **Date:** 2026-11-10
- **Issue:** #55 (the second half of #13)

## Context

#13 shows eight settings of Bitcoin Core: `dbcache`, `maxconnections`, `maxmempool`, `prune`, `txindex`, `listen`,
`blocksonly` and `maxuploadtarget`. #55 lets people change them with a form.

- **Writing is blocked today, on purpose.** `node-dash.service` runs with `ProtectSystem=strict`,
  `ProtectHome=read-only` and `NoNewPrivileges=true` ([decision 0001](0001-controlling-bitcoind.md)). It can read the
  data directory, not write it.
- **No login:** whatever the form can do, anyone on the home network can do, and so can web pages in their browser
  (see 0001, *Security*).
- **The file holds secrets** (`rpcpassword`, `rpcauth`) and settings that can break the node or its privacy
  (`rpcallowip`, `proxy`, `bind`). The form must never be able to write those, nor show them.
- **Some changes cost a lot:**
  - turning `prune` on deletes old blocks at the next start;
  - turning it off again is impossible without downloading the whole chain again (Bitcoin Core refuses to start
    without `-reindex`);
  - `txindex` takes hours to build and about 60 GB.
- **Where the file is:**
  - the data directory (`~/.bitcoin/bitcoin.conf`), owned by the node's user. This is the first node;
  - `/etc/bitcoin/bitcoin.conf` with the contrib unit, usually owned by root and only readable by group `bitcoin`;
  - in Docker, mounted read-only (`docker-compose.yml`).
- **`settings.json`:** Bitcoin Core also reads `<datadir>/settings.json`, and its values **win over
  `bitcoin.conf`**. Order of precedence: command line, then `settings.json`, then `bitcoin.conf`. bitcoind itself
  only writes wallet names there. The GUI (`bitcoin-qt`) also stores `dbcache`, `prune` and others there. #13
  doesn't read it yet.

## Options

### Who writes the file

- **A. The dashboard itself,** with `ReadWritePaths=` for that one file in a drop-in. All other hardening stays.
  - **No new process.**
  - **What an attacker could do:** change the eight settings, within the checks below. Nothing else, because the
    API only takes those keys and only integers.
- **B. A root-owned helper,** rung like the path unit of 0001, option D. The dashboard puts the new file in its state
  directory; root checks it and installs it.
  - **Works for a root-owned `/etc/bitcoin/bitcoin.conf` too.**
  - **But root would have to parse and check a file that came from the dashboard.** A bug there runs as root. There
    are also two more units, and the dashboard gets no direct answer.
- **C. Give the dashboard write access to the data directory.** Rejected: it holds the chain, the wallets and the
  cookie.

### How the file is written

- **Atomically:** a temporary file, then `rename()`. That needs write access to the **directory**, which is the data
  directory (option C). It also gives the file a new owner and mode unless both are copied.
- **In place:** open the file itself, write and `fsync`. Owner, mode and hard links stay. A crash in the middle could
  leave a half-written file, so a backup must come first.

## Decision

### 1. Opt-in, like node control

- **Off by default.** `NODEDASH_SETTINGS=1` turns it on. Without it, the section of #13 stays read-only, there is
  no "Change settings" link, and every settings write returns 404.
- **Same protections as `/api/control`:**
  - only `POST` with `Content-Type: application/json`;
  - the `Origin` and `Host` checks of 0001;
  - never anything on `GET`.

### 2. The dashboard writes the file itself (option A), in place

`install.sh` with `NODEDASH_SETTINGS=1` writes a drop-in, `node-dash.service.d/settings.conf`:

```ini
[Service]
Environment=NODEDASH_SETTINGS=1
ReadWritePaths=/home/martijn/.bitcoin/bitcoin.conf
StateDirectory=node-dash-settings
StateDirectoryMode=0700
```

- **Write access for one file only.** `ReadWritePaths=` gets the real path of `bitcoin.conf` (symlinks resolved),
  never the data directory.
- **The file must exist** and the dashboard's user must be able to write it. Otherwise the installer stops with the
  reason. **It never changes an owner or a mode:** for a root-owned `/etc/bitcoin/bitcoin.conf`, it says so and
  turns nothing on.
- **In place, because it must be:** `ReadWritePaths=` on a file is a bind mount of that file, and nothing can be
  renamed over a mount point. So the dashboard writes into the file itself, after the backup (4), then `fsync`s.
- **An editor that replaces the file:** `vim` over SSH, for example, writes a new file and renames it. Linux then
  drops the bind mount in the dashboard's namespace, so the new file is read-only for the dashboard. A save then
  fails with "restart node-dash to save again" until the service restarts. It fails closed: it never writes to a
  stale copy. [Verification](#verification) checks this on the node.
- **Not supported for now:**
  - **Docker:** the file is mounted read-only. The form says so.
  - **A root-owned file (the contrib unit):** option B, only when someone needs it, with a decision of its own.

### 3. What can be changed

- **Only the eight keys, and only as integers.** The API takes `{"key": number}` for these keys and nothing else.
  The dashboard writes the line itself (`key=123`), so no other text can get into the file.
- **Ranges:**

  | Setting           | Allowed                                                                                    |
  |-------------------|--------------------------------------------------------------------------------------------|
  | `dbcache`         | 4 MiB or more. A warning above half of the server's memory; refused above all of it        |
  | `maxconnections`  | 0 or more                                                                                  |
  | `maxmempool`      | 5 MB or more                                                                               |
  | `prune`           | 0 (off) or 550 MiB or more. A `prune=1` (manual) that is already there is kept, not offered |
  | `txindex`, `listen`, `blocksonly` | 0 or 1                                                                     |
  | `maxuploadtarget` | 0 (unlimited) or more, in MiB                                                              |

- **Refused, with the reason:**
  - `prune` with `txindex`: Bitcoin Core won't start with both;
  - turning `prune` off while the node reports `pruned`, or while the node can't be reached and the file has
    `prune` on.
- **Read-only, with the reason:**
  - a setting that comes from the **command line**, because it wins over the file. The form names the unit to edit;
  - a setting from an **`includeconf` file**: the dashboard only writes the one file it has access to;
  - a setting in **`settings.json`**, because it wins over the file. #55 therefore also reads `settings.json`, for
    these eight keys only, and #13's section then shows "settings.json" as a source.
- **Confirmations,** in the form, before saving:
  - **turning `prune` on:** "Old blocks will be deleted when bitcoind restarts. Turning pruning off again means
    downloading the whole blockchain";
  - **turning `txindex` on:** "Building the index takes hours, uses about 60 GB, and the node is slower meanwhile".

### 4. A save, step by step

The form gets the file's SHA-256 when it opens and sends it back with the changes. One save at a time (a mutex).

1. **Read** the file again. **Refuse** if its hash differs: "bitcoin.conf changed since you opened the form".
2. **Validate** the values and combinations (3) against the file and what the node reports.
3. **Edit the text**, line by line, keeping every other byte:
   - **A key that is set:** its effective line is replaced: in the network section (`[main]`, `[test]`,
     `[signet]`…) if it is set there, otherwise the first one at the top level. A trailing `# comment` stays, and
     `noprune=1` becomes `prune=…`.
   - **A key that isn't set yet:** it goes at the top level, **before the first section header**. Otherwise it would
     land in a section. It goes under one `# Set by node-dash` comment, which is reused next time.
   - **The rest is never touched:** later duplicates, which Bitcoin Core ignores, other keys, comments, sections,
     line endings, and whether the file ends with a newline.
4. **Back up** the current file to `/var/lib/node-dash-settings/backups/bitcoin.conf.<UTC time>`:
   - mode `0600` in a `0700` directory, because it holds `rpcpassword`;
   - `fsync`ed, then the oldest beyond 10 are removed;
   - outside the data directory. It isn't `/var/lib/node-dash` either, because that belongs to root and the updater.
5. **Write** in place: truncate, write, `fsync`. The file is a few kB, so it is one write.
6. **Check** by parsing the file again. The eight settings must be what was asked, and every other line the same.
   If not, the backup is written back and the save fails with the reason.
7. **Log** one line, with only the keys that changed, e.g.
   `settings saved: maxmempool 300 → 500, dbcache 450 → 2048; backup …/bitcoin.conf.20261110T195017Z`.
   Never other lines, never secrets.

### 5. Restart

- **When it is needed:** a saved change only applies when bitcoind restarts. The dashboard keeps the time of the last
  save in its state directory. "Saved · restart needed" shows until bitcoind has started after that time; its start
  time is in `/proc/<pid>/stat`, or `uptime` over RPC.
  - **The node reports five of the eight values** (`prune`, `txindex`, `maxmempool`, `maxuploadtarget`,
    `blocksonly`). For those, #13's mismatch hint also shows.
  - **`dbcache`, `maxconnections` and `listen` have only the start time to go by.**
- **With node control on** (`NODEDASH_CONTROL=1`), the form offers **Restart now**, through `/api/control`. If the
  save turned `prune` or `txindex` on, its confirmation repeats that warning.
- **Without node control,** the form shows the command: `sudo systemctl restart bitcoind.service`.
- **Restarts are never automatic.**

## Security

- **An attacker who gets past the Origin and Host checks** can change the eight settings within the ranges above, and
  restart bitcoind if node control is on.
  - **They can't** read or write other lines, read the backups, or touch any other file.
  - **The worst they can do:** turn `prune` on, which costs a full download of the chain to undo. That is why the
    feature is off by default, and why the README says so before showing how to turn it on.
- **The write access is to one file.** Even a bug in the editor can't write outside it. The backups are only readable
  by the dashboard's user.

## Verification

Before #55 is built, check on the node. Nothing here changes `bitcoin.conf`.

1. **The file and its owner:**

   ```bash
   ls -l /home/martijn/.bitcoin/bitcoin.conf; readlink -f /home/martijn/.bitcoin/bitcoin.conf
   ```

2. **`ReadWritePaths=` on one file works with the dashboard's hardening.** The file must be writable, and the data
   directory must not be:

   ```bash
   sudo systemd-run --pty --wait --uid=martijn -p ProtectSystem=strict -p ProtectHome=read-only -p NoNewPrivileges=true -p ReadWritePaths=/home/martijn/.bitcoin/bitcoin.conf sh -c 'test -w /home/martijn/.bitcoin/bitcoin.conf && echo "bitcoin.conf: writable"; test -w /home/martijn/.bitcoin && echo "data dir: WRITABLE (wrong)" || echo "data dir: read-only"'
   ```

3. **An editor that replaces the file:** the same, on a copy in `/tmp`, so the real file stays untouched. The second
   check must say "read-only":

   ```bash
   cp /home/martijn/.bitcoin/bitcoin.conf /tmp/rw-test.conf && sudo -v && { sudo systemd-run --quiet --pipe --wait --uid=martijn -p ProtectSystem=strict -p ReadOnlyPaths=/tmp -p ReadWritePaths=/tmp/rw-test.conf sh -c 'test -w /tmp/rw-test.conf && echo "before: writable"; sleep 5; test -w /tmp/rw-test.conf && echo "after: writable" || echo "after: read-only"' & sleep 2; cp /tmp/rw-test.conf /tmp/rw-test.new && mv /tmp/rw-test.new /tmp/rw-test.conf; wait; }; rm -f /tmp/rw-test.conf /tmp/rw-test.new
   ```

4. **`settings.json`:** which keys it has. `{}` or only `wallet` means nothing overrides the eight settings:

   ```bash
   cat /home/martijn/.bitcoin/settings.json 2>/dev/null || echo "no settings.json"
   ```

- **If 2 or 3 behave differently,** the in-place approach is reconsidered before building.

# 2. How node-dash updates itself

- **Status:** accepted.
- **Date:** 2026-11-10
- **Issue:** #50 (split from #39)

## Context

People who install node-dash with `install.sh` update it by running the install command again, over SSH. #39 shows
when a new version exists; #50 installs it without SSH.

- **The dashboard can't update itself, on purpose.** `node-dash.service` runs with `ProtectSystem=strict`, so
  `/usr/local/bin` is read-only, and `NoNewPrivileges=true`, so no sudo. That stays.
- **No login:** whatever the dashboard can start, anyone on the home network can start too.
- **Checksums alone prove little:** `checksums.txt` comes from the same GitHub release as the archive. It catches a
  broken download, not a changed release. Automatic updates install without anyone looking, so that matters more.

## Options

- **A. A root-owned systemd timer** that runs the updater once a day. Nothing is added to the dashboard that can be
  attacked.
- **B. An "Update" button** that rings a root-owned path unit, as in [decision 0001](0001-controlling-bitcoind.md),
  option D. Needs the protections of node control (POST with JSON, Origin and Host checks).
- **C. Only a notice** with the command (#39).

## Decision

C (#39) and A (#50), both opt-in. B only when it's missed.

### The updater

`node-dash update`, run as root by `node-dash-update.service` from `node-dash-update.timer`
(`OnCalendar=daily`, `RandomizedDelaySec=1d`, `FixedRandomDelay=true`, `Persistent=true`). Written in Go, in the
binary that is installed, so it is tested like the rest and needs no `jq` or `minisign` on the server.

1. **Look up** the latest release on GitHub. Stop unless it is newer **and of the same major version**, or the step from 0.x to 1.x (see the addendum).
2. **Download** `checksums.txt`, `checksums.txt.minisig` and the archive for this CPU.
3. **Verify**, before anything changes:
   - the signature, with the release key built into the **installed** binary;
   - the trusted comment, which must be `node-dash <tag>`, so an old signed `checksums.txt` can't pass for a newer
     release;
   - the archive's SHA-256 against `checksums.txt`;
   - that the new binary says it is that version.
4. **Install:** copy the installed binary to `/var/lib/node-dash/node-dash.previous`, put the new one next to it and
   rename it into place, so the binary is never half written. Restart `node-dash.service`.
5. **Roll back** when the new version doesn't answer `/healthz` and report its version in `/api/status` within
   30 seconds: put the previous binary back and restart again.
6. **Report** in `/var/lib/node-dash/update.json`, which the dashboard reads and shows. The dashboard needs no new
   rights for that.

Its unit is hardened as far as its job allows: `ProtectSystem=strict` with `ReadWritePaths=/usr/local/bin`,
`StateDirectory=node-dash`, `PrivateTmp`, `ProtectHome`, `NoNewPrivileges`.

### Signatures

- **minisign:** small, one key, one file per release. The private key and its password are secrets of the GitHub
  environment `release`, which only `v*` tags can use. The maintainer made the key on their own machine; it never passed
  through anyone else.
- **Legacy format (`minisign -l`):** minisign 0.10 and newer sign a BLAKE2b hash of the file by default, which Go's
  standard library can't compute. The legacy format signs the file itself with Ed25519, which it can. `checksums.txt`
  is small, so hashing first gains nothing. No new dependencies.
- **Verified with the installed binary:** the new binary is never trusted to check itself.
- **The release workflow checks its own signature** with `minisign.pub` and with the built binary before publishing,
  so a wrong key or format fails the release instead of every updater.
- **The installer** checks the signature with the installed node-dash when it can, else with `minisign` when that is
  installed, else only the checksum, and says so.

## Security

- **What it protects against:** a changed archive or `checksums.txt` on GitHub's download servers or on the way, and
  an older signed release passed off as a newer one.
- **What it doesn't:** a stolen signing key, or a stolen GitHub account that changes the release workflow and signs
  with it. The first install trusts `install.sh` from GitHub either way.
- **Why only the same major version:** a major version may change the unit, the settings or the data the dashboard
  needs. Those need the install command, and someone reading the release notes. v1.0.0 is the one exception (see the
  addendum).
- **Why not in Docker:** a container can't replace its own image. `docker compose pull` or Watchtower do that.
- **Losing the key:** a new key means a new release key in the binary. Servers with the old key refuse releases signed
  with the new one, so they need the install command once.

## Verification

The CI job `install-script` runs it on a real systemd host, from a fake release server and with a throwaway key built
into test binaries: turning it on updates v0.90.0 to v0.91.0; a v0.92.0 that doesn't start is rolled back to
v0.91.0; a v0.93.0 signed with another key is refused; turning it off removes the timer, the service and the status.

Since #74 it also updates v0.91.0 to v1.0.0 by itself, and then skips a v2.0.0. Unit tests cover 0.x → 1.0.0,
1.x → 2.0.0, 0.x → 2.0.0 and an older major version.

## Addendum: from 0.x to 1.0 (#74, 2026-11-12)

The updater also takes the step from any 0.x to 1.x; every other new major version still needs the install command.

- **Why:** under semver, 0.x promises nothing, and 1.0 is where the promise starts. v1.0.0 is v0.13.2 with a new
  number: it changes none of the unit, the settings or the data. Skipping it would leave every server that updates
  itself on 0.x, with a note to run the install command for nothing.
- **Why a v0.13.2 first:** the check runs in the version that is installed. v0.13.1 refuses v1.0.0, so v0.13.2 brings
  the exception, and v1.0.0 comes once the daily timers have installed it.
- **What a major version means from 1.0 on:** the README's "Versioning" section.

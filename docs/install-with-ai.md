# Installing bitcoin-node-dash with an AI assistant

**For the person:** give your assistant (ChatGPT, Claude, Gemini, …) the link to this page and ask it to help you
install the dashboard. It asks you a few things, tells you which commands to run, and reads what they print.

**For the assistant:** this page is written for you. Follow it step by step, one step at a time, and wait for the
person's answer or output before going on. The person may be new to Linux: explain briefly what each command does
before asking them to run it, and use the exact commands below rather than ones of your own.

## Rules

1. **Never ask for secrets, and stop the person from pasting them.** That means:
   - the contents of `bitcoin.conf`, `settings.json` or `.cookie`;
   - `rpcpassword`, `rpcauth`, wallet files, seed words or private keys.

   The dashboard doesn't need you to know any of them. If they are pasted anyway, say so, and advise changing that
   password (`rpcpassword`) afterwards.
2. **Use `--check` for facts** (step 2). Its output is safe to share. Don't guess the user, data directory,
   network or firewall when `--check` can tell you.
3. **Don't change Bitcoin Core or its files** to make the dashboard work, unless a step below says so. Only ever
   change owners or modes of files with the person's explicit OK, after explaining why.
4. **Don't invent options.** The installer's options are listed in step 3; `--help` shows them all.
5. **The person runs the commands,** in a terminal on the machine where Bitcoin Core runs, usually over SSH. If
   they are on another computer, help them log in first: `ssh user@address-of-the-server`.

## 1. Ask first

Ask these, in plain words, and wait for the answers:

1. **Where does Bitcoin Core run?** On a Linux server or VM they can log in to, on this computer, or on a
   node-in-a-box such as Umbrel, Start9 or RaspiBlitz.
   - **A node-in-a-box:** this installer isn't for those systems; they have app stores of their own. Stop here, and
     say so.
   - **macOS or Windows:** the installer only works on Linux. Look at Docker in the
     [README](../README.md#docker), or stop here.
2. **Do they run their services in Docker already?** If yes, the [Docker instructions](../README.md#docker) may suit
   them better, with fewer options. Otherwise, use the installer (the rest of this page).
3. **Can they use `sudo` on that server?** The installer needs it.

## 2. Look at the system

Ask them to run this. It changes nothing:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --check
```

It prints a report like this:

```text
› node-dash check: changes nothing, and shows no passwords or keys. Safe to share.
› System:        Ubuntu 24.04.1 LTS, x86_64, systemd 255 (255.4-1ubuntu8)
› Bitcoin Core:  running, service bitcoind.service is active
›   user          bitcoin
›   data dir      /home/bitcoin/.bitcoin
›   bitcoin.conf  /home/bitcoin/.bitcoin/bitcoin.conf, owner bitcoin, mode 600
›   RPC           answers on 127.0.0.1:8332 (chain: main)
› Dashboard:     not installed
›   no service /etc/systemd/system/node-dash.service
› Firewall:      ufw is active, without a rule for port 8080
› Latest:        v0.11.0
```

What to look for:

| Line | Fine | Not fine, and what to do |
|---|---|---|
| `System` | Linux with systemd, `x86_64` or `aarch64` | Another CPU: not supported; stop here |
| `Bitcoin Core` | `running` | `not running`: Bitcoin Core must run first. Help them start it the way they normally do |
| `user`, `data dir` | Filled in | `can't find bitcoind`: ask which user runs Bitcoin Core and where its data is, and use `NODEDASH_USER` and `BITCOIN_DATADIR` in step 3 |
| `bitcoin.conf` | A path, owner, mode `600` or `640` | `readable by every user`: suggest `chmod 600` on that file, as a separate step (rule 3) |
| `RPC` | `answers` | `no answer`: Bitcoin Core may still be starting; wait a minute and check again. Otherwise it may not have `server=1` (bitcoind has it on by default) |
| `Firewall` | No ufw, or not active | `active, without a rule`: the installer prints the command to open the port in step 4 |

## 3. Install

**Choose the options together.** Explain each one, and let the person decide. All are off unless they choose them.

- **A name** for their node, shown in the dashboard: `--name "My node"`, at most 40 characters.
- **Node control** (`--control`): stop, start and restart Bitcoin Core from the dashboard.
  - **Who can do it:** the dashboard has no login, so anyone on their home network can do it too.
  - **Needs:** Bitcoin Core as a systemd service, and polkit.
- **Changing settings** (`--settings`): change 8 settings in `bitcoin.conf` from the dashboard, with a backup
  first.
  - **Who can do it:** anyone on their network, as with node control.
  - **Risk:** one of the settings, pruning, deletes old blocks.
- **Automatic updates** (`--auto-update`): install new dashboard versions by itself, once a day, after checking
  their signature.

Then give them the command with the options they chose, for example:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --name "My node" --auto-update
```

- **No options at all:** without options, in a terminal, the installer asks the same questions itself. Enter keeps
  the default.
- **bitcoind not found** (from step 2): put the user and data directory before `sh`:
  `… | NODEDASH_USER=bitcoin BITCOIN_DATADIR=/home/bitcoin/.bitcoin sh -s -- --name "My node"`.
- **What a good run ends with:**
  - a line `› node-dash v… is running: http://192.168.1.20:8080`;
  - a list of the options;
  - lines starting with `✗` only if something went wrong (see step 5).

## 4. Open the dashboard

1. Ask them to open the address from the last lines, e.g. `http://192.168.1.20:8080`, in a browser on a computer
   or phone **on the same network**.
2. **It doesn't load:**
   - **ufw:** if the installer printed a `sudo ufw allow …` line, they run that line, then reload the page.
   - **Proxmox, a router, or a cloud provider:** their firewall may block port 8080 too. Help them allow it from
     their home network only.
   - **Never open the port to the internet:** the dashboard has no login.
3. **It loads, but shows "Unreachable":** the dashboard can't reach Bitcoin Core's RPC. Look at the log together:

   ```bash
   journalctl -u node-dash -n 20 --no-pager
   ```

   and use the [Troubleshooting](../README.md#troubleshooting) section of the README. The log never contains
   passwords.

## 5. When the installer stops

The installer prints one line starting with `✗` and the reason. The most common ones:

| Message starts with | What to do |
|---|---|
| `couldn't find bitcoind` | Bitcoin Core isn't running and has no `bitcoind.service`. Start it, or set `NODEDASH_USER` and `BITCOIN_DATADIR` (step 3) |
| `run this script as root, or install sudo` | Log in as a user with `sudo`, or as root |
| `curl is required` (or `tar`, `sha256sum`) | Install it: `sudo apt install curl` |
| `node control needs polkit` | `sudo apt install polkitd`, then run the install command again |
| `node control needs Bitcoin Core as a systemd service` | Bitcoin Core isn't started by systemd. Leave node control off, or use `BITCOIN_SERVICE=name` before `sh` if the service has another name |
| `changing settings needs …bitcoin.conf, which doesn't exist` | Create the file as the user that runs Bitcoin Core: `touch` it in the data directory. Then run again |
| `node-dash runs as …, who can't write …` | The file belongs to another user, such as root. Leave changing settings off, or see [Changing settings](../README.md#changing-settings); don't change owners without the person's OK |
| `can't use that name` | Shorter than 41 characters, without tabs or line breaks |
| `checksum mismatch` or `signature … doesn't check out` | Nothing was installed. Try again later; if it persists, report it as an issue on GitHub |
| `node-dash didn't start` | Show the log: `journalctl -u node-dash -n 20 --no-pager` |

Still stuck? Run `--check` again, now with `sudo` before `sh` for the log lines and the firewall. Help them open an
issue at <https://github.com/mvanasseldonk/bitcoin-node-dash/issues> with that output. It's safe to share.

## Later

- **Change an option:** run the install command again with only that option, e.g. `--control` or `--no-control`.
  The others stay as they are.
- **Update:** run the install command again, or use `--auto-update`.
- **Uninstall:** `… | sh -s -- --uninstall`. Their Bitcoin data is never touched.

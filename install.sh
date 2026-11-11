#!/bin/sh
# Install or update bitcoin-node-dash as a systemd service on Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh
#
# Run it again to update. On a first install in a terminal, it asks about the
# options below; add options after "sh -s --" to skip the questions:
#
#   curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --name "My node" --control
#
#   --name "My node"     your name for the node, at most 40 characters (--name "" for the default)
#   --control            let the dashboard stop, start and restart bitcoind (--no-control: off again)
#   --settings           let the dashboard change bitcoin.conf, with backups (--no-settings: off again)
#   --auto-update        install new releases automatically, once a day (--no-auto-update: off again)
#   --yes                ask nothing; keep the defaults for what isn't given
#   --check              change nothing; print a report that is safe to share (no passwords or keys)
#   --uninstall          remove node-dash (never touches your Bitcoin data)
#
# Environment variables, put before "sh": VERSION=v0.2.0 (a specific release),
# NODEDASH_USER=bitcoin and BITCOIN_DATADIR=/path (when bitcoind can't be
# found), BITCOIN_SERVICE=name (default: bitcoind.service), and the options as
# NODEDASH_NAME="My node", NODEDASH_CONTROL=1, NODEDASH_SETTINGS=1 and
# NODEDASH_AUTO_UPDATE=1 (0 turns one off). Read the README before turning on
# node control or changing settings.

set -eu

REPO="mvanasseldonk/bitcoin-node-dash"
BIN="/usr/local/bin/node-dash"
UNIT="/etc/systemd/system/node-dash.service"
STATE_DIR="/var/lib/node-dash-install"
UNIT_HASH="$STATE_DIR/unit.sha256"
# Node control (stop / start / restart bitcoind): a polkit rule and a drop-in.
POLKIT_RULE="/etc/polkit-1/rules.d/50-node-dash.rules"
CONTROL_DROPIN="/etc/systemd/system/node-dash.service.d/control.conf"
BTC_SERVICE=${BITCOIN_SERVICE:-bitcoind.service}
# Changing settings: a drop-in that lets node-dash write bitcoin.conf, and
# keep backups of it.
SETTINGS_DROPIN="/etc/systemd/system/node-dash.service.d/settings.conf"
SETTINGS_STATE="/var/lib/node-dash-settings"
# The name of the node: a drop-in with NODEDASH_NAME.
NAME_DROPIN="/etc/systemd/system/node-dash.service.d/name.conf"
# Automatic updates: a root-owned timer runs "node-dash update" once a day.
UPDATE_SERVICE="/etc/systemd/system/node-dash-update.service"
UPDATE_TIMER="/etc/systemd/system/node-dash-update.timer"
UPDATE_STATE="/var/lib/node-dash"
INSTALL_CMD="curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | sh -s --"
# The key that signs releases (minisign.pub in the repository).
PUBKEY="RWT/RF9GWqUdJBDwNWdvqdV0U+5eFTghUZVmWognxJTlffqeKREbSObV"

say() { printf '%s\n' "› $*"; }
fail() {
	printf '%s\n' "✗ $*" >&2
	exit 1
}

# --- Preconditions -----------------------------------------------------------

# Options given as arguments win over the same options in the environment.
ACTION=install
ASK=yes
while [ $# -gt 0 ]; do
	case "$1" in
	--uninstall) ACTION=uninstall ;;
	--check) ACTION=check ;;
	-y | --yes) ASK=no ;;
	--control) NODEDASH_CONTROL=1 ;;
	--no-control) NODEDASH_CONTROL=0 ;;
	--settings) NODEDASH_SETTINGS=1 ;;
	--no-settings) NODEDASH_SETTINGS=0 ;;
	--auto-update) NODEDASH_AUTO_UPDATE=1 ;;
	--no-auto-update) NODEDASH_AUTO_UPDATE=0 ;;
	--name=*) NODEDASH_NAME=${1#--name=} ;;
	--name)
		[ $# -ge 2 ] || fail "--name needs a name, e.g. --name \"My node\""
		NODEDASH_NAME=$2
		shift
		;;
	-h | --help)
		sed -n '2,24p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//' || true
		exit 0
		;;
	*) fail "unknown option: $1 (see: sh install.sh --help)" ;;
	esac
	shift
done
# Given options, as arguments or in the environment, mean no questions.
if [ -n "${NODEDASH_CONTROL:-}${NODEDASH_SETTINGS:-}${NODEDASH_AUTO_UPDATE:-}" ] || [ -n "${NODEDASH_NAME+set}" ]; then
	ASK=no
fi

[ "$(uname -s)" = Linux ] || fail "this script supports Linux with systemd; on other systems use Docker or a release archive"
command -v systemctl >/dev/null 2>&1 || fail "systemd (systemctl) not found; use Docker or a release archive instead"
for tool in curl tar sha256sum; do
	[ "$ACTION" != check ] || break
	command -v "$tool" >/dev/null 2>&1 || fail "$tool is required but not installed"
done

if [ "$(id -u)" -eq 0 ]; then
	SUDO=""
elif command -v sudo >/dev/null 2>&1; then
	SUDO="sudo"
elif [ "$ACTION" = check ]; then
	SUDO=""
else
	fail "run this script as root, or install sudo"
fi

# --- Uninstall ---------------------------------------------------------------

if [ "$ACTION" = uninstall ]; then
	say "Stopping and removing the node-dash service (your Bitcoin data is not touched)"
	$SUDO systemctl disable --now node-dash-update.timer >/dev/null 2>&1 || true
	$SUDO systemctl disable --now node-dash >/dev/null 2>&1 || true
	$SUDO rm -f "$UNIT" "$BIN" "$POLKIT_RULE" "$CONTROL_DROPIN" "$SETTINGS_DROPIN" "$NAME_DROPIN" "$UPDATE_SERVICE" "$UPDATE_TIMER"
	$SUDO rmdir "${CONTROL_DROPIN%/*}" 2>/dev/null || true
	$SUDO rm -rf "$STATE_DIR" "$UPDATE_STATE"
	$SUDO systemctl daemon-reload
	if [ -d "$SETTINGS_STATE" ]; then
		say "Kept the backups of bitcoin.conf in $SETTINGS_STATE; remove them yourself if you no longer need them"
	fi
	if [ -e /etc/node-dash.env ]; then
		say "Left /etc/node-dash.env in place; remove it yourself if you no longer need it"
	fi
	say "node-dash is uninstalled"
	exit 0
fi

# --- Node control --------------------------------------------------------------

# configure_control writes or removes the polkit rule and the drop-in that
# enable node control, per NODEDASH_CONTROL. It sets CONTROL_CHANGED=yes when
# node-dash must restart. Without NODEDASH_CONTROL it leaves things as they are.
CONTROL_CHANGED=no
configure_control() {
	case "${NODEDASH_CONTROL:-}" in
	"") return 0 ;;
	0)
		if [ -e "$POLKIT_RULE" ] || [ -e "$CONTROL_DROPIN" ]; then
			say "Turning node control off"
			$SUDO rm -f "$POLKIT_RULE" "$CONTROL_DROPIN"
			$SUDO rmdir "${CONTROL_DROPIN%/*}" 2>/dev/null || true
			CONTROL_CHANGED=yes
		fi
		return 0
		;;
	1) ;;
	*) fail "NODEDASH_CONTROL must be 1 (on) or 0 (off), got '$NODEDASH_CONTROL'" ;;
	esac

	systemctl cat "$BTC_SERVICE" >/dev/null 2>&1 ||
		fail "node control needs Bitcoin Core as a systemd service, but '$BTC_SERVICE' doesn't exist; set BITCOIN_SERVICE to its name"
	command -v pkaction >/dev/null 2>&1 ||
		fail "node control needs polkit, which isn't installed. Install it (sudo apt install polkitd) and run this again"
	pkver=$(pkaction --version 2>/dev/null | grep -o '[0-9][0-9.]*' | head -n 1)
	case "$pkver" in
	0.[0-9] | 0.[0-9].* | 0.[0-9][0-9] | 0.[0-9][0-9].* | 0.10[0-5] | 0.10[0-5].*)
		fail "polkit $pkver is too old for node control (it needs rules in /etc/polkit-1/rules.d: polkit 0.106 or newer)"
		;;
	esac

	user=$(systemctl show -p User --value node-dash)
	[ -n "$user" ] || user=root
	say "Node control: node-dash (user $user) may start, stop and restart $BTC_SERVICE"

	cat >"$TMP/50-node-dash.rules" <<RULE
// Written by the bitcoin-node-dash installer (NODEDASH_CONTROL=1).
// Lets the dashboard start, stop and restart Bitcoin Core, and nothing else.
polkit.addRule(function (action, subject) {
  if (action.id === "org.freedesktop.systemd1.manage-units" &&
      action.lookup("unit") === "$BTC_SERVICE" &&
      ["start", "stop", "restart"].indexOf(action.lookup("verb")) >= 0 &&
      subject.user === "$user") {
    return polkit.Result.YES;
  }
});
RULE
	cat >"$TMP/control.conf" <<DROPIN
# Written by the bitcoin-node-dash installer (NODEDASH_CONTROL=1).
[Service]
Environment=NODEDASH_CONTROL=1
Environment=BITCOIN_SERVICE=$BTC_SERVICE
DROPIN
	for pair in "50-node-dash.rules:$POLKIT_RULE" "control.conf:$CONTROL_DROPIN"; do
		src=$TMP/${pair%%:*}
		dst=${pair#*:}
		if ! cmp -s "$src" "$dst" 2>/dev/null; then
			$SUDO mkdir -p "${dst%/*}"
			$SUDO install -m 0644 "$src" "$dst"
			CONTROL_CHANGED=yes
		fi
	done
}

# check_control asks polkit whether the running dashboard may restart bitcoind.
check_control() {
	[ "${NODEDASH_CONTROL:-}" = 1 ] || return 0
	pid=$(systemctl show -p MainPID --value node-dash)
	for _ in 1 2 3 4 5; do
		# polkit picks up changed rules within a moment.
		if $SUDO pkcheck --action-id org.freedesktop.systemd1.manage-units --process "$pid" \
			--detail unit "$BTC_SERVICE" --detail verb restart >/dev/null 2>&1; then
			say "Node control is on: polkit allows node-dash to control $BTC_SERVICE"
			# systemd shows it as e.g. "1min 30s" or "infinity".
			stop_timeout=$(systemctl show -p TimeoutStopUSec --value "$BTC_SERVICE")
			secs=$(printf '%s\n' "$stop_timeout" | awk '{
				t = 0
				for (i = 1; i <= NF; i++) {
					v = $i
					if (v ~ /min$/) { sub(/min$/, "", v); t += v * 60 }
					else if (v ~ /ms$|us$/) { }
					else if (v ~ /h$/) { sub(/h$/, "", v); t += v * 3600 }
					else if (v ~ /d$/) { sub(/d$/, "", v); t += v * 86400 }
					else if (v ~ /s$/) { sub(/s$/, "", v); t += v }
				}
				print int(t)
			}')
			if [ "$stop_timeout" != infinity ] && [ "$secs" -lt 300 ]; then
				say "Note: systemd force-stops $BTC_SERVICE after $stop_timeout, which can be too short for a clean"
				say "shutdown. To allow 10 minutes, run: sudo systemctl edit $BTC_SERVICE, and add:"
				say "  [Service]"
				say "  TimeoutStopSec=600"
			fi
			return 0
		fi
		sleep 1
	done
	fail "polkit doesn't allow node-dash to control $BTC_SERVICE; check $POLKIT_RULE and: journalctl -u polkit -n 20"
}

# --- Finding bitcoind --------------------------------------------------------------

# arg_value prints the value of -name=value or --name=value among the words of $2.
arg_value() {
	printf '%s\n' "$2" | tr ' ' '\n' | sed -n "s/^--\{0,1\}$1=//p" | head -n 1
}

# detect_bitcoind sets BTC_USER, BTC_DATADIR and BTC_CONF from a running
# bitcoind, or else from bitcoind.service. Values may stay empty.
detect_bitcoind() {
	BTC_USER="" BTC_DATADIR="" BTC_CONF=""
	line=$(ps -eo uid=,args= | awk '$2 ~ /(^|\/)bitcoind$/ { print; exit }')
	if [ -n "$line" ]; then
		uid=$(printf '%s\n' "$line" | awk '{ print $1 }')
		BTC_USER=$(getent passwd "$uid" | cut -d: -f1)
		args=$line
	elif systemctl cat bitcoind.service >/dev/null 2>&1; then
		BTC_USER=$(systemctl show -p User --value bitcoind.service)
		[ -n "$BTC_USER" ] || BTC_USER=root
		args=$(systemctl show -p ExecStart --value bitcoind.service)
	else
		return 0
	fi
	BTC_DATADIR=$(arg_value datadir "$args")
	BTC_CONF=$(arg_value conf "$args")
}

# --- Changing settings -----------------------------------------------------------

# configure_settings writes or removes the drop-in that lets node-dash write
# bitcoin.conf, per NODEDASH_SETTINGS (decision 0003). It never changes an
# owner or a mode. It sets SETTINGS_CHANGED=yes when node-dash must restart.
SETTINGS_CHANGED=no
configure_settings() {
	case "${NODEDASH_SETTINGS:-}" in
	"") return 0 ;;
	0)
		if [ -e "$SETTINGS_DROPIN" ]; then
			say "Turning changing settings off (the backups stay in $SETTINGS_STATE)"
			$SUDO rm -f "$SETTINGS_DROPIN"
			$SUDO rmdir "${SETTINGS_DROPIN%/*}" 2>/dev/null || true
			SETTINGS_CHANGED=yes
		fi
		return 0
		;;
	1) ;;
	*) fail "NODEDASH_SETTINGS must be 1 (on) or 0 (off), got '$NODEDASH_SETTINGS'" ;;
	esac

	user=$(systemctl show -p User --value node-dash)
	[ -n "$user" ] || user=root
	env=$(systemctl show -p Environment --value node-dash | tr ' ' '\n')
	datadir=$(printf '%s\n' "$env" | sed -n 's/^BITCOIN_DATADIR=//p' | head -n 1)
	conf=$(printf '%s\n' "$env" | sed -n 's/^BITCOIN_CONF=//p' | head -n 1)
	[ -n "$conf" ] || conf=$datadir/bitcoin.conf
	# A -conf on bitcoind's command line wins, as in the dashboard.
	detect_bitcoind
	if [ -n "$BTC_CONF" ]; then
		case "$BTC_CONF" in
		/*) conf=$BTC_CONF ;;
		*) conf=${BTC_DATADIR:-$datadir}/$BTC_CONF ;;
		esac
	fi
	case "$conf" in
	/*) ;;
	*) fail "couldn't determine where bitcoin.conf is; set BITCOIN_CONF in $UNIT" ;;
	esac
	$SUDO test -f "$conf" ||
		fail "changing settings needs $conf, which doesn't exist. Create it (an empty file is fine) and run this again"
	real=$($SUDO readlink -f "$conf")
	if ! $SUDO runuser -u "$user" -- test -w "$real"; then
		owner=$($SUDO stat -c %U "$real")
		fail "node-dash runs as $user, who can't write $real (owned by $owner). The installer doesn't change owners or modes; see the README"
	fi
	say "Changing settings: node-dash (user $user) may write $real; backups go to $SETTINGS_STATE/backups"

	cat >"$TMP/settings.conf" <<DROPIN
# Written by the bitcoin-node-dash installer (NODEDASH_SETTINGS=1).
# Lets the dashboard write bitcoin.conf, that one file, and keep backups of it.
[Service]
Environment=NODEDASH_SETTINGS=1
ReadWritePaths=$real
StateDirectory=node-dash-settings
StateDirectoryMode=0700
DROPIN
	if ! cmp -s "$TMP/settings.conf" "$SETTINGS_DROPIN" 2>/dev/null; then
		$SUDO mkdir -p "${SETTINGS_DROPIN%/*}"
		$SUDO install -m 0644 "$TMP/settings.conf" "$SETTINGS_DROPIN"
		SETTINGS_CHANGED=yes
	fi
}

# check_settings asks the running dashboard whether it can write bitcoin.conf.
check_settings() {
	[ "${NODEDASH_SETTINGS:-}" = 1 ] || return 0
	code=$(curl -s -o "$TMP/settings.json" -w '%{http_code}' "http://127.0.0.1:$PORT/api/settings" 2>/dev/null || true)
	case "$code" in
	200)
		if grep -q '"error":' "$TMP/settings.json"; then
			fail "node-dash can't save bitcoin.conf: $(sed -n 's/.*"error":"\([^"]*\)".*/\1/p' "$TMP/settings.json")"
		fi
		say "Changing settings is on: Details → Node settings → Change settings"
		;;
	404) say "Note: node-dash $VERSION can't change settings yet; it will once updated" ;;
	*) fail "changing settings didn't turn on; see: journalctl -u node-dash -n 20 --no-pager" ;;
	esac
}

# --- Name ---------------------------------------------------------------------------

# The longest name the dashboard takes (maxNameLen in cmd/node-dash).
MAX_NAME=40

# name_problem prints why name $1 can't be used, or nothing when it can.
name_problem() {
	# Characters, not bytes: UTF-8 continuation bytes don't count.
	n=$(($(printf '%s' "$1" | LC_ALL=C tr -d '\200-\277' | wc -c)))
	if [ "$n" -gt "$MAX_NAME" ]; then
		printf '%s\n' "the name is $n characters long; at most $MAX_NAME fit"
	elif [ "$(printf '%s' "$1" | LC_ALL=C tr -d '[:cntrl:]')" != "$1" ]; then
		printf '%s\n' "the name contains a control character, such as a tab"
	fi
}

# current_name prints the name in the installer's drop-in; nothing when there is none.
current_name() {
	[ -e "$NAME_DROPIN" ] || return 0
	sed -n 's/^Environment="NODEDASH_NAME=\(.*\)"$/\1/p' "$NAME_DROPIN" |
		sed -e 's/\\\(.\)/\1/g' -e 's/%%/%/g'
}

# configure_name writes or removes the drop-in with the node's name, per
# NODEDASH_NAME; an empty name removes it. It sets NAME_CHANGED=yes when
# node-dash must restart. Without NODEDASH_NAME it leaves things as they are.
NAME_CHANGED=no
configure_name() {
	[ -n "${NODEDASH_NAME+set}" ] || return 0
	if [ -z "$NODEDASH_NAME" ]; then
		if [ -e "$NAME_DROPIN" ]; then
			say "Using the default name again"
			$SUDO rm -f "$NAME_DROPIN"
			$SUDO rmdir "${NAME_DROPIN%/*}" 2>/dev/null || true
			NAME_CHANGED=yes
		fi
		return 0
	fi
	problem=$(name_problem "$NODEDASH_NAME")
	[ -z "$problem" ] || fail "can't use that name: $problem"
	# A quoted systemd value: backslash and double quote escaped, % doubled.
	escaped=$(printf '%s' "$NODEDASH_NAME" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/%/%%/g')
	cat >"$TMP/name.conf" <<DROPIN
# Written by the bitcoin-node-dash installer (NODEDASH_NAME).
[Service]
Environment="NODEDASH_NAME=$escaped"
DROPIN
	if ! cmp -s "$TMP/name.conf" "$NAME_DROPIN" 2>/dev/null; then
		say "Name: $NODEDASH_NAME"
		$SUDO mkdir -p "${NAME_DROPIN%/*}"
		$SUDO install -m 0644 "$TMP/name.conf" "$NAME_DROPIN"
		NAME_CHANGED=yes
	fi
}

# --- Automatic updates -----------------------------------------------------------

# needs_updater fails when NODEDASH_AUTO_UPDATE=1 and binary $1 can't update
# itself: releases before v0.7.0.
needs_updater() {
	[ "${NODEDASH_AUTO_UPDATE:-}" = 1 ] || return 0
	"$1" update -h >/dev/null 2>&1 ||
		fail "automatic updates need node-dash v0.7.0 or newer; run this without VERSION= to install the latest release"
}

# configure_auto_update writes or removes the timer and service that update
# node-dash, per NODEDASH_AUTO_UPDATE. It sets AUTO_CHANGED=yes when systemd
# must reload. Without NODEDASH_AUTO_UPDATE it leaves things as they are.
AUTO_CHANGED=no
configure_auto_update() {
	case "${NODEDASH_AUTO_UPDATE:-}" in
	"") return 0 ;;
	0)
		if [ -e "$UPDATE_TIMER" ] || [ -e "$UPDATE_SERVICE" ]; then
			say "Turning automatic updates off"
			$SUDO systemctl disable --now node-dash-update.timer >/dev/null 2>&1 || true
			$SUDO rm -f "$UPDATE_TIMER" "$UPDATE_SERVICE"
			$SUDO rm -rf "$UPDATE_STATE"
			AUTO_CHANGED=yes
		fi
		return 0
		;;
	1) ;;
	*) fail "NODEDASH_AUTO_UPDATE must be 1 (on) or 0 (off), got '$NODEDASH_AUTO_UPDATE'" ;;
	esac

	cat >"$TMP/node-dash-update.service" <<'SERVICE'
# Written by the bitcoin-node-dash installer (NODEDASH_AUTO_UPDATE=1).
# Installs the latest node-dash release of the same major version. It checks
# the release's signature, keeps the previous binary, and puts it back when the
# new version doesn't start. Root, because it replaces /usr/local/bin/node-dash.
[Unit]
Description=Update node-dash to the latest release
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/node-dash update
StateDirectory=node-dash
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/usr/local/bin
ProtectHome=true
NoNewPrivileges=true
SERVICE
	cat >"$TMP/node-dash-update.timer" <<'TIMER'
# Written by the bitcoin-node-dash installer (NODEDASH_AUTO_UPDATE=1).
# Once a day, at a random time that stays the same on this server.
[Unit]
Description=Check daily for a new node-dash release

[Timer]
OnCalendar=daily
RandomizedDelaySec=1d
FixedRandomDelay=true
Persistent=true

[Install]
WantedBy=timers.target
TIMER
	for pair in "node-dash-update.service:$UPDATE_SERVICE" "node-dash-update.timer:$UPDATE_TIMER"; do
		src=$TMP/${pair%%:*}
		dst=${pair#*:}
		if ! cmp -s "$src" "$dst" 2>/dev/null; then
			$SUDO install -m 0644 "$src" "$dst"
			AUTO_CHANGED=yes
		fi
	done
}

# start_auto_update turns the timer on and checks for an update right away,
# so the dashboard shows the result.
start_auto_update() {
	[ "${NODEDASH_AUTO_UPDATE:-}" = 1 ] || return 0
	$SUDO systemctl enable --now node-dash-update.timer >/dev/null 2>&1
	say "Automatic updates are on: once a day, at a random time. Checking now…"
	if $SUDO systemctl start node-dash-update.service; then
		result=$(sed -n 's/^ *"result": *"\([a-z]*\)".*/\1/p' "$UPDATE_STATE/update.json" 2>/dev/null)
		say "First check: ${result:-done}. Logs: journalctl -u node-dash-update"
	else
		say "The first check failed; see: journalctl -u node-dash-update -n 20 --no-pager"
	fi
}

# verify_signature checks checksums.txt against its minisign signature: with
# the installed node-dash when it can, else with minisign when installed.
verify_signature() {
	if ! curl -fsSL -o "$TMP/checksums.txt.minisig" "$BASE/checksums.txt.minisig" 2>/dev/null; then
		say "Note: $VERSION isn't signed (releases before v0.7.0 aren't); checked against checksums.txt only"
		return 0
	fi
	if [ -x "$BIN" ] && "$BIN" verify -h >/dev/null 2>&1; then
		"$BIN" verify "$TMP/checksums.txt" "$TMP/checksums.txt.minisig" "$VERSION" >/dev/null ||
			fail "the signature of $VERSION doesn't check out; nothing was changed"
		say "Signature checked with the installed node-dash"
	elif command -v minisign >/dev/null 2>&1; then
		if ! out=$(minisign -V -P "$PUBKEY" -m "$TMP/checksums.txt" -x "$TMP/checksums.txt.minisig" 2>&1) ||
			! printf '%s\n' "$out" | grep -qx "Trusted comment: node-dash $VERSION"; then
			fail "the signature of $VERSION doesn't check out; nothing was changed"
		fi
		say "Signature checked with minisign"
	else
		say "Note: checked against checksums.txt only. Install minisign to check the signature too;"
		say "once installed, node-dash checks the signature of every update itself."
	fi
}

# wait_healthy waits until the dashboard answers, and sets PORT.
wait_healthy() {
	listen=$(systemctl show -p Environment --value node-dash | tr ' ' '\n' | sed -n 's/^NODEDASH_LISTEN=//p' | head -n 1)
	PORT=${listen##*:}
	PORT=${PORT:-8080}
	for _ in 1 2 3 4 5 6 7 8 9 10; do
		curl -fsS -o /dev/null "http://127.0.0.1:$PORT/healthz" 2>/dev/null && return 0
		sleep 1
	done
	fail "node-dash didn't start; see: journalctl -u node-dash -n 20 --no-pager"
}

# --- Options summary and --check ---------------------------------------------------

# is_on prints "on" when file $1 exists, else "off".
is_on() {
	if [ -e "$1" ]; then echo on; else echo off; fi
}

# options_report prints which options are on.
options_report() {
	name=$(current_name)
	say "  name               ${name:-Node (the default)}"
	say "  node control       $(is_on "$CONTROL_DROPIN")"
	say "  changing settings  $(is_on "$SETTINGS_DROPIN")"
	say "  automatic updates  $(is_on "$UPDATE_TIMER")"
}

# summary lists the options at the end of an install, and how to change them.
summary() {
	say "Options:"
	options_report
	say "To change one, run the installer again with it, for example:"
	say "  $INSTALL_CMD --control"
	say "Others: --no-control, --settings, --auto-update, --name \"My node\" (all: --help)"
}

# as_root runs a command as root when that needs no password; else it fails.
as_root() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
	elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
		sudo -n "$@"
	else
		return 1
	fi
}

# rpc_port prints Bitcoin Core's RPC port, from its arguments $1 and the chain
# and rpcport lines of bitcoin.conf $2. Nothing else of either is used.
rpc_port() {
	rport=$(arg_value rpcport "$1")
	rchain=$(arg_value chain "$1")
	for flag in testnet4 testnet signet regtest; do
		case " $1 " in *" -$flag "* | *" --$flag "* | *" -$flag=1 "*) rchain=${rchain:-$flag} ;; esac
	done
	if [ -n "$2" ]; then
		lines=$(as_root grep -E '^(chain|testnet4|testnet|signet|regtest|rpcport)=' "$2" 2>/dev/null ||
			grep -E '^(chain|testnet4|testnet|signet|regtest|rpcport)=' "$2" 2>/dev/null || true)
		[ -n "$rport" ] || rport=$(printf '%s\n' "$lines" | sed -n 's/^rpcport=//p' | head -n 1)
		[ -n "$rchain" ] || rchain=$(printf '%s\n' "$lines" | sed -n 's/^chain=//p' | head -n 1)
		for flag in testnet4 testnet signet regtest; do
			if printf '%s\n' "$lines" | grep -qx "$flag=1"; then rchain=${rchain:-$flag}; fi
		done
	fi
	if [ -z "$rport" ]; then
		case "$rchain" in
		test | testnet) rport=18332 ;;
		testnet4) rport=48332 ;;
		signet) rport=38332 ;;
		regtest) rport=18443 ;;
		*) rport=8332 ;;
		esac
	fi
	printf '%s %s\n' "$rport" "${rchain:-main}"
}

# run_check prints what the installer and an assistant need to know, changing
# nothing. It never prints bitcoind's arguments or lines of bitcoin.conf: they
# can hold rpcpassword.
run_check() {
	say "node-dash check: changes nothing, and shows no passwords or keys. Safe to share."
	# shellcheck disable=SC1091
	os=$(. /etc/os-release 2>/dev/null && printf '%s' "${PRETTY_NAME:-}")
	say "System:        ${os:-Linux}, $(uname -m), $(systemctl --version | head -n 1)"

	detect_bitcoind
	args=$(ps -eo args= | awk '$1 ~ /(^|\/)bitcoind$/ { print; exit }')
	if [ -n "$args" ]; then
		running="running"
	else
		running="not running"
		args=$(systemctl show -p ExecStart --value "$BTC_SERVICE" 2>/dev/null || true)
	fi
	service=$(systemctl is-active "$BTC_SERVICE" 2>/dev/null || true)
	if systemctl cat "$BTC_SERVICE" >/dev/null 2>&1; then
		service="service $BTC_SERVICE is $service"
	else
		service="no service $BTC_SERVICE"
	fi
	say "Bitcoin Core:  $running, $service"
	if [ -n "$BTC_USER" ]; then
		home=$(getent passwd "$BTC_USER" | cut -d: -f6)
		datadir=${BTC_DATADIR:-$home/.bitcoin}
		say "  user          $BTC_USER"
		say "  data dir      $datadir"
		case "$BTC_CONF" in
		"") conf=$datadir/bitcoin.conf ;;
		/*) conf=$BTC_CONF ;;
		*) conf=$datadir/$BTC_CONF ;;
		esac
		info=$(as_root stat -c '%U %a' "$conf" 2>/dev/null || stat -c '%U %a' "$conf" 2>/dev/null || true)
		if [ -z "$info" ]; then
			say "  bitcoin.conf  $conf: doesn't exist, or can't be seen without sudo"
		else
			say "  bitcoin.conf  $conf, owner ${info% *}, mode ${info#* }"
			case "${info#* }" in
			*[4567]) say "                readable by every user on this server; chmod 600 is safer" ;;
			esac
		fi
		rpc=$(rpc_port "$args" "$conf")
		code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:${rpc% *}/" 2>/dev/null || true)
		case "$code" in
		000 | "") say "  RPC           no answer on 127.0.0.1:${rpc% *} (chain: ${rpc#* })" ;;
		*) say "  RPC           answers on 127.0.0.1:${rpc% *} (chain: ${rpc#* })" ;;
		esac
	else
		say "  can't find bitcoind: not running, and no $BTC_SERVICE"
	fi

	if [ -x "$BIN" ]; then
		say "Dashboard:     $("$BIN" version 2>/dev/null || echo "unknown version") in $BIN"
	else
		say "Dashboard:     not installed"
	fi
	if [ -e "$UNIT" ]; then
		if [ -e "$UNIT_HASH" ] && [ "$(sha256sum <"$UNIT" | cut -d' ' -f1)" = "$(cat "$UNIT_HASH")" ]; then
			how="written by the installer"
		else
			how="changed or installed by hand"
		fi
		state=$(systemctl is-active node-dash 2>/dev/null || true)
		say "  service       $state, $(systemctl is-enabled node-dash 2>/dev/null || true) at boot; $how"
		say "  runs as       $(systemctl show -p User --value node-dash)"
		listen=$(systemctl show -p Environment --value node-dash | tr ' ' '\n' | sed -n 's/^NODEDASH_LISTEN=//p' | head -n 1)
		port=${listen##*:}
		port=${port:-8080}
		if curl -fsS -o /dev/null --max-time 3 "http://127.0.0.1:$port/healthz" 2>/dev/null; then
			say "  answers       yes, on port $port"
		else
			say "  answers       no, not on port $port"
		fi
		ip=$(hostname -I 2>/dev/null | awk '{ print $1 }')
		say "  address       http://${ip:-<this-server>}:$port"
		systemctl show -p Environment --value node-dash | grep -q 'BITCOIN_RPC_URL=' &&
			say "  RPC address   set with BITCOIN_RPC_URL"
		say "Options:"
		options_report
		if [ "$state" != active ]; then
			if as_root true; then
				say "Last log lines of node-dash:"
				as_root journalctl -u node-dash -n 10 --no-pager -o cat 2>/dev/null | sed 's/^/    /'
			else
				say "Log lines: run this with sudo to include them"
			fi
		fi
	else
		say "  no service $UNIT"
	fi

	if command -v ufw >/dev/null 2>&1; then
		if ufw=$(as_root ufw status 2>/dev/null); then
			if printf '%s\n' "$ufw" | grep -q '^Status: active'; then
				if printf '%s\n' "$ufw" | grep -q "^${port:-8080}[/ ]"; then
					say "Firewall:      ufw is active, with a rule for port ${port:-8080}"
				else
					say "Firewall:      ufw is active, without a rule for port ${port:-8080}"
				fi
			else
				say "Firewall:      ufw is installed, not active"
			fi
		else
			say "Firewall:      ufw is installed; its status needs sudo"
		fi
	else
		say "Firewall:      no ufw"
	fi
	latest=$(curl -fsSLI -o /dev/null --max-time 5 -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null || true)
	say "Latest:        ${latest##*/}"
}

if [ "$ACTION" = check ]; then
	run_check
	exit 0
fi

# --- What to install ---------------------------------------------------------

case "$(uname -m)" in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*) fail "unsupported CPU architecture: $(uname -m) (supported: x86-64, ARM64)" ;;
esac

if [ -z "${VERSION:-}" ]; then
	latest_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
		fail "could not look up the latest release on GitHub"
	VERSION=${latest_url##*/}
fi
case "$VERSION" in
v*) ;;
*) fail "could not determine the version to install (got '$VERSION'); set VERSION=v0.2.0 to choose one" ;;
esac

CURRENT=""
if [ -x "$BIN" ]; then
	CURRENT=$("$BIN" version 2>/dev/null || true)
fi
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

if [ "$CURRENT" = "$VERSION" ] && [ -e "$UNIT" ]; then
	needs_updater "$BIN"
	configure_name
	configure_control
	configure_settings
	configure_auto_update
	if [ "$NAME_CHANGED" = no ] && [ "$CONTROL_CHANGED" = no ] && [ "$SETTINGS_CHANGED" = no ] && [ "$AUTO_CHANGED" = no ]; then
		say "node-dash $VERSION is already installed and up to date; nothing to do"
		summary
		exit 0
	fi
	$SUDO systemctl daemon-reload
	if [ "$NAME_CHANGED" = yes ] || [ "$CONTROL_CHANGED" = yes ] || [ "$SETTINGS_CHANGED" = yes ]; then
		$SUDO systemctl restart node-dash
		wait_healthy
		check_control
		check_settings
	fi
	[ "$AUTO_CHANGED" = no ] || start_auto_update
	say "node-dash $VERSION is running"
	summary
	exit 0
fi

# --- Download and verify -----------------------------------------------------
NAME="node-dash_${VERSION}_linux_${ARCH}"
BASE="https://github.com/$REPO/releases/download/$VERSION"

say "Downloading node-dash $VERSION ($ARCH)"
curl -fsSL -o "$TMP/$NAME.tar.gz" "$BASE/$NAME.tar.gz" || fail "download failed: $BASE/$NAME.tar.gz"
curl -fsSL -o "$TMP/checksums.txt" "$BASE/checksums.txt" || fail "download failed: $BASE/checksums.txt"
(
	cd "$TMP"
	grep " $NAME.tar.gz\$" checksums.txt | sha256sum -c - >/dev/null 2>&1
) || fail "checksum mismatch for $NAME.tar.gz; nothing was changed"
verify_signature
tar -xzf "$TMP/$NAME.tar.gz" -C "$TMP"
[ -x "$TMP/$NAME/node-dash" ] || fail "the archive doesn't contain node-dash; nothing was changed"

# --- Service settings ----------------------------------------------------------

# unit_setting prints the value of "Key=" or "Environment=KEY=" in the installed unit.
unit_setting() {
	sed -n "s/^$1=//p" "$UNIT" | head -n 1
}

MANAGED=no
if [ -e "$UNIT" ] && [ -e "$UNIT_HASH" ] && [ "$(sha256sum <"$UNIT" | cut -d' ' -f1)" = "$(cat "$UNIT_HASH")" ]; then
	MANAGED=yes
fi

WRITE_UNIT=yes
if [ -e "$UNIT" ] && [ "$MANAGED" = no ]; then
	# Installed by hand, or changed since: leave it exactly as it is.
	WRITE_UNIT=no
	SVC_USER=$(unit_setting User)
	SVC_DATADIR=$(unit_setting Environment=BITCOIN_DATADIR)
	SVC_CONF=""
elif [ -e "$UNIT" ]; then
	# Written by this script and unchanged: refresh it, keeping its settings.
	SVC_USER=$(unit_setting User)
	SVC_DATADIR=$(unit_setting Environment=BITCOIN_DATADIR)
	SVC_CONF=$(unit_setting Environment=BITCOIN_CONF)
else
	detect_bitcoind
	SVC_USER=${NODEDASH_USER:-$BTC_USER}
	[ -n "$SVC_USER" ] || fail "couldn't find bitcoind (not running, no bitcoind.service). Set NODEDASH_USER and BITCOIN_DATADIR, e.g.:
  curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | NODEDASH_USER=bitcoin BITCOIN_DATADIR=/home/bitcoin/.bitcoin sh"
	getent passwd "$SVC_USER" >/dev/null || fail "user '$SVC_USER' does not exist"
	home=$(getent passwd "$SVC_USER" | cut -d: -f6)
	SVC_DATADIR=${BITCOIN_DATADIR:-${BTC_DATADIR:-$home/.bitcoin}}
	SVC_CONF=""
	if [ -n "$BTC_CONF" ]; then
		case "$BTC_CONF" in
		/*) ;;
		*) BTC_CONF="$SVC_DATADIR/$BTC_CONF" ;; # relative to the data directory, like Bitcoin Core
		esac
		[ "$BTC_CONF" = "$SVC_DATADIR/bitcoin.conf" ] || SVC_CONF=$BTC_CONF
	fi
fi

if [ "$WRITE_UNIT" = yes ]; then
	if [ -z "$SVC_USER" ] || [ -z "$SVC_DATADIR" ]; then
		fail "couldn't determine the service user and data directory"
	fi
	SVC_GROUP=$(id -gn "$SVC_USER")
	sed -e "s|^User=.*|User=$SVC_USER|" \
		-e "s|^Group=.*|Group=$SVC_GROUP|" \
		-e "s|^Environment=BITCOIN_DATADIR=.*|Environment=BITCOIN_DATADIR=$SVC_DATADIR|" \
		"$TMP/$NAME/node-dash.service" >"$TMP/node-dash.service"
	if [ -n "$SVC_CONF" ]; then
		sed -i "s|^Environment=BITCOIN_DATADIR=.*|&\nEnvironment=BITCOIN_CONF=$SVC_CONF|" "$TMP/node-dash.service"
	fi
fi

# --- Questions -------------------------------------------------------------------

# ask prints question $1 on the terminal and sets ANSWER to the reply, or to
# default $2 when the reply is empty.
ask() {
	printf '%s ' "$1" >/dev/tty
	IFS= read -r ANSWER </dev/tty || ANSWER=""
	[ -n "$ANSWER" ] || ANSWER=$2
}

# ask_yes_no asks question $1 until the reply is yes or no; the default is no.
# It sets the variable named $2 to 1 or 0.
ask_yes_no() {
	while :; do
		ask "$1 [y/N]" n
		case "$ANSWER" in
		y | Y | yes | Yes | YES) eval "$2=1" && return 0 ;;
		n | N | no | No | NO) eval "$2=0" && return 0 ;;
		esac
		printf '%s\n' "  Please answer y or n." >/dev/tty
	done
}

# ask_options asks about the options on a first install in a terminal. Not
# with options given, with --yes, without a terminal (CI, an assistant running
# commands), or on an update.
ask_options() {
	if [ "$ASK" = no ] || [ -e "$UNIT" ] || ! (: </dev/tty) 2>/dev/null; then
		return 0
	fi
	{
		echo
		echo "A few questions; Enter keeps the default. Change any of it later by running"
		echo "this installer again with an option, e.g. --control (see --help)."
		echo
	} >/dev/tty
	while :; do
		ask "Name for this node, shown in the dashboard [Node]:" ""
		problem=$(name_problem "$ANSWER")
		[ -n "$problem" ] || break
		printf '%s\n' "  Can't use that: $problem." >/dev/tty
	done
	[ -z "$ANSWER" ] || NODEDASH_NAME=$ANSWER

	echo "Node control: stop, start and restart Bitcoin Core from the dashboard." >/dev/tty
	echo "  Anyone on your network can then do that; the dashboard has no login." >/dev/tty
	if ! systemctl cat "$BTC_SERVICE" >/dev/null 2>&1; then
		echo "  Not possible here: Bitcoin Core doesn't run as the systemd service $BTC_SERVICE." >/dev/tty
	elif ! command -v pkaction >/dev/null 2>&1; then
		echo "  Needs polkit first: sudo apt install polkitd. Then run this again with --control." >/dev/tty
	else
		ask_yes_no "  Turn it on?" NODEDASH_CONTROL
	fi

	echo "Changing settings: change 8 settings of bitcoin.conf, such as dbcache, from the" >/dev/tty
	echo "  dashboard. A backup is made first; anyone on your network can do it." >/dev/tty
	conf=${SVC_CONF:-$SVC_DATADIR/bitcoin.conf}
	if ! $SUDO test -f "$conf"; then
		echo "  Not possible yet: $conf doesn't exist. Create it (empty is fine)," >/dev/tty
		echo "  then run this again with --settings." >/dev/tty
	elif ! $SUDO runuser -u "$SVC_USER" -- test -w "$($SUDO readlink -f "$conf")"; then
		echo "  Not possible here: node-dash runs as $SVC_USER, who can't write $conf (see the README)." >/dev/tty
	else
		ask_yes_no "  Turn it on?" NODEDASH_SETTINGS
	fi

	echo "Automatic updates: install new node-dash versions by itself, once a day." >/dev/tty
	ask_yes_no "  Turn it on?" NODEDASH_AUTO_UPDATE
	echo >/dev/tty
}

ask_options
needs_updater "$TMP/$NAME/node-dash"

# --- Install -------------------------------------------------------------------

if [ -n "$CURRENT" ]; then
	say "Updating node-dash $CURRENT → $VERSION"
else
	say "Installing node-dash $VERSION"
fi
say "  runs as user:   ${SVC_USER:-(see $UNIT)}"
say "  data directory: ${SVC_DATADIR:-(see $UNIT)}"
[ -z "${SVC_CONF:-}" ] || say "  bitcoin.conf:   $SVC_CONF"

$SUDO install -m 0755 "$TMP/$NAME/node-dash" "$BIN"
if [ "$WRITE_UNIT" = yes ]; then
	$SUDO install -m 0644 "$TMP/node-dash.service" "$UNIT"
	$SUDO mkdir -p "$STATE_DIR"
	sha256sum <"$TMP/node-dash.service" | cut -d' ' -f1 | $SUDO tee "$UNIT_HASH" >/dev/null
else
	say "Kept $UNIT as it is, because it was changed or installed by hand"
fi
$SUDO systemctl daemon-reload
configure_name
configure_control
configure_settings
configure_auto_update
if [ "$NAME_CHANGED" = yes ] || [ "$CONTROL_CHANGED" = yes ] || [ "$SETTINGS_CHANGED" = yes ] || [ "$AUTO_CHANGED" = yes ]; then
	$SUDO systemctl daemon-reload
fi
$SUDO systemctl enable node-dash >/dev/null 2>&1
$SUDO systemctl restart node-dash

# --- Check and tell where to find it -------------------------------------------

wait_healthy
check_control
check_settings
[ "$AUTO_CHANGED" = no ] || start_auto_update

IP=$(hostname -I 2>/dev/null | awk '{ print $1 }')
say "node-dash $VERSION is running: http://${IP:-<this-server>}:$PORT"
say "Logs: journalctl -u node-dash -f"

if command -v ufw >/dev/null 2>&1 && $SUDO ufw status 2>/dev/null | grep -q '^Status: active'; then
	if ! $SUDO ufw status 2>/dev/null | grep -q "^${PORT}[/ ]"; then
		say "The ufw firewall is active and has no rule for port $PORT yet. To open it for your network:"
		say "  sudo ufw allow from ${IP%.*}.0/24 to any port $PORT proto tcp"
	fi
fi
say "Not reachable from another computer? Also check other firewalls, such as your router or hypervisor."
summary

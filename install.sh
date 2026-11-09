#!/bin/sh
# Install or update bitcoin-node-dash as a systemd service on Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh
#
# Run it again to update. Options (environment variables):
#   VERSION=v0.2.0        install this release instead of the latest
#   NODEDASH_USER=bitcoin run the dashboard as this user (default: the user that runs bitcoind)
#   BITCOIN_DATADIR=/path Bitcoin data directory (default: detected, or ~/.bitcoin of that user)
#   NODEDASH_CONTROL=1    let the dashboard stop, start and restart bitcoind (polkit rule);
#                         NODEDASH_CONTROL=0 turns it off again (read the README first)
#   BITCOIN_SERVICE=name  the systemd service of Bitcoin Core (default: bitcoind.service)
#
# Uninstall (never touches your Bitcoin data):
#   curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --uninstall

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

say() { printf '%s\n' "› $*"; }
fail() {
	printf '%s\n' "✗ $*" >&2
	exit 1
}

# --- Preconditions -----------------------------------------------------------

case "${1:-}" in
"") ACTION=install ;;
--uninstall) ACTION=uninstall ;;
-h | --help)
	sed -n '2,17p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//' || true
	exit 0
	;;
*) fail "unknown option: $1 (use --uninstall, or no option to install or update)" ;;
esac

[ "$(uname -s)" = Linux ] || fail "this script supports Linux with systemd; on other systems use Docker or a release archive"
command -v systemctl >/dev/null 2>&1 || fail "systemd (systemctl) not found; use Docker or a release archive instead"
for tool in curl tar sha256sum; do
	command -v "$tool" >/dev/null 2>&1 || fail "$tool is required but not installed"
done

if [ "$(id -u)" -eq 0 ]; then
	SUDO=""
elif command -v sudo >/dev/null 2>&1; then
	SUDO="sudo"
else
	fail "run this script as root, or install sudo"
fi

# --- Uninstall ---------------------------------------------------------------

if [ "$ACTION" = uninstall ]; then
	say "Stopping and removing the node-dash service (your Bitcoin data is not touched)"
	$SUDO systemctl disable --now node-dash >/dev/null 2>&1 || true
	$SUDO rm -f "$UNIT" "$BIN" "$POLKIT_RULE" "$CONTROL_DROPIN"
	$SUDO rmdir "${CONTROL_DROPIN%/*}" 2>/dev/null || true
	$SUDO rm -rf "$STATE_DIR"
	$SUDO systemctl daemon-reload
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
	configure_control
	if [ "$CONTROL_CHANGED" = no ]; then
		say "node-dash $VERSION is already installed and up to date; nothing to do"
		exit 0
	fi
	$SUDO systemctl daemon-reload
	$SUDO systemctl restart node-dash
	wait_healthy
	check_control
	say "node-dash $VERSION is running"
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
tar -xzf "$TMP/$NAME.tar.gz" -C "$TMP"
[ -x "$TMP/$NAME/node-dash" ] || fail "the archive doesn't contain node-dash; nothing was changed"

# --- Service settings ----------------------------------------------------------

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
configure_control
[ "$CONTROL_CHANGED" = no ] || $SUDO systemctl daemon-reload
$SUDO systemctl enable node-dash >/dev/null 2>&1
$SUDO systemctl restart node-dash

# --- Check and tell where to find it -------------------------------------------

wait_healthy
check_control

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

#!/bin/sh
# Install or update bitcoin-node-dash as a systemd service on Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh
#
# Run it again to update. Options (environment variables):
#   VERSION=v0.2.0        install this release instead of the latest
#   NODEDASH_USER=bitcoin run the dashboard as this user (default: the user that runs bitcoind)
#   BITCOIN_DATADIR=/path Bitcoin data directory (default: detected, or ~/.bitcoin of that user)
#
# Uninstall (never touches your Bitcoin data):
#   curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s -- --uninstall

set -eu

REPO="mvanasseldonk/bitcoin-node-dash"
BIN="/usr/local/bin/node-dash"
UNIT="/etc/systemd/system/node-dash.service"
STATE_DIR="/var/lib/node-dash-install"
UNIT_HASH="$STATE_DIR/unit.sha256"

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
	sed -n '2,13p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//' || true
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
	$SUDO rm -f "$UNIT" "$BIN"
	$SUDO rm -rf "$STATE_DIR"
	$SUDO systemctl daemon-reload
	if [ -e /etc/node-dash.env ]; then
		say "Left /etc/node-dash.env in place; remove it yourself if you no longer need it"
	fi
	say "node-dash is uninstalled"
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
if [ "$CURRENT" = "$VERSION" ] && [ -e "$UNIT" ]; then
	say "node-dash $VERSION is already installed and up to date; nothing to do"
	exit 0
fi

# --- Download and verify -----------------------------------------------------

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM
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
	[ -n "$SVC_USER" ] && [ -n "$SVC_DATADIR" ] || fail "couldn't determine the service user and data directory"
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
$SUDO systemctl enable node-dash >/dev/null 2>&1
$SUDO systemctl restart node-dash

# --- Check and tell where to find it -------------------------------------------

listen=$(sed -n 's/^Environment=NODEDASH_LISTEN=//p' "$UNIT" | head -n 1)
PORT=${listen##*:}
PORT=${PORT:-8080}

ok=no
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -fsS -o /dev/null "http://127.0.0.1:$PORT/healthz" 2>/dev/null; then
		ok=yes
		break
	fi
	sleep 1
done
if [ "$ok" = no ]; then
	fail "node-dash didn't start; see: journalctl -u node-dash -n 20 --no-pager"
fi

IP=$(hostname -I 2>/dev/null | awk '{ print $1 }')
say "node-dash $VERSION is running: http://${IP:-<this-server>}:$PORT"
say "Logs: journalctl -u node-dash -f"

if command -v ufw >/dev/null 2>&1 && $SUDO ufw status 2>/dev/null | grep -q '^Status: active'; then
	if ! $SUDO ufw status 2>/dev/null | grep -q "^$PORT[/ ]"; then
		say "The ufw firewall is active and has no rule for port $PORT yet. To open it for your network:"
		say "  sudo ufw allow from ${IP%.*}.0/24 to any port $PORT proto tcp"
	fi
fi
say "Not reachable from another computer? Also check other firewalls, such as your router or hypervisor."

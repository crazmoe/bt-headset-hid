#!/bin/bash
# Fixes the incoming-call crash of PipeWire 1.0.x for bt-headset-hid -mode pipewire.
#
# PipeWire before 1.2 passes a NULL message to libdbus in the CallAdded
# handler of spa/plugins/bluez5/modemmanager.c (a shadowed variable), and
# libdbus aborts: every incoming call crashes WirePlumber. 1.2.0 fixed it by
# renaming the variable. This script rebuilds only the Bluetooth plugin
# (libspa-bluez5.so) of the installed PipeWire version with that fix and
# replaces the installed file. Everything else stays as it is.
#
# Usage, as your normal user (it asks for sudo when needed):
#   contrib/pipewire-1.0-fix.sh            build and install the fixed plugin
#   contrib/pipewire-1.0-fix.sh --restore  reinstall the original plugin
#
# A PipeWire update from your distribution overwrites the fixed file; run the
# script again afterwards (bt-headset-hid install tells you when).

set -euo pipefail

MARKER=/etc/bt-headset-hid/pipewire-fix
PACKAGE=libspa-0.2-bluetooth

say() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -ne 0 ] || die "run as your normal user, not with sudo (it asks for sudo itself)"
command -v pipewire >/dev/null || die "pipewire is not installed"
command -v dpkg >/dev/null || die "only Debian/Ubuntu-based systems are supported"

restart_audio() {
	say "restarting PipeWire and WirePlumber"
	systemctl --user restart pipewire pipewire-pulse wireplumber || true
}

if [ "${1:-}" = "--restore" ]; then
	say "reinstalling the original $PACKAGE"
	sudo apt-get install --reinstall -y "$PACKAGE"
	sudo rm -f "$MARKER"
	restart_audio
	say "done; the original plugin is back (-mode pipewire crashes WirePlumber again on PipeWire 1.0)"
	exit 0
fi
[ $# -eq 0 ] || die "usage: $0 [--restore]"

version=$(pipewire --version | sed -n 's/.*Linked with libpipewire \([0-9][0-9.]*\).*/\1/p' | tail -n 1)
[ -n "$version" ] || die "could not read the version from 'pipewire --version'"
case "$version" in
1.0.*) ;;
*) say "PipeWire $version does not need this fix (only 1.0.x does)"; exit 0 ;;
esac

plugin=$(dpkg -L "$PACKAGE" 2>/dev/null | grep '/spa-0.2/bluez5/libspa-bluez5\.so$' | head -n 1 || true)
if [ -z "$plugin" ] || [ ! -f "$plugin" ]; then
	die "libspa-bluez5.so of package $PACKAGE not found"
fi
pkgver=$(dpkg-query -W -f='${Version}' "$PACKAGE")
say "PipeWire $version, plugin $plugin (package $PACKAGE $pkgver)"

say "installing build dependencies"
sudo apt-get install -y --no-install-recommends build-essential meson ninja-build pkg-config curl \
	libdbus-1-dev libsbc-dev libbluetooth-dev libglib2.0-dev libusb-1.0-0-dev modemmanager-dev

work="${XDG_CACHE_HOME:-$HOME/.cache}/bt-headset-hid/pipewire-$version"
src="$work/pipewire-$version"
mkdir -p "$work"
if [ ! -d "$src" ]; then
	say "downloading PipeWire $version sources"
	tarball="$work/pipewire-$version.tar.gz"
	curl -fL -o "$tarball" "https://gitlab.freedesktop.org/pipewire/pipewire/-/archive/$version/pipewire-$version.tar.gz" ||
		curl -fL -o "$tarball" "https://github.com/PipeWire/pipewire/archive/refs/tags/$version.tar.gz" ||
		die "download failed"
	mkdir -p "$src"
	tar -xzf "$tarball" -C "$src" --strip-components=1
fi

mm="$src/spa/plugins/bluez5/modemmanager.c"
[ -f "$mm" ] || die "$mm missing in the sources"
if grep -q 'spa_autoptr(DBusMessage) m = NULL;' "$mm"; then
	say "fixing the CallAdded handler in modemmanager.c"
	# Only inside the CallAdded block, exactly as PipeWire 1.2.0 does it.
	sed -i '/MM_MODEM_VOICE_SIGNAL_CALLADDED)) {/,/MM_MODEM_VOICE_SIGNAL_CALLDELETED)) {/{
		s/spa_autoptr(DBusMessage) m = NULL;/spa_autoptr(DBusMessage) m2 = NULL;/
		s/\bm = dbus_message_new_method_call(/m2 = dbus_message_new_method_call(/
		s/if (m == NULL)/if (m2 == NULL)/
		s/dbus_message_append_args(m, /dbus_message_append_args(m2, /
		s/send_with_reply(this->conn, m, /send_with_reply(this->conn, m2, /
	}' "$mm"
fi
fixed=$(sed -n '/MM_MODEM_VOICE_SIGNAL_CALLADDED)) {/,/MM_MODEM_VOICE_SIGNAL_CALLDELETED)) {/p' "$mm" | grep -c '\bm2\b' || true)
[ "$fixed" -eq 5 ] || die "the fix did not apply cleanly ($fixed of 5 places); nothing was installed"

say "building libspa-bluez5.so (only the Bluetooth plugin)"
# Same paths as the distribution package: the plugin reads its data files
# (bluez-hardware.conf with the headset quirks) from <prefix>/share/spa-0.2.
libdir=${plugin%/spa-0.2/bluez5/libspa-bluez5.so}
rm -rf "$work/build"
meson setup "$work/build" "$src" --buildtype=release \
	--prefix=/usr --libdir="$libdir" --sysconfdir=/etc --localstatedir=/var \
	-Dauto_features=disabled -Dsession-managers=[] \
	-Dspa-plugins=enabled -Ddbus=enabled -Dlibusb=enabled \
	-Dbluez5=enabled -Dbluez5-backend-hsp-native=enabled -Dbluez5-backend-hfp-native=enabled \
	-Dbluez5-backend-native-mm=enabled -Dbluez5-backend-ofono=enabled -Dbluez5-backend-hsphfpd=enabled
ninja -C "$work/build" spa/plugins/bluez5/libspa-bluez5.so
new="$work/libspa-bluez5.so"
strip --strip-unneeded -o "$new" "$work/build/spa/plugins/bluez5/libspa-bluez5.so"
grep -q 'org.freedesktop.ModemManager1' "$new" || die "built plugin lacks ModemManager support; nothing was installed"
grep -q '/usr/share/spa-0.2' "$new" || die "built plugin looks for its data files in the wrong place; nothing was installed"

needed() { readelf -d "$1" | sed -n 's/.*(NEEDED).*\[\(.*\)\]/\1/p' | sort; }
if ! diff <(needed "$plugin") <(needed "$new") >/dev/null; then
	say "note: the new plugin links other libraries than the original:"
	diff <(needed "$plugin") <(needed "$new") || true
fi

say "installing the fixed plugin (back to the original: $0 --restore, or apt-get install --reinstall $PACKAGE)"
sudo install -m 0644 "$new" "$plugin.bt-headset-hid.new"
sudo mv "$plugin.bt-headset-hid.new" "$plugin"
sudo mkdir -p "$(dirname "$MARKER")"
sha256sum "$plugin" | sudo tee "$MARKER" >/dev/null
restart_audio
say "done. Now: sudo ./bt-headset-hid install -mode pipewire, then reconnect the headset"

#!/bin/sh
# Install the package from dist/ in a Debian or Fedora container, check it,
# and remove it again. Run as root in the container by ci.yml; systemd does
# not run there, so the scripts must cope with that.
#
#   packaging/test-install.sh deb|rpm
set -eu
fmt=${1:?usage: packaging/test-install.sh deb|rpm}
cd "$(dirname "$0")/.."

case "$fmt" in
deb)
	apt-get update -qq
	apt-get install -y -qq ./dist/bt-headset-hid_*_amd64.deb
	;;
rpm)
	dnf install -y -q ./dist/bt-headset-hid-*.x86_64.rpm
	;;
esac

fail() {
	echo "test-install: $*" >&2
	exit 1
}

bt-headset-hid -version
for f in \
	/usr/bin/bt-headset-hid \
	/usr/lib/systemd/user/bt-headset-hid-session.service \
	/usr/lib/systemd/system/bt-headset-hid.service.d/hardening.conf \
	/usr/share/doc/bt-headset-hid/README.md; do
	[ -f "$f" ] || fail "$f is missing"
done
# License texts, where each format keeps them.
case "$fmt" in
deb) licenses="/usr/share/doc/bt-headset-hid/copyright
/usr/share/doc/bt-headset-hid/LICENSE
/usr/share/doc/bt-headset-hid/THIRD_PARTY_LICENSES" ;;
rpm) licenses="/usr/share/licenses/bt-headset-hid/LICENSE
/usr/share/licenses/bt-headset-hid/THIRD_PARTY_LICENSES" ;;
esac
for f in $licenses; do
	[ -f "$f" ] || fail "$f is missing"
done
# postinstall ran "bt-headset-hid install -system-only".
setup="/etc/systemd/system/bt-headset-hid.service
/etc/udev/rules.d/70-bt-headset-hid.rules
/etc/dbus-1/system.d/bt-headset-hid.conf
/etc/modules-load.d/bt-headset-hid.conf"
for f in $setup; do
	[ -f "$f" ] || fail "postinstall did not write $f"
done
grep -q '^ExecStart=/usr/bin/bt-headset-hid ' /etc/systemd/system/bt-headset-hid.service ||
	fail "system service does not run /usr/bin/bt-headset-hid"

case "$fmt" in
deb) apt-get remove -y -qq bt-headset-hid ;;
rpm) dnf remove -y -q bt-headset-hid ;;
esac

# preremove ran "bt-headset-hid uninstall".
for f in /usr/bin/bt-headset-hid $setup; do
	[ ! -e "$f" ] || fail "$f is left after removal"
done
echo "test-install: $fmt ok"

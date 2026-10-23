#!/bin/sh
# On removal (not upgrade), undo "bt-headset-hid install". deb passes
# "remove", rpm passes the number of versions left (0).
case "$1" in
remove | 0)
	for uid in $(loginctl list-users --no-legend 2>/dev/null | awk '$1 >= 1000 { print $1 }'); do
		user=$(id -nu "$uid" 2>/dev/null) || continue
		systemctl --user -M "$user@" stop bt-headset-hid-session.service >/dev/null 2>&1 || true
	done
	systemctl --global disable bt-headset-hid-session.service || true
	if [ -f /etc/systemd/system/bt-headset-hid.service ]; then
		/usr/bin/bt-headset-hid uninstall || true
	fi
	;;
esac
exit 0

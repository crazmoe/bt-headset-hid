#!/bin/sh
# On removal (not upgrade), undo "bt-headset-hid install". deb passes
# "remove", rpm passes the number of versions left (0).
case "$1" in
remove | 0)
	if [ -f /etc/systemd/system/bt-headset-hid.service ]; then
		/usr/bin/bt-headset-hid uninstall || true
	fi
	;;
esac
exit 0

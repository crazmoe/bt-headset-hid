#!/bin/sh
# Set up bt-headset-hid, or after an upgrade run the new program. The user
# service sets up each user's PipeWire; it starts at every login, and here
# for the users logged in now.
if [ -f /etc/systemd/system/bt-headset-hid.service ]; then
	systemctl try-restart bt-headset-hid.service || true
	action=try-restart
else
	/usr/bin/bt-headset-hid install -system-only ||
		echo "bt-headset-hid: setup failed; run \"sudo bt-headset-hid install\" and then \"bt-headset-hid doctor\"."
	action=start
fi
systemctl --global enable bt-headset-hid-session.service || true
for uid in $(loginctl list-users --no-legend 2>/dev/null | awk '$1 >= 1000 { print $1 }'); do
	user=$(id -nu "$uid" 2>/dev/null) || continue
	systemctl --user -M "$user@" daemon-reload >/dev/null 2>&1 || true
	systemctl --user -M "$user@" "$action" bt-headset-hid-session.service >/dev/null 2>&1 || true
done
exit 0

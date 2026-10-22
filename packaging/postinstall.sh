#!/bin/sh
# After an upgrade, run the new program if the service is set up.
if [ -f /etc/systemd/system/bt-headset-hid.service ]; then
	systemctl try-restart bt-headset-hid.service || true
else
	echo "bt-headset-hid: run \"sudo bt-headset-hid install\" from your desktop user to set it up."
fi
exit 0

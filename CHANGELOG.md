# Changelog

## Beta testers wanted

bt-headset-hid is beta software. If you have a Bluetooth headset
and use a softphone (Teams, Zoom, a WebHID browser app, ...), please try it in your daily work
and report what works and what does not, with your headset model, distribution, PipeWire
version and the output of `journalctl -u bt-headset-hid`, as a
[GitHub issue](https://github.com/crazmoe/bt-headset-hid/issues).

## Unreleased

- pipewire mode: only the headset bt-headset-hid serves starts in the phone profile; other
  Bluetooth headphones and speakers keep music (A2DP) again. `bt-headset-hid wireplumber`
  keeps the headsets set up before and takes `-headset` to change them.
- pipewire mode: several headsets, e.g. one for the office and one for home:
  `-headset XX:XX:XX:XX:XX:XX,YY:YY:YY:YY:YY:YY`. The `bt-headset-hid` speaker and microphone
  use whichever of them is connected.

## 0.2.0

- Release packages: `.deb` and `.rpm` for amd64 and arm64 in addition to the tarballs.
  The package puts the program in `/usr/bin`; `sudo bt-headset-hid install` then sets it up,
  and removing the package undoes that.
- pipewire mode: the headset only offers the phone profile, with a fixed speaker and microphone
  named "bt-headset-hid".
- pipewire mode: the headset's play/pause key becomes the call button; `-grab-media-keys`
  takes the media keys away from the media player.
- pipewire mode: a short off-hook before ringing is ignored.
- `install` refuses pipewire mode with PipeWire before 1.2; `contrib/pipewire-1.0-fix.sh`
  fixes PipeWire 1.0.x on Debian/Ubuntu.
- ModemManager emulation: Introspectable, InterfacesAdded and godbus 5.2 compatibility.
- Logs call state changes and the softphone's LED reports.

## 0.1.0

First pre-release: HID telephony device over `/dev/uhid`, modes `pipewire` and `hfp`,
`install`, `uninstall` and `wireplumber` commands, tarballs for amd64 and arm64.

# Changelog

## Beta testers wanted

bt-headset-hid is beta software. If you have a Bluetooth headset
and use a softphone (Teams, Zoom, a WebHID browser app, ...), please try it in your daily work
and report what works and what does not, with your headset model, distribution, PipeWire
version and the output of `journalctl -u bt-headset-hid`, as a
[GitHub issue](https://github.com/crazmoe/bt-headset-hid/issues).

## Unreleased

- New `bt-headset-hid doctor`: checks the service, Bluetooth, the paired headsets, programs that
  get in the way (oFono, hsphfpd, ModemManager, PulseAudio), PipeWire, the WirePlumber
  configuration and known errors in the service's log, and prints the command that fixes each
  problem, in German or English after the desktop language. `install` points to it at the end.
- `install` and `bt-headset-hid wireplumber` ask which headsets to set up when several are
  paired, instead of setting up none; without a terminal they take the connected ones, or all.
- Modes pipewire and ofono: a headset paired after `install` is set up by itself; the user
  service `bt-headset-hid-session` adds it and restarts WirePlumber. Headsets that were already
  paired when the headsets were last set up stay left out (`~/.config/bt-headset-hid/known-headsets`).
  The user service therefore now also runs with `-profile phone` and without a headset yet.
- Modes pipewire and ofono: a headset you unpair is taken out of the configuration by itself,
  without restarting anything, and stays out if paired again.
- `bt-headset-hid wireplumber` only asks to restart the daemon when the mode changed.

## 0.4.0

- New mode `ofono`: bt-headset-hid handles the headset itself as in mode hfp (all buttons,
  including redial and volume) and poses as oFono, so PipeWire's oFono backend fetches the call
  audio from it, in wideband (mSBC) where headset and adapter support it. ModemManager keeps
  running, so mobile broadband (WWAN) works next to bt-headset-hid, and PipeWire 1.0 needs no
  fix. Several headsets can be connected at once. `install -mode ofono` sets it up and starts
  ModemManager again if mode pipewire had stopped it.
- Modes hfp and ofono: BlueZ now also connects HFP to the headset itself, for headsets that
  wait for the computer instead of connecting HFP on their own.
- ofono is the default mode now (`install` recommends it, `-mode` defaults to it); pipewire
  stays as an alternative.
- ofono mode: the call goes to the headset you answer or dial on, so the default output can be
  another device, e.g. a Bluetooth speaker. While it rings no headset is switched yet.
- `bt-headset-hid wireplumber` without `-mode` keeps the mode set up before instead of
  switching to pipewire.

## 0.3.0

- pipewire mode: only the headset bt-headset-hid serves starts in the phone profile; other
  Bluetooth headphones and speakers keep music (A2DP) again. `bt-headset-hid wireplumber`
  keeps the headsets set up before and takes `-headset` to change them.
- pipewire mode: several headsets, e.g. one for the office and one for home:
  `-headset XX:XX:XX:XX:XX:XX,YY:YY:YY:YY:YY:YY`. The `bt-headset-hid` speaker and microphone
  use whichever of them is connected.
- pipewire mode: the headsets play music (A2DP) between calls. For a call the new user service
  `bt-headset-hid-session` pauses media players (MPRIS), switches the headsets to the phone
  profile, and afterwards switches back and resumes them. `-profile phone` keeps them in the
  phone profile all the time instead. With two headsets connected, only the one chosen as
  output in the sound settings switches, so the call goes where the music plays. WirePlumber's
  own switch to the phone profile, which switched every Bluetooth headset, is turned off then.

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

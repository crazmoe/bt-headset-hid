# bt-headset-hid

**English** · [Deutsch](README.de.md)

Makes the buttons of a Bluetooth headset (e.g. Jabra) usable like a USB headset dongle:
softphones (Teams, Zoom, browser apps via WebHID, ...) see a HID telephony device
(`/dev/hidrawN`, created via `/dev/uhid`) that reports answer and hang-up from the
headset and receives the ring and off-hook state from the softphone.

**Status:** beta.
See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the design and
[CHANGELOG.md](CHANGELOG.md) for the changes.

> **Beta testers wanted:** please try bt-headset-hid with your headset and softphone in daily use
> and report what works and what does not in a [GitHub issue](https://github.com/crazmoe/bt-headset-hid/issues)
> (headset model, distribution, PipeWire version, `journalctl -u bt-headset-hid`).

## Three modes

Telephone audio and the headset buttons both travel over HFP, and only one program can own
HFP. So you choose:

| | `-mode pipewire` (default) | `-mode hfp` | `-mode ofono` (new) |
|---|---|---|---|
| Who owns HFP | PipeWire | bt-headset-hid | bt-headset-hid, PipeWire takes the audio from it |
| Call audio (headset mic and speaker) | yes, as speaker and microphone "bt-headset-hid" | no (A2DP music only) | yes, as speaker and microphone "bt-headset-hid", wideband (mSBC) where the headset can |
| Music over the headset | A2DP; for a call it switches to the phone profile and pauses the music (`-profile phone`: always phone quality) | A2DP | as in `pipewire` |
| Answer / hang up / reject | yes | yes | yes |
| Redial, volume keys, HSP button | no | yes | yes |
| Call button outside a call, if the headset sends it as AVRCP play/pause | to the media player (`-grab-media-keys`: to the softphone) | to the softphone | as in `pipewire` |
| Mobile broadband (ModemManager) | no, switched off | yes | yes |
| PipeWire version | 1.2 or later (or the fix below) | any | any built with the oFono backend (the default) |
| How it works | bt-headset-hid poses as a ModemManager modem; PipeWire forwards `ATA`/`AT+CHUP` to it | bt-headset-hid registers the HFP/HSP Audio Gateway with BlueZ and parses the AT commands itself | as in `hfp`, and bt-headset-hid poses as oFono, so PipeWire's oFono backend fetches the call audio from it |

Use `pipewire` if you want to talk through the headset, which is what a dongle does. It needs
**PipeWire 1.2 or later** (`pipewire --version`): older versions, e.g. 1.0.x in Ubuntu 24.04 and
distributions based on it, crash on incoming calls from a modem, so the headset neither rings nor
answers, and `install` refuses `-mode pipewire`. There use `ofono` or `hfp`, a newer PipeWire, or fix
PipeWire 1.0.x on Debian/Ubuntu with the included script, as your normal user:

```
contrib/pipewire-1.0-fix.sh            # rebuild only the Bluetooth plugin with the fix
contrib/pipewire-1.0-fix.sh --restore  # back to the original plugin
```

The script downloads the sources of your PipeWire version, applies the one-line fix from 1.2.0,
builds only `libspa-bluez5.so` and replaces the installed file. A PipeWire update from your
distribution overwrites it again; `install` notices that and asks you to rerun the script.

`ofono` is new and combines the two: bt-headset-hid handles the headset like in `hfp` and hands
the call audio to PipeWire through oFono's interface (`org.ofono`; oFono itself is not needed and
must not run). ModemManager keeps running, so a mobile broadband modem (WWAN) keeps working, and
it does not need the PipeWire fix. It is not yet tested as widely as `pipewire`; please report
how it works with your headset.

## Setup

Requirements: Linux with BlueZ and PipeWire with WirePlumber.

1. **Download or build**

   The [releases page](https://github.com/crazmoe/bt-headset-hid/releases) has packages for
   `amd64` and `arm64`. On Debian/Ubuntu or Fedora/openSUSE install the package, which puts the
   program in `/usr/bin` (removing the package later also undoes step 2):

   ```
   sudo apt install ./bt-headset-hid_*_amd64.deb      # Debian, Ubuntu
   sudo dnf install ./bt-headset-hid-*.x86_64.rpm     # Fedora (openSUSE: zypper install)
   ```

   On other distributions take the tarball, unpack it and change into the folder:

   ```
   tar xzf bt-headset-hid_*_linux_amd64.tar.gz
   cd bt-headset-hid_*_linux_amd64
   ```

   Or build it yourself with Go:

   ```
   go build ./cmd/bt-headset-hid
   ```

2. **Install**, from your normal desktop user with `sudo`:

   ```
   sudo bt-headset-hid install      # from the package
   sudo ./bt-headset-hid install    # from the tarball or your own build
   ```

   It asks for the mode (or pass `-mode pipewire` / `-mode hfp` / `-mode ofono`) and then sets up
   everything: the program in `/usr/local/bin`, the `uhid` kernel module, the udev rule that lets
   softphones open the device, the systemd service with the chosen mode, for `pipewire` the D-Bus
   policy and switching off ModemManager, for `ofono` the D-Bus policy and starting ModemManager
   again, and the WirePlumber configuration of your user (0.4 or 0.5, detected automatically). In
   `-mode pipewire` and `-mode ofono` it also creates a speaker and a microphone named
   `bt-headset-hid` for the paired headset (for several headsets see `-headset` below). Run it again to switch modes; `sudo bt-headset-hid uninstall`
   removes everything.

3. **Connect the headset** as usual (`bluetoothctl` or your desktop; reconnect it if it was
   already connected) and watch `journalctl -u bt-headset-hid -f` for "headset … connected".
   In `-mode pipewire` and `-mode ofono` the headset plays music (A2DP). When a call rings or you call someone,
   the user service `bt-headset-hid-session` pauses media players that are playing, switches
   the headset to the phone profile (HFP), and after the call switches back and resumes them
   (`journalctl --user -u bt-headset-hid-session -f`). Players are paused through MPRIS, which
   most players and browsers support. If you paired the headset only after `install`, run
   `bt-headset-hid wireplumber` once as your user so this and the `bt-headset-hid` speaker and
   microphone apply to it.

4. **Select the device in the softphone.** It is called `bt-headset-hid`. In `-mode pipewire`
   and `-mode ofono` also choose `bt-headset-hid` as speaker and microphone: these always exist, pass the sound to
   the headset while it is connected and to the default devices otherwise. Browser apps ask for
   WebHID access once.

### Manual setup

What `install` does, step by step, if you prefer to do it yourself:

- `sudo install bt-headset-hid /usr/local/bin/`
- PipeWire: `bt-headset-hid wireplumber -mode pipewire` (or `-mode hfp`, `-mode ofono`) as your normal user.
  It detects the WirePlumber version, writes the matching file (0.4: Lua in
  `~/.config/wireplumber/bluetooth.lua.d/`, 0.5 and later: `~/.config/wireplumber/wireplumber.conf.d/`),
  removes the file of the other version and restarts PipeWire; `-remove` undoes it. By hand: copy
  the matching file from `contrib/wireplumber/` to the path written in its first lines.
  In `-mode pipewire` and `-mode ofono` it also sets up the headsets: a WirePlumber rule
  (`52-bt-headset-hid-headset.lua` or `.conf`) gives their call audio one name,
  `~/.config/systemd/user/bt-headset-hid-session.service` switches them to the phone profile for
  calls, and `~/.config/pipewire/pipewire.conf.d/51-bt-headset-hid.conf` adds the `bt-headset-hid` speaker
  and microphone, which use whichever of them is connected (with two connected, the call goes to
  the one chosen as output in the sound settings; with `-profile phone`, to the first one PipeWire
  finds). It takes the headsets set up before, otherwise the paired one; to set up
  several, e.g. one for the office and one for home, or to change them:
  `-headset XX:XX:XX:XX:XX:XX,YY:YY:YY:YY:YY:YY`. It also turns off WirePlumber's own switch to the
  phone profile (0.4: `policy.lua.d/52-bt-headset-hid-session.lua`), which would switch every
  Bluetooth headset when a call starts. With `-profile phone` the headsets stay in the
  phone profile all the time instead, without the user service (`-profile switch` goes back).
- Only for `-mode pipewire`: `sudo systemctl disable --now ModemManager` (mobile broadband modems
  stop working while bt-headset-hid runs) and
  `sudo cp contrib/dbus/bt-headset-hid.conf /etc/dbus-1/system.d/`. PipeWire must be built with
  ModemManager support (most distributions do this).
- Only for `-mode ofono`: `sudo cp contrib/dbus/bt-headset-hid-ofono.conf /etc/dbus-1/system.d/bt-headset-hid.conf`
  (instead of the file above). If `-mode pipewire` switched off ModemManager:
  `sudo systemctl enable --now ModemManager`.
- `sudo cp contrib/udev/70-bt-headset-hid.rules /etc/udev/rules.d/ && sudo udevadm control --reload`
- `echo uhid | sudo tee /etc/modules-load.d/bt-headset-hid.conf && sudo modprobe uhid`
- `sudo cp contrib/systemd/bt-headset-hid.service /etc/systemd/system/` (edit `-mode` in
  `ExecStart`), then `sudo systemctl daemon-reload && sudo systemctl enable --now bt-headset-hid`.

### Testing without a softphone

```
grep -l bt-headset-hid /sys/class/hidraw/*/device/uevent    # e.g. .../hidraw5/...
# terminal 1: show input reports (hexdump buffers 16 bytes, reports are 2 bytes)
sudo python3 -c 'import os; f=os.open("/dev/hidraw5", os.O_RDONLY); [print(os.read(f, 64).hex(" "), flush=True) for _ in iter(int, 1)]'
# terminal 2: act as the softphone
printf '\x02\x02' | sudo tee /dev/hidraw5 >/dev/null   # Ring LED: the headset rings
printf '\x02\x01' | sudo tee /dev/hidraw5 >/dev/null   # Off-Hook LED: call active
printf '\x02\x00' | sudo tee /dev/hidraw5 >/dev/null   # all LEDs off: idle
```

Answering on the headset shows `01 01` (hook switch on), hanging up `01 00`. Like with a USB
dongle, the headset keeps ringing until the host sets the Off-Hook LED; a softphone does that
when it sees the hook switch.

### Troubleshooting

- `org.freedesktop.ModemManager1 is already owned`: ModemManager is still running (`install` with `-mode pipewire` does this).
- `org.ofono is already owned` in `-mode ofono`: the real oFono is running
  (`sudo systemctl disable --now ofono`).
- No call audio in `-mode ofono`: `journalctl -u bt-headset-hid` should show `audio agent ...
  registered` after the start and `audio for ...: codec 1` (or 2) when a call starts. Without the
  first line PipeWire does not use its oFono backend: check that the WirePlumber configuration is
  active and that your PipeWire has it (`grep -c HandsfreeAudioManager` on `libspa-bluez5.so`,
  usually in `/usr/lib/*/spa-0.2/bluez5/`). If the first call after connecting has no sound or
  the journal shows `no wideband audio link`, the headset or adapter has trouble with wideband
  speech; set `bluez5.enable-msbc` to `false` in the WirePlumber file and restart WirePlumber.
- `UUID already registered` or `already provides the HFP/HSP Audio Gateway` in `-mode hfp` or `-mode ofono`:
  another program still owns HFP. Usually the WirePlumber configuration is not active: wrong WirePlumber version of the
  file, wrong directory, or WirePlumber not restarted. Also check that neither PulseAudio
  (`pactl info | grep "Server Name"` must name PipeWire) nor oFono/hsphfpd
  (`systemctl status ofono hsphfpd`) is running. Then `sudo systemctl restart bt-headset-hid`.
- `RFCOMM receive command but modem not available` or `dbus_message_get_path() were incorrect` in
  the WirePlumber log, the system sound icon vanishes briefly, or the headset does not ring in
  `-mode pipewire`: check `pipewire --version` (1.2 or later, or the fix script above), then
  restart WirePlumber (`systemctl --user restart wireplumber`).
- The media player reacts to the call button in `-mode pipewire` or `-mode ofono`: check with
  `sudo evtest /dev/input/eventN` (the "<headset> (AVRCP)" device) whether the press arrives
  there. If it does, `-grab-media-keys` takes that device exclusively, so play/pause becomes the
  call button (the journal shows `media keys of "<headset> (AVRCP)" now go to the softphone`)
  and next/previous no longer control music from the headset.
- The headset stays in the music profile during a call in `-mode pipewire` or `-mode ofono`: check
  `systemctl --user status bt-headset-hid-session` and its journal. As a workaround,
  `bt-headset-hid wireplumber -profile phone` keeps it in the phone profile.
- No sound during a call in `-mode pipewire` or `-mode ofono`: the softphone must use `bt-headset-hid` (or the
  headset) as speaker and microphone, otherwise PipeWire opens no call audio to the headset.
  After restarting PipeWire, restart the softphone too. `wpctl status` shows under "Streams"
  where its sound goes during a call.
- Buttons do nothing in `-mode pipewire`: check that the WirePlumber configuration is active
  (`wpctl status`, `journalctl --user -u wireplumber`) and restart the headset connection.
- `create HID device: open /dev/uhid`: the daemon is not running as root, or the `uhid` kernel
  module is missing (`sudo modprobe uhid`).
- Some softphones only accept HID devices of known vendors; try `-vendor`/`-product` (and adjust
  the udev rule).

## Development

```
gofmt -l .
go vet ./...
go test -race ./...
go build ./cmd/bt-headset-hid
```

Tests that need `/dev/uhid` (root) or `dbus-daemon` are skipped where those are missing.

## Feedback

Problems, ideas and test reports are welcome as
[issues](https://github.com/crazmoe/bt-headset-hid/issues/new/choose), in English or German.
Please report security problems privately, see [SECURITY.md](SECURITY.md).

## License

MIT

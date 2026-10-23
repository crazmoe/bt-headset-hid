# bt-headset-hid: details

**English** · [Deutsch](ADVANCED.de.md)

The short guide for the usual case is the [README](../README.md). This page has everything else:
the modes, setup without the package or by hand, testing, troubleshooting and development.
See [ARCHITECTURE.md](ARCHITECTURE.md) for the design.

## Three modes

Telephone audio and the headset buttons both travel over HFP, and only one program can own
HFP. So you choose:

| | `-mode pipewire` | `-mode hfp` | `-mode ofono` (default) |
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

Use `ofono` (the default) if you want to talk through the headset, which is what a dongle does:
bt-headset-hid handles the headset like in `hfp` and hands the call audio to PipeWire through
oFono's interface (`org.ofono`; oFono itself is not needed and must not run). ModemManager keeps
running, so a mobile broadband modem (WWAN) keeps working, and any PipeWire version works.

`pipewire` was the default before `ofono` and stays as an alternative. It needs
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

## Setup

Requirements: Linux with BlueZ and PipeWire with WirePlumber.

1. **Install the package**

   The [releases page](https://github.com/crazmoe/bt-headset-hid/releases) has packages for
   `amd64` and `arm64`. On Debian/Ubuntu or Fedora/openSUSE install the package:

   ```
   sudo apt install ./bt-headset-hid_*_amd64.deb      # Debian, Ubuntu
   sudo dnf install ./bt-headset-hid-*.x86_64.rpm     # Fedora (openSUSE: zypper install)
   ```

   That is the whole setup. The package chooses the mode by itself (see step 2), sets up the system
   service and then the PipeWire of each desktop user: right away for the users logged in, at
   their next login for the others. A desktop notification says when it is ready. If several
   headsets are paired, it sets up the connected ones, or all; `bt-headset-hid wireplumber`
   chooses others. The package also adds its package repository, so later versions come with your
   system updates. Removing the package undoes everything. Then check the setup as your normal
   user:

   ```
   bt-headset-hid doctor
   ```

   It checks the service, Bluetooth, the paired headsets, programs that get in the way (oFono,
   ModemManager, PulseAudio), PipeWire and your WirePlumber configuration, and prints the command
   that fixes each problem it finds (in German if your desktop is set to German). The user service
   runs the same checks now and then and shows a desktop notification for each new problem, with
   the fix.

2. **Without the package**, on other distributions, take the tarball, unpack it and change into
   the folder, or build it yourself with Go:

   ```
   tar xzf bt-headset-hid_*_linux_amd64.tar.gz
   cd bt-headset-hid_*_linux_amd64
   go build ./cmd/bt-headset-hid                      # instead: build it yourself
   ```

   Then install it from your normal desktop user with `sudo`:

   ```
   sudo ./bt-headset-hid install
   ```

   It chooses the mode by itself and says which: `ofono`, unless the oFono service runs or your
   PipeWire lacks the oFono backend; then `pipewire` if your PipeWire can (see above) and
   ModemManager is not in use, otherwise `hfp`. Run again, it keeps the mode; `-mode pipewire`,
   `-mode hfp` or `-mode ofono` chooses it yourself (also with the package:
   `sudo bt-headset-hid install -mode hfp`). It sets up everything: the program in
   `/usr/local/bin`, the `uhid` kernel module, the udev rule that lets softphones open the
   device, the systemd service with the mode, for `pipewire` the D-Bus policy and switching off
   ModemManager, for `ofono` the D-Bus policy and starting ModemManager again, and the WirePlumber
   configuration of your user (0.4 or 0.5, detected automatically). In `-mode pipewire` and
   `-mode ofono` it also creates a speaker and a microphone named `bt-headset-hid` for the paired
   headset; if several are paired, it lists them and asks which ones you use for calls (Enter
   takes the connected ones, or all). `sudo bt-headset-hid uninstall` removes everything. At the
   end, check the setup with `bt-headset-hid doctor` as in step 1.

3. **Connect the headset** as usual (`bluetoothctl` or your desktop; reconnect it if it was
   already connected). `bt-headset-hid doctor` should now list it as connected; the log is in
   `journalctl -u bt-headset-hid -f` ("headset … connected").
   In `-mode pipewire` and `-mode ofono` the headset plays music (A2DP). When a call rings or you call someone,
   the user service `bt-headset-hid-session` pauses media players that are playing, switches
   the headset to the phone profile (HFP), and after the call switches back and resumes them
   (`journalctl --user -u bt-headset-hid-session -f`). In `-mode ofono` it switches only once you
   answer, and only the headset you answer on; while it rings, the headsets beep themselves.
   Players are paused through MPRIS, which most players and browsers support. A headset you pair
   later is set up by itself a few seconds after pairing, without any restart (a notification
   says so, the user service's log `newly paired headset ... set up`). Headsets that were already
   paired at the setup and that it did not set up stay left out. A headset you unpair
   (remove in the Bluetooth settings) is taken out by itself, without a restart, and is not set up
   again if you pair it again.

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
  removes the file of the other version and restarts PipeWire; `-remove` undoes it, and the user service of the package leaves your PipeWire
  alone from then on, until you run `bt-headset-hid wireplumber` again. Without `-mode`
  it keeps the mode set up before. By hand: copy
  the matching file from `contrib/wireplumber/` to the path written in its first lines.
  In `-mode pipewire` and `-mode ofono` it also sets up the headsets: their list goes to
  `~/.config/bt-headset-hid/headsets`, `~/.config/pipewire/pipewire.conf.d/51-bt-headset-hid.conf`
  adds the `bt-headset-hid` speaker and microphone, and the user service
  `~/.config/systemd/user/bt-headset-hid-session.service` (with the package
  `/usr/lib/systemd/user/bt-headset-hid-session.service`, for every user) switches the headsets to the phone
  profile for calls and points the `bt-headset-hid` speaker and microphone at the call audio of
  the one connected, while everything runs, so changing the list needs no restart (with two
  connected, the call goes to the one you answer or dial on in `-mode ofono`, so the default output
  can stay e.g. a Bluetooth speaker; otherwise to the one chosen as output in the sound settings;
  with `-profile phone`, to the first in the list). It takes the headsets set up before, otherwise the paired one,
  and asks if several are paired (without a terminal it takes the connected ones, or all); to set up
  several, e.g. one for the office and one for home, or to change them:
  `-headset XX:XX:XX:XX:XX:XX,YY:YY:YY:YY:YY:YY`. It notes the headsets paired at that moment, and
  the ones you leave out with `-headset`, in `~/.config/bt-headset-hid/known-headsets`; these are
  not set up again even when paired again. A headset paired later for the first time is not on
  that list, and the user service sets it up by itself. It also turns off WirePlumber's own switch to the
  phone profile (0.4: `policy.lua.d/52-bt-headset-hid-session.lua`, 0.5:
  `wireplumber.conf.d/52-bt-headset-hid-headset.conf`), which would switch every
  Bluetooth headset when a call starts. With `-profile phone` the headsets stay in the
  phone profile all the time instead; the user service puts them there when they connect
  (`-profile switch` goes back). The command restarts PipeWire only when a WirePlumber or PipeWire
  file changed; the per-headset WirePlumber rules of older versions are removed then.
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

Start with `bt-headset-hid doctor` (as your normal user, or with `sudo` to also read the
service's log): it finds most of the problems below and prints the fix.

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

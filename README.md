# bt-headset-hid

**English** · [Deutsch](README.de.md)

Makes the buttons of a Bluetooth headset (e.g. Jabra) usable like a USB headset dongle:
softphones (Teams, Zoom, browser apps via WebHID, ...) see a HID telephony device
(`/dev/hidrawN`, created via `/dev/uhid`) that reports answer and hang-up from the
headset and receives the ring and off-hook state from the softphone.

**Status:** feature complete, not yet tested with real hardware.
See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the design.

## Two modes

Telephone audio and the headset buttons both travel over HFP, and only one program can own
HFP. So you choose:

| | `-mode pipewire` (default) | `-mode hfp` |
|---|---|---|
| Who owns HFP | PipeWire | bt-headset-hid |
| Call audio (headset mic and speaker) | yes | no (A2DP music only) |
| Answer / hang up / reject | yes | yes |
| Redial, volume keys, HSP button | no | yes |
| How it works | bt-headset-hid poses as a ModemManager modem; PipeWire forwards `ATA`/`AT+CHUP` to it | bt-headset-hid registers the HFP/HSP Audio Gateway with BlueZ and parses the AT commands itself |

Use `pipewire` if you want to talk through the headset, which is what a dongle does.

## Setup

Requirements: Linux with BlueZ, PipeWire with WirePlumber, root for installing.

1. **Build and install**

   ```
   go build ./cmd/bt-headset-hid
   sudo install bt-headset-hid /usr/local/bin/
   ```

2. **Configure PipeWire** for your mode, as your normal user (not with `sudo`):

   ```
   bt-headset-hid wireplumber -mode pipewire    # or: -mode hfp
   ```

   This detects the WirePlumber version, writes the matching file (0.4: Lua in
   `~/.config/wireplumber/bluetooth.lua.d/`, 0.5 and later: `~/.config/wireplumber/wireplumber.conf.d/`),
   removes the file of the other version and restarts PipeWire and WirePlumber.
   `bt-headset-hid wireplumber -remove` undoes it. Run it again after switching modes or
   upgrading WirePlumber. To do it by hand, copy the matching file from `contrib/wireplumber/`
   to the path written in its first lines.

3. **Only for `-mode pipewire`**: the real ModemManager must not run (you lose mobile broadband
   modems while bt-headset-hid runs), and the daemon needs permission to take its bus name:

   ```
   sudo systemctl disable --now ModemManager
   sudo cp contrib/dbus/bt-headset-hid.conf /etc/dbus-1/system.d/
   ```

   PipeWire must be built with ModemManager support (most distributions do this).

4. **Give your user access to the HID device** so softphones can open it:

   ```
   sudo cp contrib/udev/70-bt-headset-hid.rules /etc/udev/rules.d/
   sudo udevadm control --reload
   ```

5. **Start the daemon**. It runs as root, because it creates the device via `/dev/uhid`:

   ```
   sudo cp contrib/systemd/bt-headset-hid.service /etc/systemd/system/
   # edit ExecStart there for -mode hfp
   sudo systemctl daemon-reload
   sudo systemctl enable --now bt-headset-hid
   journalctl -u bt-headset-hid -f
   ```

6. **Pair and connect the headset** as usual (`bluetoothctl` or your desktop). In `-mode pipewire`
   pick the headset's "Headset Head Unit (HSP/HFP)" profile for calls, or let WirePlumber switch
   automatically when the softphone opens the microphone.

7. **Select the device in the softphone.** It is called `bt-headset-hid`. Browser apps ask for
   WebHID access once.

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

- `org.freedesktop.ModemManager1 is already owned`: ModemManager is still running (step 3).
- `UUID already registered` or `already provides the HFP/HSP Audio Gateway` in `-mode hfp`:
  another program still owns HFP. Usually step 2 is not active: wrong WirePlumber version of the
  file, wrong directory, or WirePlumber not restarted. Also check that neither PulseAudio
  (`pactl info | grep "Server Name"` must name PipeWire) nor oFono/hsphfpd
  (`systemctl status ofono hsphfpd`) is running. Then `sudo systemctl restart bt-headset-hid`.
- Buttons do nothing in `-mode pipewire`: check that step 2 is active
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

## License

MIT

# Architecture

HFP carries both the call audio and the headset buttons, and only one program can own it.
The daemon therefore has two modes (`-mode`); both end in the same HID device.

```
mode pipewire (default, with call audio)

Headset --HFP--> BlueZ --> PipeWire (audio, AT commands)
                               | ATA / AT+CHUP -> Call.Accept / Call.Hangup (D-Bus)
                               v
                      bt-headset-hid as ModemManager modem --> mapping --> /dev/uhid --> /dev/hidrawN
                               ^                                                          |
                               +---- call state (RINGING_IN / ACTIVE) <---- LED reports --+

mode hfp (all buttons, no call audio)

Headset --HFP/HSP--> BlueZ --RFCOMM fd--> bt-headset-hid: AT parser + HFP AG --> mapping --> /dev/uhid
                                                ^                                              |
                                                +------ +CIEV / RING <------ LED reports ------+
```

In mode pipewire, PipeWire's native HFP backend talks to the first ModemManager modem when
`bluez5.hfphsp-backend-native-modem` is set (see `spa/plugins/bluez5/modemmanager.c` in PipeWire).
It forwards only `ATA` and `AT+CHUP`, refuses dialing, and handles `AT+VGS` itself as the
Bluetooth device volume.

Some softphones set the Off-Hook LED for a moment before Ring when a call comes in. The daemon
passes an Active that follows Idle on to the modem only after one second, and drops it when
another state comes first, so the headset does not see a call that ends at once.

Outside a call many headsets send their call button as AVRCP play/pause, which BlueZ turns into
an input device "<name> (AVRCP)". With `-grab-media-keys` the daemon grabs that device exclusively
(`EVIOCGRAB`) and feeds play/pause into the mapping as the HSP-style button, so the press reaches
the softphone instead of the media player. A press within 1.5 s of an answer or hang-up from
PipeWire is dropped, in case the headset reported it both ways.

## Packages (under `internal/`)

| Package | Responsibility | I/O |
|---|---|---|
| `atparser` | Split RFCOMM stream into AT lines, bounded length | none |
| `hfp` | Audio Gateway state machine: SLC handshake (`BRSF`, `CIND`, `CMER`, `CHLD`), call state / `+CIEV`, button events (`ATA`, `CHUP`, `CKPD`, `BLDN`, `BVRA`, `VGS`/`VGM`) | none |
| `mapping` | Headset event -> HID input reports (hook switch, drop, redial, volume); LED output report -> call state | none |
| `hiddesc` | Report descriptor + report packing/unpacking | none |
| `avrcp` | Headset AVRCP input device: matching and play/pause decoding (pure), exclusive grab and read of `/dev/input/eventN` (`Watcher`, mode pipewire) | evdev |
| `uhid` | `/dev/uhid` create/input/output events | uhid |
| `modem` | ModemManager modem for PipeWire: call state logic (`Calls`, pure) and its D-Bus export (`Server`) | D-Bus |
| `bluez` | Register HFP AG / HSP AG profiles via D-Bus, receive RFCOMM fds (mode hfp) | D-Bus |
| `daemon` | Wire everything together: `Bridge` (mapping + HID device), `Session` (one headset in mode hfp), `Run` | all |
| `wpconf` | Per-user WirePlumber configuration for a mode (`bt-headset-hid wireplumber`), WirePlumber 0.4 Lua or 0.5 SPA-JSON | files |
| `install` | `bt-headset-hid install`/`uninstall`: plan of the system files (binary, uhid, udev rule, D-Bus policy, systemd unit) for a mode, applied by a runner | files, commands |
| `dbustest` | Private `dbus-daemon` for tests | process |

## HID reports

Laid out like a USB headset dongle; every report is the report ID plus one byte.

| ID | Direction | Page | Bits |
|---|---|---|---|
| 1 | input | Telephony (0x0B), Headset | Hook Switch (abs), Flash, Redial, Drop, Phone Mute (rel), 3 padding |
| 2 | output | LED (0x08) | Off-Hook, Ring, Mute, Hold, 4 padding |
| 3 | input | Consumer (0x0C) | Volume Increment, Volume Decrement, 6 padding |

Relative usages and volume keys are sent as press then release. The Off-Hook LED
maps to call state Active, otherwise Ring to Incoming, otherwise Idle; the hook
switch follows the Off-Hook LED so the next button press toggles correctly.

## Prerequisites on the host

See the setup section of the [README](../README.md). In short: in mode pipewire, WirePlumber
enables the modem link and ModemManager must not run; in mode hfp, PipeWire must not claim
HFP/HSP (`bluez5.roles = [ a2dp_sink a2dp_source ]`), otherwise BlueZ refuses the registration.
The daemon needs read/write access to `/dev/uhid` (root) and, in mode pipewire, a D-Bus policy
that lets it own `org.freedesktop.ModemManager1` (`contrib/dbus/`).

`go test ./internal/uhid` creates a real device only when it has access to `/dev/uhid`;
the D-Bus tests start a private `dbus-daemon` and are skipped if it is not installed.

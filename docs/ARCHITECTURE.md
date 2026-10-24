# Architecture

HFP carries both the call audio and the headset buttons, and only one program can own it.
The daemon therefore has three modes (`-mode`); all end in the same HID device.

```
mode pipewire (with call audio, answer and hang-up only)

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

mode ofono (default: all buttons and call audio, ModemManager stays free)

Headset --HFP/HSP--> BlueZ --RFCOMM fd--> bt-headset-hid: as in mode hfp --> mapping --> /dev/uhid
   ^                                          | as oFono: card per headset, Acquire
   |                                          v (+BCS codec, SCO socket fd)
   +------------- SCO audio ------------- PipeWire (oFono backend): "bt-headset-hid" speaker/mic
```

In mode pipewire, PipeWire's native HFP backend talks to the first ModemManager modem when
`bluez5.hfphsp-backend-native-modem` is set (see `spa/plugins/bluez5/modemmanager.c` in PipeWire).
It forwards only `ATA` and `AT+CHUP`, refuses dialing, and handles `AT+VGS` itself as the
Bluetooth device volume.

In mode ofono the daemon talks HFP itself as in mode hfp, serving every connected headset, and
owns `org.ofono` instead of the ModemManager name. PipeWire's oFono backend
(`bluez5.hfphsp-backend = "ofono"`, `spa/plugins/bluez5/backend-ofono.c`) registers as audio
agent with `HandsfreeAudioManager.Register` and learns the headsets from `GetCards` and
`CardAdded`; the daemon adds a card (type "gateway") once a headset's service level connection
is up and removes it when the connection ends. When PipeWire opens the call audio it calls
`HandsfreeAudioCard.Acquire` and waits: the daemon proposes a codec with `+BCS` (mSBC if the
headset listed it in `AT+BAC` and PipeWire registered it, else CVSD; only if both sides announce
codec negotiation), waits up to two seconds for `AT+BCS`, falls back to CVSD on refusal or
silence, connects the SCO socket (transparent mode for mSBC) and returns it with the codec.
If the wideband link fails the card keeps to CVSD. Only the registered agent may acquire, since
the socket carries the microphone. The call state for `bt-headset-hid session` is the property
`CallState` of `io.github.crazmoe.BtHeadsetHid1` at `/bt_headset_hid` on the same name; the
session reads it and falls back to the modem of mode pipewire. Volume changes on the headset
(`AT+VGS`) become HID volume keys as in mode hfp, since the oFono backend has no device volume.

In modes pipewire and ofono the configured headsets (one or more) play music (A2DP) between calls. The user
service `bt-headset-hid session` watches the calls of the modem on the system bus (`Voice.Calls`,
`Call.State`, refreshed on every signal of the modem's name). When a call is dialled, rings or
goes on, it pauses the MPRIS players that are playing and switches the connected configured
headsets to `headset-head-unit` (`pw-dump`, `wpctl set-profile`); two seconds after the last call
ended it switches back the ones it switched and resumes the players it paused. The logic is in
`callswitch`, the I/O in `desktop`. With `-profile phone` the session puts them in
`headset-head-unit` when they connect and leaves them there. Other Bluetooth devices keep A2DP
either way. The headsets are a list in `~/.config/bt-headset-hid/headsets`. The PipeWire
configuration adds two loopbacks, a sink and a source both called `bt-headset-hid`; the session
watches PipeWire (`pw-dump --monitor`) and points their side towards the headset at the call audio
(`api.bluez5.sco.sink`/`.source`) of a listed headset by setting `target.object` in the
`default` metadata (`callswitch.RouteFor`), preferring the one that took the call, or deletes it so WirePlumber
falls back to the default devices. So pairing, unpairing and changing the list need no restart
of WirePlumber. With two connected, `bt-headset-hid session`
switches only the one whose node is the default output (`default.audio.sink`), so the call goes
where the music plays; if neither is, it switches both and the loopbacks take the first in the list,
as with `-profile phone`. In mode ofono the daemon knows which headset took the call (the
one whose button answered or dialled, published as `CallDevice`): the session then switches only
that one, and none while the call only rings (`callswitch.Route`); a call taken in the softphone
falls back to the default output as above. Without `-profile phone` WirePlumber's own switch
(`media-role.use-headset-profile` in 0.4, `bluetooth.autoswitch-to-headset-profile` in 0.5) is
turned off: when a call stream starts it would switch every Bluetooth headset, including ones
bt-headset-hid does not serve. The loopbacks' side towards the headset is passive, so the call
audio only runs while a program uses them.

The deb and rpm packages ship the session as a user unit in `/usr/lib/systemd/user`, enabled
for every user (`systemctl --global enable`); `postinst` runs `install -system-only` and starts
the unit for the users logged in. At its start the session compares the user's WirePlumber mode
with the mode of the system unit and, if they differ, sets up the user's PipeWire as
`bt-headset-hid wireplumber` would (in mode hfp it then only shows the tray icon and runs the checks). The daemon needs no restart for
that: while PipeWire still holds HFP, its registration fails and systemd starts it again every
five seconds. `~/.config/bt-headset-hid/off`, written by `wireplumber -remove`, keeps the session
from doing so. The session also shows desktop notifications (`org.freedesktop.Notifications`):
when it set up PipeWire or a new headset, and for each new failure of the `doctor` checks, which
it runs 30 seconds after the start and then every 30 minutes (`doctor.NewFailures`).

The session shows a tray icon: a StatusNotifierItem (`org.kde.StatusNotifierItem`, registered
with `org.kde.StatusNotifierWatcher`, again whenever that restarts) with a `com.canonical.dbusmenu`
menu, as KDE, Xfce, Cinnamon, MATE, LXQt, waybar and GNOME with the AppIndicator extension show
them; without a tray nothing is shown. `tray` builds icon, tooltip and menu from the state (mode,
paired headsets and which are set up, call, failures of the last check); `desktop.Tray` exports
it. Ticking a headset in the menu writes the headset list as `-headset` would, without a restart.
The menu shows the battery level from `org.bluez.Battery1`, or in mode ofono from the daemon's
`Batteries` property, and the session warns once below 20 % (`tray.LowBatteries`). In modes hfp
and ofono the daemon announces HF indicators and Apple's accessory battery reports, turns the
`AT+BIEV`, `AT+IPHONEACCEV` and `AT+XEVENT=BATTERY` reports into `hfp.Battery` events and hands
them to BlueZ as a battery provider (`bluez.Batteries`, `org.bluez.BatteryProviderManager1`).

Some softphones set the Off-Hook LED for a moment before Ring when a call comes in. The daemon
passes an Active that follows Idle on to the modem only after one second, and drops it when
another state comes first, so the headset does not see a call that ends at once.

Outside a call many headsets send their call button as AVRCP play/pause, which BlueZ turns into
an input device "<name> (AVRCP)". With `-grab-media-keys` the daemon grabs that device exclusively
(`EVIOCGRAB`) and feeds play/pause into the mapping as the HSP-style button, so the press reaches
the softphone instead of the media player. A press within 1.5 s of an answer or hang-up from
PipeWire (or, in mode ofono, over HFP) is dropped, in case the headset reported it both ways.

## Packages (under `internal/`)

| Package | Responsibility | I/O |
|---|---|---|
| `atparser` | Split RFCOMM stream into AT lines, bounded length | none |
| `hfp` | Audio Gateway state machine: SLC handshake (`BRSF`, `CIND`, `CMER`, `CHLD`), call state / `+CIEV`, button events (`ATA`, `CHUP`, `CKPD`, `BLDN`, `BVRA`, `VGS`/`VGM`) | none |
| `mapping` | Headset event -> HID input reports (hook switch, drop, redial, volume); LED output report -> call state | none |
| `hiddesc` | Report descriptor + report packing/unpacking | none |
| `avrcp` | Headset AVRCP input device: matching and play/pause decoding (pure), exclusive grab and read of `/dev/input/eventN` (`Watcher`, modes pipewire and ofono) | evdev |
| `uhid` | `/dev/uhid` create/input/output events | uhid |
| `modem` | ModemManager modem for PipeWire: call state logic (`Calls`, pure) and its D-Bus export (`Server`) | D-Bus |
| `bluez` | Register HFP AG / HSP AG profiles via D-Bus, receive RFCOMM fds, adapter address (modes hfp and ofono) | D-Bus |
| `ofono` | oFono hands-free audio manager and cards for PipeWire, plus the call state for the session (mode ofono) | D-Bus |
| `sco` | Connect the SCO audio socket to a headset (mode ofono) | Bluetooth socket |
| `daemon` | Wire everything together: `Bridge` (mapping + HID device), `Session` (one headset connection in modes hfp and ofono, including codec negotiation), `Run` | all |
| `wpconf` | Per-user WirePlumber configuration for a mode (`bt-headset-hid wireplumber`), WirePlumber 0.4 Lua or 0.5 SPA-JSON; in modes pipewire and ofono also the headset rules, the PipeWire loopbacks for the `bt-headset-hid` speaker and microphone and the user service for `bt-headset-hid session` | files |
| `callswitch` | During a call: pause playing media players, switch headsets from A2DP to the phone profile, and back afterwards | none (behind an interface) |
| `desktop` | I/O of `bt-headset-hid session`: `pw-dump`/`wpctl`, MPRIS on the session bus, the call state on the system bus (oFono name or modem), notifications and the tray icon | commands, D-Bus |
| `tray` | What the tray icon shows: icon, tooltip and menu from the state of the setup | none |
| `doctor` | `bt-headset-hid doctor`: findings with fixes from the facts gathered by the command (service, files, BlueZ, conflicting programs, PipeWire, WirePlumber configuration, log lines) | none |
| `install` | `bt-headset-hid install`/`uninstall`: plan of the system files (binary, uhid, udev rule, D-Bus policy, systemd unit) for a mode, applied by a runner; the choice of the mode when none is given | files, commands |
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

See the setup section of [ADVANCED.md](ADVANCED.md). In short: in mode pipewire, WirePlumber
enables the modem link and ModemManager must not run; in mode hfp, PipeWire must not claim
HFP/HSP (`bluez5.roles = [ a2dp_sink a2dp_source ]`), otherwise BlueZ refuses the registration;
in mode ofono, PipeWire uses its oFono backend, which registers nothing with BlueZ, and the real
oFono must not run. The daemon needs read/write access to `/dev/uhid` (root) and a D-Bus policy
that lets it own `org.freedesktop.ModemManager1` (mode pipewire) or `org.ofono` (mode ofono)
(`contrib/dbus/`).

`go test ./internal/uhid` creates a real device only when it has access to `/dev/uhid`;
the D-Bus tests start a private `dbus-daemon` and are skipped if it is not installed.

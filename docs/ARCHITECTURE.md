# Architecture

```
Headset --BT/RFCOMM--> BlueZ --fd--> bt-headset-hid --/dev/uhid--> /dev/hidrawN
                                          |
                                     AT parser + SLC handshake (HFP/HSP Audio Gateway role)
                                          |
                                     AT -> HID mapping (Telephony page 0x0B, Consumer page 0x0C)
```

## Packages (planned, under `internal/`)

| Module | Responsibility | I/O |
|---|---|---|
| `atparser` | Split RFCOMM stream into AT lines, bounded length | none |
| `hfp` | Audio Gateway state machine: SLC handshake (`BRSF`, `CIND`, `CMER`, `CHLD`), call state / `+CIEV`, button events (`ATA`, `CHUP`, `CKPD`, `BLDN`, `BVRA`, `VGS`/`VGM`) | none |
| `mapping` | Headset event -> HID input reports (hook switch, drop, redial, volume); LED output report -> call state | none |
| `hiddesc` | Report descriptor + report packing/unpacking | none |
| `uhid` | `/dev/uhid` create/input/output events | uhid |
| `bluez` | Register HFP AG / HSP AG profiles via D-Bus, receive RFCOMM fds | D-Bus |
| `daemon` | Wire everything together (goroutines, context cancellation) | all |

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

PipeWire/WirePlumber must not claim the HFP/HSP profiles
(`bluez5.roles = [ a2dp_sink a2dp_source ]`), otherwise BlueZ refuses the registration.

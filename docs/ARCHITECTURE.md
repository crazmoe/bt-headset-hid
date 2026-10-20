# Architecture

```
Headset --BT/RFCOMM--> BlueZ --fd--> bt-headset-hid --/dev/uhid--> /dev/hidrawN
                                          |
                                     AT parser + SLC handshake (HFP/HSP Audio Gateway role)
                                          |
                                     AT -> HID mapping (Telephony page 0x0B, Consumer page 0x0C)
```

## Modules (planned)

| Module | Responsibility | I/O |
|---|---|---|
| `at_parser` | Split RFCOMM stream into AT lines, bounded length | none |
| `slc` | HFP service-level-connection handshake replies (`BRSF`, `CIND`, `CMER`, `CHLD`) | none |
| `mapping` | AT command -> HID input report (hook switch, mute, redial, volume) | none |
| `hid_descriptor` | Report descriptor + report packing/unpacking | none |
| `uhid` | `/dev/uhid` create/input/output events | uhid |
| `bluez` | Register HFP AG / HSP AG profiles via D-Bus, receive RFCOMM fds | D-Bus |
| `daemon` | Wire everything together, asyncio loop | all |

## Prerequisites on the host

PipeWire/WirePlumber must not claim the HFP/HSP profiles
(`bluez5.roles = [ a2dp_sink a2dp_source ]`), otherwise BlueZ refuses the registration.

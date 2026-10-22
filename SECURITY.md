# Security Policy

bt-headset-hid runs as root and reads AT commands from Bluetooth devices, so security problems
matter. Thank you for reporting them responsibly.

## Supported versions

Only the latest release gets security fixes. Please check that the problem still exists there or
on `main`.

## Reporting a vulnerability

Please do **not** open a public issue. Report it privately through GitHub:
[Report a vulnerability](https://github.com/crazmoe/bt-headset-hid/security/advisories/new)
(Security tab → "Report a vulnerability"). English or German is fine.

Helpful details:

- the affected version (`bt-headset-hid -version`) and mode (`pipewire` or `hfp`)
- what an attacker needs (a paired headset, a nearby unpaired device, a local user, ...)
- steps or a proof of concept, e.g. the AT commands or D-Bus calls that trigger it
- what happens (crash, hang, privilege escalation, ...)

You get an answer within a week. Once the problem is confirmed, a fix and a release follow as
soon as possible, and the advisory is published afterwards. You are credited unless you prefer
not to be.

## Scope

In scope is the code in this repository: the daemon, the `install`/`uninstall`/`wireplumber`
commands and the files in `contrib/`. Bugs in BlueZ, PipeWire, WirePlumber, the kernel or a
softphone belong to those projects; if bt-headset-hid makes them reachable, report it here too.

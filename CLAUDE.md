# bt-headset-hid

Daemon: Bluetooth headset (HFP/HSP AT commands) -> virtual HID telephony device (`/dev/uhid` -> `/dev/hidrawN`).
See `docs/ARCHITECTURE.md` for the design.

- Python >= 3.10, package in `src/bt_headset_hid/`, tests in `tests/`.
- Pure logic (AT parser, AT->HID mapping, report descriptor) must stay free of D-Bus/uhid I/O so it is testable without hardware.
- Run `ruff check .` and `pytest` before committing.
- Treat all AT input from the remote device as untrusted: whitelist commands, bound line length.

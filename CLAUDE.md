# bt-headset-hid

Daemon: Bluetooth headset (HFP/HSP AT commands) -> virtual HID telephony device (`/dev/uhid` -> `/dev/hidrawN`).
See `docs/ARCHITECTURE.md` for the design.

- Go (see `go.mod`), entry point in `cmd/bt-headset-hid/`, logic in `internal/<package>/` with tests next to the code.
- Prefer the standard library; the only planned dependency is `github.com/godbus/dbus/v5` for BlueZ.
- Pure logic (AT parser, HFP/HSP state machine, AT->HID mapping, report descriptor, modem call state) must stay free of D-Bus/uhid I/O so it is testable without hardware.
- Run `gofmt -l .`, `go vet ./...` and `go test -race ./...` before committing.
- `README.md` is the short English guide for non-technical users, `docs/ADVANCED.md` has the details; `README.de.md` and `docs/ADVANCED.de.md` are the German translations. Keep each pair in sync.
- Treat all AT input from the remote device as untrusted: whitelist commands, bound line length.

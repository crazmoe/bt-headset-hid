# bt-headset-hid

[English](README.md) · **Deutsch**

Macht die Tasten eines Bluetooth-Headsets (z. B. Jabra) so nutzbar wie mit einem USB-Headset-Dongle:
Der Daemon arbeitet als HFP/HSP Audio Gateway, empfängt die AT-Befehle des Headsets
(`ATA`, `AT+CHUP`, `AT+CKPD=200`, `AT+BLDN`, `AT+VGS`, ...) und stellt sie über `/dev/uhid`
als virtuelles HID-Telefoniegerät bereit (sichtbar als `/dev/hidrawN`).

**Status:** Projektgerüst, noch nicht funktionsfähig. Siehe [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Entwicklung

```
go vet ./...
go test -race ./...
go build ./cmd/bt-headset-hid
```

## Lizenz

MIT

# bt-headset-hid

Makes the buttons of a Bluetooth headset (e.g. Jabra) usable like a USB headset dongle:
the daemon acts as HFP/HSP Audio Gateway, receives the headset's AT commands
(`ATA`, `AT+CHUP`, `AT+CKPD=200`, `AT+BLDN`, `AT+VGS`, ...) and exposes them as a virtual
HID telephony device via `/dev/uhid` (visible as `/dev/hidrawN`).

**Status:** project scaffold, not functional yet. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Development

```
python -m venv .venv && . .venv/bin/activate
pip install -e ".[dev]"
ruff check . && pytest
```

## License

MIT

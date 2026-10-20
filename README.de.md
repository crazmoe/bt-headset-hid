# bt-headset-hid

[English](README.md) · **Deutsch**

Macht die Tasten eines Bluetooth-Headsets (z. B. Jabra) so nutzbar wie mit einem USB-Headset-Dongle:
Softphones (Teams, Zoom, Browser-Apps über WebHID, ...) sehen ein HID-Telefoniegerät
(`/dev/hidrawN`, angelegt über `/dev/uhid`), das Annehmen und Auflegen vom Headset meldet und
vom Softphone den Klingel- und Off-Hook-Zustand bekommt.

**Status:** Funktionen vollständig, noch nicht mit echter Hardware getestet.
Siehe [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) für den Aufbau.

## Zwei Modi

Telefon-Audio und die Headset-Tasten laufen beide über HFP, und nur ein Programm kann HFP
belegen. Deshalb wählst du:

| | `-mode pipewire` (Standard) | `-mode hfp` |
|---|---|---|
| Wer belegt HFP | PipeWire | bt-headset-hid |
| Telefon-Audio (Headset-Mikrofon und -Lautsprecher) | ja | nein (nur A2DP-Musik) |
| Annehmen / Auflegen / Ablehnen | ja | ja |
| Wahlwiederholung, Lautstärketasten, HSP-Taste | nein | ja |
| Funktionsweise | bt-headset-hid gibt sich als ModemManager-Modem aus; PipeWire leitet `ATA`/`AT+CHUP` daran weiter | bt-headset-hid registriert das HFP/HSP Audio Gateway bei BlueZ und wertet die AT-Befehle selbst aus |

Nimm `pipewire`, wenn du über das Headset telefonieren willst, so wie mit einem Dongle.

## Einrichtung

Voraussetzungen: Linux mit BlueZ, PipeWire mit WirePlumber, root für die Installation.

1. **Bauen und installieren**

   ```
   go build ./cmd/bt-headset-hid
   sudo install bt-headset-hid /usr/local/bin/
   ```

2. **PipeWire einrichten**: die passende Datei für deinen Modus kopieren und neu starten:

   ```
   mkdir -p ~/.config/wireplumber/wireplumber.conf.d
   cp contrib/wireplumber/51-bt-headset-hid-pipewire.conf ~/.config/wireplumber/wireplumber.conf.d/51-bt-headset-hid.conf
   # oder für -mode hfp: contrib/wireplumber/51-bt-headset-hid-hfp.conf
   systemctl --user restart wireplumber pipewire pipewire-pulse
   ```

   Das gilt für WirePlumber 0.5 und neuer (`wireplumber --version`). WirePlumber 0.4 ignoriert
   diese Dateien; dann stattdessen die `.lua`-Dateien aus `contrib/wireplumber/` nach
   `~/.config/wireplumber/bluetooth.lua.d/51-bt-headset-hid.lua` kopieren. Als normaler
   Benutzer kopieren, nicht mit `sudo`.

3. **Nur für `-mode pipewire`**: Der echte ModemManager darf nicht laufen (Mobilfunkmodems gehen
   dann nicht, solange bt-headset-hid läuft), und der Daemon braucht die Erlaubnis für dessen
   Busnamen:

   ```
   sudo systemctl disable --now ModemManager
   sudo cp contrib/dbus/bt-headset-hid.conf /etc/dbus-1/system.d/
   ```

   PipeWire muss mit ModemManager-Unterstützung gebaut sein (bei den meisten Distributionen der Fall).

4. **Deinem Benutzer Zugriff auf das HID-Gerät geben**, damit Softphones es öffnen können:

   ```
   sudo cp contrib/udev/70-bt-headset-hid.rules /etc/udev/rules.d/
   sudo udevadm control --reload
   ```

5. **Daemon starten**. Er läuft als root, weil er das Gerät über `/dev/uhid` anlegt:

   ```
   sudo cp contrib/systemd/bt-headset-hid.service /etc/systemd/system/
   # für -mode hfp dort ExecStart anpassen
   sudo systemctl daemon-reload
   sudo systemctl enable --now bt-headset-hid
   journalctl -u bt-headset-hid -f
   ```

6. **Headset koppeln und verbinden** wie gewohnt (`bluetoothctl` oder über den Desktop). Bei
   `-mode pipewire` für Anrufe das Profil „Headset Head Unit (HSP/HFP)“ wählen oder WirePlumber
   automatisch umschalten lassen, wenn das Softphone das Mikrofon öffnet.

7. **Gerät im Softphone auswählen.** Es heißt `bt-headset-hid`. Browser-Apps fragen einmal nach
   dem WebHID-Zugriff.

### Test ohne Softphone

```
grep -l bt-headset-hid /sys/class/hidraw/*/device/uevent    # z. B. .../hidraw5/...
# Terminal 1: Eingabe-Reports anzeigen (hexdump puffert 16 Byte, Reports sind 2 Byte lang)
sudo python3 -c 'import os; f=os.open("/dev/hidraw5", os.O_RDONLY); [print(os.read(f, 64).hex(" "), flush=True) for _ in iter(int, 1)]'
# Terminal 2: das Softphone spielen
printf '\x02\x02' | sudo tee /dev/hidraw5 >/dev/null   # Ring-LED: das Headset klingelt
printf '\x02\x01' | sudo tee /dev/hidraw5 >/dev/null   # Off-Hook-LED: Anruf aktiv
printf '\x02\x00' | sudo tee /dev/hidraw5 >/dev/null   # alle LEDs aus: kein Anruf
```

Annehmen am Headset zeigt `01 01` (Hook Switch an), Auflegen `01 00`. Wie bei einem USB-Dongle
klingelt das Headset weiter, bis der Rechner die Off-Hook-LED setzt; ein Softphone macht das,
sobald es den Hook Switch sieht.

### Fehlersuche

- `org.freedesktop.ModemManager1 is already owned`: ModemManager läuft noch (Schritt 3).
- `UUID already registered` oder `already provides the HFP/HSP Audio Gateway` bei `-mode hfp`:
  Ein anderes Programm belegt HFP noch. Meist ist Schritt 2 nicht aktiv: Datei für die falsche
  WirePlumber-Version, falsches Verzeichnis oder WirePlumber nicht neu gestartet. Außerdem
  prüfen, dass weder PulseAudio (`pactl info | grep "Server Name"` muss PipeWire nennen) noch
  oFono/hsphfpd (`systemctl status ofono hsphfpd`) läuft. Danach `sudo systemctl restart bt-headset-hid`.
- Tasten tun nichts bei `-mode pipewire`: prüfen, ob Schritt 2 aktiv ist
  (`wpctl status`, `journalctl --user -u wireplumber`), und das Headset neu verbinden.
- `create HID device: open /dev/uhid`: Der Daemon läuft nicht als root, oder das Kernelmodul
  `uhid` fehlt (`sudo modprobe uhid`).
- Manche Softphones akzeptieren nur HID-Geräte bekannter Hersteller; dann `-vendor`/`-product`
  ausprobieren (und die udev-Regel anpassen).

## Entwicklung

```
gofmt -l .
go vet ./...
go test -race ./...
go build ./cmd/bt-headset-hid
```

Tests, die `/dev/uhid` (root) oder `dbus-daemon` brauchen, werden übersprungen, wenn diese fehlen.

## Lizenz

MIT

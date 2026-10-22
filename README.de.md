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
| Telefon-Audio (Headset-Mikrofon und -Lautsprecher) | ja, als Lautsprecher und Mikrofon „bt-headset-hid“ | nein (nur A2DP-Musik) |
| Musik über das Headset | Telefonqualität (kein A2DP, das Headset bleibt anrufbereit) | A2DP |
| Annehmen / Auflegen / Ablehnen | ja | ja |
| Wahlwiederholung, Lautstärketasten, HSP-Taste | nein | ja |
| Anruftaste ausserhalb eines Anrufs, falls das Headset sie als AVRCP Play/Pause schickt | an den Medienplayer (`-grab-media-keys`: ans Softphone) | ans Softphone |
| Funktionsweise | bt-headset-hid gibt sich als ModemManager-Modem aus; PipeWire leitet `ATA`/`AT+CHUP` daran weiter | bt-headset-hid registriert das HFP/HSP Audio Gateway bei BlueZ und wertet die AT-Befehle selbst aus |

Nimm `pipewire`, wenn du über das Headset telefonieren willst, so wie mit einem Dongle. Dafür
braucht es **PipeWire 1.2 oder neuer** (`pipewire --version`): Ältere Versionen, z. B. 1.0.x in
Ubuntu 24.04 und darauf aufbauenden Distributionen, stürzen bei eingehenden Anrufen eines Modems ab,
das Headset klingelt also nicht und kann nicht abnehmen, und `install` verweigert `-mode pipewire`.
Dort `hfp` nehmen, ein neueres PipeWire, oder PipeWire 1.0.x unter Debian/Ubuntu mit dem
mitgelieferten Skript korrigieren, als normaler Benutzer:

```
contrib/pipewire-1.0-fix.sh            # nur das Bluetooth-Plugin mit der Korrektur neu bauen
contrib/pipewire-1.0-fix.sh --restore  # zurück zum Original-Plugin
```

Das Skript lädt die Quellen deiner PipeWire-Version, wendet die einzeilige Korrektur aus 1.2.0 an,
baut nur `libspa-bluez5.so` und ersetzt die installierte Datei. Ein PipeWire-Update deiner
Distribution überschreibt sie wieder; `install` merkt das und bittet, das Skript erneut auszuführen.

## Einrichtung

Voraussetzungen: Linux mit BlueZ und PipeWire mit WirePlumber.

1. **Herunterladen oder bauen**

   Fertige Programme für `amd64` und `arm64` gibt es auf der
   [Release-Seite](https://github.com/crazmoe/bt-headset-hid/releases). Entpacken und in den Ordner wechseln:

   ```
   tar xzf bt-headset-hid_*_linux_amd64.tar.gz
   cd bt-headset-hid_*_linux_amd64
   ```

   Oder selbst mit Go bauen:

   ```
   go build ./cmd/bt-headset-hid
   ```

2. **Installieren**, als dein normaler Desktop-Benutzer mit `sudo`:

   ```
   sudo ./bt-headset-hid install
   ```

   Der Befehl fragt nach dem Modus (oder `-mode pipewire` / `-mode hfp` angeben) und richtet dann
   alles ein: das Programm in `/usr/local/bin`, das Kernelmodul `uhid`, die udev-Regel, damit
   Softphones das Gerät öffnen dürfen, den systemd-Dienst mit dem gewählten Modus, bei `pipewire`
   die D-Bus-Policy und das Abschalten von ModemManager, und die WirePlumber-Konfiguration deines
   Benutzers (0.4 oder 0.5, wird automatisch erkannt). Bei `-mode pipewire` legt er ausserdem
   einen Lautsprecher und ein Mikrofon namens `bt-headset-hid` für das gekoppelte Headset an. Zum Moduswechsel einfach erneut ausführen;
   `sudo bt-headset-hid uninstall` entfernt alles wieder.

3. **Headset verbinden** wie gewohnt (`bluetoothctl` oder über den Desktop; war es schon
   verbunden, neu verbinden) und in `journalctl -u bt-headset-hid -f` auf „headset … connected“
   achten. Bei `-mode pipewire` bietet das Headset nur das Telefonprofil (HFP) an und ist so
   immer anrufbereit. Hast du es erst nach `install` gekoppelt, einmal
   `bt-headset-hid wireplumber` als normaler Benutzer ausführen, damit Lautsprecher und Mikrofon
   `bt-headset-hid` darauf zeigen.

4. **Gerät im Softphone auswählen.** Es heißt `bt-headset-hid`. Bei `-mode pipewire` auch als
   Lautsprecher und Mikrofon `bt-headset-hid` wählen: Die gibt es immer, sie leiten den Ton zum
   Headset, solange es verbunden ist, sonst zu den Standardgeräten. Browser-Apps fragen einmal nach
   dem WebHID-Zugriff.

### Einrichtung von Hand

Was `install` Schritt für Schritt macht, falls du es lieber selbst machst:

- `sudo install bt-headset-hid /usr/local/bin/`
- PipeWire: `bt-headset-hid wireplumber -mode pipewire` (oder `-mode hfp`) als normaler Benutzer.
  Das erkennt die WirePlumber-Version, schreibt die passende Datei (0.4: Lua in
  `~/.config/wireplumber/bluetooth.lua.d/`, ab 0.5: `~/.config/wireplumber/wireplumber.conf.d/`),
  entfernt die Datei der jeweils anderen Version und startet PipeWire neu; `-remove` macht es
  rückgängig. Von Hand: die passende Datei aus `contrib/wireplumber/` an den Pfad kopieren, der in
  ihren ersten Zeilen steht. Bei `-mode pipewire` schreibt es ausserdem
  `~/.config/pipewire/pipewire.conf.d/51-bt-headset-hid.conf` mit Lautsprecher und Mikrofon
  `bt-headset-hid` für das gekoppelte Headset (bei mehreren Headsets:
  `-headset XX:XX:XX:XX:XX:XX`).
- Nur für `-mode pipewire`: `sudo systemctl disable --now ModemManager` (Mobilfunkmodems gehen
  nicht, solange bt-headset-hid läuft) und
  `sudo cp contrib/dbus/bt-headset-hid.conf /etc/dbus-1/system.d/`. PipeWire muss mit
  ModemManager-Unterstützung gebaut sein (bei den meisten Distributionen der Fall).
- `sudo cp contrib/udev/70-bt-headset-hid.rules /etc/udev/rules.d/ && sudo udevadm control --reload`
- `echo uhid | sudo tee /etc/modules-load.d/bt-headset-hid.conf && sudo modprobe uhid`
- `sudo cp contrib/systemd/bt-headset-hid.service /etc/systemd/system/` (`-mode` in `ExecStart`
  anpassen), dann `sudo systemctl daemon-reload && sudo systemctl enable --now bt-headset-hid`.

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

- `org.freedesktop.ModemManager1 is already owned`: ModemManager läuft noch (`install` mit `-mode pipewire` erledigt das).
- `UUID already registered` oder `already provides the HFP/HSP Audio Gateway` bei `-mode hfp`:
  Ein anderes Programm belegt HFP noch. Meist ist die WirePlumber-Konfiguration nicht aktiv: Datei für die falsche
  WirePlumber-Version, falsches Verzeichnis oder WirePlumber nicht neu gestartet. Außerdem
  prüfen, dass weder PulseAudio (`pactl info | grep "Server Name"` muss PipeWire nennen) noch
  oFono/hsphfpd (`systemctl status ofono hsphfpd`) läuft. Danach `sudo systemctl restart bt-headset-hid`.
- `RFCOMM receive command but modem not available` oder `dbus_message_get_path() were incorrect` im
  WirePlumber-Log, das Lautsprechersymbol verschwindet kurz, oder das Headset klingelt bei
  `-mode pipewire` nicht: `pipewire --version` prüfen (1.2 oder neuer nötig oder das Skript oben),
  dann WirePlumber neu starten (`systemctl --user restart wireplumber`).
- Der Medienplayer reagiert bei `-mode pipewire` auf die Anruftaste: Mit
  `sudo evtest /dev/input/eventN` (das Gerät "<Headset> (AVRCP)") prüfen, ob der Druck dort
  ankommt. Wenn ja, übernimmt `-grab-media-keys` dieses Gerät exklusiv, Play/Pause wird so zur
  Anruftaste (im Journal steht `media keys of "<Headset> (AVRCP)" now go to the softphone`), und
  Vor/Zurück steuern vom Headset aus keine Musik mehr.
- Kein Ton im Gespräch bei `-mode pipewire`: Das Softphone muss `bt-headset-hid` (oder das
  Headset) als Lautsprecher und Mikrofon verwenden, sonst öffnet PipeWire keinen Gesprächston zum
  Headset. Nach einem Neustart von PipeWire auch das Softphone neu starten. `wpctl status` zeigt
  während eines Anrufs unter „Streams“, wohin sein Ton geht.
- Tasten tun nichts bei `-mode pipewire`: prüfen, ob die WirePlumber-Konfiguration aktiv ist
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

## Rückmeldungen

Fehler, Ideen und Testberichte sind als
[Issues](https://github.com/crazmoe/bt-headset-hid/issues/new/choose) willkommen, auf Deutsch oder Englisch.
Sicherheitsprobleme bitte vertraulich melden, siehe [SECURITY.md](SECURITY.md).

## Lizenz

MIT

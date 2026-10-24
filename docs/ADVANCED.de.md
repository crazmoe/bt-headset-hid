# bt-headset-hid: Details

[English](ADVANCED.md) · **Deutsch**

Die Kurzanleitung für den üblichen Fall steht im [README](../README.de.md). Diese Seite hat alles
andere: die Modi, die Einrichtung ohne Paket oder von Hand, Tests, Fehlersuche und Entwicklung.
Siehe [ARCHITECTURE.md](ARCHITECTURE.md) für den Aufbau (auf Englisch).

## Drei Modi

Telefon-Audio und die Headset-Tasten laufen beide über HFP, und nur ein Programm kann HFP
belegen. Deshalb wählst du:

| | `-mode pipewire` | `-mode hfp` | `-mode ofono` (Standard) |
|---|---|---|---|
| Wer belegt HFP | PipeWire | bt-headset-hid | bt-headset-hid, PipeWire holt den Ton bei ihm ab |
| Telefon-Audio (Headset-Mikrofon und -Lautsprecher) | ja, als Lautsprecher und Mikrofon „bt-headset-hid“ | nein (nur A2DP-Musik) | ja, als Lautsprecher und Mikrofon „bt-headset-hid“, in HD-Qualität (mSBC), wo das Headset es kann |
| Musik über das Headset | A2DP; für ein Gespräch wechselt es ins Telefonprofil und pausiert die Musik (`-profile phone`: immer Telefonqualität) | A2DP | wie bei `pipewire` |
| Annehmen / Auflegen / Ablehnen | ja | ja | ja |
| Wahlwiederholung, Lautstärketasten, HSP-Taste | nein | ja | ja |
| Anruftaste ausserhalb eines Anrufs, falls das Headset sie als AVRCP Play/Pause schickt | an den Medienplayer (`-grab-media-keys`: ans Softphone) | ans Softphone | wie bei `pipewire` |
| Mobilfunk (ModemManager) | nein, wird abgeschaltet | ja | ja |
| PipeWire-Version | 1.2 oder neuer (oder die Korrektur unten) | beliebig | beliebig, mit oFono-Backend gebaut (Standard) |
| Funktionsweise | bt-headset-hid gibt sich als ModemManager-Modem aus; PipeWire leitet `ATA`/`AT+CHUP` daran weiter | bt-headset-hid registriert das HFP/HSP Audio Gateway bei BlueZ und wertet die AT-Befehle selbst aus | wie bei `hfp`, und bt-headset-hid gibt sich als oFono aus, sodass PipeWires oFono-Backend den Gesprächston bei ihm abholt |

Nimm `ofono` (Standard), wenn du über das Headset telefonieren willst, so wie mit einem Dongle:
bt-headset-hid bedient das Headset wie bei `hfp` und gibt den Gesprächston über die Schnittstelle
von oFono (`org.ofono`) an PipeWire weiter; oFono selbst wird nicht gebraucht und darf nicht laufen.
ModemManager läuft weiter, ein Mobilfunkmodem (WWAN) funktioniert also weiterhin, und jede
PipeWire-Version geht.

`pipewire` war vor `ofono` der Standard und bleibt als Alternative. Dafür
braucht es **PipeWire 1.2 oder neuer** (`pipewire --version`): Ältere Versionen, z. B. 1.0.x in
Ubuntu 24.04 und darauf aufbauenden Distributionen, stürzen bei eingehenden Anrufen eines Modems ab,
das Headset klingelt also nicht und kann nicht abnehmen, und `install` verweigert `-mode pipewire`.
Dort `ofono` oder `hfp` nehmen, ein neueres PipeWire, oder PipeWire 1.0.x unter Debian/Ubuntu mit dem
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

1. **Paket installieren**

   Auf der [Release-Seite](https://github.com/crazmoe/bt-headset-hid/releases) gibt es Pakete für
   `amd64` und `arm64`. Unter Debian/Ubuntu oder Fedora/openSUSE das Paket installieren:

   ```
   sudo apt install ./bt-headset-hid_*_amd64.deb      # Debian, Ubuntu
   sudo dnf install ./bt-headset-hid-*.x86_64.rpm     # Fedora (openSUSE: zypper install)
   ```

   Das ist schon die ganze Einrichtung. Das Paket wählt den Modus selbst (siehe Schritt 2),
   richtet den Systemdienst ein und danach das PipeWire jedes Desktop-Benutzers: sofort für die
   angemeldeten, für die anderen bei der nächsten Anmeldung. Eine Desktop-Benachrichtigung meldet,
   wenn es bereit ist. Sind mehrere Headsets gekoppelt, richtet es die verbundenen ein, sonst
   alle; `bt-headset-hid wireplumber` wählt andere. Das Paket trägt ausserdem seine Paketquelle
   ein, sodass neue Versionen mit den Systemupdates kommen. Wer das Paket entfernt, macht alles
   rückgängig. Danach als normaler Benutzer die Einrichtung prüfen:

   ```
   bt-headset-hid doctor
   ```

   Das prüft den Dienst, ältere Kopien des Programms, die das Paket verdecken, Bluetooth, die gekoppelten Headsets, Programme, die sich in die Quere
   kommen (oFono, ModemManager, PulseAudio), PipeWire und deine WirePlumber-Konfiguration, und gibt
   zu jedem gefundenen Problem den Befehl aus, der es behebt (auf Deutsch, wenn dein Desktop auf
   Deutsch eingestellt ist). Der Benutzerdienst macht dieselben Prüfungen ab und zu selbst und
   meldet jedes neue Problem mit der Lösung als Desktop-Benachrichtigung.

   Der Benutzerdienst zeigt ausserdem ein Symbol im Infobereich auf Desktops mit Infobereich (KDE,
   Xfce, Cinnamon, MATE, LXQt, waybar; GNOME braucht die AppIndicator-Erweiterung, die Ubuntu
   mitbringt). Es zeigt, ob ein Headset verbunden ist, einen Anruf, oder ein Warnzeichen, solange
   `doctor` ein Problem findet. Sein Menü listet die Probleme (ein Klick zeigt die Lösung), die
   gekoppelten Headsets, bei denen du in den Modi `pipewire` und `ofono` die für Anrufe abhakst (wie
   mit `-headset`, ohne Neustart), „Jetzt prüfen“ und „Hilfe“. Unter Cinnamon (Linux Mint) braucht
   die Leiste dafür das „XApp-Status-Applet“ (Rechtsklick auf die Leiste, „Applets“); ohne es
   erscheint von keinem Programm ein Tray-Symbol.

   Neben jedem verbundenen Headset zeigt das Menü den Akkustand, wenn er bekannt ist, und unter
   20 % warnt einmal eine Benachrichtigung. In den Modi `ofono` und `hfp` liest bt-headset-hid den
   Stand selbst vom Headset (HFP-Indikatoren `AT+BIEV`, Apples `AT+IPHONEACCEV` oder Plantronics'
   `AT+XEVENT`) und gibt ihn an BlueZ weiter, sodass ihn auch die Bluetooth-Einstellungen des
   Desktops zeigen; BlueZ-Versionen, die ihn nur mit `--experimental` annehmen, protokollieren
   `BlueZ does not take the battery level` (im Modus `ofono` zeigt ihn das Tray-Symbol trotzdem).
   Im Modus `pipewire` liest ihn PipeWire.

2. **Ohne Paket**, auf anderen Distributionen, das Tarball nehmen, entpacken und in den Ordner
   wechseln, oder selbst mit Go bauen:

   ```
   tar xzf bt-headset-hid_*_linux_amd64.tar.gz
   cd bt-headset-hid_*_linux_amd64
   go build ./cmd/bt-headset-hid                      # stattdessen: selbst bauen
   ```

   Dann als dein normaler Desktop-Benutzer mit `sudo` installieren:

   ```
   sudo ./bt-headset-hid install
   ```

   Der Befehl wählt den Modus selbst und sagt, welchen: `ofono`, ausser der oFono-Dienst läuft
   oder deinem PipeWire fehlt das oFono-Backend; dann `pipewire`, wenn dein PipeWire es kann
   (siehe oben) und ModemManager nicht gebraucht wird, sonst `hfp`. Erneut ausgeführt behält er
   den Modus; `-mode pipewire`, `-mode hfp` oder `-mode ofono` wählt ihn selbst (auch mit dem
   Paket: `sudo bt-headset-hid install -mode hfp`). Er richtet alles ein: das Programm in
   `/usr/local/bin`, das Kernelmodul `uhid`, die udev-Regel, damit Softphones das Gerät öffnen
   dürfen, den systemd-Dienst mit dem Modus, bei `pipewire` die D-Bus-Policy und das Abschalten
   von ModemManager, bei `ofono` die D-Bus-Policy und das erneute Starten von ModemManager, und
   die WirePlumber-Konfiguration deines Benutzers (0.4 oder 0.5, wird automatisch erkannt). Bei
   `-mode pipewire` und `-mode ofono` legt er ausserdem einen Lautsprecher und ein Mikrofon namens
   `bt-headset-hid` für das gekoppelte Headset an; sind mehrere gekoppelt, listet er sie auf und
   fragt, welche du zum Telefonieren nutzt (Enter nimmt die verbundenen, sonst alle).
   `sudo bt-headset-hid uninstall` entfernt alles wieder. Zum Schluss die Einrichtung mit
   `bt-headset-hid doctor` prüfen wie in Schritt 1.

3. **Headset verbinden** wie gewohnt (`bluetoothctl` oder über den Desktop; war es schon
   verbunden, neu verbinden). `bt-headset-hid doctor` sollte es jetzt als verbunden zeigen; das
   Protokoll steht in `journalctl -u bt-headset-hid -f` („headset … connected“). Bei `-mode pipewire` und `-mode ofono` spielt das Headset Musik (A2DP). Klingelt es oder rufst du an,
   pausiert der Benutzerdienst `bt-headset-hid-session` laufende Player, schaltet das Headset ins
   Telefonprofil (HFP) und nach dem Gespräch zurück und setzt die Player fort
   (`journalctl --user -u bt-headset-hid-session -f`). Bei `-mode ofono` schaltet er erst beim
   Abnehmen um, und nur das Headset, mit dem du abnimmst; solange es klingelt, piepsen die Headsets
   selbst. Pausiert wird über MPRIS, das die meisten Player und Browser können. Ein Headset, das du erst
   später koppelst, wird ein paar Sekunden nach dem Koppeln von selbst eingerichtet, ganz ohne
   Neustart (eine Benachrichtigung meldet es, im Protokoll des Benutzerdienstes steht
   `newly paired headset ... set up`). Headsets, die bei der Einrichtung schon gekoppelt waren und
   nicht eingerichtet wurden, bleiben aussen vor. Ein Headset, das du entkoppelst (in den
   Bluetooth-Einstellungen entfernst), wird von selbst ausgetragen, ohne Neustart, und beim
   erneuten Koppeln nicht wieder eingerichtet.

4. **Gerät im Softphone auswählen.** Es heißt `bt-headset-hid`. Bei `-mode pipewire` und `-mode ofono` auch als
   Lautsprecher und Mikrofon `bt-headset-hid` wählen: Die gibt es immer, sie leiten den Ton zum
   Headset, solange es verbunden ist, sonst zu den Standardgeräten. Browser-Apps fragen einmal nach
   dem WebHID-Zugriff.

### Einrichtung von Hand

Was `install` Schritt für Schritt macht, falls du es lieber selbst machst:

- `sudo install bt-headset-hid /usr/local/bin/`
- PipeWire: `bt-headset-hid wireplumber -mode pipewire` (oder `-mode hfp`, `-mode ofono`) als normaler Benutzer.
  Das erkennt die WirePlumber-Version, schreibt die passende Datei (0.4: Lua in
  `~/.config/wireplumber/bluetooth.lua.d/`, ab 0.5: `~/.config/wireplumber/wireplumber.conf.d/`),
  entfernt die Datei der jeweils anderen Version und startet PipeWire neu; `-remove` macht es
  rückgängig, und der Benutzerdienst des Pakets lässt dein PipeWire von da an in Ruhe, bis du
  wieder `bt-headset-hid wireplumber` ausführst. Ohne `-mode` bleibt der bisher eingerichtete Modus. Von Hand: die passende Datei aus `contrib/wireplumber/` an den Pfad kopieren, der in
  ihren ersten Zeilen steht. Bei `-mode pipewire` und `-mode ofono` richtet es ausserdem die Headsets ein: Ihre Liste kommt
  nach `~/.config/bt-headset-hid/headsets`, `~/.config/pipewire/pipewire.conf.d/51-bt-headset-hid.conf`
  legt Lautsprecher und Mikrofon `bt-headset-hid` an, und der Benutzerdienst
  `~/.config/systemd/user/bt-headset-hid-session.service` (mit dem Paket
  `/usr/lib/systemd/user/bt-headset-hid-session.service`, für jeden Benutzer) schaltet die Headsets für Gespräche ins
  Telefonprofil und richtet Lautsprecher und Mikrofon `bt-headset-hid` im laufenden Betrieb auf
  den Gesprächston des verbundenen aus, sodass eine Änderung der Liste keinen Neustart braucht
  (sind zwei verbunden, geht das Gespräch bei `-mode ofono` an das, mit dem du abnimmst oder
  wählst, sodass die Standardausgabe z. B. ein Bluetooth-Lautsprecher bleiben kann; sonst an das,
  das in den Toneinstellungen als Ausgabe gewählt ist; mit `-profile phone` an das erste der Liste). Es nimmt die bisher eingerichteten
  Headsets, sonst das gekoppelte, und fragt nach, wenn mehrere gekoppelt sind (ohne Terminal nimmt
  es die verbundenen, sonst alle); um mehrere einzurichten, etwa eines fürs Büro und eines für zu
  Hause, oder zum Wechseln: `-headset XX:XX:XX:XX:XX:XX,YY:YY:YY:YY:YY:YY`. Die in dem Moment
  gekoppelten Headsets und die, die du mit `-headset` weglässt, merkt es sich in
  `~/.config/bt-headset-hid/known-headsets`; sie werden auch nach erneutem Koppeln nicht wieder
  eingerichtet. Ein Headset, das später zum ersten Mal gekoppelt wird, steht nicht darauf, und der
  Benutzerdienst richtet es von selbst ein. Ausserdem schaltet es WirePlumbers eigenen
  Wechsel ins Telefonprofil ab (0.4: `policy.lua.d/52-bt-headset-hid-session.lua`, 0.5:
  `wireplumber.conf.d/52-bt-headset-hid-headset.conf`), der bei einem
  Gespräch jedes Bluetooth-Headset umschalten würde. Mit `-profile phone` bleiben die Headsets
  stattdessen immer im Telefonprofil; der Benutzerdienst schaltet sie beim Verbinden dorthin
  (`-profile switch` schaltet zurück). PipeWire startet der Befehl nur neu, wenn sich eine Datei
  von WirePlumber oder PipeWire geändert hat; die WirePlumber-Regeln pro Headset älterer
  Versionen entfernt er dabei.
- Nur für `-mode pipewire`: `sudo systemctl disable --now ModemManager` (Mobilfunkmodems gehen
  nicht, solange bt-headset-hid läuft) und
  `sudo cp contrib/dbus/bt-headset-hid.conf /etc/dbus-1/system.d/`. PipeWire muss mit
  ModemManager-Unterstützung gebaut sein (bei den meisten Distributionen der Fall).
- Nur für `-mode ofono`: `sudo cp contrib/dbus/bt-headset-hid-ofono.conf /etc/dbus-1/system.d/bt-headset-hid.conf`
  (statt der Datei oben). Hat `-mode pipewire` ModemManager abgeschaltet:
  `sudo systemctl enable --now ModemManager`.
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

Am besten mit `bt-headset-hid doctor` anfangen (als normaler Benutzer, oder mit `sudo`, um auch das
Protokoll des Dienstes zu lesen): Es findet die meisten der folgenden Probleme und nennt die Lösung.

- `org.freedesktop.ModemManager1 is already owned`: ModemManager läuft noch (`install` mit `-mode pipewire` erledigt das).
- `org.ofono is already owned` bei `-mode ofono`: Das echte oFono läuft
  (`sudo systemctl disable --now ofono`).
- Kein Gesprächston bei `-mode ofono`: `journalctl -u bt-headset-hid` sollte nach dem Start
  `audio agent ... registered` zeigen und beim Gespräch `audio for ...: codec 1` (oder 2). Fehlt die
  erste Zeile, nutzt PipeWire sein oFono-Backend nicht: prüfen, ob die WirePlumber-Konfiguration
  aktiv ist und dein PipeWire es enthält (`grep -c HandsfreeAudioManager` auf `libspa-bluez5.so`,
  meist in `/usr/lib/*/spa-0.2/bluez5/`). Hat das erste Gespräch nach dem Verbinden keinen Ton
  oder steht im Journal `no wideband audio link`, hat Headset oder Adapter Mühe mit HD-Sprache;
  dann `bluez5.enable-msbc` in der WirePlumber-Datei auf `false` setzen und WirePlumber neu starten.
- `UUID already registered` oder `already provides the HFP/HSP Audio Gateway` bei `-mode hfp` oder `-mode ofono`:
  Ein anderes Programm belegt HFP noch. Meist ist die WirePlumber-Konfiguration nicht aktiv: Datei für die falsche
  WirePlumber-Version, falsches Verzeichnis oder WirePlumber nicht neu gestartet. Außerdem
  prüfen, dass weder PulseAudio (`pactl info | grep "Server Name"` muss PipeWire nennen) noch
  oFono/hsphfpd (`systemctl status ofono hsphfpd`) läuft. Danach `sudo systemctl restart bt-headset-hid`.
- `RFCOMM receive command but modem not available` oder `dbus_message_get_path() were incorrect` im
  WirePlumber-Log, das Lautsprechersymbol verschwindet kurz, oder das Headset klingelt bei
  `-mode pipewire` nicht: `pipewire --version` prüfen (1.2 oder neuer nötig oder das Skript oben),
  dann WirePlumber neu starten (`systemctl --user restart wireplumber`).
- Der Medienplayer reagiert bei `-mode pipewire` oder `-mode ofono` auf die Anruftaste: Mit
  `sudo evtest /dev/input/eventN` (das Gerät "<Headset> (AVRCP)") prüfen, ob der Druck dort
  ankommt. Wenn ja, übernimmt `-grab-media-keys` dieses Gerät exklusiv, Play/Pause wird so zur
  Anruftaste (im Journal steht `media keys of "<Headset> (AVRCP)" now go to the softphone`), und
  Vor/Zurück steuern vom Headset aus keine Musik mehr.
- Das Headset bleibt bei `-mode pipewire` oder `-mode ofono` im Gespräch im Musikprofil: `systemctl --user status
  bt-headset-hid-session` und dessen Journal prüfen. Als Ausweg hält
  `bt-headset-hid wireplumber -profile phone` es im Telefonprofil.
- Kein Ton im Gespräch bei `-mode pipewire` oder `-mode ofono`: Das Softphone muss `bt-headset-hid` (oder das
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

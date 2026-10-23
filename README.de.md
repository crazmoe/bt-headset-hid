# bt-headset-hid

[English](README.md) · **Deutsch**

Macht die Tasten eines Bluetooth-Headsets (z. B. Jabra) in Softphones wie Teams, Zoom oder
Browser-Apps nutzbar, so wie mit einem USB-Headset-Dongle: annehmen, auflegen und über das
Headset sprechen.

**Status:** Beta. Voraussetzungen: Linux mit Bluetooth und PipeWire (das Standard-Tonsystem
aktueller Distributionen).

## In 3 Schritten

1. **Paket installieren.** Das Paket für dein System von der
   [Download-Seite](https://crazmoe.github.io/bt-headset-hid/) herunterladen (die meisten
   Rechner: `amd64` bzw. `x86_64`) und in einem Terminal installieren:

   ```
   sudo apt install ./bt-headset-hid_*_amd64.deb      # Debian, Ubuntu, Linux Mint
   sudo dnf install ./bt-headset-hid-*.x86_64.rpm     # Fedora
   ```

   Damit ist alles von selbst eingerichtet. Eine Desktop-Benachrichtigung meldet, wenn es bereit
   ist. Neue Versionen kommen mit den normalen Systemupdates.

2. **Headset verbinden** wie gewohnt in den Bluetooth-Einstellungen. War es schon verbunden,
   trennen und neu verbinden.

3. **Im Softphone `bt-headset-hid` auswählen**, als Telefoniegerät (Teams und Zoom: in den
   Audio-Einstellungen) und auch als Lautsprecher und Mikrofon, falls sie in der Liste stehen.
   Browser-Apps fragen einmal nach dem Zugriff auf das Gerät; erlauben.

Fertig: Die Anruftaste am Headset nimmt jetzt ab und legt auf.

## Etwas geht nicht?

Als normaler Benutzer (ohne `sudo`) ausführen:

```
bt-headset-hid doctor
```

Das prüft die Einrichtung und nennt zu jedem gefundenen Problem den Befehl, der es behebt.
Hört etwas auf zu funktionieren, kommt ausserdem eine Desktop-Benachrichtigung mit der Lösung.

Hilft das nicht, bitte ein [Issue](https://github.com/crazmoe/bt-headset-hid/issues/new/choose)
eröffnen (auf Deutsch oder Englisch) mit Headset-Modell, Distribution und der Ausgabe von
`bt-headset-hid doctor`. Testberichte, was funktioniert, sind auch willkommen.

## Entfernen

```
sudo apt remove bt-headset-hid      # Debian, Ubuntu, Linux Mint
sudo dnf remove bt-headset-hid      # Fedora
```

Das macht alle Änderungen rückgängig.

## Mehr

- [docs/ADVANCED.de.md](docs/ADVANCED.de.md): die drei Modi, Einrichtung ohne Paket und von Hand,
  die Befehle und Optionen, ausführliche Fehlersuche, Entwicklung.
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): wie es innen funktioniert (auf Englisch).
- [docs/REPOSITORY.md](docs/REPOSITORY.md): die Paketquelle (auf Englisch).
- [CHANGELOG.md](CHANGELOG.md): was sich geändert hat (auf Englisch).

Sicherheitsprobleme bitte vertraulich melden, siehe [SECURITY.md](SECURITY.md).

## Lizenz

MIT

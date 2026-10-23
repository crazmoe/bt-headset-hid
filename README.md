# bt-headset-hid

**English** · [Deutsch](README.de.md)

Makes the buttons of a Bluetooth headset (e.g. Jabra) work in softphones like Teams, Zoom or
browser apps, as with a USB headset dongle: answer, hang up and talk through the headset.

**Status:** beta. Requirements: Linux with Bluetooth and PipeWire (the default sound system of
current distributions).

## In 3 steps

1. **Install the package.** Download the package for your system from the
   [download page](https://crazmoe.github.io/bt-headset-hid/) (most computers: `amd64` or
   `x86_64`) and install it in a terminal:

   ```
   sudo apt install ./bt-headset-hid_*_amd64.deb      # Debian, Ubuntu, Linux Mint
   sudo dnf install ./bt-headset-hid-*.x86_64.rpm     # Fedora
   ```

   That sets up everything by itself. A desktop notification says when it is ready. Later
   versions come with your normal system updates.

2. **Connect the headset** as usual in the Bluetooth settings. If it was already connected,
   disconnect it and connect it again.

3. **Choose `bt-headset-hid` in the softphone** as the phone device (Teams and Zoom: in the
   audio settings), and also as speaker and microphone if they are in the list. Browser apps
   ask once for access to the device; allow it.

Done: the call button on the headset now answers and hangs up.

## Something does not work?

Run this as your normal user (without `sudo`):

```
bt-headset-hid doctor
```

It checks the setup and prints the command that fixes each problem it finds. You also get a
desktop notification when something stops working, with the fix.

If that does not help, please open an [issue](https://github.com/crazmoe/bt-headset-hid/issues/new/choose)
(in English or German) with your headset model, your distribution and the output of
`bt-headset-hid doctor`. Test reports of what works are welcome too.

## Removing it

```
sudo apt remove bt-headset-hid      # Debian, Ubuntu, Linux Mint
sudo dnf remove bt-headset-hid      # Fedora
```

This undoes all changes.

## More

- [docs/ADVANCED.md](docs/ADVANCED.md): the three modes, setup without the package and by hand,
  the commands and options, troubleshooting in detail, development.
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): how it works inside.
- [docs/REPOSITORY.md](docs/REPOSITORY.md): the package repository.
- [CHANGELOG.md](CHANGELOG.md): what changed.

Please report security problems privately, see [SECURITY.md](SECURITY.md).

## License

MIT

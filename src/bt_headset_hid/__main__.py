import sys

from . import __version__


def main() -> int:
    print(f"bt-headset-hid {__version__}: not implemented yet", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())

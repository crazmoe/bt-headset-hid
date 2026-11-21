#!/bin/sh
# Check that THIRD_PARTY_LICENSES and packaging/copyright cover what the Go
# toolchain built into the program: every module with its version and its
# license text, and the license and patent grant of the Go standard library.
# Run by build.sh, so a new or updated dependency without its notices stops
# the release.
#
#   packaging/check-licenses.sh build/.../bt-headset-hid ...
set -eu
[ $# -gt 0 ] || { echo "usage: packaging/check-licenses.sh PROGRAM..." >&2; exit 2; }
cd "$(dirname "$0")/.."

notices=$(cat THIRD_PARTY_LICENSES)
copyright=$(cat packaging/copyright)
failed=0
fail() {
	echo "check-licenses: $*" >&2
	failed=1
}
# has TEXT WHAT: the license TEXT occurs in THIRD_PARTY_LICENSES as it is,
# and in the Debian copyright file without its copyright lines (up to the
# first empty line), as a DEP-5 field value: indented, "." for empty lines.
has() {
	dep5=$(printf '%s\n' "$1" | sed '1,/^$/d; s/^$/./; s/^/ /')
	case "$notices" in *"$1"*) ;; *) fail "THIRD_PARTY_LICENSES lacks $2" ;; esac
	case "$copyright" in *"$dep5"*) ;; *) fail "packaging/copyright lacks $2" ;; esac
}

goroot=$(go env GOROOT)
has "$(cat "$goroot/LICENSE")" "the Go license ($goroot/LICENSE)"
has "$(cat "$goroot/PATENTS")" "the Go patent grant ($goroot/PATENTS)"

for prog in "$@"; do
	go version -m "$prog" | awk '$1 == "dep" || $1 == "=>" { print $1, $2, $3 }' | {
		while read -r kind path version; do
			if [ "$kind" = "=>" ]; then
				echo "check-licenses: $prog uses a replaced module ($path), check its license by hand" >&2
				exit 1
			fi
			case "$notices" in *"$path $version"*) ;; *) fail "THIRD_PARTY_LICENSES lacks $path $version" ;; esac
			case "$copyright" in *"$path $version"*) ;; *) fail "packaging/copyright lacks $path $version" ;; esac
			dir=$(go mod download -json "$path@$version" | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p')
			lic=
			for f in "$dir"/LICENSE* "$dir"/COPYING*; do
				[ -f "$f" ] && { lic=$f; break; }
			done
			if [ -z "$lic" ]; then
				fail "$path $version has no LICENSE file"
				continue
			fi
			has "$(cat "$lic")" "the license text of $path"
		done
		exit "$failed"
	} || failed=1
done
[ "$failed" -eq 0 ] || exit 1
echo "check-licenses: ok"

#!/bin/sh
# Build the release archives and the deb and rpm packages of VERSION for
# amd64 and arm64 into dist/. Used by .github/workflows/release.yml and, to
# catch packaging mistakes before a release, by ci.yml.
#
#   packaging/build.sh 1.0.0
set -eu
version=${1:?usage: packaging/build.sh VERSION}
cd "$(dirname "$0")/.."
rm -rf build dist
mkdir build dist

for arch in amd64 arm64; do
	name="bt-headset-hid_${version}_linux_${arch}"
	mkdir -p "build/$name/docs"
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
		-ldflags "-s -w -X main.version=$version" \
		-o "build/$name/bt-headset-hid" ./cmd/bt-headset-hid
	cp -r LICENSE THIRD_PARTY_LICENSES README.md README.de.md CHANGELOG.md contrib "build/$name/"
	cp docs/ADVANCED.md docs/ADVANCED.de.md "build/$name/docs/"
	tar -C build -czf "dist/$name.tar.gz" "$name"
done
# THIRD_PARTY_LICENSES and packaging/copyright must cover what went into the
# programs.
packaging/check-licenses.sh build/bt-headset-hid_"${version}"_linux_*/bt-headset-hid

go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.49.0
nfpm="$(go env GOPATH)/bin/nfpm"
# With the repository key, the packages bring the package repository (see
# docs/REPOSITORY.md).
cp packaging/nfpm.yaml build/nfpm.yaml
if [ -f packaging/repo-key.asc ]; then
	cat packaging/nfpm-repo.yaml >> build/nfpm.yaml
fi
export VERSION="$version"
for ARCH in amd64 arm64; do
	export ARCH
	cp "build/bt-headset-hid_${version}_linux_${ARCH}/bt-headset-hid" build/bt-headset-hid
	for fmt in deb rpm; do
		"$nfpm" package -f build/nfpm.yaml -p "$fmt" -t dist/
	done
done
(cd dist && sha256sum -- * > SHA256SUMS)

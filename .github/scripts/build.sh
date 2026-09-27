#!/usr/bin/env bash
# Builds a release archive of sptui for this machine, as
# dist/sptui_<os>_<arch>.tar.gz. install.sh downloads these.
#
#   .github/scripts/build.sh [version]
#
# The audio codecs (ogg, vorbis, flac, mpg123) are linked in statically, so
# the binary needs nothing but the system's libasound on Linux, and nothing
# at all on macOS. On Linux, build it on an old distro (Debian stable) so it
# runs against any glibc since.
#
# Needs Go, a C compiler, pkg-config and the codecs' static libraries: from
# Homebrew on macOS; on Linux, libogg-dev libvorbis-dev libflac-dev
# libasound2-dev, plus make and bzip2 to build mpg123 (Debian ships no static
# libmpg123).
set -euo pipefail

VERSION=${1:-dev}
MPG123_VERSION=1.33.7

root=$(cd "$(dirname "$0")/../.." && pwd)
os=$(go env GOOS)
arch=$(go env GOARCH)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

if [[ $os == linux ]]; then
	echo "building mpg123 $MPG123_VERSION"
	curl -fsSL "https://www.mpg123.de/download/mpg123-${MPG123_VERSION}.tar.bz2" | tar -xj -C "$work"
	(
		cd "$work/mpg123-${MPG123_VERSION}"
		./configure --quiet --prefix="$work/mpg123" --enable-static --disable-shared --with-pic \
			--disable-components --enable-libmpg123
		make -s -j"$(getconf _NPROCESSORS_ONLN)" install
	) >/dev/null
	export PKG_CONFIG_PATH="$work/mpg123/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
fi

# A directory holding only the codecs' static libraries, searched before any
# other, so the linker picks them over the shared ones.
mkdir "$work/static"
for pair in ogg:ogg vorbis:vorbis vorbisenc:vorbisenc flac:FLAC libmpg123:mpg123; do
	pc=${pair%%:*}
	lib="$(pkg-config --variable=libdir "$pc")/lib${pair#*:}.a"
	[[ -f $lib ]] || {
		echo "no static library for $pc: $lib" >&2
		exit 1
	}
	ln -s "$lib" "$work/static/"
done

# With --static, pkg-config also lists what each codec itself links against.
real_pkg_config=$(command -v pkg-config)
printf '#!/bin/sh\nexec %q --static "$@"\n' "$real_pkg_config" >"$work/pkg-config"
chmod +x "$work/pkg-config"

mkdir "$work/out"
(
	cd "$root"
	CGO_ENABLED=1 PKG_CONFIG="$work/pkg-config" CGO_LDFLAGS="-L$work/static" \
		go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$VERSION" \
		-o "$work/out/sptui" ./cmd/sptui
)

# Make sure none of the codecs were linked dynamically after all.
if [[ $os == linux ]]; then
	deps=$(ldd "$work/out/sptui")
else
	deps=$(otool -L "$work/out/sptui")
fi
echo "$deps"
if grep -Ei 'ogg|vorbis|flac|mpg123' <<<"$deps"; then
	echo "codecs are linked dynamically" >&2
	exit 1
fi
"$work/out/sptui" -version

mkdir -p "$root/dist"
tar -czf "$root/dist/sptui_${os}_${arch}.tar.gz" -C "$work/out" sptui
echo "built dist/sptui_${os}_${arch}.tar.gz"

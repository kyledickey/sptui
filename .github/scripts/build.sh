#!/usr/bin/env bash
# Builds a release archive of sptui for this machine, as
# dist/sptui_<os>_<arch>.tar.gz, or .zip on Windows. install.sh and
# install.ps1 download these.
#
#   .github/scripts/build.sh [version]
#
# The audio codecs (ogg, vorbis, flac, mpg123) are linked in statically, so
# the binary needs nothing but the system's libasound on Linux, and nothing
# at all on macOS or Windows. On Linux, build it on an old distro (Debian
# stable) so it runs against any glibc since.
#
# Needs Go, a C compiler, pkg-config and the codecs' static libraries: from
# Homebrew on macOS; on Linux, libogg-dev libvorbis-dev libflac-dev
# libasound2-dev, plus make and bzip2 to build mpg123 (Debian ships no static
# libmpg123); on Windows, MSYS2's UCRT64 gcc, pkgconf, libogg, libvorbis,
# flac and mpg123, plus zip, run from its bash.
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
	cp "$lib" "$work/static/"
done

# With --static, pkg-config also lists what each codec itself links against.
real_pkg_config=$(command -v pkg-config)
ldflags="-s -w -X main.version=$VERSION"
bin=sptui
if [[ $os == windows ]]; then
	# Go runs PKG_CONFIG itself, and Windows can't run a shell script, so
	# the wrapper is a batch file, and paths are Windows paths.
	printf '@"%s" --static %%*\r\n' "$(cygpath -w "$real_pkg_config")" >"$work/pkg-config.cmd"
	pkg_config=$(cygpath -w "$work/pkg-config.cmd")
	static=$(cygpath -m "$work/static")
	out=$(cygpath -m "$work/out")
	# FLAC's headers expect its DLL unless told otherwise. -static also
	# links in MinGW's own runtime, leaving only Windows' DLLs.
	export CGO_CFLAGS="${CGO_CFLAGS:--O2 -g} -DFLAC__NO_DLL"
	ldflags+=" -linkmode=external -extldflags=-static"
	bin=sptui.exe
else
	printf '#!/bin/sh\nexec %q --static "$@"\n' "$real_pkg_config" >"$work/pkg-config"
	chmod +x "$work/pkg-config"
	pkg_config=$work/pkg-config
	static=$work/static
	out=$work/out
fi

mkdir "$work/out"
(
	cd "$root"
	CGO_ENABLED=1 PKG_CONFIG="$pkg_config" CGO_LDFLAGS="-L$static" \
		go build -trimpath -buildvcs=false -ldflags "$ldflags" \
		-o "$out/$bin" ./cmd/sptui
)

# Make sure none of the codecs (or on Windows, MinGW's runtime) were linked
# dynamically after all.
case $os in
linux) deps=$(ldd "$work/out/$bin") ;;
darwin) deps=$(otool -L "$work/out/$bin") ;;
windows) deps=$(objdump -p "$work/out/$bin" | grep 'DLL Name') ;;
esac
echo "$deps"
if grep -Ei 'ogg|vorbis|flac|mpg123|winpthread|libgcc|libstdc' <<<"$deps"; then
	echo "linked dynamically against a library that won't be there" >&2
	exit 1
fi
"$work/out/$bin" -version

mkdir -p "$root/dist"
if [[ $os == windows ]]; then
	archive=sptui_${os}_${arch}.zip
	rm -f "$root/dist/$archive"
	(cd "$work/out" && zip -q "$root/dist/$archive" "$bin")
else
	archive=sptui_${os}_${arch}.tar.gz
	tar -czf "$root/dist/$archive" -C "$work/out" "$bin"
fi
echo "built dist/$archive"

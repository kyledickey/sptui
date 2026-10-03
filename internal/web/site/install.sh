#!/usr/bin/env bash
# Installs sptui from its latest GitHub release. On Linux it also makes sure
# ALSA's library is there, the one thing the binary links against.
#
#   curl -fsSL https://sptui.sh/install.sh | bash
#
# Flags (curl ... | bash -s -- --yes):
#   -y, --yes    don't ask, say yes to everything (also when there's no terminal)
#
# Environment:
#   SPTUI_VERSION      release to install, like v1.2.0 (default: the latest)
#   SPTUI_INSTALL_DIR  where to put sptui (default: ~/.local/bin)
#   SPTUI_ARCHIVE      install this sptui_<os>_<arch>.tar.gz instead of downloading one
#   GUM_VERSION        gum release to borrow for the output (default: 2.0.2)
#
# Runs on macOS and Linux (x86-64 and ARM). Arch, Debian/Ubuntu and Fedora
# get the ALSA library installed if it's missing; elsewhere, install it
# yourself.

# Everything is in main, so bash has read the whole script before it runs
# any of it, even when it comes from a pipe.
set -euo pipefail

REPO=kyledickey/sptui
VERSION=${SPTUI_VERSION:-latest}
INSTALL_DIR=${SPTUI_INSTALL_DIR:-$HOME/.local/bin}
ARCHIVE=${SPTUI_ARCHIVE:-}
GUM_VERSION=${GUM_VERSION:-2.0.2}
ACCENT="#1ed760"
MUTED="#9aa0a9"
DANGER="#ff6b6b"

GUM=""        # gum's path, or empty to print plainly
INTERACTIVE=0 # 1 when we can ask questions
TMP=""

# --- output --------------------------------------------------------------

banner() {
	local art
	art=$(printf '%s\n' "▄█▀ █▀▄ ▀█▀ █ █ ▀█▀" "▄▄▀ █▀   █  █▄█ ▄█▄")
	if [[ -n $GUM ]]; then
		"$GUM" style --foreground "$ACCENT" --bold --margin "1 2 0 2" "$art"
		"$GUM" style --foreground "$MUTED" --margin "0 2 1 2" "spotify in your terminal · installer"
	else
		printf '\n%s\n\nspotify in your terminal · installer\n\n' "$art"
	fi
}

# step prints a section heading.
step() {
	if [[ -n $GUM ]]; then
		"$GUM" style --foreground "$ACCENT" --bold "▌ $*"
	else
		printf '▌ %s\n' "$*"
	fi
}

ok() {
	if [[ -n $GUM ]]; then
		"$GUM" style --margin "0 0 0 2" "$("$GUM" style --foreground "$ACCENT" "✓") $*"
	else
		printf '  ✓ %s\n' "$*"
	fi
}

note() {
	if [[ -n $GUM ]]; then
		"$GUM" style --foreground "$MUTED" --margin "0 0 0 2" "$*"
	else
		printf '  %s\n' "$*"
	fi
}

die() {
	if [[ -n $GUM ]]; then
		"$GUM" style --foreground "$DANGER" --bold --margin "1 0 0 0" "✗ $1" >&2
		shift
		[[ $# -gt 0 ]] && "$GUM" style --foreground "$MUTED" --margin "0 0 1 2" "$@" >&2
	else
		printf '\n✗ %s\n' "$1" >&2
		shift
		[[ $# -gt 0 ]] && printf '  %s\n' "$@" >&2
	fi
	exit 1
}

# run runs a command (or one of our functions) under a spinner, showing its
# output only if it fails.
run() {
	local title=$1
	shift
	if [[ -n $GUM && $INTERACTIVE == 1 ]]; then
		# gum can only spin over programs, so the command runs here in the
		# background and gum waits for it.
		"$@" >"$TMP/log" 2>&1 &
		local pid=$! status=0
		"$GUM" spin --spinner minidot --spinner.foreground "$ACCENT" --title " $title" -- \
			bash -c "while kill -0 $pid 2>/dev/null; do sleep 0.1; done" || kill "$pid" 2>/dev/null || true
		wait "$pid" || status=$?
		if [[ $status != 0 ]]; then
			tail -n 30 "$TMP/log" >&2
			die "$title failed" "command: $*"
		fi
		ok "$title"
	else
		note "$title…"
		"$@" || die "$title failed" "command: $*"
	fi
}

# confirm asks a yes/no question, defaulting to yes.
confirm() {
	[[ $INTERACTIVE == 1 ]] || return 0
	if [[ -n $GUM ]]; then
		"$GUM" confirm --default --selected.background "$ACCENT" --selected.foreground "#000000" \
			--prompt.foreground "#e4e6eb" "  $1" </dev/tty
	else
		local reply
		printf '  %s [Y/n] ' "$1"
		read -r reply </dev/tty
		[[ -z $reply || $reply == [yY]* ]]
	fi
}

# --- setup ---------------------------------------------------------------

have() { command -v "$1" >/dev/null 2>&1; }

cleanup() { [[ -n $TMP ]] && rm -rf "$TMP"; }

# fetch downloads a URL to a file, with curl or wget.
fetch() {
	if have curl; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif have wget; then
		wget -q -O "$2" "$1"
	else
		echo "neither curl nor wget is installed" >&2
		return 1
	fi
}

detect_platform() {
	OS=$(uname -s)
	case $(uname -m) in
	x86_64 | amd64) ARCH=amd64 ;;
	arm64 | aarch64) ARCH=arm64 ;;
	*) die "No sptui build for $(uname -m)" "Build it yourself: https://github.com/$REPO#install" ;;
	esac
	case $OS in
	Darwin) DISTRO=macos ;;
	Linux)
		DISTRO=unknown
		if [[ -r /etc/os-release ]]; then
			local id like
			# shellcheck disable=SC1091
			id=$(. /etc/os-release && echo "${ID:-}")
			# shellcheck disable=SC1091
			like=$(. /etc/os-release && echo "${ID_LIKE:-}")
			case " $id $like " in
			*" arch "*) DISTRO=arch ;;
			*" debian "* | *" ubuntu "*) DISTRO=debian ;;
			*" fedora "* | *" rhel "*) DISTRO=fedora ;;
			esac
		fi
		;;
	MINGW* | MSYS* | CYGWIN*) die "On Windows, install sptui from PowerShell:" "irm https://sptui.sh/install.ps1 | iex" ;;
	*) die "sptui runs on macOS, Linux and Windows" ;;
	esac
}

# get_gum borrows a gum binary for the duration of the install, unless one's
# already on PATH. Without it the output is plain, but everything still works.
get_gum() {
	if have gum && [[ $(gum --version 2>/dev/null) == *"v2."* ]]; then
		GUM=$(command -v gum)
		return
	fi
	local arch name
	case $ARCH in amd64) arch=x86_64 ;; arm64) arch=arm64 ;; esac
	name="gum_${GUM_VERSION}_${OS}_${arch}"
	fetch "https://github.com/charmbracelet/gum/releases/download/v${GUM_VERSION}/${name}.tar.gz" "$TMP/gum.tar.gz" 2>/dev/null || return 0
	tar -xzf "$TMP/gum.tar.gz" -C "$TMP" 2>/dev/null || return 0
	[[ -x "$TMP/$name/gum" ]] && GUM="$TMP/$name/gum"
}

# need_root makes sure as_root will work, asking for the sudo password now
# rather than in the middle of a spinner.
need_root() {
	[[ $(id -u) == 0 ]] && return
	have sudo || die "sudo is needed to install packages" "Run this as root, or install sudo."
	if ! sudo -n true 2>/dev/null; then
		[[ $INTERACTIVE == 1 ]] || die "sudo needs a password" "Run this in a terminal, or as root."
		note "sudo is needed to install system packages."
		# shellcheck disable=SC2024 # the tty is for the password prompt
		sudo -v </dev/tty || die "couldn't get sudo"
	fi
}

as_root() {
	if [[ $(id -u) == 0 ]]; then
		"$@"
	else
		sudo "$@"
	fi
}

# --- install -------------------------------------------------------------

has_alsa() {
	if have ldconfig && ldconfig -p 2>/dev/null | grep -q 'libasound\.so\.2'; then
		return 0
	fi
	local d
	for d in /usr/lib /usr/lib64 /lib /lib64 /usr/lib/*-linux-gnu /lib/*-linux-gnu; do
		[[ -e $d/libasound.so.2 ]] && return 0
	done
	return 1
}

# install_alsa installs libasound, the only library the Linux build links
# against that isn't part of the C library.
install_alsa() {
	[[ $OS == Linux ]] || return 0
	step "ALSA"
	if has_alsa; then
		ok "already installed"
		return
	fi
	case $DISTRO in
	arch)
		need_root
		run "pacman -S alsa-lib" as_root pacman -S --needed --noconfirm alsa-lib
		;;
	debian)
		need_root
		run "apt-get update" as_root env DEBIAN_FRONTEND=noninteractive apt-get update -q
		# Ubuntu 24.04 and Debian 13 renamed it for the 64-bit time_t move.
		local pkg=libasound2
		if as_root apt-cache show libasound2t64 >/dev/null 2>&1; then
			pkg=libasound2t64
		fi
		run "apt-get install $pkg" as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y -q "$pkg"
		;;
	fedora)
		need_root
		run "dnf install alsa-lib" as_root dnf install -y -q alsa-lib
		;;
	*)
		die "sptui needs ALSA's library (libasound.so.2)" \
			"Install your distribution's alsa-lib or libasound2 package and run this again."
		;;
	esac
	has_alsa || die "libasound.so.2 still can't be found"
}

# verify checks the archive against the release's checksums.txt.
verify() {
	local want got
	want=$(awk -v f="$2" '$2 == f || $2 == "*"f {print $1}' "$1")
	[[ -n $want ]] || {
		echo "$2 isn't in checksums.txt" >&2
		return 1
	}
	if have sha256sum; then
		got=$(sha256sum "$TMP/$2" | awk '{print $1}')
	else
		got=$(shasum -a 256 "$TMP/$2" | awk '{print $1}')
	fi
	[[ $got == "$want" ]] || {
		echo "checksum mismatch: got $got, want $want" >&2
		return 1
	}
}

install_sptui() {
	step "sptui"
	local os file base
	os=$(echo "$OS" | tr '[:upper:]' '[:lower:]')
	file="sptui_${os}_${ARCH}.tar.gz"
	if [[ -n $ARCHIVE ]]; then
		cp "$ARCHIVE" "$TMP/$file"
	else
		if [[ $VERSION == latest ]]; then
			base="https://github.com/$REPO/releases/latest/download"
		else
			base="https://github.com/$REPO/releases/download/$VERSION"
		fi
		run "download $file" fetch "$base/$file" "$TMP/$file"
		fetch "$base/checksums.txt" "$TMP/checksums.txt" || die "Couldn't download checksums.txt"
		run "verify checksum" verify "$TMP/checksums.txt" "$file"
	fi
	mkdir -p "$TMP/out"
	tar -xzf "$TMP/$file" -C "$TMP/out" || die "Couldn't unpack $file"
	mkdir -p "$INSTALL_DIR" || die "Couldn't create $INSTALL_DIR"
	# Move rather than overwrite, so a running sptui keeps its old binary.
	mv -f "$TMP/out/sptui" "$INSTALL_DIR/sptui" || die "Couldn't write to $INSTALL_DIR"
	chmod +x "$INSTALL_DIR/sptui"
	INSTALLED=$("$INSTALL_DIR/sptui" -version 2>&1) || die "sptui was installed but doesn't run" "$INSTALLED"
	ok "${INSTALLED#sptui } in $INSTALL_DIR"
}

# rc_file picks the startup file of the user's shell.
rc_file() {
	case ${SHELL##*/} in
	zsh) echo "${ZDOTDIR:-$HOME}/.zshrc" ;;
	bash) [[ $OS == Darwin ]] && echo "$HOME/.bash_profile" || echo "$HOME/.bashrc" ;;
	fish) echo "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish" ;;
	*) echo "$HOME/.profile" ;;
	esac
}

on_path() { case ":$PATH:" in *":$1:"*) return 0 ;; esac; return 1; }

# add_to_path puts the install directory on PATH for new shells, if it
# isn't already.
add_to_path() {
	local found
	found=$(command -v sptui 2>/dev/null || true)
	if on_path "$INSTALL_DIR"; then
		if [[ -n $found && $found != "$INSTALL_DIR/sptui" ]]; then
			step "PATH"
			note "Another sptui, $found, comes first on your PATH."
			note "Remove it to use this one."
		fi
		return
	fi

	local rc line
	rc=$(rc_file)
	if [[ ${SHELL##*/} == fish ]]; then
		line="fish_add_path $INSTALL_DIR"
	else
		line="export PATH=\"$INSTALL_DIR:\$PATH\""
	fi
	step "PATH"
	if [[ -f $rc ]] && grep -qF "$line" "$rc"; then
		ok "already in $rc (open a new terminal)"
	elif confirm "Add $INSTALL_DIR to PATH in $rc?"; then
		mkdir -p "$(dirname "$rc")"
		printf '\n# added by the sptui installer\n%s\n' "$line" >>"$rc"
		ok "added to $rc (open a new terminal)"
	else
		note "Add this to $rc:"
		note "  $line"
	fi
}

outro() {
	local cmd="sptui"
	on_path "$INSTALL_DIR" || cmd="$INSTALL_DIR/sptui"
	if [[ -n $GUM ]]; then
		"$GUM" style --border thick --border-foreground "$ACCENT" --padding "1 3" --margin "1 0" \
			"$("$GUM" style --bold "Done. Now run")" \
			"$("$GUM" style --foreground "$ACCENT" --bold "$cmd")" \
			"" \
			"$("$GUM" style --foreground "$MUTED" "Log in with Spotify Premium, or try  $cmd -demo")"
	else
		printf '\nDone. Now run\n\n  %s\n\nLog in with Spotify Premium, or try  %s -demo\n\n' "$cmd" "$cmd"
	fi
}

main() {
	local arg
	for arg in "$@"; do
		case $arg in
		-y | --yes) YES=1 ;;
		-h | --help)
			echo "usage: install.sh [-y|--yes]"
			exit 0
			;;
		*) echo "unknown flag: $arg" >&2 && exit 2 ;;
		esac
	done

	if [[ -z ${YES:-} && -z ${CI:-} && -t 1 ]] && (: </dev/tty) 2>/dev/null; then
		INTERACTIVE=1
	fi
	[[ -z $ARCHIVE || -f $ARCHIVE ]] || die "No such archive: $ARCHIVE"
	ARCHIVE=${ARCHIVE:+$(cd "$(dirname "$ARCHIVE")" && pwd)/$(basename "$ARCHIVE")}

	TMP=$(mktemp -d)
	trap cleanup EXIT
	detect_platform
	get_gum

	banner
	install_alsa
	install_sptui
	add_to_path
	outro
}

main "$@"

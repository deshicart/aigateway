#!/bin/sh
# OpenBridge installer: detects OS/arch and downloads the matching binary.
# Usage: sh scripts/install.sh [--version v1.0.0] [--dir /usr/local/bin]
set -eu
VERSION="v1.0.0"
DIR="/usr/local/bin"
REPO="openbridge/gateway"
while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2;;
    --dir) DIR="$2"; shift 2;;
    *) echo "unknown arg $1"; exit 2;;
  esac
done
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$OS" in
  linux) OS="linux";;
  darwin) OS="macos";;
  mingw*|msys*|cygwin*|windowsnt) OS="windows";;
  *) echo "unsupported OS: $OS"; exit 1;;
esac
case "$ARCH" in
  x86_64|amd64) ARCH="x86_64";;
  aarch64|arm64) ARCH="arm64";;
  armv7l|armv7|arm) ARCH="armv7";;
  i386|i686|x86) ARCH="x86";;
  *) echo "unsupported arch: $ARCH (try building from source)"; exit 1;;
esac
# Android/Termux reports linux + arch; prefer android asset when termux detected
if [ -n "${TERMUX_VERSION:-}" ] || [ -d "/data/data/com.termux" ]; then
  OS="android"
fi
ASSET="openbridge-${OS}-${ARCH}"
[ "$OS" = "windows" ] && ASSET="${ASSET}.exe"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
echo "${OS} ${ARCH} detected"
echo "Downloading: ${ASSET} (${URL})"
tmp="$(mktemp)"
if command -v curl >/dev/null 2>&1; then
  curl -fL -o "$tmp" "$URL"
elif command -v wget >/dev/null 2>&1; then
  wget -O "$tmp" "$URL"
else
  echo "need curl or wget"; exit 1
fi
chmod +x "$tmp"
echo "Verify checksum against ${URL}.sha256 before installing in production."
mkdir -p "$DIR"
mv "$tmp" "${DIR}/openbridge"
echo "Installed to ${DIR}/openbridge — run: openbridge"

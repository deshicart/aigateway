#!/bin/sh
# Build all Tier-1 targets (PRD §3, §73). Pure-Go SQLite: no cgo needed.
set -eu
cd "$(dirname "$0")/.."
OUT="${OUT:-dist}"
mkdir -p "$OUT"
build() { # os arch [arm] [suffix] [env...]
  os="$1"; arch="$2"; arm="${3:-}"; suf="$4"
  echo "== $os/$arch$arm -> $OUT/openbridge$suf"
  if [ -n "$arm" ]; then
    GOOS="$os" GOARCH="$arch" GOARM="$arm" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$OUT/openbridge$suf" ./cmd/openbridge
  else
    GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$OUT/openbridge$suf" ./cmd/openbridge
  fi
}
build linux amd64 "" "-linux-x86_64"
build linux arm64 "" "-linux-arm64"
build linux arm 7 "-linux-armv7"
build windows amd64 "" "-windows-x86_64.exe"
build darwin amd64 "" "-macos-x86_64"
build darwin arm64 "" "-macos-arm64"
build android arm64 "" "-android-arm64"
build windows arm64 "" "-windows-arm64"
build linux 386 "" "-linux-x86"
ls -la "$OUT"

#!/bin/sh
# Builds the program for Windows. Usage: ./build.sh <output folder>
set -e
OUT=${1:-dist}; mkdir -p "$OUT"
python3 tools/make_rsrc.py assets/icon-source.png rsrc_windows_amd64.syso "מצב שבת" ShabbatMode
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H=windowsgui -s -w" -o "$OUT/ShabbatMode.exe" .

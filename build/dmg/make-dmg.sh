#!/bin/bash
# Build the pretty Raven .dmg (styled drag-to-Applications window).
#
# Run this in a NORMAL Terminal window on your Mac (not over SSH / not headless):
#   bash "build/dmg/make-dmg.sh"
# It needs an interactive desktop session because create-dmg scripts Finder to
# place the icons and apply the background.
#
# Requires: create-dmg (brew install create-dmg) and a built build/bin/raven.app.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"   # repo root
APP="$ROOT/build/bin/raven.app"
BG="$ROOT/build/dmg/background.png"
VOLICON="$ROOT/build/raven.icns"
OUT="$HOME/Downloads/Raven/Raven-macOS-universal.dmg"

if [ ! -d "$APP" ]; then
  echo "error: $APP not found. Build it first: wails build -platform darwin/universal"
  exit 1
fi
if ! command -v create-dmg >/dev/null 2>&1; then
  echo "error: create-dmg not installed. Run: brew install create-dmg"
  exit 1
fi

# Stage just the app under a clean, capitalized name.
STAGE="$(mktemp -d)"
cp -R "$APP" "$STAGE/Raven.app"

mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"

# Window 540x380; app icon left, Applications drop-link right, matching background.png.
create-dmg \
  --volname "Raven" \
  --volicon "$VOLICON" \
  --background "$BG" \
  --window-pos 200 120 \
  --window-size 540 380 \
  --icon-size 104 \
  --icon "Raven.app" 140 210 \
  --hide-extension "Raven.app" \
  --app-drop-link 400 210 \
  --no-internet-enable \
  "$OUT" "$STAGE"

rm -rf "$STAGE"
echo
echo "Done -> $OUT"

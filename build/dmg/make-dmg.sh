#!/bin/bash
# Build the Raven .dmg (styled drag-to-Applications window).
#
# On your Mac, in a normal Terminal window:
#   bash "build/dmg/make-dmg.sh"
#
# The background image and icon placement are applied by scripting Finder, which
# needs a live Finder to talk to. That step has a known race where the freshly
# mounted volume is not visible to Finder yet, so it is retried rather than
# treated as fatal. A .dmg that never gets its styling is a failure, not a quiet
# downgrade: set RAVEN_DMG_ALLOW_PLAIN=1 to accept a plain window on purpose.
#
# Overridable:
#   RAVEN_APP              path to the built .app  (default build/bin/raven.app)
#   RAVEN_DMG_OUT          path to write the .dmg  (default ~/Downloads/Raven/...)
#   RAVEN_DMG_ATTEMPTS     styled build attempts   (default 3)
#   RAVEN_DMG_ALLOW_PLAIN  accept an unstyled .dmg (default 0)
#
# Requires: create-dmg (brew install create-dmg) and a built .app.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"   # repo root
APP="${RAVEN_APP:-$ROOT/build/bin/raven.app}"
BG="$ROOT/build/dmg/background.png"
VOLICON="$ROOT/build/raven.icns"
OUT="${RAVEN_DMG_OUT:-$HOME/Downloads/Raven/Raven-macOS-universal.dmg}"
ATTEMPTS="${RAVEN_DMG_ATTEMPTS:-3}"

# create-dmg exits 64 when the Finder scripting step fails. That is the flake
# worth retrying; any other non-zero status is a real error.
APPLESCRIPT_FAILED=64

if [ ! -d "$APP" ]; then
  echo "error: $APP not found. Build it first: wails build -platform darwin/universal"
  exit 1
fi
if ! command -v create-dmg >/dev/null 2>&1; then
  echo "error: create-dmg not installed. Run: brew install create-dmg"
  exit 1
fi

# If the filename claims a universal build, hold it to that rather than shipping
# a single-architecture app under a name that promises both.
case "$(basename "$OUT")" in
  *universal*)
    EXE="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' \
      "$APP/Contents/Info.plist" 2>/dev/null || echo raven)"
    ARCHS="$(lipo -archs "$APP/Contents/MacOS/$EXE" 2>/dev/null || true)"
    case " $ARCHS " in
      *" x86_64 "*) ;;
      *) echo "error: $(basename "$OUT") claims universal but $EXE is: ${ARCHS:-unreadable}"
         echo "       Build it with: wails build -platform darwin/universal"
         exit 1 ;;
    esac
    case " $ARCHS " in
      *" arm64 "*) ;;
      *) echo "error: $(basename "$OUT") claims universal but $EXE is: ${ARCHS:-unreadable}"
         echo "       Build it with: wails build -platform darwin/universal"
         exit 1 ;;
    esac
    ;;
esac

# Stage just the app under a clean, capitalized name.
STAGE="$(mktemp -d)"
cp -R "$APP" "$STAGE/Raven.app"
trap 'rm -rf "$STAGE"' EXIT

mkdir -p "$(dirname "$OUT")"

# Window 540x380; app icon left, Applications drop-link right, matching background.png.
build_dmg() {
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
    --applescript-sleep-duration 5 \
    "$@" \
    "$OUT" "$STAGE"
}

# Mount the result and confirm the cosmetic pass actually landed. A plain .dmg
# still holds the app and the Applications link, so those alone prove nothing;
# .background and the volume icon are what only the Finder pass writes.
verify_styled() {
  local mnt rc=0
  mnt="$(mktemp -d)"
  if ! hdiutil attach "$OUT" -mountpoint "$mnt" -nobrowse -readonly -quiet; then
    rmdir "$mnt" 2>/dev/null || true
    return 1
  fi
  [ -d "$mnt/Raven.app" ]        || rc=1
  [ -L "$mnt/Applications" ]     || rc=1
  [ -d "$mnt/.background" ]      || rc=1
  [ -f "$mnt/.VolumeIcon.icns" ] || rc=1
  hdiutil detach "$mnt" -quiet >/dev/null 2>&1 || true
  rmdir "$mnt" 2>/dev/null || true
  return $rc
}

styled=0
for attempt in $(seq 1 "$ATTEMPTS"); do
  rm -f "$OUT"
  rc=0
  build_dmg || rc=$?
  if [ "$rc" -eq 0 ] && [ -f "$OUT" ] && verify_styled; then
    styled=1
    break
  fi
  if [ "$rc" -eq "$APPLESCRIPT_FAILED" ]; then
    echo "note: attempt $attempt/$ATTEMPTS could not script Finder, retrying"
  else
    echo "note: attempt $attempt/$ATTEMPTS produced no styled .dmg (exit $rc)"
  fi
done

if [ "$styled" -ne 1 ]; then
  if [ "${RAVEN_DMG_ALLOW_PLAIN:-0}" != "1" ]; then
    echo "error: no styled .dmg after $ATTEMPTS attempts."
    echo "       The background and icon layout come from Finder, so this needs a"
    echo "       desktop session. Run it in a normal Terminal window, or set"
    echo "       RAVEN_DMG_ALLOW_PLAIN=1 to ship a plain window deliberately."
    exit 1
  fi
  echo "note: RAVEN_DMG_ALLOW_PLAIN=1, building without the Finder pass"
  rm -f "$OUT"
  rc=0
  build_dmg --skip-jenkins || rc=$?
  if [ "$rc" -ne 0 ] || [ ! -f "$OUT" ]; then
    echo "error: create-dmg failed (exit $rc)"
    exit 1
  fi
fi

echo
if [ "$styled" -eq 1 ]; then
  echo "Done (styled) -> $OUT"
else
  echo "Done (plain, no Finder pass) -> $OUT"
fi

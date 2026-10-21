#!/usr/bin/env bash
# Start the linux-dev engine on :99 with the game running, and keep it alive.
# Writes the engine stdout (with the FLWGAME handle) to $OUT/engine-stdout.log
# so scripts/flwprobe.py can drive the game.
set -u
DISP="${1:-:99}"
OUT="${2:-/tmp/opencode/live}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="$ROOT/output/linux-dev"

mkdir -p "$OUT"
export DISPLAY="$DISP"
export GDK_BACKEND=x11
export WEBKIT_DISABLE_DMABUF_RENDERER=1
export GSK_RENDERER=cairo
export WEBKIT_DISABLE_COMPOSITING_MODE=1

pkill -x flwgamehost 2>/dev/null
pkill -x flwengine 2>/dev/null
sleep 0.5

cd "$TARGET" || exit 1
./flwengine > "$OUT/engine-stdout.log" 2>&1 &
echo $! > "$OUT/engine.pid"

for i in $(seq 1 60); do
    xdotool search --name "NuggetOS" >/dev/null 2>&1 && break
    sleep 0.5
done
sleep 3
# Click the "MyZone" game card instead of pressing Enter: Enter activates
# whatever tile the pointer happens to hover (hover-steal), which is not
# deterministic. The card is at window coords (48,130) size 316x...
SHELL=$(xdotool search --name "NuggetOS" | tail -1)
GEO=$(xdotool getwindowgeometry --shell "$SHELL" 2>/dev/null)
OX=$(echo "$GEO" | sed -n 's/.*Position: *-\?[0-9]*,\(-\?[0-9]*\).*/\1/p')
OY=$(echo "$GEO" | sed -n 's/.*Position: *\(-?[0-9]*\),-\?[0-9]*.*/\1/p')
if [ -z "$OX" ]; then OX=0; OY=0; fi
xdotool mousemove --sync $((OX + 206)) $((OY + 180)) 2>/dev/null
sleep 0.4
xdotool windowactivate --sync "$SHELL" 2>/dev/null
sleep 0.4
xdotool mousedown 1; sleep 0.25; xdotool mouseup 1
sleep 12

echo "engine pid $(cat "$OUT/engine.pid")"
grep FLWGAME "$OUT/engine-stdout.log" || echo "no FLWGAME line yet"

#!/usr/bin/env bash
# E2E harness for the flower engine (linux-dev).
#
# Usage: scripts/e2e.sh [display] [outdir]
#
# Boots the engine on a virtual display, launches the game, drives it through
# the key scenarios that kept regressing (F10 devconsole, model switch, save
# load, window shake) and writes screenshots + the decoded engine log to the
# output dir so the result can be inspected afterwards.
set -u

DISP="${1:-:99}"
OUT="${2:-/tmp/opencode/e2e}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="$ROOT/output/linux-dev"

mkdir -p "$OUT"
rm -f "$OUT"/*.png "$OUT"/*.log 2>/dev/null

export DISPLAY="$DISP"
export GDK_BACKEND=x11
export WEBKIT_DISABLE_DMABUF_RENDERER=1
export GSK_RENDERER=cairo
export WEBKIT_DISABLE_COMPOSITING_MODE=1

log() { echo "[e2e] $*"; }

shot() {
    local name="$1"
    local w
    w=$(xdotool search --name "NuggetOS" 2>/dev/null | tail -1)
    [ -z "$w" ] && w=$(xdotool search --name "Rosemary" 2>/dev/null | tail -1)
    if [ -n "$w" ]; then
        import -window root "$OUT/$name.png" 2>/dev/null
        echo "[e2e] shot $name (window $w)"
    else
        echo "[e2e] shot $name: no window"
    fi
}

key() {
    local k="$1"
    xdotool key --clearmodifiers "$k"
    sleep 0.15
}

cleanup() {
    pkill -x flwgamehost 2>/dev/null
    pkill -x flwengine 2>/dev/null
}
trap cleanup EXIT

cleanup
log "starting engine from $TARGET"
cd "$TARGET" || exit 1
rm -f flwdata/logs/supervisor.nklog
./flwengine > "$OUT/engine-stdout.log" 2>&1 &
ENGINE_PID=$!
log "engine pid $ENGINE_PID"

# wait for the shell window
for i in $(seq 1 60); do
    if xdotool search --name "NuggetOS" >/dev/null 2>&1; then break; fi
    sleep 0.5
done
sleep 2
shot 01-boot

log "launching game (Enter on the focused tile)"
xdotool mousemove --sync 1279 799 2>/dev/null
xdotool windowactivate --sync $(xdotool search --name "NuggetOS" | tail -1) 2>/dev/null
sleep 0.4
key Enter
sleep 12
shot 02-game

# the game window takes over; find it
GAMEW=$(xdotool search --name "Rosemary" 2>/dev/null | tail -1)
log "game window: ${GAMEW:-none}"
if [ -n "$GAMEW" ]; then
    log "geometry: $(xdotool getwindowgeometry $GAMEW | tr '\n' ' ')"
fi

log "opening devconsole with F10 (from the game window)"
if [ -n "$GAMEW" ]; then xdotool windowactivate --sync "$GAMEW" 2>/dev/null; fi
sleep 0.5
key F10
sleep 3
shot 03-devconsole

log "navigating: Down Down (to MODEL) then Right"
key Down
key Down
key Right
sleep 1.5
shot 04-model-menu

log "activating first model"
key Enter
sleep 3
shot 05-model-applied

log "closing console (F10)"
key F10
sleep 2
shot 06-after-console

# engine log
if [ -f "$TARGET/flwdata/logs/supervisor.nklog" ]; then
    python3 "$ROOT/scripts/nklogdec.py" "$TARGET/flwdata/logs/supervisor.nklog" > "$OUT/engine.log" 2>&1
    log "engine log -> $OUT/engine.log ($(wc -l < "$OUT/engine.log") lines)"
fi

log "done; artifacts in $OUT"

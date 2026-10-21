
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENGINE="$ROOT/flwengine"
ANDROID_PROJ="$ROOT/android"
SDK="${ANDROID_HOME:-$HOME/android-sdk}"
TYPE="${1:-dev}"
API="${FLW_ANDROID_API:-26}"

TARGET="android-$TYPE"
OUTDIR="$ROOT/output/$TARGET"
AAR="$ANDROID_PROJ/app/libs/flwengine.aar"
ASSETS="$ANDROID_PROJ/app/src/main/assets"

export ANDROID_HOME="$SDK"
export PATH="$PATH:$HOME/go/bin"
export PKG_CONFIG_PATH="$HOME/pkgconfig:${PKG_CONFIG_PATH:-}"

if [[ ! -d "$SDK/ndk" ]]; then
    echo "android ndk not found in $SDK/ndk" >&2
    exit 1
fi

if [[ ! -f "$OUTDIR/.ldflags" ]]; then
    echo "run build.py $TARGET first (no .ldflags found)" >&2
    exit 1
fi
LDFLAGS="$(cat "$OUTDIR/.ldflags")"
TAGS="$(cat "$OUTDIR/.tags")"

echo "== generating launcher icons from logo.svg =="
ICON_SRC="$ENGINE/internal/nuggetos/logo.svg"
if [[ ! -f "$ICON_SRC" ]]; then
    echo "logo.svg missing at $ICON_SRC" >&2
    exit 1
fi
gen_mipmap() {
    local dpi="$1" size="$2"
    local dir="$ANDROID_PROJ/app/src/main/res/mipmap-$dpi"
    mkdir -p "$dir"
    rsvg-convert -w "$size" -h "$size" -b "#0b0e14" -o "$dir/ic_launcher_fg.png" "$ICON_SRC"
}
gen_mipmap mdpi 108
gen_mipmap hdpi 162
gen_mipmap xhdpi 216
gen_mipmap xxhdpi 324
gen_mipmap xxxhdpi 432
rm -f "$ANDROID_PROJ/app/src/main/res/drawable/ic_launcher_fg.xml"
echo "icons generated"

echo "== building go aar (androidapi $API, tags $TAGS) =="
rm -rf "$ANDROID_PROJ/app/libs"
mkdir -p "$ANDROID_PROJ/app/libs"
cd "$ENGINE"

# In a production build the engine library is obfuscated with garble: the
# names and the string literals of the binary are not readable any more.
# gomobile calls "go build" for the shared library, so a wrapper in front of
# the go tool routes that call through garble. The mobile package itself is
# excluded from the obfuscation: its exported names are the api of the java
# binding.
GARBLE_BIN="$(command -v garble 2>/dev/null || true)"
if [[ -z "$GARBLE_BIN" && -x "$HOME/go/bin/garble" ]]; then
    GARBLE_BIN="$HOME/go/bin/garble"
fi
if [[ "$TYPE" == "prod" ]]; then
    if [[ -z "$GARBLE_BIN" ]]; then
        echo "the production build needs garble: go install mvdan.cc/garble@latest" >&2
        exit 1
    fi
    WRAPDIR="$(mktemp -d)"
    # The real go tool: the wrapper must not call itself.
    REALGO="$(command -v go)"
    cat > "$WRAPDIR/go" <<WRAPEOF
#!/bin/sh
# Route the build of the engine library through garble.
#
# garble itself calls "go build -toolexec=garble": that call must not be
# intercepted again, otherwise the chain of the toolexec grows without a
# limit and the build dies with "resource temporarily unavailable".
case "\$*" in
    *-toolexec*) exec "$REALGO" "\$@" ;;
esac
if [ "\$1" = "build" ]; then
    shift
    exec "$GARBLE_BIN" -literals -tiny build "\$@"
fi
exec "$REALGO" "\$@"
WRAPEOF
    chmod +x "$WRAPDIR/go"
    export PATH="$WRAPDIR:$PATH"
    # Only the packages of the engine are obfuscated: the standard library
    # is public knowledge anyway and obfuscating it makes the build of the
    # four architectures far too heavy. The mobile package is excluded: its
    # exported names are the api of the java binding.
    export GOGARBLE="flwengine/*,!flwengine/mobile"
    # The build of the shared library is limited: the cgroup of the session
    # has a limit for the number of processes and garble starts a lot of
    # them.
    export GOMAXPROCS=4
    export GOFLAGS="-p=4"
    echo "== the engine library is obfuscated with garble =="
fi

gomobile bind \
    -target=android -androidapi "$API" -tags "$TAGS" -javapkg su.neakami.flowerengine \
    -ldflags "$LDFLAGS" \
    -o "$AAR" \
    flwengine/mobile
echo "aar: $AAR"

echo "== staging packages =="
rm -rf "$ASSETS"
mkdir -p "$ASSETS"
if [[ "$TYPE" == "prod" ]]; then
    cp "$OUTDIR/flwdata/packages/prodgame.nkpkgenc" "$ASSETS/"
    cp "$OUTDIR/flwdata/packages/runtime.nkpkgenc" "$ASSETS/" 2>/dev/null || true
else
    cp "$OUTDIR/flwdata/packages/game.nkpkg" "$ASSETS/"
    cp "$OUTDIR/flwdata/packages/runtime.nkpkg" "$ASSETS/" 2>/dev/null || true
fi
# No plain/ assets: the runtime and its manifest live inside the runtime
# package (encrypted and signed in a production build).
ls -la "$ASSETS"

echo "== gradle assemble =="
cd "$ANDROID_PROJ"
if [[ "$TYPE" == "prod" ]]; then
    GRADLE_TASK=assembleRelease
else
    GRADLE_TASK=assembleDebug
fi
gradle --no-daemon "$GRADLE_TASK"

mkdir -p "$OUTDIR"
if [[ "$TYPE" == "prod" ]]; then
    KEYSTORE="$ROOT/keys/android-release.jks"
    if [[ ! -f "$KEYSTORE" ]]; then
        keytool -genkeypair -keystore "$KEYSTORE" -alias flower -keyalg RSA -keysize 2048 -validity 10000 \
            -storepass flowerengine -keypass flowerengine -dname "CN=flower engine" 2>/dev/null
        echo "generated $KEYSTORE"
    fi
    UNSIGNED="$ANDROID_PROJ/app/build/outputs/apk/release/app-release-unsigned.apk"
    ALIGNED="$ANDROID_PROJ/app/build/outputs/apk/release/app-release-aligned.apk"
    BT="$(ls -d "$SDK"/build-tools/*/ | sort | tail -1)"
    "$BT/zipalign" -f 4 "$UNSIGNED" "$ALIGNED"
    "$BT/apksigner" sign --ks "$KEYSTORE" --ks-pass pass:flowerengine --key-pass pass:flowerengine \
        --out "$OUTDIR/rosemaryfield-$TARGET.apk" "$ALIGNED"
    echo "apk signed: $OUTDIR/rosemaryfield-$TARGET.apk"
else
    cp app/build/outputs/apk/debug/app-debug.apk "$OUTDIR/rosemaryfield-$TARGET.apk"
    echo "apk: $OUTDIR/rosemaryfield-$TARGET.apk"
fi

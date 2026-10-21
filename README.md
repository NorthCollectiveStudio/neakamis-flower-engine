# neakamis flower engine

A custom runtime for **RPG Maker MZ** games, written in Go. It runs a game
inside a managed window, gives the game a bridge to the system (window, sound,
vibration, files, events) and ships a small shell around it ("NuggetOS") for
the development builds.

The engine is the *flower engine* (`flwengine`); the game that is built with
it is **not** a part of this repository.

## What it does

* **Runs an RPG Maker MZ game** in a native window (WebKitGTK on Linux,
  WebView2 on Windows, the system web view on Android). The game itself is
  untouched: the engine serves the files of the game and adds a bridge.
* **A bridge for the game** (`flwengine/internal/bridge/web/flwbridge.js`):
  a `node`-like API for the page - window (move, size, fullscreen, always on
  top, shake), sound, vibration, the file system, the events of the engine.
  The screen shake of the game is a real window shake on the desktop and a
  vibration of the phone on Android.
* **A shell** (NuggetOS) for the development builds: a dashboard with the
  installed games, a settings screen, a terminal (`nsh`), a developer console
  with a model of the running game and a hot reload of the changed files.
* **A package format** (`nkpkg`) with encryption, signing and obfuscation of
  the JavaScript of the game. A production build contains the game only in an
  encrypted and signed package; the bridge and the boot page live in the
  runtime package, not in the binary.
* **A guard** that checks for a debugger when the engine starts (and every few
  seconds afterwards), keeps the key material in locked memory and verifies
  the integrity of its own binary.
* **A build matrix**: `dev`, `test` and `prod` for Linux, Windows and
  Android, with the features of the build type (dashboard, hot reload,
  developer console, encryption, signing, obfuscation) switched by the build.

## Layout

```
flwengine/            the engine (go)
  cmd/flwengine       the shell and the game host
  cmd/flwgamehost     the game host as a separate program (dev, test)
  internal/bridge     the bridge between the engine and the game page
  internal/gamehost   the window, the http server, the rpc
  internal/nuggetos   the shell: dashboard, settings, terminal, console
  internal/nkpkg      the package format (encryption, signing)
  internal/guard      the anti debug guard
  internal/supervisor the process management of the game
  runtime/            the boot page and the node runtime
apidemos/             a small demo game for the api of the bridge
android/              the android project of the engine
scripts/              the build and the regression scripts
build.py              the build
buildconfig.json      the build matrix and the packaging rules
```

## Building

The engine needs a C toolchain and the development packages of the web view:

```sh
# debian/ubuntu
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.0-dev libasound2-dev
# windows (cross build from linux)
sudo apt install gcc-mingw-w64
# android
# the ndk of the android sdk, the android sdk and gradle
```

The game is expected in the folder `therosemaryfield` (the name is set in
`buildconfig.json`):

```sh
git clone <your rpg maker mz project> therosemaryfield
```

Then:

```sh
python3 build.py all              # every target of the matrix
python3 build.py linux-dev        # one target
./scripts/build_android.sh prod   # the android package of a type
```

The result is in `output/<target>/`. The keys of the build are generated in
`keys/` on the first build - they are personal and are not a part of this
repository.

A production build obfuscates the binary with [garble], so it has to be
installed for that build type:

```sh
go install mvdan.cc/garble@latest
```

## Tests

The scripts in `scripts/` drive the real engine and the real game:

| script | what it checks |
| --- | --- |
| `regress.py` | the behaviour of the game through the rpc of the engine |
| `ui_model_test.py` | the model of the developer console |
| `deadlock_test.py` | the shell does not deadlock on the mouse |
| `shake_test.py` | the window shake and the position of the window |
| `sound_test.py` | the sounds of the engine reach the sound server |
| `prod_protect_test.py` | the protection of a production build |
| `cache_leak_test.py`, `leak_test.py` | the memory of the engine |
| `check_tree.py` | the source tree is intact before a build |

They need a running X server (`Xvfb :99` is enough) and a build in
`output/linux-dev`.

## License

MIT, see [LICENSE](LICENSE).

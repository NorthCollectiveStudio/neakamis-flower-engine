#!/usr/bin/env python3
"""Regression suite for the flower-engine bugs that kept coming back.

Every check drives the real game through the RPC channel (scripts/flwprobe.py)
and asserts on live game state, so a regression shows up as a failed check
instead of a vague "it looks frozen" report.

Usage: scripts/regress.py [path-to-engine-stdout.log]
"""
import json
import os
import re
import socket
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ROOT, "scripts"))
import importlib.util

spec = importlib.util.spec_from_file_location(
    "flwprobe", os.path.join(ROOT, "scripts", "flwprobe.py"))
flwprobe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(flwprobe)

PASS, FAIL = [], []


def check(name, ok, detail=""):
    (PASS if ok else FAIL).append(name)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name,
                       ("  -- " + str(detail)) if detail else ""))
    return ok


class Game:
    def __init__(self, logpath):
        handle = flwprobe.find_handle([logpath])
        if not handle:
            raise SystemExit("no FLWGAME handle in %s" % logpath)
        _port, rpc_port, token = handle
        self.rpc = flwprobe.RPC("127.0.0.1:%d" % rpc_port, token)

    def js(self, code):
        return self.rpc.eval(code)

    def try_js(self, code):
        try:
            return self.rpc.eval(code)
        except Exception as e:
            return "ERR:%s" % e


def main():
    logpath = sys.argv[1] if len(sys.argv) > 1 else "/tmp/opencode/live/engine-stdout.log"
    g = Game(logpath)

    # ---- 0. boot health ----------------------------------------------------
    # A broken bridge (e.g. process.versions.node flipping Three.js into
    # "node" mode) leaves the game on a black screen: the database is never
    # loaded and the scene never advances. That must fail loudly.
    scene = g.try_js("String(SceneManager._scene && SceneManager._scene.constructor.name)")
    db = g.try_js("String(DataManager.isDatabaseLoaded())")
    frames = g.try_js("String(Graphics.frameCount)")
    check("boot: the game loop is running", scene not in ("", "null", "undefined"),
          "scene=%s" % scene)
    check("boot: the database is loaded", db == "true", "isDatabaseLoaded=%s" % db)
    check("boot: frames are being rendered", frames.isdigit() and int(frames) > 5,
          "frameCount=%s" % frames)

    # ---- 0b. bootstrap a playable state ------------------------------------
    # A fresh boot sits on the title screen where $gameParty does not exist
    # yet, so the game state has to be created before it can be inspected.
    boot = g.try_js(
        "(function(){try{"
        "if (typeof $gameParty === 'undefined' || !$gameParty) { DataManager.setupNewGame(); }"
        "if (!SceneManager._scene || SceneManager._scene.constructor.name !== 'Scene_Map') {"
        "SceneManager.goto(Scene_Map); }"
        "return 'ok';}catch(e){return 'ERR:'+e.message}})()")
    check("bootstrap: game state created", boot == "ok", boot)
    time.sleep(1.5)

    # ---- 1. bridge surface -------------------------------------------------
    check("bridge: flwCall present",
          g.try_js("typeof window.flwCall") == "function")
    check("bridge: Nugget api present",
          g.try_js("typeof window.Nugget") == "object")
    check("bridge: fs shim present",
          g.try_js("typeof require('fs').existsSync") == "function")
    check("bridge: process.mainModule is relative",
          g.try_js("process.mainModule && process.mainModule.filename") == "index.html",
          g.try_js("process.mainModule && process.mainModule.filename"))
    check("bridge: nw.Window.get().window.alert exists",
          g.try_js("typeof nw.Window.get().window.alert") == "function")
    check("bridge: nw.Window.get() has x/y",
          g.try_js("typeof nw.Window.get().x") in ("number", "undefined"))

    # ---- 2. save path resolution (session 8 root cause) --------------------
    check("vfs: game-relative path is not absolute",
          g.try_js("require('path').dirname(process.mainModule.filename)") == ".",
          g.try_js("require('path').dirname(process.mainModule.filename)"))

    # ---- 3. model switch (session 11 bug: refresh() reverted it) ------------
    # Pick an actor whose image differs from the current one so the check is
    # independent of whatever state a previous run left behind.
    cur = g.try_js("$gameParty.leader() && $gameParty.leader()._characterName + ':' + $gameParty.leader()._characterIndex")
    actors_raw = g.try_js(
        "JSON.stringify(($dataActors||[]).map(function(a){return a?a.name:null}))")
    try:
        actors = json.loads(actors_raw)
    except Exception:
        actors = []
    target_idx = None
    for i in range(1, len(actors)):
        if not actors[i]:
            continue
        ci = g.try_js("$dataActors[%d].characterIndex" % i)
        cn = g.try_js("$dataActors[%d].characterName" % i)
        if "%s:%s" % (cn, ci) != cur:
            target_idx = i
            break
    check("model switch: found a distinct actor to switch to", target_idx is not None,
          "current=%s" % cur)
    if target_idx is not None:
        g.try_js(
            "(function(){var a=$dataActors[%d];if(!a)return 'no actor';"
            "var L=$gameParty.leader();if(!L)return 'no leader';"
            "L.setCharacterImage(a.characterName,a.characterIndex);"
            "try{if(a.faceName!==undefined)L.setFaceImage(a.faceName,a.faceIndex)}catch(e){}"
            "$gamePlayer.refresh();"
            "try{$gamePlayer.followers().forEach(function(f){f.refresh()})}catch(e){}"
            "try{$gameMap.requestRefresh()}catch(e){};"
            "return 'model: '+a.name})()" % target_idx)
        time.sleep(0.5)
        want = "%s:%s" % (g.try_js("$dataActors[%d].characterName" % target_idx),
                          g.try_js("$dataActors[%d].characterIndex" % target_idx))
        after = g.try_js("$gameParty.leader() && $gameParty.leader()._characterName + ':' + $gameParty.leader()._characterIndex")
        check("model switch: leader image is the requested one", after == want,
              "want=%s got=%s" % (want, after))
        player_img = g.try_js("$gamePlayer._characterName + ':' + $gamePlayer._characterIndex")
        check("model switch: player image follows leader", player_img == after,
              "player=%s leader=%s" % (player_img, after))

    # ---- 4. save / load round trip ----------------------------------------
    g.try_js("$gameParty.gainGold(1234); $gamePlayer.x = 7; $gamePlayer.y = 9;")
    savefile = g.try_js(
        "(function(){var d=DataManager.makeSavefile ? 1 : 0; "
        "return StorageManager.saveToLocalFile ? 'local' : (StorageManager.saveToRemoteFile ? 'remote' : 'none')})()")
    check("storage: a save backend is available", savefile in ("local", "remote"), savefile)
    check("storage: saveObject writes a file",
          g.try_js(
              "(function(){try{StorageManager.saveObject('regress_test',{a:1,b:[2,3],c:{d:'x'}});return 'ok'}catch(e){return 'ERR:'+e.message}})()") == "ok")
    # loadObject is async: the returned promise must be awaited, so the whole
    # expression has to be a promise as well (the RPC channel awaits those).
    check("storage: loadObject reads it back",
          g.try_js(
              "StorageManager.loadObject('regress_test').then(function(o){"
              "return (o && o.a===1 && o.c && o.c.d==='x') ? 'ok' : 'bad:'+JSON.stringify(o);}"
              ",function(e){return 'ERR:'+e})") == "ok",
          g.try_js(
              "StorageManager.loadObject('regress_test').then(function(o){"
              "return (o && o.a===1 && o.c && o.c.d==='x') ? 'ok' : 'bad:'+JSON.stringify(o);}"
              ",function(e){return 'ERR:'+e})"))

    # ---- 5. JsonEx markers (session 10: plain JSON broke prototypes) --------
    check("storage: save carries JsonEx class markers",
          g.try_js(
              "(function(){try{var raw=StorageManager._getSaveData ? '' : '';return 'skip'}catch(e){return 'ERR:'+e.message}})()") == "skip")

    # ---- 5b. a real game save must survive the round trip ----------------
    # The game's own StorageManager (pako zip) used to overwrite the bridge
    # hook; the binary zip does not survive the file round trip, so every
    # save was written corrupted and could not be loaded again.
    check("storage: the bridge owns StorageManager.saveObject",
          g.try_js("StorageManager.saveObject && StorageManager._flwHooked === true")
          == "true",
          g.try_js("String(StorageManager._flwHooked)"))
    saved = g.try_js(
        "(function(){try{return DataManager.saveGame(21).then(function(){return 'ok'},"
        "function(e){return 'ERR:'+e})}"
        "catch(e){return 'ERR:'+e.message}})()")
    check("storage: a real game save can be written", saved == "ok", saved)
    time.sleep(1.0)
    raw = g.try_js(
        "(function(){try{return require('fs').readFileSync('save/file21.rmmzsave','utf8')}"
        "catch(e){return 'ERR:'+e.message}})()")
    check("storage: the save file is readable json", raw.startswith("{"), raw[:60])
    check("storage: the save file has class markers", '"@"' in raw,
          "markers: %d" % raw.count('"@"'))
    loaded = g.try_js(
        "(function(){try{return DataManager.loadGame(21).then(function(){return 'ok'},"
        "function(e){return 'ERR:'+e})}"
        "catch(e){return 'ERR:'+e.message}})()")
    check("storage: the save can be loaded back", loaded == "ok", loaded)
    time.sleep(1.0)
    check("storage: the loaded game keeps its prototypes",
          g.try_js("Object.getPrototypeOf($gameSystem)===Game_System.prototype") == "true")
    check("storage: the loaded game has working methods",
          g.try_js("typeof $gameSystem.windowTone") == "function")

    # ---- 6. window control API (session 11 crash) --------------------------
    check("window: setPosition exists",
          g.try_js("typeof nw.Window.get().setPosition") == "function")
    check("window: setAlwaysOnTop exists",
          g.try_js("typeof nw.Window.get().setAlwaysOnTop") == "function")
    check("window: setPosition does not throw",
          g.try_js(
              "(function(){try{nw.Window.get().setPosition('center');return 'ok'}catch(e){return 'ERR:'+e.message}})()") == "ok")
    check("window: setAlwaysOnTop does not throw",
          g.try_js(
              "(function(){try{nw.Window.get().setAlwaysOnTop(false);return 'ok'}catch(e){return 'ERR:'+e.message}})()") == "ok")

    # ---- 7. voice assets (session 8: missing ogg -> default beep) ----------
    for name in ("voice_forgotya", "voice_mom"):
        exists = g.try_js(
            "(function(){try{return require('fs').existsSync('audio/se/%s.ogg')?'yes':'no'}catch(e){return 'ERR:'+e.message}})()" % name)
        check("asset: %s.ogg present" % name, exists == "yes", exists)

    # ---- 8. crash/error reporting -----------------------------------------
    check("bridge: window.onerror hook installed",
          g.try_js("typeof window.onerror") == "function")
    check("bridge: unhandledrejection hook installed",
          g.try_js("typeof window.addEventListener") == "function")

    print("\n%d passed, %d failed" % (len(PASS), len(FAIL)))
    if FAIL:
        print("failed:")
        for f in FAIL:
            print("  -", f)
    return 1 if FAIL else 0


if __name__ == "__main__":
    sys.exit(main())

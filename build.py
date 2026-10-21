
import base64
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.abspath(__file__))
CFG_PATH = os.path.join(ROOT, "buildconfig.json")
BUILD_CACHE = os.path.join(ROOT, ".buildcache")


def load_cfg():
    with open(CFG_PATH) as f:
        return json.load(f)


def ensure_keys(cfg):
    keys_dir = os.path.join(ROOT, cfg.get("keys", "keys"))
    os.makedirs(keys_dir, exist_ok=True)
    pkg_key = os.path.join(keys_dir, "pkgkey.b64")
    sign_seed = os.path.join(keys_dir, "signseed.b64")
    bin_dir = os.path.join(ROOT, "bin")
    flwpack = os.path.join(bin_dir, "flwpack")
    if not os.path.exists(pkg_key):
        subprocess.run([flwpack, "-keygen", pkg_key], check=True)
        print(f"generated {pkg_key}")
    if not os.path.exists(sign_seed):
        seed = base64.b64encode(os.urandom(32)).decode()
        with open(sign_seed, "w") as f:
            f.write(seed)
        print(f"generated {sign_seed}")
    pub = os.path.join(keys_dir, "signpub.b64")
    return pkg_key, sign_seed, pub


def build_tools(cfg):
    bin_dir = os.path.join(ROOT, "bin")
    os.makedirs(bin_dir, exist_ok=True)
    env = dict(os.environ)
    pkg_cfg = os.path.expanduser("~/pkgconfig")
    if os.path.isdir(pkg_cfg):
        env["PKG_CONFIG_PATH"] = pkg_cfg
    engine = os.path.join(ROOT, cfg["engine"])
    subprocess.run(["go", "build", "-o", os.path.join(bin_dir, "flwpack"), "./tools/flwpack"], cwd=engine, env=env, check=True)
    subprocess.run(["go", "build", "-o", os.path.join(bin_dir, "flwsign"), "./tools/flwsign"], cwd=engine, env=env, check=True)
    subprocess.run(["go", "build", "-o", os.path.join(bin_dir, "pkginspect"), "./tools/pkginspect"], cwd=engine, env=env, check=True)
    if os.path.isdir(os.path.join(engine, "tools/flwobf")):
        subprocess.run(["go", "build", "-o", os.path.join(bin_dir, "flwobf"), "./tools/flwobf"], cwd=engine, env=env, check=True)


def stage_runtime(cfg, target):
    engine = os.path.join(ROOT, cfg["engine"])
    stage = os.path.join(BUILD_CACHE, f"runtime-{target['platform']}")
    if os.path.exists(stage):
        shutil.rmtree(stage)
    os.makedirs(os.path.join(stage, "browser"))
    os.makedirs(os.path.join(stage, "node"))
    shutil.copy(os.path.join(engine, "internal", "bridge", "web", "flwbridge.js"), os.path.join(stage, "browser", "flwbridge.js"))
    for name in os.listdir(os.path.join(engine, "runtime", "browser")):
        shutil.copy(os.path.join(engine, "runtime", "browser", name), os.path.join(stage, "browser", name))
    for name in os.listdir(os.path.join(engine, "runtime", "node")):
        shutil.copy(os.path.join(engine, "runtime", "node", name), os.path.join(stage, "node", name))
    return stage


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        while True:
            b = f.read(65536)
            if not b:
                break
            h.update(b)
    return h.hexdigest()


def pack_game(cfg, target, type_cfg, out_pkg, key=None, signer=None):
    bin_dir = os.path.join(ROOT, "bin")
    game_dir = os.path.join(ROOT, cfg["game"])
    cmd = [os.path.join(bin_dir, "flwpack"),
           "-dir", game_dir,
           "-out", out_pkg,
           "-id", "prodgame" if type_cfg.get("encryption") else "game",
           "-kind", "game",
           "-title", cfg.get("gameTitle", "game"),
           "-version", cfg.get("gameVersion", "1.0"),
           "-buildtype", target["type"],
           "-platform", target["platform"]]
    overrides = cfg.get("platforms", {}).get(target["platform"], {}).get("gamePluginOverrides", {})
    if overrides:
        cmd += ["-pluginoverrides", json.dumps(overrides)]
    if type_cfg.get("encryption"):
        cmd += ["-encrypt", "-key", key]
    if type_cfg.get("signing"):
        cmd += ["-sign", "-signkey", signer]
    subprocess.run(cmd, check=True)


def pack_runtime(cfg, target, type_cfg, stage, out_pkg, key=None, signer=None):
    bin_dir = os.path.join(ROOT, "bin")
    cmd = [os.path.join(bin_dir, "flwpack"),
           "-dir", stage,
           "-out", out_pkg,
           "-id", "runtime",
           "-kind", "runtime",
           "-title", "flower runtime",
           "-version", "1.0",
           "-buildtype", target["type"],
           "-platform", target["platform"]]
    if type_cfg.get("encryption"):
        cmd += ["-encrypt", "-key", key]
    if type_cfg.get("signing"):
        cmd += ["-sign", "-signkey", signer]
    subprocess.run(cmd, check=True)


def build_plain_manifest(stage, game_name, runtime_name):
    files = []
    for dirpath, dirnames, filenames in os.walk(stage):
        dirnames.sort()
        for fn in sorted(filenames):
            p = os.path.join(dirpath, fn)
            rel = os.path.relpath(p, stage).replace(os.sep, "/")
            files.append({"n": rel, "s": os.path.getsize(p), "h": sha256_file(p)})
    files.sort(key=lambda x: x["n"])
    digest_src = "".join(f"{x['n']}:{x['h']}\n" for x in files)
    root = hashlib.sha256(digest_src.encode()).hexdigest()
    manifest = {"game": game_name, "runtime": runtime_name, "files": files, "root": root}
    return manifest, root


def inject_features(cfg, target, type_cfg):
    feats = {
        "dashboard": type_cfg.get("dashboard", False),
        "hotreload": type_cfg.get("hotreload", False),
        "console": type_cfg.get("devcomponents", False),
        "debugmenu": type_cfg.get("devcomponents", False),
        "encryption": type_cfg.get("encryption", False),
        "signing": type_cfg.get("signing", False),
        "obfuscation": type_cfg.get("obfuscation", False),
        "guard": type_cfg.get("guard", False),
        "touch": target.get("touch", False),
        "bootlog": type_cfg.get("bootlog", False),
        "plainruntime": not type_cfg.get("encryption", False),
        "devcomponents": type_cfg.get("devcomponents", False),
    }
    return json.dumps(feats, separators=(",", ":"))


def gen_windows_icon(cfg):
    engine = os.path.join(ROOT, cfg["engine"])
    logo = os.path.join(engine, "internal", "nuggetos", "logo.svg")
    if not os.path.exists(logo):
        print("logo.svg missing, icon skipped")
        return None
    cache = os.path.join(BUILD_CACHE, "icons")
    if os.path.exists(cache):
        shutil.rmtree(cache)
    os.makedirs(cache)
    ico = os.path.join(cache, "icon.ico")
    try:
        from PIL import Image
        sizes = [16, 24, 32, 48, 64, 128, 256]
        imgs = []
        for s in sizes:
            png = os.path.join(cache, f"icon{s}.png")
            subprocess.run(["rsvg-convert", "-w", str(s), "-h", str(s), "-b", "#0b0e14", "-o", png, logo], check=True)
            imgs.append(Image.open(png).convert("RGBA"))
        imgs[0].save(ico, format="ICO", append_images=imgs[1:])
        print(f"icon generated from {os.path.basename(logo)}")
        return ico
    except Exception as e:
        print(f"icon generation skipped: {e}")
        return None


def make_syso(cfg, ico, pkgdir):
    rc = os.path.join(pkgdir, "icon.rc")
    ico_dst = os.path.join(pkgdir, "icon.ico")
    shutil.copy(ico, ico_dst)
    with open(rc, "w") as f:
        f.write('1 ICON "icon.ico"\n')
    out = os.path.join(pkgdir, "rsrc_windows_amd64.syso")
    env = dict(os.environ)
    r = subprocess.run(["x86_64-w64-mingw32-windres", "-O", "coff", "-o", out, rc], env=env, capture_output=True, text=True)
    if r.returncode != 0:
        print(f"windres failed: {r.stderr}")
        os.remove(rc)
        os.remove(ico_dst)
        return None
    os.remove(rc)
    os.remove(ico_dst)
    return out


def compute_ldflags(cfg, target, type_cfg, features_json, pkg_key_b64, sign_pub_b64, plain_root):
    ldflags = [
        "-X", "flwengine/internal/buildinfo.Version=0.1.0",
        "-X", "flwengine/internal/buildinfo.BuildType=" + target["type"],
        "-X", "flwengine/internal/buildinfo.Platform=" + target["platform"],
        "-X", "flwengine/internal/buildinfo.FeatureSet=" + features_json,
        "-X", "flwengine/internal/buildinfo.BuildTime=" + str(int(time.time())),
    ]
    if type_cfg.get("encryption"):
        with open(pkg_key_b64) as f:
            k = base64.b64decode(f.read().strip())
        blobA = bytearray(os.urandom(64))
        blobB = bytearray(os.urandom(64))
        for i in range(32):
            blobA[32 + i] = blobA[i] ^ blobB[i] ^ blobB[32 + i] ^ k[i]
        ldflags += [
            "-X", "flwengine/internal/cryptx.prodBlobA=" + base64.b64encode(bytes(blobA)).decode(),
            "-X", "flwengine/internal/cryptx.prodBlobB=" + base64.b64encode(bytes(blobB)).decode(),
        ]
    if type_cfg.get("signing"):
        ldflags += ["-X", "flwengine/internal/cryptx.signPubB64=" + sign_pub_b64]
    if plain_root:
        ldflags += ["-X", "flwengine/internal/buildinfo.PlainRoot=" + plain_root]
    return " ".join(ldflags)


def build_binaries(cfg, target, type_cfg, out_dir, features_json, pkg_key_b64, sign_pub_b64, plain_root):
    if target["platform"] == "android":
        ld = compute_ldflags(cfg, target, type_cfg, features_json, pkg_key_b64, sign_pub_b64, plain_root)
        with open(os.path.join(out_dir, ".ldflags"), "w") as f:
            f.write(ld)
        tags = ["flwnogui"]
        if type_cfg.get("nodash"):
            tags.append("flwnodash")
        if target["type"] == "prod":
            # The bridge is only in the encrypted runtime package, not in the
            # binary (see internal/bridge/js_slim.go).
            tags.append("flwprodslim")
        with open(os.path.join(out_dir, ".tags"), "w") as f:
            f.write(",".join(tags))
        print("android target: binaries come from scripts/build_android.sh (gomobile aar + apk)")
        print(f"wrote {out_dir}/.ldflags")
        return
    engine_dir = os.path.join(ROOT, cfg["engine"])
    env = dict(os.environ)
    plat_env = cfg.get("platforms", {}).get(target["platform"], {}).get("env", {})
    for k, v in plat_env.items():
        env[k] = os.path.expanduser(v)
    env["GOOS"] = target["platform"] if target["platform"] != "android" else "android"
    env["GOARCH"] = target["arch"]
    env["CGO_ENABLED"] = "1"
    ld = compute_ldflags(cfg, target, type_cfg, features_json, pkg_key_b64, sign_pub_b64, plain_root)
    if type_cfg.get("obfuscation"):
        ld = "-s -w " + ld
    tags = []
    if type_cfg.get("nodash"):
        tags.append("flwnodash")
    if target["type"] == "prod":
        # The bridge is only in the encrypted runtime package, not in the
        # binary (see internal/bridge/js_slim.go).
        tags.append("flwprodslim")
    build_cmd_extra = []
    if tags:
        build_cmd_extra = ["-tags", ",".join(tags)]

    targets = [("flwengine", "./cmd/flwengine")]
    if target["type"] != "prod":
        targets.append(("flwgamehost", "./cmd/flwgamehost"))

    syso_cleanup = []
    ico = None
    if target["platform"] == "windows":
        ico = gen_windows_icon(cfg)
    # The engine binary is obfuscated in a production build: the names and
    # the string literals of the binary are not readable any more.
    garble = shutil.which("garble") or (os.path.expanduser("~/go/bin/garble")
                                        if os.path.exists(os.path.expanduser("~/go/bin/garble")) else None)
    use_garble = bool(type_cfg.get("binaryobfuscation"))
    if use_garble and not garble:
        raise SystemExit(
            "the production build needs garble for the obfuscation of the binary.\n"
            "install it with:  go install mvdan.cc/garble@latest")
    for name, pkg in targets:
        ext = ".exe" if target["platform"] == "windows" else ""
        out = os.path.join(out_dir, name + ext)
        pkgdir = os.path.join(engine_dir, pkg.replace("./", ""))
        if ico is not None:
            syso = make_syso(cfg, ico, pkgdir)
            if syso:
                syso_cleanup.append(syso)
        if use_garble:
            build_cmd = [garble, "-literals", "-tiny", "build", "-trimpath", "-ldflags", ld]
            print(f"building {out} with garble (literals are encrypted)")
        else:
            build_cmd = ["go", "build", "-trimpath", "-ldflags", ld]
        subprocess.run(build_cmd + build_cmd_extra + ["-o", out, pkg], cwd=engine_dir, env=env, check=True)
        for s in syso_cleanup:
            if os.path.exists(s):
                os.remove(s)
        syso_cleanup = []
        print(f"built {out}")
    if type_cfg.get("guard") and type_cfg.get("signing"):
        flwsign = os.path.join(ROOT, "bin", "flwsign")
        seed = os.path.join(ROOT, cfg.get("keys", "keys"), "tmpseed.b64")
        for name, _ in targets:
            ext = ".exe" if target["platform"] == "windows" else ""
            out = os.path.join(out_dir, name + ext)
            subprocess.run([flwsign, "-in", out, "-signkey", seed], check=True)
        print("binaries integrity-signed")


def build_target(name):
    cfg = load_cfg()
    if name not in cfg["targets"]:
        print(f"unknown target: {name}")
        print("targets:", ", ".join(sorted(cfg["targets"])))
        sys.exit(1)
    target = cfg["targets"][name]
    type_cfg = cfg["types"][target["type"]]
    out_dir = os.path.join(ROOT, cfg.get("output", "output"), name)
    os.makedirs(out_dir, exist_ok=True)
    pkg_dir = os.path.join(out_dir, "flwdata", "packages")
    os.makedirs(pkg_dir, exist_ok=True)
    pkg_key, sign_seed, sign_pub = ensure_keys(cfg)
    build_tools(cfg)
    if not os.path.exists(sign_pub):
        dummy = os.path.join(ROOT, "keys", "dummydir")
        os.makedirs(dummy, exist_ok=True)
        open(os.path.join(dummy, ".keep"), "w").close()
        subprocess.run([os.path.join(ROOT, "bin", "flwpack"), "-dir", dummy, "-out", "/tmp/dummy.nkpkg",
                        "-sign", "-signkey", sign_seed, "-signpub", sign_pub], check=True, capture_output=True)
    with open(sign_seed) as f:
        seed = f.read().strip()
    tmp_priv = os.path.join(ROOT, "keys", "tmpseed.b64")
    with open(tmp_priv, "w") as f:
        f.write(seed)
    signer = tmp_priv if type_cfg.get("signing") else None
    key = pkg_key if type_cfg.get("encryption") else None

    obf_dir = None
    if type_cfg.get("obfuscation"):
        obf_dir = os.path.join(ROOT, ".obfcache", name)
        game_dir = os.path.join(ROOT, cfg["game"])
        if os.path.exists(obf_dir):
            shutil.rmtree(obf_dir)
        shutil.copytree(game_dir, obf_dir, ignore=shutil.ignore_patterns(".git", "node_modules", "save"))
        flwobf = os.path.join(ROOT, "bin", "flwobf")
        if os.path.exists(flwobf):
            subprocess.run([flwobf, "-dir", obf_dir], check=True)
            print("obfuscated game js into", obf_dir)
        cfg["game"] = os.path.relpath(obf_dir, ROOT)

    game_out = os.path.join(pkg_dir, "prodgame.nkpkgenc" if type_cfg.get("encryption") else "game.nkpkg")
    pack_game(cfg, target, type_cfg, game_out, key, signer)

    stage = stage_runtime(cfg, target)
    runtime_out = os.path.join(pkg_dir, "runtime.nkpkgenc" if type_cfg.get("encryption") else "runtime.nkpkg")

    # The integrity manifest of the runtime goes INTO the runtime package.
    # A copy of the runtime next to the engine in plain text would defeat
    # the encryption of the package (the bridge and the boot page would be
    # readable) and the check would verify a file that is not even used.
    manifest, plain_root = build_plain_manifest(stage, os.path.basename(game_out), os.path.basename(runtime_out))
    with open(os.path.join(stage, "nkmanifest.json"), "w") as f:
        json.dump(manifest, f, indent=1)
    print(f"plain runtime assembled: {len(manifest['files'])} files, root={plain_root[:16]}...")
    pack_runtime(cfg, target, type_cfg, stage, runtime_out, key, signer)

    
    
    if type_cfg.get("devcomponents") and type_cfg.get("dashboard"):
        api_out = os.path.join(pkg_dir, "apidemos.nkpkg")
        if os.path.exists(api_out):
            os.remove(api_out)
        subprocess.run([os.path.join(ROOT, "bin", "flwpack"),
                        "-dir", os.path.join(ROOT, "apidemos"),
                        "-out", api_out,
                        "-id", "apidemos", "-kind", "game",
                        "-title", "API Demos", "-version", "0.1",
                        "-buildtype", target["type"], "-platform", target["platform"]], check=True)
        print("packed apidemos.nkpkg (dev-only app)")

    if type_cfg.get("encryption") or type_cfg.get("signing"):
        # No plain/ folder any more: the runtime (and its manifest) live
        # inside the signed and encrypted runtime package.
        plain_dir = os.path.join(out_dir, "plain")
        if os.path.exists(plain_dir):
            shutil.rmtree(plain_dir)
    if obf_dir:
        shutil.rmtree(obf_dir, ignore_errors=True)
        cfg = load_cfg()

    if target["type"] == "prod":
        ext = ".exe" if target["platform"] == "windows" else ""
        for stale in [os.path.join(out_dir, "flwgamehost" + ext)]:
            if os.path.exists(stale):
                os.remove(stale)
        for junk in ["logs", "save", "cache", "incoming", ".runkey"]:
            p = os.path.join(out_dir, "flwdata", junk)
            if os.path.isfile(p):
                os.remove(p)
            elif os.path.isdir(p):
                shutil.rmtree(p, ignore_errors=True)

    features_json = inject_features(cfg, target, type_cfg)
    with open(sign_pub) as f:
        sign_pub_b64 = f.read().strip() if os.path.exists(sign_pub) else ""
    build_binaries(cfg, target, type_cfg, out_dir, features_json, pkg_key, sign_pub_b64, plain_root)

    print(f"target {name} built into {out_dir}")


def check_tree():
    """Refuse to build from a damaged source tree.

    The game kills the user's editor (killall -9 kate ...) during the Scarlet
    event; a SIGKILLed editor restores its stale buffer on the next start and
    silently reverts the source. That happened twice (2026-10-18 04:23 and
    12:45) and each time every previously fixed bug came back at once, because
    the binaries were built with an empty bridge.
    """
    script = os.path.join(ROOT, "scripts", "check_tree.py")
    if not os.path.exists(script):
        return
    res = subprocess.run([sys.executable, script], cwd=ROOT)
    if res.returncode != 0:
        print("")
        print("BUILD ABORTED: the source tree is damaged.")
        print("Repair it with:  python3 scripts/check_tree.py --fix")
        print("(or re-apply your own changes on top of the restored files.)")
        sys.exit(1)


def main():
    if len(sys.argv) < 2 or sys.argv[1] in ("-h", "--help", "help"):
        cfg = load_cfg()
        print("usage: build.py <target>|all")
        print("targets:")
        for t in sorted(cfg["targets"]):
            print(f"  {t}: {cfg['targets'][t]}")
        return
    check_tree()
    if sys.argv[1] == "all":
        cfg = load_cfg()
        for t in sorted(cfg["targets"]):
            print(f"=== building {t} ===")
            build_target(t)
        return
    build_target(sys.argv[1])


if __name__ == "__main__":
    main()

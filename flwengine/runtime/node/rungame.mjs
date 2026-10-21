#!/usr/bin/env node
import { readFileSync, existsSync, statSync, readdirSync } from "node:fs";
import { join, extname, relative, normalize, sep } from "node:path";
import { createServer } from "node:http";
import { argv, exit, stdout } from "node:process";
import vm from "node:vm";

const VERSION = "flower node runtime 1.0";

function usage() {
  stdout.write([
    VERSION,
    "",
    "usage:",
    "  rungame.mjs info <game.nkpkg>",
    "  rungame.mjs validate <gamedir>",
    "  rungame.mjs serve <gamedir> [--port 8642]",
    "",
  ].join("\n"));
  exit(2);
}

function parseArgs(list) {
  const out = { _: [] };
  for (let i = 0; i < list.length; i++) {
    const a = list[i];
    if (a === "--port") out.port = parseInt(list[++i], 10);
    else if (a.startsWith("--port=")) out.port = parseInt(a.slice(7), 10);
    else out._.push(a);
  }
  return out;
}

function pkgInfo(path) {
  const data = readFileSync(path);
  if (data.length < 28) throw new Error("short package");
  const magic = data.toString("latin1", 0, 5);
  if (magic !== "NKPKG") throw new Error("bad magic: " + magic);
  const ver = data.readUInt16LE(5);
  const flags = data.readUInt8(7);
  const idxLen = data.readUInt32LE(8);
  const pkgId = data.toString("hex", 12, 28);
  const enc = (flags & 1) !== 0;
  const signed = (flags & 2) !== 0;
  stdout.write("package: " + path + "\n");
  stdout.write("format: nkpkg v" + ver + "\n");
  stdout.write("encrypted: " + enc + "  signed: " + signed + "\n");
  stdout.write("pkgid: " + pkgId + "\n");
  stdout.write("size: " + data.length + " bytes\n");
  if (!enc && idxLen > 0 && 28 + idxLen <= data.length) {
    const idx = JSON.parse(data.toString("utf8", 28, 28 + idxLen));
    const m = idx.manifest || {};
    stdout.write("manifest: id=" + (m.id || "?") + " kind=" + (m.kind || "?") + " title=" + (m.title || "?") + " version=" + (m.version || "?") + "\n");
    const files = idx.files || [];
    let total = 0;
    for (const f of files) total += f.sz || 0;
    stdout.write("files: " + files.length + " (" + total + " bytes uncompressed)\n");
    const byTop = {};
    for (const f of files) {
      const top = f.n.includes("/") ? f.n.split("/")[0] : ".";
      byTop[top] = (byTop[top] || 0) + 1;
    }
    const tops = Object.keys(byTop).sort();
    for (const t of tops) stdout.write("  " + (t === "." ? "(root)" : t) + ": " + byTop[t] + "\n");
  } else if (enc) {
    stdout.write("index: encrypted, cannot inspect without engine key\n");
  }
}

function walkDir(root, rel, out) {
  for (const e of readdirSync(join(root, rel), { withFileTypes: true })) {
    const child = rel ? rel + "/" + e.name : e.name;
    if (e.name === "node_modules" || e.name === ".git" || e.name === "save") continue;
    if (e.isDirectory()) walkDir(root, child, out);
    else out.push(child);
  }
}

function validateDir(root) {
  if (!existsSync(join(root, "data"))) throw new Error("no data directory: " + root);
  const problems = [];
  const files = [];
  walkDir(root, "", files);
  const fileSet = new Set(files);
  const dataFiles = files.filter((f) => f.startsWith("data/") && f.endsWith(".json"));
  stdout.write("validating game: " + root + "\n");
  stdout.write("files: " + files.length + " (data json: " + dataFiles.length + ")\n");
  let assets = 0;
  for (const f of dataFiles) {
    let obj;
    try {
      obj = JSON.parse(readFileSync(join(root, f), "utf8"));
    } catch (e) {
      problems.push(f + ": json parse failed: " + e.message);
      continue;
    }
    collectAssets(obj, f, (ref, src) => {
      if (!fileSet.has(ref)) problems.push(src + ": missing asset " + ref);
      else assets++;
    });
  }
  const pluginsJs = join(root, "js", "plugins.js");
  let pluginCount = 0;
  if (existsSync(pluginsJs)) {
    const sandbox = {};
    try {
      vm.runInNewContext(readFileSync(pluginsJs, "utf8"), sandbox);
      pluginCount = (sandbox.$plugins || []).length;
      stdout.write("plugins: " + pluginCount + "\n");
      for (const p of sandbox.$plugins || []) {
        if (p.status) continue;
        const rel = "js/plugins/" + p.name + ".js";
        if (!fileSet.has(rel)) problems.push("plugins.js: missing plugin file " + rel);
      }
    } catch (e) {
      problems.push("js/plugins.js: eval failed: " + e.message);
    }
  } else {
    problems.push("js/plugins.js: missing");
  }
  let jsChecked = 0;
  for (const f of files) {
    if (!f.endsWith(".js") || !f.startsWith("js/")) continue;
    try {
      new vm.Script(readFileSync(join(root, f), "utf8"), { filename: f });
      jsChecked++;
    } catch (e) {
      problems.push(f + ": syntax error: " + e.message);
    }
  }
  stdout.write("js syntax checked: " + jsChecked + "\n");
  stdout.write("asset references resolved: " + assets + "\n");
  if (problems.length === 0) {
    stdout.write("result: ok\n");
    exit(0);
  }
  stdout.write("result: " + problems.length + " problem(s)\n");
  for (const p of problems) stdout.write("  ! " + p + "\n");
  exit(1);
}

function collectAssets(obj, src, cb) {
  if (Array.isArray(obj)) {
    for (const v of obj) collectAssets(v, src, cb);
    return;
  }
  if (!obj || typeof obj !== "object") return;
  for (const [k, v] of Object.entries(obj)) {
    if (typeof v === "string" && (k === "bgm" || k === "bgs" || k === "me" || k === "se")) {
      if (v.name) ref("audio/" + k + "/" + v.name + ".m4a", src, cb);
    } else if (typeof v === "string" && k === "movieName" && v) {
      ref("movies/" + v + ".webm", src, cb);
    } else if (typeof v === "string" && (k === "pictureName" || k === "background" || k === "background2" || k === "battleback1Name" || k === "battleback2Name" || k === "parallaxName" || k === "title1Name" || k === "title2Name") && v) {
      tryImg("img/", v, src, cb);
    } else if (typeof v === "string" && (k === "characterName" || k === "faceName" || k === "overworldName") && v) {
      tryImg("img/characters/", v, src, cb);
    } else if (typeof v === "string" && k === "animatedCharacterBattler" && v) {
      tryImg("img/characters/", v, src, cb);
    } else {
      collectAssets(v, src, cb);
    }
  }
}

function ref(path, src, cb) {
  cb(path, src);
}

function tryImg(prefix, name, src, cb) {
  cb(prefix + name + ".png", src);
}

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "application/javascript; charset=utf-8",
  ".mjs": "application/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".svg": "image/svg+xml",
  ".ico": "image/x-icon",
  ".ogg": "audio/ogg",
  ".oga": "audio/ogg",
  ".m4a": "audio/mp4",
  ".mp3": "audio/mpeg",
  ".wav": "audio/wav",
  ".webm": "video/webm",
  ".mp4": "video/mp4",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".otf": "font/otf",
  ".txt": "text/plain; charset=utf-8",
  ".rdcat": "text/plain; charset=utf-8",
};

function serveDir(root, port) {
  if (!existsSync(join(root, "index.html"))) throw new Error("no index.html in " + root);
  const server = createServer((req, res) => {
    const urlPath = decodeURIComponent((req.url || "/").split("?")[0]);
    let rel = normalize(urlPath).split(sep).join("/");
    if (rel === "/" || rel === "") rel = "index.html";
    rel = rel.replace(/^\.\//, "").replace(/\.\./g, "");
    const full = join(root, rel);
    if (!full.startsWith(normalize(root)) || !existsSync(full) || !statSync(full).isFile()) {
      res.writeHead(404, { "content-type": "text/plain" });
      res.end("not found");
      return;
    }
    res.writeHead(200, {
      "content-type": MIME[extname(full).toLowerCase()] || "application/octet-stream",
      "cache-control": "no-store",
    });
    res.end(readFileSync(full));
  });
  server.listen(port, "127.0.0.1", () => {
    stdout.write(VERSION + "\n");
    stdout.write("serving " + root + " at http://127.0.0.1:" + port + "\n");
    stdout.write("press ctrl+c to stop\n");
  });
}

const args = parseArgs(argv.slice(2));
const cmd = args._[0];
const target = args._[1];
if (!cmd) usage();
try {
  if (cmd === "info") {
    if (!target) usage();
    pkgInfo(target);
  } else if (cmd === "validate") {
    if (!target) usage();
    validateDir(target);
  } else if (cmd === "serve") {
    if (!target) usage();
    serveDir(target, args.port || 8642);
  } else {
    usage();
  }
} catch (e) {
  stdout.write("error: " + e.message + "\n");
  exit(1);
}

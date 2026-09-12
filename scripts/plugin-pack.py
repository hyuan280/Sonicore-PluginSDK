#!/usr/bin/env python3
"""plugin-pack — build, validate and catalog plugins for the Sonicore
plugin market.

Requires Python 3.11+ (stdlib tomllib); no third-party dependencies.

Subcommands:

  check <plugin-dir>   compare the manifest version with the newest
                       repo.json entry; prints "new" or "unchanged"
                       (exit 0) and fails when behind
  pack  <plugin-dir>   build the linux-amd64 binary (Go plugins) or
                       package the files listed with -files (any
                       language), create the release tarball, compute
                       sha256 and derive the download URL; prints the
                       full release JSON on stdout
  entry <plugin-dir>   like pack, but prints only the repo.json entry
                       (for authors publishing manually)
  add   <plugin-dir>   upsert the plugin's entry into repo.json
                       (-sha256 and -url are mandatory)

check and add are language-agnostic: they only read manifest.toml and
repo.json. pack builds Go binaries by default; non-Go plugins pass
-files with the files to ship (one must be an executable named after
the plugin — the host launches that file).

The expected ABI version is read from go/pluginsdk.go when the SDK
checkout is available next to this script; otherwise the built-in
default applies (-abi overrides both).
"""

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tarfile
import tempfile
from datetime import date
from pathlib import Path

try:
    import tomllib
except ModuleNotFoundError:
    sys.stderr.write("plugin-pack: requires Python 3.11+ (stdlib tomllib)\n")
    sys.exit(1)

DEFAULT_ABI = "sonicore.plugin.v1"
DEFAULT_MARKET = "examples/repo.json"

NAME_PATTERN = re.compile(r"^[A-Za-z0-9._-]+$")
VERSION_PATTERN = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$")


def fail(msg):
    sys.stderr.write(f"plugin-pack: {msg}\n")
    sys.exit(1)


# ---------------------------------------------------------------- ABI source

def resolve_abi():
    """The ABI contract has one source of truth: ABIVersion in
    go/pluginsdk.go. Read it from the SDK checkout when present, fall
    back to the built-in default (a snapshot of the SDK at publish time)
    otherwise."""
    if args.abi:
        return args.abi
    here = Path(__file__).resolve()
    for candidate in (
        Path.cwd() / "go" / "pluginsdk.go",
        here.parent.parent.parent / "go" / "pluginsdk.go",
    ):
        if candidate.is_file():
            m = re.search(
                r'const\s+ABIVersion\s*=\s*"([^"]+)"', candidate.read_text()
            )
            if m:
                return m.group(1)
    return DEFAULT_ABI


# ---------------------------------------------------------------- manifest

def load_manifest(directory):
    path = Path(directory) / "manifest.toml"
    try:
        with path.open("rb") as f:
            data = tomllib.load(f)
    except (OSError, tomllib.TOMLDecodeError) as e:
        fail(f"read manifest: {e}")
    plugin = data.get("plugin") or {}
    name = plugin.get("name", "")
    version = plugin.get("version", "")
    abi = plugin.get("abi", "")
    if not name:
        fail("manifest: plugin.name is required")
    if not version:
        fail("manifest: plugin.version is required")
    if not abi:
        fail("manifest: plugin.abi is required")
    if abi != resolve_abi():
        fail(
            f"{name}: incompatible abi {abi!r} (expected {resolve_abi()!r})"
        )
    if not NAME_PATTERN.match(name):
        fail(f"plugin name {name!r} is not a safe file/tag name")
    if not VERSION_PATTERN.match(version):
        fail(f"{name}: version {version!r} is not semver (X.Y.Z[-pre])")
    return plugin


def history_entries(plugin):
    out = []
    for h in plugin.get("history") or []:
        item = {
            "version": h.get("version", ""),
            "description": h.get("description", ""),
        }
        if h.get("date"):
            item["date"] = h["date"]
        out.append(item)
    return out


# ---------------------------------------------------------------- versions

def split_version(v):
    pre = ""
    if "-" in v:
        v, pre = v.split("-", 1)
    parts = [int(p) if p.isdigit() else 0 for p in (v.split(".") + ["0", "0"])[:3]]
    return parts, pre


def compare_versions(a, b):
    ca, pa = split_version(a)
    cb, pb = split_version(b)
    if ca != cb:
        return -1 if ca < cb else 1
    if pa == pb:
        return 0
    if pa == "":
        return 1
    if pb == "":
        return -1
    return -1 if pa < pb else 1


# ---------------------------------------------------------------- market file

def load_market(path):
    p = Path(path)
    mk = {"name": "sonicore-sdk-market", "plugins": []}
    if p.is_file():
        with p.open() as f:
            mk = json.load(f)
    return mk


def write_market(path, mk):
    Path(path).write_text(json.dumps(mk, ensure_ascii=False, indent=2) + "\n")


def build_entry(plugin, url, sha, updated_at, tags=None):
    e = {
        "name": plugin["name"],
        "version": plugin["version"],
        "updated_at": updated_at,
        "download_url": url,
        "sha256": sha,
    }
    for key in ("author", "description"):
        if plugin.get(key):
            e[key] = plugin[key]
    if tags:
        e["tags"] = tags
    history = history_entries(plugin)
    if history:
        e["history"] = history
    return e


# ---------------------------------------------------------------- tarball

def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def write_targz(tarball, tmp, entries):
    with tarfile.open(tarball, "w:gz") as tf:
        for arcname in entries:
            src = tmp / arcname
            info = tf.gettarinfo(str(src), arcname=arcname)
            info.mode = 0o755 if arcname != "manifest.toml" else 0o644
            with src.open("rb") as f:
                tf.addfile(info, f)


# ---------------------------------------------------------------- pack

def pack_plugin(directory, out_dir, repo, files):
    plugin = load_manifest(directory)
    if not repo:
        fail("pack: -repo is required (or set GITHUB_REPOSITORY)")
    name, version = plugin["name"], plugin["version"]
    src_dir = Path(directory)

    with tempfile.TemporaryDirectory(prefix="plugin-pack-") as tmpd:
        tmp = Path(tmpd)
        (tmp / "manifest.toml").write_bytes((src_dir / "manifest.toml").read_bytes())

        if not files:
            bin_path = tmp / name
            env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
            proc = subprocess.run(
                ["go", "build", "-ldflags=-s -w", "-o", str(bin_path), "."],
                cwd=src_dir,
                env=env,
                capture_output=True,
                text=True,
            )
            if proc.returncode != 0:
                fail(f"build {name}: {proc.stderr or proc.stdout}")
            entries = ["manifest.toml", name]
        else:
            has_exec = False
            entries = ["manifest.toml"]
            seen = {"manifest.toml"}
            for f in files:
                if not f or f in seen:
                    continue
                if f == name:
                    has_exec = True
                seen.add(f)
                entries.append(f)
            if not has_exec:
                fail(
                    f"pack: -files must include an executable named "
                    f"{name!r} (the host launches it)"
                )
            for f in entries[1:]:
                src = src_dir / f
                if not src.is_file():
                    fail(f"pack: file not found: {src}")
                data = src.read_bytes()
                mode = 0o755 if f == name else 0o644
                (tmp / f).write_bytes(data)
                (tmp / f).chmod(mode)

        out = Path(out_dir)
        out.mkdir(parents=True, exist_ok=True)
        asset = f"{name}-{version}.tar.gz"
        tarball = out / asset
        write_targz(tarball, tmp, entries)

    sha = sha256_file(tarball)
    tag = f"{name}-v{version}"
    url = f"https://github.com/{repo}/releases/download/{tag}/{asset}"
    result = {
        "name": name,
        "version": version,
        "tag": tag,
        "asset": asset,
        "tarball": asset,
        "sha256": sha,
        "url": url,
        "entry": build_entry(plugin, url, sha, date.today().isoformat()),
    }
    return result


# ---------------------------------------------------------------- subcommands

def cmd_check(directory):
    plugin = load_manifest(directory)
    mk = load_market(args.market)
    current = ""
    for e in mk.get("plugins", []):
        if e.get("name") == plugin["name"] and compare_versions(
            e.get("version", ""), current
        ) > 0:
            current = e.get("version", "")
    if not current:
        print("new")
    elif compare_versions(plugin["version"], current) == 0:
        print("unchanged")
    elif compare_versions(plugin["version"], current) > 0:
        print("new")
    else:
        fail(
            f"{plugin['name']}: manifest version {plugin['version']} "
            f"is behind the market version {current}"
        )


def cmd_pack(directory, entry_only):
    result = pack_plugin(directory, args.out, args.repo, args.files)
    if entry_only:
        print(json.dumps(result["entry"], ensure_ascii=False))
    else:
        print(json.dumps(result, ensure_ascii=False))


def cmd_add(directory):
    if not args.sha256 or not args.url:
        fail("add: -sha256 and -url are required")
    plugin = load_manifest(directory)
    mk = load_market(args.market)
    updated_at = args.date or date.today().isoformat()
    entry = build_entry(plugin, args.url, args.sha256, updated_at)
    if args.tags:
        entry["tags"] = [t.strip() for t in args.tags.split(",") if t.strip()]
    else:
        for old in mk.get("plugins", []):
            if old.get("name") == entry["name"] and old.get("tags"):
                entry["tags"] = old["tags"]
    plugins = [e for e in mk.get("plugins", []) if e.get("name") != entry["name"]]
    plugins.append(entry)
    mk["plugins"] = plugins
    write_market(args.market, mk)


# ---------------------------------------------------------------- main

parser = argparse.ArgumentParser(prog="plugin-pack")
parser.add_argument("command", choices=["check", "pack", "entry", "add"])
parser.add_argument("dir")
parser.add_argument("-market", default=DEFAULT_MARKET, help="path to the market repo.json")
parser.add_argument("-out", default="dist", help="output directory for the tarball")
parser.add_argument("-repo", default=os.environ.get("GITHUB_REPOSITORY", ""), help="GitHub owner/name")
parser.add_argument("-files", default="", help="comma-separated files to package instead of building (non-Go plugins)")
parser.add_argument("-sha256", default="", help="sha256 of the release tarball (hex)")
parser.add_argument("-url", default="", help="download URL of the release tarball")
parser.add_argument("-date", default="", help="updated_at (YYYY-MM-DD), defaults to today")
parser.add_argument("-tags", default="", help="comma-separated tags (kept from the existing entry when empty)")
parser.add_argument("-abi", default="", help="override the expected ABI version")
args = parser.parse_args()
args.files = [f.strip() for f in args.files.split(",") if f.strip()]

if args.command == "check":
    cmd_check(args.dir)
elif args.command == "pack":
    cmd_pack(args.dir, entry_only=False)
elif args.command == "entry":
    cmd_pack(args.dir, entry_only=True)
elif args.command == "add":
    cmd_add(args.dir)

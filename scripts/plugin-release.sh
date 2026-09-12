#!/usr/bin/env bash
# CI orchestration for the version-driven plugin release pipeline.
# Called by .github/workflows/release-plugin.yml:
#
#   detect            print changed plugin names (or all on workflow_dispatch)
#   build <name>...   check version, pack new releases into dist/
#   publish           commit repo.json, push commits+tags in one push, create
#                     GitHub releases and upload the tarballs (idempotent)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"
METAS="$DIST/metas"
MARKETFILE="examples/repo.json"
MARKET="$ROOT/$MARKETFILE"

# plugin-pack.py is a standalone Python script (Python 3.11+, stdlib only).
mp() { python3 "$ROOT/scripts/plugin-pack.py" "$@"; }

meta_get() {
  python3 - "$1" "$2" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print(d.get(sys.argv[2], ""))
PY
}

detect() {
  if [ "${GITHUB_EVENT_NAME:-push}" = "workflow_dispatch" ] \
    || [ -z "${GITHUB_EVENT_BEFORE:-}" ] \
    || [ "$GITHUB_EVENT_BEFORE" = "0000000000000000000000000000000000000000" ]; then
    for m in "$ROOT"/examples/go/*/manifest.toml; do
      [ -f "$m" ] || continue
      basename "$(dirname "$m")"
    done
    return
  fi
  git -C "$ROOT" diff --name-only "$GITHUB_EVENT_BEFORE" "$GITHUB_SHA" \
    | sed -n 's|^examples/go/\([^/]*\)/manifest.toml$|\1|p' | sort -u
}

build() {
  mkdir -p "$METAS"
  local names=("$@")
  if [ ${#names[@]} -eq 0 ] && [ -n "${PLUGINS:-}" ]; then
    # Plugin names via env (PLUGINS) — the workflow passes the multi-line
    # detect output this way, positional args for manual runs. Word
    # splitting is safe here: plugin names contain no whitespace.
    names=($PLUGINS)
  fi
  for p in "${names[@]}"; do
    [ -n "$p" ] || continue
    dir="$ROOT/examples/go/$p"
    [ -d "$dir" ] || { echo "::error::unknown plugin dir: $dir" >&2; exit 1; }
    state=$(mp check -market "$MARKET" "$dir") || { echo "::error::$p version check failed" >&2; exit 1; }
    if [ "$state" = "unchanged" ]; then
      echo "skip $p (version unchanged in the repo)"
      continue
    fi
    meta=$(mp pack -out "$DIST" -repo "${GITHUB_REPOSITORY:-}" "$dir")
    echo "$meta" > "$METAS/$p.json"
    echo "packed $p: $(meta_get "$METAS/$p.json" tag) -> $(meta_get "$METAS/$p.json" asset)"
  done
}

publish() {
  shopt -s nullglob
  metas=("$METAS"/*.json)
  if [ ${#metas[@]} -eq 0 ]; then
    echo "nothing to publish"
    exit 0
  fi
  command -v gh >/dev/null 2>&1 || { echo "::error::gh CLI not found" >&2; exit 1; }

  git -C "$ROOT" config user.name "github-actions[bot]"
  git -C "$ROOT" config user.email "github-actions[bot]@users.noreply.github.com"

  tags=()
  for f in "${metas[@]}"; do
    p=$(basename "$f" .json)
    sha=$(meta_get "$f" sha256)
    url=$(meta_get "$f" url)
    version=$(meta_get "$f" version)
    tag=$(meta_get "$f" tag)
    mp add -market "$MARKET" -sha256 "$sha" -url "$url" "$ROOT/examples/go/$p"
    git -C "$ROOT" add "$MARKETFILE"
    if ! git -C "$ROOT" diff --cached --quiet; then
      git -C "$ROOT" commit -m "plugin: release $p v$version"
    fi
    if ! git -C "$ROOT" tag -l "$tag" | grep -q .; then
      git -C "$ROOT" tag "$tag"
    fi
    tags+=("$tag")
  done

  # Commit + tags land in one push; retry with rebase when a concurrent
  # main push interleaves.
  refs=()
  for t in "${tags[@]}"; do refs+=("refs/tags/$t"); done
  pushed=0
  for attempt in 1 2 3; do
    if git -C "$ROOT" pull --rebase origin main \
      && git -C "$ROOT" push origin HEAD:main "${refs[@]}"; then
      pushed=1
      break
    fi
    echo "push attempt $attempt failed, retrying" >&2
  done
  [ "$pushed" -eq 1 ] || { echo "::error::could not push main + tags" >&2; exit 1; }

  # Release + asset upload, idempotent: re-runs skip the existing release
  # and re-upload (clobber) the asset.
  for f in "${metas[@]}"; do
    tag=$(meta_get "$f" tag)
    asset=$(meta_get "$f" asset)
    if ! gh release view "$tag" >/dev/null 2>&1; then
      gh release create "$tag" --generate-notes --title "$tag"
    fi
    gh release upload "$tag" "$DIST/$asset" --clobber
  done
}

case "${1:-}" in
  detect) detect ;;
  build) shift; build "$@" ;;
  publish) publish ;;
  *) echo "usage: plugin-release.sh <detect|build <plugin>...|publish>" >&2; exit 2 ;;
esac

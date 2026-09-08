#!/bin/sh
# Regenerates the open-seed import fixture from the live repository
# (plans/os-cf13fb51.md D6): export.json at the newest seed-anchor tag
# and seed-state.bundle with the history and tags up to it.
#
#   next/fixtures/import/open-seed/regenerate.sh [--at-anchor]
#
# Without --at-anchor the state head must BE the newest anchor (run
# `scripts/seed state anchor` first), and the export is the v1
# command's. With it, the export document is derived from the anchored
# tree, for a head that has moved on since the anchor.
set -eu
root=$(git rev-parse --show-toplevel)
cd "$root"
dir=next/fixtures/import/open-seed
tag=$(git tag -l 'seed-anchor/*' | sort | tail -1)
[ -n "$tag" ] || { echo "regenerate: no seed-anchor/* tag; anchor first (scripts/seed state anchor)" >&2; exit 2; }
commit=$(git rev-parse "$tag^{commit}")
head=$(git rev-parse refs/remotes/origin/seed-state 2>/dev/null || git rev-parse refs/heads/seed-state 2>/dev/null || echo "")
if [ "${1:-}" = "--at-anchor" ]; then
  python3 - "$commit" > "$dir/export.json" <<'PY'
import json, subprocess, sys
commit = sys.argv[1]
files = {}
for p in subprocess.check_output(["git", "ls-tree", "-r", "--name-only", commit]).decode().split("\n"):
    p = p.strip()
    if p:
        files[p] = subprocess.check_output(["git", "show", commit + ":" + p]).decode()
json.dump({"document": {"schema_version": "1.0", "backend": "filecards", "head": commit, "files": files}}, sys.stdout)
PY
elif [ "$head" = "$commit" ]; then
  scripts/seed state export > "$dir/export.json"
else
  echo "regenerate: the state head $head is not the newest anchor $tag ($commit); anchor first (scripts/seed state anchor), or pass --at-anchor" >&2
  exit 2
fi
# Every anchor tag up to and including the newest, and the history they reach.
git bundle create "$dir/seed-state.bundle" $(git tag -l 'seed-anchor/*' | sort | awk -v t="$tag" '$0 <= t')
echo "regenerated at $tag ($commit): $(wc -c < "$dir/export.json") bytes of export, $(wc -c < "$dir/seed-state.bundle") bytes of bundle"

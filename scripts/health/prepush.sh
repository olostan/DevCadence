#!/bin/sh
set -eu

repo_root=$(git rev-parse --show-toplevel)
tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/devcadence-prepush.XXXXXX")
trap 'rm -rf "$tmp_root"' EXIT HUP INT TERM
candidate="$tmp_root/head"
mkdir -p "$candidate"

git -C "$repo_root" archive HEAD | tar -x -C "$candidate"

echo "health: validating committed HEAD $(git -C "$repo_root" rev-parse --short HEAD)"
(
  cd "$candidate"
  make ci
)

echo "health: committed HEAD PASS"

#!/bin/sh
set -eu

repo_root=$(git rev-parse --show-toplevel)
tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/devcadence-prepush.XXXXXX")
candidate="$tmp_root/head"

cleanup() {
  git -C "$repo_root" worktree remove --force "$candidate" >/dev/null 2>&1 || true
  rm -rf "$tmp_root"
}
trap cleanup EXIT HUP INT TERM

git -C "$repo_root" worktree add --detach "$candidate" HEAD >/dev/null

if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
  fmt_base=$(git -C "$repo_root" merge-base HEAD origin/main)
else
  fmt_base=$(git -C "$repo_root" rev-parse HEAD^)
fi

echo "health: validating committed HEAD $(git -C "$repo_root" rev-parse --short HEAD)"
(
  cd "$candidate"
  FMT_BASE="$fmt_base" make ci
)

echo "health: committed HEAD PASS"

#!/bin/sh
set -eu

repo_root=$(git rev-parse --show-toplevel)

if git -C "$repo_root" diff --cached --quiet; then
  echo "health: no staged changes"
  exit 0
fi

git -C "$repo_root" diff --cached --check

tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/devcadence-precommit.XXXXXX")
trap 'rm -rf "$tmp_root"' EXIT HUP INT TERM
candidate="$tmp_root/candidate"
mkdir -p "$candidate"

git -C "$repo_root" checkout-index --all --force --prefix="$candidate/"

echo "health: validating staged snapshot"
(
  cd "$candidate"
  make fmt-check
  make mod-check
  make vet
  make schemas
  make docs-check
)

sh "$repo_root/scripts/health/coverage-guard.sh" HEAD "$candidate"

echo "health: staged snapshot PASS"

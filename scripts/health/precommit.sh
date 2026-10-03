#!/bin/sh
set -eu

repo_root=$(git rev-parse --show-toplevel)
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

if git -C "$repo_root" diff --cached --quiet; then
  echo "health: no staged changes"
  exit 0
fi

git -C "$repo_root" diff --cached --check

tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/devcadence-precommit.XXXXXX")
trap 'rm -rf "$tmp_root"' EXIT HUP INT TERM
candidate="$tmp_root/candidate"
mkdir -p "$candidate"

# Materialize exactly the Git index. Unstaged working-tree files therefore
# cannot make the candidate look healthier than the commit being created.
git -C "$repo_root" checkout-index --all --force --prefix="$candidate/"

staged_go=$(git -C "$repo_root" diff --cached --name-only --diff-filter=ACMR -- '*.go')
if [ -n "$staged_go" ]; then
  for rel in $staged_go; do
    [ -f "$candidate/$rel" ] || continue
    out=$(gofmt -l "$candidate/$rel")
    if [ -n "$out" ]; then
      echo "gofmt required for staged file: $rel" >&2
      exit 1
    fi
  done
fi

echo "health: validating staged snapshot"
(
  cd "$candidate"
  make mod-check
  make vet
  make schemas
  make docs-check
)

# coverage-guard also executes the complete Go test suite against the staged
# snapshot and compares it with the exact current HEAD baseline. HEAD coverage
# is cached by SHA under the Git directory, so only the candidate is re-measured.
COVERAGE_CACHE_DIR=$(git -C "$repo_root" rev-parse --absolute-git-dir)/devcadence-coverage-cache \
  sh "$script_dir/coverage-guard.sh" HEAD "$candidate"

echo "health: staged snapshot PASS"

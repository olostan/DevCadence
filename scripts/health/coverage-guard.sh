#!/bin/sh
set -eu

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <base-ref> <candidate-directory>" >&2
  exit 2
fi

base_ref=$1
candidate_dir=$2
tolerance=${COVERAGE_TOLERANCE:-0.00}
repo_root=$(git rev-parse --show-toplevel)
tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/devcadence-coverage.XXXXXX")
trap 'rm -rf "$tmp_root"' EXIT HUP INT TERM

base_dir="$tmp_root/base"
mkdir -p "$base_dir"
git -C "$repo_root" archive "$base_ref" | tar -x -C "$base_dir"

run_coverage() {
  tree=$1
  profile=$2
  report=$3
  (
    cd "$tree"
    go test -count=1 -covermode=atomic -coverpkg=./... -coverprofile="$profile" ./... >/dev/null
    go tool cover -func="$profile" > "$report"
  )
  awk '/^total:/ { gsub(/%/, "", $3); print $3 }' "$report"
}

base_profile="$tmp_root/base.out"
base_report="$tmp_root/base.txt"
candidate_profile="$tmp_root/candidate.out"
candidate_report="$tmp_root/candidate.txt"

echo "health: measuring coverage for base $base_ref"
base_pct=$(run_coverage "$base_dir" "$base_profile" "$base_report")

echo "health: measuring coverage for candidate snapshot"
candidate_pct=$(run_coverage "$candidate_dir" "$candidate_profile" "$candidate_report")

echo "health: coverage base=$base_pct% candidate=$candidate_pct% tolerance=$tolerance pp"

if [ -n "${COVERAGE_EXPORT_DIR:-}" ]; then
  mkdir -p "$COVERAGE_EXPORT_DIR"
  cp "$candidate_profile" "$COVERAGE_EXPORT_DIR/coverage.out"
  cp "$candidate_report" "$COVERAGE_EXPORT_DIR/coverage.txt"
  delta=$(awk -v b="$base_pct" -v c="$candidate_pct" 'BEGIN { printf "%+.2f", c-b }')
  printf "base=%s%%\ncandidate=%s%%\ndelta=%s pp\n" "$base_pct" "$candidate_pct" "$delta" > "$COVERAGE_EXPORT_DIR/coverage-summary.txt"
fi

awk -v base="$base_pct" -v candidate="$candidate_pct" -v tolerance="$tolerance" '
BEGIN {
  if (candidate + tolerance + 0.000001 < base) {
    printf "coverage regression: %.2f%% -> %.2f%% (allowed regression %.2f pp)\n", base, candidate, tolerance > "/dev/stderr"
    exit 1
  }
}'

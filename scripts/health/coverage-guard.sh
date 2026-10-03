#!/bin/sh
# Compare whole-module statement coverage of a candidate tree with an exact base.
#
# Percentages are computed from the coverage profile (covered/total statements),
# not from the one-decimal figure printed by `go tool cover`, so the default
# zero-point tolerance really is zero. With -coverpkg=./... the same block can
# appear once per test binary; a block counts as covered if any entry hit it.
set -eu

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <base-ref> <candidate-directory>" >&2
  exit 2
fi

base_ref=$1
candidate_dir=$2
tolerance=${COVERAGE_TOLERANCE:-0.00}
repo_root=$(git rev-parse --show-toplevel)
base_sha=$(git -C "$repo_root" rev-parse --verify "$base_ref^{commit}")
# Hook-provided repository variables must not redirect git inside the tests.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES
tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/devcadence-coverage.XXXXXX")
trap 'rm -rf "$tmp_root"' EXIT HUP INT TERM

# stmt_counts prints "<covered> <total>" for a coverage profile.
stmt_counts() {
  awk 'NR > 1 { n[$1] = $2; if ($3 > 0) hit[$1] = 1 }
       END { for (k in n) { t += n[k]; if (k in hit) c += n[k] } printf "%d %d\n", c, t }' "$1"
}

# run_coverage <tree> <profile> <report>: prints "<covered> <total>".
run_coverage() {
  tree=$1
  profile=$2
  report=$3
  (
    cd "$tree"
    test_log="$profile.test.log"
    if ! go test -count=1 -covermode=atomic -coverpkg=./... -coverprofile="$profile" ./... >"$test_log" 2>&1; then
      cat "$test_log" >&2
      exit 1
    fi
    go tool cover -func="$profile" >"$report"
  )
  stmt_counts "$profile"
}

# Base measurements are cached by commit SHA and Go version when requested
# (the pre-commit hook does), so each commit measures only the candidate.
base_counts=
cache_file=
if [ -n "${COVERAGE_CACHE_DIR:-}" ]; then
  mkdir -p "$COVERAGE_CACHE_DIR"
  go_id=$(go version | tr -c 'A-Za-z0-9.\n' '_')
  cache_file="$COVERAGE_CACHE_DIR/$base_sha.$go_id"
  if [ -f "$cache_file" ]; then
    base_counts=$(cat "$cache_file")
  fi
fi

if [ -z "$base_counts" ]; then
  base_dir="$tmp_root/base"
  mkdir -p "$base_dir"
  git -C "$repo_root" archive "$base_sha" | tar -x -C "$base_dir"
  echo "health: measuring coverage for base $base_sha"
  base_counts=$(run_coverage "$base_dir" "$tmp_root/base.out" "$tmp_root/base.txt")
  if [ -n "$cache_file" ]; then
    printf '%s\n' "$base_counts" >"$cache_file"
  fi
else
  echo "health: using cached coverage for base $base_sha"
fi

candidate_profile="$tmp_root/candidate.out"
candidate_report="$tmp_root/candidate.txt"
echo "health: measuring coverage for candidate snapshot"
candidate_counts=$(run_coverage "$candidate_dir" "$candidate_profile" "$candidate_report")

result=$(awk -v b="$base_counts" -v c="$candidate_counts" -v tol="$tolerance" '
BEGIN {
  split(b, bs, " "); split(c, cs, " ")
  if (bs[2] == 0 || cs[2] == 0) { print "error"; exit }
  bp = 100 * bs[1] / bs[2]; cp = 100 * cs[1] / cs[2]
  printf "%s %.4f %.4f %s\n", (cp + tol + 1e-9 < bp) ? "regression" : "ok", bp, cp, tol
}')
set -- $result
status=$1
base_pct=${2:-0}
candidate_pct=${3:-0}
if [ "$status" = error ]; then
  echo "health: coverage profile has no statements" >&2
  exit 1
fi

echo "health: coverage base=$base_pct% candidate=$candidate_pct% tolerance=$tolerance pp (base $base_counts, candidate $candidate_counts covered/total statements)"

if [ -n "${COVERAGE_EXPORT_DIR:-}" ]; then
  mkdir -p "$COVERAGE_EXPORT_DIR"
  cp "$candidate_profile" "$COVERAGE_EXPORT_DIR/coverage.out"
  cp "$candidate_report" "$COVERAGE_EXPORT_DIR/coverage.txt"
  delta=$(awk -v b="$base_pct" -v c="$candidate_pct" 'BEGIN { printf "%+.4f", c-b }')
  printf "base=%s%%\ncandidate=%s%%\ndelta=%s pp\n" "$base_pct" "$candidate_pct" "$delta" >"$COVERAGE_EXPORT_DIR/coverage-summary.txt"
fi

if [ "$status" = regression ]; then
  echo "coverage regression: $base_pct% -> $candidate_pct% (allowed regression $tolerance pp)" >&2
  exit 1
fi

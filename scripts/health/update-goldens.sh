#!/bin/sh
# Regenerate the compiler golden digests after an intentional change to
# INVARIANTS.md or the invariant catalog.
#
# The equivalence tests freeze compiler behavior, so an unconditional update
# would hide an unintended behavior change. This refuses unless a normative
# source differs from the revision that last changed the goldens. Set FORCE=1
# to override deliberately, and review the printed diff as a behavior change.
set -eu

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

golden=internal/cognition/compiler/testdata/golden_digests.json
sources="INVARIANTS.md internal/cognition/compiler/catalog.go"

if [ -z "${FORCE:-}" ]; then
  last=$(git log -1 --format=%H -- "$golden" 2>/dev/null || true)
  if [ -n "$last" ]; then
    # Working tree against the goldens' last revision: covers committed,
    # staged and unstaged edits alike.
    # shellcheck disable=SC2086
    changed=$(git diff --name-only "$last" -- $sources)
  else
    changed=$sources
  fi
  if [ -z "$changed" ]; then
    echo "update-goldens: INVARIANTS.md and the invariant catalog are unchanged since the goldens were last updated." >&2
    echo "A digest change now would mean compiler behavior changed. Review that as a behavior change, or rerun with FORCE=1." >&2
    exit 1
  fi
fi

"${GO:-go}" test -count=1 ./internal/cognition/compiler -run '^TestEquivalence_' -args -update-goldens

echo
if git diff --quiet -- "$golden"; then
  echo "update-goldens: goldens already match the current compiler output."
else
  git --no-pager diff -- "$golden"
  echo
  echo "update-goldens: review the diff above and commit $golden with the invariant change."
fi

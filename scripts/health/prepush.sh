#!/bin/sh
# Pre-push gate. Git supplies "<local ref> <local sha> <remote ref> <remote sha>"
# lines on stdin; every pushed commit is validated in an isolated worktree.
# Without stdin refs (make prepush) the current HEAD is validated.
set -eu

repo_root=$(git rev-parse --show-toplevel)
zero=0000000000000000000000000000000000000000
tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/devcadence-prepush.XXXXXX")
worktree=

cleanup() {
  if [ -n "$worktree" ]; then
    git -C "$repo_root" worktree remove --force "$worktree" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp_root"
}
trap cleanup EXIT HUP INT TERM

shas=
if [ ! -t 0 ]; then
  while read -r local_ref local_sha remote_ref remote_sha; do
    [ -n "${local_sha:-}" ] || continue
    [ "$local_sha" = "$zero" ] && continue # branch deletion
    case " $shas " in *" $local_sha "*) ;; *) shas="$shas $local_sha" ;; esac
  done
fi
if [ -z "$shas" ]; then
  shas=$(git -C "$repo_root" rev-parse HEAD)
fi

for sha in $shas; do
  worktree="$tmp_root/$sha"
  git -C "$repo_root" worktree add --detach "$worktree" "$sha" >/dev/null

  if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
    base=$(git -C "$repo_root" merge-base "$sha" origin/main)
  else
    base=$(git -C "$repo_root" rev-parse --verify "$sha^" 2>/dev/null || git -C "$repo_root" hash-object -t tree /dev/null)
  fi

  echo "health: validating pushed commit $(git -C "$repo_root" rev-parse --short "$sha")"
  (
    cd "$worktree"
    # Hook-provided repository variables must not redirect Git inside the worktree.
    unset GIT_DIR GIT_INDEX_FILE GIT_WORK_TREE
    FMT_BASE="$base" DIFF_BASE="$base" make ci
  )
  git -C "$repo_root" worktree remove --force "$worktree" >/dev/null 2>&1 || true
  worktree=
done

echo "health: pushed commits PASS"

# Engineering Health Enforcement Policy

## Purpose

This document owns the concrete repository policy that turns DevCadence's engineering-health principles into deterministic local and CI gates.

`docs/REFACTORING_AND_HEALTH.md` explains why health is measured and periodically reconciled. This document defines what contributors, coding agents, hooks, and CI must actually run.

## 1. Principles

1. **Every committed checkpoint should be healthy.** A commit is a handoff/recovery boundary and should not knowingly contain failing tests, broken contracts, broken documentation references, or unexplained coverage regression.
2. **The staged candidate is the thing pre-commit validates.** Unstaged files must not be able to hide a broken commit.
3. **CI is authoritative.** Local hooks are fast feedback and accident prevention; GitHub Actions independently repeats policy from a clean checkout.
4. **Coverage is a regression signal, not a quality score.** No arbitrary global percentage substitutes for semantic review.
5. **Health policy is not self-waivable by feature work.** A PR may not lower thresholds, add exclusions, or weaken checks merely to make itself pass without explicit review of that policy change.
6. **Metrics complement review.** Deterministic checks do not replace architecture, security, maintainability, or independent semantic review.

## 2. Local gates

Install repository hooks once per clone:

```sh
make hooks-install
```

The installed hooks invoke versioned repository scripts, so later policy improvements do not require reinstalling the hooks. Existing historical formatting debt is not grandfathered into changed code: formatting is enforced on staged files locally and on files changed from the exact merge base in CI.

### Pre-commit

Pre-commit validates the exact Git index snapshot and blocks the commit if any of these fail:

- staged diff whitespace validation;
- Go formatting for staged/changed files;
- module metadata consistency;
- `go vet`;
- schema/fixture validation;
- documentation integrity checks;
- full Go tests under coverage instrumentation;
- statement coverage regression against current `HEAD`.

Coverage comparison is base-relative on the same machine/toolchain. The default allowed regression is zero percentage points. A non-zero tolerance is a health-policy decision, not a routine feature-PR escape hatch.

### Pre-push

Pre-push validates the committed `HEAD` in an isolated snapshot and runs the full CI-equivalent local gate, including the race detector.

This catches commits created with hooks disabled before they reach the remote.

## 3. Canonical Make targets

Repository health policy is exposed through Make targets so local checks and GitHub Actions share implementation:

- `make fmt-check`
- `make mod-check`
- `make vet`
- `make test`
- `make race`
- `make schemas`
- `make docs-check`
- `make coverage`
- `make verify`
- `make ci`
- `make precommit`
- `make prepush`
- `make hooks-install`

Workflow YAML should orchestrate these targets rather than reimplementing repository policy.

## 4. Coverage policy

Coverage uses:

```sh
go test -count=1 -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
```

For pull requests, head coverage is compared with the exact PR base SHA using the same Go version and command.

For local pre-commit, the staged snapshot is compared with current `HEAD`.

A coverage decrease fails the gate unless the health policy itself is explicitly amended and independently reviewed. This prevents a feature PR from "fixing" its own regression by lowering a checked-in baseline.

Coverage artifacts and a human-readable summary are produced in CI.

Coverage does not prove that behavior is well tested. Review must still consider missing negative cases, boundary behavior, concurrency, security, and whether tests assert public behavior rather than implementation shape.

## 5. Documentation integrity

`make docs-check` validates semantic repository documentation integrity, including:

- repository-relative Markdown links;
- Markdown heading anchors;
- referenced DCI identifiers;
- referenced ADR identifiers;
- existing code/document drift tests.

The checker intentionally prioritizes broken references and contract drift over stylistic Markdown trivia.

Protocol/schema synchronization continues to be enforced by schema fixtures and Go/schema parity tests.

## 6. GitHub Actions

CI runs on pull requests and pushes to `main`.

Required logical checks are:

- static/contract verification;
- unit tests;
- race detector;
- documentation/schema contracts;
- coverage and base-relative coverage regression.

CI runs with read-only repository permissions unless a future job has an explicitly reviewed need for more authority. Stale runs for the same PR are cancelled.

## 7. Exceptions

`--no-verify` is for exceptional recovery/debugging, not normal agent operation.

If a health check is believed to be wrong:

1. preserve the failing evidence;
2. explain why the check is invalid for the candidate;
3. fix or amend the health policy in an explicitly reviewed change;
4. do not silently disable the check, delete the test, reduce coverage expectations, or add a candidate-specific exclusion.

## 8. Merge enforcement

Once workflow names are stable, `main` should require the CI checks through GitHub branch protection or a repository ruleset.

GitHub account identity is not equivalent to DevCadence reviewer independence. Independent review remains governed by the DevCadence review/convergence protocol even when multiple agents operate through one GitHub account.

## 9. Evolution

This is the initial enforcement baseline. Later health work may add:

- changed-line coverage;
- package-specific risk expectations;
- staticcheck after signal/noise evaluation;
- vulnerability scanning;
- flaky-test detection;
- cross-platform scheduled matrices;
- structural health trend snapshots.

New checks should be deterministic, actionable, and sufficiently low-noise that contributors do not normalize bypassing them.

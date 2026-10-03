# Engineering Work Package: WP-M3C-H1 — Repository Health Enforcement Baseline

- **Kind:** deterministic engineering-health infrastructure
- **Base commit:** `23f2dc1bb23868b9e3fc18c80aae3d5d89bd4539`
- **Branch:** `chore/health-enforcement`
- **Version:** 1.0
- **Status:** Implemented — accepted on merge (CI green on head; review findings addressed)

## Objective

Turn the existing DevCadence engineering-health principles into mechanically enforced local and GitHub gates without replacing semantic review with metrics.

## Required behavior

1. Pre-commit validates the exact staged snapshot, not unstaged working-tree state.
2. A commit is blocked by formatting errors in staged Go files, module drift, vet failures, test failures, schema/document contract failures, or statement-coverage regression against `HEAD`.
3. Pre-push validates committed `HEAD` in an isolated snapshot and includes race testing.
4. GitHub Actions independently repeats the same Make-target policy from a clean checkout.
5. Pull-request coverage is compared with the exact PR base SHA rather than a mutable checked-in percentage.
6. Documentation checks validate repository-relative links/anchors and DCI/ADR references in addition to existing code/document drift tests.
7. Health checks are deterministic and actionable; feature work may not silently weaken them to pass.
8. Coverage remains a regression signal, not a substitute for semantic or architectural review.

## Scope

Allowed paths:

- `.github/workflows/ci.yml`
- `.githooks/*`
- `scripts/health/*`
- `tests/docs_integrity_test.go`
- `tests/docs_integrity_helpers_test.go`
- `Makefile`
- `.gitignore`
- `CONTRIBUTING.md`
- `AGENTS.md`
- `docs/ENGINEERING_HEALTH_POLICY.md`
- `docs/README.md` (repair of health-check-detected stale internal references only)
- `docs/REFACTORING_AND_HEALTH.md`
- `docs/WORK_PACKAGES.md`
- this EWP

## Explicit non-goals

- no production/runtime behavior changes;
- no arbitrary global coverage target;
- no third-party coverage SaaS;
- no broad lint suite before signal/noise is evaluated;
- no GitHub approval rule pretending account identity provides reviewer independence;
- no branch-protection mutation in this PR; required checks should be enabled after workflow names stabilize.

## Verification

Required before acceptance:

- `make fmt-check`
- `make mod-check`
- `make vet`
- `make test`
- `make race`
- `make schemas`
- `make docs-check`
- `make coverage`
- clean GitHub Actions runs for the PR
- manual inspection that pre-commit materializes the Git index rather than the dirty working tree
- manual inspection that coverage comparison uses the exact PR base SHA in CI

## Acceptance criteria

- hooks install with one command;
- installed hooks delegate to versioned repository policy;
- staged-only validation cannot be masked by unstaged files;
- coverage regression is blocking with zero default percentage-point tolerance;
- CI exposes separate static, test/contracts, race, and coverage status;
- coverage artifacts are retained;
- documentation link/anchor/reference breakage is mechanically detected;
- local and CI checks share Make targets;
- no production package behavior changes.

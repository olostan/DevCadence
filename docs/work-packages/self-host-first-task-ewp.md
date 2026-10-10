# EWP SH1-4B-T1: first self-modification task (internal/ids empty-prefix tests)

Revision 1, base: `origin/main` at the commit that merges this file. Part of [WP-M5-SH1](wp-m5-sh1-self-host-alpha-ewp.md); runbook in [self-host-alpha-progress.md](self-host-alpha-progress.md#sh1-4b-owner-runbook-first-self-modification-attempt).

## Objective

DevCadence adds missing unit tests to `internal/ids` through its own executor, as a harmless first self-modification. Evidence gap (verified): `internal/ids` coverage is 90.9% and `join` is 66.7% because the empty-prefix branch (`Source.New("")` returning the bare 26-character body) is never exercised for `ULIDSource` or `Sequential`. `internal/ids` is a leaf (stdlib only), has no schema, persistence, authority or concurrency change in scope.

## Write scope (exact)

- `internal/ids/ids_test.go` (existing; the only file that may change).

Read scope: `internal/ids/ids.go`, `internal/ids/ids_test.go`.

## Requirements

MUST: add tests in package `ids_test` that (1) assert `NewSequential().New("")` and `ids.NewULIDSource().New("")` and `.NewAt("", fixedTime)` return exactly `ids.Length` characters with no underscore and satisfy `ids.Valid`; (2) assert `Sequential.New("")` is deterministic and increments (`...01`, `...02`) and is independent of a prefixed counter; (3) assert `ULIDSource.NewAt` with a fixed instant gives the same 10-character timestamp prefix on repeated calls and a different random suffix. Use only the exported API, `t.Run`/table style like the existing tests, no sleeps, no network.

MUST NOT: edit `ids.go`, any other file, `go.mod`, docs, schemas, goldens or `.devcadence/`; add dependencies; change any exported identifier; weaken or delete an existing test; run git commands (the executor commits).

## Acceptance (exact commands, repo root)

1. `go run ./scripts/health/fmtcheck internal/ids` exits 0.
2. `go vet ./internal/ids/...` exits 0.
3. `go test -count=1 -cover ./internal/ids/...` passes and reports coverage above 90.9%; `join` is 100% in `go test -coverprofile=c.out ./internal/ids && go tool cover -func=c.out`.
4. Manifest contains exactly `internal/ids/ids_test.go`.

Checks 1-3 are the registered `.devcadence/validation.yaml` profile `default` (scoped to `internal/ids` only). Before integration the owner MUST also run full `make ci`; the profile does not replace it.

## Escalation

Stop and report (no workaround) if: a test needs a change to `ids.go`; an existing test fails on the base; scope other than `internal/ids/ids_test.go` is needed; the post-check refuses (`postcheck_requires_yolo`, `prohibited-profile-content`); or the model asserts behavior contradicted by `ids.go`.

## Delegation text (Principal to DevCadence)

> Task: in `internal/ids/ids_test.go` only, add tests covering the empty-prefix behavior of `ULIDSource.New/NewAt` and `Sequential.New` per EWP SH1-4B-T1 (docs/work-packages/self-host-first-task-ewp.md). Read `internal/ids/ids.go` first. Do not modify any other file. Use `apply_patch`; verify with `go vet ./internal/ids/...` and `go test ./internal/ids/...` via `run_command`. Finish only when the registered `default` profile passes. If an implementation change seems needed, stop and report it.

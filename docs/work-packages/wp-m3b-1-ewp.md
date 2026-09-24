# Engineering Work Package: WP-M3B-1 — Setup domain types and plan digest

- **Milestone:** M3B — Guided bootstrap and onboarding
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3b-1--setup-domain-types-and-plan-digest](../WORK_PACKAGES.md#wp-m3b-1--setup-domain-types-and-plan-digest)
- **Base commit:** `a38b293` (origin/main, merge of PR #9 — `AGENT_HANDOFF_PROTOCOL.md` and M3B work-package breakdown)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Status:** Implementation already present on `main` prior to this EWP's authorship — see "Provenance" below. This EWP documents, verifies, and formally accepts that existing implementation as WP-M3B-1's deliverable under `AGENT_HANDOFF_PROTOCOL.md`'s Principal/Implementer sequence, rather than re-deriving the same design from a blank slate.

## Provenance (read before anything else)

This is not a forward-looking design document for code that doesn't exist yet. `docs/WORK_PACKAGES.md` describes the M3B branch as something to "create when WP-M3B-1 starts," and `docs/IMPLEMENTATION_PLAN.md` describes M3B as "not implemented." Both were accurate when written, but the repository's actual `main` branch already contained a full implementation of this WP's scope, committed directly by the project owner (`olostan@gmail.com`) on 2026-09-22 — one to two days *before* `AGENT_HANDOFF_PROTOCOL.md` and the WP-M3B-1 scope card were merged (PR #9, `a38b293`, 2026-09-24):

- `deae362` — "feat(setup): add M3B protocol types, schemas, and ADR-0014 (Phase 1)"
- `2e945e4` — "feat(setup): add Doctor, profile recommender, cache, and planner (Phase 2)"
- `f9ffcc3`, `c6bec0a`, `216bad2`, `42617ab` — four follow-up fix commits addressing review findings against that code (digests, readiness, profiles, cache, Ollama identity trust, MLX gating).

That code was never routed through this protocol's EWP process and never lived on a `feat/m3b-*` branch — it landed straight on `main` as the human owner's own direct work, before the handoff protocol existed. `docs/WORK_PACKAGES.md`'s WP-M3B-1 scope card was written independently but turns out to describe — almost field-for-field — exactly what `internal/protocol/setup.go` already implements: the same five `TypedOperation` kinds, the same six `Condition` kinds, the same `ManagedDirectoryLocation`/`CacheTarget`/`ManagedConfigKey` allowlists, the same `PlanDigest` rule, the same mutual-exclusion and `IntrinsicPolicy` semantics. ADR-0014 (`Accepted`, 2026-09-22) is the settled design document both the scope card and the existing code trace back to.

Per the resolution the project owner gave when this discrepancy was surfaced: **the existing code on `main` is treated as the real implementation of WP-M3B-1.** This EWP's job is to state that design formally per `AGENTS.md` §6 (so a future session has a real frozen artifact to work against, not just a scope card and an untraceable pile of commits), verify it against WP-M3B-1's acceptance criteria with fresh evidence, and identify what — if anything — is missing before the checkpoint can be marked accepted.

`internal/setup/{doctor,planner,profiles,cache}.go` (from the same Phase 1/2 commits) go considerably further than WP-M3B-1's scope — into WP-M3B-3/5/6 territory (executor semantics, doctor readiness, profile recommendation, bounded recipes). This EWP does **not** cover, verify, or accept that code. It is out of scope for WP-M3B-1 and is left for whichever session expands WP-M3B-3/5/6's EWPs to assess against — noted here, and in `HANDOFF.md`, so it isn't lost.

## 1. Objective and rationale

Establish the closed, typed data model that every later M3B WP (executor, ledger, doctor, recipes, CLI, TUI) builds on: a discriminated-union `TypedOperation`, a discriminated-union `Condition`, an immutable `SetupPlan` envelope with a canonical, tamper-evident `PlanDigest`, and executor-defined `IntrinsicPolicy` enforcement. No process execution, no CLI, no ledger — pure types, validation functions, and JSON Schemas, matching ADR-0014 §1–2.

The rationale (per ADR-0014's Context) is that free-form shell scripts or `map[string]string` parameter bags turn `devcadence setup` into an unverified script runner (violates ADR-0008, DCI-033), and an un-hashed or silently-adaptable plan lets approval drift from what actually executes (violates DCI-108). Closing the union and hashing the exact approved envelope is what makes the two-step approval workflow (WP-M3B-3) meaningful.

## 2. Architectural intent

- Lives in `internal/protocol` (package `protocol`), alongside every other typed protocol record (`ProjectState`, `EngineeringWorkPackage`, etc.) — not a separate package. This keeps `SetupPlan` a first-class protocol record with the same `Record` interface (`RecordKind`/`RecordID`/`SchemaVer`/`Validate`), the same canonical-JSON marshal/unmarshal discipline (`protocol.Marshal`/`protocol.Unmarshal`), and the same schema-pairing test (`tests/schema_fixtures_test.go`'s `TestEveryRecordKindHasASchema`) as every other domain type — not a bespoke setup-specific serialization path.
- `TypedOperation` and `Condition` are Go structs with a `Kind` discriminator field plus one `*Params`/`*Operand` pointer per kind, exactly one of which may be non-nil (`count != 1` check in `Validate()`). This is the project's established discriminated-union pattern in Go (no `interface{}`/`any`, no open maps) — see `SetupAction.Operation *TypedOperation` / `SetupAction.ManualInstructions *ManualGuide` for the same pattern one level up.
- `IntrinsicPolicy(op TypedOperation) ([]EffectCategory, Authority)` is a pure function keyed on `OperationKind`, owned by the executor's contract (this package), not by whatever recipe later constructs the operation (WP-M3B-6). A recipe cannot self-declare a weaker authority than the operation kind intrinsically requires; `SetupAction.Validate()` enforces this at the type level so no downstream caller can bypass it by skipping a policy-check step.
- `PlanDigest` is computed over a shadow struct (`setupPlanDigestView`) that mirrors every `SetupPlan` field except `PlanDigest` itself, marshaled through the same canonical encoder as every other record. This avoids the two-pass "hash with digest field zeroed, but the zero value and an explicit omission must canonicalize identically" trap: the view type simply never has the field.
- Mutual exclusion (`AuthorityHighImpactManual` ⟺ `ManualInstructions` set, `Operation == nil` — anything else ⟺ `Operation` set, `ManualInstructions == nil`) is enforced once, centrally, in `SetupAction.Validate()`, not duplicated per-caller.

## 3. Verified assumptions and evidence

- **Assumption:** ADR-0014 is `Accepted` and normative for this milestone, not open for relitigation. Verified: `docs/adr/0014-guided-bootstrap-and-remediation.md` line 3, `Status: Accepted`, dated 2026-09-22.
- **Assumption:** the existing `internal/protocol/setup.go` implementation matches ADR-0014 §1–2 and the WP-M3B-1 scope card's deliverable list field-for-field. Verified by direct reading: operation kinds (`OpKindOllamaPullModel`, `OpKindCreateDirectory`, `OpKindWriteManagedConfig`, `OpKindRemoveStaleCache`, `OpKindRunDiagnosticCheck`) match the scope card exactly; condition kinds (`CondKindCommandAvailable`, `CondKindExecutableVerified`, `CondKindManagedDirExists`, `CondKindPortListening`, `CondKindEndpointHealthy`, `CondKindModelDigestPresent`) match exactly; `ManagedDirectoryLocation`, `CacheTarget`, `ManagedConfigKey` allowlists present (`internal/protocol/setup.go:101-139`).
- **Assumption:** the JSON Schemas and fixtures required by the scope card already exist and pass round-trip validation. Verified: `schemas/setup-plan.schema.json`, `schemas/setup-execution-report.schema.json`, `schemas/setup-ledger-event.schema.json` exist and are wired into `schema.RecordKindToSchema`; `fixtures/protocol/setup-plan.valid.json` and `setup-plan.invalid-target.json` exist and are exercised by `tests/schema_fixtures_test.go`'s `TestValidFixturesValidate`, `TestInvalidFixturesAreRejected`, and `TestFixturesRoundTripWithoutSemanticLoss` (the last one decodes, re-encodes, and re-validates `SetupPlan`, `SetupExecutionReport`, and `SetupLedgerEvent` against their schemas and asserts byte-stable double round-tripping).
- **Assumption:** the acceptance criteria in the scope card are met by existing tests, not just existing code. Verified by running the suite fresh (see §8 below) rather than trusting prior commit messages — this session did not assume "tests pass" from history; it re-ran them.

No assumption in this list was found false. No escalation is needed.

## 4. Constraints

**MUST** (already satisfied by the existing implementation; binding on any future change to this code):
- `TypedOperation` and `Condition` remain closed discriminated unions. No open `map[string]string` parameter bag is ever added to either (ADR-0014 §1, DCI-033).
- No operation defined here executes anything. This package is data/validation only; process execution is WP-M3B-3's `internal/setup.CommandRunner` boundary.
- `IntrinsicPolicy` stays executor-owned: a `SetupAction`'s declared `Authority`/`Effects` can never validate successfully below what `IntrinsicPolicy(op)` requires for that operation kind.
- Manual and executable actions remain mutually exclusive at the type level (`SetupAction.Validate()`), enforced for every action, not opt-in per caller.
- `PlanDigest` is SHA-256 over the canonical JSON encoding of the complete `SetupPlan` with only `plan_digest` omitted — never a partial-field hash, never recomputed with a different field set without a new ADR.
- `port_listening` conditions probe loopback addresses only (`localhost`, `127.0.0.1`, `::1`) — already enforced in `Condition.Validate()`.

**SHOULD:**
- New operation/condition kinds added in later WPs (e.g. WP-M3B-6's recipes) extend the existing `OperationKind`/`ConditionKind` enums and `IntrinsicPolicy` switch rather than introducing a parallel mechanism.
- Any future field added to `SetupPlan` is added to both `SetupPlan` and `setupPlanDigestView` together — a mismatch between the two would silently exclude a field from the digest.

**SUGGESTED:**
- If a sixth+ operation or condition kind is added later, consider whether `IntrinsicPolicy`'s `switch` should grow a table-driven form instead of a growing switch — not needed at five/six cases, worth revisiting past ten.

**LOCAL_DISCRETION:**
- Internal helper naming, test table structure, and fixture file layout within `fixtures/protocol/`.

## 5. Interface sketch (as implemented)

```go
// internal/protocol/setup.go

type OperationKind string // ollama_pull_model | create_directory | write_managed_config | remove_stale_cache | run_diagnostic_check

type TypedOperation struct {
    Kind               OperationKind
    OllamaPullModel    *OllamaPullModelParams    `json:"ollama_pull_model,omitempty"`
    CreateDirectory    *CreateDirectoryParams    `json:"create_directory,omitempty"`
    WriteManagedConfig *WriteManagedConfigParams `json:"write_managed_config,omitempty"`
    RemoveStaleCache   *RemoveStaleCacheParams   `json:"remove_stale_cache,omitempty"`
    RunDiagnosticCheck *RunDiagnosticCheckParams `json:"run_diagnostic_check,omitempty"`
}
func (o TypedOperation) Validate() error
func IntrinsicPolicy(op TypedOperation) ([]EffectCategory, Authority)

type ConditionKind string // command_available | executable_verified | managed_dir_exists | port_listening | endpoint_healthy | model_digest_present

type Condition struct {
    Kind ConditionKind
    CommandAvailable   *CommandAvailableOperand
    ExecutableVerified *ExecutableVerifiedOperand
    ManagedDirExists   *ManagedDirOperand
    PortListening      *PortOperand
    EndpointHealthy    *EndpointOperand
    ModelDigestPresent *ModelDigestOperand
}
func (c Condition) Validate() error

type SetupAction struct {
    ActionID, RecipeID, RecipeVersion, Title, Description string
    Authority          Authority
    Effects            []EffectCategory
    Preconditions      []Condition
    Postconditions     []Condition
    ExpectedMutations  []ExpectedMutation
    IdempotencyKey     string
    DependsOn          []string
    Operation          *TypedOperation // mutually exclusive with ManualInstructions
    ManualInstructions *ManualGuide
}
func (a SetupAction) Validate() error // enforces mutual exclusion + IntrinsicPolicy

type SetupPlan struct {
    SchemaVersion, PlanID, PlanDigest, RecipeSetVersion, MachineFingerprint string
    CreatedAt         Timestamp
    Target            SetupTarget
    Actions           []SetupAction
    RequiredAuthority Authority
    TotalEffects      []EffectCategory
}
func ComputePlanDigest(p *SetupPlan) (string, error)
func (p *SetupPlan) Validate() error // includes digest match, dup action/idempotency-key, dependency-order checks
```

No changes to this interface are proposed by this EWP.

## 6. Edge cases and failure modes (already covered)

- Weaker declared authority than `IntrinsicPolicy` demands → rejected (`TestSetupActionIntrinsicPolicyEnforcement`).
- Missing a required intrinsic effect category → rejected (same test).
- Manual action carrying a non-nil `Operation`, or executable action carrying non-nil `ManualInstructions` → rejected both directions (`TestSetupActionMutualExclusion`).
- Tampered plan (any field changed after digest computed) → digest mismatch, rejected (`TestSetupPlanValidationAndDigest`).
- Empty `PlanDigest` → rejected.
- Duplicate `action_id` within a plan → rejected.
- Duplicate `idempotency_key` within a plan → rejected.
- Forward/cyclic `depends_on` reference → rejected.
- Missing `depends_on` target → rejected.
- Non-loopback `port_listening` host → rejected (`TestLoopbackOnlyPortCondition`).

## 7. Acceptance criteria — verification against this session's fresh run

Scope card's stated criteria, each checked against a fresh test run (not trusted from prior commit messages):

| Criterion | Evidence |
|---|---|
| Schema round-trip tests | `tests/schema_fixtures_test.go::TestValidFixturesValidate`, `TestInvalidFixturesAreRejected`, `TestFixturesRoundTripWithoutSemanticLoss` — PASS (see §8) |
| Weaker-than-`IntrinsicPolicy` authority rejected | `internal/protocol/setup_test.go::TestSetupActionIntrinsicPolicyEnforcement` — PASS |
| `PlanDigest` stable/reproducible for identical input, changes for any other field change | `TestSetupPlanValidationAndDigest`, `TestComputeDigestForFixtures` — PASS |
| Fixtures cover manual and executable action shapes | `validManualAction()`/`validExecutableAction()` helpers in `setup_test.go`, exercised by `TestSetupActionMutualExclusion`, `TestSetupPlanValidationAndDigest` — PASS. (JSON fixture corpus under `fixtures/protocol/` currently only carries an executable-shaped `setup-plan.valid.json`; the manual-action shape and the cross-shape mutual-exclusion rejection are covered by the Go-level table above rather than a second JSON fixture file. This is judged sufficient — the scope card does not mandate JSON-fixture-only coverage, and the Go tests exercise the identical validation path a JSON-decoded plan would hit — but is flagged here explicitly rather than silently treated as fully equivalent to a fixture-file check.) |
| Mixing manual + executable on one action rejected | `TestSetupActionMutualExclusion` (both directions) — PASS |

## 8. Deterministic evidence (this session, base commit `a38b293`, Go toolchain `go1.25.0`, linux/amd64)

```
$ go build ./...
(clean, exit 0)

$ go vet ./...
(clean, exit 0)

$ go test -count=1 ./...
ok  	github.com/olostan/DevCadence/internal/protocol	0.005s
ok  	github.com/olostan/DevCadence/internal/schema	0.038s
ok  	github.com/olostan/DevCadence/internal/setup	0.026s
ok  	github.com/olostan/DevCadence/tests	0.821s
... (all 21 other packages ok, 0 failures)
```

Full per-relevant-test breakdown (`go test ./internal/protocol/... ./internal/schema/... -run "Setup|Plan|Digest|Operation|Condition" -v`):
`TestDigestIsAlgorithmPrefixedAndContentAddressed`, `TestSetupActionMutualExclusion`, `TestSetupActionIntrinsicPolicyEnforcement`, `TestSetupPlanValidationAndDigest`, `TestLoopbackOnlyPortCondition`, `TestSetupLedgerEventValidationAndChain`, `TestComputeDigestForFixtures` — all PASS.

## 9. Non-goals / forbidden changes for this WP

- No CLI (`devcadence setup plan/apply`) — WP-M3B-7.
- No process execution, no `CommandRunner` — WP-M3B-3.
- No ledger — WP-M3B-2.
- No recipe content beyond the operation *kinds* already defined — concrete recipe instances are WP-M3B-6.
- No assessment, acceptance, or re-verification of `internal/setup/{doctor,planner,profiles,cache}.go` — out of scope for this WP; left for WP-M3B-3/5/6's own EWPs.
- No change to `docs/IMPLEMENTATION_PLAN.md`'s M3B status line — that flip is explicitly WP-M3B-9's deliverable per its scope card, not this checkpoint's.

## 10. Escalation conditions

None triggered. If a future session finds a WP-M3B-1-level type actually contradicts ADR-0014 (not just something this EWP under-specifies), it amends this EWP as a new committed revision per `AGENT_HANDOFF_PROTOCOL.md`'s Principal/Implementer sequence — it does not silently redesign `internal/protocol/setup.go`.

## 11. Disposition

**WP-M3B-1 is accepted at this checkpoint.** No implementation code changes were required; the existing `internal/protocol/setup.go` (plus `schemas/setup-plan.schema.json` and `fixtures/protocol/setup-plan.*.json`) already satisfies every deliverable and acceptance criterion in the scope card, verified fresh in §8 above. This EWP itself, plus the verification run, is the checkpoint artifact.

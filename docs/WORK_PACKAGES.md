# Work Package Breakdown

## Purpose

`docs/IMPLEMENTATION_PLAN.md` describes milestones at the grain a roadmap
needs: goal, deliverables, verification, exit criterion. That grain is too
coarse to hand to a single agent session, especially now that development
follows `AGENT_HANDOFF_PROTOCOL.md` — sessions are expected to run out of
quota mid-milestone, sometimes mid-Work-Package, and the point of splitting
work this finely is to make that cheap rather than disruptive.

This document splits milestones into Work Packages: independently
buildable/testable checkpoints, each small enough that losing "the rest of
one WP" to a quota cutoff is an acceptable loss, each large enough to be a
coherent unit of review.

**Each entry here is a scope card, not the full Engineering Work Package
AGENTS.md §6 describes.** The first action of whichever agent starts a given
WP is to read the milestone's ADRs (cited per WP below), expand the scope
card into a full Work Package (objective, architectural intent, MUST/
SHOULD/SUGGESTED/LOCAL_DISCRETION constraints, interface sketches,
pseudocode where logic is non-trivial, edge cases, acceptance criteria,
base commit) per AGENTS.md §6, and record that expansion — either inline in
this file under the WP's entry, or in a linked note — so the next agent (if
a handoff happens mid-WP) inherits the detailed plan, not just this card.

All Work Packages for one milestone land on **one shared branch**, one PR,
per `AGENT_HANDOFF_PROTOCOL.md` — not a branch/PR per WP.

---

## M3B — Guided bootstrap and onboarding

Branch: `feat/m3b-guided-bootstrap` (create when WP-M3B-1 starts).

Normative grounding for this milestone: ADR-0014 (guided bootstrap, setup
plans, operational event ledger, readiness contracts — already `Accepted`,
so the architecture decisions below are mostly settled, not open for
relitigation), ADR-0011, ADR-0013, `docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md`,
`docs/MODEL_RUNTIME.md`, `docs/SETUP.md`, `docs/SECURITY.md`. Read these —
in that order — before expanding WP-M3B-1.

Dependency chain: WP1 → WP2 → WP3 → {WP4, WP5, WP6 in any order} → WP7 →
WP8 → WP9. WP4–WP6 can be parallelized across sessions if more than one is
active, since they don't depend on each other, only on WP1–WP3.

### WP-M3B-1 — Setup domain types and plan digest

**Objective:** the closed, typed data model everything else builds on:
`TypedOperation` discriminated union, `Condition` discriminated union,
`SetupPlan` envelope, canonical `PlanDigest` computation, `IntrinsicPolicy`
validation. No process execution, no CLI, no ledger yet — pure types,
validation functions, and JSON Schemas.

**Deliverables:**
- Go types for `TypedOperation` (`ollama_pull_model`, `create_directory`,
  `write_managed_config`, `remove_stale_cache`, `run_diagnostic_check`) and
  `Condition` (`command_available`, `executable_verified`,
  `managed_dir_exists`, `port_listening`, `endpoint_healthy`,
  `model_digest_present`) per ADR-0014 §1.
- `ManagedDirectoryLocation`, `CacheTarget`, `ManagedConfigKey` allowlists.
- `SetupPlan`/`SetupAction` types and canonical `PlanDigest` (SHA-256 over
  canonical JSON with only `plan_digest` omitted) per ADR-0014 §2.
- `IntrinsicPolicy(op)` — executor-defined minimum authority/effect
  category per operation kind; plan validation rejects a weaker declared
  authority.
- Mutual-exclusion validation: manual actions vs. executable actions
  (ADR-0014 §1).
- JSON Schemas for all of the above, plus fixtures.

**MUST:** closed discriminated unions only — no open `map[string]string`
parameter bags (ADR-0014 §1, DCI-033). No operation executes anything at
this stage; this WP is data/validation only.

**Acceptance criteria:** schema round-trip tests; a plan with an action
whose declared authority is weaker than its `IntrinsicPolicy` is rejected;
`PlanDigest` is stable/reproducible for identical input and changes for
any field change other than `plan_digest` itself; fixtures cover both
manual and executable action shapes and reject a plan mixing both on one
action.

**Non-goals:** the CLI (`devcadence setup plan/apply`) — that's WP-M3B-7.

### WP-M3B-2 — Setup event ledger and operational state layout

**Objective:** the crash-safe append-only ledger and the
`$DEVCADENCE_HOME` operational-state layout, independent of the executor
that will write to it.

**Deliverables:**
- JSONL ledger at `$DEVCADENCE_HOME/state/setup-ledger.jsonl` with the
  closed event payload set from ADR-0014 §3
  (`ExecutionCreatedPayload`/`PlanApprovedPayload`/`ActionStartingPayload`/
  `ActionProcessCompletedPayload`/`PostconditionVerifiedPayload`/
  `ActionTerminatedPayload`/`ExecutionFinishedPayload`).
- Hash-chain integrity (`sequence`, `previous_event_digest`,
  `event_digest`), corruption detection, torn-write recovery for an
  incomplete final line only.
- Interruption recovery: an action left in `ActionStarting` with no
  terminal event is marked `ActionStatusInterrupted` on restart, then
  reconciled via postcondition check (succeeded/blocked) — never blindly
  rerun.
- `$DEVCADENCE_HOME` layout: `state/machine-profile.json` cache (with
  `MachineFingerprint` freshness check), `state/setup.lock` (`flock`),
  `artifacts/setup/` (bounded, mode `0700`/`0600`), `tmp/` (mode `0700`).
- `SetupExecutionReport` as a derived projection over the ledger, not a
  second source of truth.

**MUST:** none of this operational state enters Git repositories, commits,
or project event journals (ADR-0014 §4) — this is machine-global state,
structurally separate from `internal/state`'s project-scoped reducer.

**Acceptance criteria:** a simulated crash mid-action (kill between
`ActionStarting` and any terminal event) recovers to `interrupted` then
correctly resolves to `succeeded`/`blocked` on restart depending on
postcondition state; a corrupted non-final ledger line fails closed; a
corrupted/truncated final line recovers as a torn write; concurrent setup
runs are serialized by `setup.lock`.

### WP-M3B-3 — Executor and CLI approval workflow

**Objective:** wire WP-M3B-1's plan types and WP-M3B-2's ledger into an
actual executor, plus the two-step CLI approval workflow.

**Deliverables:**
- `devcadence setup plan [target] --output <file>` and
  `devcadence setup apply --plan <file> --approve-plan <sha256:digest>
  [--yes]` per ADR-0014 §2.
- Precondition rechecking immediately before each action executes; any
  drift since plan generation invalidates approval and halts, demanding a
  fresh plan.
- `--yes` authorizes only `user_confirmation`-level actions; privileged/
  high-impact actions always require the explicit digest-approval path.
- Subprocess execution exclusively through `internal/process.Runner`
  behind the narrow `internal/setup.CommandRunner` interface (ADR-0014 §7).
- Output capture bounded to 4 MiB, ANSI-stripped, content-addressed;
  authentication operations capture zero raw output artifacts.

**MUST:** no operation reachable through any path other than this executor
— no ad hoc shell-out anywhere else in `internal/setup`.

**Acceptance criteria:** a plan approved with the wrong digest is rejected;
a plan whose preconditions drifted between generation and apply halts
without executing later actions; `--yes` on a plan containing a
privileged action is rejected outright; output artifacts respect the byte
cap and never appear for authentication operations.

### WP-M3B-4 — Credential-reference abstraction

**Objective:** opaque credential references and secret isolation — this is
also what ADR-0017 (external research) explicitly deferred to and depends
on, so treat this WP's interface as a real, reusable primitive, not
setup-specific.

**Deliverables:**
- `CredentialRef` with kinds `env_var`, `cli_session`, `keychain_ref`
  (ADR-0014 §6) — reuse the `looksLikeSecret`/opaque-reference validation
  pattern already established in `internal/cognition/service.go` rather
  than reimplementing it.
- `os.LookupEnv`-based presence-only env lookup; secret values/lengths
  never logged.
- Explicit rejection of secrets in `process.Spec.Args`/`process.Spec.Env`.
- CLI-session credential discovery: version-output alone never establishes
  authentication (ADR-0014 §6) — needs an actual authenticated-call probe
  or equivalent evidence.

**MUST (security-sensitive — needs the `CONTRIBUTING.md` threat-model
review against `docs/SECURITY.md` before merge, independent of the rest of
this milestone's review):** no raw secret ever reaches a durable record —
config, ledger event, artifact metadata, or log line.

**Acceptance criteria:** a config value that looks like a raw secret is
rejected at load time; ledger events and artifacts for a credentialed
operation contain no secret material under inspection; version-output-only
"detection" of an authenticated CLI is explicitly not accepted as a
`command_available`/readiness signal.

### WP-M3B-5 — Doctor readiness and recommendation engine

**Objective:** `devcadence doctor` diagnostic evaluation and the pure-
function deployment-profile recommendation engine.

**Deliverables:**
- `DoctorReport` with explicit `ReadinessEvaluationScope` (target profile,
  required roles, evidence freshness) per ADR-0014 §5.
- The four normative readiness states (`READY`,
  `READY_WITH_REDUCED_CAPABILITY`, `PARTIALLY_READY`, `ACTION_REQUIRED`)
  computed correctly against live, verified endpoint evidence — memory/
  hardware presence alone never establishes capability.
- Recommendation engine as a pure function over `RecommendationInput`
  (facts, profile, policy, preferences) → `SelectedProfile` (one of
  local-heavy / hybrid-thin / cloud-cognition / offline / custom) or
  `nil` with reported missing prerequisites.
- `doctor --fix` as a convenience that only generates a `SetupPlan` file —
  never executes or approves anything (ADR-0014 §7).

**Acceptance criteria:** same synthetic-fixture-machine verification
pattern M3A already established (`internal/environment/fixtures.go`) — no
GPU/runtime/network required; a fixture with no viable profile returns
`nil` selection plus missing-prerequisite reasons, never a forced choice;
cached machine-profile evidence past its freshness window is treated as
stale, not silently reused.

### WP-M3B-6 — Bounded recipes

**Objective:** the actual recipe set — the concrete `TypedOperation`
instances doctor/setup can plan and execute.

**Deliverables:**
- Versioned recipes for the operation kinds from WP-M3B-1, each with
  declared registries, immutable digests, sizes, and licenses for anything
  it installs/downloads (ADR-0014 §7).
- System/kernel/driver-level operations classified strictly
  `AuthorityHighImpactManual` — never auto-executable.
- Model/package pre-resolution (resolving a digest before planning, so the
  plan itself is immutable and reproducible).

**Acceptance criteria:** every executable recipe's declared authority
matches or exceeds its `IntrinsicPolicy` (WP-M3B-1); a recipe with no
resolvable digest fails plan generation rather than planning an
under-specified action; license metadata is present for every
install-class operation.

### WP-M3B-7 — CLI surface (non-interactive/plain/JSON first)

**Objective:** wire WP-M3B-1 through WP-M3B-6 into the actual
`devcadence doctor` / `devcadence setup` commands, non-interactive and
`--json` modes first — the TUI (WP-M3B-8) layers on top of this, not the
other way around.

**Deliverables:**
- `devcadence doctor` (plain and `--json` output).
- `devcadence setup plan` / `devcadence setup apply` (already scoped in
  WP-M3B-3; this WP is the CLI-command wiring/UX polish, help text, exit
  codes, `--no-tui` accepted as a no-op since there's no TUI yet).
- Non-interactive mode emits no control sequences of any kind, verified by
  fixture.

**Acceptance criteria:** every command has a `--json` mode whose output
validates against a published schema; help output documents the two-step
approval workflow; exit codes distinguish "nothing to do," "plan
generated," "drift detected, refresh needed," and "execution failed."

### WP-M3B-8 — Terminal UX

**Objective:** the compact terminal UX using Huh v2 with Bubble Tea v2/Lip
Gloss v2, per `docs/IMPLEMENTATION_PLAN.md`'s M3B deliverables list — this
is new external-dependency surface with no ADR precedent yet in this repo,
so budget more iteration than the other WPs.

**Deliverables:**
- Interactive `doctor`/`setup` flows using Huh v2 for prompts/approval,
  Bubble Tea v2/Lip Gloss v2 where richer dynamic rendering
  (progress, live ledger tail) is warranted.
- SSH/plain-terminal fallback and `--no-tui` becomes a real flag (not the
  WP-M3B-7 no-op).
- Accessible-mode behavior (no reliance on color/motion alone for meaning).

**Acceptance criteria:** the interactive flow and the non-interactive
(`--json`/`--no-tui`) flow produce the same underlying `SetupPlan`/
execution result for the same inputs — the TUI is a rendering layer, not a
second code path with independent logic; SSH/basic-terminal smoke test
passes; non-interactive mode still emits zero control sequences (repeat
the WP-M3B-7 fixture against the TUI build to catch a regression).

### WP-M3B-9 — Verification suite and docs sync

**Objective:** close the milestone. Full verification suite across the
fixture matrix `docs/IMPLEMENTATION_PLAN.md`'s M3B section already
specifies, and required documentation synchronization (AGENTS.md §14 —
"an implementation that changes behavior but leaves normative docs
misleading is not done").

**Deliverables:**
- Fixture coverage for: blank machine with no optional AI software; dry-run
  shows every planned mutation; privileged/high-impact changes require
  explicit approval; interrupted setup re-runs safely (exercises
  WP-M3B-2's recovery path end-to-end through the real CLI); existing
  usable tools preferred over unnecessary installation; non-interactive
  mode emits no TUI control sequences; SSH/TTY/basic-terminal behavior;
  readiness summary correctly reports reduced capability rather than
  generic failure.
- `docs/IMPLEMENTATION_PLAN.md`: flip M3B's status from "not implemented"
  to "implemented," matching the M3A section's own precedent for how that
  status update should read (cite the actual test names that satisfy the
  exit criterion, as M3A's own entry does).
- `docs/SETUP.md`: update the "remain intended M3B behaviour" framing to
  describe what's actually implemented.
- `README.md`/`INVARIANTS.md`: spot-check for anything M3B changes that
  needs reflecting there.
- Delete `HANDOFF.md` from the branch as the final commit before the PR
  goes to review, per `AGENT_HANDOFF_PROTOCOL.md`.

**Acceptance criteria:** every fixture above passes; `go test -count=1
./... && go test -race ./... && go vet ./...` clean; the milestone's own
exit criterion in `docs/IMPLEMENTATION_PLAN.md` is met and the entry says
so with cited evidence, not just "done."

---

## Future milestones

Add a new `## <Milestone>` section here, following the same shape (branch
name, normative grounding, dependency chain, WP entries with objective/
deliverables/MUST/acceptance-criteria/non-goals), when the next milestone
after M3B is ready to be broken down. Don't pre-populate future milestones
speculatively — this file describes work that's actually about to start,
not the whole remaining roadmap in advance.

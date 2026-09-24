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
AGENTS.md §6 describes, and a scope card is not implementation authority.**
Per `AGENT_HANDOFF_PROTOCOL.md`'s "Principal/Implementer separation," a
Principal-capable session must read the milestone's ADRs (cited per WP
below), expand the scope card into a full Work Package (objective,
architectural intent, MUST/SHOULD/SUGGESTED/LOCAL_DISCRETION constraints,
interface sketches, pseudocode where logic is non-trivial, edge cases,
acceptance criteria, base commit) per AGENTS.md §6, and commit that EWP as
its own linked file (e.g. `docs/work-packages/wp-m3b-4-ewp.md`) before any
implementation code is written against it. An implementation session works
against the committed EWP, not the scope card directly, and escalates
(amending the EWP explicitly) rather than silently redesigning if it finds
the EWP's assumptions false.

All Work Packages for one milestone land on **one shared branch**, one PR,
per `AGENT_HANDOFF_PROTOCOL.md` — not a branch/PR per WP. Only one session
implements at a time (see that protocol's "Concurrency model"); WPs with no
dependency on each other may be done in whichever order, never
concurrently, under this v1 protocol.

---

## M3B — Guided bootstrap and onboarding

Branch: `feat/m3b-guided-bootstrap` (create when WP-M3B-1 starts).

Normative grounding for this milestone: ADR-0014 (guided bootstrap, setup
plans, operational event ledger, readiness contracts — already `Accepted`,
so the architecture decisions below are mostly settled, not open for
relitigation), ADR-0011, ADR-0013, ADR-0018, `docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md`,
`docs/MODEL_RUNTIME.md`, `docs/SETUP.md`, `docs/SECURITY.md`. Read these —
in that order — before expanding WP-M3B-1.

Dependency chain: WP1 → WP2 → WP3 → {WP4, WP5, WP6 in any order} → WP7 →
WP8. WP4–WP6 have no dependency on each other and may be completed in
any order; under this protocol's single-writer model they are still
implemented one at a time, not concurrently — see
`AGENT_HANDOFF_PROTOCOL.md`'s "Concurrency model."

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

### WP-M3B-3 — Executor and approval semantics (service layer, no public CLI)

**Objective:** wire WP-M3B-1's plan types and WP-M3B-2's ledger into an
actual executor with the two-step approval workflow — as a service-level
API `internal/setup` exposes, not the public CLI command. Public command
registration/flag-parsing/presentation is WP-M3B-7's job; this WP owns the
approval/precondition/execution *semantics* the CLI will later call.

**Deliverables:**
- Service-level plan-generation and plan-apply operations implementing the
  two-step approval workflow from ADR-0014 §2 (`PlanDigest` verification,
  `--yes` scope, drift rejection) as Go APIs a caller invokes directly — a
  thin test harness is fine, a registered Cobra/CLI command is not this
  WP's deliverable.
- Precondition rechecking immediately before each action executes; any
  drift since plan generation invalidates approval and halts, demanding a
  fresh plan.
- `--yes`-equivalent scope: authorizes only `user_confirmation`-level
  actions; privileged/high-impact actions always require the explicit
  digest-approval path, regardless of how the caller is invoked.
- Subprocess execution exclusively through `internal/process.Runner`
  behind the narrow `internal/setup.CommandRunner` interface (ADR-0014 §7).
- Output capture bounded to 4 MiB, ANSI-stripped, content-addressed;
  authentication operations capture zero raw output artifacts.

**MUST:** no operation reachable through any path other than this executor
— no ad hoc shell-out anywhere else in `internal/setup`. No public command
registration in this WP (WP-M3B-7 owns that surface).

**Acceptance criteria:** a plan approved with the wrong digest is rejected;
a plan whose preconditions drifted between generation and apply halts
without executing later actions; the `--yes`-equivalent scope on a plan
containing a privileged action is rejected outright; output artifacts
respect the byte cap and never appear for authentication operations — all
exercised against the service API directly, without a CLI in the loop.

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
operation contain no secret material under inspection; version output may
establish installation/`command_available` evidence only — it MUST NOT be
accepted as establishing authenticated-session availability or provider/
cognition readiness, which require an actual authenticated-call probe or
equivalent evidence (ADR-0014 §6: version output proves installation,
never authentication — this is narrower than "never a readiness signal at
all," since installation genuinely is one legitimate `command_available`
signal).

### WP-M3B-5 — Doctor readiness and resource inventory (service layer, no public CLI)

**Objective:** provide deterministic `devcadence doctor` readiness evaluation and the factual ResourceInventory consumed by M3C. M3B does **not** solve the multidimensional portfolio optimization problem with a static profile chooser.

**Deliverables:**
- DoctorReport with explicit readiness scope;
- normative readiness states from verified evidence;
- deterministic ResourceInventory over hardware, runtimes/models, cognition endpoints, session evidence, hosts, credential references and available economic/policy metadata;
- profile labels MAY be emitted for UX, but are not a closed SelectedProfile routing decision;
- `doctor --fix` generates SetupPlan from concrete missing/remediable facts and never AI-selects a cognition portfolio.

**MUST:** no provider/model role doctrine or static weighted "optimal" portfolio logic. AI-assisted synthesis belongs to M3C/ADR-0018.

**Acceptance criteria:** fixtures produce stable readiness + ResourceInventory without GPU/runtime/network; optional resource absence degrades gracefully; stale evidence is reported; provider/model renaming does not create built-in role preference; no cognition endpoint remains a valid deterministic state.

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

### WP-M3B-7 — CLI surface and minimal guided interaction

**Objective:** own all public command registration for `devcadence doctor` /
`devcadence setup`, wiring WP-M3B-3's and WP-M3B-5's service APIs (plus
WP-M3B-1/2/4/6 underneath them) to actual commands. M3B requires a safe,
usable plain/JSON/basic-terminal surface, not the final rich adaptive setup UI.

**Deliverables:**
- `devcadence doctor` command (plain and `--json` output), including
  `--fix` calling WP-M3B-5's plan-generation behavior;
- `devcadence setup plan` / `devcadence setup apply` commands calling
  WP-M3B-3's service API;
- help text documenting the two-step approval workflow and a defined exit-code
  contract;
- minimal confirmations where interactive approval is required, with SSH/basic
  terminal fallback;
- non-interactive mode emits no control sequences and remains fully usable;
- `--no-tui` may remain a compatibility/no-op flag until M3D adds the richer
  adaptive terminal experience.

**MUST:** the CLI is a rendering/argument layer only. Approval, readiness,
credential and remediation semantics remain in their owning services. Do not
introduce Huh/Bubble Tea/Lip Gloss as an M3B completion dependency.

**Acceptance criteria:** every machine-readable command has `--json` output
validated by schema; help documents approval; exit codes distinguish no-op,
plan generated, drift/refresh required and execution failure; plain/SSH
operation works; no control sequences appear in non-interactive output; no
service semantics are duplicated in command code.

### WP-M3B-8 — Verification suite and docs sync

**Objective:** close the deterministic-bootstrap milestone. Full verification
across the fixture matrix and synchronization of normative docs.

**Deliverables:**
- fixture coverage for blank machine; dry-run mutation visibility;
  privileged/high-impact approval; interrupted setup recovery through the real
  CLI; preference for existing usable tools; plain/non-interactive operation;
  SSH/basic-terminal behavior; and readiness/resource-inventory degradation
  rather than generic failure;
- `docs/IMPLEMENTATION_PLAN.md`: mark M3B implemented with cited test evidence;
- `docs/SETUP.md`: describe implemented M3B behavior and clearly defer rich
  adaptive onboarding to M3D;
- README/INVARIANTS spot-check;
- HANDOFF.md retained through review/repair, then removed before the frozen
  closure candidate per `AGENT_HANDOFF_PROTOCOL.md`.

**Acceptance criteria:** all fixtures pass; `go test -count=1 ./... && go
test -race ./... && go vet ./...` clean; the M3B exit criterion is met and
documented with evidence.

---

## M3C — Cognition resource and session substrate

M3C begins after M3B deterministic bootstrap contracts stabilize. It builds
provider-neutral deterministic cognition/economic/session infrastructure; it
does not yet let AI choose the portfolio.

Branch: `feat/m3c-cognition-substrate`.

### WP-M3C-1 — Portfolio protocol, economics and policy
Define AccessChannel/session capabilities, EconomicRegime, BudgetPool,
BudgetState/ResourceState, policy constraints, CognitionPortfolio,
PortfolioRecommendation and WorkflowPlan protocol/schema shapes without
overloading model identity with billing semantics.

### WP-M3C-2 — Session-driver abstraction
Normalize model selection, structured/streaming events, resume, cancellation,
worktree/tool/MCP access and usage/quota evidence across authenticated
CLIs/SDKs/APIs/local runtimes. Include at least two materially different
drivers and a fake third-adapter contract test.

### WP-M3C-3 — Deterministic portfolio validator and activation
Validate endpoints, capability provenance, source exposure, spending/overage,
budget bindings, driver features and resource constraints. Add versioned
portfolio activation/persistence/rollback primitives. No AI output can bypass
this layer later.

### WP-M3C-4 — Substrate integration verification
Exercise local, subscription, metered and mixed candidate portfolios; missing
quota/resource evidence; endpoint removal; denied source exposure/spend; and
driver capability differences. Prove a future driver can participate without
core provider-specific changes.

## M3D — Adaptive portfolio and workflow synthesis

M3D consumes M3C's deterministic substrate and adds AI-assisted recommendation,
adaptive workflow topology and the richer setup/explanation UX.

Branch: `feat/m3d-adaptive-planning`.

### WP-M3D-1 — AI-assisted Portfolio Planner
Use any sufficiently capable eligible endpoint to synthesize typed alternatives
from ResourceInventory + role needs + project characteristics + policy +
available historical evidence. Include rationale/tradeoffs/confidence and no
authority expansion.

### WP-M3D-2 — Workflow topology planner
Compile task/risk + active portfolio + current resource state into a bounded
WorkflowPlan. Explicitly support topology collapse, local-heavy iteration,
subscription-diverse review and metered-budget-constrained execution.

### WP-M3D-3 — Adaptation, versioning and rollback
Support explicit portfolio-change proposals when resources/policy/evidence
change, with auditable diffs, deterministic revalidation, activation,
versioning and rollback. No silent learned/policy mutation.

### WP-M3D-4 — Adaptive setup and explanation UX
Integrate recommend/explain/apply into the setup experience. Plain/JSON remains
canonical; optional rich terminal rendering (Huh/Bubble Tea/Lip Gloss if still
justified) presents the same underlying typed recommendation/validation path,
never a second decision engine.

### WP-M3D-5 — Cross-portfolio verification
Cover Apple/MLX, NVIDIA/local, one subscription, multiple subscriptions,
paid-API allowed/forbidden, mixed portfolios, endpoint loss, quota pressure,
new-resource addition, topology collapse and future-driver extensibility.

## Future milestones

Add a new `## <Milestone>` section here, following the same shape (branch
name, normative grounding, dependency chain, WP entries with objective/
deliverables/MUST/acceptance-criteria/non-goals), when a later milestone is ready to be broken down into implementation-sized
work. Don't pre-populate all future milestones speculatively — this file keeps
detailed scope cards only for work close enough to execute.

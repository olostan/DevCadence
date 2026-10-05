# Rolling Planning Window 2026-10-F — M5 semantic boundary preparation

## Identity and status

- Revision: 2; authoring base: `bd6c424e292815460033b2570dce4d682ef5cb73` (`origin/main`, 2026-10-05).
- Branch: `docs/m5-planning-window`; design-only. No implementation, endpoint execution, credential use or spending is authorized by this window.
- Status: **DRAFT — NOT FROZEN**. Architecture and Implementation Readiness require independent verdicts. A documentation PR is not permission to execute.
- Source of milestone scope: [IMPLEMENTATION_PLAN M5](../IMPLEMENTATION_PLAN.md#m5--semantic-principal-integration-and-host-portability); M4's synthetic caveat and mandatory real re-evaluation remain binding.

## Objective and decomposition

Prepare the host-neutral semantic boundary without delegating unresolved runtime architecture to implementers. Five review units cover the requested M5 domains:

| EWP | Owns | Dependencies | Independent acceptance |
| --- | --- | --- | --- |
| [WP-M5-1](wp-m5-1-semantic-contract-ewp.md) | Principal v1 request identities, local caller authority, transactional state/WP guards and batch writes | M1–M3 durable contracts | Two callers cannot both mutate the same expected prefix; old records remain readable |
| [WP-M5-2](wp-m5-2-semantic-mcp-ewp.md) | Semantic application facade, bounded evidence, thin stdio MCP, execution ports | WP1 | Real state/evidence/persistence tools; unavailable executors deny explicitly; no raw tools |
| [WP-M5-3](wp-m5-3-discovery-ewp.md) | Discovery facade, ledger consistency, human decisions, requirements/readiness persistence | WP1, WP2 facade boundary | Fresh Day-0 state is reconstructed without a transcript; model cannot confirm human intent |
| [WP-M5-4](wp-m5-4-host-integration-ewp.md) | PrincipalHost adapters, Antigravity reference, Cursor portability, blank-host guidance | WP2, WP3 for discovery smoke | Identical semantic wire contract, approved configuration and independently verified firewall |
| [WP-M5-5](wp-m5-5-empirical-campaign-ewp.md) | Real-evidence admission contract, bounded campaign protocol and M4 re-evaluation runbook | WP2; live-runtime follow-on; human run authorization | Honest gate replay from independently attested empirical outcomes; no synthetic substitution |

**Scope adjustment, grounded in repository evidence:** the suggested five EWPs assumed a runtime that does not exist. `controlplane.Apply` records one lifecycle event, not worker execution. `TaskDelegated` and `AttemptStarted` are separate events. The current benchmark consumes model text but has no model-output patch materializer; its default verifier simulates outcomes. Neither `Delegate`, a production review dispatcher nor a real provider-client composition is implemented. These are facts from the bounded scout, not an argument to widen WP2 invisibly.

This is the **first M5 rolling window**, not a claim that five packages complete M5. WP2 specifies executable ports and fail-closed absence, while the next window must provide bounded production task execution, independent review and empirical candidate verification. WP5 closes the evidence/authorization design now, but live campaign execution remains blocked until those dependencies are accepted. Host connection tests alone cannot satisfy the milestone's one-task exit criterion. No EWP is frozen for implementation before its own current-base gate.

### Required follow-on window (explicit scope cards, not implementation authority)

1. **M5-R1 native task executor:** worktree creation, compiler-admitted worker context, eligible live endpoint resolver, candidate materialization, durable attempt outcomes, bounded cancellation/crash recovery. Must implement WP2's `TaskExecutor` port; no executor-private state becomes authority (DCI-159–161).
2. **M5-R2 independent review executor and acceptance gate:** immutable candidate/evidence pack, canonical ActorProvenance binding, versioned acceptance policy, independent review dimensions, durable results and unresolved-disagreement handling; activate acceptance only through its separately accepted transaction-guarded contract. Full M7 campaign automation remains M7; the minimal M5 task path still requires actual independent review.
3. **M5-R3 empirical verifier/provider composition:** real patch application and deterministic seeded-defect verification, endpoint/model identity per tier, production driver clients and reproducible outcomes. Must satisfy WP5's empirical admission conditions.

4. **M5-R4 protected operator evidence ingress:** enforceable human/owner receipt issuance, statement/subject/purpose/digest binding and independence from the model process. Same-UID JSON/CLI flags are insufficient. Human-changing discovery and campaign authorization remain denied until this dependency is accepted.

Each requires its own implementation-ready EWP, architecture critique and independent review. Do not let a worker invent them inside WP2 or WP5. The next window may split these cards further. This is a disclosed representability/implementation gap; until filled, **M5 cannot close or self-host task development**.

## Context Manifest and reading record

Role: Principal/spec author; read authority includes normative design reconciliation and bounded scout evidence. Write authority in this change: `docs/work-packages/window-2026-10-f-overview.md`, the five linked EWPs, and minimal planning links in `docs/WORK_PACKAGES.md`, `docs/IMPLEMENTATION_PLAN.md`, `docs/MCP_API.md`, `docs/PRINCIPAL_HOSTS.md`. No source/schema/invariant changes now.

Risk tags: authority, state concurrency, immutable evidence, host isolation, discovery epistemics, external spend, empirical provenance, protocol compatibility. Re-resolution triggers: advanced main, different existing signature, new credential/provider path, undeclared durable record, host/SDK version drift, changed M4 criteria, or evidence of fail-open behavior.

| Source admitted | Why / exact sections |
| --- | --- |
| AGENTS §§1–9, 12–15,17; docs/README normative hierarchy/context routing | Authoring, scope, workers, review independence, hooks and truthful acceptance |
| WORK_PACKAGES Rolling Planning Windows; Execution Contract/Readiness; ADR-0024 §§1–8 | Window review and zero implementation-critical ambiguity |
| IMPLEMENTATION_PLAN M4 Status/Measurements/Exit; M5 verbatim; M2.5 Principal exposure | Carry-over, actual milestone scope, evidence tiers and deferred runtime |
| MCP_API §§1–2A,5–10 | Tool names, intended semantics, stale requests, async and thin adapter |
| PRINCIPAL_HOSTS §§1–8; ANTIGRAVITY_INTEGRATION §§3–6,8–11,17–18 | Host/provider separation, setup, config, instruction and firewall obligations |
| PROJECT_STATE §§7.1–7.4,13–17.1 | Prefix identity, checked lineage and discovery derivation |
| PRINCIPAL_ENGINEER §§1A–6,9,11–16 | Specification precondition, alternatives, assumptions, semantic closure |
| DISCOVERY_AND_SPECIFICATION §§1,3–4,8–12,16–19,21 | Human authority, epistemic status and readiness |
| SECURITY §§1–9,11–12A,14–17; exact INVARIANTS clauses per EWP | Permissions, source exposure, controlled execution, secrets, setup and audit |
| ADR-0002 Decision §§2–7a; ADR-0003 Decision; ADR-0004 Decision; ADR-0005 Decision | Storage transaction, schema compatibility, lifecycle and state identity |
| ADR-0016 §1 and accepted 2026-09-23 amendment | Compact search/symbol results versus worker-mediated content; overrides broad file-depth examples |
| ADR-0010; REVIEW_AND_CONVERGENCE §§1,6–7,14 | One consolidated repair, focused independent verification, bounded closure |
| Window E, WP-M4-6, WP-M3D-1C1, Implementation-ready template | Format/provenance exemplars, not inherited approval |
| ENGINEERING_STANDARDS §§1–5; ENGINEERING_HEALTH_POLICY §§1–5 | Boundaries, versioned schemas, hooks/checks and health |

The prompt's targeted reading order takes precedence over indiscriminate whole-corpus loading. All relevant domains above were resolved; unrelated future M6–M10 implementation is not a resident reading assignment.

## Window-level architecture contract

### Dependency direction and ownership

`protocol`, `events`, `state`, `storage`, `controlplane` remain host/provider/transport independent. New `internal/principal` is application-layer composition and may import those packages, evidence/validation abstractions and narrow executor ports. `internal/mcpadapter` imports principal/protocol and the pinned MCP SDK; the command entry point composes local dependencies. Host-specific recipes reside in adapter packages under `internal/principalhosts`; no core package imports them. Native runtime follow-on adapters may import the facade's ports, never the reverse. No Antigravity-specific core types; no one host is mandatory.

WP1 owns shared request/response schemas and concurrency guards. WP2 owns semantic dispatch, tool registration and ports. WP3 owns discovery semantics, including its versioned snapshot representation. WP4 owns host configuration/instructions and smoke evidence. WP5 owns empirical run manifest/attestation and authorization semantics, not a production task scheduler.

### Authority rules resolved once

1. The local operator provisions a protected per-launch binding: project allowlist, principal identity, allowed semantic actions, source-depth policy and opaque references to existing policy/approval. Principal requests cannot choose their role, actor, policy, credentials or authority. Host tool approval does not confer server authority.
2. MCP is single-user stdio; no HTTP listener, OAuth service, multi-user auth or browser write API. No policy/approval mutation or operator-capability minting is exposed as an MCP tool. Missing binding means no server launch; missing action grant means `POLICY_DENIED`.
3. Human-only product decisions use a separately verified local operator receipt tied to exact statement/artifact digest and project. A request field saying `authority: human` is evidence to validate, never proof. Human confirmation cannot be inferred from a host conversation.
4. Context admission grants no write/network/source/spend permission. Executor ports re-check current portfolio, policy, budget, freshness, capabilities and isolation before effects. Absence is denied, not guessed or routed to cloud.
5. Acceptance is **disabled** in this preparation facade, regardless of grant or apparently passing record. M5-R2 must define canonical policy/producer evidence and use transactional read guards before activating it. Enabled acceptance remains separate from integration and never merges/advances accepted source by itself. Unknown gates deny.

### State and failure semantics

- Strict stale policy for this bootstrap: equality with the current prefix is required for mutations. The optional disjoint-change accommodation in MCP_API is not used. State revision is opaque on the wire; inside a transaction compare the full canonical revision, not lexical order.
- A WP is fresh for execution only when the requested exact approved ID/version/digest/base matches current task lineage and the task has not changed since approval. Global unrelated events may advance state; the caller still supplies fresh state, but the historical WP planning revision is preserved rather than rewritten to that fresh state.
- WP1 introduces bounded trusted transaction read guards and write-intent serialization with cancellation-aware contention, plus transactional batch commands so delegation plus attempt-start, and multi-object discovery persistence, cannot partially commit. Records/events/projection commit together; a before-commit error exposes none. An ambiguous commit response requires state/record lookup before any new action; retry never silently repeats external effects.
- No durable executor job queue is smuggled into MCP. WP2 initially returns process-scoped operation handles with explicit instance identity; lost operations become `OPERATION_LOST` after restart, without automatic replay. Durable task/attempt evidence stays canonical. Follow-on executor recovery must be specified separately.
- Source evidence is project/base-bound, digest-checked and redacted before serialization. Search/symbol may be fulfilled directly. Content is always bounded worker-mediated. No raw `read_file`, `fetch_content`, shell, arbitrary SQL, network URL fetch, credential or destructive tools/resources/prompts are exposed.

### Shared interface and schema policy

New principal wire schemas use family version `1.0`, strict additional-property rejection, finite closed action/depth/error enums and required arrays encoded as `[]`. Existing durable record schemas are not loosened. Any new optional fields follow ADR-0003 and carry explicit schema compatibility. WP1 defines common `CallMeta`, `CallerContext`, `WorkPackageRef`, `CandidateRef`, `SemanticError`, `OperationRef`; WP2/3 define their owned request payloads. Host adapters consume the same bytes.

No mutation takes a free-form event or generic arbitrary record command from the principal. Typed semantic methods map to explicit event constructors with server-set actor/time/IDs. Unknown action/enum/schema or trailing JSON fails before dispatch.

## Top five design decisions and rejected alternatives

| Decision / criteria | Selected | Viable alternative rejected | Adversarial critique / mitigation |
| --- | --- | --- | --- |
| Contract layering: authority, substitutability, testability | Application facade plus transport adapters | Put guards into each host/MCP handler | Facade can become a god service; use operation-owned files/ports and package dependency tests |
| Concurrency: durability, simpler weaker-worker contract | Transactional prefix CAS plus typed batch | Read-before-write freshness plus mutex | Batch expands trusted surface; enforce all members, one project, per-event reduction and rollback tests |
| MCP: interoperability, supply chain | Official Go SDK pinned at implementation gate; support its documented revisions | Hand-written JSON-RPC transport | SDK behavior may drift; pin version, host transcript test and stdout cleanliness; no semantic policy in SDK |
| Strict workspace: security, honest claims | Separate workspace plus independently tested host/OS denial | Prompt/rule-only prohibition | Host-wide privileges can escape workspace; strict-ready stays blocked without negative permission tests; assisted mode explicit |
| Empirical gate: scientific validity, spending | Outcome/identity/authorization-bound attestation plus unchanged gate | Relabel existing synthetic snapshots `empirical_campaign` | Attestation alone can lie; independent verifier checks real candidates/commands/source hashes; never numeric unknown=0 |

No irreversible architecture decision in this proposed window becomes accepted merely because it appears in this table. Existing ADR-0025 is **Proposed**, not Accepted; DCI-159–161 and approved architecture remain authoritative. New stable schema/authority decisions require normal owner/reviewer acceptance of the window and owning-doc synchronization in implementation.

## Facts, assumptions, inferences and unresolved owner inputs

**FACT:** current main has inventory-only principalhosts; one-event Apply; no task/review runtime; benchmark simulated verification; synthetic-only M4 report. Evidence paths and symbols are in each EWP.

**INFERENCE:** shipping an MCP shim does not make task execution or M4 evidence empirical. Consequently there must be a follow-on runtime window before M5 exit.

**PREFERENCE:** strict equality CAS, Cursor as first portability proof, single-user local binding. These implement existing fail-closed/host-neutral requirements without assuming provider preference.

**UNKNOWN / owner inputs for live execution only:** which real endpoints may be used, which source may leave the machine, allowed dollar/quota/compute/time caps, and which host/OS installations the operator can test. No answer is required to review the design; unset values block the affected live action. No token keys or credential strings should be supplied in this PR.

## Documentation/schema synchronization

This PR adds planning links only. Each implementation EWP owns its exact sync: MCP_API, PRINCIPAL_HOSTS, PROTOCOLS, PROJECT_STATE, discovery and host instructions, schemas/README and new fixtures. Status prose must distinguish implemented, unavailable and manually verified. No INVARIANTS/catalog edit is planned. If a later authorized amendment changes either, `make update-goldens` and the regenerated digest fixture belong in the same commit; do not regenerate without a source change.

## Review and freezing

Review input is an immutable document snapshot with file hashes, base SHA and complete five-EWP contracts. Two clean reviewers in parallel: Architecture/Contract and Implementability/Failure Semantics. Consolidate material findings into **one repair round**; then obtain focused independent verification of changed obligations. Authors never establish their own PASS. New equivalent opinion does not restart broad review. If blockers remain, keep draft and record explicit unresolved items rather than freeze.

| Lens | Candidate | Verdict | Repair / verification |
| --- | --- | --- | --- |
| Architecture/Contract | r2 immutable manifest SHA-256 e4722b5103d4d23778021f7310027e8d4accefb1babdcc87db49727704a9290e | APPROVE_DRAFT | Transaction read guards and SQLite contention; complete evidence/discovery contracts; host launch binding; trusted verifier and pre-run bindings independently verified |
| Implementability/Failure Semantics | same r2 manifest | APPROVE_DRAFT | Transaction/evidence/action boundaries; readiness and decision overlay; deferral semantics; host mutation/rollback; verifier and plan integrity independently verified |

Both reviewers approve the **reduced preparation draft only**. Neither approves implementation readiness, architecture freeze, enabled acceptance, human/operator authority, live execution or M5 closure. All five contracts remain NOT_READY until their applicable dependency/current-base/admission gates close. No remaining material draft-contract blocker was reported in focused repair verification. Uncovered evidence remains explicit: protected operator ingress, native host/OS smoke and isolation, actual task/scout/review/verifier execution, live usage/spend and empirical outcome verification. Subsequent documentation cleanup aligns review status and owning-contract references without changing proposed execution semantics. Session validation and publication exceptions are tracked in the PR discussion.

Per-WP current-base gates must still resolve actual accepted dependency SHAs, exact signatures/paths, full contract admission and executable validation commands. Every EWP's authoring base is pinned now; subsequent changes require explicit re-resolution. Missing live hardware/host/credential evidence is disclosed, never represented by mocked smoke PASS.

## M5 closure checklist

- Real principal uses project_state, investigation, authored EWP, delegation, validation, independent review and authorized acceptance through semantic tools; no ambient repository browsing.
- Antigravity and Cursor (or an explicitly amended VS Code alternative) consume the same contract and have installed-version smoke evidence.
- Day-0 decisions/requirements/readiness survive restart and retain human/evidence provenance.
- Blank host can skip, choose, plan, approve and verify setup.
- Live task/review executors and empirical verifier are accepted, not supplied by unreviewed operator scripts.
- Bounded real Strategy 1/4 campaign per authorized available capability tier passes empirical provenance admission; missing tiers and unknown metrics are explicit.
- Existing gate outputs and contradictions with synthetic results are recorded. Revise/Inconclusive prevents a claim of proven hypothesis and triggers a normal repair/policy EWP.

## Changelog

- r1 (2026-10-05): initial five-unit preparation window; disclosed missing production runtime and empirical verifier rather than hiding them in the MCP EWP.

- r2: one consolidated dual-review repair round; shared read guards/concurrency, complete evidence DTOs/actions, disabled acceptance, discovery readiness/overlay corrections, complete host launch and safe manual fallback, acyclic empirical plan/verifier.

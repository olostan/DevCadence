# Rolling Planning Window 2026-10-G — M5 runtime completion

## Identity and status

- Revision: 2 (repair round 1 applied after two independent reviews; see Review); authoring base: `71bdaec6d6d81c1b6e52d8b30f0f8485f928925a` (`main` `cebb4f0`, the merged WP-M5-1..5 implementations, plus the PR #83 reconcile merge; 2026-10-05).
- Branch: `ccr-6065f4db-biu95i`; design-only. No implementation, endpoint execution, credential use, spending or key enrolment is authorized by this window.
- Status: **DRAFT — NOT_READY / NOT FROZEN**. One review round has happened and its findings were repaired in r2, but the repaired text has **not** been independently re-verified; no Part has an approving verdict (see Review). A documentation PR is not permission to execute.
- Source of scope: the "Required follow-on window" of [window F](window-2026-10-f-overview.md#required-follow-on-window-explicit-scope-cards-not-implementation-authority) and the blocked items in the Implementation records of [WP-M5-3](wp-m5-3-discovery-ewp.md), [WP-M5-4](wp-m5-4-host-integration-ewp.md) and [WP-M5-5](wp-m5-5-empirical-campaign-ewp.md). M4's synthetic caveat and the mandatory real re-evaluation remain binding ([IMPLEMENTATION_PLAN M5](../IMPLEMENTATION_PLAN.md#m5--semantic-principal-integration-and-host-portability)).

## Objective and decomposition

Window F delivered the semantic boundary with every runtime absent by design. This window specifies the runtime that fills it, without delegating unresolved architecture: a native task executor, an independent review executor with an enforceable acceptance gate, an empirical verifier with provider composition, and the protected operator ingress that three merged-but-denied features wait on.

| EWP | Owns | Dependencies | Independent acceptance |
| --- | --- | --- | --- |
| [M5-R4](wp-m5-r4-protected-operator-ingress-ewp.md) | Receipt format, protected trust anchors, one-time consumption, host-plan adapter; issuer ceremony (Part B) | WP-M5-1 batch guards | A same-identity forged receipt, a replay and a swapped anchor all fail closed; no mint surface |
| [M5-R1](wp-m5-r1-native-task-executor-ewp.md) | `ExecutionPolicy`, eligible-endpoint resolver, endpoint binding (A); project lock, worktree, compiler-admitted session, candidate commit, durable outcomes, cancellation, startup recovery (B); validation (C) | WP-M5-1/2; R2-A and R4-A (both needed already by A) | A candidate with lineage exists only via recorded intent and in-scope changes; a crash never replays a model call |
| [M5-R2](wp-m5-r2-independent-review-acceptance-ewp.md) | Invocation provenance and actor derivation (A); independent review executor (B); acceptance policy and transactional activation of `accept` (C) | R1 A/B for B; R4-A, the `controlplane` read amendments and the `facade.AcceptanceGate` replacement for C | Acceptance denies without complete, independent, current evidence and is re-verified in its own transaction |
| [M5-R3](wp-m5-r3-empirical-verifier-composition-ewp.md) | Loopback (Ollama native) provider composition (A); pinned independent verifier, campaign authority adapter, `Admitter`, CLI replay (B) | R1-A types; R2-A; R4-A; merged WP-M5-5 package | Admission can succeed for genuine verified evidence and cannot succeed for any label or fixture |

Dependency order and freeze units (each Part is a separate Implementation Readiness gate; sizes are chosen to fit one endpoint session):

~~~text
R4-A (verifier, anchors, consumption) ─┬─> R1-A (policy, resolver) ─> R1-B (executor) ─> R1-C (validate)
R2-A (provenance, actor derivation) ───┘                                │                 │
                                       R1-A, R1-B ─> R2-B (reviewer)  ─┘                 │
R4-A + R1-C + R2-B + controlplane/facade amendments ─> R2-C (acceptance gate)
R4-B (issuer ceremony; OWNER INPUT-1) ─────> enrolment, any live human authority
R3-A (loopback composition) <── R1-A     R3-B (verifier, authority, CLI) <── R2-A, R4-A, R1-B primitives
Later cards (not authored): R3-D corpus verification profiles -> R3-C campaign runner -> live M4 re-evaluation
~~~

Parallelism: R4-A and R2-A are mutually independent and independent of everything else, so they freeze first; **R1-A is not independent of them** (its resolver derives actor ids with R2-A and its policy loader verifies receipts with R4-A) and freezes after both; R1-B waits for R1-A, R1-C for R1-B; R2-B waits for R1-A/B; R2-C waits for R4-A, R2-B (evidence producers) and the authorized amendments; R3-A needs R1-A; R3-B needs R2-A, R4-A and the verifier-side primitives of R1-B.

### Items deliberately not authored here (explicit scope, no implementation authority)

1. **WP-M5-3 write side** (human-confirming discovery mutations). Its contract exists and stays DRAFT. After R4-A it still needs its own current-base gate and a review-provenance contract that uses R2-A/R2-B; R4 only supplies `HumanReceiptVerifier` semantics (`Verified` + `ConsumeOnce`).
2. **WP-M5-4 automatic apply and native host smoke.** R4-A supplies the host-plan approval verifier; automatic apply additionally needs an exclusive operator-owned writer channel M3 does not have, so manual apply stays the only path. Antigravity/Cursor installed-version smoke remains manual operator evidence.
3. **R3-C campaign runner** and **R3-D corpus verification profiles/fixtures** (see R3). They are needed only for the live re-evaluation.
4. Full M7 review-campaign automation, finding-closure ledger workflow, durable executor job queues, integration/merge automation.

## Context Manifest and reading record

Role: Principal/spec author. Read authority: normative design reconciliation and bounded signature scouts of the merged code; implementation bodies were not ingested (AGENTS §§2–3). Write authority: this overview, the four EWPs, and minimal planning links in `docs/WORK_PACKAGES.md` and `docs/IMPLEMENTATION_PLAN.md`. No source/schema/invariant changes.

| Source admitted | Why / exact sections |
| --- | --- |
| AGENTS §§1–9, 12–15, 17 | Authoring, scope, worker delegation, review independence, hooks, truthful acceptance |
| WORK_PACKAGES Rolling Planning Windows, Execution Contract, Implementation-ready template; ADR-0024 | Format, readiness and zero hidden ambiguity |
| Window F overview; Implementation records of WP-M5-1..5 (authoritative description of what now exists) | Facts, blocked items, follow-on cards |
| WP-M5-3 §Dependency surface and authority matrix; WP-M5-4 `ApprovalVerifier`; WP-M5-5 §§Exact representation, Authorization, Admission | Consumer interfaces R4/R3 must satisfy |
| SECURITY §§1, 3, 5–9, 12A, 14–17; INVARIANTS DCI-025, 030–033, 040–049, 080–084, 120–124, 133–135, 159–161 (exact text admitted per EWP) | Authority, isolation, review, spending, executor boundary |
| Signatures: `controlplane` batch/records, `facade` ports/Accept, `worktrees`, `process`, `validation`, `compiler`, `drivers`, `cognition` routing/portfolio, `protocol` review/actor types, `benchmark/empirical` | Verified facts per EWP |

Re-resolution triggers: advanced `main`, a changed facade/controlplane/empirical signature, a new credential or provider path, an owner decision that differs from a recommendation below, a new record kind that cannot be added additively, or any evidence of fail-open behavior.

## Window-level architecture contract

### Dependency direction and ownership

`protocol` (additive records), `events`, `state`, `storage`, `controlplane` stay host/provider independent. New leaf packages: `internal/operator/receipts` (R4), `internal/actors` (R2-A), `internal/execpolicy` (R1-A). Application composition: `internal/taskexec` (R1-B), `internal/reviewexec` (R2-B), `internal/acceptance` (R2-C); they import the facade's ports, never the reverse. Provider composition `internal/cognition/sessionclients` and `internal/benchmark/empirical/verifier` (R3) import `execpolicy`, `actors`, `receipts` adapters; `internal/benchmark/empirical` core imports none of them. The MCP SDK stays confined to `internal/mcpadapter`. No package gains a model-reachable surface that mints authority.

### Authority rules resolved once

1. **Human/owner authority is a verified receipt**, never a label: an Ed25519 signature over a typed statement bound to project, purpose, subject, digest and expiry, verified against trust anchors that live where the verifier's own OS identity cannot write (R4). Roots, same-identity files, flags, `AllowedActions`, `PolicyRef` and host tool approval are not authority.
2. **Standing policy: bytes pinned, authority re-verified at every use.** `ExecutionPolicy` (R1) and `AcceptancePolicy` (R2) are files whose bytes are digest-pinned at launch (a changed file denies until relaunch; changing content means a new receipt and a relaunch), but their `grant` receipt, expiry and revocation are re-verified at every `Delegate`, every `Review` start, every `Evaluate` and every campaign run admission (receipts are found by subject, R4). Revocation or expiry therefore stops the next start with no relaunch; an attempt already started ends under its own caps and wall time. No new projection field and no mutable runtime toggle.
3. **Executor-private state is never authority (DCI-160).** The executor's lock file and handle map decide only liveness; every policy-significant outcome is a typed control-plane event; recovery closes orphaned attempts as failed and never replays or fabricates success.
4. **Nothing degrades silently.** Missing policy, endpoint, cap, receipt, anchor protection, independent reviewer or evidence denies with a fixed code and zero effects. Zero cap is no authority. No fallback to another endpoint, tier or metered path (DCI-122).
5. **Independence is derived.** `ActorID` is computed by DevCadence (`internal/actors`) from the resolved endpoint basis (provider, family, account, endpoint, model and observed model revision, all carried by `ResolvedEndpoint`, which the R1 resolver alone derives); reviewers exclude the worker's actor; the acceptance gate recomputes and applies the existing `ActorsIndependent` rule. A model never names itself.
6. **Acceptance stays separate from integration.** Enabled acceptance records `ChangeAccepted` with a deterministic `AcceptanceEvidence` record and never merges or advances accepted source.

### State and failure semantics

Intent before effect: `TaskDelegated`+`AttemptStarted` commit in one guarded batch before the worktree and model call. Every other write is a typed event in a guarded batch with fresh expected prefix. `ReceiptConsumption` commits in the same transaction as the effect it authorizes. Evidence completeness for acceptance is enumerated inside the acceptance transaction by one additive `BatchReadView.AttemptEvidence` method plus an additive `Service.ReadView` (explicit amendments of WP-M5-1, R2 G-C1), and the unused `facade.CandidateGate` seam is replaced by `facade.AcceptanceGate` (R2 G-C2). The task stays `running` after a failed attempt (reducer fact), so a retry is `AttemptStarted` alone (R1). Ambiguous commit responses require a state/record lookup before any retry; retries never repeat external effects. Documented process-scoped operations (WP-M5-2) stay process-scoped; review failures leave no durable record by design.

### Shared schema policy

New durable records are additive kinds (`ReceiptConsumption`, `InvocationProvenance`, `AcceptanceEvidence`) with versioned schemas and fixtures under ADR-0003; no existing payload, record or wire schema is loosened. Edits to existing types are explicit and disclosed, and each needs window-level review before its Part freezes: (1) R3 amendments to the unissued WP-M5-5 plan/run/authorization types (`VerifierSourceCommit`, `VerificationProfileDigest`, `SessionEvidenceDigest`, integer `MaxAPISpendMicroUSD` replacing the float, `OperatorAuthority.VerifyAuthorization` now returning an `AuthorityWindow`, `SessionEvidence` in `empirical`); (2) `controlplane`: `BatchReadView.AttemptEvidence` and `Service.ReadView` (additive); (3) `facade`: `CandidateGate` (unused) replaced by `AcceptanceGate`, `Options.Acceptance`, additive `AcceptResult` fields; (4) R1-A `EndpointRequest.ExcludeActorIDs/IndependenceBasis` and `ResolvedEndpoint.Provider/ModelFamily/AccountRef` (new types, consumed by R2-B); (5) `receipts.Request.ReceiptID` optional with subject lookup.

## Top five design decisions and rejected alternatives

| Decision / criteria | Selected | Viable alternative rejected | Adversarial critique / mitigation |
| --- | --- | --- | --- |
| Protected ingress: independence from a same-identity agent, simplicity, testability | Signed receipts verified against anchors in a location the verifier's identity cannot write; one-time consumption record in the effect's transaction | In-process secret/HMAC token, TTY/keychain prompt or marker file | Anchor substitution is the real attack, so requirement R2 of M5-R4 makes protection a checked structural property; the signing-key custody is an owner choice (INPUT-1); a cached `sudo` credential is a disclosed residual risk |
| Standing authority: auditability, no schema churn | Digest-pinned policy bytes with receipt, expiry and revocation re-verified at every use; relaunch only to change content | A projection field for the active policy; mutable runtime config | Relaunch friction is acceptable for a single-user tool; each decision embeds the full policy copy for audit |
| Executor lifecycle: crash safety, honesty | Commit intent first, single-owner lock decides orphans, recovery records `executor_lost` | Effects first then commit; durable job queue | A crash leaves a failed attempt, not a clean slate; queue state would be authority-bearing (DCI-160) |
| Independence and acceptance: no cherry-picking, TOCTOU | Derived `ActorProvenance`; complete evidence enumerated and re-verified in the acceptance transaction | Principal-supplied evidence ids; a review-state projection | Needs one additive read method (explicit amendment); the M7 ledger remains the long-term home for finding closure |
| Empirical verification: scientific validity | Pinned verifier and profile re-execute deterministic checks on the immutable candidate and are compared with the claimed receipt | Trust a well-formed receipt; widen `TelemetrySnapshot` | Profile authoring is real work (card R3-D) and flaky checks block runs rather than pass them |

No irreversible decision here becomes accepted merely because it appears in this table. ADR-0025 remains **Proposed**; DCI-159–161 and approved architecture stay authoritative. A new ADR is required if a reviewer finds that the pinned-bytes/re-verified-authority policy or receipt design alters an accepted architectural boundary.

## OWNER INPUTS (explicit; safe default for every one is deny)

Each is detailed with a criteria comparison in the named EWP. No answer is needed to review the design. No keys, tokens, endpoints or credentials are to be supplied in this change.

| ID | Decision | Options compared | Recommendation | Remains blocked until answered |
| --- | --- | --- | --- | --- |
| INPUT-1 ([R4](wp-m5-r4-protected-operator-ingress-ewp.md#owner-input-1--protected-ingress-mechanism-on-a-single-user-machine)) | Protected-ingress mechanism on a single-user machine; operator account; OS | A dedicated OS account/root owns key, anchors and issuer, run via `su`/`sudo` with a password every time; B same-identity TTY/Touch ID prompt (insufficient); C = A plus hardware-key touch; D second-device signing | **A**, hardening toward C later; B rejected; Windows unsupported | R4 Part B, enrolment, human-confirming discovery writes, host-plan approval, campaign authorization, both policy activations |
| INPUT-2 ([R1](wp-m5-r1-native-task-executor-ewp.md#owner-input-2--endpoints-source-exposure-and-spend)) | Which endpoints/models may run, maximum source exposure and network domains per endpoint, per-attempt caps, unknown-quota permission | A loopback local only; B plus one authenticated subscription CLI; C plus one metered remote API | **A first**; add B/C only by naming each endpoint, exposure ceiling and caps in the signed policy | Any live task execution, live review, live campaign, any non-loopback provider client |
| INPUT-3 ([R2](wp-m5-r2-independent-review-acceptance-ewp.md#owner-input-3--acceptance-policy-content-and-independence-basis)) | Independence basis, required dimensions per change class, blocking severities, validity | A `model_family_account`; B `endpoint_model`; C human-adjudicated acceptance for single-actor setups (own EWP) | **B**, with the proposed per-class dimensions in R2; A when two families exist | R2 Part C activation, i.e. any enabled `accept`; with one actor, acceptance is impossible by design |
| INPUT-4 (this window) | Scope and target of the M4 re-evaluation: which capability tiers must run, which repository the seeded-defect fixtures target | A all three tiers; B local plus one other; C available authorized tiers only with explicit missing-tier limitations | **C**: plan only authorized, available tiers and report the rest as limitations (WP-M5-5 already refuses all-tier claims); decide the target source/fixture repository when R3-D starts | Cards R3-C/R3-D, the campaign plan and authorization, the live A9 scenario |

If the owner chooses a different mechanism for INPUT-1 (for example C or D), R4 Part A is unchanged; only Part B's `Signer` and the enrolment procedure change, and the owner must still provide a location the verifier's identity cannot write.

## Facts, assumptions, inferences, unknowns

**FACT** (from the Implementation records and bounded scouts at the authoring base):

- Facade ports `TaskExecutor`, `ReviewExecutor`, `SnippetWorker`, `Investigator` exist; nil ports deny `MODEL_UNAVAILABLE`; `accept` is hard-disabled with `NEEDS_PRINCIPAL`/`acceptance-runtime-unavailable` (the earlier label `ACCEPTANCE_NOT_IMPLEMENTED` does not exist in the repository).
- `ApplyBatch` with `WorkPackageGuard`, `BatchGuard` pre/postconditions and `BatchReadView{ProjectState, Record}` exists; `WorkPackageProposed` and the drift observer exist.
- Discovery has a read side and `DiscoveryWrite` denials (`NEEDS_HUMAN` for human-changing tools); `principalhosts` plans and verifies but applies manually; `ApprovalVerifier` has no implementation.
- `internal/benchmark/empirical` has strict admission and `ReplayGate`; production authority is `denyAuthority`; no verifier, session artifact, plan-level verifier pinning or CLI wrapper exists.
- Worktree manager, controlled process runner, validation `ExecuteAndRecord`, compiler, `MeteredDriver`, `ScopedToolMediator` exist. No repository commit function, no real session client, no write tool, no execution policy exist.

**ASSUMPTIONS** (each tested at step 0 of the owning EWP): duplicate record-key insertion semantics (R4 does not rely on them); `BatchReadView` has one in-repo implementation; the record-kind registry accepts new kinds additively; build info exposes `vcs.revision` in the owner's build; a local runtime reports model revision and usage.

**INFERENCE:** M5 cannot close, and tasks cannot be self-hosted, until R1–R4 are accepted, R3-C/R3-D exist, and the live re-evaluation runs and is adjudicated. The mechanism choice in INPUT-1 decides whether any human-authority feature can ever be enabled on this machine.

**PREFERENCE:** smallest correct slice first (loopback local, receipts before policies, provenance before executor), additive records over projection changes, relaunch over runtime mutation.

**UNKNOWNS / owner inputs:** INPUT-1..4 above; the target repository for seeded-defect fixtures (U1); whether the owner will accept single-tier (local-only) M4 evidence; macOS versus Linux for the protected directory path.

## Documentation/schema synchronization

This change adds planning links only. Owning syncs per EWP at implementation: PROTOCOLS (`InvocationProvenance`, `AcceptanceEvidence`, `ReceiptConsumption`, receipt and policy documents), PROJECT_STATE (records, acceptance evidence), MCP_API (delegate/validate/review/accept availability and denial refs), PRINCIPAL_HOSTS (approval verifier), SECURITY §17 (bootstrap posture and the protected-ingress procedure), REVIEW_AND_CONVERGENCE (cross-reference to the M5 minimal path and M7 ownership), `schemas/README.md` and new schemas/fixtures, IMPLEMENTATION_PLAN M5 status, WORK_PACKAGES. Status prose must distinguish implemented, unavailable and manually verified. No INVARIANTS/catalog edit is planned; if a later authorized amendment changes either, `make update-goldens` and the regenerated digest fixture belong in the same commit.

## Review and freezing

Review input is an immutable snapshot of this overview and the four EWPs with file hashes and the base SHA. Two clean reviewers in parallel: Architecture/Contract and Implementability/Failure Semantics. Consolidate material findings into **one repair round**, then focused independent verification of changed obligations. Authors never establish their own PASS. If blockers remain, keep draft and record unresolved items.

Round 1 happened on the r1 snapshot. Outcome as reported to the author: Architecture/Contract returned APPROVE_DRAFT with five findings to fix before any Part freezes (receipt binding for the campaign authorization, empirical layering, actor-basis representability, policy revocation consistency, `ChangeAccepted` field sources) plus non-blocking items; Implementability/Failure Semantics returned REJECT as implementation-ready, chiefly for missing types, enums and wiring (collaborator interfaces, failure-summary enum and post-failure task state, accept wiring, the loopback protocol and verifier rules, the receipt subject table). r2 closes those findings by repository-grounded specification or by explicit owner inputs; the per-finding closure record is the changelog of each EWP and the report accompanying the repair. **The verdict rows below stay PENDING: no reviewer has seen r2.**

| Lens | Candidate | Verdict | Repair / verification |
| --- | --- | --- | --- |
| Architecture/Contract | r1 reviewed (APPROVE_DRAFT with fixes, see above); r2 is the repaired candidate | PENDING re-verification of r2 | Repair round 1 applied in r2; focused re-verification not yet run |
| Implementability/Failure Semantics | r1 reviewed (REJECT as implementation-ready, see above); r2 is the repaired candidate | PENDING re-verification of r2 | Repair round 1 applied in r2; focused re-verification not yet run |

Nothing in this window is approved. All four EWPs remain NOT_READY; Part-level gates require resolved dependencies, the owner inputs they list, a current-base check and executable validation commands. Missing live hardware, host or credential evidence is disclosed, never replaced by mocked PASS.

## M5 closure checklist (delta over window F)

- R4-A, R2-A, R1-A/B, R2-B/C, R3-A/B accepted with independent review and mutation observation; R4-B and the owner's INPUT-1 mechanism exercised on the real machine (manual operator evidence).
- Acceptance enabled only through a receipt-verified policy and shown on a real candidate with at least two distinct actors, or the single-actor limitation recorded.
- Human-confirming discovery writes (WP-M5-3 write side) and host-plan approval functional through R4 receipts, with replay and revocation proven.
- R3-D profiles reviewed and frozen; R3-C runner accepted; a bounded real campaign passes empirical admission for each authorized available tier; missing tiers and unknown metrics are explicit; Revise/Inconclusive prevents a "proven" claim and triggers a normal repair/policy EWP.
- M5 owner adjudicates scope limitations through the normal change process.

## Changelog

- r2 (2026-10-05): repair round 1 after the two independent reviews: dependency/freeze order corrected (R1-A needs R2-A and R4-A); R1 split into A/B/C; policy rule unified (bytes pinned, authority re-verified per use); receipts discovered by subject with one location; disclosed cross-package amendments listed under Shared schema policy; R4 subject table; R2 acceptance wiring (`AcceptanceGate`, `ReadView`); R3 layering, loopback protocol and verifier rules; readiness tallies made honest. Verdict rows remain PENDING.
- r1 (2026-10-05): initial window; four EWPs split into separately freezable Parts; WP-M5-5 amendments and one `BatchReadView` amendment disclosed; four owner inputs with safe-default deny.

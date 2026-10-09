# Rolling Planning Window 2026-10-G — M5 runtime completion

## Identity and status

- Revision: 7 (Window G progress update: M5-R4-A, M5-R1-A/B/C, M5-R2-A/B, M5-R3-0 delivered and merged in PR #85; M5-R3-A and M5-R3-B delivered in PR #86; M5-R2-C remains pending OWNER INPUT-3, and M5-R4-B remains pending OWNER INPUT-1; M5-R3-C and M5-R3-D remain open scope); authoring base: `71bdaec6d6d81c1b6e52d8b30f0f8485f928925a` (`main` `cebb4f0`, merged PR #85; 2026-10-05).
- Branch: `feat/m5-r3-provider-composition` (PR #86).
- Status: **IN PROGRESS / RECONCILED**. Delivered in PR #85: R4-A, R1-A/B/C, R2-A/B, R3-0. Delivered in PR #86: R3-A (loopback provider composition) and R3-B (independent empirical verifier, authority adapter, admission wiring, CLI replay). R2-C remains pending OWNER INPUT-3, and R4-B remains pending OWNER INPUT-1. R3-C and R3-D remain the open scope of R3.
- Source of scope: the "Required follow-on window" of [window F](window-2026-10-f-overview.md#required-follow-on-window-explicit-scope-cards-not-implementation-authority) and the blocked items in the Implementation records of [WP-M5-3](wp-m5-3-discovery-ewp.md), [WP-M5-4](wp-m5-4-host-integration-ewp.md) and [WP-M5-5](wp-m5-5-empirical-campaign-ewp.md). M4's synthetic caveat and the mandatory real re-evaluation remain binding ([IMPLEMENTATION_PLAN M5](../IMPLEMENTATION_PLAN.md#m5--semantic-principal-integration-and-host-portability)).

## Priority overlay — SELF_HOST_ALPHA (owner decision 2026-10-09)

This original window remains the dependency/contract reference for M5-R1..R4, **not** a requirement to finish all of empirical M5 before DevCadence can code. The execution priority is now [WP-M5-SH1](wp-m5-sh1-self-host-alpha-ewp.md): one real Ollama-backed coding loop, bounded edits, local validation, truthful reviewer or manual-review fallback, Antigravity host path, then one DevCadence self-change. This is an additive owner-approved **sequencing revision**, not retroactive completion of this window's draft/freeze gates.

| Priority | Work | Alpha gate |
| --- | --- | --- |
| P0 | R1 executor and bounded editing (R1-D) + R3-A loopback composition | real model creates commit |
| P0 | R2 independent review/repair or explicit manual-only limitation; R4 local approval hand-off | candidate tested and awaits human integration |
| P0 | Antigravity semantic MCP dogfood on fixture then DevCadence | SELF_HOST_ALPHA evidence |
| P1 (post-alpha) | R3-B empirical verifier hardening, R3-C runner, R3-D profiles/corpus, real M4 benchmark rerun | EMPIRICAL_VALIDATED, not alpha |
| P2 | broader host portability, automated integration, M6 capability-pack breadth, advanced reviews/analytics | product maturity |

Experimental project-level YOLO may explicitly permit unconfined local subprocesses without a sandbox, **only with a visible unsafe label and without bypassing spend, model identity, artifact integrity or manual merge**. Default strict policies remain unchanged. Never interpret this as a production security guarantee.

## Objective and decomposition

Window F delivered the semantic boundary with every runtime absent by design. This window specifies the runtime that fills it, without delegating unresolved architecture: a native task executor, an independent review executor with an enforceable acceptance gate, an empirical verifier with provider composition, and the protected operator ingress that three merged-but-denied features wait on.

| EWP | Owns | Dependencies | Independent acceptance |
| --- | --- | --- | --- |
| [M5-R4](wp-m5-r4-protected-operator-ingress-ewp.md) | Receipt format, protected trust anchors, one-time consumption, host-plan adapter; issuer ceremony (Part B) | WP-M5-1 batch guards | A same-identity forged receipt, a replay and a swapped anchor all fail closed; no mint surface |
| [M5-R1](wp-m5-r1-native-task-executor-ewp.md) | `ExecutionPolicy`, eligible-endpoint resolver, endpoint binding (A); leaf `execrt` (lock, repository provider, artifact sink), worktree, compiler-admitted session, candidate commit, durable outcomes, cancellation, startup recovery (B); validation (C) | WP-M5-1/2; R2-A and R4-A (both needed already by A); R3 Part 0 (driver usage-knownness) for B | A candidate with lineage exists only via recorded intent and in-scope changes; a crash never replays a model call |
| [M5-R2](wp-m5-r2-independent-review-acceptance-ewp.md) | Invocation provenance and actor derivation (A); independent review executor (B); acceptance policy and transactional activation of `accept` (C) | R1 A/B for B (policy-independent); R1-C, R2-B, R4-A, the `controlplane` read amendments, `principal.CodedError` and the `facade.AcceptanceGate` replacement for C | Acceptance denies without complete, independent, current evidence and is re-verified in its own transaction |
| [M5-R3](wp-m5-r3-empirical-verifier-composition-ewp.md) | Driver usage-knownness amendment (Part 0); loopback (Ollama native) provider composition (A); pinned independent verifier, campaign authority adapter, `Admitter`, CLI replay (B) | Part 0 first; R1-A types; R2-A; R4-A; leaf `execrt`; merged WP-M5-5 package | Admission can succeed for genuine verified evidence and cannot succeed for any label or fixture |

### Window G Progress and Delivery Status

| EWP / Part | Scope | Status | Notes / Blockers |
| --- | --- | --- | --- |
| [M5-R4-A](wp-m5-r4-protected-operator-ingress-ewp.md) | Operator receipt format, trust anchors, one-time consumption | Delivered (PR #85) | Merged in `origin/main` |
| [M5-R4-B](wp-m5-r4-protected-operator-ingress-ewp.md) | Operator issuer ceremony and signing key custody | Pending | BLOCKED on **OWNER INPUT-1** (operator account & key custody) |
| [M5-R1-A](wp-m5-r1-native-task-executor-ewp.md) | Execution policy, eligible-endpoint resolver, endpoint binding | Delivered (PR #85) | Merged in `origin/main` |
| [M5-R1-B](wp-m5-r1-native-task-executor-ewp.md) | Task executor runtime (`execrt`, worktrees, session admission, candidate commit) | Delivered (PR #85) | Merged in `origin/main` |
| [M5-R1-C](wp-m5-r1-native-task-executor-ewp.md) | Native validation executor | Delivered (PR #85) | Merged in `origin/main` |
| [M5-R2-A](wp-m5-r2-independent-review-acceptance-ewp.md) | Invocation provenance and actor derivation (`internal/actors`) | Delivered (PR #85) | Merged in `origin/main` |
| [M5-R2-B](wp-m5-r2-independent-review-acceptance-ewp.md) | Independent review executor (`internal/reviewexec`) | Delivered (PR #85) | Merged in `origin/main` |
| [M5-R2-C](wp-m5-r2-independent-review-acceptance-ewp.md) | Acceptance policy & transactional acceptance gate | Pending | BLOCKED on **OWNER INPUT-3** (policy content & independence basis); requires G-C1 / G-C2 |
| [M5-R3-0](wp-m5-r3-empirical-verifier-composition-ewp.md) | Driver usage-knownness amendment | Delivered (PR #85) | Merged in `origin/main` |
| [M5-R3-A](wp-m5-r3-empirical-verifier-composition-ewp.md) | Loopback provider composition (`internal/cognition/sessionclients`) | Delivered (PR #86) | Loopback Ollama native client, fail-closed guards |
| [M5-R3-B](wp-m5-r3-empirical-verifier-composition-ewp.md) | Independent empirical verifier, authority adapter, admission wiring, CLI replay | Delivered (PR #86) | Candidate checkout immutability, operator file verifier authority, integer micro-spend, CLI replay |
| [M5-R3-C](wp-m5-r3-empirical-verifier-composition-ewp.md) | Live empirical campaign runner | Open scope | Follow-on card; depends on live endpoint authorization (OWNER INPUT-2) |
| [M5-R3-D](wp-m5-r3-empirical-verifier-composition-ewp.md) | Empirical corpus verification profiles and fixture repositories | Open scope | Follow-on card; 10 corpus task profiles (OWNER INPUT-4) |

Dependency order and freeze units (each Part is a separate Implementation Readiness gate; sizes are chosen to fit one endpoint session):

~~~text
R4-A (verifier, anchors, consumption) ─┬─> R1-A (policy, resolver) ─> R1-B (executor) ─> R1-C (validate)
R2-A (provenance, actor derivation) ───┤          ^                      │                 │
R3-0 (driver usage-knownness) ─────────┘          └─ R3-0 also gates R1-B and R3-A       │
                                       R1-A, R1-B ─> R2-B (reviewer; NO policy dependency, NO Part C dependency: deterministic intent id)
R4-A + R1-C + R2-B + controlplane/facade/principal amendments ─> R2-C (acceptance gate)
R4-B (issuer ceremony; OWNER INPUT-1) ─────> enrolment, any live human authority
R3-A (loopback composition) <── R3-0, R1-A     R3-B (verifier, authority, CLI) <── R2-A, R4-A, execrt (R1-B)
Later cards (not authored): R1-D bounded edit tool; R3-D corpus verification profiles -> R3-C campaign runner -> live M4 re-evaluation
~~~

Parallelism: R4-A, R2-A and R3-0 are mutually independent and independent of everything else, so they freeze first; **R1-A is not independent of R4-A/R2-A** (its resolver compares actor bases defined by R2-A and its policy loader verifies receipts with R4-A) and freezes after both; R1-B waits for R1-A and R3-0, R1-C for R1-B; R2-B waits for R1-A/B only (not for Part C, `BatchReadView.AttemptEvidence`/G-C1 or any acceptance policy: its per-`(attempt, dimension)` intent uniqueness uses the deterministic record id `intent:<attempt_id>:<dimension>` and `BatchReadView.Record`, r5 B1; `AttemptEvidence` remains a Part C acceptance-enumeration need); **R2-C waits for R4-A, R1-C (validation producer), R2-B (review producer)** and the authorized amendments; R3-A needs R3-0 and R1-A; R3-B needs R2-A, R4-A and the `execrt` leaf frozen with R1-B.

### Items deliberately not authored here (explicit scope, no implementation authority)

1. **WP-M5-3 write side** (human-confirming discovery mutations). Its contract exists and stays DRAFT. After R4-A it still needs its own current-base gate and a review-provenance contract that uses R2-A/R2-B; R4 only supplies `HumanReceiptVerifier` semantics (`Verified` + `ConsumeOnce`).
2. **WP-M5-4 automatic apply and native host smoke.** R4-A supplies the host-plan approval verifier; automatic apply additionally needs an exclusive operator-owned writer channel M3 does not have, so manual apply stays the only path. Antigravity/Cursor installed-version smoke remains manual operator evidence.
3. **R3-C campaign runner** and **R3-D corpus verification profiles/fixtures** (see R3). They are needed only for the live re-evaluation.
3a. **R1-D bounded edit tool** (`apply_patch` / exact-range replace with the R1 scope and symlink checks); recommended before practical self-hosting; not required to freeze R1 (see R1 follow-up card).
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

`protocol` (additive records), `events`, `state`, `storage`, `controlplane` stay host/provider independent. New leaf packages: `internal/operator/receipts` (R4; **imports `controlplane`** for the `BatchGuard`/`RecordToStore` it returns, so it is not a pure leaf), `internal/actors` (R2-A; `ActorBasis` is defined in `internal/protocol` and `actors.ActorBasis` is an alias), `internal/execpolicy` (R1-A; imports `actors`, `receipts`, `principal`), and the execution-runtime leaf `internal/execrt` (R1-B: `ProjectLock`, `RepositoryProvider`, `ArtifactSink`; imports only `repository`, `protocol`, `errs`). Application composition: `internal/taskexec` (R1-B), `internal/reviewexec` (R2-B), `internal/acceptance` (R2-C); they import the facade's ports, never the reverse, and `reviewexec`/`verifier` import `execrt`, never `taskexec`. Provider composition `internal/cognition/sessionclients` and `internal/benchmark/empirical/verifier` (R3) import `execpolicy`, `actors`, `receipts` adapters and `execrt`; `internal/benchmark/empirical` core imports none of them. `internal/principal` (imports only `errs`) gains the exported `CodedError` (SC-2). The MCP SDK stays confined to `internal/mcpadapter`. No package gains a model-reachable surface that mints authority.

### Authority rules resolved once

1. **Human/owner authority is a verified receipt**, never a label: an Ed25519 signature over a typed statement bound to project, purpose, subject, digest and expiry, verified against trust anchors that live where the verifier's own OS identity cannot write (R4). Roots, same-identity files, flags, `AllowedActions`, `PolicyRef` and host tool approval are not authority.
2. **Standing policy: bytes pinned, authority re-verified at every use.** `ExecutionPolicy` (R1) and `AcceptancePolicy` (R2) are files whose bytes are digest-pinned at launch (a changed file denies until relaunch; changing content means a new receipt and a relaunch), but their `grant` receipt, expiry and revocation are re-verified at every `Delegate`, every `Review` start, every acceptance attempt (`AcceptanceGate.Authorize`, outside the transaction) and every campaign run admission (receipts are found by subject, R4; the R4 anchor set is process-pinned while `revoked.json` is re-read at every `Verify`). Revocation or expiry therefore stops the next start with no relaunch; an attempt already started ends under its own caps and wall time. No new projection field and no mutable runtime toggle.
3. **Executor-private state is never authority (DCI-160).** The executor's lock file and handle map decide only liveness; every policy-significant outcome is a typed control-plane event; recovery closes orphaned attempts as failed and never replays or fabricates success.
4. **Nothing degrades silently.** Missing policy, endpoint, cap, receipt, anchor protection, independent reviewer or evidence denies with a fixed code and zero effects. Zero cap is no authority. No fallback to another endpoint, tier or metered path (DCI-122).
5. **Independence is derived.** `ActorID` is computed by DevCadence (`internal/actors`) from the bound endpoint basis (provider, family, account, endpoint, model and observed model revision, all carried by `ResolvedEndpoint`, which the R1 resolver alone derives); every `InvocationProvenance` stores the full six-field basis, the worker's is recorded under the fixed `endpoint_model` at `AttemptStarted`; reviewers are resolved by excluding the worker's **stored basis** (tuple comparison, not actor-id hashes); the review executor uses an explicit configured basis (no policy dependency) and the acceptance gate **re-derives both worker and reviewer actors from stored bases under the policy basis** before applying the existing `ActorsIndependent` rule. A model never names itself.
6. **Acceptance stays separate from integration.** Enabled acceptance records `ChangeAccepted` with a deterministic `AcceptanceEvidence` record and never merges or advances accepted source.

### Shared contracts resolved in r3 (Phase 1)

Each is a contract later text is built on; the owning EWP holds the exact types.

| ID | Contract | Owner / exact text |
| --- | --- | --- |
| SC-1 | Reviewer exclusion passes `ExcludeBases []actors.ActorBasis` (the worker's stored basis); pre-bind comparison is a tuple comparison (`(EndpointID, ModelID)` under `endpoint_model`; `(Provider, ModelFamily, AccountRef)` with unknown = drop under `model_family_account`); post-`Bind` derivation re-checks | R1 Part A types and resolution step 5 |
| SC-2 | Exported `principal.CodedError` / `principal.NewCodedError(code, retryable, refs, detail)` in `internal/principal` (imports only `errs`); `facade.coded` delegates; `facade.semanticFor` maps it via `errors.As`; the `review-partial` counts travel as evidence refs `completed:<n>`, `requested:<m>` | R1 facts + failure mapping; R2 Part B step 7 |
| SC-3 | `OperationRegistry` unchanged: runs yield `completed`/`failed`/`cancelled`; `lost` only for unknown/stale handles; EWP expectations restated, no registry amendment | R1 Algorithm: failure and cancellation; A9 |
| SC-4 | `OpenedEndpoint{Driver, Observed EndpointObservation}`: `Open` is metadata-only and allocates nothing needing cleanup; the callers (R1 Delegate step 4, R2-B Review step 3) assert `Observed.DriverID == Driver.ID()` (`driver-id-mismatch`, r5) because the pure `Bind` cannot, and `Bind` requires/binds driver id, model revision and runtime version **before** `BindingDigest`; every `EndpointBinding` field therefore has an exact source (`DriverID`, `ChannelID`, `RuntimeVersion`, `ContextProfileDigest`, `PolicyDigest`, capability class) | R1 Part A; R3 Part A `Open`/`BindingFor` |
| SC-5 | Usage knownness: `TokenMeasurement{Known,Value}` with zero-value = unknown and explicit `KnownZeroUsage()` accumulator identity; `KnownZeroUsage()` is used only when no driver generation request was made, while an invoked turn that returns no usage stays unknown (r6); unprovable limits are explicit in `MeterSnapshot.UnprovableLimits`; limits are enforced per metric and unknown never proves a budget | R3 Part 0; R1 R9 and `ExecutionLimits` |
| SC-6 | Worker provenance: recorded at `AttemptStarted` under fixed `endpoint_model` with the full six-field `Basis`; every gate/verifier resolves the record and re-derives actor ids from the stored basis under its own basis before `ActorsIndependent` | R2 Part A re-derivation rule, Part B step 2, Decide steps 4-5; R3 Part B step 4; R1 Delegate step 6 |
| SC-7 | Review intent is durable **before reviewer effect**: one registered/replayable `ReviewInvocationStarted` durable-fact event (explicit no-op reducer case, r6) + deterministic-key `ReviewInvocationIntent` per `(attempt, dimension)` precedes every model/session call; every terminal outcome is `ReviewCompleted` + `ReviewInvocation`; orphan intent blocks re-invocation/acceptance; no retry-until-pass | R2 Part A event contract, R5/R7, Part B steps 4-7, Decide step 5 |
| SC-8 | R4 trust: `anchors.json` pinned per process, `revoked.json` re-read fresh each `Verify`; `HumanActorID` bound to the anchor; `OperatorUID` + `TrustedOwnerUIDs = {0, OperatorUID}`; concrete Linux/macOS ACL probes through the stated OS/process primitives | R4 R2/R5/R10, protection check, A17-A21 |
| SC-9 | One policy authority read per start: `Delegate`/`Review` call `PolicySource.Current` exactly once and pass the resulting verified `ExecutionPolicy` + digest into `EndpointResolver.Resolve`; routing never re-reads authority and therefore cannot use a different snapshot from the attempt/review budget decision | R1 Part A/Delegate; R2 Part B steps 2-3 |

### State and failure semantics

Intent before effect: `TaskDelegated`+`AttemptStarted` commit in one guarded batch before the worktree and model call. Every other write is a typed event in a guarded batch with fresh expected prefix. `ReceiptConsumption` commits in the same transaction as the effect it authorizes; no guard or in-transaction callback performs filesystem, subprocess, network, verifier or model work (r5). Evidence completeness for acceptance is enumerated inside the acceptance transaction by one additive `BatchReadView.AttemptEvidence` method plus an additive `Service.ReadView` (explicit amendments of WP-M5-1, R2 G-C1; Part C only), while the acceptance policy receipt is verified by `AcceptanceGate.Authorize` **outside** the transaction and `Decide` is pure inside it (r5; likewise `receipts.Verify` runs before a batch is built and `ConsumeOnce`'s guard is a pure `view.Record` replay check), and the unused `facade.CandidateGate` seam is replaced by `facade.AcceptanceGate` (R2 G-C2). The task stays `running` after a failed attempt (reducer fact), so a retry is `AttemptStarted` alone (R1). Ambiguous commit responses require a state/record lookup before any retry; retries never repeat external effects. Documented process-scoped operations (WP-M5-2) stay process-scoped, and their statuses are only what `OperationRegistry` produces (`completed`/`failed`/`cancelled`; `lost` only for an unknown or stale handle). Review invocations are bounded and auditable: a durable `ReviewInvocationStarted` + `ReviewInvocationIntent` commits before every reviewer call, and every terminal outcome is a durable `ReviewCompleted` plus `ReviewInvocation`; one intent per `(attempt, dimension)`, enforced by the deterministic intent record id `intent:<attempt_id>:<dimension>` checked in the batch guard (r5), means crash or terminal-persistence loss leaves an orphan intent that blocks re-invocation and acceptance rather than enabling pass-shopping. Before any durable `TaskDelegated`+`AttemptStarted`, no prompt, source, tool content, generation, session, credential-bearing request or spend occurs; one bounded metadata-only identity probe is permitted.

### Shared schema policy

New durable records are additive kinds (`ReceiptConsumption`, `InvocationProvenance`, `ReviewInvocationIntent`, `ReviewInvocation`, `AcceptanceEvidence`) plus additive event `ReviewInvocationStarted`, with versioned schemas/fixtures under ADR-0003; no existing payload, record or wire schema is loosened. Edits to existing types are explicit and disclosed, and each needs window-level review before its Part freezes: (1) R3 amendments to the unissued WP-M5-5 plan/run/authorization types (`VerifierSourceCommit`, `VerificationProfileDigest`, `SessionEvidenceDigest`, integer `MaxAPISpendMicroUSD` replacing the float, `OperatorAuthority.VerifyAuthorization` now returning an `AuthorityWindow`, `SessionEvidence` in `empirical`); (2) `controlplane`: `BatchReadView.AttemptEvidence` and `Service.ReadView` (additive; Part C only); (3) `facade`: `CandidateGate` (unused) replaced by `AcceptanceGate` (`Authorize` outside any transaction, pure `Decide`), `Options.Acceptance`, additive `AcceptResult` fields; (4) R1-A `EndpointRequest.ExcludeBases/IndependenceBasis`, policy-snapshot `EndpointResolver.Resolve`, `ResolvedEndpoint.Provider/ModelFamily/AccountRef/RuntimeVersion/DriverID`, `EndpointObservation`, metadata-only `OpenedEndpoint` and per-call `ExecutionLimits` (new types, consumed by R2-B and R3-A); (5) `receipts.Request.ReceiptID` optional with subject lookup; (6) **`principal.CodedError`** exported typed semantic error and evidence-ref carrier (SC-2; `facade.coded` becomes a wrapper; `facade.semanticFor` honours it); (7) **driver evidence contract** `drivers.TokenMeasurement`/`TokenUsage` knownness, `KnownZeroUsage` accumulator identity, per-metric enforcement, `MeterLimits.AllowUnknownUsage` and additive `MeterSnapshot.UsageKnown`/`UnprovableLimits` (R3 Part 0, back-compatible wire form; no existing schema, golden or fixture changes); (8) additive `ActorBasis` type in `internal/protocol`; (9) leaf package `internal/execrt`; (10) R4's macOS ACL implementation reads `internal/process` runner semantics explicitly (`/bin/ls -lde`, pure parser).

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

This change adds planning links only. Owning syncs per EWP at implementation: PROTOCOLS (`InvocationProvenance`, `ReviewInvocationIntent`, `ReviewInvocation`, `ReviewInvocationStarted`, `ActorBasis`, `AcceptanceEvidence`, driver usage knownness (`TokenMeasurement`), `principal.CodedError`, `ReceiptConsumption`, receipt and policy documents), PROJECT_STATE (records, acceptance evidence), MCP_API (delegate/validate/review/accept availability and denial refs), PRINCIPAL_HOSTS (approval verifier), SECURITY §17 (bootstrap posture and the protected-ingress procedure), REVIEW_AND_CONVERGENCE (cross-reference to the M5 minimal path and M7 ownership), `schemas/README.md` and new schemas/fixtures, IMPLEMENTATION_PLAN M5 status, WORK_PACKAGES. Status prose must distinguish implemented, unavailable and manually verified. No INVARIANTS/catalog edit is planned; if a later authorized amendment changes either, `make update-goldens` and the regenerated digest fixture belong in the same commit.

## Review and freezing

Review input is an immutable snapshot of this overview and the four EWPs with file hashes and the base SHA. Two clean reviewers in parallel: Architecture/Contract and Implementability/Failure Semantics. Consolidate material findings into **one repair round**, then focused independent verification of changed obligations. Authors never establish their own PASS. If blockers remain, keep draft and record unresolved items.

Round 1 happened on the r1 snapshot. Outcome as reported to the author: Architecture/Contract returned APPROVE_DRAFT with five findings to fix before any Part freezes (receipt binding for the campaign authorization, empirical layering, actor-basis representability, policy revocation consistency, `ChangeAccepted` field sources) plus non-blocking items; Implementability/Failure Semantics returned REJECT as implementation-ready, chiefly for missing types, enums and wiring (collaborator interfaces, failure-summary enum and post-failure task state, accept wiring, the loopback protocol and verifier rules, the receipt subject table). r2 closes those findings by repository-grounded specification or by explicit owner inputs; the per-finding closure record is the changelog of each EWP and the report accompanying the repair. The verdict table below records the r1 and r2 outcomes; r2 verification is described in the subsection after the table.

| Lens | Candidate | Verdict | Repair / verification |
| --- | --- | --- | --- |
| Architecture/Contract | r1 (c2ce5e3): APPROVE_DRAFT with fixes. r2 (e91f365): not re-reviewed by this lens | APPROVE_DRAFT (on r1, c2ce5e3) | Repair round 1 applied in r2; this lens did not re-review r2 |
| Implementability/Failure Semantics | r1 (c2ce5e3): REJECT. r2 (e91f365): focused re-check | REJECT on r1 (c2ce5e3); REJECT (narrow) on r2 (e91f365) with 5 open blocking items | Repair round 1 spent; five blockers remain UNRESOLVED (below) |
| Owner review (Codex) | r2 head 106dafd | NOT READY to freeze; 5 confirmed + 9 additional blocking findings, 4 before-live-smoke items | r3 closed them in text; full clean re-review by all three lenses PENDING |
| Owner independent review (Antigravity), PR #84 comment 6030903787 | r4 head `fc03164` (base `2c0a3d45`, CI success) | NEEDS_TARGETED_REPAIR: 1 BLOCKER (B1), 5 IMPORTANT (I1-I5) | r5 (this revision) closes them in text (T58-T63); the owner's re-review verdicts are PENDING and are the owner's alone |

Nothing in this window is approved. All four EWPs remain NOT_READY; Part-level gates require resolved dependencies, the owner inputs they list, a current-base check and executable validation commands. Missing live hardware, host or credential evidence is disclosed, never replaced by mocked PASS.

### Status after repair round 2 (r3)

r2 verification (head e91f365) returned REJECT (narrow) with five blocking items; the owner's review of head 106dafd (comment 6006619681) confirmed them and added nine more blocking findings, four before-live-smoke items and a repair order. **Repair round 2 (r3) closed the original owner-review items; focused repair r4 closes the second-order gaps found by independent review of r3. Nothing in r4 is independently verified yet.** No Part may freeze before the full clean re-review of the repaired snapshot (Architecture/Contract/Authority, Implementability/Failure Semantics, Test Adequacy/Mutation), which is **PENDING**. All four EWPs remain **DRAFT / NOT_READY / NOT FROZEN** and carry **no implementation authority**. Items that remain open or became owner input are marked in the table. Safe default for every OWNER INPUT remains deny.

### Repair round r5 (owner independent review of r4 head `fc03164`, comment 6030903787)

The owner's review returned **NEEDS_TARGETED_REPAIR** (one BLOCKER, five IMPORTANT findings). Repair round r5 addressed the review findings; r6 adds two verification-derived tightenings (explicit reducer handling for the new intent event and conservative unknown usage after an invoked failed turn). The findings are **CLOSED in r5/r6 text, pending clean independent re-review**. Nothing here is a freeze decision, and no Part is marked freeze-ready by the author.

| Finding | Closed in r5 text by | Where |
| --- | --- | --- |
| B1 [BLOCKER] intent uniqueness not transactionally enforceable | deterministic intent record id `intent:<attempt_id>:<dimension>` (verified valid under repository id rules: no record-id validator in `protocol`/`storage`/`controlplane`, colons already used by this window's ids, 53 bytes against `MaxIDBytes` 128; the schema MUST permit `:`); `intentAbsentGuard` via `view.Record`; R2-B no longer needs G-C1; scenario B13 | R2 Part A id-relationships, Part B steps 1/4/6, G-C1 text, failure tables, B13; overview graph and parallelism |
| I1 [IMPORTANT] macOS ACL `@` masking; verification under the write lock | `/bin/ls -lde -- <path>` + pure `parseDarwinLsACL` (one line only; `@` accepted solely when no ACE lines); `Verify` before building the batch, guard is a pure `view.Record` replay check; R2-C `AcceptanceGate.Authorize` (outside) + pure `Decide`; A21/A21b/A22, C15 | R4 protection check step 4, consumption algorithm; R2 Part C |
| I2 [IMPORTANT] DriverID binding integrity | explicit `opened.Observed.DriverID == opened.Driver.ID()` assertion (`driver-id-mismatch`) before `Bind` in R1 Delegate step 4 and R2-B Review step 3; mutation rows; A18 and B14 | R1 steps/tables/A18; R2 step 3, B14; R3 Part A `Open` |
| I3 [IMPORTANT] token knownness poisoning, unprovable limits undefined | named `RecordOperationEnd` call sites; accumulators use `KnownZeroUsage()` identity, but any turn method that was actually invoked and returns no usage remains unknown (r6); streams use `sawUsage`; `MeterSnapshot.UsageKnown` and closed `UnprovableLimits []string` flow into artifacts/`SessionEvidence`; P0-1..P0-11 | R3 Part 0; R1 Delegate run step 6 |
| I4 [IMPORTANT] intent/terminal field equality | Decide step 5 field-by-field equality (binding digest, reviewer basis, independence basis, ids, candidate); scenario C17 with isolated per-field cases | R2 Decide step 5, C17 |
| I5 [IMPORTANT] replay test passes with in-memory state | scenario A8b (consume, close, fresh service on the same SQLite file, replay `receipt-replayed`) | R4 A8b, mutants |

Per-Part delivery and review status:
- **R4-A**: Delivered in PR #85 (merged in `origin/main`).
- **R4-B**: PENDING; blocked on **OWNER INPUT-1** (operator account & key custody).
- **R1-A, R1-B, R1-C**: Delivered in PR #85 (merged in `origin/main`).
- **R2-A, R2-B**: Delivered in PR #85 (merged in `origin/main`).
- **R2-C**: PENDING; blocked on **OWNER INPUT-3** (acceptance policy content, change class dimensions, and independence basis); requires G-C1 and G-C2.
- **R3-0**: Delivered in PR #85 (merged in `origin/main`).
- **R3-A, R3-B**: Delivered in PR #86 (`feat/m5-r3-provider-composition`).
- **R3-C, R3-D**: Open scope of R3 (follow-on cards for live campaign execution and corpus profiles).

Author tallies after r6 (self-counts, recounted from the tables; not evidence): R1 22 scenarios (A1-A22), 25 mutation rows; R2 37 scenarios (A1-A6, B1-B14, C1-C17), 30 mutation rows; R3 Part 0 11 scenarios (P0-1..P0-11) with a mapped mutant each, 20 numbered A scenarios; R4 24 scenarios (A1-A21, A8b, A21b, A22), 24 mutation rows.

**Unverified environment observation (not investigated here):** the owner reported that `make verify` in his clean worktree showed pre-existing timeouts in `internal/mcpadapter` and a smoke failure in `internal/principalhosts`, while CI on `fc03164` passed all workflows. This pull request is documentation-only and cannot have caused them; they are recorded as an unverified environment observation, and r5 does not claim the cause. See the r5 repair report for what `make verify` showed in the author's environment.

### Review traceability (owner review 6006619681)

Dispositions: **CLOSED r3/r4** = specified in the named repair revision, pending clean re-review; **OWNER INPUT** = needs an owner answer (deny by default); **LATER GATE / LATER CARD** = an ordered gate or card with the stated entry condition, not done; **OPEN** = unresolved (reason given). "Owner item" numbers are those of the comment.

| # | Owner item | Disposition | Location |
| --- | --- | --- | --- |
| T1 | Overall assessment: agree DRAFT/NOT_READY/NOT FROZEN; do not freeze or implement any Part; CI green | CLOSED r3 (status kept) | this overview Identity and status; every EWP Status line |
| T2 | Confirmed blocker 1 — actor exclusion not representable (`ExcludeActorIDs`) | CLOSED r3 | SC-1; R1 Part A `EndpointRequest.ExcludeBases`, resolution step 5, A20; R2 Part B step 3, B8 |
| T3 | Confirmed blocker 2 — semantic codes/refs not constructible outside `facade` | CLOSED r3 | SC-2; Shared schema policy (6); R1 facts row, failure mapping, A21; R2 Part B step 7, Decide denials |
| T4 | Confirmed blocker 3 — `lost / OPERATION_LOST` not producible | CLOSED r3 (restated; no registry amendment) | SC-3; R1 failure and cancellation, A9, Part C |
| T5 | Confirmed blocker 4 — `BindingFor` field sources | CLOSED r4 (r3 left `DriverID` unrepresentable; r4 binds it from the actual opened driver) | SC-4; R1 Part A `EndpointObservation`/Binding; R3 Part A `Open`/`BindingFor`, A11 |
| T6 | Confirmed blocker 5 — worker provenance reconstruction incomplete | CLOSED r3 | SC-6; R1 Delegate step 6, A14; R2 Part A re-derivation rule, Part B step 2, Decide steps 4-5, C16; R3 Part B steps 2 and 4, A7 |
| T7 | Item 6 — "zero driver calls" vs pre-batch `Open` | CLOSED r3 | R1 R1/R2, Delegate, failure tables, A2/A5 (spy: no session/model invocation; prefix not refreshed); overview State semantics |
| T8 | Item 7 — policy used before loading; sync order | CLOSED r3 | R1 Delegate steps 1-8, A16 |
| T9 | Item 8 — `OpenedEndpoint` cleanup semantics | CLOSED r3 (metadata-only; no Close) | SC-4; R1 Part A `OpenedEndpoint` contract, A18; R3 Part A `Open` |
| T10 | Item 9 — R2-B hidden dependency on acceptance policy | CLOSED r3 | R2 R3, Part B `Options.IndependenceBasis` and explicit dimensions, B9; dependency graph and parallelism above |
| T11 | Item 10 — reviewer pass-shopping | CLOSED r4: r3 closed normal terminal paths; r4 closes crash/persistence-loss with pre-call durable intent | SC-7; R2 R5/R7, `ReviewInvocationIntent`, Part B steps 1/4-7, B7/B11, C13 |
| T12 | Item 11 — unknown token usage vs driver contract | CLOSED r4: r3 introduced knownness; r4 fixes accumulator identity, partial-known Ollama usage and per-limit knownness | SC-5; R3 Part 0 P0-1..P0-11 (r5); R1 R9/A8/A22 |
| T13 | Item 12 — R4 revocation vs digest pinning | CLOSED r3 | SC-8; R4 R5, protection check step 5, Verify steps 2-3, A9, A18 |
| T14 | Item 13 — bind `HumanActorID` to the anchor | CLOSED r3 | R4 R10, Verify step 4, A19 |
| T15 | Item 14 — ownership path-chain and ACL per OS | CLOSED r3 (OS probes fixed for Linux and macOS; other OS refuses) | R4 R2, `FileOptions.OperatorUID`, protection check steps 3-4, A20/A21 |
| T16 | Before-live-smoke 1 — R2 paid-call crash accounting | CLOSED r4 mechanically: intent commits before any reviewer call for every endpoint class; orphan intent blocks retry/acceptance | R2 R5/R7, Part B steps 4-7, B11/C13 |
| T17 | Before-live-smoke 2 — review worktree ids after partial failure | CLOSED r3 | R2 R11, Part B step 4, B10 |
| T18 | Before-live-smoke 3 — bounded `apply_patch`/range-replace tool | LATER CARD R1-D (recorded; not required to freeze R1) | R1 follow-up card; this overview item 3a |
| T19 | Before-live-smoke 4 — leaf execution-runtime package | CLOSED r3 | Dependency direction paragraph; R1 Part B `internal/execrt`; R2 Part B Options; R3 Part B Options |
| T20 | Phase 1 step 1 — explicit excluded actor bases | CLOSED r3 | = T2 |
| T21 | Phase 1 step 2 — exported semantic-error/ref mechanism | CLOSED r3 | = T3 |
| T22 | Phase 1 step 3 — resolve `OperationRegistry` expectations | CLOSED r3 | = T4 |
| T23 | Phase 1 step 4 — `OpenedEndpoint` lifecycle and all `EndpointBinding` sources | CLOSED r4 | = T5, T9 |
| T24 | Phase 1 step 5 — usage knownness and enforceable pre-call bounds | CLOSED r4 | = T12 |
| T25 | Phase 2 step 6 — split pinned anchors from reloadable revocations | CLOSED r3 | = T13 |
| T26 | Phase 2 step 7 — bind human identity to the anchor | CLOSED r3 | = T14 |
| T27 | Phase 2 step 8 — trusted-owner/path-chain and ACL rules | CLOSED r3 | = T15 |
| T28 | Phase 2 step 9 — re-run R4-A Contract/Authority and Implementability reviews; freeze R4-A only if both pass | LATER GATE; entry: the full clean re-review of this snapshot (T47-T49) passes both lenses | R4 Status; this section |
| T29 | Phase 3 step 10 — freeze actor derivation/provenance representation | CLOSED r3 in text; the R2-A freeze itself is a LATER GATE (entry: re-review pass, R4-A unaffected) | R2 Part A; SC-6 |
| T30 | Phase 3 step 11 — R1 metadata-probe/intent wording, policy ordering, cleanup contract, exact event field sources | CLOSED r3 | = T7, T8, T9; R1 Delegate step 7 field sources |
| T31 | Phase 3 step 12 — verify R1 against a fake driver, then a loopback runtime with no live remote credentials or spend | LATER GATE; entry: R1-A/B/C frozen and implemented; live loopback run only after INPUT-2 option A; no remote credentials | R1 acceptance scenarios, OWNER INPUT-2 |
| T32 | Phase 4 step 13 — remove hidden R2-B to R2-C policy dependency | CLOSED r3 | = T10 |
| T33 | Phase 4 step 14 — durable/bounded review invocation failure semantics | CLOSED r3 | = T11 |
| T34 | Phase 4 step 15 — worktree identity and cleanup | CLOSED r3 | = T17 |
| T35 | Phase 4 step 16 — freeze G-C1/G-C2; re-check worker and reviewer actor derivation inside the acceptance transaction | CLOSED r3 in text (Decide re-derives both actors, C16); the G-C1/G-C2 freeze is a LATER GATE (entry: amendments reviewed at window level and authorized, re-review pass) | R2 Part C, G-C1/G-C2, Decide steps 4-5, C9/C16 |
| T36 | Phase 4 step 17 — exercise acceptance on a real candidate with two distinct local actors, else record the limitation | LATER GATE / OWNER INPUT-3; entry: R2-C implemented and an INPUT-3 policy with two actors; otherwise the single-actor limitation is recorded, not worked around | R2 OWNER INPUT-3; M5 closure checklist |
| T37 | Phase 5 step 18 — exact endpoint binding/session evidence sources on the frozen driver contract | CLOSED r3 in text (Part 0 precedes Part A); freeze LATER GATE | R3 Part 0, Part A table, `SessionEvidence` |
| T38 | Phase 5 step 19 — worker provenance resolvable and re-derived | CLOSED r3 | = T6 |
| T39 | Phase 5 step 20 — verify Ollama metadata revision and usage behavior empirically on the owner's runtime | LATER GATE; entry: step 0 of R3-A on the owner's runtime (`/api/version`, `/api/tags` digest, `prompt_eval_count`/`eval_count`); a false row escalates | R3 Part A, readiness report |
| T40 | Phase 5 step 21 — freeze R3-A/B only after WP-M5-5 amendments are independently re-reviewed | LATER GATE; entry: re-review of the amendments list in R3 (T47-T49) and Part 0 | R3 explicit amendments |
| T41 | Phase 6 step 22 — author and independently freeze R3-D verification profiles before observations | LATER CARD R3-D; entry: R3-B frozen, INPUT-4 target repository decided | R3 cards; INPUT-4 |
| T42 | Phase 6 step 23 — implement R3-C campaign runner | LATER CARD R3-C; entry: R1, R2-A, R3-0/A/B, R4 accepted; INPUT-2 | R3 cards |
| T43 | Phase 6 step 24 — bounded real M4 re-evaluation; admit only evidence passing R3 | LATER GATE; entry: R3-C, R3-D done, INPUT-1/2/4 answered, campaign receipt | M5 closure checklist |
| T44 | Phase 6 step 25 — adjudicate M5 closure | LATER GATE; entry: all above and owner adjudication | M5 closure checklist |
| T45 | Phase ordering note (one more repair round, this order, built on stable shared contracts) | CLOSED r3 (Shared contracts SC-1..SC-8 placed first; EWPs edited in Phase 1 to 5 order) | Shared contracts section |
| T46 | "Too important to waive contradictions": do not delegate ambiguity | CLOSED r3 (no implementer-invented semantics left known; unknowns are explicit gates) | Implementation Readiness Reports |
| T47 | Requested re-review lens 1 — Architecture / Contract / Authority of the repaired snapshot | PENDING (full clean re-review required; not performed by the author) | Review and freezing |
| T48 | Requested re-review lens 2 — Implementability / Failure Semantics | PENDING | Review and freezing |
| T49 | Requested re-review lens 3 — Test Adequacy / Mutation | PENDING | Review and freezing |
| T50 | Verdict: strong architecture; NOT READY to freeze; support R1-R4 decomposition; repair, do not redesign; comfortable only after issues closed and full snapshot passes independent review | CLOSED r3 as a status (decomposition kept, no redesign); the approving condition is PENDING (T47-T49) | this overview |
| T51 | Earlier note 1 — "Evaluate" vs "Decide" | CLOSED r3 | Authority rule 2; R4 R5; R1 R11 |
| T52 | Earlier note 2 — R2-C waits on R1-C in table and parallelism | CLOSED r3 | Decomposition table; Parallelism paragraph; R2 Dependencies |
| T53 | Earlier note 3 — `AttemptStarted` field sources; "first delegation" = task `ready` | CLOSED r3 | R1 Delegate steps 1 and 7, A16 |
| T54 | Earlier note 4 — review worktree id collision and cleanup | CLOSED r3 | = T17 |
| T55 | Earlier note 5 — R3 exit codes 3 and 4 are subcommand-local | CLOSED r3 | R3 CLI replay |
| T56 | Earlier note 6 — shared lock/provider package; `ActorBasis` home | CLOSED r3 | = T19; R2 Part A (`protocol.ActorBasis`, alias in `actors`) |
| T57 | Earlier note 7 — `receipts` imports `controlplane` | CLOSED r3 | Dependency direction paragraph |
| T58 | r4 review (comment 6030903787) B1 [BLOCKER] — R2-B intent uniqueness not transactionally enforceable without G-C1 | CLOSED r5 in text, pending owner re-review | R2 Part A id relationships, Part B steps 1/4/6, G-C1, B13; overview graph/parallelism |
| T59 | r4 review I1 — macOS ACL `@` masking; verification inside the write lock | CLOSED r5 in text, pending owner re-review | R4 protection check step 4, consumption algorithm, A21/A21b/A22; R2 Part C `Authorize`/`Decide`, C15 |
| T60 | r4 review I2 — DriverID binding integrity unchecked before `Bind` | CLOSED r5 in text, pending owner re-review | R1 Delegate step 4, A18; R2 Part B step 3, B14; R3 Part A `Open` |
| T61 | r4 review I3 — token knownness poisoning; unprovable limits not representable | CLOSED r5/r6 in text, pending clean re-review | R3 Part 0 call-site rule, fail-closed invoked-turn unknown semantics, `UnprovableLimits`, P0-1..P0-11; R1 run step 6 |
| T62 | r4 review I4 — terminal records not compared to intent fields | CLOSED r5 in text, pending owner re-review | R2 Decide step 5, C17 |
| T63 | r4 review I5 — replay coverage process-local only | CLOSED r5 in text, pending owner re-review | R4 A8b, mutation catalog |

The disposition of the previously open review findings is: all are CLOSED in the current r3-r6 text and await the clean verification gate; **no finding is OPEN** and none became a new OWNER INPUT, except the already-declared dependencies of T36 on INPUT-3 and T31/T42 on INPUT-2.

## M5 closure checklist (delta over window F)

- R4-A, R2-A, R1-A/B, R2-B/C, R3-A/B accepted with independent review and mutation observation; R4-B and the owner's INPUT-1 mechanism exercised on the real machine (manual operator evidence).
- Acceptance enabled only through a receipt-verified policy and shown on a real candidate with at least two distinct actors, or the single-actor limitation recorded.
- Human-confirming discovery writes (WP-M5-3 write side) and host-plan approval functional through R4 receipts, with replay and revocation proven.
- R3-D profiles reviewed and frozen; R3-C runner accepted; a bounded real campaign passes empirical admission for each authorized available tier; missing tiers and unknown metrics are explicit; Revise/Inconclusive prevents a "proven" claim and triggers a normal repair/policy EWP.
- M5 owner adjudicates scope limitations through the normal change process.

## Changelog

- r7 (2026-10-08): updated Window G progress and delivery status: marked R3-A and R3-B as Delivered in PR #86 (loopback provider composition, independent empirical verifier with candidate checkout immutability, protected operator file verifier authority, integer micro-spend admission, and replay CLI); documented that R2-C remains pending OWNER INPUT-3 and R4-B remains pending OWNER INPUT-1; marked R3-C and R3-D as remaining open scope of R3.
- r6 (2026-10-06): final verification tightening on top of r5: R2 explicitly registers `ReviewInvocationStarted` as a durable no-op projection event so current fail-closed reducer semantics can replay it; R3 no longer fabricates known-zero usage when `StreamTurn` was invoked and returned an error without usage evidence. No scope or authority expansion; still pending clean independent re-review.
- r5 (2026-10-07): targeted repair after the owner's independent review of r4 head `fc03164` (comment 6030903787, NEEDS_TARGETED_REPAIR): B1 deterministic intent record id and `view.Record` guard (R2-B independent of Part C/G-C1; B13); I1 macOS `ls -lde` pure parser, verification before the batch, `AcceptanceGate.Authorize` outside the transaction (A21b, A22, C15); I2 `driver-id-mismatch` assertion before `Bind` (A18, B14); I3 `RecordOperationEnd` call-site rule, `MeterSnapshot.UnprovableLimits`, scenario-to-mutant table; I4 Decide step 5 field equality (C17); I5 durable replay scenario A8b. Findings recorded as CLOSED in r5 text pending the owner's re-review (T58-T63); status wording per Part: r5 repair applied; freeze decision pending owner re-review. The owner's environment observation about `make verify` is recorded as unverified and not investigated. Still DRAFT/NOT_READY/NOT FROZEN.
- r4 (2026-10-06): focused repair after independent review of r3 head `0da5013`: driver id is now bound from the actual opened driver before endpoint digesting; driver token knownness gets an explicit known-zero accumulator and per-metric semantics; R2 gains durable pre-call review intents that close crash/persistence pass-shopping; resolver policy authority is read once per start and passed as a snapshot; R4 names the `internal/process` dependency used by its macOS ACL probe. Still DRAFT/NOT_READY/NOT FROZEN pending clean re-review.
- r3 (2026-10-06): repair round 2 after the owner review of head 106dafd (comment 6006619681): shared contracts SC-1..SC-8 (`ExcludeBases`, `principal.CodedError`, registry-faithful operation statuses, metadata-only `OpenedEndpoint` and exact `EndpointBinding` sources, driver usage-knownness Part 0, worker provenance re-derivation, durable bounded review invocations, R4 anchor/revocation split); R1 pre-batch invariant restated and Delegate order fixed; R2-B made policy-independent; `internal/execrt` leaf; R1-D follow-up card; traceability table. Not verified; verdict rows remain PENDING.
- r2 verification (head e91f365): focused Implementability re-check REJECT (narrow), five blocking items open; Architecture/Contract not re-run on r2; all EWPs remain DRAFT/NOT_READY/NOT FROZEN. Documentation-only update to this overview.
- r2 (2026-10-05): repair round 1 after the two independent reviews: dependency/freeze order corrected (R1-A needs R2-A and R4-A); R1 split into A/B/C; policy rule unified (bytes pinned, authority re-verified per use); receipts discovered by subject with one location; disclosed cross-package amendments listed under Shared schema policy; R4 subject table; R2 acceptance wiring (`AcceptanceGate`, `ReadView`); R3 layering, loopback protocol and verifier rules; readiness tallies made honest. Verdict rows were PENDING at r2 authoring; see the r2 verification outcome below.
- r1 (2026-10-05): initial window; four EWPs split into separately freezable Parts; WP-M5-5 amendments and one `BatchReadView` amendment disclosed; four owner inputs with safe-default deny.

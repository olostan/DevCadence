# WP-M5-R3 — Empirical verifier and provider composition

## Identity

- Revision: 1; task: task-m5-r3-empirical-verifier-composition; window: [2026-10-G](window-2026-10-g-overview.md).
- Base: `71bdaec6d6d81c1b6e52d8b30f0f8485f928925a` (`main` `cebb4f0` plus the PR #83 reconcile merge, 2026-10-05).
- Contract digest: reviewed immutable Git blob. No fictitious runtime state revision; record the actual accepted dependency commits at execution.
- Endpoint: competent Go implementer with Git, deterministic-test and HTTP-client skill; complete admission is mandatory. Each Part is sized for one endpoint session.
- Status: **DRAFT / NOT_READY / NOT FROZEN. No implementation authority; no endpoint call, credential, spend or live campaign is authorized.** This EWP **amends** the draft [WP-M5-5](wp-m5-5-empirical-campaign-ewp.md) contract at the points listed under "Explicit amendments"; the amendments need window-level review before Part B freezes.
- Dependencies: [M5-R1](wp-m5-r1-native-task-executor-ewp.md) Part A types (`ResolvedEndpoint`, `DriverFactory`) for Part A, and Part B worktree primitives for Part B; [M5-R2](wp-m5-r2-independent-review-acceptance-ewp.md) Part A (`DeriveActorID`, `InvocationProvenance`); [M5-R4](wp-m5-r4-protected-operator-ingress-ewp.md) Part A (`receipts.Verifier`) for the production authority; merged WP-M5-5 admission package.
- Parts (separately freezable): **A** provider composition (`DriverFactory` implementations, first slice loopback local only). **B** independent verifier, authority adapter, admission wiring and CLI replay.

## Objective

Supply what WP-M5-5 explicitly left to a follow-on: the production `IndependentVerifier` (receipt re-execution on an immutable candidate with a pinned profile), the production operator authority (campaign-grant receipts through R4), the missing evidence bindings (session evidence, verifier source and profile pinned in the plan), a CLI that replays the unchanged gate only through admission, and the minimal provider composition so a granted endpoint can actually run a session. Nothing here runs a campaign or spends anything; it makes admission *able to succeed* for genuine evidence and *unable to succeed* for anything else.

## Context Manifest

Role: Go implementer; independent Contract/Authority and Test Adequacy/Mutation reviewers; the verifier author is not the campaign author. Read envelope: `internal/benchmark/empirical/*` (types, plan, admission, replay), `internal/benchmark/{runner,defects,telemetry,gate,experiments}`, `internal/benchmark/corpus` (signatures), `cmd/devcadence/benchmark.go`, `internal/cognition/drivers/{direct_api,types,metering}.go`, `internal/cognition/ollama` (transport conventions), R1 `execpolicy` types, R2 `actors`, R4 `receipts`, `internal/worktrees`, `internal/process`. Write scope: new `internal/benchmark/empirical/verifier/`, `internal/cognition/sessionclients/` (Part A), additive fields in `internal/benchmark/empirical` (amendments), `cmd/devcadence` subcommand `benchmark replay-empirical` and the legacy refusal, schemas/fixtures under `schemas/`, owning docs. No criteria, gate mathematics or invariant change.

Exact clauses: AGENTS §§2–9, 12–15, 17; ADR-0024 §§3–5, 10; WP-M5-5 §§Exact representation, Admission and gate decision semantics, Authority matrix; SECURITY §§3, 5–9, 14–17; DCI-005, 011–014, 032–033, 040–044, 080–084, 090, 100, 120–124, 126, 129, 159–161. Risks: contaminated verifier, forged receipt, flaky deterministic checks, non-hermetic commands, unknown-as-zero, unbounded spend through a client. Re-resolution triggers: gate criteria or corpus change, a new driver kind or credential path, a verifier needing network, empirical type drift, or an owner decision outside OWNER INPUT-2.

## Scope envelope

Authorized: as above. Forbidden: a campaign runner, live execution or fixture authoring (cards R3-C, R3-D below); any credential reading, provider login, remote client or subscription-CLI mapper in the first slice; shell-executing verifier commands; modifying or weakening `EvaluateM4Gate`, criteria, existing snapshot schemas or the synthetic report; a verifier that reads the candidate from the worker's writable worktree; accepting a receipt by digest alone; any bypass of the injected `IndependentVerifier`/authority; a test fake reachable from production composition.

LOCAL_DISCRETION: private helpers, test layout, report typography, HTTP client internals that keep the stated limits.

## Requirements and invariants

| ID | MUST requirement | Invariant |
| --- | --- | --- |
| R1 | The verifier re-executes the pinned deterministic checks on the immutable candidate in a fresh isolated worktree and derives `VerifiedOutcome` from its own execution; the caller receipt is a claim compared field by field | I1: receipt authenticity and independent outcome are distinct checks |
| R2 | The plan pins `VerifierSourceCommit` and `VerificationProfileDigest`; the verifier determines its own source commit from build info (`vcs.revision`, unmodified) and refuses on any mismatch or unknown | I2: the judge is fixed before observations |
| R3 | `VerifiedOutcome.Worker` and `.Verifier` are canonical `ActorProvenance` (R2-A); the verifier identity is non-model and independent under `ActorsIndependent`, with a separate invocation and no lineage overlap | I3: no self-verification |
| R4 | Seeded-defect counts follow the profile's deterministic probe rule: implementation tasks count a defect caught only when all its probe checks pass on the candidate; review tasks only when a strictly parsed finding anchors the defect location | I4: quality is computed, not asserted |
| R5 | A session-evidence artifact (strict schema, digest-bound in `RunEvidence`) records live endpoint/model/config, prompt digest, timestamps, driver outcome and usage with unknown ≠ zero; admission checks authorization expiry against its start time | I5: no run is empirical without direct session provenance |
| R6 | The production operator authority accepts a campaign authorization only through an R4 `empirical.campaign_authorize` grant receipt bound to the authorization digest, project and plan digest | I6: authority is protected, not labeled |
| R7 | Verifier infrastructure failure yields `blocked`, a genuine quality failure yields completed-not-accepted, and original-versus-rerun disagreement yields `blocked` (`INCONSISTENT_VERIFICATION`); none counts as accepted | I7: failures keep their identity |
| R8 | Provider composition opens only a granted, eligible endpoint; the first slice implements loopback local runtimes only; every other kind is `MODEL_UNAVAILABLE` (`driver-not-implemented`) | I8: no ambient provider, no hidden fallback (DCI-122) |
| R9 | `devcadence benchmark replay-empirical` runs Admitter then `ReplayGate`; legacy `evaluate-gate --evidence-kind empirical_campaign` is refused | I9: the caller's label can never establish empirical evidence |
| R10 | Verifier commands are fixed argv arrays from the pinned profile, with a minimal environment, no network and no shell | I10: verification is controlled execution (DCI-033) |

## Verified facts and representability gaps

| Fact / gap | Evidence at pinned base | Implication |
| --- | --- | --- |
| Admission is complete but production `ValidateAdmission` cannot return `Admitted` (`denyAuthority`, nil verifier path) | `empirical/admission.go`, `types.go` | export a constructor taking an authority and verifier |
| `operatorAuthority` is unexported | `empirical/types.go` | **G-1:** an exported `OperatorAuthority` interface and `Admitter` are needed |
| `RunEvidence` has `SessionEvidenceRef` and no digest; session artifact is never resolved; `VerifiedOutcome.SessionDigest` is format-checked only | WP-M5-5 implementation record, blocked item 3 | **G-2:** add `SessionEvidenceDigest` and define the artifact |
| `CampaignPlan` pins no verifier source or profile; `VerifiedOutcome` carries both | `empirical/types.go` | **G-3:** add two required plan fields |
| Authorization `expiry` only checked as RFC3339 | record, blocked item 2 | **G-4:** compare to session start (G-2) and to the receipt window |
| Corpus defects are synthetic file patches with `FileTarget`/`PatchContent`; `DefaultVerificationSuite` simulates outcomes by defect id; no per-defect detector command exists | `benchmark/defects.go`, `runner.go` | **G-5:** a `VerificationProfile` is required; authoring its content for the ten corpus tasks is card R3-D |
| Fixture targets such as `internal/auth/checker.go` may not exist in any pinned source tree | `benchmark/defects.go` | unknown U1: which repository the campaign targets (owner/Principal decision with R3-D) |
| `evaluate-gate` trusts `--evidence-kind empirical_campaign` and has no falsification input | WP-M5-5 §Bounded campaign, record item 4 | R9 replaces the path; no change to gate math |
| Only `DirectAPIDriver` and `CLIWrapperDriver` exist; no real client; `ollama` package is probe-only with an `HTTPTransport` | `cognition/drivers`, `cognition/ollama` | Part A adds one loopback `DirectAPIClient` |
| `telemetry.RunTelemetrySnapshot` has `AccountingUncertain` and no endpoint/model fields | `benchmark/telemetry/metrics.go` | continue to use the sidecar; unknown tokens are zero placeholders with the flag, never comparable |
| `protocol.ActorProvenance` and `ActorsIndependent` exist; empirical already uses them | `protocol/review_ledger.go`, `empirical` | reuse |

Step 0 re-verifies each row; a false row escalates.

## Explicit amendments to the WP-M5-5 draft (all additive; no plan or authorization has ever been issued)

1. `CampaignPlan` gains required `VerifierSourceCommit` and `VerificationProfileDigest` (G-3). `PlanDigest` fixtures are regenerated; no historical plan exists.
2. `RunEvidence` gains required `SessionEvidenceDigest` for completed/failed runs that started a session (G-2).
3. An exported `OperatorAuthority` interface (same method as today) and `Admitter` replace the unexported seam as the only way to obtain `Admitted=true`; the old `ValidateAdmission` keeps its fail-closed behavior and doc comment (G-1).
4. Replay entry point is a new subcommand (R9); the legacy `evaluate-gate` argv in WP-M5-5 is superseded for empirical use.
5. The `m5-m4-empirical-report.md/json` artifact set and the three-way `Report{Admission, RawGate, Conclusion}` are unchanged.

## Part A — Provider composition (first slice: loopback local only)

~~~go
// package sessionclients
type Composition struct{ /* options below */ }
type Options struct {
    LoopbackBaseURLs map[string]string // endpoint id -> http://127.0.0.1:<port> or http://[::1]:<port>, validated
    Clock clock.Clock
}
func New(Options) (*Composition, error)
func (c *Composition) Open(ctx context.Context, ep execpolicy.ResolvedEndpoint) (drivers.SessionDriver, error) // implements execpolicy.DriverFactory
func BindingFor(ep execpolicy.ResolvedEndpoint, contextProfileDigest string) empirical.EndpointBinding
~~~

`Open` dispatches on `ep.Kind`: `local_runtime` builds a `DirectAPIDriver` over a loopback chat client; `authenticated_cli` and `remote_api` return `MODEL_UNAVAILABLE` `driver-not-implemented` (their mappers and clients are added only after OWNER INPUT-2 names an endpoint, as amendments of this EWP). The loopback client implements `drivers.DirectAPIClient.Complete` against the local runtime's non-streaming chat endpoint (`Stream` returns a typed unsupported error). Rules: URL host must be a literal loopback IP (or `localhost` resolved by the client to loopback only, re-checked on the dialed address); no redirects; no proxy; no credentials or auth headers; TLS not required for loopback; request/response size caps (1 MiB / 4 MiB); per-request timeout from the resolved limits; tool definitions map to the runtime's tool schema, tool calls are returned to the mediator and never executed by the client; usage fields are mapped only when the runtime reports them, otherwise `TokenUsage` marks unknown (never zero); the model identity is captured from the runtime's own report (model digest/revision) into `ResolvedEndpoint.ModelRevision`'s source of truth; a runtime that does not report a revision gives revision `unknown` and the empirical binding refuses it (`ModelRevision` is required there). `BindingFor` maps `local_runtime→local_small`, `authenticated_cli→subscription_cli`, `remote_api→frontier_api`, copies ids, sets `SubscriptionQuotaUnit` to literal `unknown` unless the endpoint pins a unit, and takes the context-profile and policy digests from the resolved endpoint.

## Part B — Verifier, authority, admission wiring, CLI

### Types (new unless noted)

~~~go
type CheckSpec struct {
    CheckID string
    Argv []string          // fixed; Argv[0] is an executable name from the profile allow-list, never a shell
    Dir string             // relative to the worktree root, no ".."
    TimeoutSeconds int     // 1..600
    ExpectExitCode int
}
type ReviewAnchor struct { Path string; StartLine, EndLine int }
type DefectProbe struct {
    DefectID string
    CatchCheckIDs []string      // implementation tasks: caught iff all pass on the candidate
    Anchor *ReviewAnchor        // review tasks: caught iff a finding cites "path:line" inside the anchor
}
type TaskVerification struct {
    TaskID, TaskDigest, Class string // "implementation" | "review"
    BaseCommit string
    Checks []CheckSpec
    AcceptanceCheckIDs []string
    Defects []DefectProbe
}
type VerificationProfile struct {
    Version, ProfileID string        // "1.0"
    Executables []string             // allow-list; "sh","bash","zsh","cmd","powershell" refused at load
    Tasks []TaskVerification         // sorted, unique TaskID
}
type SessionEvidence struct {          // artifact schema "empirical-session" 1.0, strict
    Version, RunID, CampaignID, AttemptID, TaskDigest string
    Endpoint empirical.EndpointBinding
    PromptDigest, ContextManifestDigest, InvocationProvenanceDigest, ExecutionPolicyDigest, AuthorizationDigest string
    StartedAt, EndedAt string          // RFC3339 UTC
    DriverOutcome string               // "completed" | "error" | "cancelled" | "limit_reached"
    Turns int
    Usage map[string]empirical.Measurement // the ten-key vocabulary of WP-M5-5; unknown stays Known=false
}
type OperatorAuthority interface { // exported replacement of the unexported seam
    VerifyAuthorization(ctx context.Context, planDigest, authorizationDigest string, authorization []byte) error
}
type Options struct { Authority OperatorAuthority; Verifier IndependentVerifier; Resolver ArtifactResolver; Clock clock.Clock }
func NewAdmitter(Options) (*Admitter, error) // nil Authority/Verifier/Resolver refused
func (a *Admitter) Admit(ctx context.Context, m CampaignManifest) (AdmissionResult, error)
~~~

Profile load validates: strict decode, unique ids, each `CheckSpec` argv non-empty with `Argv[0]` in `Executables`, no shell, no URL-looking argument, no `..` directory; `AcceptanceCheckIDs` and `CatchCheckIDs` reference existing checks; review-class tasks have anchors, implementation tasks have probes. The profile digest is the canonical digest of the whole document and must equal `CampaignPlan.VerificationProfileDigest`. The campaign renderer (R4) prints that digest and the distinct executables across all checks.

### Algorithm: `Verify` (implements `empirical.IndependentVerifier`)

1. Preconditions: run status `completed`; plan, run, evidence internally consistent (admission has already matched ids). Unknown/unavailable inputs return an infrastructure error (`blocked`), never an accept.
2. Resolve and digest-check the receipt, session evidence and candidate artifact via `ArtifactResolver` (no network). Strict-decode the receipt (fields per WP-M5-5 `VerifierReceipt`) and session evidence. Cross-check ids, task digest, seed, strategy, endpoint binding digest, prompt digest, candidate commit and artifact digest, snapshot digest against plan/run.
3. Determine own identity: `vcs.revision` and `vcs.modified=false` from build info must equal `plan.VerifierSourceCommit`; the profile digest must equal `plan.VerificationProfileDigest`. Unknown build info, a modified tree or a mismatch fails closed.
4. Build the verifier `ActorProvenance` (role verifier, `ActorID = "verifier:" + sourceCommit[:12] + ":" + profileDigest[:12]`, fresh `InvocationID`, empty lineage) and the worker `ActorProvenance` from the session evidence's `InvocationProvenance` (R2-A) with actor id re-derived from its stored basis; require `protocol.ActorsIndependent`.
5. Create an isolated worktree at the **candidate commit** (R1 manager, id `<run>-verify`), clean and read-only for the candidate: verify commit exists, its sole parent equals the task profile's `BaseCommit` (implementation) and the changed paths are inside the closed EWP write scope. Review-class tasks verify the candidate artifact JSON strictly instead.
6. Execute every `CheckSpec` of the task through `process.Runner`: argv only, `Dir` resolved inside the worktree, env limited to a fixed set (`PATH` of resolved executables, `HOME`/`TMPDIR` in a verifier scratch directory, `GOFLAGS=-mod=readonly`, `GOPROXY=off`, `GOTOOLCHAIN=local`, no proxy or credential variables), timeout per check, bounded output capture stored as verifier artifacts. A check needing the network fails with a nonzero exit and the run is `blocked` with `VERIFIER_INFRASTRUCTURE`, not scored as a defect result.
7. Compute results: `passed_acceptance_ids` = acceptance checks whose exit code equals `ExpectExitCode`; per defect, caught per the probe rule (implementation: all `CatchCheckIDs` pass; review: some finding's `evidence_refs` entry matches `path:line` within the anchor); `SeededDefectTotal = len(task.Defects)`, `SeededDefectsCaught` = count; `QualityVerdict = accepted` iff all acceptance checks passed and the task's defect rule is satisfied under the closed rules above, else `rejected`.
8. Deterministic re-run: compare the decision fields (acceptance ids, per-check exit codes, caught set, verdict) with the claimed original receipt; any disagreement returns `INCONSISTENT_VERIFICATION` (run `blocked`). Output bytes and digests are not compared (timestamps vary); the original receipt digest is returned as `ReceiptDigest` and must equal `RunEvidence.VerifierReceiptDigest` (admission checks).
9. Return `VerifiedOutcome` with `VerifiedCommandArtifactRefs` for the verifier's own output artifacts. The verifier never modifies the candidate, the profile or the judge source; the worktree is cleaned if clean.

### Algorithm: authority adapter

`receipts.CampaignAuthority{Verifier}` implements `OperatorAuthority`: strictly parse the authorization bytes, take its `authorized_by`/receipt reference, call `receipts.Verifier.Verify(Request{ReceiptID, ProjectID, Purpose: empirical.campaign_authorize, Subject: {Kind:"CampaignAuthorization", ID: campaign_id, Version: 1}, SubjectDigest: authorizationDigest})`, and require the authorization's `plan_digest` to equal `planDigest` and its `expiry` not after the receipt `NotAfter`. `Admitter` additionally requires each run's `SessionEvidence.StartedAt` to lie within `[receipt IssuedAt, min(authorization.expiry, receipt NotAfter)]` (G-4). The adapter lives with R4 or R3 adapters, never in `empirical` core (no dependency from `empirical` to `receipts`).

### Algorithm: CLI replay

`devcadence benchmark replay-empirical --manifest <file> --plan <file> --authorization <file> --artifacts <dir> --criteria <file> --output <report.md> [--json <report.json>]`: build resolver over the digest-addressed `--artifacts` directory (no network), compose the production authority and verifier, `Admit`; on refusal print the closed reason codes and exit `3`; otherwise `ReplayGate(admission, criteria)` with the pinned criteria and write the report (`Admission`, `RawGate`, `Conclusion` kept separate). Exit `0/1/2` is the raw Go/Revise/Inconclusive conclusion of the combined report. `evaluate-gate --evidence-kind empirical_campaign` exits `2` with a message naming `replay-empirical`; other evidence kinds are unchanged. No flag accepts a precomputed aggregate, a summary or a boolean "verified".

## Cards not authored in this window (explicit, no implementation authority)

- **R3-C campaign runner.** Per-run protocol steps 1–8 of WP-M5-5, per-call cap enforcement before each endpoint call, no-replay of uncertain calls (A7, A8, A11), manifest/receipt production. Depends on R1, R2-A, R3 Parts A/B and R4 and on OWNER INPUT-2. Needed only for the live re-evaluation.
- **R3-D corpus verification profiles and fixture repositories.** Authoring the `VerificationProfile` content and pinned fixture sources for the ten corpus tasks (resolves G-5 and U1). Profiles are the "tests" of the experiment and must be reviewed by someone other than the campaign author and frozen before any observation.

Until both exist, M5 cannot close and A9 cannot run.

## Authority matrix

| Effect | Authority | Forbidden substitute |
| --- | --- | --- |
| Admit a run as empirical | Admitter: authority receipt + verifier re-execution + admission checks | caller label, receipt bytes alone, default harness |
| Authorize a campaign | R4 grant receipt bound to authorization digest | JSON/CLI flag, same-identity file |
| Open a provider session | `DriverFactory` for a granted, eligible endpoint | API key presence, ambient CLI, discovery |
| Judge quality | pinned verifier + pinned profile on the immutable candidate | model confidence, worker report |

## Missing / unknown / stale and failure semantics

| Input | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| Receipt / authorization / grant | not admitted, zero verifier effects | unverifiable: not admitted | expired/revoked: not admitted | refusal with closed reason |
| Verifier build info / profile | not admitted | unknown revision: refuse | mismatch: refuse | refuse |
| Session evidence / usage | run not completed | unknown usage: `Known=false`, no efficiency proof | n/a | integrity refusal |
| Check execution | `blocked` | timeout: failed check, not infra if exit recorded | n/a | `blocked` |
| Endpoint kind | `driver-not-implemented` | n/a | n/a | n/a |

| Failure boundary | Required postcondition | Recovery |
| --- | --- | --- |
| Verifier unavailable/crash | run blocked, never accepted | rerun verification only (no model call) |
| Original/rerun disagreement | `INCONSISTENT_VERIFICATION`, blocked | profile owner reviews flakiness |
| Candidate cannot be checked out | blocked | n/a |
| Report write fails | inputs retained; regenerate | no model call |
| Gate Revise/Inconclusive | no product claim | separate EWP |

## Traceability and acceptance scenarios

| ID | Setup → action → expected | Requirement → invariant → representation |
| --- | --- | --- |
| A1 | receipt claims pass but re-execution fails → verify → rejected/blocked per rule | R1 → I1 |
| A2 | forged receipt bytes/digest mismatch → refused before verifier effects | R1 → I1 |
| A3 | plan verifier commit or profile digest differs from the running verifier → refuse | R2 → I2 |
| A4 | worker and verifier actors overlap/lineage/same invocation → refuse | R3 → I3 |
| A5 | implementation defect: probe checks pass → caught; fail → not caught; counts exact | R4 → I4 |
| A6 | review task: finding anchors inside range → caught; vague/out-of-range → not caught | R4 → I4 |
| A7 | session evidence missing, wrong digest, or authorization expired at session start → refuse | R5 → I5 |
| A8 | unknown token usage → `Known=false`; gate numeric input skipped; Inconclusive | R5 → I5 |
| A9a | campaign grant receipt valid/forged/revoked/wrong plan → authority accepts only the valid one | R6 → I6 |
| A10 | infra failure vs quality failure vs inconsistency → blocked / completed-rejected / blocked | R7 → I7 |
| A11 | `Open` for remote or CLI endpoint → `driver-not-implemented`, zero network | R8 → I8 |
| A12 | loopback client refuses non-loopback host, redirect, proxy, oversize response, credential header | R8 → I8 |
| A13 | `replay-empirical` with refused admission exits 3, no gate call; legacy label exits 2 | R9 → I9 |
| A14 | profile with `sh -c`, network-looking arg, `..` dir → load refused; verifier env contains no proxy/credential variables | R10 → I10 |
| A15 | nil authority/verifier/resolver → `NewAdmitter` refused; production `ValidateAdmission` still denies | R6/R1 → I6/I1 |

The live A9 scenario of WP-M5-5 is **not** claimed by anything in this EWP; fixtures here are synthetic and prove schema and control flow only.

## Validation and Mutation Catalog

`go test -count=1 -race ./internal/benchmark/empirical/... ./internal/cognition/sessionclients/... ./cmd/devcadence/... ./tests/...`; `make schemas`; `make docs-check`; `make verify`. Record versions, fixture digests, check argv arrays, exit statuses and the replay-gate argv/exit.

| Mutant | Expected failure |
| --- | --- |
| Accept receipt claims without re-execution | A1 |
| Skip digest/receipt cross-checks | A2 |
| Ignore verifier source/profile pinning or build-info unknown | A3 |
| Treat same actor, shared lineage or same invocation as independent | A4 |
| Count a defect caught when any probe passes, or by finding text without anchor | A5/A6 |
| Skip session-evidence digest/expiry check | A7 |
| Unknown usage becomes zero | A8 |
| Authority accepts any receipt/grant, ignores revocation or plan digest | A9a |
| Infra failure counted accepted/rejected-quality; disagreement ignored | A10 |
| Remote/CLI kind opens a client | A11 |
| Client follows redirect, honors proxy, or dials non-loopback | A12 |
| Replay calls the gate despite refusal; legacy label still works | A13 |
| Shell or ambient env allowed in checks | A14 |
| Exported test authority reachable from production | A15 |

Independent lenses: Contract/Authority; Test Adequacy/Mutation. The verifier author is not a reviewer and is not the campaign author.

## Rationale, escalation and readiness

Selected: re-execution of a pinned deterministic profile by a pinned verifier on the immutable candidate, compared to the claimed receipt, versus accepting a structurally valid receipt or having the campaign author supply the verifier. Rejected: widening `TelemetrySnapshot` in place (WP-M5-5 rationale stands). Cost: profile authoring (R3-D) and a stricter, slower admission.

Escalate on: gate criteria, corpus or snapshot schema changes; a verifier needing network or shell; no deterministic probe expressible for a corpus task; build-info unavailable in the owner's build process; any request for a remote client before OWNER INPUT-2; the amendments to WP-M5-5 not being authorized.

Implementation Readiness Report:

~~~text
requirements represented: 10/10
state transitions specified: 5/5 (verify, authority check, admit, replay, open)
failure cases specified: 8/8
authority decisions specified: 4/4
missing/unknown input semantics: 5/5
acceptance scenarios mapped: 15/15
unresolved architecture choices: 2 (G-5 profile content; U1 target repository), both owned by card R3-D
readiness: Part A NOT_READY pending R1-A freeze and OWNER INPUT-2 for anything beyond loopback; Part B NOT_READY pending amendments' review, R2-A and R4-A freezes
~~~

Weaker-implementer check: Part A yes (loopback only); Part B yes for the verifier algorithm, with profile content explicitly out of scope.

## Changelog

- r1: initial draft for window 2026-10-G.

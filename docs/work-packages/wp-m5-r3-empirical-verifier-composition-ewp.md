# WP-M5-R3 — Empirical verifier and provider composition

## Identity

- Revision: 3 (owner review of r2 head 106dafd repaired; verdicts pending re-verification); task: task-m5-r3-empirical-verifier-composition; window: [2026-10-G](window-2026-10-g-overview.md).
- Base: `71bdaec6d6d81c1b6e52d8b30f0f8485f928925a` (`main` `cebb4f0` plus the PR #83 reconcile merge, 2026-10-05).
- Contract digest: reviewed immutable Git blob. No fictitious runtime state revision; record the actual accepted dependency commits at execution.
- Endpoint: competent Go implementer with Git, deterministic-test and HTTP-client skill; complete admission is mandatory. Each Part is sized for one endpoint session.
- Status: **DRAFT / NOT_READY / NOT FROZEN. No implementation authority; no endpoint call, credential, spend or live campaign is authorized.** This EWP **amends** the draft [WP-M5-5](wp-m5-5-empirical-campaign-ewp.md) contract at the points listed under "Explicit amendments"; the amendments need window-level review before Part B freezes.
- Dependencies: **Part 0** none (it amends the merged `internal/cognition/drivers`); [M5-R1](wp-m5-r1-native-task-executor-ewp.md) Part A types (`ResolvedEndpoint`, `EndpointObservation`, `OpenedEndpoint`, `DriverFactory`, `Bind`) and Part 0 for Part A; the leaf `internal/execrt` (frozen with R1-B: `RepositoryProvider`) and `internal/worktrees` for Part B; [M5-R2](wp-m5-r2-independent-review-acceptance-ewp.md) Part A (`DeriveActorID`, `InvocationProvenance`); [M5-R4](wp-m5-r4-protected-operator-ingress-ewp.md) Part A (`receipts.Verifier` with subject lookup, `Verified.IsValid`) for the production authority; merged WP-M5-5 admission package.
- Parts (separately freezable): **0** driver usage-knownness amendment (shared driver evidence contract; freezes before R1-B and R3-A). **A** provider composition (`DriverFactory` implementations, first slice loopback local only). **B** independent verifier, authority adapter, admission wiring and CLI replay.

## Objective

Supply what WP-M5-5 explicitly left to a follow-on: the production `IndependentVerifier` (receipt re-execution on an immutable candidate with a pinned profile), the production operator authority (campaign-grant receipts through R4), the missing evidence bindings (session evidence, verifier source and profile pinned in the plan), a CLI that replays the unchanged gate only through admission, and the minimal provider composition so a granted endpoint can actually run a session. Nothing here runs a campaign or spends anything; it makes admission *able to succeed* for genuine evidence and *unable to succeed* for anything else.

## Context Manifest

Role: Go implementer; independent Contract/Authority and Test Adequacy/Mutation reviewers; the verifier author is not the campaign author. Read envelope: `internal/benchmark/empirical/*` (types, plan, admission, replay), `internal/benchmark/{runner,defects,telemetry,gate,experiments}`, `internal/benchmark/corpus` (signatures), `cmd/devcadence/benchmark.go`, `internal/cognition/drivers/{direct_api,types,metering}.go`, `internal/cognition/ollama` (transport conventions), R1 `execpolicy` types, R2 `actors`, R4 `receipts`, `internal/worktrees`, `internal/process`. Write scope: `internal/cognition/drivers/` usage types, meter and their callers/tests (Part 0), new `internal/benchmark/empirical/verifier/` (verifier, profile, `CampaignAuthority` adapter, `BuildInfoSource`), `internal/cognition/sessionclients/` (Part A), additive types/fields and the `Admitter` in `internal/benchmark/empirical` (amendments), `cmd/devcadence` subcommand `benchmark replay-empirical` and the legacy refusal, schemas/fixtures under `schemas/`, owning docs. No criteria, gate mathematics or invariant change.

Exact clauses: AGENTS §§2–9, 12–15, 17; ADR-0024 §§3–5, 10; WP-M5-5 §§Exact representation, Admission and gate decision semantics, Authority matrix; SECURITY §§3, 5–9, 14–17; DCI-005, 011–014, 032–033, 040–044, 080–084, 090, 100, 120–124, 126, 129, 159–161. Risks: contaminated verifier, forged receipt, flaky deterministic checks, non-hermetic commands, unknown-as-zero, unbounded spend through a client. Re-resolution triggers: gate criteria or corpus change, a new driver kind or credential path, a verifier needing network, empirical type drift, or an owner decision outside OWNER INPUT-2.

## Scope envelope

Authorized: as above. Forbidden: a campaign runner, live execution or fixture authoring (cards R3-C, R3-D below); any credential reading, provider login, remote client or subscription-CLI mapper in the first slice; shell-executing verifier commands; modifying or weakening `EvaluateM4Gate`, criteria, existing snapshot schemas or the synthetic report; a verifier that reads the candidate from the worker's writable worktree; accepting a receipt by digest alone; any bypass of the injected `IndependentVerifier`/authority; a test fake reachable from production composition.

LOCAL_DISCRETION: private helpers, test layout, report typography, HTTP client internals that keep the stated limits.

## Requirements and invariants

| ID | MUST requirement | Invariant |
| --- | --- | --- |
| R1 | The verifier re-executes the pinned deterministic checks on the immutable candidate in a fresh isolated worktree and derives `VerifiedOutcome` from its own execution; the caller receipt is a claim compared field by field | I1: receipt authenticity and independent outcome are distinct checks |
| R2 | The plan pins `VerifierSourceCommit` and `VerificationProfileDigest`; the verifier determines its own source commit from an injected `BuildInfoSource` (production: `debug.ReadBuildInfo` `vcs.revision` with `vcs.modified=false`) and refuses on any mismatch or unknown. This is a **label check**, not process independence: a binary that carries the right revision label proves nothing about who built or runs it; independence rests on the pinned profile, the separate worktree and actor rules, and is disclosed as such | I2: the judge is fixed before observations |
| R3 | `VerifiedOutcome.Worker` and `.Verifier` are canonical `ActorProvenance` (R2-A); both actor ids are derived through `actors.DeriveActorID` (the verifier from `ActorBasis{EndpointID:"devcadence-verifier", ModelID: source commit, ModelRevision: profile digest}` under `endpoint_model`, no ad-hoc string); the verifier identity is non-model and independent under `ActorsIndependent`, with a separate invocation and no lineage overlap | I3: no self-verification |
| R4 | Seeded-defect counts follow the profile's deterministic probe rule: implementation tasks count a defect caught only when all its probe checks pass on the candidate; review tasks only when a strictly parsed finding anchors the defect location. Defect counts are measurements; they never decide `QualityVerdict` (see step 7) | I4: quality is computed, not asserted |
| R5 | A session-evidence artifact (strict schema, digest-bound in `RunEvidence`) records live endpoint/model/config, prompt digest, timestamps, driver outcome and usage with unknown ≠ zero; admission checks authorization expiry against its start time | I5: no run is empirical without direct session provenance |
| R6 | The production operator authority accepts a campaign authorization only through an R4 `empirical.campaign_authorize` grant receipt bound to the authorization digest, project and plan digest | I6: authority is protected, not labeled |
| R7 | Verifier infrastructure failure yields `blocked`, a genuine quality failure yields completed-not-accepted, and original-versus-rerun disagreement yields `blocked` (`INCONSISTENT_VERIFICATION`); none counts as accepted. The infrastructure/quality split is a *decidable process-level rule* (step 6), not a guess about network need | I7: failures keep their identity |
| R8 | Provider composition opens only a granted, eligible endpoint; the first slice implements loopback local runtimes only; every other kind is `MODEL_UNAVAILABLE` (`driver-not-implemented`) | I8: no ambient provider, no hidden fallback (DCI-122) |
| R11 (Part 0) | Token usage carries explicit knownness: an omitted or unparsable usage is **unknown**, never measured zero; unknown usage can never prove compliance with a token or spend budget; the meter observes usage after a call and pre-call safety comes only from independently enforceable per-call bounds | I11: unknown is not zero (shared evidence contract) |
| R9 | `devcadence benchmark replay-empirical` runs Admitter then `ReplayGate`; legacy `evaluate-gate --evidence-kind empirical_campaign` is refused with its own exit code (4), distinct from every replay conclusion | I9: the caller's label can never establish empirical evidence |
| R10 | Verifier commands are fixed argv arrays from the pinned profile, with a minimal environment, no network and no shell | I10: verification is controlled execution (DCI-033) |

## Verified facts and representability gaps

| Fact / gap | Evidence at pinned base | Implication |
| --- | --- | --- |
| Admission is complete but production `ValidateAdmission` cannot return `Admitted` (`denyAuthority`, nil verifier path) | `empirical/admission.go`, `types.go` | export a constructor taking an authority and verifier |
| `operatorAuthority` is unexported and `VerifyAuthorization` returns only `error` | `empirical/types.go` | **G-1:** an exported `OperatorAuthority` and an `Admitter` are needed, and the authority must return the receipt window so admission can check session start (G-4) without `empirical` importing `receipts` |
| `CampaignAuthorization` has `AuthorizedBy` (a label), `Expiry`, `PlanDigest` and no receipt field; it carries `MaxAPISpendUSD float64`; `EndpointBinding` is exported from `empirical` | `empirical/types.go` | no receipt id can live in the signed bytes (circular: the id is issuer-generated and the signature covers the digest of those bytes); the receipt is discovered by subject (R4 Lookup); `AuthorizedBy` is display-only; the float is replaced by integer micro-USD (amendment 6) |
| `RunEvidence` has `SessionEvidenceRef` and no digest; session artifact is never resolved; `VerifiedOutcome.SessionDigest` is format-checked only | WP-M5-5 implementation record, blocked item 3 | **G-2:** add `SessionEvidenceDigest` and define the artifact |
| `CampaignPlan` pins no verifier source or profile; `VerifiedOutcome` carries both | `empirical/types.go` | **G-3:** add two required plan fields |
| Authorization `expiry` only checked as RFC3339 | record, blocked item 2 | **G-4:** compare to session start (G-2) and to the receipt window |
| Corpus defects are synthetic file patches with `FileTarget`/`PatchContent`; `DefaultVerificationSuite` simulates outcomes by defect id; no per-defect detector command exists | `benchmark/defects.go`, `runner.go` | **G-5:** a `VerificationProfile` is required; authoring its content for the ten corpus tasks is card R3-D |
| Fixture targets such as `internal/auth/checker.go` may not exist in any pinned source tree | `benchmark/defects.go` | unknown U1: which repository the campaign targets (owner/Principal decision with R3-D) |
| `evaluate-gate` trusts `--evidence-kind empirical_campaign` and has no falsification input | WP-M5-5 §Bounded campaign, record item 4 | R9 replaces the path; no change to gate math |
| Only `DirectAPIDriver` and `CLIWrapperDriver` exist; no real client; `ollama` package is probe-only (`GET /api/version`, `/api/tags`, `/api/ps`, `POST /api/generate`) with an `HTTPTransport`; its `/api/tags` entry decoder reads no `digest` | `cognition/drivers`, `cognition/ollama/ollama.go` | Part A adds one loopback `DirectAPIClient` speaking the Ollama native protocol (decision below); step 0 verifies `/api/tags` returns a `digest` per model |
| `go test` binaries carry no `vcs.revision`/`vcs.modified` build settings | Go toolchain behaviour (step 0 verifies with the owner's Go version) | the verifier takes build identity through an injected `BuildInfoSource`; the production adapter fails closed when unknown |
| `telemetry.RunTelemetrySnapshot` has `AccountingUncertain` and no endpoint/model fields | `benchmark/telemetry/metrics.go` | continue to use the sidecar; unknown tokens are zero placeholders with the flag, never comparable |
| `drivers.TokenUsage{InputTokens, CachedTokens, OutputTokens int64}`; `Add`/`Total` pure sums; `MeteredSession` enforces cumulative token limits from these values after each call; `RecordOperationEnd(start, TokenUsage{}, ...)` is used for tool-only operations; a client omitting usage is indistinguishable from measured zero | `cognition/drivers/{types,metering,direct_api,cli_wrapper,fake_driver}.go` | **Part 0:** explicit knownness before any consumer can claim "unknown != zero" |
| Facade errors cannot be built outside `facade` | `facade/errors.go` | `principal.CodedError` (window overview SC-2) |
| `empirical.EndpointBinding` fields `ChannelID`, `CapabilityClass`, `RuntimeVersion`, `ContextProfileDigest`, `PolicyDigest`, `SubscriptionQuotaUnit`; `protocol.AccessChannel.ChannelID` exists; Ollama `GET /api/version` returns the runtime version | `empirical/types.go`, `protocol/access_channel.go`, `cognition/ollama/ollama.go` | every field has a stated source in Part A `BindingFor`; no literals invented by the implementer |
| `protocol.ActorProvenance` and `ActorsIndependent` exist; empirical already uses them | `protocol/review_ledger.go`, `empirical` | reuse |

Step 0 re-verifies each row; a false row escalates.

## Explicit amendments to the WP-M5-5 draft (no plan or authorization has ever been issued; amendments 3 and 6 change an unexported seam signature and a never-issued field, the rest are additive)

1. `CampaignPlan` gains required `VerifierSourceCommit` and `VerificationProfileDigest` (G-3). `PlanDigest` fixtures are regenerated; no historical plan exists.
2. `RunEvidence` gains required `SessionEvidenceDigest` for completed/failed runs that started a session (G-2).
3. An exported `OperatorAuthority` interface **whose method now returns the receipt window** and an `Admitter` (both in package `empirical`, which imports neither `receipts`, `actors` nor `verifier`) replace the unexported seam as the only way to obtain `Admitted=true`; the old `ValidateAdmission` keeps its fail-closed behavior and doc comment (G-1, G-4):
~~~go
// package empirical (core; additive)
type AuthorityWindow struct{ IssuedAt, NotAfter time.Time } // UTC; NotAfter already min(authorization.Expiry, receipt NotAfter)
type OperatorAuthority interface {
    VerifyAuthorization(ctx context.Context, planDigest, authorizationDigest string, authorization []byte) (AuthorityWindow, error)
}
~~~
   `denyAuthority` adopts the new signature and still always errors. `Admitter` decodes each run's session evidence through the `ArtifactResolver` (type `empirical.SessionEvidence`, amendment 7), checks its digest equals `RunEvidence.SessionEvidenceDigest` and that `StartedAt` lies in `[window.IssuedAt, window.NotAfter]`.
4. Replay entry point is a new subcommand (R9); the legacy `evaluate-gate` argv in WP-M5-5 is superseded for empirical use.
5. The `m5-m4-empirical-report.md/json` artifact set and the three-way `Report{Admission, RawGate, Conclusion}` are unchanged.
6. `CampaignPlan`/`CampaignAuthorization` replace `MaxAPISpendUSD float64` by `MaxAPISpendMicroUSD int64` (digests must not depend on float formatting, so spend is integer micro-USD everywhere in this window); step 0 verifies how the plan and authorization digests are computed (and how `protocol.Digest` canonicalizes numbers) and records it.
7. `SessionEvidence` (below) is a type of package `empirical` because it embeds `empirical.EndpointBinding`; the `verifier` package imports `empirical`, never the reverse. `CampaignAuthorization.AuthorizedBy` stays a display label with no authority meaning; the receipt is found by subject (R4 Lookup), never named inside the signed bytes.

## Part 0 — Driver usage-knownness amendment (shared contract; freezes first)

This amends the merged shared driver evidence contract, disclosed in the window overview (Shared schema policy). It is separately reviewable and freezable and is a prerequisite of R1-B and R3-A.

~~~go
// package drivers
type TokenMeasurement struct { Known bool; Value int64 }   // the zero value is UNKNOWN
type TokenUsage struct { Input, Cached, Output TokenMeasurement }   // JSON keys input_tokens, cached_tokens, output_tokens unchanged
func KnownUsage(input, cached, output int64) TokenUsage
func (u TokenUsage) Add(other TokenUsage) TokenUsage   // per field: Known iff both Known; Value is the sum only when Known (else 0)
func (u TokenUsage) Total() (total int64, known bool)  // known iff Input and Output are both Known
func (u TokenUsage) Complete() bool                    // Input, Cached and Output all Known
~~~

Wire form (back-compatible): a JSON integer decodes to `{Known:true, Value:n}` (negative refuses); a missing key or `null` decodes to unknown; unknown encodes as `null`/omitted. A producer that never reports usage therefore yields **unknown**, not measured zero. Existing producers are updated in the same change: `fake_driver` and tests use `KnownUsage`; `direct_api` response decoding uses the wire form above; `cli_wrapper` marks usage known only for the fields it actually parsed; the tool-result-only operation at `metering.go` (no model call) records `KnownUsage(0,0,0)` because no generation occurred.

Meter semantics (`MeterLimits` gains `AllowUnknownUsage bool`, default false; the meter still **observes after** each call):

1. Cumulative usage is a `TokenUsage` with the `Add` rule; `MeterSnapshot` exposes `UsageKnown bool` (cumulative `Complete` for input/output).
2. A configured token limit is evaluated only against known cumulative totals. Unknown usage can never be treated as compliance: if a call reports unknown usage and `AllowUnknownUsage` is false, the session is suspended with `PAUSED_BUDGET_EXCEEDED` and pause reason `usage_unknown` (a post-call observation; the call already happened).
3. If `AllowUnknownUsage` is true, the session continues, the token limit is recorded as **unprovable** (`UsageKnown=false`), and the other hard bounds continue to apply (turns, wall time, tool calls, and the per-call bounds enforced by the driver client: output cap and request/context ceilings). Consumers (R1 policy loader) permit `AllowUnknownUsage` only for local-locality grants without metered spend; a metered or remote channel with unknown billable usage is not eligible (R1 resolver step 4) and ends the attempt `limit_reached` if it occurs.
4. "Metering is a pre-call control" is **not** claimed anywhere: pre-call safety is the per-call bound, not the meter.

Acceptance scenarios (P0-1..P0-6): omitted usage block decodes unknown (not zero) and `Add` with known stays unknown for that field; integer wire value decodes known, negative refuses; unknown usage with `AllowUnknownUsage=false` suspends with `usage_unknown`; with `true` continues, never reports the token budget as met, and a turn/wall-time limit still trips; known usage over a limit trips as before (regression); all existing driver tests pass unchanged except constructors, fixtures regenerate deterministically. Mutants: unknown decoded as zero; `Add` treating unknown as zero; token limit evaluated against `Value` while unknown; `AllowUnknownUsage` defaulting true.

## Part A — Provider composition (first slice: loopback local only)

~~~go
// package sessionclients
type Composition struct{ /* options below */ }
type Options struct {
    LoopbackBaseURLs map[string]string // endpoint id -> http://127.0.0.1:<port> or http://[::1]:<port>, validated
    Clock clock.Clock
}
func New(Options) (*Composition, error)
func (c *Composition) Open(ctx context.Context, ep execpolicy.ResolvedEndpoint) (execpolicy.OpenedEndpoint, error) // implements execpolicy.DriverFactory; metadata-only, nothing to close
func BindingFor(ep execpolicy.ResolvedEndpoint) (empirical.EndpointBinding, error)                               // ep MUST be bound (BindingDigest != "" and RuntimeVersion != ""), else error
~~~

`Open` dispatches on `ep.Kind`: `local_runtime` builds a `DirectAPIDriver` over a loopback chat client; `authenticated_cli` and `remote_api` return `MODEL_UNAVAILABLE` `driver-not-implemented` (their mappers and clients are added only after OWNER INPUT-2 names an endpoint, as amendments of this EWP).

**Client protocol decision (closed for the first slice).** The loopback client speaks the **Ollama native API**, the only local runtime this repository already probes (`internal/cognition/ollama`): `POST /api/chat` with `{"model": ep.ModelID, "messages": [...], "tools": [{"type":"function","function":{"name","description","parameters"}}], "stream": false, "options": {"num_predict": ep.Limits.MaxOutputTokensPerCall}}`; the request body is refused before sending when larger than `ep.Limits.MaxRequestBytes`; the response `message.content` and `message.tool_calls[].function{name, arguments (JSON object)}` map to `DirectAPIResponse` content and `ToolCall`s (a tool call whose `arguments` is not an object, or whose name is not in the request's tool list, is a typed protocol error, never executed); usage maps from `prompt_eval_count`/`eval_count` as **known** only when both are present (`KnownUsage(prompt, 0, eval)`, cached unknown), otherwise the whole `TokenUsage` is unknown (Part 0 form). The OpenAI-compatible `/v1/chat/completions` shape is **not** implemented in this slice; wanting it is an OWNER INPUT-2 amendment (default deny). `Stream` returns a typed unsupported error.

**Metadata-only `Open` (closes the BindingDigest/ModelRevision/RuntimeVersion circularity).** `Open` sends only two bounded metadata requests and no prompt, source or spend: `GET /api/version` (the runtime's own `version` string, sanitized, 1..64 bytes) and `GET /api/tags` (find the entry whose `name`/`model` equals `ep.ModelID` exactly). It returns `Observed.RuntimeVersion = <version>` and `Observed.ModelRevision = "sha256:" + <entry.digest>` when the digest is present and non-empty; either value absent yields `""`, which makes `execpolicy.Bind` deny (`runtime-version-unknown` / `model-revision-unknown`). The returned driver holds only a base URL and a shared HTTP transport owned by the `Composition` (created in `New`, never per-`Open`), so nothing needs `Close` per endpoint. Rules for every request: URL host must be a literal loopback IP (or `localhost` resolved by the client to loopback only, re-checked on the dialed address); no redirects; no proxy; no credentials or auth headers; TLS not required for loopback; request/response size caps (1 MiB / 4 MiB); per-request timeout from the resolved limits; tool calls are returned to the mediator and never executed by the client.

**`BindingFor` field sources (exact; no literal is invented).** All inputs come from the **bound** `ResolvedEndpoint`; an unbound endpoint returns an error:

| `empirical.EndpointBinding` field | Source |
| --- | --- |
| `EndpointID`, `ModelID`, `DriverID` | `ep.EndpointID`, `ep.ModelID`, `ep.DriverID` (driver id from the `DirectAPIDriver` the composition built) |
| `ModelRevision` | `ep.ModelRevision` (the runtime-reported `sha256:` digest from `/api/tags`) |
| `ChannelID` | `ep.Channel.ChannelID` (the portfolio `protocol.AccessChannel`, copied by the resolver) |
| `CapabilityClass` | closed map from `ep.Kind`: `local_runtime` -> `local_small`, `authenticated_cli` -> `subscription_cli`, `remote_api` -> `frontier_api`; any other kind is an error |
| `RuntimeVersion` | `ep.RuntimeVersion` (the `GET /api/version` observation bound by `execpolicy.Bind`; never a literal) |
| `ContextProfileDigest` | `protocol.Digest` (canonical JSON) of `ep.ContextProfile` |
| `PolicyDigest` | `ep.PolicyDigest` (the canonical execution-policy digest from `PolicySource.Current`) |
| `SubscriptionQuotaUnit` | the closed-vocabulary value `unknown` unless the granted endpoint pins a unit (the local slice has none; `unknown` is the schema's own "not known" value, not an invented fact) |

## Part B — Verifier, authority, admission wiring, CLI

### Types (new unless noted)

~~~go
// package empirical (core): SessionEvidence moves here (amendment 7); Admitter and OperatorAuthority per amendment 3
type SessionEvidence struct {          // artifact schema "empirical-session" 1.0, strict
    Version, RunID, CampaignID, AttemptID, TaskDigest string
    Endpoint EndpointBinding
    PromptDigest, ContextManifestDigest, InvocationProvenanceDigest, ExecutionPolicyDigest, AuthorizationDigest string
    StartedAt, EndedAt string          // RFC3339 UTC
    DriverOutcome string               // "completed" | "error" | "cancelled" | "limit_reached"
    Turns int
    Usage map[string]Measurement       // the ten-key vocabulary of WP-M5-5; unknown stays Known=false
}
type AdmitterOptions struct { Authority OperatorAuthority; Verifier IndependentVerifier; Resolver ArtifactResolver; Clock clock.Clock }
func NewAdmitter(AdmitterOptions) (*Admitter, error) // nil Authority/Verifier/Resolver/Clock refused; there is no exported test authority
func (a *Admitter) Admit(ctx context.Context, m CampaignManifest) (AdmissionResult, error)

// package verifier (imports empirical, actors, receipts, process, worktrees, execrt (leaf); never taskexec or reviewexec)
type CheckSpec struct {
    CheckID string
    Argv []string          // fixed; Argv[0] is an executable name from the profile allow-list, never a shell
    Dir string             // relative to the worktree root, no ".."
    TimeoutSeconds int     // 1..600
    ExpectExitCode int
}
type ReviewAnchor struct { Path string; StartLine, EndLine int }   // 1 <= StartLine <= EndLine
type DefectProbe struct {
    DefectID string
    CatchCheckIDs []string      // implementation tasks: caught iff all pass on the candidate
    Anchor *ReviewAnchor        // review tasks: caught iff a finding cites "path:line" inside the anchor
}
type TaskVerification struct {
    TaskID, TaskDigest, Class string // "implementation" | "review"
    BaseCommit string
    WriteScope []string         // implementation only: repo-relative file paths or directory prefixes ending "/"; part of the profile digest; the only source of the write scope
    Checks []CheckSpec
    AcceptanceCheckIDs []string // implementation: required; review: must be empty
    Defects []DefectProbe
}
type VerificationProfile struct {
    Version, ProfileID string        // "1.0"
    Executables []string             // allow-list; "sh","bash","zsh","cmd","powershell" refused at load
    Tasks []TaskVerification         // sorted, unique TaskID
}
type BuildInfoSource interface { VCSRevision() (revision string, modified bool, ok bool) }
// Production default (Options.BuildInfo == nil): debug.ReadBuildInfo settings vcs.revision / vcs.modified; ok=false when absent
// (always so in `go test` binaries). Tests inject a source from _test.go only; no exported permissive source exists.
type Options struct { Resolver empirical.ArtifactResolver; Worktrees *worktrees.Manager; Repositories execrt.RepositoryProvider; Runner *process.Runner; BuildInfo BuildInfoSource; Clock clock.Clock; IDs ids.Source; ScratchDir string }
func New(Options) (*Verifier, error)        // implements empirical.IndependentVerifier
~~~

Profile load validates: strict decode, unique ids, each `CheckSpec` argv non-empty with `Argv[0]` in `Executables`, no shell, no URL-looking argument, no `..` directory; `AcceptanceCheckIDs` and `CatchCheckIDs` reference existing checks; implementation tasks have a non-empty `WriteScope`, at least one acceptance check and probes; review tasks have anchors and no acceptance checks or write scope. The profile digest is the canonical digest of the whole document and must equal `CampaignPlan.VerificationProfileDigest`. The campaign renderer (R4) prints that digest and the distinct executables across all checks.

### Algorithm: `Verify` (implements `empirical.IndependentVerifier`)

1. Preconditions: run status `completed`; plan, run, evidence internally consistent (admission has already matched ids). Unknown/unavailable inputs return an infrastructure error (`blocked`), never an accept.
2. Resolve and digest-check the receipt, session evidence, candidate artifact **and the worker `InvocationProvenance` record** (by `SessionEvidence.InvocationProvenanceDigest`, through the same `ArtifactResolver`; the campaign runner, card R3-C, stores that record as a digest-addressed artifact; a missing or digest-mismatching record is an infrastructure `blocked`, never an accept) (no network). Strict-decode the receipt (fields per WP-M5-5 `VerifierReceipt`), the session evidence and the provenance record. Cross-check ids, task digest, seed, strategy, endpoint binding digest, prompt digest, candidate commit and artifact digest, snapshot digest against plan/run.
3. Determine own identity: `BuildInfo.VCSRevision()` must return `ok` and `modified == false` and a revision equal to `plan.VerifierSourceCommit`; the profile digest must equal `plan.VerificationProfileDigest`. Unknown build info, a modified tree or a mismatch fails closed (R2 label-check caveat applies).
4. Build the verifier `ActorProvenance` (role verifier; `ActorID = actors.DeriveActorID("endpoint_model", ActorBasis{EndpointID:"devcadence-verifier", ModelID: sourceCommit, ModelRevision: profileDigest})`; fresh `InvocationID`; empty lineage) and the worker `ActorProvenance` from the **resolved** `InvocationProvenance` record (step 2; never from a digest alone): require role `implementer`, `AttemptID`/`WorkPackageID`/`TaskID` equal to the run, `EndpointBindingDigest` and `Basis.{EndpointID, ModelID, ModelRevision}` equal to `SessionEvidence.Endpoint` (`EndpointID`, `ModelID`, `ModelRevision`); then **re-derive** the worker actor `actors.DeriveActorID("endpoint_model", record.Basis)` from the stored trusted `Basis` (the verifier's own basis is `endpoint_model`, so independence is evaluated under it; an empty selected field fails closed) and require it equal to the stored `Actor.ActorID`; evaluate `protocol.ActorsIndependent` on provenance copies carrying the re-derived ids (separate invocation id, no lineage overlap).
5. Implementation tasks: create an isolated worktree at the **candidate commit** (R1 manager, id `<run>-verify`), clean and read-only for the candidate: verify the commit exists, its sole parent equals the task's `BaseCommit`, and every changed path (`git diff --name-status base..commit`, renames and deletions included, no symlink/submodule/`.git` change) lies inside `TaskVerification.WriteScope`; a violation is a **quality** failure (`rejected`, verdict reason `scope`), not infrastructure. Review tasks skip the worktree and verify the candidate artifact JSON strictly instead: it must strict-decode into `protocol.ReviewResult` (unknown fields refuse) and pass `Validate()`; failure is a quality failure.
6. Execute every `CheckSpec` of the task through `process.Runner`: argv only, `Dir` resolved inside the worktree, env limited to a fixed set (`PATH` of resolved executables, `HOME`/`TMPDIR` in a verifier scratch directory, `GOFLAGS=-mod=readonly`, `GOPROXY=off`, `GOTOOLCHAIN=local`, no proxy or credential variables), timeout per check, bounded output capture stored as verifier artifacts. **Infrastructure vs quality is decided by the process outcome alone:** if the process could not be started, the executable was not found, the runner killed it for exceeding the timeout or output cap, it died to a signal, or the worktree could not be created → `blocked` with `VERIFIER_INFRASTRUCTURE`. If the process exited with any exit code, the check is a normal result (pass iff the code equals `ExpectExitCode`), including a failure caused by a missing dependency or a refused network call. Network need is *not* detected (it is undecidable); it is excluded structurally (offline environment) and covered by an R3-D obligation: every task's profile must be shown, before freeze, to pass all acceptance checks with its reference solution under this exact environment, so a check that cannot pass offline is found at profile review rather than scored as a model defect.
7. Compute results: `passed_acceptance_ids` = acceptance checks whose exit code equals `ExpectExitCode`; per defect, caught per the probe rule (implementation: all `CatchCheckIDs` pass; review: some finding's `evidence_refs` entry matches `^([^:\s]+):([1-9][0-9]*)$`, its path (slash-separated, `path.Clean`, no `..`, no leading `/`) equals `Anchor.Path` and its line lies in `[StartLine, EndLine]`); `SeededDefectTotal = len(task.Defects)`, `SeededDefectsCaught` = count. **`QualityVerdict = accepted` iff** the write-scope/artifact checks of step 5 passed **and** every `AcceptanceCheckID` passed (review tasks, having none, are accepted iff the artifact validated); defect counts are measurements recorded beside the verdict and never decide it (a model that passes acceptance but misses seeded defects is `accepted` with a low catch rate, which is exactly what the gate's quality metric must see). Otherwise `rejected`.
8. Deterministic re-run: compare the decision fields (acceptance ids, per-check exit codes, caught set, verdict) with the claimed original receipt; any disagreement returns `INCONSISTENT_VERIFICATION` (run `blocked`). Output bytes and digests are not compared (timestamps vary); the original receipt digest is returned as `ReceiptDigest` and must equal `RunEvidence.VerifierReceiptDigest` (admission checks).
9. Return `VerifiedOutcome` with `VerifiedCommandArtifactRefs` for the verifier's own output artifacts. The verifier never modifies the candidate, the profile or the judge source; the worktree is cleaned if clean.

### Algorithm: authority adapter

`verifier.CampaignAuthority{Verifier receipts.Verifier, Clock}` (package `verifier`, which may import `receipts`; package `empirical` imports neither) implements `empirical.OperatorAuthority`:

1. Strictly decode `authorization` into `empirical.CampaignAuthorization`; require `PlanDigest == planDigest`, a parseable `Expiry`, `Expiry` in the future.
2. `v, err := Verifier.Verify(Request{ReceiptID: "", ProjectID, Purpose: empirical.campaign_authorize, Subject: {Kind:"CampaignAuthorization", ID: planDigest, Version: 1}, SubjectDigest: authorizationDigest})` (R4 lookup by subject; the receipt is a detached file under `receipts/`, never referenced from the authorization bytes, so there is no circular receipt-id binding; `AuthorizedBy` is ignored for authority). Require `v.IsValid()`.
3. Require `Expiry <= receipt NotAfter`; return `AuthorityWindow{IssuedAt: receipt IssuedAt, NotAfter: min(Expiry, receipt NotAfter)}`. Any failure returns an error (the grant is also re-verified for every run admission, per R4 R5).

### Algorithm: CLI replay

`devcadence benchmark replay-empirical` (exit codes **3** refused admission and **4** legacy refusal are **subcommand-local** to the `benchmark` subcommands and deliberately do not use the global `ExitCodeNotFound`(3)/`ExitCodeDrift`(4) meanings of `cmd/devcadence/main.go`; the report and help text state this) `--manifest <file> --plan <file> --authorization <file> --artifacts <dir> --criteria <file> --output <report.md> [--json <report.json>]`: build resolver over the digest-addressed `--artifacts` directory (no network), compose the production authority (`CampaignAuthority`), verifier and `empirical.NewAdmitter`, `Admit`; on refusal print the closed reason codes and exit `3`; otherwise `ReplayGate(admission, criteria)` with the pinned criteria and write the report (`Admission`, `RawGate`, `Conclusion` kept separate). Exit `0/1/2` is the raw Go/Revise/Inconclusive conclusion of the combined report. `evaluate-gate --evidence-kind empirical_campaign` exits **`4`** with a message naming `replay-empirical` (so the legacy refusal is distinguishable from Inconclusive `2` and refused admission `3`); other evidence kinds are unchanged. No flag accepts a precomputed aggregate, a summary or a boolean "verified".

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
| Check execution | `blocked` | timeout/signal/start failure: infrastructure `blocked`; any recorded exit code is a normal result | n/a | `blocked` |
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
| A1 | receipt claims pass but re-execution fails an acceptance check → verify → completed-`rejected` (quality), caught-set recomputed from probes | R1 → I1 |
| A2 | forged receipt bytes/digest mismatch → refused before verifier effects | R1 → I1 |
| A3 | plan verifier commit or profile digest differs from the running verifier → refuse | R2 → I2 |
| A4 | worker and verifier actors overlap/lineage/same invocation → refuse | R3 → I3 |
| A5 | implementation defect: probe checks pass → caught; fail → not caught; counts exact | R4 → I4 |
| A6 | review task: finding anchors inside range → caught; vague/out-of-range → not caught | R4 → I4 |
| A7 | session evidence missing, wrong digest, or authorization expired at session start → refuse; worker provenance record missing/digest-mismatching → `blocked`; record whose `Basis`/binding digest disagrees with `SessionEvidence.Endpoint`, whose stored actor id differs from the re-derivation, or whose re-derived actor equals the verifier's → refuse (no digest-only acceptance) | R3/R5 → I3/I5 |
| A8 | unknown token usage → `Known=false`; gate numeric input skipped; Inconclusive | R5 → I5 |
| A9a | campaign grant receipt valid/forged/revoked/wrong plan/expired → authority accepts only the valid one and returns the window; a detached receipt is found by subject with `AuthorizedBy` set to any string; zero-value `Verified` refused | R6 → I6 |
| A9b | a run whose `SessionEvidence.StartedAt` is before the receipt `IssuedAt` or after `min(expiry, NotAfter)` → refused by the Admitter in package `empirical`; `empirical` has no import of `receipts`/`verifier`/`actors` (package-boundary test) | R5/R6 → I5/I6 |
| A10 | process cannot start / timeout / signal → `VERIFIER_INFRASTRUCTURE` blocked; exit code ≠ expected (including a dependency/network-refused failure) → completed-rejected; original vs rerun differ → `INCONSISTENT_VERIFICATION` blocked | R7 → I7 |
| A10b | candidate touches a path outside the profile `WriteScope` (rename, delete, symlink) → rejected (scope); passes all acceptance checks but misses seeded defects → `accepted` with low caught count (defects never decide the verdict) | R4/R7 → I4/I7 |
| A10c | build info unknown (`ok=false`, as in `go test`) → refuse; injected source with matching revision and `modified=false` → proceeds; the verifier actor id equals `DeriveActorID` of its basis | R2/R3 → I2/I3 |
| A11 | `Open` for remote or CLI endpoint → `driver-not-implemented`, zero network; loopback `Open` sends only `GET /api/version` and `GET /api/tags` (httptest records exactly these two, no body); a missing/empty digest or version → empty observation and `Bind` denies; `BindingFor` of an unbound endpoint errors and every field matches the source table (`ChannelID`, `RuntimeVersion`, `ContextProfileDigest` mutation-tested) | R8 → I8 |
| A12 | loopback client refuses non-loopback host, redirect, proxy, oversize response, credential header, a tool call with a non-object `arguments` or an unlisted name; maps `/api/chat` tool calls and usage per the decision, unknown usage stays unknown (`TokenUsage` unknown form); request above `MaxRequestBytes` refused before send; `num_predict` equals `MaxOutputTokensPerCall` | R8/R11 → I8/I11 |
| A13 | `replay-empirical` with refused admission exits 3, no gate call; Inconclusive conclusion exits 2; legacy `--evidence-kind empirical_campaign` exits 4 | R9 → I9 |
| A14 | profile with `sh -c`, network-looking arg, `..` dir → load refused; verifier env contains no proxy/credential variables | R10 → I10 |
| A16 | Part 0 scenarios P0-1..P0-6 above | R11 → I11 |
| A15 | nil authority/verifier/resolver → `NewAdmitter` refused; production `ValidateAdmission` still denies | R6/R1 → I6/I1 |

The live A9 scenario of WP-M5-5 is **not** claimed by anything in this EWP; fixtures here are synthetic and prove schema and control flow only.

## Validation and Mutation Catalog

`go test -count=1 -race ./internal/cognition/drivers/... ./internal/benchmark/empirical/... ./internal/cognition/sessionclients/... ./cmd/devcadence/... ./tests/...`; `make schemas`; `make docs-check`; `make verify`. Record versions, fixture digests, check argv arrays, exit statuses and the replay-gate argv/exit.

Test seams (explicit): the loopback client is tested against an in-process `httptest` server bound to `127.0.0.1` (non-loopback and redirect cases use a second server/handler, never a real network); the verifier uses an injected `BuildInfoSource` and temporary Git repositories; infrastructure faults are injected by a `process.Runner` wrapper from `_test.go` that returns start errors/timeouts; the package-boundary tests use `go list -deps -json` to assert `empirical` imports none of `receipts`, `actors`, `verifier`, `sessionclients` and that no non-test package constructs `Options.BuildInfo` with a non-production source.

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
| Admitter ignores the window, or `empirical` imports a receipt package | A9b |
| Infra failure counted accepted/rejected-quality; disagreement ignored | A10 |
| Defect catch rate gates the verdict; write-scope read from the worker | A10b |
| Production default for `BuildInfo` is permissive | A10c |
| Remote/CLI kind opens a client | A11 |
| Client follows redirect, honors proxy, or dials non-loopback | A12 |
| Replay calls the gate despite refusal; legacy label still works or shares an exit code with Inconclusive | A13 |
| Shell or ambient env allowed in checks | A14 |
| Exported test authority reachable from production | A15 |
| Unknown usage as zero / meter treated as pre-call control | A16, A12 |
| Binding field filled with a literal instead of its source; `Open` closes or allocates per-endpoint resources | A11 |
| Worker actor taken from the digest/claim without resolving and re-deriving the provenance record | A7/A4 |

Independent lenses: Contract/Authority; Test Adequacy/Mutation. The verifier author is not a reviewer and is not the campaign author.

## Rationale, escalation and readiness

Selected: re-execution of a pinned deterministic profile by a pinned verifier on the immutable candidate, compared to the claimed receipt, versus accepting a structurally valid receipt or having the campaign author supply the verifier. Rejected: widening `TelemetrySnapshot` in place (WP-M5-5 rationale stands). Cost: profile authoring (R3-D) and a stricter, slower admission.

Escalate on: gate criteria, corpus or snapshot schema changes; a verifier needing network or shell; no deterministic probe expressible for a corpus task; build-info unavailable in the owner's build process; any request for a remote client before OWNER INPUT-2; the amendments to WP-M5-5 not being authorized.

Implementation Readiness Report:

~~~text
author tally after repair round 2 (r3) (a self-count, not evidence; independent re-verification PENDING):
requirements represented: 10 (R1-R10; R2/R3/R7/R9 tightened in r2)
acceptance scenarios mapped: 20 (A1-A16 plus A9b, A10b, A10c; Part 0 P0-1..P0-6 under A16)
unresolved architecture choices: 2 (G-5 profile content; U1 target repository), both owned by card R3-D; the loopback protocol is closed (Ollama native) with other protocols an INPUT-2 amendment
readiness: Part 0 NOT_READY pending independent review of the driver amendment; Part A NOT_READY pending Part 0 and R1-A freezes, step-0 check that `/api/tags` reports a digest and `/api/version` a version (empirical verification on the owner's runtime), and OWNER INPUT-2 for anything beyond loopback; Part B NOT_READY pending the amendments' review, R2-A and R4-A freezes
~~~

Weaker-implementer check: author expectation only, to be re-tested by the independent Implementability reviewer (Part B verifier algorithm; profile content explicitly out of scope).

## Changelog

- r3: owner review of head 106dafd: Part 0 driver usage-knownness amendment (`TokenMeasurement`, meter semantics, unknown never proves a budget, meter observes post-call, per-call bounds are the pre-call control) (item 11); `Open` does `GET /api/version` + `GET /api/tags`, `EndpointObservation`, exact `BindingFor` source table with no invented literals (4); worker provenance resolved via `ArtifactResolver` and re-derived from stored `Basis` (5); `execrt` leaf import instead of `taskexec`; exit codes 3/4 declared subcommand-local.
- r1: initial draft for window 2026-10-G.
- r2: repair round 1: receipt discovered by subject (no receipt id in signed bytes); `OperatorAuthority` returns `AuthorityWindow`, `Admitter` and `SessionEvidence` live in `empirical` (layering fixed); loopback protocol closed (Ollama `/api/chat`, tool and usage mapping) and metadata-only `Open`/`Bind` resolves the revision circularity; verdict rule, `WriteScope` in the profile, review artifact schema and anchor matching, decidable infra/quality rule, `BuildInfoSource` seam, verifier actor via `DeriveActorID`; `vcs.revision` documented as a label check; integer micro-USD; CLI exit 4 for the legacy refusal; test seams; honest readiness tally.

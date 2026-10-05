# WP-M5-5 — Empirical admission and bounded M4 re-evaluation

## Identity

- Revision: 2; task: task-m5-5-empirical-re-evaluation.
- Base: bd6c424e292815460033b2570dce4d682ef5cb73.
- Contract digest: reviewed immutable Git blob. No fictitious runtime state revision; record actual fixture prefix and accepted dependency commits at execution.
- Endpoint: competent Go/data-contract implementer with full ≥24k admission and ≥6k reserves. Live runner is a separate accepted follow-on dependency.
- Status: **DRAFT / NOT_READY; live execution BLOCKED until explicit operator authorization and real verifier/runtime evidence**.
- Scope: pure empirical evidence-admission contract, sidecar schema, bounded runbook and existing-gate replay. No provider SDK, production worker scheduler or automatic live campaign execution in this preparation window.

## Objective

Make the mandatory M4 re-evaluation meaningful: Strategy 1 versus Strategy 4 within each available authorized capability tier, real endpoint/model/session/candidate provenance, independently verified quality, honest resource unknowns, and the existing benchmark gate. A synthetic snapshot relabeled empirical must be rejected. The prior synthetic report stays intact and explicitly synthetic.

## Context Manifest

Role: evidence-contract implementer/campaign operator/independent verifier. Read: IMPLEMENTATION_PLAN M4 status/experiment/measurements/exit and M5; WP-M4-6; benchmark corpus/campaign/telemetry/gate interfaces through bounded scout; drivers and workflow authorizer signatures; WP1/2 contracts and follow-on runtime acceptance evidence.

Exact normative clauses: AGENTS §§4–9,12–15,17; ADR-0024 §§3–5,10 falsification; SECURITY §§3,7–9,14–17; ADR-0016 admission rules; full base DCI-005,011–014,019,025,032,033,041–049,080–084,090–093,100,120–124,126,129,131,134,159–161. Authority/provenance clauses are exact text admitted, not catalog shorthand.

Risks: empirical mislabeling, measurement zero substitution, failed-run denominator, spend/quota/source authority, model nondeterminism, contaminated quality verifier. Write/read separation: artifact storage has no credential/source authorization. Assumptions must be independently tested: live runtime can produce actual candidate/verification evidence; missing candidate adapter blocks, not guessed success. New endpoint/credential, changed corpus/gate/model/version/source policy, unobservable hard cap or changed criteria requires re-resolution.

## Scope envelope

Authorized implementation domains: new internal/benchmark/empirical/ pure sidecar admission/validation and tests; versioned empirical schemas and fixtures; command integration that validates admission before existing evaluate-gate **without changing its mathematical criteria**; docs/evidence/ new empirical report and run manifests only after approved live execution; owning PROTOCOLS/IMPLEMENTATION_PLAN/WORK_PACKAGES/schema README. Existing synthetic m4-evidence-report files remain historical, not overwritten.

Forbidden: provider/network client, new live task runtime, permissive fake benchmark verifier, alternate criteria to make Go, changing existing snapshots to make unknown numeric zero, automatic credential reading/login, hidden metered fallback, use of private target source without explicit authority, health/invariant weakening or same-producer self-verification.

LOCAL_DISCRETION: helper/test layout, report typography, streaming digest implementation. All field meanings, admission, denominators and stop rules below are MUST.

## Requirements and invariants

| ID | MUST requirement | Invariant |
| --- | --- | --- |
| R1 | Validate empirical sidecar and immutable artifacts before gate invocation | I1: a provenance label alone cannot establish empirical evidence |
| R2 | Pin same tasks/seeds/EWP/endpoint model configuration across Strategy 1/4 paired runs | I2: layout effect not confused with endpoint/task/decomposition changes |
| R3 | Count attempted, completed, accepted, failed, blocked and skipped separately | I3: failed/unavailable snapshots cannot count as completed quality evidence |
| R4 | Preserve measurement presence/provenance; unknown never zero | I4: unobservable token/quota/spend cannot establish efficiency |
| R5 | Bind actual candidate and independent deterministic verifier outcomes to each run | I5: text "task completed", default harness verifier or self-grading is not accepted work |
| R6 | Require fresh exact human-authorized campaign digest/caps/source/credential references before any endpoint call | I6: context/campaign spec never grants network/spend |
| R7 | Replay existing gate with its predeclared criteria and publish raw result plus admission/limits | I7: synthetic Go cannot overrule real Revise/Inconclusive |
| R8 | Bounded execution and documented missing-tier limitations; no fallback to another tier | I8: optional capability absence doesn't imply another spend path |
| R9 | Preserve synthetic report and empirical audit artifacts/versioned schemas | I9: history remains interpretable and replays reproducibly |

## Verified facts and representability gaps

| Fact / gap | Evidence at pinned base | Implication |
| --- | --- | --- |
| Current RunTask consumes model text; no model-output patch materializer | benchmark/runner.go: RunTask, BenchmarkRunner | native task adapter must be accepted first |
| Default VerificationSuite simulates seeded-defect outcomes | same file, verification seam | cannot produce empirical quality evidence |
| DriverProvider chooses driver by tier; runner has one ModelID | benchmark/campaign; runner.go | require per-run endpoint/model mapping sidecar/live composition |
| evaluate-gate accepts caller empirical label and nonempty driver after evaluation | cmd/devcadence/benchmark.go; benchmark/gate provenance | not attestation; sidecar admission required |
| TelemetrySnapshot lacks endpoint/model/status and measurement-presence fields | benchmark/telemetry | use strict sidecar, no lossy schema override |
| Aggregate includes failed snapshots; uncertainty disclosure does not prohibit Go | telemetry aggregator, gate evaluator | empirical wrapper excludes noncompleted numeric data and limits Go claims |

Facts verified by bounded scout; implementer re-verifies before action. Changing criteria or falsification interpretation needs a separate normal EWP, not a convenient adjustment here.

## Exact representation and admission interface

New schema family empirical-campaign 1.0, strict additional properties/finite numbers/no duplicate IDs, canonical digests over exact artifact bytes. New Go types belong in internal/benchmark/empirical, not a host/provider core type.

~~~go
type Measurement struct {
    Known bool
    Value *float64
    Unit string
    Provenance string
    EvidenceRef string
}
type EndpointBinding struct {
    EndpointID, DriverID, ModelID, ModelRevision, ChannelID string
    CapabilityClass string
    RuntimeVersion, ContextProfileDigest, PolicyDigest string
    SubscriptionQuotaUnit string
}
type RunEvidence struct {
    RunID, TaskID, TaskDigest, Seed, Strategy string
    Repetition int
    Endpoint EndpointBinding
    Status string
    SnapshotRef, SnapshotDigest string
    SessionEvidenceRef, PromptDigest, CandidateCommit string
    CandidateArtifactRef, CandidateArtifactDigest string
    VerifierReceiptRef, VerifierReceiptDigest string
    InvocationProducerID, VerifierProducerID string
    Accepted bool
    Measurements map[string]Measurement
}
type ClosedEWPBinding struct {
    ID string
    Version int
    RecordDigest, ContractDigest, BaseCommit string
}
type PlannedRun struct {
    Ordinal int
    RunID, TaskID, TaskDigest, Seed, Strategy string
    Repetition int
    Endpoint EndpointBinding
    EWP ClosedEWPBinding
}
type TierLimitation struct { Tier, Cause, EvidenceRef string }
type RunLimits struct {
    MaxTotalRuns, MaxCallsPerRun, MaxTotalCalls int
    MaxRunSeconds, MaxCampaignSeconds int
    MaxAPISpendUSD float64
    MaxSubscriptionCalls int
    MaxLocalComputeSeconds float64
    AllowMetered, AllowUnknownSubscriptionQuota bool
}
type CampaignPlan struct {
    SchemaVersion, CampaignID, SourceCommit, CorpusDigest, CriteriaDigest string
    RequestedTiers []string
    MissingTiers []TierLimitation
    Runs []PlannedRun
    Limits RunLimits
    AllowedSourceClasses, AllowedNetworkDomains, CredentialRefs []string
}
type CampaignManifest struct {
    SchemaVersion, CampaignID, PlanRef, PlanDigest string
    AuthorizationRef, AuthorizationDigest string
    MissingTiers []TierLimitation
    Runs []RunEvidence
    FalsificationEvidenceRef, FalsificationEvidenceDigest string
    RegenerationCommand []string
}
type ArtifactResolver interface {
    ReadVerified(context.Context, string, string) ([]byte, error)
}
type AdmissionResult struct {
    Admitted bool
    ReasonCodes []string
    CompletedRunIDs, ExcludedRunIDs []string
    Limitations []string
    ComparableResourceMetrics []string
}
type VerifiedOutcome struct {
    RunID, PlanDigest, SessionDigest, CandidateDigest, SnapshotDigest string
    ReceiptDigest, VerifierSourceCommit, VerificationProfileDigest string
    Worker, Verifier protocol.ActorProvenance
    QualityVerdict string
    SeededDefectTotal, SeededDefectsCaught int
    VerifiedCommandArtifactRefs []string
}
type IndependentVerifier interface {
    Verify(context.Context, CampaignPlan, PlannedRun, RunEvidence, ArtifactResolver) (VerifiedOutcome, error)
}
func ValidateAdmission(context.Context, CampaignManifest, ArtifactResolver, IndependentVerifier) (AdmissionResult, error)
~~~

### Exact pre-run plan identity and result matching

CampaignPlan is the sole pre-run authorized content. SchemaVersion exactly 1.0; canonical digest = protocol canonical JSON bytes of the **entire plan**, SHA-256 with sha256: prefix. Plan has no digest field, approval/authorization refs, produced result/session/receipt/timestamp fields, eliminating circularity. Record its digest in the independently issued authorization and produced manifest. Unknown fields fail. SourceCommit/CorpusDigest/CriteriaDigest/EWP record+contract digest/model revision/context profile are all explicit pinned identities.

Plan Runs order is authoritative execution order: Ordinal 1..N without gaps, stable unique RunID and unique (task,seed,tier,endpoint/config,strategy,repetition) pair keys. Same closed EWP/task/seed/model/config across strategy pair, order full_history then hybrid_4layer for repetition1 and reverse for repetition2. Each run Repetition≥1, Strategy exact enum. RequestedTiers sorted unique; MissingTiers has unique Tier from requested set, Cause closed absent/not_authorized/unavailable, nonblank verified EvidenceRef. Every requested tier either has a planned complete paired matrix or a predeclared limitation. A late availability loss appears in manifest additional limitations and failed/blocked statuses, never secretly changes the approved plan.

Plan Limits are the proposed workload envelope, not authority. Operator receipt binds exact PlanDigest and all caps; authorized caps must be no greater than plan ceilings and may be lower. If lower caps cannot complete the declared run matrix, create a new reduced plan and obtain a new receipt before any live call; never execute an implicitly truncated matrix. Plan CredentialRefs are opaque, never tokens; source/network classes/domains exact. Plan cannot authorize itself.

Each produced RunEvidence must match exactly one PlannedRun on RunID/task/digest/seed/strategy/repetition and full Endpoint binding; verify EWP binding via immutable task+plan references. No extra/unplanned/duplicate output run, changed order, model, fixture, EWP or seed admitted. Failed/interrupted/skipped results retain their planned identity. A result manifest is independent immutable post-run content; changing it never retroactively changes approved plan. Missing runs cause explicit incomplete coverage, no manufactured zeros.

EndpointBinding.SubscriptionQuotaUnit is pinned per endpoint: exact declared provider unit or literal unknown. Known subscription measurement requires a known pinned unit and direct measurement evidence; unknown unit implies Known=false. Do not use undocumented token-to-quota conversion. TierLimitation has cause and evidence, not a lossy string list.

This is a closed vocabulary, not an open measurement bag: Measurements keys exactly cumulative_input_tokens/cached_input_tokens/output_tokens/resident_peak_tokens/api_spend_usd/subscription_quota/local_compute_seconds/wall_seconds/repair_rounds/principal_reentries. Every key must exist with known or unknown. Required units are respectively token/token/token/token/USD/provider_unit/second/second/count/count. subscription_quota unit includes separately pinned provider unit string in manifest, not an invented token conversion; if provider exposes no quota meter Known=false. Integral count/token values nonnegative; spend/duration finite nonnegative. Known=true requires nonnil Value and nonempty measured provenance/EvidenceRef; Known=false requires Value=nil and provenance unknown with reason evidence ref. No estimate may masquerade as measured. Explicit measured zero is distinct from missing/unknown.

CapabilityClass exact existing values local_small/subscription_cli/frontier_api. Strategy exact wire values full_history/hybrid_4layer; no strategies 2/3 for this bounded re-evaluation. No silent enum normalization. Status closed completed/failed/blocked/cancelled/skipped. Accepted may be true only if completed and independent receipt proves accepted quality. No completed status for unavailable endpoint or missing verification.

Candidate commit and artifact digest required on implementation tasks. For review-only tasks, candidate commit identifies pinned defect-bearing input, artifact is structured review output. Receipt asserts task class and verifier rule; omission cannot be represented as empty candidate success. Session evidence must identify live endpoint/model/config, exact prompt, start/end timestamps and direct driver outcome; fake/scripted replay/test adapters are disallowed as live provenance.

VerifierReceipt artifact schema includes receipt_version, campaign_id, run_id, task_digest, seed, strategy, endpoint_binding_digest, prompt_digest, session_evidence_digest, candidate_commit, candidate_artifact_digest, snapshot_digest, verifier_producer_id, verifier_source_commit, verification_profile_digest, command_argv_arrays, command_exit_codes, output_artifact_refs/digests, passed_acceptance_ids, seeded_defect_total, seeded_defects_caught, quality_verdict and accounting_evidence_refs/digests. Required arrays are never null; commands contain fixed approved argv, not arbitrary shell. Check caller-supplied receipt by independently rerunning authorized deterministic verifier from its pinned source/profile on the immutable candidate in an isolated worktree. Merely reading a correctly formatted receipt does not establish truth.

ArtifactResolver is digest-checked local retrieval, no implicit network. Structural/digest validation is preliminary only and **never sets Admitted=true** by itself. ValidateAdmission first resolves strict plan/authorization/artifacts, matches every result to plan, then calls the separately injected IndependentVerifier for each completed result. Nil/unavailable verifier yields Admitted=false/UNVERIFIED_EMPIRICAL_OUTCOME, no gate call. No boolean verified input or untrusted receipt can bypass that step.

The trusted verifier reruns exact pinned deterministic commands/profile on the immutable candidate with isolated process/worktree authority, constructs output from direct command outcomes, and verifies receipt/session/plan/candidate/snapshot hashes. It returns VerifiedOutcome as trusted in-process output, not decoded model input. Admission cross-checks every field against plan/result/artifacts, quality verdict and seeded-defect counts. protocol.ActorProvenance supplies stable ActorID/InvocationID/Role/LineageActorIDs; Worker and Verifier must be independent under existing CheckVerification semantics, not merely different string profiles, with separate invocation and no overlapping author lineage. Unknown identity denies.

Production composition selects only an independently accepted verifier adapter bound to its implementation source/profile digest and separately granted process capability. No principal tool may register/replace verifier or claim approval. Test fakes validate schema/control-flow/zero-call denial only; they cannot establish empirical-ready or A9 live proof. Verifier implementation and protected authority ingress are follow-on prerequisites, not an omission the admission implementer may fill with accepting JSON. Before any verifier process/network effect, its own authorization/cap budget must pass, even though it does not call a model.

Receipt byte authenticity plus independent execution outcome are distinct checks. Direct verified quality failures remain completed nonaccepted outcomes; infrastructure failure becomes blocked with reason and never accepted. The verifier is not permitted to modify candidate or its judge profile to make it pass.

### Exact authorization sidecar

CampaignAuthorization schema fields: version=1.0; plan_digest (exact canonical CampaignPlan digest above); authorized_by/operator receipt reference; expiry; allowed_endpoint_bindings; allowed_source_classes; allowed_network_domains; credential_refs (opaque); max_total_runs; max_calls_per_run; max_total_calls; max_run_seconds; max_campaign_seconds; max_api_spend_usd; max_subscription_calls; max_local_compute_seconds; allow_metered=false unless explicit; allow_unknown_subscription_quota boolean; issued_policy_digest.

Every cap required and nonnegative. Zero means **no authority for that resource**, never unlimited. Hard API authorization requires independently enforceable upper charge per call, such as verified input/output limits and current exact pricing/currency. Unknown pricing, unbounded native tool use or unknown API spend prevents launching the metered run. Post hoc accounting alone is not a budget control. Subscription quota may remain unknown only when operator explicitly permits that uncertainty and bounded call/time caps can be enforced. No subscription-overage authorization unless a separate explicit metered grant. Local calls require local compute/time cap and no remote fallback.

Approval receipt uses protected operator authority verifier; request-supplied human label/path or same-OS-user-readable JSON cannot establish capability separation. Without an accepted issuer/verifier/isolation mechanism, **no live authorization exists**. This window may prepare the sidecar but cannot mint an approval or ask for credentials. Product source-exposure decisions remain owner-controlled.

### Bounded campaign protocol

Preparation plan: exactly enumerated PlannedRun IDs for all ten existing corpus tasks, Strategy 1/4, two repetitions, up to three available tiers: maximum 120 runs. Same pinned task fixture/seed/closed EWP and model quantization/context profile within pair. Predeclare seeds and paired order AB then BA per repetition to reduce ordering effect; no tuning on held-out tasks. Endpoint/source/caps may reduce tiers or run count, but reduction becomes a new approved manifest digest and limitations before execution. No implicit substitute model.

Operational ceiling: max_total_runs ≤120, max_calls_per_run ≤4, max_total_calls ≤480, max_run_seconds ≤1800, max_campaign_seconds ≤21600, one active run at a time. These are upper ceilings, **not granted spend/quota**; owner can select lower numbers, never default higher. Resume only unstarted run IDs under still-valid authorization; interrupted/uncertain live invocation is recorded blocked and not replayed until operator resolves possible prior charge/effects. No automatic repair beyond manifest call cap. Actual endpoint output may be nondeterministic; reproduction targets artifact/gate replay, not identical model text.

Per run:
1. Confirm authorization/corpus/criteria/model/config/policy digests and time; resolve endpoint auth and capability with non-secret evidence.
2. Validate remaining hard budgets and isolation/source policy; if any missing/unknown positive grant, mark blocked before endpoint call.
3. Create isolated immutable-base worktree using accepted runtime; apply seeded fixture; compile exact strategy-specific prompt with complete requirements/reserves.
4. Invoke one authorized live endpoint; record actual session/config/usage unknowns. Materialize actual candidate/review output through accepted adapter; no completion-text heuristic.
5. Independent verifier runs real pinned validation/seeded-defect checks; records commands/exit/artifacts. Producer identity differs from worker.
6. Generate typed snapshot plus sidecar status/measured-or-unknown values, bound to immutable receipt; retain failed/blocked runs as such.
7. Before next invocation recompute remaining caps; any cap/cancellation/stale authority stops. No tier/provider fallback.
8. After all authorized runs, validate empirical admission; independently regenerate snapshots/report/gate from archived artifacts.

Missing requested tiers are listed explicitly with cause absent/not_authorized/unavailable. Empty or one-sided tier cannot silently disappear from the report. An empirical campaign may report limits across available tiers but may not claim all-tier validation. M5 owner adjudicates scope limitations through normal change process.

### Admission and gate decision semantics

- Structural corruption, wrong digest, contradictory status/measurements, synthetic driver, matching producer/verifier, missing real outcome verification => admission refusal, no gate-based empirical Go published.
- At least ten **independently verified completed** runs across a fully paired set, consistent with existing gate minimum; required per-tier pairing must include both strategies on identical task/repetition/endpoint bindings. Incomplete pairs reported and excluded from efficiency/quality comparison, never treated as zero.
- completed-but-not-accepted remains a legitimate quality failure. Failed/blocked/unavailable runs remain in attempted counts/limitations, not completed-run threshold. Do not discard valid quality failures to improve acceptance rate.
- Existing numeric telemetry does not represent measurement presence. Generate numeric input to resource comparison only when both sides have compatible directly measured quantity and denominator. Unknown required token accounting => resource comparison unavailable, campaign empirical conclusion **Inconclusive**, even if unchanged raw gate returns Go.
- Invoke existing evaluate-gate only after sidecar admission, with exact current predeclared CriteriaDigest and applicable falsification artifacts. Record its raw decision/exit/stdout/artifact digests. Never edit criteria or fabricate empty/inapplicable falsifications to pass. Missing required falsification evidence => Inconclusive, not Go.
- Final empirical report separates Admission, RawGateDecision and EmpiricalConclusion. Valid observed quality/integrity/falsification failure => Revise; absent/unknown required comparable evidence => Inconclusive; Go only when admission + unchanged gate + full declared coverage/comparability all pass. Raw Go does not bypass sidecar limitations. This is an evidence-sufficiency check, not a silent change to existing mathematical thresholds.
- If this guard changes what existing tooling calls Go, synchronize owning docs and have independent review before use. A change to gate criteria or routing/profile policy requires separate EWP. Preserve raw outputs so disagreement is visible.
- Synthetic report remains unchanged; new report filename m5-m4-empirical-report.md/json includes provenance empirical_campaign plus exact live run/session/verifier identity. Synthetic Go cannot justify policy contradicting real results.

Exact existing replay argv:

~~~text
devcadence benchmark evaluate-gate --snapshots <admitted-snapshot-array.json> --criteria <predeclared-criteria.json> --evidence-kind empirical_campaign --driver <manifest-driver-set-id> --source-commit <source-sha> --regen-command <recorded-literal-argv-description> --output <new-report.md>
~~~

Repeat with --json and separate .json output. Exit 0/1/2 means raw Go/Revise/Inconclusive, never bypassing admission. Use the admitted snapshot array only; do not pass --summary or a precomputed AggregatedReport, because existing loader trusts supplied aggregates and permissive JSON. Admission checks strict schemas/duplicates and recomputes aggregates from verified complete paired snapshots. Flags also include --summary alias (forbidden here), --output, --criteria, --json, --evidence-kind, --driver, --source-commit, --regen-command. Reverify parser at execution base. No campaign execution CLI currently exists.

Criteria JSON MUST contain all six existing fields: min_completed_runs=10, min_defect_catch_rate=0.80, max_resource_ratio_versus_baseline=1.00, require_zero_falsifications=true, max_falsification_rate=0, max_resident_context_ratio_versus_baseline=1.00. These are current defaults pinned before observations; omitted fields/custom weakening forbidden. Residual-context value 0 would disable an existing criterion and is not allowed here. CriterionResult.ObservedUndefined and ResourceEfficiency.IsUndefined flags remain meaningful; numeric zero beside true flag is not a measured quantity. Gate replay compares decisions/criterion mathematics/input ReportDigest; EvaluatedAt is wall time and need not be byte-identical. Archive both originals and replay timestamps.

Current CLI does not accept a standalone falsification-map flag. An accepted empirical command wrapper must invoke EvaluateM4Gate directly with verified applicable falsifications while generating CLI-compatible input/report artifacts, or a separately reviewed additive CLI input must be provided. **This integration is an explicit blocking representability gap**, not permission to drop falsification evidence. Do not call a legacy CLI result a full delegation-floor gate when its falsification map was absent. The regenerated report records literal argv arrays, cwd class, versions and input/output digests. Human shell examples on macOS use fish set -gx syntax.

## Authority matrix

| Effect | Authority | Forbidden substitute |
| --- | --- | --- |
| Start live call/charge | Fresh exact operator receipt + deterministic budget/source/endpoint check | EWP/context admission, API-key presence |
| Authenticate | Existing supported provider session/opaque reference, separate operator flow | Credential scraping/pasting into prompt |
| Confirm empirical quality | Independent real verifier on immutable output | Driver label, model confidence, default harness |
| Admit report | Typed admission + exact artifact/provenance/measurement checks | Changing provenance string |
| Gate criteria/policy revise | Normal owner-authorized EWP | Tuning thresholds after observations |

## Missing / unknown / stale and failure semantics

| Input | Missing/unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- |
| Operator authority/price/cap | no live call | no live call | POLICY_DENIED |
| Endpoint tier | record missing/blocked, no fallback | re-probe under authority | reject binding |
| Usage/quota | Known=false, explicit reason; no efficiency proof | not current budget | integrity refusal |
| Task/candidate/receipt | no accepted empirical run | no verifier reuse | admission refused |
| Falsification/corpus/criteria | Inconclusive/no proof | new approved plan needed | refuse |
| Interrupted call | uncertain charge/effect; no automatic retry | operator reconciliation | retain trajectory |

| Failure boundary | Required postcondition | Recovery/evidence |
| --- | --- | --- |
| Before call authorization fails | zero endpoint calls/charges/worktree effects | spy + blocked receipt |
| Live invocation fails after possible charge | attempted count retained, usage unknown if unobserved | no replay/fallback |
| Candidate patch cannot apply | failed outcome, not completed accepted | actual patch/error artifacts |
| Verifier unavailable/fails | no accepted receipt; distinguish verifier infrastructure failure from valid quality fail | blocked/failed explicit |
| Crash after session before archive | run blocked/unknown, no fabricated snapshot | operator reconciliation |
| Artifact write/report response fails | immutable inputs retained; no duplicated calls | regenerate reports only |
| Gate Revise/Inconclusive or contradiction | cannot claim product validated; separate policy/criteria EWP | raw outputs + new findings |

## Traceability and acceptance scenarios

| ID | Setup → action → expected | Requirement → invariant → representation |
| --- | --- | --- |
| A1 | synthetic snapshots relabeled empirical → admission → refused before gate | R1 → I1 → sidecar/session receipt |
| A2 | per-pair endpoint/model/task/seed/profile mismatch → admit → refused/explicit incomplete coverage | R2 → I2 → EndpointBinding/pair keys |
| A3 | failed/unavailable snapshot with zero metrics → report → attempted only, no completed denominator | R3 → I3 → RunEvidence.Status |
| A4 | unknown token/quota/spend → archive/replay → null value, reason, no efficiency Go | R4 → I4 → Measurement |
| A5 | "task completed" but no actual candidate/real receipt → admit → refused | R5 → I5 → verifier artifacts |
| A6 | worker self-verifies or receipt references other candidate → admit → refused | R5 → I5 → identities/digests |
| A7 | missing/expired/wrong digest/zero cap/unknown hard API cost → run → zero live callbacks | R6 → I6 → authorization |
| A8 | subscription unavailable → run → blocked/missing tier, no metered substitute | R8 → I8 → bindings/stop rule |
| A9 | valid real paired complete measured evidence → existing gate replay → honest raw and empirical Go, independent artifact reproduction | R1/R7 → I1/I7 → admission/gate |
| A10 | valid gate Revise/Inconclusive/unknown required comparison → report → cannot claim M4 product proof | R4/R7 → I4/I7 → separate conclusion |
| A11 | caps reached/crash uncertain charge → continue → stop, no replay/fallback | R6/R8 → I6/I8 → run manifest/receipt |
| A12 | empirical report published → compare history → synthetic original intact, all unknowns/limits/provenance preserved | R9 → I9 → new report artifacts |
| A13 | change approved seed/order/model/EWP/add run or authorization/result reference → admission → plan digest/match rejection before live or gate callbacks | R2/R6 → I2/I6 → CampaignPlan/PlannedRun |
| A14 | structurally valid receipt but nil/untrusted verifier, unknown producer or wrong returned digest → admission → not admitted; zero empirical Go | R1/R5 → I1/I5 → IndependentVerifier/VerifiedOutcome |

A9 is a required future **live** acceptance scenario; synthetic fixtures test parser/guards only, never claim A9 passed.

## Validation and Mutation Catalog

Implementation: go test -count=1 -race ./internal/benchmark/empirical/... ./internal/benchmark/gate/... ./cmd/devcadence/... ; make schemas ; make docs-check ; make verify. Re-resolve absent package commands at execution gate. Live operator runs no paid test as ordinary unit test. Record exact gate CLI help/argv, versions, source/corpus/criteria/report digests, counts by status/tier, independently verified command outcomes and limitations.

Mutation review sufficient for pure admission; independent verifier must actively kill:

| Mutant | Expected failure |
| --- | --- |
| Trust empirical string/nonempty driver | A1 |
| Pair across model/task/profile changes | A2 |
| Count failed snapshots as completed | A3 |
| Unknown converted to numeric zero | A4 |
| Completion text/default verifier accepted | A5 |
| Self-verification or receipt digest ignored | A6 |
| Missing authority/price treated unlimited | A7 |
| Silent API fallback | A8 |
| Ignore failed criteria/falsification/comparability | A10 |
| Retry uncertain charged call / exceed cap | A11 |
| Overwrite synthetic report / omit limitations | A12 |
| Hash projection excludes seed/order/EWP or includes mutable authorization/results | A13 |
| Artifact-only validation admits / verifier provenance bypassed | A14 |

Dual independent implementation lenses: Contract/Authority and Test Adequacy/Mutation; campaign outcome verifier separate from worker/campaign author.

## Rationale, escalation and readiness

Selected: immutable typed sidecar admission plus unchanged gate, versus viable alternative expanding TelemetrySnapshot in place. Sidecar preserves strict historical snapshot readers and explicitly binds lifecycle/provenance; cost is another artifact pair to verify. In-place redesign could simplify querying but would require broader compatible schema and aggregation changes. Neither permits trusting a caller label or simulating quality.

Stop for missing production candidate/verifier driver, unobservable enforceable budget, policy/authority issuer not capability-separated, unknown representation, gate command mismatch or attempted change to criteria. Unanswered operator endpoint/source/spend choices are **required inputs for execution**, not ambiguities delegated to an implementer. No dollars or credentials authorized here.

Readiness: **NOT_READY** pending exact current enums/CLI binding, accepted independent empirical verifier/provider runtime, protected operator receipt, independent review and current-base gate. Requirements 9/9 mapped to 14 scenarios; live A9 unrun. Pure admission can later be frozen independently; execution cannot. The mandatory M5 re-evaluation is planned and blocked explicitly, not dropped.

## Changelog

- r1: empirical sidecar/admission design, real verifier and hard authority gates, bounded paired campaign and honest unknown/failed-run accounting.


- r2: consolidated campaign-plan and trusted-verifier repair; exact acyclic pre-run CampaignPlan, EWP/unit/tier-cause bindings and capability-bound independent verifier result API.

## Implementation record

Implemented from base `829006a` in the working tree, **without commit**: only the pure, offline admission-contract subset. **Gate exception:** the repository owner explicitly authorized this implementation while the header status stays DRAFT/NOT_READY; no independent Contract/Authority or Test Adequacy review and no separate-reviewer mutant observation has run. This record does not change the contract above. **No live campaign, endpoint call, credential, spend, operator receipt, verifier runtime or task/review executor exists or was added; M5 cannot close until the follow-on runtime window (M5-R1..R4) is accepted and the live A9 scenario passes.**

**Delivered (`internal/benchmark/empirical`, plus `protocol.ActorsIndependent`, a one-line exported wrapper of the existing review-ledger independence rule).**

- Strict types and `schema_version` 1.0 decoding (unknown fields, trailing content refused; wire names are snake_case of the field names above); `PlanDigest` (protocol canonical JSON, SHA-256) and `ValidatePlan` (ordinals, closed enums, AB/BA pair order, pair keys, EWP binding per pair, tier limitation vocabulary, operational ceilings).
- `ValidateAdmission(ctx, manifest, resolver, verifier)`: re-hashes every artifact itself (never trusts the resolver), requires the plan artifact to be the canonical encoding of its digest, validates the authorization sidecar against the plan (digest binding, caps within plan and ceilings, zero is no authority, no truncated matrix, endpoint/source/network/credential coverage, explicit metered and unknown-quota grants), matches every run to exactly one planned run (identity, endpoint, order), validates the closed ten-key measurement vocabulary (unknown is `Known=false`, nil value, never zero), binds each snapshot to measurements, then calls the injected `IndependentVerifier` per completed run and cross-checks the returned `VerifiedOutcome` (digests, plan, verdict, defect counts, `ActorProvenance` independence via the existing rule). Synthetic/scripted/fake markers in endpoint or producer identities are refused as defence in depth, not as the trust basis.
- **Fail-closed authority.** The operator-receipt check is an unexported `operatorAuthority` seam whose only production value denies (`OPERATOR_AUTHORITY_UNAVAILABLE`): the exported `ValidateAdmission` can never return `Admitted=true` today and makes zero verifier calls. Tests reach the full control flow only through the unexported `validateAdmission` with a test authority; fixtures are synthetic and prove schema/control flow, never A9.
- `ReplayGate(admission, criteria)`: refuses any result not produced by a successful admission (the admitted data is unexported), requires criteria equal to the pinned defaults and to the plan's `CriteriaDigest`, runs the **unchanged** `EvaluateM4Gate` on admitted paired snapshots only, and reports `Report{Admission, RawGate, Conclusion}`. Unknown cumulative/peak token accounting skips the numeric gate (no zeros) and concludes Inconclusive; Revise stays Revise; raw Go becomes Inconclusive without an applicable falsification entry, with incomplete coverage or with a missing tier. Counts keep planned/reported/missing/attempted/completed/accepted/failed/blocked/cancelled/skipped separate; incomplete pairs are excluded and listed.
- `tests/boundaries_test.go` `TestEmpiricalAdmissionIsPureAndOffline`.

**Requirement and acceptance coverage (admission subset).** R1/I1/A1/A14: `TestProductionAdmissionFailsClosedWithZeroVerifierCalls`, `TestNilVerifierIsNotAdmitted`, `TestVerifierFailureAndMismatchesRefuse`, synthetic cases. R2/A2/A13: `TestStructuralRefusalsMakeZeroVerifierCalls` (model/seed/order/extra/duplicate/authorization digest), `TestValidatePlan`, `TestPlanDigestCoversSeedOrderAndEWPButNotAuthorization`. R3/A3: `TestFailedBlockedAndUnpairedRunsAreCountedNotCompleted`. R4/A4/A10: measurement cases, `TestUnknownTokenAccountingNeverFeedsZerosToGate`, `TestObservedFailuresReviseAndAbsentEvidenceIsInconclusive`. R5/A5/A6: completed-without-receipt, self-verification, receipt/candidate digest cases. R6/A7/A8 (static part): zero cap, cap above plan, truncated matrix, metered without grant, unknown quota without permission. R7: `ReplayGate` tests, forged-result and changed-criteria refusals. R9/A12 (data only): the report type keeps raw gate, admission and limits separate; the synthetic report is untouched.

**Blocked / not delivered (explicit).**

1. Any live execution, per-run runtime steps 1-8, call spies, cap enforcement before a call (A7 runtime, A8 runtime, A11): no runtime exists (M5-R1/R3).
2. Operator receipt issuance and verification (M5-R4); `ValidateAdmission` therefore cannot admit in production. Authorization `expiry` is only checked as RFC3339: comparing it to run time needs session-evidence timestamps whose schema is not specified.
3. A production `IndependentVerifier`, VerifierReceipt artifact parsing and re-execution (M5-R3). `RunEvidence` carries no session-evidence digest, so the session artifact is not resolved and `VerifiedOutcome.SessionDigest` is only format-checked; the EWP does not say how to bind it.
4. CLI integration: the blocking falsification-input representability gap stands, no CLI wrapper was added, and the existing `evaluate-gate --evidence-kind empirical_campaign` still trusts the caller's label. It must not be used for empirical claims until a wrapper calls `ReplayGate`.
5. JSON Schema files and fixtures under `schemas/`, and the `m5-m4-empirical-report.md/json` artifacts (they exist only after an approved live run). Duplicate JSON keys inside the falsification map are not detected (standard decoding).

**Ambiguities and interpretations.** (1) Wire field names are snake_case of the Go fields; the plan digest depends on them. (2) Snapshot defect fields: `defect_seeded` equals `seeded_total > 0`, and a `detected`/`prevented` status requires caught == total; the snapshot cannot represent partial catches. (3) Unknown token/duration measurements must appear in the legacy snapshot as zero placeholders with `accounting_uncertain=true`; they are never listed as comparable and the gate is skipped when cumulative or peak tokens are unknown. (4) `Admitted` additionally requires at least one verified, fully paired completed run. (5) Raw Go with any missing requested tier is Inconclusive (strictest reading of "no all-tier claim"); the owner may adjudicate. (6) Pair repetitions are limited to 1 and 2. (7) Worker provenance role may be implementer or reviewer (review-only tasks). (8) `CompletedRunIDs` lists runs admitted to comparison; verified but unpaired runs are in `ExcludedRunIDs` with a limitation.

**Mutation observation.** Not observed by a separate reviewer; tests are written to fail the listed mutants but that is unverified.

# WP-M3D-1B — Portfolio planner service (invoker-driven, validator-gated)

## Identity

- Work Package ID: WP-M3D-1B (second slice of WP-M3D-1; requires merged WP-M3D-1A)
- Revision: 2 (independent readiness review: READY_WITH_FIXES; 3 majors and 11 minors incorporated)
- Task ID: autonomous-run-1 (issue #36)
- Base commit: origin/main after WP-M3D-1A merges (implementer records the SHA)
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: READY_FOR_IMPLEMENTATION once WP-M3D-1A is merged (r2)

## Objective

Provide a pure-Go planner service that (1) builds a deterministic planning prompt from facts the caller supplies, (2) calls an injected `Invoker` once, (3) strictly decodes the model's JSON output into alternatives, (4) assigns every identity/provenance/timestamp field itself, and (5) runs each alternative through the existing `cognition.PortfolioValidator`. It returns validated `PortfolioRecommendation` records (WP-M3D-1A fields) plus structured rejections. It never activates anything, never retries, never selects an endpoint and never reads credentials. "AI proposes. Deterministic machinery authorizes." (COGNITION_PORTFOLIO §10, DCI-123/124).

## Context Manifest

- role: implementer (Go), independent reviewers
- read-authority envelope: `internal/cognition/portfolio_validator*.go`, `portfolio_diagnostics.go`, `internal/protocol/portfolio.go`, `resource_inventory.go`, `internal/clock`, `internal/errs`, `tests/boundaries_test.go`, docs named below
- semantic write/scope envelope: below
- risk tags: authority (spending/privacy/credentials), untrusted model output parsing, determinism
- exact normative clauses: DCI-123, DCI-124, DCI-122, DCI-104 (capability absence degrades, not fails), DCI-129, DCI-054/055 (no provider coupling), ADR-0018 §5-6, COGNITION_PORTFOLIO §8-10
- assumptions: A1 `protocol.CanonicalJSON` is the digest basis for the inventory (portfolio_validator.go:108-111). A2 `PortfolioRecommendation` has the 1A fields and `Validate()`. A3 `clock.Clock` has `Now() time.Time`.
- re-resolution triggers: need to change validator semantics, drivers, compiler, activation, protocol types, or to read credentials/historical evidence

## Semantic scope envelope

### Authorized domains / path patterns

- NEW package `internal/cognition/planner/` (non-test and test files)
- `internal/cognition/portfolio_validator.go`: ONLY to add exported `InventoryDigest(inv *protocol.ResourceInventory) string` (returns `""` for nil, else `"sha256:"+hashBytes(CanonicalJSON(inv))`) and make the validator call it; behavior-preserving
- `tests/boundaries_test.go`: add `internal/cognition/planner` to the `core` list of `TestProviderAdaptersDoNotLeakIntoTheCore`
- `docs/COGNITION_PORTFOLIO.md` (§8/§9 status of the planner), `docs/WORK_PACKAGES.md` (M3D-1 card: 1A/1B/1C split and status)

### Explicitly forbidden semantic changes

- No change to validator rules/diagnostics, activation, routing, drivers, compiler, protocol types or schemas.
- The planner package MUST NOT import `internal/cognition/drivers`, any adapter package (`ollama`, `mlx`, `codingcli`, `remoteapi`), `internal/storage`, `internal/controlplane`, `internal/state`, `internal/events`.
- No retries, repair loops, endpoint selection, driver adapter, compiler integration, historical-evidence input, persistence or activation (these are WP-M3D-1C or later; recorded below).

### LOCAL_DISCRETION

Helper/file names inside the planner package, internal struct layout, test helper layout, prompt prose wording beyond the normative elements in REQ-04.

## Requirements

| ID | Strength | Requirement | Source |
| --- | --- | --- | --- |
| REQ-01 | MUST | Public API (names exact): `type Invoker interface { Invoke(ctx context.Context, inv Invocation) (InvocationResult, error) }`; `type Invocation struct { Prompt string; PromptDigest string }`; `type InvocationResult struct { Content string; EndpointID string; DriverID string; ModelID string }`; `type ProjectCharacteristics struct { Languages []string; RiskTags []string }`; `type Request struct { Inventory *protocol.ResourceInventory; MachineProfile *protocol.MachineCapabilityProfile; ContextProfiles map[string]*protocol.ContextProfile; BudgetStates map[string]*protocol.BudgetState; ResourceStates map[string]*protocol.ResourceState; Policy *cognition.ValidationPolicy; Project ProjectCharacteristics; Intents []protocol.RecommendationIntent; Invoker Invoker; Clock clock.Clock }`; `type Outcome string` with constants `OutcomeRecommended="recommended"`, `OutcomeAllRejected="all_rejected"`, `OutcomeNoPlanner="no_planner"`, `OutcomeInvocationFailed="invocation_failed"`, `OutcomeMalformedOutput="malformed_output"`; `type Rejected struct { Intent protocol.RecommendationIntent; Reason string; Diagnostics []cognition.PortfolioDiagnostic }`; `type Result struct { Outcome Outcome; SetID string; InventoryDigest string; PromptDigest string; Accepted []*protocol.PortfolioRecommendation; Rejected []Rejected; Detail string }`; `func Plan(ctx context.Context, req Request) (*Result, error)`; `func BuildPrompt(req Request) (prompt string, promptDigest string, err error)`. | design D-1 |
| REQ-02 | MUST | `Plan` returns a non-nil `error` ONLY for: `req.Inventory == nil`, `req.Clock == nil`, invalid `req.Intents` (unknown value or duplicates), or `ctx.Err() != nil` before or after the invocation (`errs.CategoryInvalidArgument` for the first three; for ctx return `ctx.Err()` UNWRAPPED so `errors.Is(err, context.Canceled/DeadlineExceeded)` works — `internal/errs` has no cancellation category and none may be invented), or a `BuildPrompt` failure (return it wrapped with `errs.CategoryInternal`). `BuildPrompt` validates `Inventory` and `Intents` with the same canonicalize function as `Plan` and does NOT require `Clock` or `Invoker`. Every other situation is an `Outcome` with a nil error. | DCI-104 |
| REQ-03 | MUST | `req.Invoker == nil` ⇒ `Result{Outcome: OutcomeNoPlanner}`, `Detail: "no planning endpoint available; deterministic-only"`, nothing invoked. The service selects no endpoint and defines no "planning capability": the caller binds an eligible endpoint into the `Invoker`. | DCI-104 |
| REQ-04 | MUST | `BuildPrompt` is a pure deterministic function of `Request` (same input ⇒ identical bytes and digest; no clock, no map-order dependence: all maps/slices serialized via `protocol.CanonicalJSON` or sorted). The prompt MUST contain: (a) the literal first line `devcadence-planner-prompt/1`; (b) the requested intents in canonical order (below); (c) the inventory's `CognitionEndpoints` (each serialized with `CredentialRef` blanked to ""), `Hardware`, `Profile`, `Readiness`, `Policy` as canonical JSON; (c2) IDs only, no values: the sorted key list of `ContextProfiles` with, per profile, only its `ObservedContextControl` (needed to match `AccessChannel.context_control`), and the sorted key list of `BudgetStates` (when non-nil), labelled "valid context_profile_id values" and "budget pool ids that have live state"; (d) the effective validation policy as canonical JSON, where effective = `*req.Policy` or `DefaultValidationPolicy()`, with the same back-fill the validator applies (`len(RoleRequirements)==0` ⇒ `DefaultRequirements()`); (e) `Project` characteristics; (f) the output contract: REQ-06's rules in prose PLUS a constant minimal valid example alternative (`plannerExampleAlternative`, a package-level string) that shows every required field of `CognitionPortfolio`, `AccessChannel`, `BudgetPool`, `RoleBinding`, `FallbackBinding` with their exact JSON key names from `internal/protocol` (authoritative), a nested `"schema_version":"1.0"` where the protocol requires it, and the sentence "IDs must come from the lists above"; (g) the sentence "Your output is advisory. Deterministic validation decides whether any alternative is usable." The prompt MUST NOT contain `Inventory.Credentials`, `PrincipalHosts`, any `BudgetStates`/`ResourceStates` VALUES, any `CredentialRef`, or the raw MachineProfile. `PromptDigest = "sha256:" + hex(sha256(prompt))`. | DCI-124; credential opacity |
| REQ-05 | MUST | Canonical intent order is `minimum_spend`, `balanced`, `maximum_quality_within_policy`, `privacy_first`. `req.Intents` empty ⇒ all four. Otherwise the set given (any order) is reduced to canonical order. | determinism |
| REQ-06 | MUST | Output contract (strict): the model returns one JSON object `{"alternatives":[{"intent":<intent>,"portfolio":<CognitionPortfolio object>,"rationale":<string>,"tradeoffs":[<string>,...],"confidence":"high"|"medium"|"low"}]}`. Decoding rules: (1) `len(Content) <= 262144` bytes; (2) trim whitespace; if the text starts with "```" it MUST be a single fenced block (optional label `json`) and the fences are stripped, otherwise decode the text as-is; (3) `json.Decoder` with `DisallowUnknownFields`; (4) exactly one JSON value, no trailing non-whitespace; (5) `1 <= len(alternatives) <= len(requested intents)`; (6) every `intent` valid, requested, and distinct. Any violation of (1)-(6) ⇒ whole-output `OutcomeMalformedOutput` with `Detail` starting exactly `rule N:` (N = the rule number; empty content, non-JSON text, a JSON value of the wrong type such as `[]`, and nested unknown keys are rule 3; trailing data is rule 4); `Accepted`/`Rejected` nil. Fence algorithm (rule 2): after `strings.TrimSpace` the text starts with "```"; the first line's remainder after the fence must be "" or exactly `json` (case-sensitive, trimmed); the text MUST contain a newline and its last line MUST be exactly "```" (CRLF accepted: trim `\r` from fence lines); the body is everything between; a one-line fenced form or a missing closing fence is a rule 2 failure. Text that does not start with a fence is decoded as-is (so prose before a fence ⇒ rule 3). Decode target (private): `struct{ Alternatives []struct{ Intent protocol.RecommendationIntent `json:"intent"`; Portfolio protocol.CognitionPortfolio `json:"portfolio"`; Rationale string `json:"rationale"`; Tradeoffs []string `json:"tradeoffs"`; Confidence protocol.RecommendationConfidence `json:"confidence"` } `json:"alternatives"` }`. `DisallowUnknownFields` applies recursively (through the protocol struct tags), so any unknown key, top-level or inside the portfolio, is rule 3; `encoding/json`'s case-insensitive key matching is accepted. A null/absent `portfolio` decodes to the zero value and ends as `record_invalid` or `validation_failed`. The decoded portfolio is copied into the record; nothing is aliased or retained. | untrusted output |
| REQ-07 | MUST | After a successful decode, for each alternative IN CANONICAL INTENT ORDER the service builds a `PortfolioRecommendation` itself: `SchemaVersion` = current; `SetID = "set_" + first 16 hex chars of sha256(InventoryDigest + "|" + PromptDigest + "|" + synthesizedAt)`; `RecommendationID = SetID + "_" + intent`; `InventoryDigest = cognition.InventoryDigest(req.Inventory)`; `SynthesizedAt = req.Clock.Now().UTC().Format(time.RFC3339)` sampled ONCE per `Plan` call; `RecommendedPortfolio` = the decoded portfolio with `SchemaVersion = protocol.SchemaVersion1`, `PortfolioID = SetID + "-" + intent`, `Revision = 1`, `CreatedAt = SynthesizedAt` OVERWRITTEN (planner values ignored), and `protocol.SchemaVersion1` STAMPED on any `Channels[i]` and `BudgetPools[i]` whose `SchemaVersion` is empty (a non-empty wrong value is left for validation to reject); `Rationale`, `Tradeoffs`, `Confidence`, `Intent` from the alternative; `ExplanatoryDiagnostics` and `CapabilityProvenance` empty non-nil slices; `Planner = &PlannerProvenance{EndpointID, DriverID, ModelID from InvocationResult, InvocationDigest: PromptDigest}`. An empty or whitespace-only `EndpointID` or `DriverID` in the result ⇒ ALL alternatives are Rejected with Reason `planner_provenance_incomplete`, evaluated BEFORE `rec.Validate()` (provenance may never be fabricated); `ModelID` may be empty. | DCI-129; authority |
| REQ-08 | MUST | Each built recommendation is checked with `rec.Validate()`; failure ⇒ `Rejected{Intent, Reason: "record_invalid: " + err.Error(), Diagnostics: nil}`. Otherwise `cognition.NewPortfolioValidator().Validate(ValidationInput{Portfolio: &rec.RecommendedPortfolio, Inventory, MachineProfile, ContextProfiles, BudgetStates, ResourceStates, Policy: &effectivePolicy, ExpectedInventoryDigest: InventoryDigest, Clock: <private fixed clock returning the same `now` sampled for SynthesizedAt; do NOT pass req.Clock>})` where `effectivePolicy = *req.Policy` if non-nil else `cognition.DefaultValidationPolicy()` (an explicit policy's zero-valued flags are honored; do not merge defaults into it). `!Valid` ⇒ `Rejected{Intent, Reason: "validation_failed", Diagnostics}`; valid ⇒ appended to `Accepted`. | DCI-123 |
| REQ-09 | MUST | Outcome after decode: `len(Accepted) >= 1` ⇒ `OutcomeRecommended`; else `OutcomeAllRejected`. Partial acceptance is allowed. `Accepted` and `Rejected` are ordered by canonical intent order, never by planner order, and are nil (not empty slices) when empty. Result population: `InventoryDigest` is set whenever `Inventory` is non-nil; `NoPlanner` sets only `Outcome`, `Detail`, `InventoryDigest`; `InvocationFailed` and `MalformedOutput` additionally set `PromptDigest`; `SetID` is set only after a successful decode. | determinism |
| REQ-10 | MUST | `Invoker.Invoke` is called exactly once per `Plan` (no retries). An invoker error ⇒ `OutcomeInvocationFailed` with `Detail` = the error text truncated to the largest prefix of at most 256 bytes that ends on a UTF-8 rune boundary, unless `ctx.Err() != nil` (REQ-02 applies first). | bounded behavior |
| REQ-11 | MUST NOT | The package MUST NOT activate, persist, mutate the inventory/policy/states inputs, read environment variables or files, log prompt contents, or spawn goroutines. | DCI-124/128 |
| REQ-12 | MUST | `cognition.InventoryDigest` added and used by the validator; `TestProviderAdaptersDoNotLeakIntoTheCore` covers the planner package; a new test (which MUST NOT pass vacuously: `dependenciesOf` skips on `go list` failure, so assert that a known dependency such as `internal/protocol` IS present before checking exclusions) asserts the planner package's transitive deps exclude `internal/cognition/drivers`, all adapter packages and `internal/storage|controlplane|state|events`. | DCI-054/055 |
| REQ-13 | MUST | Docs updated: planner flow status in COGNITION_PORTFOLIO §8/§9 (what exists: service; what does not: endpoint selection, driver adapter, compiler admission, historical evidence, activation link — WP-M3D-1C and later), WORK_PACKAGES M3D-1 card split. | docs sync |

## Invariants / state rules

| ID | Statement | Reqs |
| --- | --- | --- |
| INV-01 | The planner can only produce advisory records; nothing it returns is active. | REQ-11 |
| INV-02 | Every identity, timestamp, revision, digest and provenance field in an Accepted record is Go-assigned. | REQ-07 |
| INV-03 | An Accepted record's portfolio was judged `Valid` by the deterministic validator against the caller's policy and the exact inventory digest. | REQ-08 |
| INV-04 | Same inputs and same invoker output ⇒ byte-identical `Result` (clock fixed). | REQ-04, 05, 07, 09 |
| INV-05 | The prompt never contains credential inventory entries, credential refs, or live budget/resource state VALUES (ids only). | REQ-04 |

## Interface / algorithm contract

```
Plan(ctx, req):
  if req.Inventory==nil || req.Clock==nil: error(InvalidArgument)
  intents := canonicalize(req.Intents)  // error on unknown/duplicate
  if ctx.Err()!=nil: return ctx error
  if req.Invoker==nil: return {NoPlanner}
  prompt, pd := BuildPrompt(req)
  res, err := Invoker.Invoke(ctx, {prompt, pd})
  if ctx.Err()!=nil: return ctx error
  if err!=nil: return {InvocationFailed, Detail:trunc(err,256), PromptDigest:pd}
  alts, rule := decode(res.Content, intents)   // REQ-06
  if rule!="": return {MalformedOutput, Detail:rule, PromptDigest:pd}
  now := Clock.Now()  (once);  invD := InventoryDigest(inv); setID := ...
  for intent in canonical order that appears in alts:
     rec := build(...)            // REQ-07
     if provenance incomplete → Rejected(planner_provenance_incomplete)
     elif rec.Validate()!=nil → Rejected(record_invalid)
     else v := validator.Validate(...) ; valid→Accepted else Rejected(validation_failed, diags)
  Outcome := Recommended if Accepted else AllRejected
```

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Which alternatives are usable | `cognition.PortfolioValidator` under caller policy | model's `confidence`, `rationale`, `intent` |
| Record ids, revision, timestamps, provenance, inventory digest | the planner service (Go) | any value in model output |
| Which endpoint plans | caller (binds `Invoker`) | service-side ranking or model self-nomination |
| Spending / privacy / credentials | validator + policy; prompt omits credentials | prompt text instructing the model |
| Activation | `ActivationManager` (not called here) | `Plan` |

## Missing / unknown / stale input semantics

| Input | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| Inventory | error InvalidArgument | n/a | caught by validator via `ExpectedInventoryDigest` (digest computed from the same object, so staleness vs the activation-time inventory is the activation manager's job) | n/a |
| Invoker | `no_planner` | n/a | n/a | n/a |
| BudgetStates/ResourceStates | passed through to the validator unchanged (H2 default-policy rules apply, so unknown state under default policy ⇒ validation_failed) | same | n/a | n/a |
| Policy | defaults (fail-closed flags per H2) | n/a | n/a | explicit policy honored as given |
| Model output | n/a | unknown intent/field ⇒ malformed_output | n/a | REQ-06 rules |
| Clock | error InvalidArgument | n/a | n/a | n/a |

## Failure matrix

| Condition | Required postcondition | Evidence |
| --- | --- | --- |
| nil Inventory / nil Clock / duplicate or unknown requested intent | error, no invocation | ACC-02 |
| ctx cancelled before invoke / during invoke | `errors.Is(err, context.Canceled)`; invoker not called (before) / called once (during) | ACC-03 |
| nil Invoker | `no_planner`, no panic | ACC-04 |
| invoker returns error | `invocation_failed`, Detail truncated to 256 B, called exactly once | ACC-05 |
| content not JSON / unknown field / trailing data / oversize / 0 or too many alternatives / duplicate or unrequested intent | `malformed_output`, Detail starts `rule N:`, nil Accepted/Rejected | ACC-06 |
| fenced JSON with and without `json` label; fence without closing | accepted / malformed | ACC-07 |
| alternative fails validator (e.g. unknown endpoint, spend not authorized) | in `Rejected` with diagnostics; other alternatives unaffected | ACC-08 |
| every alternative fails | `all_rejected` | ACC-09 |
| model supplies portfolio_id/revision/created_at/provenance-looking text | ignored/overwritten; unknown top-level fields ⇒ malformed | ACC-10 |
| result lacks EndpointID/DriverID | alternatives Rejected `planner_provenance_incomplete` | ACC-11 |
| model omits tradeoffs/confidence for an alternative | Rejected `record_invalid` | ACC-12 |

## Representability map

| Concept | Representation | Adequacy |
| --- | --- | --- |
| alternative set | sibling `PortfolioRecommendation` with shared `SetID` (1A) | represented |
| rejection | `Rejected` (this package) | represented; not persisted |
| planner provenance | `PlannerProvenance` (1A) with `InvocationDigest = PromptDigest` | represented; digest is of the exact prompt sent (compiler-backed digest deferred to 1C) |
| inventory digest | `cognition.InventoryDigest` (new, REQ-12) | represented |

## Acceptance scenarios

Test names MUST contain `PortfolioPlanner` and the `ACC-xx` id. Use a scripted fake `Invoker` (records call count and received `Invocation`) and the existing validator test builders pattern (copy needed builders into the planner test package; do not export test helpers from `cognition`).

| ID | Setup | Action | Expected | Maps |
| --- | --- | --- | --- | --- |
| ACC-01 | valid inventory/profiles/policy (BudgetStates nil or covering every used non-local pool), fake invoker returning 4 valid alternatives in shuffled order, a fake clock with step > 0 | `Plan` | `recommended`; 4 Accepted in canonical order; shared SetID; each record passes `Validate()`; `Planner` provenance equals invoker result + `InvocationDigest == PromptDigest`; all ids/timestamps Go-assigned (EWP formulas) | REQ-05,07,09, INV-02,03 |
| ACC-02 | nil Inventory; nil Clock; `Intents: [balanced, balanced]`; unknown intent | `Plan` | error each, invoker never called | REQ-02 |
| ACC-03 | cancelled ctx before call; invoker that cancels ctx then returns | `Plan` | `errors.Is(err, context.Canceled)`; first case invoker not called; second case called exactly once | REQ-02 |
| ACC-04 | `Invoker: nil` | `Plan` | `no_planner`, nil error | REQ-03 |
| ACC-05 | invoker errors with a 1000-byte ASCII message, and one with 1000 bytes of "é" | `Plan` | `invocation_failed`; Detail ≤ 256 bytes and valid UTF-8; call count 1 | REQ-10 |
| ACC-06 | table: `not json`, extra field, trailing text after JSON, 262145-byte content, `{"alternatives":[]}`, 5 alternatives when 4 requested, duplicate intents, unrequested intent, `[]` | `Plan` | each `malformed_output` with Detail asserted by its `rule N:` prefix; Accepted/Rejected nil | REQ-06 |
| ACC-07 | content fenced with ```json, with bare ```, CRLF line endings inside a valid fence, with unclosed fence, with text before fence, one-line ```json{...}``` | `Plan` | first three decode; unclosed, text-before and one-line are malformed | REQ-06 |
| ACC-08 | 2 alternatives: one with an unknown endpoint, one valid | `Plan` | `recommended`; the bad one in Rejected with `ENDPOINT_NOT_FOUND`; good in Accepted | REQ-08,09 |
| ACC-09 | all alternatives unauthorized (e.g. metered pool with `ForbidMeteredAPI`) | `Plan` | `all_rejected`; diagnostics present | REQ-09 |
| ACC-10 | case A: model JSON includes known fields `portfolio_id:"evil"`, `revision:99`, `created_at:"1999..."`, `schema_version:"9.9"` inside the portfolio; case B: an unknown top-level key `planner`; case C: an unknown key inside the portfolio | `Plan` | A: Go values overwrite id/revision/created_at/schema_version in the Accepted record; B, C: `malformed_output` (`rule 3:`) | REQ-06,07, INV-02 |
| ACC-11 | invoker result with empty `DriverID` | `Plan` | all alternatives Rejected `planner_provenance_incomplete`; outcome `all_rejected` | REQ-07 |
| ACC-12 | alternative with empty tradeoffs array / missing confidence | `Plan` | Rejected `record_invalid`, others unaffected | REQ-08 |
| ACC-13 | same request run twice with a frozen (step 0) fake clock | `Plan` ×2 | deep-equal Results; `BuildPrompt` bytes/digest equal; shuffling map insertion order of inputs does not change prompt | INV-04, REQ-04 |
| ACC-14 | inventory with sentinel strings in `Credentials`, in a `CognitionEndpoints[i].CredentialRef`, and in `BudgetStates`/`ResourceStates` values; ContextProfiles with a known id | `BuildPrompt` | no sentinel appears; the context-profile id, its `ObservedContextControl` and the budget pool id (no values) DO appear; first line is `devcadence-planner-prompt/1`; elements (a)-(g) present; the embedded `plannerExampleAlternative` decodes strictly (`DisallowUnknownFields`) into the REQ-06 decode target and its portfolio passes `CognitionPortfolio.Validate()` after Go stamping | REQ-04, INV-05 |
| ACC-15 | default policy + non-nil `ResourceStates` with a non-nil host entry having `UnknownMetrics`; alternative otherwise valid (BudgetStates nil or covering all used non-local pools) | `Plan` | Rejected `validation_failed` with `UNKNOWN_RESOURCE_STATE` (H2 fail-closed reaches the planner) | REQ-08 |
| ACC-16 | explicit `Policy` with both known-state flags false, same input | `Plan` | alternative Accepted (explicit opt-out honored) | REQ-08 |
| ACC-17 | `cognition.InventoryDigest(nil)=="" `; digest equals what the validator reports in `ValidationResult.InventoryDigest` | unit test | equal | REQ-12 |
| ACC-18 | boundary test | `go test ./tests/...` | planner transitive deps contain none of the forbidden packages; planner listed in the core list | REQ-12 |
| ACC-19 | input immutability | deep-copy inputs before `Plan`, compare after | unchanged | REQ-11 |

## Validation

- `go build ./... && go vet ./... && gofmt -l <changed files>` clean
- `go test -count=1 ./...`; `go test -race -count=1 ./internal/cognition/... ./tests/...`
- `make hooks-check`; no hook bypass
- mutation checks the implementer MUST record (apply, show named test fails, revert): skip the model-value overwrite of `PortfolioID` ⇒ ACC-10; iterate planner order instead of canonical ⇒ ACC-01; drop `DisallowUnknownFields` ⇒ ACC-06/10; include credentials in the prompt ⇒ ACC-14; skip validator call (accept all) ⇒ ACC-08/09/15; honor `Policy` merge with defaults ⇒ ACC-16; retry on error ⇒ ACC-05; sample clock per alternative ⇒ ACC-01 (fake clock that advances) ; map-order-dependent prompt ⇒ ACC-13
- required independent review lenses: contract/authority (can the model influence anything Go must own?), test-adequacy by mutation (reviewers must run the mutants above and report any that could not be run as UNVERIFIED), untrusted-input robustness (parser edge cases, resource limits)

## Escalation triggers

Any need to touch validator semantics, drivers, compiler, activation or protocol types; a requirement that cannot be satisfied with `Clock`/`errs` as they exist; ambiguity in the cancellation error category; conflicting facts between this EWP and the 1A fields as merged.

## Design / rationale

- D-1 Invoker injection vs direct driver use: a one-method interface keeps the package free of provider/driver coupling (DCI-054/055), makes tests deterministic with a scripted fake, and defers the driver adapter and endpoint selection (which need decisions on session config, tool-less operation and the undefined "minimum planning capability") to WP-M3D-1C. Alternative considered: call `drivers.SessionDriver` directly — rejected: couples the planner to session lifecycle and forces those decisions now.
- D-2 Go-owned identity/provenance (REQ-07): the model must not be able to collide with an active portfolio id, backdate, or claim provenance. Alternative: trust model-supplied ids and validate — rejected: validation cannot prove truthful provenance.
- D-3 Whole-output rejection for structural violations vs per-alternative salvage: simpler semantics, and a model that breaks the envelope is not trustworthy for the rest of the output. Per-alternative problems (validator, record) are salvageable because they are independent.
- D-4 No retries/repair loop: bounded cost, deterministic outcomes; a repair loop is a later, explicitly budgeted WP.
- D-5 Prompt carries no credential inventory and no live state: least exposure; the validator, not the model, consumes `BudgetStates`/`ResourceStates`.
- D-6 `InvocationDigest = PromptDigest` in 1B; the compiler's `InvocationDigest` would require compiling a planner role through `compiler` (admission of DCI-123/124 clauses), deferred to 1C.
- D-7 (r2) the prompt carries ID lists (context profile ids with observed control, budget pool ids) but no state values: without ids the model cannot emit a portfolio the validator accepts; values stay out (least exposure).
- D-8 (r2) Go stamps `schema_version` on the portfolio and on empty nested channel/pool versions so models are not asked to emit bookkeeping fields; a wrong non-empty value is still rejected by validation.
- Known limits (recorded): no historical-evidence input (no type yet; M4); no activation link from `ActivationRecord` to the recommendation; role vocabulary mismatch between `cognition.Role` and compiler roles is untouched.

## Implementation Readiness Report

```text
requirements represented: 13/13
mandatory clauses resolved: 7/7
state transitions specified: n/a (pure service; outcomes enumerated)
failure cases specified: 11/11
authority decisions specified: 5/5
missing/unknown input semantics: 6/6
acceptance scenarios mapped: 19/19
unresolved architecture choices: 0
declared local-discretion choices: 4
readiness: READY_FOR_IMPLEMENTATION once 1A merges (independent review r1 READY_WITH_FIXES; findings 1-14 incorporated)
```

# Engineering Work Package: WP-M3B-5 — Doctor readiness and resource inventory (service layer, no public CLI)

- **Milestone:** M3B — Guided bootstrap and onboarding
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3b-5--doctor-readiness-and-resource-inventory-service-layer-no-public-cli](../WORK_PACKAGES.md#wp-m3b-5--doctor-readiness-and-resource-inventory-service-layer-no-public-cli)
- **Base commit:** `e2e0849` (`feat/m3b-guided-bootstrap`, includes accepted WP-M3B-1 through WP-M3B-4 checkpoints)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 (`SetupPlan`/`SetupAction`/`TypedOperation`, accepted), WP-M3B-2 (ledger/home layout, accepted), WP-M3B-3 (executor, accepted), WP-M3B-4 (`CredentialRef`/`AuthEvidence`/`Manager`, accepted).
- **Status:** EWP drafted, not yet implemented.

---

## 0. What already exists (pre-check, same pattern as WP-M3B-1 through WP-M3B-4)

Following this milestone's established discipline, `internal/setup/doctor.go` (739 lines), `internal/setup/profiles.go` (489 lines), and `internal/setup/planner.go` (577 lines) were read in full before writing this EWP. This WP's actual remaining scope is substantially narrower than the scope card reads in isolation, because most of it is already built:

### Already substantially implemented — reused, not rebuilt

1. **`Doctor.Run`** (`doctor.go`) already produces a `protocol.DoctorReport` (a real `protocol.Record`: `schema_version`, `Validate()`, registered in `schema.RecordKindToSchema`) with `EvaluationScope`, `Readiness`, `Findings`, `RecommendedProfile`, `DiscoveredEndpoints`, and `PrincipalHosts`, from state-root, Git, hardware/accelerator, principal-host, and cognition-endpoint checks.
2. **`evaluateReadiness`** (`doctor.go:605-738`) already implements the normative readiness-state machine from verified evidence, not heuristics: mandatory-dependency errors → `ACTION_REQUIRED`; no target profile → `PARTIALLY_READY`; per-deployment-profile local/remote endpoint and acceleration requirements; role-routing checks via `cognition.Route` (the same WP-M3A routing primitive, not a duplicate); stale (`stale_inference_retained`) or any-warning evidence → `READY_WITH_REDUCED_CAPABILITY`; otherwise `READY`. This is the "normative readiness states from verified evidence" deliverable, done.
3. **`ProfileRecommender.Recommend`** (`profiles.go`) already produces `protocol.ProfileRecommendation` (`SelectedProfile`/`Alternatives`/`Rationale`/`Limitations`/`Unknowns`). It uses fixed memory/accelerator thresholds (64GiB unified / 24GiB VRAM for `local-heavy`, 16GiB / 8GiB for `hybrid-thin`/`offline`) — these are UX **labels** over verified hardware facts, explicitly permitted by ADR-0014 ("Human-readable deployment labels may summarize the environment"), not the forbidden "static weighted 'optimal' portfolio logic": actual per-role eligibility for every candidate profile is checked by routing through `cognition.Route(defaultReqs[role], policy, candidateEndpoints)`, the same deterministic, policy-governed primitive `evaluateReadiness` uses — there is no separate provider/model preference table anywhere in this file. This WP's MUST constraint reads as already satisfied by the existing design; §3 below makes that assessment explicit rather than assumed, per the WP-M3B-1 review's objection to EWPs weakening acceptance criteria without evidence.
4. **`Planner.Plan(report *DoctorReport, target, profile)`** (`planner.go:106`) **already generates a `protocol.SetupPlan` from a `DoctorReport`'s findings** — this is the scope card's "`doctor --fix` generates SetupPlan from concrete missing/remediable facts" deliverable, and it already exists as a real, working implementation: it converts `FindingCodeStateDirsMissing` into managed-directory-creation `SetupAction`s, `FindingCodeGitNotFound` into a manual Git-install action, and (independent of specific finding codes, driven by the recommended/selected deployment profile and `DiscoveredEndpoints`) generates local-model-pull actions for Ollama and MLX with resolved immutable digests, declared registries/sources, and license references — i.e. it already satisfies several of WP-M3B-6's "Bounded recipes" acceptance criteria too (resolved digest before planning, license metadata present). **This EWP does not rebuild any of this.** A prior version of this WP's pre-check (recorded in `HANDOFF.md` before this EWP was written) incorrectly stated that "no `doctor --fix` plan-generation path exists" — that was wrong, confirmed by reading `planner.go` in full; this EWP corrects that record (see §0a).

### 0a. Correction to the informal pre-check recorded in `HANDOFF.md`

The `HANDOFF.md` entry written immediately after WP-M3B-4's acceptance (before this EWP existed) said: *"No `doctor --fix` plan-generation path exists... nothing converts a `DiagnosticFinding` into a `protocol.SetupPlan`/`SetupAction`."* That was based on reading only `doctor.go` and `profiles.go`, not `planner.go`, which does exactly that and was sitting in the same package the whole time. This EWP is the corrected, full-file-read assessment; `HANDOFF.md` is updated alongside this file to point here rather than repeat the stale claim (AGENTS.md §15: state the violated assumption, cite evidence, correct rather than let a wrong record stand).

### Genuinely missing — this WP's real scope

1. **No typed `ResourceInventory` protocol record exists at all.** Confirmed by `grep -rl ResourceInventory --include=*.go .` returning zero Go files, despite the type being described in `docs/PROTOCOLS.md` (`### ResourceInventory`), ADR-0014, `docs/ARCHITECTURE.md` §6.7A, and the WP-M3B-5 scope card itself. `DoctorReport` carries adjacent pieces (`DiscoveredEndpoints`, `PrincipalHosts`) but nothing unifies them, plus credential/policy metadata, into the named, schema-backed, `protocol.Record`-conformant snapshot the scope card requires and that M3C is documented to consume. This is this WP's central deliverable. See §5.
2. **`doctor.go` never references `internal/credentials`.** The scope card requires the inventory to cover "credential references." WP-M3B-4 (immediately prior, this session) built the whole `CredentialRef`/`AuthEvidence`/`Manager` substrate; nothing in `doctor.go` calls into it. This is genuinely new wiring, not a rename.
3. **A narrow gap in `planner.go`'s finding coverage.** `grep -n "AuthExpired\|NoCodingEndpoint\|AcceleratorUnverified\|EndpointUnhealthy" internal/setup/planner.go` returns nothing: `Plan` handles `FindingCodeStateDirsMissing` and `FindingCodeGitNotFound` by finding code, plus model-pull actions gated on deployment profile — but `FindingCodeAuthExpired` and `FindingCodeNoCodingEndpoint` (both emitted by `discoverEndpoints` in `doctor.go`) currently produce no corresponding remediation action, only a human-readable `Remediation` string on the finding itself (e.g. `"Re-authenticate CLI or update API credentials for %s"`). A `doctor --fix` invocation therefore cannot currently turn an auth problem into a plan action the way it already can for a missing directory or missing Git — this is a real, narrow completion of the existing pattern, not new architecture.

## 1. Objective and rationale

Add the one missing structural piece — a deterministic, schema-backed `ResourceInventory` record — and wire the two genuinely-missing integrations (credential references, auth-finding remediation) into the substantial existing `doctor.go`/`profiles.go`/`planner.go` implementation, without duplicating or redesigning what already works. `ResourceInventory` is the factual substrate ADR-0018/M3C's Portfolio Planner will consume; M3B's job (ADR-0014 §92) is to compute it deterministically from verified evidence, not to solve portfolio optimization — this WP adds no scoring, weighting, or "best" selection beyond what `ProfileRecommender` already does as a UX label.

## 2. Relevant invariants and ADRs

- **ADR-0011** (adaptive environment / host-independent cognition): facts are separate from assessment; a capability grade requires provenance. `ResourceInventory` must reference `MachineCapabilityProfile`/`CognitionEndpointSummary` data by the same provenance-carrying shapes, never re-derive its own capability grades.
- **ADR-0013** (environment intelligence and cognition contracts): "machine profiles are computed rather than persisted." `ResourceInventory` follows the same discipline — it is a point-in-time computed snapshot keyed by `machine_fingerprint` (exactly like `DoctorReport` already is), never a second source of truth for hardware facts. It references `MachineCapabilityProfile` by fingerprint, not by re-embedding `EnvironmentFacts`.
- **ADR-0014** (guided bootstrap): §92's M3B boundary line is this WP's charter directly: "Doctor computes deterministic readiness and a ResourceInventory from verified facts... M3B does not solve optimal role/provider/budget allocation with a static pure-function selector." §6's version-output-is-not-authentication invariant is already fully implemented (WP-M3B-4) and this WP's credential-reference integration must not weaken it: `ResourceInventory`'s credential entries carry `AuthEvidence.Status`, never a raw secret or a re-derived "is this actually valid" judgment.
- **ADR-0018** (adaptive cognition portfolio, M3D): `ResourceInventory` is explicitly named as M3D's Portfolio Planner's input substrate. This WP must not implement any part of the Planner itself — only the factual snapshot the (not-yet-built) Planner will later read.
- **DCI-081** (no secret custody) / **DCI-055** (runtime-neutral adapters): both already established by WP-M3B-1/4; this WP's credential-reference integration reuses `protocol.CredentialRef`/`AuthEvidence` exactly as built, adding no new secret-adjacent surface.
- **INVARIANTS.md / AGENTS.md §7**: local discretion does not include "changing persistence semantics" or "introducing new external services" — this WP adds one new record type and wiring, no new persistence layer, no new network calls beyond what `doctor.go` already makes.

## 3. MUST-constraint compliance analysis (not assumed)

The scope card's MUST is: *"no provider/model role doctrine or static weighted 'optimal' portfolio logic. AI-assisted synthesis belongs to M3D/ADR-0018."* Per the WP-M3B-1 review's precedent (an EWP must not assert compliance without evidence), here is the concrete check against the existing code this WP builds on:

- `ProfileRecommender.Recommend` does not weight or score endpoints; every profile's eligibility is a boolean AND of concrete thresholds (memory/accelerator facts) and `cognition.Route` outcomes. No endpoint or provider is preferred over another except via the operator-supplied `Policy.Preferred` list (WP-M3A, already reviewed/accepted), which orders eligible endpoints without ever making an ineligible one eligible.
- `cognition.Route` (WP-M3A) contains no hardcoded provider names; it is capability/policy-driven.
- `planner.go`'s local-model recipes treat Ollama and MLX as symmetric peers (explicitly documented at `planner.go:225-229`, citing DCI-055) — this WP's new auth-remediation action generation (§5.3) will follow the same pattern: a generic manual "re-authenticate" action shaped by the endpoint's own `Kind`/`ID`, never a hardcoded per-provider auth flow.
- `ResourceInventory` (this WP's new type) is a passive data snapshot: it has no `Recommend`/`Select`/`Score` method. Any future consumer that wants a decision reads the inventory and decides; the type itself decides nothing. This is the structural guarantee that keeps this WP on the M3B side of the M3B/M3D boundary.

## 4. Threat model & security review

| Threat Vector | Description | Mitigation |
|---|---|---|
| **1. Secret leakage via credential-reference inventory** | `ResourceInventory`'s credential section could accidentally carry a raw secret if built carelessly from ad hoc string concatenation instead of `protocol.CredentialRef`/`AuthEvidence`. | The inventory's credential entries are typed as `[]CredentialRef` / `[]AuthEvidence` — the exact WP-M3B-4 types, already proven secret-free by `Validate()` and `LooksLikeSecret()`. No new string field is introduced for credential data. |
| **2. Stale inventory presented as current** | A cached/old `ResourceInventory` could be consumed by a later caller (e.g. a future M3D Planner) believing it reflects the current machine. | `ResourceInventory` carries `machine_fingerprint` and `observed_at`, exactly like `DoctorReport` and `MachineCapabilityProfile` (ADR-0013's existing pattern) — same freshness/fingerprint discipline, no new staleness class introduced. |
| **3. Auth-remediation action over-claims authority** | A new "re-authenticate CLI" `SetupAction` could be misclassified with an authority level that lets it auto-execute a credential-mutating operation. | The new action is `AuthorityHighImpactManual` (WP-M3B-1's `IntrinsicPolicy`), matching the existing `recipe.manual.install_git` pattern exactly — manual instructions only, never an executable `TypedOperation`, since no `OpKind` for "authenticate a CLI" exists or is being added. |
| **4. ResourceInventory used to bypass the M3B/M3D boundary** | A caller could be tempted to add a "recommended portfolio" field to `ResourceInventory` for convenience, quietly reintroducing M3D logic into M3B. | Explicitly forbidden in §10 (non-goals); `ResourceInventory.Validate()` has no field that could hold a portfolio/selection decision — only observational data and the existing `ProfileRecommendation` (already a label, already reviewed as compliant). |

## 5. Proposed interfaces and types

### 5.1 `protocol.ResourceInventory` (new file `internal/protocol/resource_inventory.go`)

```go
// ResourceInventory is a deterministic, point-in-time snapshot of the
// facts M3C/M3D's Portfolio Planner will read — never a decision itself
// (ADR-0014 §92, ADR-0018). It references MachineCapabilityProfile and
// CognitionEndpointSummary by the same provenance-carrying shapes those
// types already use (ADR-0011), rather than re-deriving capability
// grades, and follows ADR-0013's "computed, not persisted" discipline:
// callers key freshness off MachineFingerprint/ObservedAt exactly as
// DoctorReport already does.
type ResourceInventory struct {
    SchemaVersion      SchemaVersion               `json:"schema_version"`
    InventoryID        string                      `json:"inventory_id"`
    MachineFingerprint string                      `json:"machine_fingerprint"`
    ObservedAt         Timestamp                    `json:"observed_at"`

    Hardware           HardwareSummary              `json:"hardware"`
    CognitionEndpoints []CognitionEndpointSummary   `json:"cognition_endpoints,omitempty"`
    PrincipalHosts     []PrincipalHostSummary       `json:"principal_hosts,omitempty"`
    Credentials        []CredentialInventoryEntry   `json:"credentials,omitempty"`
    Policy             *PolicySummary               `json:"policy,omitempty"`
}

// HardwareSummary is a compact, non-duplicative projection of
// EnvironmentFacts (never the full facts struct — those are looked up by
// MachineFingerprint when needed, per ADR-0013).
type HardwareSummary struct {
    OSFamily          OSFamily `json:"os_family"`
    Arch              string   `json:"arch"`
    LogicalCores      int      `json:"logical_cores"`
    TotalMemoryBytes  *int64   `json:"total_memory_bytes,omitempty"`
    AcceleratorBackends []BackendKind `json:"accelerator_backends,omitempty"` // SupportSupported candidates only
}

// CredentialInventoryEntry pairs a credential reference with its most
// recent authentication evidence, both exactly as WP-M3B-4 defined them
// — no new secret-adjacent field.
type CredentialInventoryEntry struct {
    Ref      CredentialRef `json:"ref"`
    Evidence AuthEvidence  `json:"evidence"`
}

// PolicySummary is the "available economic/policy metadata" the scope
// card asks for, at the fidelity that actually exists today
// (cognition.Policy, WP-M3A) — not the EconomicRegime/BudgetPool types
// M3C has not built yet. A nil Policy means no active policy override;
// callers apply cognition.DefaultPolicy() semantics, matching
// evaluateReadiness's own fallback.
type PolicySummary struct {
    MaxSourceExposure SourceExposure `json:"max_source_exposure"`
    MaxCostClass      CostClass      `json:"max_cost_class"`
}
```

`RecordKind()` → `"ResourceInventory"`; `RecordID()` → `InventoryID`; `SchemaVer()` → `SchemaVersion`. `Validate()` requires `schema_version`, non-empty `inventory_id`, a sha256-hex `machine_fingerprint` (same regex `DoctorReport` uses), non-zero `observed_at`, and delegates to each nested type's own `Validate()`/`Valid()` (`CredentialRef.Validate()`, `AuthEvidence.Validate()`, `CognitionEndpointSummary`/`PrincipalHostSummary` exactly as `DoctorReport.Validate()` already checks them).

New schema `schemas/resource-inventory.schema.json` (Draft 2020-12, mirrors the Go type field-for-field, the same twin-representation discipline as every other WP-M3B schema), registered in `internal/schema/schema.go`'s `RecordKindToSchema` **and** `AllNames()` (WP-M3B-4's follow-up review caught exactly this omission for `CredentialRef`/`AuthEvidence` — `AllNames()` is added in the same commit as the schema this time, not as a later fix). Fixtures: `fixtures/protocol/resource-inventory.valid.json`, wired into the generic round-trip suite (`tests/schema_fixtures_test.go`) the same way WP-M3B-4's fixtures were.

### 5.2 `Doctor` integration — assembling the inventory and credential wiring

A new method on the existing `Doctor` (not a new type — this is data `Doctor.Run` already has in scope):

```go
// BuildResourceInventory projects the facts and evidence Run already
// gathered into a ResourceInventory. It performs no new discovery of its
// own; it is a pure projection, called from Run after step 5
// (discoverEndpoints) so it can reuse fingerprint/endpoints/hosts
// without re-computing them.
func (d *Doctor) BuildResourceInventory(
    ctx context.Context,
    facts protocol.EnvironmentFacts,
    fingerprint string,
    endpoints []protocol.CognitionEndpointSummary,
    hosts []protocol.PrincipalHostSummary,
    refs []protocol.CredentialRef, // operator-configured references to check, if any
) (*protocol.ResourceInventory, error)
```

Credential checking reuses `credentials.Manager.CheckCredential` exactly as WP-M3B-4 built it — `Doctor` gains an optional `*credentials.Manager` field on `DoctorOptions` (nil-safe: no manager means an empty `Credentials` slice, exactly like `d.cognitionService == nil` already short-circuits `discoverEndpoints` today). `Doctor` does not invent its own credential-checking logic.

### 5.3 `Planner` auth-finding coverage (narrow extension, not new architecture)

`Planner.Plan` gains two more `if` blocks, in the same shape as its existing `FindingCodeGitNotFound` block (`planner.go:180-223`): a `FindingCodeAuthExpired` finding produces one `AuthorityHighImpactManual` manual action per affected endpoint ID (title/description derived from the finding's own `Detail`/`Remediation` text, no new hardcoded provider strings), and `FindingCodeNoCodingEndpoint` is deliberately **not** turned into an action — there is no generic "install and authenticate some coding CLI" operation to plan, and inventing one would cross into recommending a specific provider, which §3's MUST-constraint analysis forbids. This asymmetry is itself the point: `AuthExpired` names a concrete, already-configured endpoint a human can re-authenticate; `NoCodingEndpoint` does not.

## 6. Implementation strategy

1. `internal/protocol/resource_inventory.go` — types + `Validate()` + `Record` methods (§5.1).
2. `schemas/resource-inventory.schema.json` + `internal/schema/schema.go` registration (`RecordKindToSchema` and `AllNames()` together) + `internal/protocol/protocol.go`'s `NewRecord` case.
3. `fixtures/protocol/resource-inventory.valid.json` + `tests/schema_fixtures_test.go` wiring.
4. `Doctor.BuildResourceInventory` (§5.2) + `DoctorOptions.CredentialManager *credentials.Manager` (or equivalent field name) + `DoctorOptions.CredentialRefs []protocol.CredentialRef` (what to check — doctor does not invent credential references of its own).
5. Wire `BuildResourceInventory`'s result onto `DoctorReport` — either as a new optional `ResourceInventory *protocol.ResourceInventory` field on `DoctorReport` (additive, `omitempty`, no existing field changes) or returned alongside the report from a new `Doctor.RunWithInventory`/an inventory-returning variant of `Run`. Exact shape decided during implementation against what's least disruptive to `Run`'s existing callers (none yet outside tests — confirmed by `grep -rln "\.Run(ctx" internal/setup cmd/ --include=*.go`, to be re-checked at implementation time).
6. `Planner.Plan`'s two new finding-code blocks (§5.3).
7. Tests: `ResourceInventory` Go/schema parity (mirroring `TestSchemaSecretPatternParity`'s pattern), `BuildResourceInventory` unit tests (nil manager → empty credentials; a configured ref → its real `CheckCredential` result appears verbatim), `Planner.Plan` auth-finding tests (an `AuthExpired` finding produces exactly one manual action per endpoint; `NoCodingEndpoint` produces none).

## 7. Acceptance criteria (from the scope card, mapped to this WP's actual remaining work)

- **"fixtures produce stable readiness + ResourceInventory without GPU/runtime/network"** → `ResourceInventory`'s fixture and `BuildResourceInventory`'s nil-manager/no-endpoints path both produce a valid, fully-`Validate()`-passing inventory with empty `CognitionEndpoints`/`Credentials`/no accelerator backends — no field requires a live probe to be non-nil.
- **"optional resource absence degrades gracefully"** → nil `CredentialManager`, empty `refs`, and no accelerator all produce empty (not error) inventory sections, matching `discoverEndpoints`'s existing `d.cognitionService == nil` pattern.
- **"stale evidence is reported"** → `ResourceInventory.ObservedAt`/`MachineFingerprint` carry the same staleness signal `DoctorReport`/`MachineCapabilityProfile` already do; no new staleness concept invented.
- **"provider/model renaming does not create built-in role preference"** → §3's compliance analysis; no provider name appears in any new code this WP adds.
- **"no cognition endpoint remains a valid deterministic state"** *(scope card wording for: every endpoint must resolve to a concrete state, never an ambiguous default)* → `CredentialInventoryEntry.Evidence.Status` is always one of `AuthEvidenceStatus`'s four closed values (WP-M3B-4's own structural guarantee), never omitted or defaulted silently.

## 8. Non-goals

- No `EconomicRegime`/`BudgetPool`/`BudgetState` types (M3C). `PolicySummary` uses only what `cognition.Policy` already provides today.
- No Portfolio Planner, no scoring, no "best" endpoint selection (M3D/ADR-0018).
- No rebuild of `planner.go`'s existing directory/Git/model-pull recipe generation.
- No new public CLI surface (`devcadence doctor --fix` itself is WP-M3B-7's to wire; this WP is service-layer only, per the scope card's own title).
- No change to `WP-M3B-6`'s territory: whether `planner.go`'s existing recipes fully satisfy WP-M3B-6's "declared registries, immutable digests, sizes, licenses for every operation kind" acceptance criterion is WP-M3B-6's own pre-check to make, not asserted here.

## 9. Escalation conditions

- If `Doctor.Run`'s existing callers (once actually enumerated at implementation time, §6.5) require a non-additive change to accommodate the inventory, stop and record the contradiction rather than silently breaking an existing caller.
- If `ResourceInventory`'s credential section, once implemented against real `Manager.CheckCredential` output, cannot stay secret-free without weakening WP-M3B-4's guarantees, escalate rather than loosen `LooksLikeSecret`/`AuthEvidence.Validate()` again.

## 10. Base revision

Base commit: `e2e0849` on `feat/m3b-guided-bootstrap` (WP-M3B-4 accepted at `75e65a7`; this commit records that acceptance plus the corrected pre-check, §0a).

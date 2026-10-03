# Engineering Work Package: WP-M3C-4 — Substrate Integration Verification

- **Milestone:** M3C — Cognition Resource and Session Substrate
- **Scope card:** docs/WORK_PACKAGES.md#wp-m3c-4--substrate-integration-verification
- **Status:** READY_FOR_IMPLEMENTATION
- **Purpose in DevCadence self-development:** manual Principal-authored EWP; no DevCadence self-hosting/runtime enforcement is required.

## 1. Objective

Prove that the M3C-1..3 execution substrate works as one coherent deterministic system across heterogeneous driver/context/portfolio cases, including adversarial context, stale state, authority separation, missing evidence, and provider extensibility.

This WP is verification/integration work. It MUST NOT redesign accepted M3C-1..3 semantics merely to make integration tests easier.

## 2. Semantic scope envelope

### Authorized domains

- integration tests under `tests/` for the M3C substrate;
- package-local test helpers needed by those tests;
- narrowly required bug fixes inside M3C-1..3 implementation when an integration test exposes behavior that contradicts an already accepted contract;
- documentation synchronization for M3C completion status and verified behavior.

### Explicitly forbidden semantic expansion

Requires Principal amendment before implementation continues:

- new cognition-provider-specific core abstractions;
- new economic regimes or authority semantics;
- changing ContextControl / PrefixCache meaning;
- weakening mandatory-clause admission;
- changing portfolio authorization rules;
- changing accepted persistence/crash semantics from WP-M3C-3;
- adding external API/GPU requirements to deterministic tests;
- absorbing WP-M3C-5 review-ledger scope.

### LOCAL_DISCRETION

- exact test-file split;
- helper names/data builders;
- table-driven vs subtest organization;
- package-local fake driver layout;
- test fixture organization.

## 3. Material requirements

| ID | Requirement |
| --- | --- |
| REQ-01 | Verify ExactStateless and OpaqueSession driver behavior through the same canonical substrate contract. |
| REQ-02 | Verify oversized protected context fails with `CONTEXT_UNFIT` or equivalent authoritative rejection; mandatory obligations are never silently truncated. |
| REQ-03 | Verify stale normative/evidence projections and changed worktree evidence cannot be treated as current. |
| REQ-04 | Verify mandatory admission classes survive low semantic similarity and optional ranking pressure. |
| REQ-05 | Verify read authority and write authority remain independent. |
| REQ-06 | Verify endpoint-specific renderers preserve identical canonical task/contract/evidence semantics, including hostile evidence delimiters. |
| REQ-07 | Verify local, subscription, metered and mixed candidate portfolios pass/fail according to deterministic portfolio policy. |
| REQ-08 | Verify missing/unknown resource/quota evidence degrades or rejects exactly as owning contracts specify; it must never fabricate availability. |
| REQ-09 | Verify a third fake provider/driver integrates through existing interfaces without core semantic changes. |
| REQ-10 | Keep all verification deterministic and runnable without external credentials, paid APIs or real accelerators. |

## 4. Invariants / state rules

| ID | Invariant |
| --- | --- |
| INV-01 | Canonical contract semantics are renderer/driver independent. |
| INV-02 | Mandatory clauses cannot be removed by retrieval ranking, similarity, token pressure or renderer choice. |
| INV-03 | Context/evidence freshness is tied to exact source identity/revision; stale evidence cannot authorize action. |
| INV-04 | Read permission does not imply write permission, and context admission does not grant effects. |
| INV-05 | Unknown/missing capability or resource evidence cannot be upgraded to positive support. |
| INV-06 | Provider extensibility occurs through existing interfaces; a fake third provider must not require provider-specific core branching. |
| INV-07 | Integration verification must not silently modify accepted M3C semantics to make tests pass. |

## 5. Failure matrix

| Failure / condition | Required behavior |
| --- | --- |
| Contract exceeds protected endpoint envelope | deterministic rejection; no arbitrary truncation |
| Mandatory rule has poor lexical/semantic similarity | still admitted if applicability mapping requires it |
| Evidence/worktree content changes after lease/projection | stale handle rejected/invalidated |
| Opaque session usage cannot be measured exactly | uncertainty reported; no fabricated exact accounting |
| Required resource/quota fact missing | explicit unknown/degraded/rejected state per owning policy |
| Hostile evidence contains closing tags/fences | renderer preserves instruction/data boundary |
| Driver/provider is unknown to core but implements interface | participates without core provider switch |
| Integration test reveals contradiction between accepted WPs | stop and escalate with exact contradiction evidence |

## 6. Authority matrix

| Decision | Authority |
| --- | --- |
| Mandatory rule applicability | deterministic compiler mapping/closure |
| Context-fit decision | compiler + ContextProfile/accounting |
| Portfolio legality | deterministic portfolio validator |
| Effect/write authority | control-plane authorization, never context/model |
| Whether accepted M3C semantics change | Principal / amended EWP, not integration test author |
| Provider-specific adaptation | driver implementation behind accepted interface |

## 7. Missing / unknown / stale input semantics

- missing endpoint/resource evidence: never treated as supported;
- unknown quota/budget state: use explicit unknown/degraded behavior from portfolio contracts, never assume free capacity;
- stale worktree/evidence identity: reject or refresh before use;
- uncalibrated ContextProfile: provisional bounds/uncertainty only; never claim measured effectiveness;
- missing required normative mapping: fail closed / re-resolve, not “no rule applies.”

## 8. Representability map

| Concept | Representation |
| --- | --- |
| mandatory rule admission | compiler rule registry / ContextPack |
| context envelope | ContextProfile / compiler accounting |
| stale evidence | EvidenceLease/source identity |
| driver context behavior | session-driver interface + ContextControl |
| render semantics | prompt renderer interface |
| portfolio legality | PortfolioValidator / ValidationResult |
| resource uncertainty | ResourceInventory / ResourceState / BudgetState |
| provider extensibility | driver interface/registry |

If any listed concept cannot actually be represented by the current accepted implementation, stop and report the mismatch rather than inventing a local representation.

## 9. Acceptance scenarios

| ID | Scenario | Expected result |
| --- | --- | --- |
| ACC-01 | same canonical invocation rendered through two supported renderer/driver shapes | equivalent canonical obligations/evidence boundaries |
| ACC-02 | protected contract larger than endpoint envelope | deterministic rejection; no requirement dropped |
| ACC-03 | mandatory clause deliberately given low similarity score | clause remains admitted |
| ACC-04 | evidence source changes after lease/projection | stale evidence rejected/invalidated |
| ACC-05 | read-authorized path with no write authority | read succeeds where allowed; write is rejected |
| ACC-06 | hostile evidence includes apparent closing tags/fences | evidence cannot escape its data boundary |
| ACC-07 | local/subscription/metered/mixed portfolios | valid cases pass; unauthorized cases reject with deterministic diagnostics |
| ACC-08 | quota/resource state missing or unknown | explicit unknown/degraded behavior; no fabricated viability |
| ACC-09 | third fake provider implements driver contract | participates without core provider-specific code changes |
| ACC-10 | full deterministic suite | no tokens, credentials, paid network or GPU required |

## 10. Validation

Minimum evidence:

- `go test -count=1 ./...`
- `go test -race ./...`
- `go vet ./...`
- repository health/precommit verification currently required by main;
- targeted integration test evidence for ACC-01..ACC-10.

## 11. Escalation triggers

Stop implementation when:

- an accepted M3C-1..3 contract is internally contradictory;
- satisfying an integration case requires changing public/protocol/schema semantics;
- a missing state has no owning semantic;
- a provider-extensibility test requires provider-specific core branching;
- deterministic tests require real credentials/external services;
- WP-M3C-5 behavior is required to satisfy an execution-substrate acceptance case.

## 12. Implementation Readiness Report

```text
requirements represented: 10/10
invariants specified: 7/7
failure classes specified: 8/8
authority decisions specified: 6/6
missing/unknown input classes specified: 5/5
acceptance scenarios mapped: 10/10
unresolved architecture choices: 0
local-discretion classes: 5
readiness: READY_FOR_IMPLEMENTATION
```

Weaker-implementer check: **PASS**. The implementer should be writing/integrating tests and correcting contract violations, not inventing M3C architecture.

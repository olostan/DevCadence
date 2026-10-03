# ADR-0020 — Cognitive Invocation Compiler, Deterministic Rule Admission, and Review Ledger

- **Status:** Accepted
- **Date:** 2026-09-30
- **Owners:** DevCadence architecture / cognition substrate
- **Related:** ADR-0016, ADR-0018, ADR-0019
- **Related invariants:** DCI-014, DCI-018, DCI-019, DCI-046–049, DCI-104, DCI-120–130
- **Primary implementation milestones:** M3C, M4, M7
- **Normative effect:** DCI-131–135 and this ADR are the accepted candidate architecture; merging PR #17 publishes that accepted state to `main`.

## Context

DevCadence deliberately accumulates durable knowledge: requirements, invariants, ADRs, Engineering Work Packages, project state, review findings, evidence, and learned calibration. That growth is desirable for the **control plane** but dangerous if every cognition invocation is expected to read and reason over the whole corpus.

There are two symmetric failure modes:

1. **Under-admission:** a required rule, risk, dependency, prior finding, or evidence object is omitted, so a model makes a locally plausible but globally invalid decision.
2. **Over-admission:** the model receives a large rule/document corpus, spends attention on process rather than the engineering problem, loses important constraints among distractors, exceeds an effective context envelope, or becomes more likely to ignore/confuse instructions.

Nominal context capacity is not equivalent to reliable usable context. Long-context evaluations such as *Lost in the Middle* (TACL 2024) and RULER show that retrieval/reasoning quality can degrade with position, task complexity, and sequence length even when the advertised context window is not exceeded. Prompt-template studies also show format sensitivity, with smaller/weaker models generally less robust than stronger models. These findings do not dictate one fixed budget or prompt syntax; they justify measuring **effective workload envelopes** and avoiding unnecessary residency.

The same problem appears in review. A conversational sequence such as:

`review → author reply → repair → broad re-review → another reply → another broad review`

causes duplicated context, rediscovery, phrasing drift, and repeated token spend. Review findings become prose conversation rather than durable state. The implementer may claim "addressed" even though only an independent verifier can establish whether the resolution is correct.

The architectural requirement is therefore:

> **System/process complexity may grow; model-visible complexity must remain bounded and task-specific.**

DevCadence should compile the smallest complete invocation contract that is safe for the current action rather than teaching each model how the whole system works.

## Decision

DevCadence will introduce a **Cognitive Invocation Compiler** (also referred to as the Context Compiler where the narrower context-only meaning is clear) and a **Review Ledger**.

The compiler is deterministic where authority/applicability is concerned and retrieval-assisted where relevance discovery is concerned. The ledger stores review state so review conversations do not become the source of truth.

### 1. Control-plane rules are not model instructions by default

A rule belongs in the model-visible prompt only when the model must use it to make the current semantic decision.

Rules that the Go control plane can enforce mechanically — lifecycle transitions, authority checks, candidate identity, budget ceilings, required-field completeness, reviewer independence scheduling, retry bounds, closure conditions — SHOULD remain outside the prompt.

The runtime enforces them and supplies only the consequences relevant to the current action.

Example:

- Bad: every implementer prompt explains the full ReviewFinding state machine.
- Good: the runtime gives the implementer two accepted findings and asks for a fix or challenge for each; the runtime itself prevents the implementer from marking its own fix verified.

### 2. Mandatory applicability is a predicate, not a relevance score

Execution-critical MUST/MUST-NOT clauses are admitted through deterministic applicability mapping and dependency closure.

Embedding similarity, BM25, reranking, or an LLM MAY help retrieve optional/background material but MUST NOT be the mechanism that decides whether a mandatory clause applies.

A semantically distant safety or spending rule can still be binding. Conversely, a highly similar ADR paragraph may be rationale rather than authority.

To reduce exposure to **mis-mapped** rules while deterministically catching orphaned rules, every execution-critical clause belongs to a deterministic admission class:

- `always`: a very small project-wide authority floor that is present in every cognition invocation;
- `capability_default`: admitted whenever the invocation can exercise the corresponding authority class (for example repository mutation, credentials, network access, spending, or durable-state mutation); exclusion requires an explicit revision-pinned not-applicable mapping, never merely the absence of a tag;
- `mapped`: admitted through the normal task/role/action/path/domain/risk mappings and dependency closure.

A reverse-coverage linter rejects any mandatory clause that has no deterministic admission path. That proves only **non-orphaning**, not semantic mapping correctness: a clause mapped only to the wrong domain can still pass reverse coverage. Mapping correctness is validated separately by Contract Completeness Review and M4's independent held-out/seeded applicability audit. This split keeps cross-cutting authority fail-safe without pretending static reachability proves the mapping is correct.

The admission pipeline is therefore ordered:

~~~text
task + role + EWP + paths + domains + risks + action + project state
                              |
                              v
                 deterministic applicability
                              |
                     mandatory clause set
                              |
                    dependency closure
                              |
         +--------------------+--------------------+
         |                                         |
         v                                         v
 exact/lexical retrieval                    dense retrieval
 symbols, IDs, paths                         semantic neighbors
         |                                         |
         +--------------------+--------------------+
                              |
                    optional reranking
                              |
                 redundancy/token packing
                              |
              endpoint-specific rendering
                              |
                       ContextPack
~~~

Mandatory objects survive ranking and token pressure. Before M4 has calibrated an endpoint, M3C uses a **provisional ContextProfile** derived from the runtime/declared hard window, explicit output/reasoning/tool reserves, configured policy ceilings, and conservative accounting uncertainty. A provisional target is configuration, not an empirical effectiveness claim; there is no universal fixed percentage such as 70%. If the complete mandatory pack cannot fit the applicable hard/provisional bound, the compiler returns `CONTEXT_UNFIT`; it does not drop requirements. Policies that require empirically demonstrated reliability may reject an uncalibrated endpoint rather than pretending the provisional profile is verified.

### 2A. Layered Authority Hierarchy and Effect Capability Model

Authority in DevCadence is not a monolithic flat list of engine rules. It forms a strict **layered authority hierarchy**:

```text
DevCadence system invariants (engine rules)
        +
organization/team policy [future]
        +
project-local invariants (.devcadence/INVARIANTS.md, target codebase rules)
        +
current EWP/task constraints (narrow temporary execution authority)
        ↓
deterministic composition
        ↓
frozen authority snapshot
        ↓
Cognitive Invocation Compiler
```

1. **Layered Authority Hierarchy**:
   - **System Invariants (`system`)**: Universal DevCadence invariants governing control-plane safety, evidence integrity, model boundaries, and isolated execution (94 DevCadence DCI invariants from `INVARIANTS.md`: DCI-001 through DCI-135, with intentional gaps). Constructed by `NewCanonicalRuleRegistry()`.
   - **Organization Policy (`organization`)**: Enterprise or team-level governance policies (e.g. licensing restrictions, approved dependency registries, mandatory internal auditing). [Deferred to future enterprise milestones].
   - **Project-Local Invariants (`project`)**: Codebase-specific durable rules located in the target project's committed canonical baseline (e.g. `.devcadence/INVARIANTS.md`), discovered and governed during M6 Project Adoption.
   - **Task Constraints (`task`)**: Narrow, temporary execution contracts defined in the active Engineering Work Package.
   - **Composition Invariant**: Lower authority layers may add further restrictions but MUST NOT silently weaken or contradict higher-authority constraints; detected contradictions fail closed.

2. **Authority Provenance: Current Implementation vs. Future Contract**:
   - **Current M3C Implementation**: The compiler operates on the DevCadence system catalog (`NewCanonicalRuleRegistry()`), embedding an explicit `SourceKind` seam (`system`, `organization`, `project`, `task`) directly on every `Rule`. Rules carry concrete source provenance (`SourceKind`, `SourceDoc`, `Revision`, `ContentDigest`), authenticated cryptographically by `CatalogDigest`.
   - **Planned M6 / Future Contract**: In Milestone M6 (Project Adoption) and future distributed topologies, the authority model expands into the full `AuthoritySource` descriptor (`source_kind`, `source_id`, `revision`, `digest`), multi-source deterministic composition (System + Org + Project + Task), and automated non-weakening conflict checking across disparate authority catalogs.

3. **Capability Model as Effect Authority**:
   The capability system is **Effect Authority** (what real-world effects this invocation can cause: `write`, `exec`, `credentials`, `network`, `spending`, `durable_state_mutation`), NOT enterprise IAM or human user authentication.
   - The deterministic control plane determines which capabilities are attached to an execution context (e.g. worktree write access, tool availability, access channels) and automatically admits the corresponding mandatory rules (`capability_default`).
   - Models never infer, negotiate, or grant their own authority.

4. **Path Toward Distributed / Multi-User DevCadence**:
   While M3C implements local-first execution, the architecture preserves clean seams for future distributed development (multiple developers, remote cognition workers, CI agents, delegated reviewers, and varied privilege tiers):
   - Authority provenance is explicitly layered and tracked per rule.
   - Capability authority is strictly control-plane-owned and verified.
   - The `ContextManifest` and authority provenance model accommodate future actor/identity dimensions without structural redesign or premature IAM complexity.

### 3. Hybrid retrieval, not embeddings alone

For non-mandatory discovery the compiler SHOULD combine:

- exact identifiers and lexical search (symbols, paths, requirement IDs, schema names);
- explicit dependency graph edges (rule → rule, rule → ADR, schema → protocol, finding → requirement);
- domain/risk/action metadata;
- dense semantic retrieval;
- optional reranking;
- freshness/revision compatibility;
- redundancy and token cost.

Embeddings are a **recall mechanism**, not authority.

M3C's required baseline is deterministic metadata plus lexical/exact and dependency-graph retrieval. Dense embeddings/reranking are **not an M3C implementation requirement**; M4 may prototype and compare them, and they graduate into production only if they materially improve accepted quality/resource use without weakening mandatory-rule recall.

### 4. Inline obligations; reference rationale; lease evidence

References alone are not sufficient for an execution-critical requirement a model must obey. The compiler uses three residency classes:

1. **Operative obligation:** inline exact clause text plus stable ID/revision/digest.
2. **Supporting rationale/reference:** compact handle; retrieved only if needed.
3. **Large evidence:** content-addressed EvidenceLease admitted for a concrete question.

This preserves DCI-019 while avoiding whole-document residency.

### 5. Model-facing rendering is an adapter concern

Canonical state remains typed Go / JSON Schema. The model-facing representation is rendered per endpoint/profile.

DevCadence MUST NOT assume one universal best format such as YAML, JSON, Markdown, or XML.

Default human-readable prompts SHOULD use compact, explicit sections/tags so task, contract, state, evidence, and action are visibly separated. Strict JSON is preferred for machine-validated **outputs** when the endpoint supports reliable structured decoding.

M4 benchmarks prompt renderers by endpoint/configuration/workload. A renderer change is empirical configuration, not a new source of authority.

### 6. Context profiles include effective cognitive envelopes

ContextProfile calibration SHOULD eventually cover more than nominal token capacity.

Useful empirical dimensions include:

- reliable resident-token envelope by workload;
- active normative-clause count;
- number of independent evidence objects;
- cross-file/dependency depth;
- structured-output reliability;
- retry/review task performance;
- prompt-renderer format;
- runtime/model/quantization/configuration identity.

These are evidence-backed routing inputs, not universal hard-coded limits.

### 7. Review findings become durable objects, not chat history

DevCadence already has durable `ReviewCampaign`, `FindingDisposition`, and `ClosureDecision` records plus raw findings embedded in `ReviewResult`. ADR-0020 **extends this lineage; it does not replace or fork it**.

The intended flow is:

~~~text
ReviewResult.findings (reviewer observations)
              |
              v
      normalized ReviewFinding
              |
              v
 existing FindingDisposition
              |
       +------+------+
       |             |
    FIX_NOW       reject/defer/
       |          duplicate/human
       v
 FindingResolution
 (fix_attempted | challenge)
       |
       v
 ResolutionVerification
       |
       v
 existing ClosureDecision
~~~

A ReviewCampaign remains anchored to immutable candidate identity and Work Package/contract revision.

A normalized **ReviewFinding** supplies the stable identity that the existing `ReviewCampaign.finding_refs` and `FindingDisposition.finding_id` already anticipate. Its minimum semantic content is:

- finding ID;
- candidate/contract identity;
- `severity` compatible with the existing durable vocabulary (`info | low | medium | high | critical`);
- `materiality` compatible with the existing durable vocabulary (`blocking | material_non_blocking | opportunistic`);
- optional epistemic `confidence` (`high | medium | low`);
- claim and evidence;
- applicable requirement/invariant when known;
- impact/why-now;
- verification method;
- source reviewer/lens;
- normalization links to raw ReviewResult observations;
- status.

These dimensions are intentionally distinct: **severity** is the harm if the claim is true; **materiality** is whether/how it affects the current candidate/campaign; **confidence** is the strength of the evidence. `FindingDisposition` must carry the normalized finding's severity/materiality values; any reclassification happens during normalization/adjudication with rationale rather than through a second vocabulary hidden in the prompt.

### 8. Resolution and verification are separate authorities

For a finding whose existing `FindingDisposition` requires current repair, the implementer may produce a **FindingResolution**:

- `fix_attempted` with candidate commit/evidence; or
- `challenge` with contradiction rationale/evidence.

The implementer cannot mark a finding verified. The durable `FindingResolution` records producer + invocation provenance, and `ResolutionVerification` records verifier + invocation provenance, so the control plane can reject logical self-verification across clean sessions even when the visible GitHub/user account is the same. A verified challenge does not silently mutate historical `FindingDisposition` evidence; the campaign records the verification and, where policy requires a changed adjudication, appends the appropriate new/superseding decision record according to the durable compatibility rules.

A clean independent verifier evaluates an attempted fix. A challenged finding is evaluated in an unbiased adjudication context containing the original claim/evidence, the challenge argument, and the applicable normative material — not the accumulated conversation transcript. The cognitive verifier is **blinded by default** to the identities/model/provider of the original reviewer and challenger; independence/capability provenance is checked by the control plane and is exposed to the verifier only when identity itself is materially relevant evidence.

Verification outcomes are conceptually:

- `verified_fixed`;
- `verified_dismissed`;
- `re_adjudication_required`.

A verifier never accepts deferred risk. If verification supports deferral or another disposition change, `re_adjudication_required` carries the evidence back to the authorized Principal/Human, who must issue a new/superseding `FindingDisposition`. A deferral becomes closure-eligible only after that authorized disposition supplies the existing deferred-target, safety/accepted-risk evidence and reconsideration trigger. "Future work" without ownership is not closure.

The exact durable schema/state machine belongs to its implementation WP; this ADR owns the authority split.

### 9. Review verification is focused; broad rediscovery is exceptional

The default review shape is:

~~~text
immutable candidate
      |
      +--> independent reviewer/lens A
      +--> independent reviewer/lens B
      +--> optional additional required lenses
      |
      v
finding normalization / deduplication
      |
      v
single repair packet
      |
      v
per-finding independent verification
      |
      v
one closure review
~~~

Focused verification asks whether accepted findings were resolved and whether the repair introduced a material regression. It is not another unrestricted hunt for improvements.

A closure review may add a new finding only when it crosses the closure threshold and records an origin such as:

- `repair_regression`;
- `previously_missed_material_defect`;
- `contract_change`;
- `new_evidence`.

Equivalent restatements of already adjudicated findings do not reopen the campaign.

### 10. Contract completeness review precedes implementation

[ADR-0024](0024-implementation-ready-work-packages-and-contract-completeness.md) generalizes the earlier systemic-protocol Contract Completeness Review into an **Implementation Readiness Gate for all substantial delegated EWPs**.

The Cognitive Invocation Compiler may prove that known mandatory clauses are admitted, fresh and within the endpoint envelope. It cannot prove that the Principal closed every implementation-critical semantic. Before delegation, the Principal therefore closes requirement → invariant/state rule → exact representation → acceptance scenario, plus applicable failure, authority and missing/unknown-input semantics.

For systemic/stateful/high-risk work, missing representation or unresolved architecture is a hard NOT READY result. The worker does not reconstruct the missing design from the broader corpus. This keeps the compiler's role clean: deterministic admission of a closed contract, not speculative completion of an incomplete one.

### 11. The runtime reports state; agents do not self-declare completion

Agents may report attempted work and evidence. DevCadence computes campaign state.

Instead of prose such as "all review comments addressed", the control plane can report:

~~~text
Findings: 8
verified_fixed: 5
fix_attempted: 2
challenge_pending: 1
blocking unresolved: 3
~~~

This is the same architectural principle as "AI recommends; deterministic machinery authorizes."

### 12. Telemetry measures whether this architecture helps

M4 MUST evaluate this design against simpler baselines.

Measure at least:

- total and resident input tokens;
- cached/output tokens where observable;
- model-visible normative tokens;
- number of active rules/evidence objects;
- retrieval expansions/restarts;
- false-negative mandatory-rule admissions;
- false-positive/irrelevant admissions;
- review findings, duplicates, repair rounds, verification failures;
- newly discovered material findings during closure;
- accepted defect yield and regression rate;
- latency and monetary/subscription resource consumption;
- performance by endpoint/configuration/prompt renderer.

Do not optimize token count at the expense of correctness. The target is **high relevant-signal density under complete authority**, not the smallest prompt.

## Prompt projection

Renderer boundaries are security boundaries: untrusted source/evidence text must be escaped, encoded, length-delimited, or otherwise structurally represented so content such as `</contract>` or Markdown fences cannot terminate an instruction block or become control text.

A typical model-visible projection should be simple even when the underlying control plane is sophisticated:

~~~text
<task>
Implement explicit fallback validation.
</task>

<contract>
MUST [DCI-104 @ rev/digest]: ...
MUST [FR-062 clause @ rev/digest]: ...
WRITE: internal/protocol/portfolio.go, tests
</contract>

<state>
base: ...
candidate: ...
</state>

<evidence>
...bounded selected source/evidence...
</evidence>

<action>
Implement the task and return the requested structured result.
</action>
~~~

This is illustrative, not a mandated universal serialization.

## Consequences

### Positive

- Hundreds of durable rules can exist without hundreds of resident instructions.
- Small/local models can receive tasks sized to their effective reasoning envelope.
- Missing mandatory rules fail closed rather than being silently ranked away.
- Review state survives conversation boundaries.
- Repair verification becomes cheaper and less biased.
- Prompt rendering can evolve empirically without changing canonical authority.

### Costs

- The control plane must maintain applicability metadata and dependency freshness.
- Retrieval and ranking introduce new observable failure modes.
- Rule/index drift requires deterministic lint and ownership.
- Model-specific prompt rendering/calibration adds configuration surface.
- Structured review state adds persistence/orchestration work.

### Risks and mitigations

- **False-negative rule mapping:** fail closed on unknown applicability; expand mapping or escalate.
- **Embedding miss:** embeddings cannot remove mandatory clauses; lexical/graph retrieval remains available.
- **Over-admission:** budget optional evidence/rationale; preserve mandatory set and reserves.
- **Prompt-format overfitting:** benchmark per endpoint; canonical semantics stay format-neutral.
- **Review bureaucracy:** keep model-facing finding/resolution contracts minimal; control plane owns lifecycle complexity.
- **Premature optimization:** M4 determines whether embeddings, rerankers, richer taxonomies, or extra review roles are worth their cost.

## Milestone alignment

### M3C

Implement the deterministic Cognitive Invocation Compiler foundation:

- applicability metadata and clause dependency resolution;
- mandatory admission and `CONTEXT_UNFIT`;
- hybrid retrieval interfaces (lexical/graph required; dense retrieval optional);
- context packing/leases/reserves;
- endpoint-specific prompt renderer interface.

A separate **WP-M3C-5** implements the compact review finding/resolution/verification protocol so review-ledger state does not inflate the session/compiler implementation package.

### M3D

Use the compiler from adaptive portfolio/workflow synthesis. Planner-generated workflows request cognition; they do not manually assemble giant prompts.

### M4

Benchmark effective context/cognitive envelopes, renderer formats, retrieval strategies, and review convergence/token efficiency. Mandatory-admission ground truth must be independent of the compiler under test: include seeded omitted/mis-mapped rule cases and held-out tasks whose applicable mandatory set is established by an independent full-corpus Principal/human audit. Report false-negative mandatory omissions separately from irrelevant optional admissions. Dense embeddings/reranking remain optional until they demonstrate value.

### M7

Expand structured review campaigns to multi-review fan-out, dynamic lenses, active falsification, aggregation, and richer campaign automation. M7 builds on the ledger semantics rather than using conversational transcripts as state.

## External evidence informing the decision

These sources motivate measurement and bounded relevant context; they are evidence, not normative authority:

- Liu et al., *Lost in the Middle: How Language Models Use Long Contexts*, TACL 2024: https://aclanthology.org/2024.tacl-1.9/
- Hsieh et al., *RULER: What's the Real Context Size of Your Long-Context Language Models?*, 2024: https://arxiv.org/abs/2404.06654
- He et al., *Does Prompt Formatting Have Any Impact on LLM Performance?*, 2024: https://arxiv.org/abs/2411.10541

## Rejected alternatives

### Load all normative documents

Rejected. Authority does not require residency, and large instruction corpora consume attention/tokens while still providing no deterministic guarantee that the model notices the right rule.

### Dense top-K over every rule

Rejected as an authority mechanism. Similarity is not applicability and cannot safely discard MUST/MUST-NOT clauses.

### Hard-code one prompt serialization

Rejected. Format sensitivity varies across models and tasks; canonical protocol must remain renderer-neutral.

### Keep review as comments/transcripts only

Rejected. Conversation is useful evidence but is a poor canonical state store for finding identity, resolution authority, verification, deduplication, and closure.

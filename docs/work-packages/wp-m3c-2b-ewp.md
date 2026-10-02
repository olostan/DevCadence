# Engineering Work Package: WP-M3C-2B — Cognitive Invocation Compiler

- **Milestone:** M3C — Cognition Resource and Session Substrate
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3c-2--session-drivers-and-cognitive-invocation-compiler](../WORK_PACKAGES.md#wp-m3c-2--session-drivers-and-cognitive-invocation-compiler)
- **Base commit:** `d99e40c2107965b5af53a8c429aa6286f430ff8f` (origin/main, merge of PR #18 — Session Execution Substrate)
- **Branch:** `feat/m3c-2b-cognitive-compiler`
- **Task ID:** `task-m3c-2b-cognitive-compiler`
- **Work Package ID:** `WP-M3C-2B`
- **Version:** 1.0
- **Status:** Approved for Implementation

---

## 1. Context Manifest (AGENTS.md §2, docs/PROTOCOLS.md §10B)

```json
{
  "manifest_id": "manifest-wp-m3c-2b-v1",
  "task_id": "task-m3c-2b-cognitive-compiler",
  "work_package_id": "WP-M3C-2B",
  "work_package_revision": 1,
  "role": "principal_engineer",
  "base_commit": "d99e40c2107965b5af53a8c429aa6286f430ff8f",
  "project_state_revision": "bootstrap-m3c-2a-closed",
  "read_envelope": [
    "AGENTS.md",
    "INVARIANTS.md",
    "docs/README.md",
    "docs/WORK_PACKAGES.md",
    "docs/PROTOCOLS.md",
    "docs/IMPLEMENTATION_PLAN.md",
    "docs/adr/0016-validation-services-bounded-tools-and-context-compaction.md",
    "docs/adr/0018-adaptive-cognition-portfolio-and-workflow-synthesis.md",
    "docs/adr/0019-non-conversational-cognition-and-adaptive-review.md",
    "docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md",
    "docs/work-packages/wp-m3c-1-ewp.md",
    "docs/work-packages/wp-m3c-2a-ewp.md",
    "internal/errs/errs.go",
    "internal/protocol/access_channel.go",
    "internal/protocol/context.go",
    "internal/protocol/economics.go",
    "internal/protocol/portfolio.go",
    "internal/tools/scope.go",
    "internal/cognition/drivers/*"
  ],
  "write_scope": [
    "internal/cognition/compiler/*",
    "internal/errs/errs.go",
    "internal/errs/errs_test.go",
    "docs/work-packages/wp-m3c-2b-ewp.md"
  ],
  "domains": [
    "cognitive_compiler",
    "context_admission",
    "deterministic_rule_mapping",
    "context_profiling",
    "prompt_projection",
    "evidence_leasing",
    "state_capsule",
    "optional_retrieval"
  ],
  "risk_tags": [
    "unadmitted_mandatory_clauses",
    "context_overflow",
    "delimiter_injection_escape",
    "orphan_rule_mapping",
    "evidence_staleness"
  ],
  "mandatory_clauses": [
    {
      "clause_id": "DCI-014",
      "source_doc": "INVARIANTS.md",
      "summary": "Evidence depth is progressive: retrieve exact evidence progressively rather than preloading whole files."
    },
    {
      "clause_id": "DCI-018",
      "source_doc": "INVARIANTS.md",
      "summary": "Authority does not imply residency: normative rules must not be preloaded wholesale."
    },
    {
      "clause_id": "DCI-019",
      "source_doc": "INVARIANTS.md",
      "summary": "Delegation must have no hidden requirements: return CONTEXT_UNFIT if budget exceeded rather than truncating."
    },
    {
      "clause_id": "DCI-045",
      "source_doc": "INVARIANTS.md",
      "summary": "Repeated failed attempts trigger escalation. Retries bounded by policy."
    },
    {
      "clause_id": "DCI-131",
      "source_doc": "INVARIANTS.md",
      "summary": "Control-plane complexity does not imply prompt complexity: deterministic control plane enforces bounds outside prompts."
    },
    {
      "clause_id": "DCI-132",
      "source_doc": "INVARIANTS.md",
      "summary": "Mandatory applicability is never similarity-ranked away: deterministic admission classes plus dependency closure."
    },
    {
      "clause_id": "DCI-133",
      "source_doc": "INVARIANTS.md",
      "summary": "Operative obligations are resident; rationale is retrievable; prompt renderers enforce delimiter safety."
    },
    {
      "clause_id": "ADR-0020-S1",
      "source_doc": "docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md",
      "summary": "Control-plane rules are not model instructions by default: keep mechanical lifecycle outside prompt."
    },
    {
      "clause_id": "ADR-0020-S2",
      "source_doc": "docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md",
      "summary": "Mandatory applicability is a predicate, not a relevance score: always, capability_default, mapped classes."
    },
    {
      "clause_id": "ADR-0020-S3",
      "source_doc": "docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md",
      "summary": "Hybrid retrieval baseline: exact identifiers + lexical search + explicit dependency graph."
    },
    {
      "clause_id": "ADR-0020-S4",
      "source_doc": "docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md",
      "summary": "Inline operative obligations, reference rationale, lease evidence."
    },
    {
      "clause_id": "ADR-0020-S5",
      "source_doc": "docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md",
      "summary": "Model-facing rendering is an adapter concern; delimiter safety prevents escape into instruction space."
    },
    {
      "clause_id": "ADR-0020-S6",
      "source_doc": "docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md",
      "summary": "Context profiles include effective cognitive envelopes and fail closed with CONTEXT_UNFIT."
    }
  ],
  "assumptions": [
    {
      "id": "asm-m3c-2b-split",
      "statement": "WP-M3C-2B delivers the Cognitive Invocation Compiler, deterministic admission, context strategy/profiling, prompt projection with delimiter safety, evidence leases, state capsule, and optional retrieval.",
      "status": "verified",
      "material": true
    },
    {
      "id": "asm-m3c-2b-protocol-types",
      "statement": "All compiler outputs strictly satisfy the protocol.ContextManifest, protocol.ContextPack, protocol.ContextProfile, and protocol.EvidenceLease types and validations defined in internal/protocol/context.go.",
      "status": "verified",
      "material": true
    }
  ],
  "expansion_triggers": [
    "Attempting to introduce dense vector embeddings or external neural rerankers into M3C triggers escalation (reserved for M4).",
    "Attempting to implement multi-reviewer consensus ledger or finding reclassification triggers escalation to WP-M3C-5 / M7."
  ]
}
```

---

## 2. Execution Contract (Authoritative & Bounded)

### 2.1 Objective

Build the deterministic **Cognitive Invocation Compiler** (`internal/cognition/compiler`) that compiles bounded, complete, and reproducible model invocations from task intent, role, execution contract, path/domain/risk mappings, project state, and endpoint ContextProfile without requiring agents to understand DevCadence's full rule/process corpus.

### 2.2 Verbatim MUST & MUST-NOT Constraints

- **MUST enforce deterministic rule admission classes (ADR-0020 §2, DCI-132):**
  Every mandatory clause must declare an admission class (`always`, `capability_default`, or `mapped`). The compiler must compute dependency closure over all admitted rules.
- **MUST validate reverse coverage (ADR-0020 §2, DCI-132):**
  A reverse-coverage validator must reject any mandatory clause with no deterministic admission path, detecting orphaned rules fail-closed.
- **MUST fail closed on missing or unknown mandatory rules or unmapped required domains (ADR-0020 §2, PROTOCOLS §10B):**
  Missing rule definitions, unknown dependencies, or unmapped required domains must return typed errors (`errs.CategoryInvalidArgument` or `errs.CategoryNotFound`).
- **MUST enforce ContextProfile bounds and return `CONTEXT_UNFIT` / `errs.CategoryContextUnfit` on overflow (DCI-019, ADR-0020 §2, PROTOCOLS §10B):**
  When token counts exceed the profile's hard resident ceiling, runtime window, or protected core reserves (with target resident tokens serving as soft packing and eviction guidance rather than a hard failure ceiling), the compiler must return `errs.CategoryContextUnfit` (`CONTEXT_UNFIT`) and MUST NOT silently truncate or drop mandatory obligations.
- **MUST NOT allow similarity, ranking, or optional retrieval to evict or override mandatory clauses (DCI-132, ADR-0020 §2, §3):**
  Optional retrieval (lexical, identifier, or graph) may suggest candidate evidence or supporting rationale, but mandatory clauses are inviolable.
- **MUST enforce delimiter safety in prompt renderers (DCI-133, ADR-0020 §5, PROTOCOLS §10B):**
  Renderers must ensure that evidence containing closing delimiters, code fences, XML tags, or command escape sequences cannot break out of evidence boundaries into model instruction space.
- **MUST maintain Cognitive State Capsule across turns (PROTOCOLS §10B, ADR-0020 §8):**
  The capsule stores non-authoritative derived hypotheses, active TODOs, intermediate decisions, open questions, and evidence dependencies across turns without certifying truth.
- **MUST manage Evidence Working Set leases with content-addressed provenance and authorization bounds (DCI-014, DCI-045, PROTOCOLS §10B):**
  Evidence leases must be content-addressed (SHA-256 matching content verbatim), bound to server-side read envelope authorization, and invalidated upon file mutation.
- **MUST support ContextControl strategy mapping (ADR-0019 §1, PROTOCOLS §10B):**
  Support `exact_stateless` (rebuild with exact leases and prefix), `append_only` (append tail with checkpoint/restart upon eviction), and `opaque_session` (scoped turns with checkpointing).

---

## 3. Package Architecture: `internal/cognition/compiler`

1. **`admission.go`**:
   - `AdmissionClass` (`always`, `capability_default`, `mapped`).
   - `Rule`: canonical representation with ID, class, doc, revision, content, digest, capabilities, domains, risk tags, roles, actions, path patterns, and dependencies.
   - `RuleRegistry`: thread-safe registry with `Register`, `Get`, `ValidateReverseCoverage`, and `ResolveAdmittedRules`.
   - `Compiler`: synthesizes `protocol.ContextManifest` and `protocol.ContextPack`.
2. **`profile.go`**:
   - `EnforceProfileBounds(pack *protocol.ContextPack, profile *protocol.ContextProfile) error`.
   - Token accounting estimators for BPE, provider API, and approximate estimation with uncertainty margins.
   - Default provisional profile builder (`DefaultProvisionalProfile`).
3. **`strategy.go`**:
   - `ContextStrategy` abstraction mapping `protocol.ContextControl`.
   - Implementations: `StatelessExactStrategy`, `AppendOnlyStrategy`, `OpaqueSessionStrategy`.
4. **`renderer.go`**:
   - `PromptRenderer` interface: `Render(pack *protocol.ContextPack) (PromptProjection, error)`.
   - `TaggedMarkdownRenderer` with delimiter escaping / nonce encapsulation preventing instruction breakout.
   - `StructuredJSONRenderer`.
5. **`capsule.go`**:
   - `CapsuleManager`: thread-safe maintenance of derived hypotheses, active TODOs, decisions, open questions, and dependencies across turns.
6. **`evidence.go`**:
   - `EvidenceLeaseManager`: content-addressed lease issuance, read envelope boundary verification, eviction, and mutation-based freshness invalidation.
7. **`retrieval.go`**:
   - `OptionalRetrievalEngine`: exact symbol/ID lookup, lexical search, dependency-graph traversal. Inviolable invariant: never evicts or overrides mandatory clauses.

---

## 4. Acceptance Criteria

1. **Deterministic Admission & Closure:** Compiles reproducible `ContextManifest` and `ContextPack` from identical inputs. Missing/unknown mandatory clauses fail closed.
2. **Reverse-Coverage Linter:** Detects orphaned rules that lack any deterministic admission path.
3. **Budget Bounds & CONTEXT_UNFIT:** When resident tokens exceed profile ceiling, compiler returns `errs.CategoryContextUnfit` (`ErrContextUnfit`). Mandatory obligations are never dropped or truncated.
4. **Delimiter Safety:** Adversarial evidence containing mock closing tags or markdown fences cannot break out of evidence containers.
5. **Evidence Leases:** Verifies verbatim SHA-256 digest, rejects out-of-scope paths outside `ReadEnvelope`, and invalidates on mutation.
6. **Cognitive State Capsule:** Correctly preserves and serializes non-authoritative hypotheses, TODOs, and decisions across turns.
7. **Optional Retrieval:** Traverses graph and lexical matches without evicting or overriding mandatory clauses.
8. **Verification:** All tests pass deterministically (`go test -count=1 ./...` and `go test -race ./...`), `go vet ./...` reports 0 issues, and `gofmt` is clean.

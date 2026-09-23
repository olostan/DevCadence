# ADR-0017: External research as a bounded evidence-acquisition service

- **Status:** Proposed
- **Date:** 2026-09-23
- **Decision owner:** Human (product owner)
- **Supersedes:** —
- **Superseded by:** —
- **Related invariants:** DCI-052, DCI-053 (reproducible/authoritative state), security invariants around worker network and untrusted content in docs/SECURITY.md
- **Related tasks:** M6 — Multi-review and consultant cognition

## Context

Execution-cognition roles (`Repository Scout`, `Implementer`) working with an
unfamiliar library or API currently have only two options: guess from
training-data recall, or have the Principal spend expensive frontier context
researching the answer. Neither is good: guessing produces plausible-looking
but wrong API usage; escalating a "how do I call X" question to the
Principal defeats the entire premise of routing narrow execution work to
cheap cognition (ADR-0011, ADR-0013).

A third option — let execution agents search public code and documentation
directly — is attractive but raises problems that must be solved
architecturally, not left to prompt instructions: context bloat (the same
failure mode ADR-0015/0016 solved for the local repository, now pointed at
the public internet), copyright/license exposure, and — as the 2026-09-23
review of this ADR's first draft established — two problems the first draft
did not address at all: **outbound network egress of potentially sensitive
query terms**, and **safe acquisition of untrusted remote content**.

### Revision history

This is the third substantive draft. The first draft (reviewed at commit
`4d899b6`) modeled external search as a **Consultant** and proposed
`external_search: enabled` as the default. An independent holistic review
against the full documentation corpus (`DISCOVERY_AND_SPECIFICATION.md`,
`CONSULTANTS.md`, ADR-0001, ADR-0013, ADR-0014, `CONTRIBUTING.md`, and the
merged `internal/tools/fetch.go`) found five blocking issues, all upheld on
verification against those documents (see Independent critique below). The
second draft (`a7f8c24`) restructured the design around that feedback and
was found to resolve the blocking issues, but a follow-up review identified
one further architectural gap — durable `SourceSnapshot` retention was
implied to happen unconditionally, before policy had decided retention was
even permitted — plus a credential-handling gap, a network-hardening gap,
and stale PR narrative. This draft incorporates that follow-up, plus one
further normalization from a second follow-up review: durable records
(`PolicyAssessment`, `DerivedDigest`) must carry durable source provenance
even on the derive-only path, where no `SourceSnapshot` exists to
reference. The funnel mechanics, license-branch logic, local-first cognition
routing, and per-project policy from the first draft are largely preserved
throughout; the abstraction boundaries, security posture, retention model,
and evidence/provenance model changed across these revisions.

## Verified facts

- `docs/DISCOVERY_AND_SPECIFICATION.md` §8's resolution-authority flowchart
  routes "current ecosystem/API/standard fact" questions to **`WEB /
  AUTHORITATIVE SOURCE`**, and routes only "is independent reasoning
  useful?" questions to **`CONSULTANT`** — these are distinct terminal nodes
  in the same decision tree, not the same thing under different names.
- `docs/CONSULTANTS.md` defines a Consultant's output contract
  (`ConsultationResult`) as reasoning-oriented: answer, alternatives,
  assumptions, concerns, uncertainty, recommended next investigation. A
  ranked list of search results with citations does not fit this shape.
- ADR-0001 (`SpecificationReviewResult` vs. `ReviewResult`) is a real,
  citable precedent for introducing a new protocol record rather than
  stretching an existing one when the semantics genuinely differ
  (`docs/adr/0001-discovery-specification-subsystem.md:208`).
- `config/project.example.yaml:146` sets `security.worker_network:
  deny_by_default` as the project's actual current default posture.
- ADR-0014 defines an **Intrinsic Policy** pattern
  (`docs/adr/0014-guided-bootstrap-and-remediation.md:32`): "the
  executor—not the recipe—defines the intrinsic minimum authority... Plan
  validation rejects any action whose declared authority or effects are
  weaker than the executor's intrinsic policy." This is a directly
  applicable precedent for provider-contract constraints that project
  configuration must not be able to weaken.
- `internal/tools.FetchContent` (merged via PR #7) takes a `content_ref` and
  resolves it through the artifact store's locator — it pages an
  **already-existing immutable artifact**. It has no code path that fetches
  a URL. Reusing it for web content therefore requires a remote-acquisition
  step that does not yet exist anywhere in the codebase.
- `CONTRIBUTING.md`'s "New providers or runtimes" section requires
  documenting authentication, capabilities, quota/rate-limit handling,
  privacy implications, test strategy and failure modes for any provider
  adapter — a general project requirement this ADR's provider adapters must
  satisfy, not something specific to search.

## Assumptions

- A project operator who enables this capability understands it grants a new
  category of outbound network access; this ADR does not assume operators
  read every word of the ADR before flipping the config flag, hence
  fail-closed defaults (§ Decision) rather than relying on informed consent
  alone.
- Search-provider terms of service are heterogeneous and change over time;
  this ADR assumes DevCadence cannot maintain a live legal registry of every
  provider's terms and instead pushes hard-constraint declaration to the
  provider adapter itself (verified/updated when the adapter is written or
  revised, not derived automatically).
- No general-purpose, free, ToS-friendly-for-automation search API exists
  today (verified informally in prior discussion, not re-verified for this
  revision) — this remains an assumption, not a verified fact, and the
  adapter-boundary design (§ Decision, provider adapters) is chosen partly
  because it does not require this assumption to hold permanently.

## Decision criteria

- **Security** — outbound egress and untrusted remote content are new attack
  surface; this must not weaken the project's existing fail-closed network
  posture or its instruction/data authority boundary.
- **Architectural consistency** — must not collapse distinct existing
  concepts (evidence, cognition, consultant, durable fact) into one.
- **Reversibility** — an optional capability with a project-scoped kill
  switch; disabling it must fully remove the new attack surface, not merely
  hide the feature.
- **Local implementability** — must compose from primitives DevCadence
  already has (artifact store, cognition routing, per-project config) plus
  the one genuinely new primitive this ADR identifies (safe remote
  acquisition), not a wholesale new subsystem.
- **Legal defensibility** — an engineering risk-reduction design, not a
  claim of copyright/ToS compliance; must not overstate what any mechanical
  check proves.

## Alternatives considered

### Option A — Principal-only web research

The Principal (which already has broader trust and, per M4A's design, will
eventually have `request_evidence`) is the only role allowed to trigger
external research; execution agents never get direct access.

**Benefits:** smallest new attack surface; reuses the Principal's existing
authority level; no new egress-policy surface needed for execution roles.

**Costs / risks:** defeats the actual motivating problem — the point is to
let cheap execution cognition answer "how do I call X" without escalating to
expensive Principal context. This alternative solves the security problem by
not solving the product problem.

**What would invalidate it:** if execution-role compromise/prompt-injection
risk turns out to be unacceptably high even with the mediation this ADR
proposes (§ Decision), this becomes the fallback.

### Option B — Consultants gain native web/search capability

Extend the existing Consultant abstraction so a consultant's adapter may
optionally perform web lookups as part of forming its `ConsultationResult`.

**Benefits:** no new top-level concept; consultants already have a
provenance/uncertainty-aware output contract.

**Costs / risks:** conflates evidence acquisition with independent
reasoning — exactly the semantic collapse the independent review flagged.
A consultant's job is judgment; grep.app returning five code matches is not
judgment. It also doesn't fit the access pattern: execution agents need
cheap, frequent, narrow lookups, not an expensive reasoning-diverse
consultation.

**What would invalidate it:** none identified; rejected on architectural
grounds (Independent critique, below), not cost or feasibility.

### Option C — Control-plane External Research service, accessible under policy (selected)

A first-class service, distinct from Consultants, that execution roles (and
optionally the Principal and Consultants) may call under explicit
per-project policy. It owns egress policy, safe remote acquisition,
license/provider-policy gating, and evidence packaging; it never performs
independent reasoning and never asserts authority over durable facts.

**Benefits:** matches the existing `WEB / AUTHORITATIVE SOURCE` vs.
`CONSULTANT` distinction already in `DISCOVERY_AND_SPECIFICATION.md`;
composes cleanly with existing artifact/cognition-routing primitives; keeps
the security boundary (egress, untrusted content) in one auditable place
instead of scattered across every role that might want to search.

**Costs / risks:** more implementation surface than Option B (a new service
plus new protocol records, per Independent critique §12); requires the
safe-acquisition primitive (§ Decision) to exist before anything else can be
built on top of it.

**What would invalidate it:** if implementation experience shows the
service boundary adds more friction than security value for the narrow
"code/API lookup" use case, Option A becomes the fallback, not Option B —
Option B's conflation problem doesn't go away with more implementation
effort.

### Option D — Direct network/search authority for Scout/Implementer

Execution roles get their own provider credentials and call search APIs
directly, no mediation layer.

**Benefits:** simplest to implement; no new service.

**Costs / risks:** violates `security.worker_network: deny_by_default`
outright; no place to enforce egress policy, license gating, or safe
acquisition; every execution-role prompt-injection concern becomes a direct
network/SSRF concern. Rejected outright, not just deprioritized.

## Independent critique / consultation

The first draft of this ADR (reviewed at commit `4d899b6`, full text in
[PR #8](https://github.com/olostan/DevCadence/pull/8#issuecomment-5804462513))
was reviewed against the full documentation corpus and returned **request
changes** with five blocking findings, all confirmed on verification (see
Verified facts):

1. External search modeled as a Consultant conflates evidence acquisition
   with independent reasoning, contradicting the `WEB / AUTHORITATIVE
   SOURCE` vs. `CONSULTANT` distinction in `DISCOVERY_AND_SPECIFICATION.md`
   and the `ConsultationResult` contract in `CONSULTANTS.md`.
2. `external_search: enabled by default` contradicts
   `security.worker_network: deny_by_default` — a "plain technical query"
   can still leak internal domain vocabulary (symbol names, error strings,
   internal type/host names) even without a narrative description of the
   task.
3. No safe remote-content-acquisition boundary was proposed; `fetch_content`
   only pages existing local artifacts, so reusing it for web content
   without a preceding safe-fetch step leaves an SSRF-shaped gap (redirect
   to loopback/private ranges, unbounded body size, etc.).
4. The copyright/license framing overstated what a license allowlist and an
   n-gram overlap check actually establish, and conflated three genuinely
   independent policy layers (provider ToS, source access authorization,
   content copyright/license).
5. A cleared digest was described as a "durable fact," contradicting the
   observed/derived-interpretation distinction ADR-0016's compaction design
   already established for trajectory digests.

Thirteen further, non-blocking findings (license provenance granularity,
cache-key insufficiency, deferring shared cache, explicit egress-quota
policy, and others) were incorporated into the second draft's Decision and
Implementation guidance sections.

A follow-up review of the second draft
([PR #8 comment](https://github.com/olostan/DevCadence/pull/8#issuecomment-5804654219))
confirmed all five blocking findings and all thirteen non-blocking findings
above were satisfactorily resolved, and found one further architectural gap
plus three smaller issues, incorporated into this draft:

1. Acquisition implied durable `SourceSnapshot` retention before
   provider/source policy had actually authorized retention — fixed by
   separating `TransientSource` (bounded, non-durable) from `SourceSnapshot`
   (durable, retention-authorized) in Decision §4.
2. Credential guidance ("simple project-config keys" as a starting point)
   risked a parallel path around the project's existing
   `CredentialRef`-must-be-opaque pattern in `internal/cognition/service.go`
   — fixed by Decision §9.
3. The Safe Source Fetcher's threat model didn't address ambient
   proxy inheritance or DNS-rebinding/connected-peer-address validation —
   fixed by the expanded threat model in Decision §4.
4. This PR's title/description still described the first (Consultant-based)
   design after the code/doc content had moved on — corrected in the same
   push as this revision.
5. Minor consistency issues (record-count mismatch, `external_search` vs.
   `external_research` naming residue, "mechanically enforced" overstating
   implementation status for a Proposed ADR) — fixed throughout this
   revision.

A third review of that revision
([PR #8 comment](https://github.com/olostan/DevCadence/pull/8#issuecomment-5804750137))
confirmed all five items above resolved and found one further
normalization: `PolicyAssessment` and `DerivedDigest` were written as if
they always reference a `SourceSnapshot`, but the derive-only path
intentionally creates none, leaving durable records with no durable
lineage on that path. Fixed by introducing the shared `SourceProvenance`
structure (§6) — captured from the `TransientSource` at assessment time,
before it is discarded, and carried by value in both records regardless of
whether a `SourceSnapshot` also exists.

The full text of all three reviews is preserved verbatim as the cited
PR #8 comments rather than reproduced here.

## Decision

Replace "external search as a Consultant" with a distinct **External
Research service** — an evidence-acquisition capability, architecturally
adjacent to but not a member of the Consultant abstraction:

```text
Scout / Implementer / Principal / Consultant
                 │
                 │ ExternalResearchRequest
                 ▼
        External Research Service
                 │
          provider / egress policy
                 │
        ┌────────┴─────────┐
        ▼                  ▼
 Code Search Adapter   Web Search Adapter
        │                  │
        └──── source refs ─┘
                 │
          Safe Source Fetcher   (new primitive — see below)
                 │
        bounded TransientSource   (non-durable — see below)
                 │
     metadata / license / provider-policy assessment
                 │
   provider ∩ source ∩ project policy (§5)
        ┌────────┼────────────┐
        ▼        ▼             ▼
    retain    derive-only    deny
      │           │
      ▼           ▼
SourceSnapshot  DerivedDigest      (raw bytes discarded)
        \          /
         \        /
     ExternalEvidencePacket   (new protocol record)
                 ▼
          requesting role
```

Each stage has exactly one job:

### 1. Not a Consultant — a new protocol boundary

New records, not reuse of `ConsultationRequest`/`ConsultationResult`:
`ExternalResearchRequest`, `ExternalSourceRef`, `TransientSource`,
`SourceSnapshot`, `PolicyAssessment`, `DerivedDigest`,
`ExternalEvidencePacket` — seven top-level records, not five or six as
earlier drafts of this ADR miscounted; `TransientSource` (§4) was added in
the prior revision. This revision adds one further piece, `SourceProvenance`
(§6) — a shared embedded structure, not an eighth top-level record —
carried by both `PolicyAssessment` and `DerivedDigest` so durable evidence
stays auditable even on the derive-only path where no `SourceSnapshot`
exists. A real M6
Consultant may *consume* an `ExternalEvidencePacket` as input evidence; it
does not *emit* one, and the External Research service never emits a
`ConsultationResult`. `docs/IMPLEMENTATION_PLAN.md`'s M6 section must not
attach this ADR to the "at least one external consultant adapter" bullet as
if it satisfied that deliverable — it is a sibling capability, tracked as
its own bullet (already corrected in this PR's `IMPLEMENTATION_PLAN.md`
edit).

### 2. Fail-closed egress, mediated by the control plane

`external_research.enabled` defaults to **disabled** (this ADR uses
`external_research` as the canonical config namespace throughout; an
earlier draft used `external_search` in one place, which was residue, not a
naming decision), consistent with `security.worker_network:
deny_by_default`. A project explicitly opts in. Execution roles never get
direct network authority for this; they send an `ExternalResearchRequest`
to the control-plane service, which decides whether the query is permitted
to leave the machine at all. The per-project policy surface (§ below)
covers this explicitly rather than folding it into cognition-routing's
existing `local_only`/privacy policy, because this governs **egress** (does
anything leave the machine), not **model selection** (which the existing
policy already governs for the rerank/digest cognition steps — that part of
the first draft's routing design is preserved unchanged).

### 3. External content is untrusted data, never instruction authority

Search results, fetched pages, and code snippets are data-only evidence,
exactly like repository content already is relative to instruction
authority. The research/rerank/digest worker gets: no repository mutation
authority, no shell/process authority, no credentials beyond the minimum
provider credential its own adapter needs, bounded output schema, and
mandatory source attribution on every claim. This must be exercised by
fixtures at implementation time (see Verification plan) — a page containing
"ignore prior instructions and..." must be treated as inert data.

### 4. Safe remote acquisition as its own primitive, and retention is a separate, later decision

This is the missing piece the first draft assumed away, and the second
draft only partially fixed: acquiring a byte stream and *durably storing*
it are two different decisions, and the second draft's pipeline let
acquisition imply durable storage before policy had actually authorized
retention. A provider or source may permit viewing/transient processing or
producing a digest while specifically forbidding caching, retention, or
reproduction — the three-layer policy model (§5) already recognizes this
distinction; the acquisition/storage lifecycle now reflects it too.

A new **Safe Source Fetcher** sits between "here is a candidate URL" and a
**bounded, non-durable `TransientSource`** — not directly an immutable
`SourceSnapshot`. Its threat model:

- scheme allowlist (`https` only, by default);
- DNS/IP validation rejecting loopback, private, link-local and multicast
  ranges — validated against the **actual connected peer address**, not
  only a preflight hostname lookup, since ambient proxy configuration
  (`HTTP_PROXY`/`HTTPS_PROXY`), DNS rebinding, or a resolver/connection race
  can make the real destination differ from what preflight validation saw;
- the fetcher MUST NOT silently inherit ambient proxy settings unless a
  project explicitly allows it;
- every redirect hop repeats the complete validation process against the
  new target, including the connected-peer-address check above — not just
  the initial request;
- connection reuse must not bypass the host/address policy check;
- bounded response body size and decompression ratio;
- timeout and cancellation, consistent with ADR-0008's controlled-process
  execution philosophy applied to network calls instead of subprocesses;
- TLS verification, no ambient cookies, no ambient authorization headers;
- content-type restriction and parser resource limits;
- HTML/document sanitization to plain text/structured data.

These security invariants are stated explicitly so an implementation cannot
satisfy the prose with a "resolve once, validate, then plain
`http.Client.Get`" fetcher that a proxy or redirect can silently route
around — the exact algorithm is implementation guidance (below), not this
ADR's job to specify.

The assessment step (§5) captures the durable `SourceProvenance` structure
(§6) from the `TransientSource` — canonical ref, provider, retrieval time,
revision, content hash, license evidence — regardless of what happens next,
since that capture must happen before the transient bytes are gone either
way. *After* that, if retention is authorized, a durable, content-addressed
`SourceSnapshot` is additionally written to the artifact store; ADR-0016's
`fetch_content` pagination then applies to it, unchanged, exactly as the
first draft proposed — `fetch_content` remains a pager over durable
artifacts only, never over transient bytes. If policy authorizes
extraction/digest but not raw retention, a `DerivedDigest` is produced
(carrying the already-captured `SourceProvenance`) and the raw bytes are
discarded without ever becoming a `SourceSnapshot`. If neither is
authorized, the `TransientSource` is discarded and the request is denied
— no durable record is created at all in that case. `SourceSnapshot`
therefore means "durable *raw-content* copy whose retention is
policy-authorized," distinct from `SourceProvenance`, which durable
records always carry regardless of whether raw retention was authorized.

### 5. Three independent policy layers, not one

```text
provider hard constraints  (Intrinsic Policy, ADR-0014 pattern — project cannot weaken)
        ∩
source/license policy      (can this content be incorporated, and how)
        ∩
project policy             (this project's own choices within the above)
        =
effective permissible use
```

A provider adapter declares hard constraints project configuration cannot
override (cache-forbidden, modification-forbidden, LLM-use-forbidden,
attribution-required, max-retention, `policy_revision` — versioned, and
attached to every evidence packet derived under it, so behavior stays
explainable after a provider changes terms). Source/license policy is
evaluated separately, and — per finding 8 of the independent review — at
file/source granularity with an SPDX expression, evidence for that
classification, a confidence/epistemic status, and the scope it applies to,
not inferred wholesale from a repository-level `LICENSE` file. Project
policy (§ below) operates only within what the first two layers allow.

Funnel ordering changes accordingly from the first draft (license filter
before rank) to: provider-policy authorization → candidate ref → safe
acquisition → source/license assessment → deterministic
extraction/optional digest → evidence packet. License often cannot be
established before inspecting source metadata, especially at file
granularity, so it moves later in the pipeline, after acquisition rather
than before it — access authorization (can we even fetch this) is a
different, earlier question than incorporation policy (what can we do with
it once fetched).

### 6. Digests are derived artifacts, not facts — and durable provenance survives even without a snapshot

A `DerivedDigest` is model-generated interpretation, not observed fact —
consistent with ADR-0016's existing observed/derived distinction for
trajectory digests, which this ADR must not contradict.

Raw-source retention (§4) and durable source *provenance* are separate
concerns: `TransientSource` is explicitly non-durable and is discarded once
its policy assessment and any derivation complete, so a durable
`DerivedDigest` cannot depend on referencing it afterward. Instead, both
`PolicyAssessment` and `DerivedDigest` carry a shared embedded
`SourceProvenance` structure — captured from the `TransientSource` at
assessment time, before it is discarded — containing: canonical source ref/
URL, provider, retrieval timestamp, revision/commit where available,
content hash, source metadata, license expression plus evidence for it,
and the provider-policy revision in effect. `SourceProvenance` is durable
and carried by value; it does not require a live reference to anything
that may have been discarded. `PolicyAssessment` and `DerivedDigest` each
additionally carry an *optional* reference to a `SourceSnapshot`, present
only on the retain path (§4/§8).

`DerivedDigest` additionally records: derivation method, digest schema
revision, the cognition endpoint/model used, and the overlap-check result.
Claims inside a digest retain `observed`/`derived`/`inferred` epistemic
tags; nothing gets promoted to fact merely by surviving the overlap check.

This means an `ExternalEvidencePacket` remains fully auditable — source
identity, retrieval time, license basis — whether its evidence came from
retained raw content or from a derive-only transient path where the raw
bytes were never durably stored.

Provenance is preserved **universally**, independent of whether the source
license legally requires attribution — the engineering reason (can the
Principal answer "where did this fact come from, and is it stale now?") is
separate from the legal reason, and the first draft only motivated
provenance from the legal angle.

### 7. Overlap check and copyright framing, stated precisely

The deterministic n-gram/longest-common-substring check is a **risk-
reduction control**, not a compliance proof: it mechanically bounds
verbatim/near-verbatim lexical overlap; it cannot detect structural
copying with renamed identifiers, and it can false-positive on necessary
API signatures. The ADR must say this explicitly rather than claim it
"enforces" non-reproduction.

Similarly, "facts/procedures are not copyrightable" is true of the
underlying idea but not automatically of a source's explanatory prose
around it (17 U.S.C. §102(b) protects against copyright on the *procedure
itself*; an original description of that procedure can still be protected
expression — see U.S. Copyright Office Circular 33). Language in
implementation-facing documentation should say "facts/procedures may be
extracted as facts where legally appropriate," never classify a whole
passage as uncopyrightable solely because it explains a procedure.

### 8. Cache and lineage records, not one blob

Replace the first draft's single `(content_hash, license)` cache key with
three independently content-addressed record types — `SourceSnapshot`
(content hash, source identity, retrieval time, revision, metadata;
created only on the retain path, §4), `PolicyAssessment` (is keyed to the
durable `SourceProvenance` structure described in §6, plus license
expression/evidence, provider constraints, policy revision, disposition,
and *may* reference a `SourceSnapshot` when raw retention was authorized —
a `SourceSnapshot` is not required for a derive-only disposition), and
`DerivedDigest` (records the same durable `SourceProvenance` plus
derivation metadata — schema revision, cognition endpoint, derivation
revision, overlap-check result — and likewise *may* reference a
`SourceSnapshot` without requiring one). This fits the existing
artifact/evidence architecture (content-addressed, immutable, independently
versionable) better than a single blob, and lets a license reassessment or
a digest schema change happen without invalidating an otherwise-valid
source snapshot — while keeping every durable record auditable back to its
source even when no snapshot exists at all.

`cache_scope` for v1 is **project-private only**, with a bounded default
TTL. `org_shared`/`public_shared` are removed from this decision — DevCadence
has no mature cross-project trust/authorization/erasure-propagation model
yet, and a shared cache is a real redistribution surface, not just a cost
optimization. Revisit behind a separate ADR if a concrete case emerges,
consistent with the project's "don't build abstractions before the
milestone that needs them" posture. A `TransientSource` (§4) is never
cached at all — by definition it exists only for the duration of a single
policy assessment, and it becomes a `SourceSnapshot` or a `DerivedDigest`
(each independently cacheable per the above) or is discarded.

### 9. Provider credentials are never raw secrets in durable state

`internal/cognition/service.go` already establishes this pattern for
cognition endpoints: `CredentialRef` is documented as "an opaque reference
— a raw secret here would be a durable credential leak," and the service
actively rejects any `CredentialRef`/`AccountRef` value that looks like a
secret. This ADR's provider adapters follow the identical rule: **raw
provider credentials MUST NOT be stored in `.devcadence/project.yaml`,
protocol records, event records, or artifact metadata.** Before M3B's full
credential-reference UX exists, provider configuration may reference a
credential only through a bounded, non-secret locator — an environment
variable *name* (`credential_ref: env:BRAVE_SEARCH_API_KEY`, never the key
value itself), an OS keychain/credential-manager reference, an
authenticated CLI/session handle, or another opaque reference. The exact
resolver mechanism is an implementation detail this ADR does not need to
settle; what it settles now is that a raw secret never becomes durable
project configuration.

### 10. Explicit research quota/egress policy, distinct from cognition routing

Reranking and digesting correctly reuse ADR-0013's existing capability
routing and ADR-0016 §5's summarizer privacy/locality/cost inheritance — no
new mechanism needed there, unchanged from the first draft. But the search
and fetch steps themselves need policy cognition routing doesn't model:
max queries per task/session, max results per query, max full-source
fetches, max aggregate bytes, timeout, allowed providers, cost class, and
fail-closed behavior when policy is ambiguous or unset. This is a distinct,
new per-project policy surface (§ below), not folded into the existing
cognition policy object.

### Per-project policy (`.devcadence/project.yaml`, extending ADR-0015's
authoritative-config pattern)

- `external_research.enabled`: **false by default**; explicit opt-in.
- `external_research.providers`: allowlisted provider adapters, each
  carrying its own hard-constraint declaration (§5) and referencing
  credentials only through a non-secret `credential_ref` (§9), never a raw
  key.
- `external_research.egress`: query-sensitivity handling, max
  queries/results/fetches/bytes per session, timeout.
- `license_policy`: per-project allowlist (SPDX-expression-aware, not a flat
  license-name list); permissive-only is the suggested default. Being an
  open-source project does not by itself widen this — the project's own
  license still determines compatibility.
- `cache_scope`: `private` only in v1, with bounded default `retention`,
  applying to `SourceSnapshot`/`DerivedDigest`/`PolicyAssessment` records —
  never to a `TransientSource` (§8), which is not cacheable by definition.

## Rationale

Option C is preferred under the stated criteria because it is the only
alternative that gets both the security posture right (egress mediated in
one place, safe acquisition as a real primitive, fail-closed defaults) and
the architectural boundary right (matching the resolution-authority
distinction the project's own discovery documentation already makes between
fact-lookup and independent reasoning), without abandoning the underlying
product goal the way Option A does. It costs more implementation surface
than Option B, but Option B's cost saving comes from a conflation the
independent review correctly identified as a real semantic error, not a
reasonable simplification.

## Consequences

### Positive
- Execution agents gain grounded, current knowledge of public APIs without
  spending Principal context or accepting unverified guesses.
- Security and provenance guarantees are **designed to be** mechanically
  enforced (egress gate, safe fetcher, transient-vs-durable retention gate,
  overlap check, lineage records) rather than left to model discretion or
  prompt instructions — this is a design property of the proposal; nothing
  in this ADR is implemented yet (§ Follow-up).
- The design reuses ADR-0011/0013/0014/0015/0016 machinery for everything
  except the genuinely new primitives (safe remote acquisition with
  transient/durable retention gating, research egress policy) it
  identifies.
- Matches, rather than strains, the project's existing fact-vs-reasoning
  and observed-vs-derived distinctions.

### Negative
- Real new implementation surface: a new service, seven new protocol record
  types, and a safe-fetch primitive with its own threat model — larger than
  the first draft's "mostly reuse existing machinery" framing suggested.
- Three-layer policy composition (provider/source/project) and file-level
  license provenance are more implementation and operator-facing complexity
  than a flat allowlist.

### New risks
- The Safe Source Fetcher is itself new, security-critical, network-facing
  code (SSRF surface) and needs its own focused security review before
  implementation, independent of this ADR's acceptance.
- A provider's terms can change after `policy_revision` is captured;
  revision drift detection is implementation guidance (below), not solved
  by this ADR.

## Implementation guidance

- Introduce `ExternalResearchRequest`, `ExternalSourceRef`,
  `TransientSource`, `SourceSnapshot`, `PolicyAssessment`, `DerivedDigest`,
  `ExternalEvidencePacket` in `internal/protocol`, distinct from
  `ConsultationRequest`/`Result`, plus a `SourceProvenance` embedded struct
  (§6) shared by `PolicyAssessment` and `DerivedDigest` — not a top-level
  record in its own right.
- Build the Safe Source Fetcher as its own package (e.g.
  `internal/research/fetch`), with the threat-model controls listed in
  Decision §4 as unit-testable behavior, not prose — including a test
  double/harness that can simulate a redirect-to-private-IP and a
  proxy-routed connection, since these are exactly the cases a naive
  `net/http` client would get wrong.
- `TransientSource` is an in-memory/bounded-temp-storage value with no
  artifact-store presence; only `SourceSnapshot` creation writes to the
  existing artifact store (`internal/artifacts`), reusing its
  content-addressing — no new storage layer for either.
- Provider adapters (code search, web search) live behind a common
  interface per ADR-0013's adapter-boundary pattern, each declaring its
  `IntrinsicPolicy`-style hard constraints per Decision §5 and referencing
  credentials only via `credential_ref` (Decision §9) — adapter
  construction should reject a value that looks like a raw secret, the
  same way `internal/cognition/service.go`'s `looksLikeSecret` check
  already does for cognition endpoints; reuse that helper rather than
  reimplementing it.
- Rerank/digest cognition calls route through existing
  `internal/cognition` capability routing, adding `SearchRelevance` and
  `SnippetDigest` roles — no new routing mechanism.

## Verification plan

Before this moves from Proposed toward implementation readiness:
provider/license-policy legal review, a focused SSRF/network threat model
for the Safe Source Fetcher, and a typed-protocol/evidence design review are
required gates, not advisory notes. At minimum, implementation must include
deterministic fixtures for:

- an internal symbol/identifier in a query is blocked from leaving the
  machine when policy requires it;
- a redirect to `127.0.0.1` or a private/link-local IP range is blocked,
  including when the redirect target only resolves to a private address
  through the connected-peer-address check (not just the preflight DNS
  lookup);
- a request routed through an ambient `HTTP_PROXY`/`HTTPS_PROXY` is
  rejected or requires explicit policy opt-in, not silently honored;
- an oversized or highly-compressed response body is bounded/refused;
- a provider whose hard constraints forbid caching cannot be overridden by
  project `cache_scope`/`retention` config, and content is still usable
  transiently (via `TransientSource`/`DerivedDigest`) without ever becoming
  a `SourceSnapshot`;
- a config value that looks like a raw API key/secret in
  `external_research.providers[].credential_ref` is rejected at load time,
  mirroring `internal/cognition/service.go`'s existing secret-shape check;
- content with unknown/unassessed license is never exposed as raw content;
- fetched content containing an embedded instruction ("ignore previous
  instructions and...") is treated as inert data by the digest/rerank
  worker;
- a digest that reproduces a long verbatim span of its source is rejected
  by the overlap gate;
- the same source under a changed `policy_revision` does not silently reuse
  the old `PolicyAssessment` disposition;
- a derive-only `DerivedDigest` (no `SourceSnapshot` created) still carries
  a complete, queryable `SourceProvenance` — source ref, provider, retrieval
  time, license evidence are all present even though the raw bytes were
  never durably stored;
- a project's cached `SourceSnapshot`/`PolicyAssessment`/`DerivedDigest` is
  not reachable from a different project's namespace;
- no provider configured results in an explicit reduced-capability signal,
  not a silent fallback.

## Rollback / supersession strategy

`external_research.enabled: false` fully disables the capability at the
policy gate before any network call is made — no partial-disable state
exists. Because the design isolates the new primitives (Safe Source
Fetcher, the new record types) from existing subsystems rather than
modifying them in place, removing this capability later does not require
unwinding changes to `internal/tools`, `internal/compaction`, or
`internal/validation`.

## Follow-up

- [ ] Work Package: `ExternalResearchRequest`/`ExternalSourceRef`/
      `TransientSource`/`SourceSnapshot`/`PolicyAssessment`/`DerivedDigest`/
      `ExternalEvidencePacket` protocol types + schemas.
- [ ] Work Package: Safe Source Fetcher, with independent security review
      covering connected-peer-address validation, proxy/DNS-rebinding
      resistance, and redirect-hop revalidation (Decision §4).
- [ ] Work Package: code-search and web-search provider adapters
      (`IntrinsicPolicy`-style hard constraints per adapter; credentials via
      `credential_ref` only, Decision §9).
- [ ] Work Package: `SearchRelevance`/`SnippetDigest` cognition-routing
      roles.
- [ ] Legal review of the license-policy/SPDX-provenance approach.
- [ ] `docs/IMPLEMENTATION_PLAN.md` M6 section: keep this as its own bullet,
      distinct from the "at least one external consultant adapter"
      deliverable (already corrected in this PR).
- [ ] `docs/SECURITY.md`: cross-reference the Safe Source Fetcher threat
      model once implemented.

# ADR-0017: External code and web search as bounded, license-aware consultants

- **Status:** Proposed
- **Date:** 2026-09-23
- **Decision owner:** Human (product owner)
- **Related:** ADR-0009 (artifact storage and validation execution), ADR-0011 (adaptive environment and host-independent cognition), ADR-0013 (environment intelligence and cognition contracts), ADR-0014 (guided bootstrap and remediation), ADR-0015 (declarative modules and scoped worktrees), ADR-0016 (validation services, bounded tools, and context compaction, including its 2026-09-23 evidence-tier amendment)
- **Documents:** docs/IMPLEMENTATION_PLAN.md (M6), docs/SECURITY.md, docs/VISION.md

This ADR is a forward-looking proposal, not yet implemented. It records a
design worked out in discussion so the decision and its rationale survive
past the conversation that produced it, per AGENTS.md §11 (learning is
proposal-based) and §17 (provenance and decision records available). It does
not authorize implementation on its own; a Work Package still needs the
usual base-commit/acceptance path once M6's consultant abstraction exists to
carry it.

## Context

Execution-cognition roles (`Repository Scout`, `Implementer`) working with an
unfamiliar library or API currently have only two options: guess from
training-data recall, or have the Principal spend expensive frontier context
researching the answer. Neither is good: guessing produces plausible-looking
but wrong API usage; escalating a "how do I call X" question to the
Principal defeats the entire premise of routing narrow execution work to
cheap cognition (ADR-0011, ADR-0013).

A third option — let execution agents search public code and documentation
directly — is attractive but raises two problems that must be solved
architecturally, not left to prompt instructions:

1. **Context bloat.** A single search can return dozens of results, each
   potentially a full file. Handing that to a small local model exhausts its
   context budget in one call — the same failure mode ADR-0015/0016 were
   written to prevent for the local repository, now pointed at the public
   internet instead.
2. **Copyright and license risk.** Code and prose found publicly are not
   automatically safe to reproduce into a project, closed-source or open.
   License compatibility, attribution obligations, and — separately —
   terms-of-service/authorized-access constraints on the search mechanism
   itself all apply, and none of them are things a model should be trusted
   to reason about unsupervised, any more than it is trusted to self-certify
   `Verified` on an unauthenticated decision claim (ADR-0016's evidence-tier
   amendment).

This ADR treats external search as a **consultant** (M6's existing
abstraction: "optional independent reasoning sources used deliberately,
never automatically trusted and never tied to one mandatory provider,"
AGENTS.md §1), not as a fifth bounded tool alongside `read_file`/
`grep_search`. It is the "at least one external consultant adapter" M6
already commits to delivering — this ADR proposes that adapter's design, not
a new milestone.

## Decision

### 1. Two consultant kinds, one shared pipeline

- **Code-sample search** — public code search (e.g. a `grep.app`-style API)
  answering "how is this API used."
- **General web search** — an authorized web-search API (e.g. Brave Search,
  Bing Web Search, SerpAPI) answering "how do I configure/troubleshoot X,"
  largely procedural/factual content.

Both share the same funnel; they differ only in which guardrail applies to
what comes back.

### 2. The funnel: cheapest/most-deterministic filter first

Each stage only runs on what survived the previous, cheaper stage:

1. **Search.** A plain technical query (API/symbol/error names — never a
   narrative description of the task or proprietary terms) against the
   configured provider. This is metadata/keyword traffic, not sensitive.
2. **License/authorization filter — deterministic, no model.** Drop results
   outside the project's license allowlist before anything is ranked or
   read. For web search, only authorized search APIs are used, never direct
   scraping of result pages — this is a distinct, contractual
   (terms-of-service/authorized-access) constraint, independent of
   copyright, and it has no code-search analog.
3. **Rerank on metadata/snippets, cheap local model or embeddings.** Ranks
   for relevance *and* diversity (five near-duplicate results teach an
   Implementer nothing extra). No full-page/full-file fetch yet.
4. **Bounded read of the surviving few**, through the same pagination
   primitives ADR-0016 already delivered (`fetch_content`'s byte/line caps,
   contiguous cursors) — a web page is just another content stream once
   mechanically stripped of boilerplate; no new pagination mechanism is
   needed.
5. **License-gated delivery to the requesting agent:**
   - **License compatible with the project's own license** (or the content
     is factual/procedural, which is not copyrightable subject matter in
     the first place): deliver the excerpt directly. No paraphrase step.
     Record attribution metadata if a substantial/recognizable chunk is
     used, per the source license's terms.
   - **License incompatible, unknown, or the project's policy declines the
     obligation** (e.g. GPL into a closed project, no `LICENSE` file found):
     do not deliver raw content. Route through a **digest step**: a cheap
     model produces a schema-shaped functional summary (purpose, signature/
     call shape, ≤2 gotchas, no free prose beyond that) — deterministic
     facts (signature, call graph) come from AST tooling where possible
     (`internal/tools` symbol inspection, ADR-0016 §6) rather than a model
     at all, minimizing both cost and the surface for verbatim leakage.
   - Content bound for a **durable artifact the project keeps** (generated
     docs, a code comment, a commit message) that would reproduce prose
     verbatim goes through the digest step regardless of license, since the
     risk there is prose-into-prose reproduction, not code-into-behavior
     transformation.
6. **Deterministic overlap check as a backstop.** An n-gram/longest-common-
   substring check between digest output and source, and — for the
   digest path — a similarity check on what the requesting agent ultimately
   writes, against everything it was shown in that session. Reject/retry the
   digest (bounded attempts) on failure; this is the mechanism that makes
   "don't reproduce the source" an enforced property rather than a prompt
   instruction the model might not honor.
7. **Cache**, keyed by `(content_hash, license)`, scoped per project by
   default (§4). A cleared digest or a license-compatible excerpt is a
   durable fact once produced; most of the ongoing cost in this design is
   avoided by not re-deriving the same answer repeatedly, not by making any
   single lookup cheaper.

### 3. Cognition routing: local-first, low-cost-cloud fallback — no new policy

The rerank and digest steps are role-routed through the existing
capability-based routing (ADR-0011/0013), the same way ADR-0016 §5 already
specifies for compaction summarizers: "Summarizers inherit the parent
session's exact privacy, locality, disclosure, and cost policies." This ADR
adds one more role to that same table (nominally `SearchRelevance` for
reranking and `SnippetDigest` for the paraphrase step) rather than inventing
a separate routing mechanism. Local is preferred; low-cost cloud is the
fallback when local capability is insufficient or unavailable, governed by
the project's existing `local_only`/privacy/cost policy — never a new,
parallel policy surface.

The search API call itself is not a cognition-routing decision — it is
always a remote network call to the configured provider, since there is no
way to query the public internet "locally." Only the reasoning *around* the
search results is subject to local/cloud routing.

### 4. Provider adapters are pluggable — no vendor lock, no built-in default

Consistent with ADR-0013's remote-API adapter-boundary pattern ("ships as
the adapter boundary plus a deterministic client rather than a provider
implementation"): code-search and web-search providers are each a thin
adapter behind a common interface, project-configured with the operator's
own API key (BYO-key). This is a practical necessity as much as an
architectural preference — there is no single free, general-purpose search
API DevCadence could hardcode a default to, so the plugin boundary is doing
real work here, not just abstraction for its own sake. Guided key
acquisition belongs to M3B's already-planned credential-reference
abstraction and setup flow (`devcadence setup`), as one more provider
alongside cognition endpoint configuration — not a new onboarding mechanism.

### 5. Per-project policy, not one global decision

A developer's projects differ — some open-source, some closed/client work
under confidentiality obligations — so this is project-scoped configuration
in `.devcadence/project.yaml` (the same authoritative-config file ADR-0015
introduced for module catalogs), not a single global setting:

- `external_search`: enabled by default (search queries are plain technical
  terms, not sensitive; there is no strong reason to default this off).
- `license_policy`: a per-project allowlist. Being an open-source project
  does **not** by itself make any license acceptable — the project's own
  license still determines compatibility (an MIT project ingesting GPL code
  creates a downstream obligation problem regardless of the project's own
  source being public). Permissive-only is a reasonable default; a project
  may explicitly widen it.
- `cache_scope`: `private` (default) | `org_shared` | `public_shared`. A
  closed/client project's cache entries never leave its own namespace by
  default; `public_shared` requires the project to be genuinely open/
  non-confidential and an explicit opt-in — this is the tier that actually
  gets the cross-project cost amortization, so it is worth having, just not
  as the default anyone falls into unintentionally.
- `retention`: session/TTL/indefinite, for projects with contractual
  data-retention or erasure obligations.

## Consequences

- **Positive:** execution agents gain grounded, current knowledge of public
  APIs without spending Principal context or accepting unverified
  guesses; the license/authorization guardrails are enforced mechanically
  (allowlist, overlap check) rather than left to model discretion; the
  design reuses ADR-0011/0013/0015/0016 machinery almost entirely rather
  than introducing new architecture.
- **Negative:** two-tier delivery (direct vs. digest) and per-project policy
  add real implementation surface to M6's consultant adapter; digest-path
  latency (search → rerank → digest → overlap check) is higher than a raw
  lookup, though bounded and cacheable.
- **Explicitly not addressed here:** this ADR is an engineering risk-
  mitigation design, not a legal opinion. License-allowlist boundaries and
  the digest/overlap approach should get real legal review before this
  ships as a shipped, relied-upon feature — see docs/SECURITY.md for how
  external-facing capabilities are otherwise gated in this project.

## Placement

This belongs to **M6** (multi-review and consultant cognition) as (one
possible realization of) "at least one external consultant adapter," not a
new top-level milestone and not something deferred until M7-M9. It depends
on M6's consultant abstraction existing; it does not depend on M7 (health/
refactoring), M8 (learning), or M9 (autonomous campaigns). It may use M3B's
credential-reference abstraction for guided key setup once that exists, but
a simpler configured-key path is sufficient to start.

## Open questions for implementation

- Exact digest schema and overlap-check thresholds (tunable per project, per
  §5, but need calibrated defaults).
- Whether `public_shared` cache should exist at all in a first cut, or be
  deferred until there's a concrete case for it.
- Which license identifiers populate the default permissive allowlist (this
  ADR assumes MIT/BSD-2/BSD-3/Apache-2.0/ISC/0BSD as a starting point,
  subject to the legal review noted above).

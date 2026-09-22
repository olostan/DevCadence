# ADR-0013: Environment intelligence and cognition capability contracts

- **Status:** Accepted
- **Date:** 2026-09-21
- **Related:** ADR-0011 (adaptive environment and host-independent cognition), ADR-0003 (durable record compatibility), ADR-0009 (artifact storage and validation execution), DCI-104, DCI-106, DCI-108, DCI-012, DCI-055
- **Documents:** docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md, docs/MODEL_RUNTIME.md, docs/PROJECT_STATE.md, docs/SECURITY.md

## Context

ADR-0011 decided the *direction*: local-first describes authority rather than
inference location, cognition is capability-routed, environment intelligence is a
product capability, acceleration must be empirically verified, and principal
hosts are replaceable. It deliberately left the durable contracts open.

Implementing M3A forced five of those contracts to be settled, because each one
has a wrong answer that is locally convenient and architecturally corrosive:

1. **What a cognition endpoint *is*, durably.** M1 reserved
   `ProjectState.capabilities` as `{local_models, consultants}`. That shape
   encodes the superseded architecture — a local model plus a set of consultant
   subscriptions — and cannot express an authenticated CLI, a remote API, health,
   authentication state, verified acceleration, cost or privacy.
2. **What "accelerated" means as a stored claim.** A boolean would be written
   from whatever the code happened to observe, and a later reader could not tell a
   measured claim from an optimistic one.
3. **Where environment and capability results live.** The obvious options — the
   event journal, a new state file, or nothing — are each wrong for different
   reasons, and choosing by accident would be worse than choosing badly.
4. **What may satisfy a capability requirement.** Without a rule, model names and
   hardware sizes become capability evidence, which is how routing acquires
   folklore.
5. **How routing explains itself.** An unexplainable router that spends money and
   sends source to a third party is not auditable.

## Decision

### 1. Facts, assessment and evidence are separate types

Three durable shapes, with a one-directional dependency:

```text
EnvironmentFacts        observed; no judgement
        │
        ├─ assessment ─► AcceleratorCandidate   plausible backends + reasons
        │
        └─ adapters ───► CognitionEndpoint      usability, capability, evidence
                                │
                                └─► AccelerationEvidence
```

`EnvironmentFacts` contains only observations and explicit statements that an
observation could not be made. Assessment is a **pure function** of facts:
`environment.AssessBackends` takes no clock, no probe and no host state, so the
same facts always yield the same candidates and a fixture machine is assessed
identically on any test runner.

Assessment may not exceed `runtime_available`. Nothing in the assessment layer
can reach `verified`.

Every unobservable fact is modelled explicitly — a nil pointer or an `unknown`
enum value, never a zero that reads like a measurement. A blank machine, a
machine whose vendor tooling is absent and a machine with unreadable device nodes
all produce valid facts (DCI-104).

### 2. Acceleration is an eight-state claim with evidence, not a boolean

`AccelerationState` is `not_detected | candidate | runtime_available |
unverified | verified | failed | unsupported | unknown`.

`verified` requires all of:

- an **authoritative** offload signal for that backend — reported by the runtime
  that performed the inference, not by a driver being loaded or a package being
  installed;
- a verification instant;
- no recorded evidence conflicts.

Record validation **refuses** a `verified` claim missing any of them, and refuses
one at a probe depth that never ran inference. DCI-106 is therefore enforced by
the contract, not only by the code that writes it.

Three consequences are deliberate:

- An authoritative runtime signal is **sufficient**. Requiring corroboration from
  weaker observers would make the answer worse, not safer, and vendor telemetry
  read after a probe completes cannot distinguish "never offloaded" from
  "already unloaded".
- A runtime reporting CPU execution produces `failed`, not silence. Silent CPU
  fallback is the misconfiguration ADR-0011 §4 exists to surface.
- Disagreeing observers produce `unverified` **plus the contradiction**. Picking
  whom to believe would manufacture the false positive the invariant forbids.

### 3. Probe depth is part of the contract

`ProbeDepth` is `inventory | health | inference`. At `inventory` no external
command runs at all; at `health` an already-running service may be queried and
bounded version commands may run; only at `inference` may a model execute — and
only a model that already exists locally.

Depth is recorded on the profile, so a shallow result cannot be read as a deep
one, and `environment inspect` refuses `inference` outright.

Depth authorises *how deep* a probe may go, and never *how many* endpoints it may
reach. `inference` depth additionally requires the caller to name the endpoints it
may invoke, one at a time; a request for `inference` with no endpoint named is
refused rather than interpreted as "all of them". Inventory surfaces — `cognition
list`, `cognition route` — therefore cannot reach `inference` at all, and
`cognition probe <endpoint-id>` discovers the machine at `health` and invokes only
the endpoint it was given. Fan-out would be the expensive failure this contract
exists to prevent: several of the endpoints on a normal machine are authenticated
coding CLIs, and probing them all would bill three providers to answer a question
about what is installed.

### 4. ProjectState carries a compact cognition projection; the M1 shape is deprecated, not deleted

`ProjectState.capabilities` gains `cognition`:

```text
assessment            ready | ready_with_reduced_capability |
                      model_cognition_unavailable | partially_ready | unknown
observed_at           when the underlying profile was observed
machine_fingerprint   which machine it describes
profile_ref           where the full profile is retrievable
endpoints[]           id, kind, locality, health, auth_status, cost_class,
                      required_source_exposure, acceleration_verified, backend
limitations[]         what this configuration cannot do
```

Capability grades, probe signals, measurements, CPU features and device nodes are
**not** in the projection. They live in `MachineCapabilityProfile`, reachable
through `profile_ref` (DCI-010, DCI-011). A grade without its provenance would
read as evidence while resting on nothing, so the projection carries neither.

`local_models` and `consultants` remain in the Go type and the JSON Schema,
marked deprecated and never written by current builds. Deleting them would make
every historical ProjectState document unreadable under strict decoding, which
ADR-0003 forbids (DCI-092, DCI-093). Keeping them costs two optional fields and
preserves the ability to inspect old trajectories. Consultant selection becomes
an M6 policy over discovered endpoints rather than a separate capability list.

Because the change is additive-optional within `schema_version` 1.0, no version
bump is required and old fixtures still validate.

**Machine capability is global; policy is per-project.** The hardware and the
installed runtimes are identical for every project on a host. What differs per
project is which endpoints its privacy and cost rules permit, and that is routing
policy — an input to a decision, not durable state.

### 5. Persistence boundaries are explicit

| Data | Lifetime | Rationale |
| --- | --- | --- |
| `EnvironmentFacts`, `MachineCapabilityProfile` | computed on demand; not stored by M3A | Endpoint health and available memory change by the minute. They are not project history, and appending that churn to an append-only journal would bury the record of what the project did. |
| Raw probe output | M2 content-addressed artifact store, referenced by digest | Large, rarely needed, and must stay retrievable when something disagrees (ADR-0009, DCI-011). |
| Compact projection | durable ProjectState field | The one project-scoped form, and the only one a principal sees. |
| Cached machine profile | **M3B** | Nothing in M3A reads a profile back, and a cache nobody consumes is a staleness bug waiting to happen. `MachineFingerprint` exists so M3B can build caching and invalidation correctly. |

`MachineFingerprint` digests only facts that change when the machine changes: OS,
architecture, CPU model, total memory, accelerator identity and driver, software
versions. Available memory, free disk space, loaded models and observation
timestamps are excluded, because a fingerprint that moved every minute could not
answer the one question it exists for — "is this the same machine, or did
something change that invalidates prior verification?"

### 6. A capability grade requires provenance

`CapabilityProvenance` is `unknown | configured | measured | evaluated`, and
record validation refuses any graded capability whose provenance is `unknown`.

- `configured` — a human or operator policy asserted it. This is legitimate
  authority: the operator knows things DevCadience cannot measure. It is recorded
  as configuration so a surprising routing choice traces back to the person who
  declared it.
- `measured` — an M3A probe observed it. M3A probes establish *operational*
  properties: does it answer, does it emit valid JSON, how long did it take.
- `evaluated` — an evaluation suite with recorded outcomes established it. This is
  what an implementation-quality claim actually requires. Nothing in M3A produces
  it.

A measured or evaluated grade is never overwritten by a declaration.

`unknown` is the normal answer for almost every discovered endpoint, and the
system must be comfortable saying so. Nothing derives a grade from a model name,
a parameter count, a hardware memory size or a single structured-output probe. A
passed structured-output probe sets `structured_output: probe_passed` — one tiny
schema, once — and asserts nothing about reliability under load.

### 7. Routing is a filter, an ordering, and an explanation

```text
hard constraints  →  ordering  →  first survivor
```

Hard constraints, each recording a reason for every rejection: probed-ready
health; authentication not in a failed state; required source exposure within the
project's maximum; cost class within the project's maximum; capability grade at or
above the role's floor; any required feature or verified acceleration.

Ordering among survivors: operator preference, then cheapest, then most private,
then best-graded, then identifier. Cost precedes capability grade because
everything still in the running already satisfies the requirement, so the
principle is "the least expensive, most private endpoint that *satisfies* the
need" (docs/MODEL_RUNTIME.md §5). The identifier tiebreak makes ties deterministic
rather than dependent on discovery order.

A decision distinguishes **ineligible** from **eligible but not chosen**, because
"could not" and "was not selected" call for different operator action. There is no
score, no weight and no confidence value.

Two properties are load-bearing:

- **No eligible endpoint is a correct, typed outcome.** A privacy-restricted
  project whose local capability is insufficient gets `no_eligible_endpoint` with
  an explanation, carried as `errs.CategoryNoEligibleEndpoint` so a caller can
  route on it. It is not an internal failure (DCI-104).
- **An unset policy fails closed** to `local_only` / `local_compute`. A zero
  `Policy` is most plausibly a caller who forgot to configure one, and defaulting
  it to permissive would let remote cognition be selected by omission — the silent
  fallback docs/MODEL_RUNTIME.md §18 forbids.

Operator preference orders eligible endpoints and never makes an ineligible one
eligible, so preference cannot be used to route around a privacy constraint.

### 8. Adapters are the only place a provider exists

`cognition.Adapter` is `ID`, `Discover`, `Probe`. Adapters translate one
runtime's or CLI's reality into protocol types and hold no policy, make no
routing decision and grade no capability beyond what they measured.

The core domain, the environment layer and the cognition core are buildable
without any adapter; an adapter is selected at the edge, in
`cmd/devcadience`. `tests/boundaries_test.go` enforces both directions: no
third-party model SDK enters the module, and no core package imports an adapter.

Compatibility knowledge — which GPU architectures ROCm supports, which CLIs exist
and how to invoke them non-interactively — is **versioned typed tables**
(`environment.KnowledgeRevision`, `environment.InventoryRevision`,
`codingcli.InvocationRevision`) recorded on the results they produce, so a later
revision reaching a different conclusion about the same machine is explainable
(ADR-0011 §5). It is typed Go, never configuration: an entry names an executable
that will be run, and a configuration-driven command table would be an arbitrary
shell engine by another name.

## Consequences

### Positive

- Acceleration cannot be claimed without evidence, at the type level.
- A blank machine, a container, an unsupported OS and a machine with no AI
  software all produce valid, useful output.
- Routing decisions are reproducible, diffable and explainable to a user whose
  money and source are at stake.
- Old ProjectState documents stay readable while the capability model moves on.
- M3B inherits a fingerprint, a depth model and a declaration mechanism it can
  build caching, remediation and setup on without redesigning contracts.
- Adapters stay replaceable in a way a test can prove.

### Costs

- `internal/protocol` grows two substantial files. Accepted: the alternative was
  a second capability universe alongside the ProjectState twin, which would drift.
- Almost every discovered endpoint starts with ungraded capability, so
  implementation routing needs either an operator declaration or evaluation
  history. This is the honest position, and it makes the missing evaluation
  subsystem visible rather than papering over it with inferred grades.
- Compatibility knowledge is real product surface that will need maintenance. A
  stale entry costs a capability (the probe fails and the endpoint stays
  `installed`) rather than producing a false claim, which is the correct failure
  direction.
- The coding-CLI health probe consumes a small amount of the user's quota, so it
  runs only at `inference` depth, only for an endpoint the caller named, and only
  when explicitly requested.
- Cost class is not inferred from software presence or authentication. An
  installed coding CLI may be billed by subscription, by a metered API key, by an
  enterprise agreement or from a credit balance, and discovery cannot distinguish
  those without reading credentials it must not touch. A discovered endpoint is
  `cost_class: unknown`, which the router already orders above every known class,
  and an operator declaration is the only path to a known one. The cost is a lost
  routing preference for a genuinely subscription-backed CLI; the alternative is
  spending a user's money on an assumption.

### Scope

This ADR settles the contracts M3A needed. It does not decide setup action
authority levels, remediation recipes, credential creation, deployment-profile
recommendation or terminal UX — those are M3B. It does not decide consultant
selection or reviewer independence policy, which are M6, though it preserves the
provider, model-family and opaque account metadata M6 will need to avoid treating
two frontends over one model as independent.

It adds no new endpoint kind for LAN or remote inference workers:
docs/MODEL_RUNTIME.md §21 requires a separate threat model first, and publishing
the enum value early would invite routing code to handle a case nothing can
produce.

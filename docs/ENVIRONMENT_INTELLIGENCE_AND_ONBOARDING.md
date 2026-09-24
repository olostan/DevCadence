# Environment Intelligence and Guided Onboarding

## Scope

DevCadence must be useful on a blank developer machine, not only on a preconfigured workstation.

The supported starting state includes:

- no local LLM runtime;
- no GPU/accelerator software stack;
- no principal host;
- no consultant CLI;
- no provider credentials;
- weak or unsuitable local inference hardware;
- only basic operating-system tooling.

The onboarding system discovers what exists, explains what is missing, proposes the best feasible configuration, obtains explicit approval for changes, verifies the result empirically, and records a compact capability profile.

> **Local-first means local authority, state, repository execution and evidence. It does not require all inference to run locally.**

## 1. Bootstrap lifecycle

The setup engine follows a fixed conceptual lifecycle:

```text
DISCOVER
  -> ASSESS
  -> PLAN
  -> APPROVE
  -> INSTALL / CONFIGURE
  -> VERIFY
  -> BENCHMARK
  -> RECOMMEND
```

Discovery and assessment are non-destructive.

Installation/configuration actions are explicit structured actions and must respect their authority level. A setup recommendation is not permission to execute it.

## 2. Environment facts

Discovery SHOULD collect machine facts without requiring vendor tooling to already be installed.

Examples:

- operating system, distribution/version and architecture;
- CPU model, cores/threads and useful instruction sets;
- physical and available memory;
- swap;
- storage availability;
- virtualization/container/WSL context where applicable;
- Apple Silicon / NVIDIA / AMD / Intel accelerators;
- relevant device nodes and permissions;
- installed Git/build/container tooling;
- installed local-inference runtimes;
- installed supported principal hosts;
- installed cognition/consultant CLIs;
- authentication readiness where it can be checked without exposing secrets.

Facts are not recommendations.

## 3. Acceleration is a verified capability

The following are distinct states:

```text
accelerator hardware exists
        !=
driver/runtime stack is available
        !=
inference runtime can use it
        !=
actual inference is accelerated
```

DevCadence MUST NOT report GPU acceleration as ready merely because a GPU or inference runtime is installed.

A supported acceleration path becomes `verified` only after a real probe demonstrates that the selected runtime actually executes through the intended backend.

Useful evidence may include:

- runtime-reported processor/backend;
- vendor/OS telemetry;
- a small inference workload;
- plausible observed throughput/memory behavior.

One signal alone should not be treated as infallible when better evidence is available.

### 3A. Implemented evidence model (M3A)

Signals carry an explicit trust level, and only the strongest verifies:

| Trust | Meaning | Can verify alone? |
| --- | --- | --- |
| `authoritative` | the runtime that performed the inference reported the backend it used | **yes** |
| `corroborating` | an independent observer (OS or vendor telemetry) agrees | no |
| `indicative` | consistent with acceleration but does not establish it — a loaded driver, an installed package | **never** |

An authoritative signal is sufficient on purpose. Requiring corroboration from
weaker observers would make the answer worse rather than safer: vendor telemetry
read after a probe completes cannot distinguish "never offloaded" from "already
unloaded", so demanding it would fail correct verifications and invite a
tie-breaking heuristic.

Three outcomes matter:

- **verified** — authoritative offload, a verification instant, no conflicts.
  Record validation refuses a `verified` claim missing any of these, and refuses
  one recorded at a probe depth that never ran inference.
- **failed** — the runtime reported that the work ran on the CPU. This is the
  silent-fallback case, surfaced rather than hidden.
- **unverified** — anything else, including signals that disagree. A contradiction
  is recorded alongside it; choosing whom to believe would manufacture exactly the
  false positive DCI-106 forbids.

For Ollama the authoritative signal is `/api/ps`, which reports a resident
model's `size` and `size_vram`. For MLX-LM it is the generating process's own
device, read in the same process immediately after generation.

## 4. Backend candidates

Backend selection is compatibility/policy data, not a hard-coded product assumption.

Typical candidates include:

### Apple Silicon

- MLX / Metal;
- Ollama / Metal;
- CPU fallback.

The setup engine should verify native Apple Silicon execution and avoid accidentally treating a translated or CPU-only path as equivalent.

### NVIDIA on Linux

- supported NVIDIA driver + CUDA inference path;
- CPU fallback.

Do not install a full CUDA development toolkit when the selected runtime only requires a compatible driver/runtime stack.

### AMD on Linux

- ROCm where the device/runtime combination is supported;
- Vulkan where supported and appropriate;
- CPU fallback.

An AMD device that is poorly supported by ROCm may still be a useful Vulkan inference device. The compatibility engine should not blindly install ROCm based only on vendor identity.

### Intel / other GPUs

- supported Vulkan or runtime-specific path where available;
- CPU fallback.

Exact compatibility data changes over time and SHOULD live in versioned setup recipes/capability knowledge, not scattered conditional statements.

## 5. Verification and microbenchmarking

Setup SHOULD use a deliberately small diagnostic model/workload when local inference is being configured.

The purpose is not to benchmark intelligence. It is to establish:

- model load succeeds;
- selected accelerator path is real;
- prompt processing and generation work;
- approximate throughput;
- memory pressure;
- structured-output reliability;
- a safe operational context estimate.

Large multi-gigabyte model downloads require explicit user approval.

A compact `MachineCapabilityProfile` may retain measured results and the software versions they depend on. Major driver/runtime/OS changes should invalidate or age the assessment.

## 6. Cognition endpoints

A **CognitionEndpoint** is a replaceable source of model cognition. It is broader than an API provider.

Candidate kinds include:

- local runtime;
- authenticated CLI;
- remote API;
- future LAN/remote inference worker.

Conceptual shape:

```yaml
id: codex-cli
kind: authenticated_cli
provider: openai

capabilities:
  repository_reasoning: strong
  implementation: strong
  review: strong
  structured_output: supported

execution:
  locality: remote_inference_local_tools
  health: ready

policy:
  cost_class: unknown        # until an operator declares it; see below
  source_exposure: focused_or_tool_mediated
```

Cost class is never inferred from a CLI being installed or authenticated. The same
executable may be billed by subscription, by a metered API key, by an enterprise
agreement or from a credit balance, and distinguishing those would require reading
credentials that discovery must not touch. A discovered endpoint is therefore
`cost_class: unknown`, which routing treats as more expensive than every known
class; an operator declaration is the only path to a known class, and it is
recorded with `configured` provenance.

Roles bind to capabilities, not permanent model names.

## 7. AI software discovery

Discovery SHOULD recognize supported tools even when DevCadence did not install them.

Examples include:

- Ollama;
- MLX-LM;
- supported provider/coding CLIs;
- supported principal hosts;
- credential stores/authenticated sessions.

For every discovered integration, distinguish:

```text
installed
  -> compatible version
  -> authenticated/configured
  -> health/smoke test
  -> usable capability
```

A binary existing on PATH is not enough.

## 8. Capability-based degradation

Missing optional capability is a normal state.

Examples:

- no strong local model -> route implementation to an allowed economical remote endpoint;
- no local model at all -> deterministic local execution + remote cognition remains valid;
- no consultants -> continue with reduced cognitive diversity;
- no principal host -> setup remains partially ready and can configure one later;
- no cloud credentials -> local/offline capabilities remain valid.

The system should report **READY**, **READY WITH REDUCED CAPABILITY**, **PARTIALLY READY**, or a similarly explicit assessment instead of collapsing every missing optional integration into setup failure.

## 9. Portfolio descriptors, not deployment modes

Labels such as `local-heavy`, `hybrid-thin`, `cloud-cognition` and `offline` remain useful human-readable summaries and fixture scenarios. They are **not the configuration object and not a closed set of architectural modes**.

The canonical result is a CognitionPortfolio over discovered resources, not a selected deployment label.

## 10. Deterministic discovery and AI-assisted portfolio synthesis

Setup proceeds in two stages.

### Stage A — deterministic bootstrap

DevCadence discovers and verifies hardware/storage/accelerators, local runtimes/models, agent/coding CLIs or SDK surfaces and auth readiness, configured APIs without reading raw secrets, principal hosts, capability/session evidence, configured EconomicRegime/BudgetPool bindings, and project/user privacy/spending/preferences.

This produces ResourceInventory and readiness without requiring model cognition.

### Stage B — portfolio synthesis

As soon as one policy-allowed endpoint meets minimum portfolio-planning capability, DevCadence may ask it to reason over ResourceInventory, DevCadence role requirements, project shape, user policy and historical evidence.

PortfolioRecommendation output is advisory. Deterministic validation rejects nonexistent/unready endpoints, unsupported capabilities/session features, privacy or spending violations, impossible independence claims and any authority expansion.

If no planning endpoint exists, setup remains useful and can guide the user to one minimal cognition path. Resource changes can later trigger an incremental recommendation instead of full reinstall.

See [COGNITION_PORTFOLIO.md](COGNITION_PORTFOLIO.md) and ADR-0018.

## 11. Setup actions and authority

Setup uses structured `SetupAction` records/plans.

Typical authority classes:

### Read-only

- detect hardware/software;
- inspect versions;
- inspect auth readiness;
- run non-sensitive smoke tests.

### User-level confirmation

- create DevCadence directories/config;
- create virtual environments;
- install user-level packages;
- download a model;
- install a user-level principal-host integration;
- launch an official authentication flow.

### Privileged confirmation

- install system packages;
- create/change services;
- change groups/device permissions;
- install GPU userspace components.

### High-impact/manual

- kernel/graphics-driver replacement;
- major CUDA/ROCm driver changes;
- operations requiring reboot;
- broad credential-store mutation.

DevCadence MUST NOT silently perform privileged/high-impact remediation.

`devcadence setup --dry-run` should expose the planned actions before mutation.

## 12. Credentials and authentication

DevCadence configuration stores credential references, never routine raw API keys.

Preferred sources:

- provider's existing authenticated CLI/session;
- OS credential manager / Secret Service;
- supported browser/device authorization;
- protected environment;
- restricted-file fallback for headless systems only when necessary.

Conceptually:

```yaml
provider:
  endpoint: cloud-economy
  credential_ref: cred_openai_default
```

Setup should discover existing usable authentication before asking the user to create another account/token.

Authentication and provider smoke tests must avoid sending repository source.

## 13. Principal hosts

Principal-host discovery and onboarding are defined in [PRINCIPAL_HOSTS.md](PRINCIPAL_HOSTS.md).

The initial first-class host set is intentionally bounded to:

- Antigravity;
- Cursor;
- Visual Studio Code.

If none is installed, this is a normal blank-machine state. Setup asks which supported host the user wants, guides/automates installation where safe, installs/configures the DevCadence integration when that milestone supports it, and verifies connectivity.

The user may skip principal-host setup and return later.

## 14. Guided terminal UX

The setup/doctor experience should be pleasant without becoming a full-screen application.

Preferred Go stack for the bootstrap implementation:

- **Huh v2** for forms, selects, multi-selects, confirmation and simple spinners;
- **Bubble Tea v2** underneath/when richer dynamic state is needed;
- **Lip Gloss v2** for restrained styling.

Guidelines:

- favor inline flows over a persistent dashboard;
- use color as enhancement, never as the only status signal;
- use Unicode status marks only with sensible terminal fallbacks;
- use spinners/progress only for genuinely long work;
- do not animate deterministic fast checks merely for decoration;
- work correctly in local terminals and ordinary SSH sessions;
- provide accessible/basic prompt mode;
- detect non-interactive stdin/stdout and avoid TUI control sequences;
- support machine-readable/plain modes such as `--json` and `--no-tui`.

Examples:

```text
$ devcadence setup

Environment
  ✓ Git 2.x
  ✓ 32 GB RAM
  ✓ AMD GPU detected
  ! Vulkan acceleration not configured
  - Ollama not installed

Recommended profile: HYBRID_THIN

What would you like DevCadence to configure?

  > Local lightweight inference
    Principal host
    Remote coding endpoint
    Skip optional setup
```

## 15. CLI surface

Conceptual commands:

```text
devcadence setup
devcadence setup hardware
devcadence setup inference
devcadence setup cognition
devcadence setup principal
devcadence setup consultants
devcadence setup auth

devcadence doctor
devcadence doctor --fix
devcadence setup --dry-run
devcadence setup verify
```

The interactive UI is an adapter over typed setup/capability services. Business logic must remain independently testable and usable without a terminal.

## 16. Milestone ownership

This subsystem is implemented progressively.

### M3A — Environment intelligence + cognition runtime — **implemented**

- hardware/software discovery (`internal/environment`);
- accelerator/backend candidates, as a pure function of observed facts;
- actual acceleration verification from an authoritative runtime signal
  (`internal/cognition`);
- local and remote cognition endpoint abstraction
  (`protocol.CognitionEndpoint`, `cognition.Adapter`);
- capability profiles (`protocol.MachineCapabilityProfile`);
- endpoint health, as the explicit installed → configured → ready chain;
- lightweight operational measurements;
- capability-based routing with an explainable decision.

Implementation notes that qualify the prose above:

- The **remote-API** kind ships as an adapter boundary plus a deterministic
  client, not a provider implementation. No provider SDK enters the module.
- **Measured capability** means operational properties — answers, emits valid
  JSON once, timing, runtime-reported tokens and memory. Reasoning and coding
  quality need evaluation history, so those grades are either declared by the
  operator (recorded as `configured`) or stay `unknown`.
- Coding-CLI **authentication** is `unknown` unless a probe's output says the
  user is signed out. No supported CLI publishes a safe, non-mutating way to ask,
  and DevCadence will not read credential files to find out.
- Everything is **read-only**. Contracts are settled in
  [adr/0013-environment-intelligence-and-cognition-contracts.md](adr/0013-environment-intelligence-and-cognition-contracts.md).

### M3B — Guided deterministic bootstrap — **not implemented**

- `setup` / `doctor`;
- dry-run remediation plans;
- installation/configuration recipes;
- credential references/auth discovery;
- deterministic ResourceInventory/readiness projection;
- terminal UX;
- blank-machine flow.

### M3C — Adaptive cognition portfolio — **planned**

- EconomicRegime, BudgetPool and dynamic BudgetState contracts;
- invocation/session driver capability model;
- AI-assisted PortfolioRecommendation over deterministic inventory + policy;
- deterministic recommendation validation and activation;
- CognitionPortfolio persistence/versioning;
- workflow-topology planning and explanation;
- incremental re-recommendation when resources/policy materially change.

M3B and M3C may be presented by one user-facing `devcadence setup` experience.

### M4 — Principal host integration

Consume M3 discovery to configure the semantic MCP/principal integration for supported hosts. Antigravity is the reference integration; Cursor and VS Code are first-class supported targets.

### M5 — Adaptability proof

The central experiment should include materially different environments, including:

- strong local Apple Silicon;
- thin 32 GB-class Linux/hybrid;
- no-local-model/cloud-cognition.

### M6 — Consultants

Consume discovered cognition endpoints. Consultant providers are optional; no particular commercial subscription is required.

## 17. Test strategy

Use deterministic fakes for most discovery/remediation tests.

Important cases:

- blank machine;
- runtime installed but acceleration absent;
- runtime claims/uses CPU when GPU was expected;
- unsupported ROCm but viable Vulkan candidate;
- Apple Silicon with MLX absent;
- NVIDIA hardware with missing/unhealthy driver;
- existing authenticated coding CLI;
- no cloud credentials;
- no principal host;
- only one supported principal host installed;
- privileged remediation declined;
- setup interrupted and resumed;
- TTY vs SSH vs non-interactive execution;
- accessible/no-TUI mode.

Hardware-specific integration probes should be separable from the deterministic test suite.

## 18. Design principle

The onboarding goal is not:

> install the DevCadence-preferred stack.

It is:

> understand the user's actual machine and existing tools, assemble the best policy-compliant configuration from what is available, and explain/guide the smallest changes needed to become more capable.

# Environment Intelligence and Guided Onboarding

## Scope

DevCadience must be useful on a blank developer machine, not only on a preconfigured workstation.

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

DevCadience MUST NOT report GPU acceleration as ready merely because a GPU or inference runtime is installed.

A supported acceleration path becomes `verified` only after a real probe demonstrates that the selected runtime actually executes through the intended backend.

Useful evidence may include:

- runtime-reported processor/backend;
- vendor/OS telemetry;
- a small inference workload;
- plausible observed throughput/memory behavior.

One signal alone should not be treated as infallible when better evidence is available.

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
  cost_class: subscription
  source_exposure: focused_or_tool_mediated
```

Roles bind to capabilities, not permanent model names.

## 7. AI software discovery

Discovery SHOULD recognize supported tools even when DevCadience did not install them.

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

## 9. Deployment profiles

Profiles are recommendation templates, not architectural modes.

### local-heavy

Strong local implementation/review capability plus optional frontier escalation.

### hybrid-thin

Deterministic local repository work, small/local cognition where useful, economical remote implementation/review, frontier escalation when needed.

This is the expected profile for machines such as a modest 32 GB Linux box with an integrated GPU.

### cloud-cognition

Local control plane, repository, worktrees, tools and evidence; model cognition is remote according to policy.

### offline

Control plane + deterministic engineering execution only. LLM-dependent roles are unavailable until configured.

### custom

Operator-defined routing/privacy/cost policy.

## 10. Recommendation inputs

The recommendation engine should consider:

- observed hardware/software capabilities;
- verified acceleration;
- measured model/runtime behavior;
- required role quality;
- project privacy policy;
- whether source may leave the machine;
- available authenticated subscriptions/tools;
- monetary budget/cost class;
- observed historical model performance.

Do not prefer local inference merely for ideological consistency. Prefer the cheapest/most private option that satisfies the required quality and policy.

## 11. Setup actions and authority

Setup uses structured `SetupAction` records/plans.

Typical authority classes:

### Read-only

- detect hardware/software;
- inspect versions;
- inspect auth readiness;
- run non-sensitive smoke tests.

### User-level confirmation

- create DevCadience directories/config;
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

DevCadience MUST NOT silently perform privileged/high-impact remediation.

`devcadience setup --dry-run` should expose the planned actions before mutation.

## 12. Credentials and authentication

DevCadience configuration stores credential references, never routine raw API keys.

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

If none is installed, this is a normal blank-machine state. Setup asks which supported host the user wants, guides/automates installation where safe, installs/configures the DevCadience integration when that milestone supports it, and verifies connectivity.

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
$ devcadience setup

Environment
  ✓ Git 2.x
  ✓ 32 GB RAM
  ✓ AMD GPU detected
  ! Vulkan acceleration not configured
  - Ollama not installed

Recommended profile: HYBRID_THIN

What would you like DevCadience to configure?

  > Local lightweight inference
    Principal host
    Remote coding endpoint
    Skip optional setup
```

## 15. CLI surface

Conceptual commands:

```text
devcadience setup
devcadience setup hardware
devcadience setup inference
devcadience setup cognition
devcadience setup principal
devcadience setup consultants
devcadience setup auth

devcadience doctor
devcadience doctor --fix
devcadience setup --dry-run
devcadience setup verify
```

The interactive UI is an adapter over typed setup/capability services. Business logic must remain independently testable and usable without a terminal.

## 16. Milestone ownership

This subsystem is implemented progressively.

### M3A — Environment intelligence + cognition runtime

- hardware/software discovery;
- accelerator/backend candidates;
- actual acceleration verification;
- local and remote cognition endpoint abstraction;
- capability profiles;
- endpoint health;
- lightweight benchmarks;
- capability-based routing.

### M3B — Guided bootstrap

- `setup` / `doctor`;
- dry-run remediation plans;
- installation/configuration recipes;
- credential references/auth discovery;
- deployment-profile recommendation;
- terminal UX;
- blank-machine flow.

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

> install the DevCadience-preferred stack.

It is:

> understand the user's actual machine and existing tools, assemble the best policy-compliant configuration from what is available, and explain/guide the smallest changes needed to become more capable.

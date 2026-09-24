# ADR-0011: Adaptive environment intelligence and host-independent cognition

- **Status:** Accepted
- **Date:** 2026-09-21
- **Related:** FR-011, FR-019, FR-027, FR-028, NFR-002, NFR-003, NFR-013, NFR-014
- **Documents:** docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md, docs/PRINCIPAL_HOSTS.md, docs/MODEL_RUNTIME.md

## Context

The original bootstrap assumptions emphasized a 48 GB Apple Silicon developer machine, strong local coding models, Ollama/MLX-LM, and Antigravity as the principal environment.

Those assumptions are useful reference configurations but are too narrow as product requirements.

A real DevCadence installation may begin on:

- a modest Linux machine with 32 GB RAM and an integrated GPU;
- hardware where a strong local coding model is impractical;
- a machine with no local model runtime installed;
- a machine where an inference runtime exists but silently falls back to CPU;
- a developer who already has one authenticated coding CLI but not another;
- a developer with no Antigravity/Cursor/VS Code installation;
- a developer who does not subscribe to every supported AI provider.

The control plane already separates durable state, worktrees, validation and model cognition. The deployment/onboarding architecture should preserve that separation rather than equating local-first with local-only inference.

## Decision

### 1. Local-first describes authority, not inference location

DevCadence keeps project authority, repository mutation, deterministic validation, evidence and canonical state under the local control plane.

Model cognition may be local or remote according to capability, privacy, cost and policy.

No strong local model is required for the control plane to operate.

### 2. Cognition is capability-routed

Core roles depend on required capabilities, not permanent model/provider identities.

Cognition endpoints may include:

- local model runtimes;
- authenticated coding/agent CLIs;
- remote APIs;
- future remote/LAN inference workers.

Missing optional endpoints reduce available capability rather than automatically failing setup.

### 3. Environment intelligence is a product capability

Bootstrap assumes a blank machine.

DevCadence discovers hardware, accelerator candidates, software, supported principal hosts, cognition endpoints and authentication readiness; builds a structured assessment; proposes a remediation/setup plan; requests approval; verifies the result; and records measured capability.

### 4. Acceleration must be empirically verified

GPU presence and runtime installation do not prove accelerated inference.

DevCadence reports an accelerator/backend as ready only after a supported real workload demonstrates use of the intended backend with sufficient evidence.

### 5. Setup recipes are versioned knowledge

OS/vendor/runtime-specific installation/remediation steps are represented as versioned recipes/knowledge rather than scattered imperative conditionals.

Privileged and high-impact setup actions require explicit approval.

### 6. Principal host is replaceable

Antigravity is the reference host, not a core dependency.

The initial first-class host scope is intentionally limited to:

- Antigravity;
- Cursor;
- Visual Studio Code.

Other hosts are future integration candidates.

No-host-installed is a valid initial state; setup asks the user which supported host to install/configure and allows deferral.

### 7. Consultants are optional

No individual consultant provider or commercial subscription is required.

The consultant subsystem consumes whichever compatible cognition endpoints are available and policy-allowed. Absence of consultants means reduced cognitive diversity, not control-plane failure.

### 8. Guided terminal UX is an adapter

Interactive setup/doctor flows use a modest terminal UI where appropriate. The intended Go stack is Huh v2 with Bubble Tea v2/Lip Gloss v2 underneath.

The UI is not business logic. Non-interactive/plain/JSON operation remains supported.

### 9. Later refinement: adaptive cognition portfolio

ADR-0018 refines the routing/economic layer without reversing this decision.

M3A endpoint discovery remains the factual substrate. M3B safely bootstraps/configures resources. M3C adds the deterministic access-channel/session/economic substrate and portfolio-validation boundary. M3D adds AI-assisted portfolio synthesis, adaptive workflow topology and the richer explain/setup experience.

Deployment labels such as strong-local/hybrid/no-local remain useful scenario descriptors, not the closed routing configuration.

## Consequences

### Positive

- DevCadence can run usefully on modest hardware.
- Existing user subscriptions/tools can be reused.
- Hardware/runtime misconfiguration such as CPU fallback becomes detectable.
- M3 can route coding to economical remote cognition without weakening local repository authority.
- M5 principal integration remains host-independent.
- M7 consultants do not impose subscription prerequisites.
- Onboarding becomes approachable for users unfamiliar with local LLM stacks.

### Costs

- Environment discovery and compatibility knowledge become real product surface.
- Hardware-specific verification requires platform integration tests.
- Credential/authentication discovery adds security-sensitive adapters.
- Routing policy must account for privacy and monetary cost, not only model quality.
- Three principal hosts create additional M5 integration/testing work.

### Scope control

The initial principal-host set stops at Antigravity, Cursor and VS Code.

The bootstrap setup engine should not turn into a universal package manager or GPU-driver manager. It plans and automates bounded supported actions; risky platform modifications remain explicit/manual where appropriate.

## Milestone mapping

- **M2:** unchanged; deterministic repository/process substrate.
- **M3A:** environment discovery, cognition endpoints, acceleration verification and capability evidence.
- **M3B:** deterministic setup/doctor/auth/remediation + ResourceInventory with safe plain/JSON/basic-terminal operation.
- **M3C:** provider-neutral cognition session/economic substrate and deterministic portfolio validation/activation.
- **M3D:** AI-assisted portfolio recommendation, adaptive task topology and richer adaptive setup/explanation UX.
- **M4:** early adaptive-cognition evidence gate against simpler baselines.
- **M5:** semantic MCP plus principal-host integration using the M3 substrate.
- **M7:** consultant/reviewer orchestration over discovered/validated cognition resources.

This ADR refines later milestone scope without requiring changes to M2 implementation.

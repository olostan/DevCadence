# Engineering Work Package: WP-M3C-2A — Session Execution Substrate and Silent Metering

- **Milestone:** M3C — Cognition Resource and Session Substrate
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3c-2--session-drivers-and-cognitive-invocation-compiler](../WORK_PACKAGES.md#wp-m3c-2--session-drivers-and-cognitive-invocation-compiler)
- **Base commit:** `248032c71453546ee355c1fe7d80f84849e2cb12` (origin/main, merge of PR #17 — Cognitive Invocation Compiler & Review Ledger)
- **Branch:** `feat/m3c-2a-session-substrate`
- **Task ID:** `task-m3c-2a-session-substrate-and-metering`
- **Work Package ID:** `WP-M3C-2A`
- **Version:** 1.0
- **Status:** Approved for Implementation

---

## 1. Context Manifest (AGENTS.md §2, docs/PROTOCOLS.md §10B)

```json
{
  "manifest_id": "manifest-wp-m3c-2a-v1",
  "task_id": "task-m3c-2a-session-substrate-and-metering",
  "work_package_id": "WP-M3C-2A",
  "work_package_revision": 1,
  "role": "principal_engineer",
  "base_commit": "248032c71453546ee355c1fe7d80f84849e2cb12",
  "project_state_revision": "bootstrap-m3c-1-closed",
  "read_envelope": [
    "AGENTS.md",
    "INVARIANTS.md",
    "docs/WORK_PACKAGES.md",
    "docs/PROTOCOLS.md",
    "docs/IMPLEMENTATION_PLAN.md",
    "docs/adr/0016-validation-services-bounded-tools-and-context-compaction.md",
    "docs/adr/0018-adaptive-cognition-portfolio-and-workflow-synthesis.md",
    "docs/adr/0019-non-conversational-cognition-and-adaptive-review.md",
    "docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md",
    "docs/work-packages/wp-m3c-1-ewp.md",
    "internal/protocol/access_channel.go",
    "internal/protocol/context.go",
    "internal/protocol/economics.go",
    "internal/protocol/portfolio.go",
    "internal/tools/scope.go"
  ],
  "write_scope": [
    "internal/cognition/drivers/*",
    "docs/work-packages/wp-m3c-2a-ewp.md"
  ],
  "domains": [
    "session_drivers",
    "process_normalization",
    "worktree_tool_mediation",
    "silent_multi_dimensional_metering",
    "semantic_loop_detection"
  ],
  "risk_tags": [
    "unbounded_subprocesses",
    "silent_token_runaway",
    "turn_countdown_prompt_pollution",
    "worktree_escape",
    "concurrency_races"
  ],
  "mandatory_clauses": [
    {
      "clause_id": "DCI-018",
      "source_doc": "INVARIANTS.md",
      "summary": "Authority does not imply residency: normative rules must not be preloaded wholesale."
    },
    {
      "clause_id": "DCI-019",
      "source_doc": "INVARIANTS.md",
      "summary": "Delegation must have no hidden requirements: all required constraints must be admitted."
    },
    {
      "clause_id": "DCI-045",
      "source_doc": "INVARIANTS.md",
      "summary": "Repeated failed attempts trigger escalation. Retries bounded by policy; infinite autonomous repair loops forbidden."
    },
    {
      "clause_id": "DCI-049",
      "source_doc": "INVARIANTS.md",
      "summary": "Review and repair campaigns are bounded. Closure criteria are policy-bounded."
    },
    {
      "clause_id": "DCI-054",
      "source_doc": "INVARIANTS.md",
      "summary": "Provider neutrality: semantics must not bake specific provider idioms into driver interfaces."
    },
    {
      "clause_id": "DCI-055",
      "source_doc": "INVARIANTS.md",
      "summary": "Equal treatment: local runtimes, authenticated CLIs, and direct APIs share one driver abstraction."
    },
    {
      "clause_id": "DCI-081",
      "source_doc": "INVARIANTS.md",
      "summary": "Zero plaintext credentials in session or driver records: use opaque CredentialRef only."
    },
    {
      "clause_id": "DCI-120",
      "source_doc": "INVARIANTS.md",
      "summary": "Roles are independent of providers and access channels: capability requirements, not vendor aliases."
    },
    {
      "clause_id": "DCI-121",
      "source_doc": "INVARIANTS.md",
      "summary": "Economics belong to access paths, not intrinsically to a model family."
    },
    {
      "clause_id": "DCI-130",
      "source_doc": "INVARIANTS.md",
      "summary": "Hosts and cognition drivers are orthogonal: human-facing host and machine-invocable session driver are separate."
    },
    {
      "clause_id": "DCI-131",
      "source_doc": "INVARIANTS.md",
      "summary": "Control-plane complexity does not imply prompt complexity: control plane enforces bounds deterministically."
    },
    {
      "clause_id": "ADR-0019-S1",
      "source_doc": "docs/adr/0019-non-conversational-cognition-and-adaptive-review.md",
      "summary": "ContextControl = ExactStateless | AppendOnly | OpaqueSession; PrefixCache = Explicit | Implicit | SessionKV | None."
    },
    {
      "clause_id": "ADR-0019-S2",
      "source_doc": "docs/adr/0019-non-conversational-cognition-and-adaptive-review.md",
      "summary": "Cognitive freedom with silent multi-dimensional metering: no artificial turn countdowns in prompts; pauses with PAUSED_BUDGET_EXCEEDED."
    },
    {
      "clause_id": "ADR-0020-S1",
      "source_doc": "docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md",
      "summary": "Control-plane rules are not model instructions by default: deterministic lifecycle and budgeting outside prompt."
    }
  ],
  "assumptions": [
    {
      "id": "asm-m3c-2a-substrate-split",
      "statement": "WP-M3C-2A is strictly the session execution substrate, drivers, and silent metering. The Cognitive Invocation Compiler is WP-M3C-2B.",
      "status": "verified",
      "material": true
    },
    {
      "id": "asm-m3c-2a-protocol-alignment",
      "statement": "Driver capabilities must strictly utilize and validate against protocol.ContextControl and protocol.PrefixCache defined in WP-M3C-1.",
      "status": "verified",
      "material": true
    }
  ],
  "expansion_triggers": [
    "Attempting to implement prompt rendering or compiler rule admission in 2A triggers escalation to 2B boundary.",
    "Attempting to add unmediated arbitrary shell execution into drivers violates DCI-010/ADR-0015 and triggers halt."
  ]
}
```

---

## 2. Execution Contract (Authoritative & Bounded)

### 2.1 Objective

Establish the session execution substrate (`internal/cognition/drivers`) across heterogeneous cognition endpoints:
1. Define the provider-neutral `SessionDriver` and `Session` interfaces normalizing:
   - Model selection and configuration (`SessionConfig`).
   - Session lifecycle: Initialize/Start, Resume, Cancel/Close.
   - Streaming and structured events (`DriverEvent`, `EventStream`).
   - Worktree/tool/MCP access mediation (`ToolMediator`, `ScopedToolMediator` with `tools.Scope`).
   - Reporting capabilities matching WP-M3C-1 protocol types: `ContextControl` (`exact_stateless`, `append_only`, `opaque_session`) and `PrefixCache` (`explicit`, `implicit`, `session_kv`, `none`).
2. Provide two materially different driver implementations:
   - Direct API / local runtime driver (`DirectAPIDriver`).
   - Authenticated CLI wrapper driver (`CLIWrapperDriver`).
3. Provide a fake third adapter (`FakeDriver`) and a comprehensive reusable driver contract test suite (`RunDriverContractTestSuite`) validating lifecycle, cancellation, streaming, tool mediation, and capability truthfulness.
4. Implement a silent multi-dimensional metering runtime:
   - Multi-dimensional tracking: cumulative input, cached, and output tokens, wall-clock duration per operation, cumulative wall-clock time, cumulative tool-call count, and semantic loop detection (oscillating edits, repeating identical failed tool calls).
   - Pausing execution with `PAUSED_BUDGET_EXCEEDED` on budget exhaustion and state checkpointing (`MeterSnapshot`, DCI-045, DCI-049).
   - Verifiable guarantee that artificial turn countdowns are NEVER injected into model prompts.

### 2.2 Verbatim MUST & MUST-NOT Constraints

- **MUST NOT inject artificial turn countdowns into prompts (ADR-0019 §2):** Prompts sent to models must never contain countdowns or artificial turn limits (e.g. "you have 5 turns left"). Outer resource limits are enforced silently by the control-plane runtime.
- **MUST pause with `PAUSED_BUDGET_EXCEEDED` on budget exhaustion (ADR-0019 §2, PROTOCOLS §10B):** When any configured token ceiling, wall-clock per-op limit, cumulative wall-clock limit, or tool-call limit is exceeded, execution must pause with status `PAUSED_BUDGET_EXCEEDED` and checkpoint state.
- **MUST detect semantic loops and escalate (DCI-045, ADR-0019 §2):** The runtime must detect oscillating file edits (reverting between states) and repeating identical failed tool calls, pausing with `PAUSED_BUDGET_EXCEEDED` and flagging escalation.
- **MUST cleanly cancel and terminate processes (DCI-055):** Context cancellation must terminate HTTP requests, streaming connections, and CLI subprocesses without orphan processes or hanging goroutines.
- **MUST enforce worktree containment on tool operations (ADR-0015):** Tool operations interacting with the worktree must be mediated through `tools.Scope.ResolvePath` to prevent directory traversal escapes.
- **MUST NOT advertise NativeWorktreeAccess without verified sandbox containment:** Default capability for `CLIWrapperDriver` MUST declare `NativeWorktreeAccess: false`. Process `Dir` sets execution CWD, but does NOT provide kernel-level filesystem containment. Filesystem modifications must be routed through mediated DevCadence tools (`ToolMediator`) for verified containment unless an explicit verified sandbox provider is configured.
- **MUST enforce process secret boundary (DCI-081):** CLI process specifications MUST be validated via `credentials.ValidateProcessSpecNoSecrets` at driver creation and execution boundaries, rejecting raw secrets in arguments or environment variables.
- **MUST NOT conflate endpoint kind with capability (ADR-0019 §1, DCI-054):** Local runtimes and remote APIs must truthfully report their actual `ContextControl` and `PrefixCache` capabilities without hardcoded assumptions.
- **MUST run reusable contract test suite across all three drivers:** `DirectAPIDriver`, `CLIWrapperDriver`, and `FakeDriver` must all satisfy the identical contract test suite.

---

## 3. Detailed Architecture and Component Specifications

### 3.1 Driver Interfaces & Types (`internal/cognition/drivers`)

```go
package drivers

// SessionDriver defines the interface for creating and resuming cognition sessions.
type SessionDriver interface {
	ID() string
	Capabilities() DriverCapabilities
	StartSession(ctx context.Context, cfg SessionConfig) (Session, error)
	ResumeSession(ctx context.Context, sessionID string, cfg SessionConfig) (Session, error)
}

// Session represents an active session.
type Session interface {
	ID() string
	DriverID() string
	Config() SessionConfig
	Status() SessionStatus
	ExecuteTurn(ctx context.Context, input TurnInput) (TurnResult, error)
	StreamTurn(ctx context.Context, input TurnInput) (EventStream, error)
	Close(ctx context.Context) error
}

// EventStream provides sequential consumption of DriverEvents.
type EventStream interface {
	Recv() (DriverEvent, error)
	Close() error
}
```

### 3.2 Tool & Worktree Access Mediation (`mediation.go`)

Mediation intercepts tool invocations before execution, validating path containment using `tools.Scope.ResolvePath`. Symlink escapes and `..` traversals are rejected with `errs.CategoryPolicyDenied`.
- **Structural Path Policy:** Path arguments are identified via custom `ToolDefinition.PathExtractor`, declared `PathParameters`, or robust recursive JSON inspection.
- **Mutation Semantics:** Tools declare `ToolDefinition.MutatesFiles bool`. Read-only tools (`MutatesFiles: false`) are validated for containment but never trigger `FileEditListener`, preventing false-positive oscillating edit loops on read operations.
- **Session Isolation & Listener Idempotency:** `ScopedToolMediator.ForSession` creates session-isolated mediators to prevent cross-session state mutation. `AttachMeterListeners(meterID, ...)` ensures listener registration is strictly idempotent across session resumes.
- **Tool Execution Observers:** Mediated executions notify `ToolExecutionListener` so that driver-internal tool calls are accurately metered and evaluated for semantic loops.

### 3.3 Silent Multi-Dimensional Metering Runtime (`metering.go`, `loop_detector.go`)

- **Multi-Dimensional Metrics:**
  - `CumulativeUsage.InputTokens`
  - `CumulativeUsage.CachedTokens`
  - `CumulativeUsage.OutputTokens`
  - `CumulativeDuration` and `LastOpDuration`
  - `CumulativeToolCalls`
  - `LoopDetector`: tracks consecutive identical failed tool calls and oscillating edit hashes.
- **Suspension:**
  - Transition to `SessionStatusPausedBudgetExceeded` (`"PAUSED_BUDGET_EXCEEDED"`).
  - Checkpoint generation: `MeterSnapshot` capturing exact state at pause.
  - Zero modification to user or compiled prompts.

### 3.4 Concrete Drivers

1. **`DirectAPIDriver`:** Direct API / local runtime driver supporting structured and streaming HTTP/JSON invocations, exact_stateless / append_only modes, explicit/none prefix caching.
2. **`CLIWrapperDriver`:** Subprocess CLI wrapper normalizing command line executions (stdout/stderr streaming or structured lines), opaque_session / append_only modes, worktree containment, and clean process killing on cancellation.
3. **`FakeDriver`:** In-memory configurable adapter supporting failure injection, latency simulation, and deterministic testing.

---

## 4. Acceptance Criteria & Deterministic Verification

| Deliverable / Requirement | Verification Command / Suite | Pass Criteria |
|---|---|---|
| Reusable Driver Contract Suite | `go test -v ./internal/cognition/drivers -run TestDriverContractSuite` | Passes across DirectAPIDriver, CLIWrapperDriver, and FakeDriver. |
| Cancellation Cleanliness | `go test -v ./internal/cognition/drivers -run TestContract_Cancellation` | Cancelling context halts execution promptly without leaking processes or goroutines. |
| Tool & Worktree Mediation | `go test -v ./internal/cognition/drivers -run TestWorktreeMediation` | Path traversal escapes outside worktree root are rejected with PolicyDenied. |
| Multi-Dimensional Token Metering | `go test -v ./internal/cognition/drivers -run TestMetering_TokenBudgets` | Exceeding input, output, or total token ceilings pauses with `PAUSED_BUDGET_EXCEEDED`. |
| Wall-Clock Metering | `go test -v ./internal/cognition/drivers -run TestMetering_WallClockLimits` | Exceeding per-operation or cumulative duration pauses with `PAUSED_BUDGET_EXCEEDED`. |
| Semantic Loop Detection | `go test -v ./internal/cognition/drivers -run TestLoopDetector` | Oscillating file edits and repeating identical failed tool calls trigger pause and escalation. |
| Zero Turn Countdown Guarantee | `go test -v ./internal/cognition/drivers -run TestMetering_NoPromptCountdownInjection` | Verifies prompt content is never modified or injected with turn limits. |
| Data Race Cleanliness | `go test -race ./internal/cognition/drivers/...` | Zero race conditions detected under concurrent execution. |
| Code Hygiene & Vetting | `gofmt -l .` and `go vet ./...` | Clean formatting and zero diagnostics. |

---

## 5. Non-Goals / Forbidden Scope for WP-M3C-2A

- **No Cognitive Invocation Compiler in 2A:** Mandatory clause resolution, ContextPack generation, and prompt renderers belong exclusively to WP-M3C-2B.
- **No Evidence Working Set Leases in 2A:** Leased evidence lifecycle and content-addressed cache invalidation belong to WP-M3C-2B.
- **No Vector / Dense Retrieval in 2A:** Embedding models and semantic search are non-goals for M3C.
- **No Prompt Countdown Pollution:** Invariant ADR-0019 §2 strictly forbids inserting artificial turn counts into prompts.

---

## 6. Escalation Triggers

- If an existing M0–M3B protocol definition or invariant must be altered to support drivers, halt and escalate.
- If a driver requires bypassing worktree scope containment to function, halt and escalate.

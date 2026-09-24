# ADR-0016: Supervised validation services, bounded execution tools, asynchronous operations, and multi-tier context compaction

- **Status:** Accepted
- **Date:** 2026-09-22
- **Related:** ADR-0004 (canonical task state machine), ADR-0008 (controlled process execution), ADR-0009 (artifact storage and validation execution), ADR-0011 (adaptive environment and host-independent cognition), ADR-0013 (environment intelligence and cognition capability contracts), ADR-0014 (guided bootstrap and remediation), ADR-0015 (declarative modules and scoped worktrees), DCI-001, DCI-002, DCI-003, DCI-010, DCI-011, DCI-013, DCI-014, DCI-032, DCI-033, DCI-041, DCI-052
- **Documents:** docs/ARCHITECTURE.md, docs/LOCAL_AGENTS.md, docs/MODEL_RUNTIME.md, docs/PROTOCOLS.md, docs/VERIFICATION.md

## Context

Integrating real-world execution and testing (such as Firebase emulators, Vite dev servers, or Flutter drivers) alongside constrained local cognition models requires resolving four architectural tensions:
1. **Background Service Lifecycle & Resource Leaks:** Validation suites often require companion daemon processes. Without rigorous supervision, dynamic port handoff, and verifiable process ownership, services leak across runs, clash on static ports, or risk killing unrelated processes upon restart.
2. **Context Bloat & Cognitive Traps in Repository Tools:** Unmediated command execution or text search can output thousands of lines, instantly exhausting local model context. Conversely, naive line numbering adds 20–30% token overhead and causes code generation errors.
3. **Asynchronous Execution vs. Busy-Polling:** Commands exceeding typical interactive latencies (e.g. 10s) must not hang the caller or force models into token-wasting polling loops.
4. **Context Compaction vs. Invariant Preservation:** When models approach context limits, naive sliding-window truncation drops the Engineering Work Package, invariants, or active amendments. Conversely, unchecked trajectory summarization risks turning speculative model claims into authoritative facts.

## Decision

### 1. Semantic Principal Boundary Preserved

- Raw tools (`read_file`, `grep_search`, `find_symbol`, `run_command`) are **execution-agent capabilities** scoped to isolated worktrees for `Repository Scout` and `Implementer` roles.
- The Principal's primary interface remains semantic engineering operations (`project_state`, `investigate`, `create_work_package`, `request_evidence`, `validate`, `review`).

### 2. Supervised Validation Services

- **Backward-Compatible Profile Schema:** `CheckSpec` and `Profile` in `internal/validation` are extended with `ServiceSpec`, supporting both legacy check lists and object-shaped profiles with duration strings.
- **Port Allocation & Concrete Handoff:**
  - `socket_inheritance`: Control plane binds `127.0.0.1:0` with `FD_CLOEXEC = false` and passes the file descriptor directly.
  - `env_var` / `cli_flag`: Ephemeral candidate port probe on `127.0.0.1` with bounded bind-conflict retry (up to 3 distinct candidates) and readiness verification.
  - Isolated temporary directory per run (`$DEVCADENCE_HOME/tmp/val_<run_id>/<service_id>`).
  - Network boundary: Declared as host-network execution with mock endpoint injection (kernel sandbox limitation explicitly documented).
- **Verified Teardown & Ownership:**
  - Services enforce an absolute `MaxLifetime` (default 15m).
  - Teardown executes unconditionally on success, failure, cancellation, and partial startup: graceful `SIGTERM` followed by forced `SIGKILL` and process reaping.
  - Controller restart reconciliation: PID files alone never authorize termination. Process ownership is verified via start time matching (anti-PID recycling). Full executable path resolution across platform variants is deferred to the background task daemon integration. If ownership cannot be verified, DevCadence reports `StatusCleanupFailed` / `unresolved_reconciliation` without signaling the PID.

### 3. Bounded Tools & Universal Artifact Pagination

- **Decoupled Output Sinks:** `internal/process.Runner` accepts decoupled `StdoutSink` and `StderrSink` (`io.Writer`) interfaces. In the current foundation, process execution decoupling is delivered in the runner; direct validation streaming into `internal/artifacts` with 4 KiB previews is scheduled for the full daemon milestone.
- **Metadata Distinctions:** Responses explicitly indicate `has_more: bool`, `capture_truncated: bool`, and `in_progress: bool`.
- **`fetch_content` Universal Pager:** Accepts `(content_ref, offset, limit, unit="lines"|"bytes")`, operating over immutable artifact snapshots with strict byte caps and contiguous pagination.
- **`read_file`:** `show_line_numbers` defaults to `false` to optimize tokens; set to `true` only for edit anchor targeting.
- **`grep_search`:** Normalized `ripgrep` / `git grep` backend capped at 20 matches with `content_ref` generated.

### 4. Asynchronous Controlled Operations

- A long-running command execution is an **`Operation`** (`OperationID`), distinct from an engineering `Task`.
- 10 seconds is a **response/yield threshold**, not a process timeout. After 10s, the operation returns `status: "running"` and continues uninterrupted under its original deadline.
- **Reactive Wakeup:** Principal hosts capable of event notifications wake the session on `OperationCompleted`. Synchronous-only hosts receive bounded status queries without busy-polling. Paused cognition state (`PAUSED_BUDGET_EXCEEDED`) is strictly decoupled from process completion. Durable SQLite event logging for operations is aligned with the daemon execution milestone.

### 5. Multi-Tier Context Compaction

- **Admission-Safe Context Budgeting:**
  - Denominator $C$ is the endpoint's verified/configured request token ceiling (`ContextTokens`). Unknown $C$ fails admission with an explicit error (`ErrContextLimitUnknown`); arbitrary defaults are forbidden.
  - Total serialized load: $T_{\text{total}} = T_{\text{in}} + T_{\text{reserve}}$. In the current foundation, token estimation uses heuristic field accounting, and summarizer delegation enforces privacy/locality policies; exact model-specific tokenizers and independent summarizer input admission are deferred to the cognition host integration.
  - Tier 1 (Deterministic Tool Pruning) triggers at $T_{\text{total}} > 75\% \cdot C$. Pruning replaces aged tool outputs with `content_ref` stubs while preserving atomic tool-call/result groups.
  - Stateful rearming: if Tier 1 yields $< 5\%$ reduction, it is marked exhausted for that turn.
  - Intermediate interval $(65\% \cdot C, 85\% \cdot C]$ proceeds to inference if it passes the final admission check.
  - Tier 2 (Episodic Trajectory Summarization) triggers if $T_{\text{total}} > 85\% \cdot C$.
  - **Universal Final Admission Guard:** On every path, $T_{\text{total}} \le C$ is asserted. If exceeded, the turn is halted with an admission escalation error; constraints are never dropped.
- **Assignment & Epistemic Preservation:**
  - Protected context preserves the original EWP and subsequent authorized amendments, user corrections, and stop instructions ordered by authority level and journal sequence.
  - Trajectory Digests are lossy derived context placed in lower-trust user messages (never system instructions). Digest claims (`authorized_by`, `evidence_ref`) must be validated against journaled records. Decision reference validity (`ReferenceValid`) is distinguished from statement verification.
  - Workspace checkpoints replace static diffs, tying test evidence to exact source SHAs and marking earlier evidence **STALE** after subsequent edits.
  - Summarizers inherit the parent session's exact privacy, locality, disclosure, and cost policies (`local_only` never routes remotely). Summarizer requests pass admission against their endpoint's limit without recursive compaction.

### 6. Syntactic Symbol Inspection

- `find_symbol` provides **syntactic pattern matching** via native Go AST for Go and regex-based syntactic matching (`syntactic-regex`) for TypeScript/JavaScript. Full Cgo-dependent Tree-sitter grammar parsing is deferred to a dedicated language-service enhancement to avoid Cgo build constraints in base environments.
- Output explicitly states `resolution_level: "syntactic"` and reports the active backend. Compiler-level semantic resolution (LSP) is deferred.

## Consequences

- **Positive:** Supervised test services prevent port conflicts and daemon leaks; execution tools prevent context overflow; context compaction safely fits local models while preserving assignment integrity and privacy.
- **Negative:** Services requiring non-standard port configuration require explicit `PortConfig` declarations.

### Delivered Foundations vs. Deferred Scope

| Component | Delivered Foundation | Deferred to Subsequent Milestones |
| :--- | :--- | :--- |
| **Output Sinks** | Decoupled `StdoutSink`/`StderrSink` in `process.Runner` | Direct validation streaming into `artifacts.Store` with live previews (Task Daemon) |
| **Operations** | In-memory `OperationManager` with 10s yield threshold and PID reconciliation | Durable SQLite operation events across daemon restarts (Task Daemon) |
| **Compaction** | Multi-tier pruning/summarization watermarks with strict final admission guard | BPE tokenizers and independent summarizer input admission (Cognition Integration) |
| **Symbols** | Native Go AST + TypeScript syntactic-regex with backend reporting | Cgo Tree-sitter parser (Language Service Enhancement) |
| **Reconciliation** | Start-time verification against PID recycling | Full cross-platform executable binary path validation |

## Amendment (2026-09-23): Evidence tiers within the bounded-tools boundary

- **Status:** Accepted
- **Decision owner:** Human (product owner)
- **Trigger:** M2.5 implementation review (PR #7) confirmed §1's boundary is
  currently structural (no MCP server exists to expose these tools at all),
  not a designed contract for when M5 wires one. §1 also treats
  `read_file`/`grep_search`/`find_symbol`/`run_command` as one undifferentiated
  "execution-agent" bucket, which understates a real difference between them.

### Problem

§1 says raw tools are execution-agent-only and the Principal's interface is
semantic operations. Taken literally and without refinement, that risks two
failure modes once M5 gives the Principal a real MCP surface:

1. **Silent scope creep:** nothing stops a future change from wiring
   `read_file`/`fetch_content` directly into the Principal's tool list,
   reintroducing the context-bloat failure mode ADR-0015/0016 exist to
   prevent — for the same reason it's wrong for execution agents, only worse
   at frontier-model prices and context sizes.
2. **Principal hesitation or false uncertainty:** if the Principal has *no*
   path to ground a specific factual claim ("does this function already
   handle nil?", "what does this error type look like?") except delegating
   an entire investigation task, it may hedge, qualify decisions with
   unverifiable assumptions, or decline to commit a Work Package rather than
   asking a small, answerable question. An architecture that makes "get one
   fact" and "read a file" the same cost produces exactly this behavior:
   models default to over-caution when their only escalation path is
   heavyweight, or they quietly read more than they need to once handed the
   capability. Neither is acceptable; the fix is a cheap, narrow, honest
   escalation path, not a hard wall.

### Decision

Split the bounded-tools boundary in §1 into two evidence tiers, not one:

1. **Search/locate tier — `grep_search`, `find_symbol` — Principal-eligible
   via `request_evidence`.** These return compact, bounded, inherently
   citable facts (a match list with line numbers; a symbol's resolved
   location and signature): they answer "does X exist / where / how many"
   without transmitting file contents. `request_evidence` (already listed as
   a Principal semantic operation in AGENTS.md §3 and M5's deliverables) MAY
   invoke these directly and return their structured result to the Principal.
   This is not a raw-tool exposure: the Principal still cannot run an
   arbitrary command or open a file; it can only ask a scoped question with a
   bounded answer shape.
2. **Content tier — `read_file`, `fetch_content`, `run_command` output —
   execution-agent-only, no exception.** The Principal never calls these
   directly, in M5 or after. When a Principal decision genuinely needs to
   see code, the correct path is `request_evidence(kind="snippet", ...)`:
   the Principal names a hit from tier 1 (or a file path plus a reason) and a
   *bounded* line range (hard cap, e.g. ≤200 lines / a few KB, enforced
   server-side, not model-side); an execution agent performs the actual
   `read_file`/`fetch_content` call in its own worktree context and returns
   only the requested excerpt, with a `content_ref` for provenance. The
   Principal is never handed an open-ended file-reading tool, but it is never
   more than one bounded, cheap call away from grounding a specific claim —
   there is always an answerable next step short of delegating a full
   investigation task.

This is additive to §1 and §3 above and constrains how M5's `request_evidence`
must be implemented; it changes no delivered code in this milestone (no MCP
server exists yet), and it does not authorize exposing `read_file`/
`fetch_content` to any principal host before this snippet-mediation path
exists.

### Consequences

- **Positive:** the Principal has a strictly bounded, always-available way to
  verify a specific factual claim, which should reduce both false-uncertainty
  hedging and the temptation to over-provision raw file access "just in
  case." Search/symbol evidence stays cheap enough to request liberally;
  content excerpts stay expensive/bounded enough to request deliberately.
- **Negative:** `request_evidence` now has two distinct fulfillment paths
  (direct tier-1 call vs. mediated tier-2 delegation) instead of one, adding
  implementation surface to M5 that a single undifferentiated boundary would
  not have needed.
- **Follow-up:** M5's Engineering Work Package for `request_evidence` MUST
  cite this amendment and specify the snippet size cap, the citation/reason
  requirement, and the routing between the two tiers before it is considered
  complete.


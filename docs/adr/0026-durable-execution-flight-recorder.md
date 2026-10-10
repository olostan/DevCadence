# ADR-0026: Durable Execution Flight Recorder and Offline Evidence Export

- **Status:** Proposed (pending independent review)
- **Date:** 2026-10-09
- **Owner:** DevCadence architecture
- **Related:** ADR-0002/0003/0006/0009/0016/0025; docs/OBSERVABILITY.md; docs/PROJECT_STATE.md; docs/SECURITY.md; docs/GO_ENGINEERING_PRACTICES.md
- **Scope:** diagnostic observations of setup, planning, routing, invocation, execution, validation, review, recovery and handoff; **not** new canonical state or execution authority

## Context and key constraint

DevCadence already persists **registered, typed** engineering domain events in SQLite (`internal/events`, `internal/storage`) and derives canonical ProjectState from that journal. The existing event package expressly disallows free-form payloads; ADR-0002 makes its transaction/journal authoritative. `internal/observability` presently provides slog correlation and is not a durable operation recorder. Task execution has persistent attempt/candidate artifacts. We need post-crash, independent, compact, exportable *diagnostic* evidence covering synchronous, parallel, asynchronous and eventually cross-node work without changing any of those authorities.

A portable execution trace must answer: what was attempted, which causal decision led there, when it started, whether a terminal outcome was recorded, which deterministic/model/tool outcomes are evidenced, what is still unknown, and which artifacts could clarify it. A trace is **reported evidence**, not independent proof of complete instrumentation or correct behavior.

## Decision

### D1. Separate the authoritative domain journal from diagnostic recording

Introduce a **diagnostic flight recorder** as a separate, derived/adjacent subsystem. It MUST NOT mutate canonical state, replay diagnostic records as authoritative domain transitions, relax existing registered-event validation, replace review/approval receipts, or introduce an additional definition of task completion. A diagnostic record may embed bounded safe summary fields and reference the corresponding canonical `event_id` / sequence / transaction identity. A diagnostic record may use arbitrary JSON or `Any`; **canonical** event registration and schemas stay strict.

A crash between a canonical transaction and diagnostic append may cause mismatch. Export MUST report the gap when detectable; the diagnostic write is not a second transactional commit. For safety-critical effects that require crash recovery, existing canonical durable intent/state is primary. Instrumentation cannot promise cross-store atomicity.

### D2. Record operation lifecycle with distinct schemas in one journal

Use one logical, per-runtime append-only journal, containing framed records of different types:

- `OperationStart`: immutable identity, name, parent, trace/actor/node/runtime/task/attempt IDs, UTC start time, bounded **initial** safe metadata and any available durable artifact references.
- `OperationEnd`: operation ID, UTC end time, explicit outcome/status/error code, bounded **result** metadata, usage if known, artifact references; does not repeat start metadata.
- `Observation`: operation ID, time, event name, safe arbitrary diagnostic payload, optional references; optional for progress, decisions and pre-effect audit.
- `JournalHealth`: detectable gaps, dropped diagnostics, recovery truncation, segment rotation and writer errors.

One terminal end per operation (completed/failed/cancelled), enforced by the local operation handle; duplication or orphan END is a reportable anomaly. A START without END on disk means **unresolved/unknown**, not necessarily failed: an external effect may already have happened. START must be durably persisted *before* effectful operations for which the recorder is the designated pre-effect audit; for existing canonical operations, the canonical intent remains the mandatory durable authority. An operation is not safe to retry solely because its END is absent. Preserve existing uncertain-effects/reconciliation guards.

### D3. Propagation and dependency injection

Inject a recorder/tracer implementation in existing constructors/options at **meaningful boundaries** (task delegation, model invocation, commands, validation, review, candidate/handoff; later setup and routing), rather than every pure helper. **Never** put recorder/tracer/journal writer/mutable handle in `context.Context`, and avoid globals and service locators. `context.Context` carries cancellation/deadlines plus **immutable** trace ID, current operation ID, and related correlation/causation values using private typed keys. `Start(ctx, name, metadata)` is a method on an *injected* recorder, returning a derived context and a local operation handle. The handle may accumulate bounded, concurrency-safe result fields; END snapshots those fields. Intermediate evidence that must survive a crash is written separately and durably, not held exclusively until END.

Detached goroutines must explicitly establish task-owned lifetime and preserve trace identity; request cancellation must not be silently inherited or silently discarded. Cross-process hops transmit a small serializable trace context, never Go context objects or recorder instances. Support one primary parent and additional causal links for fan-in. IDs must be globally unique without a central sequencer; order within each stream is its local monotonic sequence, and cross-node ordering is a **partial order** based on causal references, not wall-clock comparison.

### D4. Payload flexibility without a registry service

The stable protobuf envelope and lifecycle messages carry typed IDs, phase, time, outcome and correlation. Subsystem-specific payload is an extensible `oneof`:
- bounded `bytes json_payload` (validate UTF-8/JSON and apply pre-persistence sanitization);
- optional `google.protobuf.Any proto_payload` with type URL and raw serialized bytes;
- optional content-addressed `ArtifactRef` for large/sensitive evidence.

No central per-event-type registry is required for *diagnostic* observations. `Any` does **not** decode magically outside environments with descriptors. Export may resolve descriptors from installed code or an explicitly packaged descriptor set later; lacking a descriptor MUST yield an opaque typed payload and `descriptor_missing`, never silent omission or fatal loss of the rest of the trace. Protobuf `Struct` is not required; raw JSON bytes avoid changing numeric representation. Preserve unknown protobuf fields; version envelope and segment format independently. Names should be stable and namespaced (e.g. `model.generate`, `validation.check`) without requiring exhaustive enum definitions.

Illustrative proto only; final field numbers, encoding, and ownership are an EWP decision:
```proto
message JournalRecord {
  uint32 schema_version = 1;
  string event_id = 2;
  string node_id = 3;
  string runtime_id = 4;
  uint64 stream_sequence = 5;
  oneof body {
    OperationStart start = 10;
    OperationEnd end = 11;
    Observation observation = 12;
    JournalHealth health = 13;
  }
}
message OperationStart {
  string trace_id = 1;
  string operation_id = 2;
  string parent_operation_id = 3;
  repeated string caused_by = 4;
  string operation_name = 5;
  google.protobuf.Timestamp at = 6;
  bytes json_metadata = 7;
}
message OperationEnd {
  string operation_id = 1;
  google.protobuf.Timestamp at = 2;
  string outcome = 3;
  string error_code = 4;
  bytes json_result = 5;
}
```
Actual envelope must also support actor/task/attempt/canonical-event and artifact references without embedding private raw content.

### D5. Disk journal, crash recovery and durability

Store data under a protected, configurable subdirectory of `DEVCADENCE_HOME`, conceptually `traces/nodes/<node-id>/<runtime-id>/segment-000001.pbj`, with artifact references resolved through the existing durable artifact infrastructure. The layout is *proposed*, not a migration of SQLite.

A segment begins with magic/version and writer metadata. Records are individually length-delimited framed protobufs with a bounded maximum length and checksum (e.g. CRC32C). Serialize physical appends under single-writer ownership **per stream**. Rotate segments and define bounded retention; readers tolerate a concurrently growing active segment. Scan to last complete valid frame after crash; retain/report offsets, tail truncation, unexpected corruption and gaps. Do not quietly reinterpret mid-segment corruption as clean EOF or discard all valid prior records. Protect journal paths from traversal/symlink attacks and untrusted worker modification; if protected storage cannot be ensured, integrity confidence is downgraded explicitly.

Durability classes:
1. **Critical intent**: synchronous write + durability barrier before the protected side effect, or rely on an existing canonical transactional intent; failure refuses the side effect when the journal is the designated required evidence mechanism.
2. **Important outcome**: promptly append, durable before dependent transition when the specific EWP requires that evidence.
3. **Diagnostic progress**: bounded async queue, explicit drop/gap accounting, may be lossy.

Critical records must not wait indefinitely behind telemetry. Writer error handling, fsync policy, directory/segment creation durability, shutdown and rotation semantics must be specified and fault-tested before implementing. Do not claim that a successful `write` alone survives sudden power loss. No exactly-once external effects claim: ambiguous outcomes require idempotency/reconciliation.

### D6. Offline-first export and privacy

`devcadence trace export --trace <id> --output <zip>` is an intended CLI, not yet implemented. It MUST read **persisted files** independently of live runtime memory, Ollama, other nodes or network. It decodes valid segments, reconstructs operation START/END joins and causal order, optionally resolves durable canonical events and artifacts, and reports what is missing, ambiguous, redacted, corrupt or unavailable. An unhealthy tail is not a successful complete export.

Portable package: `manifest.json` (versions, producers, completeness, redaction, missing streams/artifacts), `events.jsonl`, `summary.md` (derived, never authoritative), `artifacts.json` (identities, digests and availability), optional explicitly selected redacted `artifacts/`. Maintain event IDs in all views so an external reviewer can request exact evidence.

Sanitize/minimize **before journal persistence**. Do not persist tokens, credentials, environment secrets, raw private prompts or source by default. Enforce inline size limits, ownership/permissions and retention. An exporter must not imply that an artifact whose digest is referenced is actually present or verified. Where artifacts are required evidence, persist them and verify their digest before recording the reference. Export paths and archive entries must be safe.

### D7. Future distribution, local-first now

Each Linux or macOS runtime records locally while disconnected; stable node installation ID plus process/runtime instance ID and per-stream sequence avoid centralized allocation. Future asynchronous segment shipping/merging may deduplicate by event ID and maintain causal links across task handoffs; no consensus, globally serialized order, central collector, distributed lock or network requirement in MVP. Any remote handoff requiring authority remains governed by existing control-plane policy; diagnostic trace context never grants permissions.

## Alternatives and rationale

| Choice | Advantage | Reason not selected now |
| --- | --- | --- |
| Extend canonical SQLite events with free-form diagnostics | One persistence engine | Violates typed authoritative event contracts/ADR-0002; noisy progress becomes domain history |
| In-memory-only OpenTelemetry spans | Mature instrumentation, low code | Abrupt crashes lose unfinished spans; exporter cannot reconstruct from disk alone |
| Separate START/END files | Distinct streams | No useful semantics gained; cross-file ordering and crash reconciliation harder |
| Global mutable tracer or tracer in context | Very convenient API | Hidden dependencies, testability and future distributed ambiguity |
| Full broker/distributed observability backend | Search and dashboards | Deployment/operational cost before demonstrable need |
| **Selected: injectable local journal with lifecycle records** | Crash evidence, simple runtime, extensible schemas and future merging | Additional file store and explicit durability/integrity contract |

## Incremental delivery

- **WP-TRACE-1 (format and local recorder):** proto schema; frames/checksums/limits; disk writer and recovery scan; explicit durability API; immutable context IDs; injected recorder/no-op testing implementation. No broad subsystem changes.
- **WP-TRACE-2 (offline exporter):** independent reader, JSONL/Markdown and manifest, missing/orphan/partial/corrupt evidence, redaction and artifact references.
- **WP-TRACE-3 (narrow instrumentation):** MCP delegation → task executor → Ollama request → tool → validation/repair → candidate handoff, with only meaningful boundaries; establish trace ID at composition root. Broader setup/routing/scout and distributed ingestion are follow-on EWPs, not blocked by a frozen global event-type catalog.

## Acceptance / falsification

- Child operations started in concurrent goroutines share trace identity, have distinct operation IDs and correct parent/causal references; no cross-stream total-order fiction.
- A child receives only derived immutable trace values via `context.Context`; injected recorder can be replaced with an in-memory fake; pure helpers remain uninstrumented.
- Terminate a separate test process *without* defers after a durable START; another process exports it as unresolved with metadata, no runtime/in-memory dependencies.
- Crash after an external effect but before END: export states outcome unknown and retry guard is not weakened.
- Export valid prefix plus truncated tail and record damage offsets; detect orphan END, duplicate END, missing artifact, missing optional Any descriptor, gaps and redaction.
- Failure of critical persistence stops the protected effect when required; overload of diagnostic queue cannot starve critical records.
- Candidate with passing checks but missing review evidence is labeled honestly; typed canonical state remains unchanged by diagnostic replay.
- Boundary tests show no worker writes to journal paths, no secrets in default export, no path traversal in stored/extracted artifacts.
- Future two-node fixture can merge causally linked locally generated segments without contacting either producer.

## Deferred questions for bounded EWPs

Exact protobuf field IDs / schema directory; fsync batching latency policy; local writer exclusivity and restart-generation IDs; whether to use the existing `clock.Clock` and `ids.Source` directly; segment index and retention defaults; artifact confidentiality classes; deterministic identity mapping into existing `events.Correlation`; whether a materialized SQLite read-only trace index is eventually worthwhile. These do not authorize an implementer to invent security- or durability-sensitive behavior: close them at EWP readiness before coding.

## Consequences

Additional disk use, careful recovery code and potential fsync cost in exchange for post-crash explainability. Raw trace evidence must be treated as privacy-sensitive. The recorder is not a proof of full system correctness, and missing events remain explicit uncertainty. This ADR is **proposed**, not a claim that recording, export or multi-node execution currently exists.

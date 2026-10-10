# EWP WP-TRACE-1: Flight Recorder Foundation (format, journal, recovery, recorder)

- **Revision:** r2 (independent-review amendments applied; Principal decisions on E0-E6 recorded in §14)
- **Review status:** independent review READY-WITH-AMENDMENTS; all blocking and non-blocking amendments applied in r2. Implementation starts only after PR #94 (ADR-0026) merges to main (E0).
- **Base identity:** `main` @ `04fdd8857817f4d4ce76a91e0c54c1b8979661eb` (go 1.25.0). Design input: ADR-0026 + `docs/GO_ENGINEERING_PRACTICES.md` from `origin/docs/flight-recorder-adr-go-practices` (both Status: Proposed; see E0).
- **Delegation:** two sequential work units, WU-A then WU-B (§1). Each worker receives AGENTS.md, this contract section for its unit plus §2-§5, §9-§11, and the exact clauses cited.
- **Context Manifest (record in PR):** role=implementation worker; read scope = files in §12 "read"; write scope = §12 "write"; no INVARIANTS/ADR/catalog edits; escalation triggers = §14. Full-document reads not required.

## 1. Objective and work-unit split

Deliver the Phase-1 diagnostic flight recorder: a crash-evident, append-only, single-writer, versioned on-disk journal of operation START/END, Observation and JournalHealth records; a recovery scanner; and an injected `Recorder` with a local `Operation` handle, sanitizer, deterministic path resolution and a bootstrap-safe constructor. It is **diagnostic evidence only** (ADR-0026 D1): it never mutates canonical state and is never an authority for task completion.

- **WU-A (format layer, no recorder semantics):** `internal/flightrec/wire` (schema + codec), `internal/flightrec/journal` (framing, writer, lock, recovery scanner, FS fault seam). Acceptance: §10 A-1..A-20. WU-A edits `go.mod`/`go.sum`, so it starts only after E0 (ADR-0026 merged) and E2 (dependency, accepted) are settled (§14).
- **WU-B (recorder layer, depends on WU-A merged):** `internal/flightrec` (context identity, Recorder, Op, Run, sanitizer, path resolution, bootstrap, no-op/degraded, health ring) + docs sync. Acceptance: §10 B-1..B-18.
- Dependency direction (MUST stay acyclic): `flightrec` -> `journal` -> `wire`. `wire` imports stdlib, `protowire` (`google.golang.org/protobuf/encoding/protowire`) and `errs`. `journal` imports `clock`, `ids`, `errs`, `wire`, `protowire`, stdlib. `flightrec` imports `journal`, `wire`, `clock`, `ids`, `errs`, `protocol` (only `LooksLikeSecret`, `ArtifactRef` conversion), stdlib.

## 2. Repository findings that bind this design (verified at base)

1. **No `protoc`, no `buf`, no `protoc-gen-go`** on PATH; `google.golang.org/protobuf` is NOT in `go.mod`/`go.sum`/module cache at base (a `go mod download` through the sandbox proxy succeeds; protobuf v1.36.10 is now present in this sandbox's local module cache, so the build works offline here; the offline fallback still holds: if the dependency can be neither resolved from cache nor downloaded, STOP and escalate, never hand-edit `go.sum`). No `go:generate` or generated code exists in the repo. CI (`.github/workflows/ci.yml`) runs `make fmt-check diff-check mod-check vet`, `make test schemas docs-check`, `make race`, coverage; none has a codegen step.
2. **Coverage guard** (`scripts/health/coverage-guard.sh`) measures whole-module statements with `-coverpkg=./...`, tolerance 0.00 pp, no generated-file exclusion. Checked-in `*.pb.go` (hundreds of mostly-unexecuted statements) would regress coverage, and excluding it would weaken a gate AGENTS.md §10 forbids weakening.
3. **Decision D-CODEC (within ADR-0026 "final field numbers, encoding" EWP discretion):** the schema is normative in a `.proto` file (`internal/flightrec/wire/journal.proto`, proto3, package `devcadence.trace.v1`) and the Go codec is **hand-written over `protowire`** (the only new dependency: `google.golang.org/protobuf`, pure Go; `go.mod` + `go.sum` change, `go mod tidy -diff` must be clean). The output is real protobuf wire format, decodable by any protobuf implementation; unknown fields are preserved. Drift is prevented by tests (A-3, A-4): a test-only `descriptorpb` + `dynamicpb` cross-codec check, and a test that parses the `.proto` text and compares `name = N` pairs to the descriptor table. No codegen step, no new CI tool, no generated files. A later EWP may swap in `buf`-generated code wire-compatibly (see E2).
4. Existing patterns reused: `clock.Clock` (UTC, µs-truncated; `clock.NewFake(start, step)`), `ids.Source{New(prefix) string}` + `ids.Valid` (shape `prefix_<26 Crockford>`; `ids.NewSequential()`, `ids.NewULIDSource()`), `errs.New/Wrap(Category, ...)` (`CategoryInvalidArgument`, `CategoryConflict`, `CategoryInternal`, ...), options-struct injection (`taskexec.Options`), POSIX `syscall.Flock` single-owner locks (`internal/execrt/lock.go`; build-tagged `internal/setup/lock_unix.go`), `protocol.LooksLikeSecret` (prefix/keyword secret heuristics) and `protocol.ArtifactRef{ID,Kind,Locator,MediaType,Digest,SizeBytes,Truncated}`, `$DEVCADENCE_HOME` resolution (`setup.ResolveHome`: env, else `$HOME/.devcadence`, must be absolute).
5. Hooks: `make hooks-install && make hooks-check` before first commit; do not bypass. `INVARIANTS.md`/`catalog.go` are NOT touched, so `make update-goldens` is NOT required (it refuses without a normative change).
6. `tests/boundaries_test.go` restricts third-party imports only for `coreDomainPackages` (new packages are not in it) and forbids model-provider SDKs (protobuf is allowed).

## 3. Concepts (definitions are normative)

| Term | Definition |
| --- | --- |
| `node_id` | Stable installation id `nod_<ULID>`; created once at `<root>/node-id` (O_EXCL, 0600, content = id + "\n"). If it cannot be persisted: ephemeral id for this process and health `NODE_ID_EPHEMERAL`. |
| `runtime_id` ("run id" in user language; CLI flag `--run` in Phase 2) | One recorder lifetime = one process start on one node: `run_<ULID>`. Unrelated to `agent_run_id` in docs/OBSERVABILITY.md (an agent invocation); do not conflate. Offline export selects a run by `(node_id, runtime_id)`. |
| `stream_id` | A single-writer, strictly sequenced record sequence inside a run. Phase 1: exactly one stream, constant `main`. Sequence, lock and segments are per stream. A child process, if ever instrumented, writes its own stream, never the parent's. |
| `trace_id` | `trc_<ULID>`: logical causal group of operations; may span streams, runs and (later) nodes. Export selects by trace across runs. |
| `operation_id` | `op_<ULID>`; `parent_operation_id` = the context's current operation at Start (empty for roots); extra causes are `Link`s. |
| `event_id` | `evt_<ULID>` unique per record (dedup key for future merging). |
| Order | `stream_sequence` (uint64, starts at 1, +1 per record, gapless when healthy) is the only order within a stream; cross-stream order is a partial order from causal references. Wall time is informational; `mono_nanos` is comparable only within one run. |

**Directory layout** (root from §8.5; dirs 0700, files 0600):
```
<root>/node-id
<root>/nodes/<node_id>/<runtime_id>/<stream_id>/LOCK
<root>/nodes/<node_id>/<runtime_id>/<stream_id>/segment-000001.pbj   (6-digit zero-padded, index from 1)
```

## 4. Schema (`journal.proto`, normative; field numbers are frozen once merged)

```proto
syntax = "proto3";
package devcadence.trace.v1;
import "google/protobuf/any.proto";   // wire-compatible; codec carries type_url+value itself

enum RecordType { RECORD_TYPE_UNSPECIFIED = 0; OPERATION_START = 1; OPERATION_END = 2; OBSERVATION = 3; JOURNAL_HEALTH = 4; }
enum Durability { DURABILITY_UNSPECIFIED = 0; DURABILITY_CRITICAL = 1; DURABILITY_DIAGNOSTIC = 2; }
enum Outcome { OUTCOME_UNSPECIFIED = 0; OUTCOME_COMPLETED = 1; OUTCOME_FAILED = 2; OUTCOME_CANCELLED = 3; OUTCOME_PANIC = 4; }
enum ObservationKind { OBSERVATION_KIND_UNSPECIFIED = 0; PROGRESS = 1; DECISION = 2; FACT = 3; AUDIT = 4; }
enum EvidenceState { EVIDENCE_STATE_UNSPECIFIED = 0; OBSERVED = 1; DOCUMENTED = 2; INFERRED = 3; CONFIRMED = 4; MISSING = 5; UNKNOWN = 6; }
enum HealthKind { HEALTH_KIND_UNSPECIFIED = 0; STREAM_STARTED = 1; SEGMENT_ROTATED = 2; DROPPED_DIAGNOSTICS = 3; WRITER_ERROR = 4; RECOVERY_REOPEN = 5; PATH_FALLBACK = 6; DEGRADED_NOOP = 7; NODE_ID_EPHEMERAL = 8; }

message Stamp { int64 wall_unix_nanos = 1; int64 mono_nanos = 2; }
message ArtifactRef { string id = 1; string kind = 2; string locator = 3; string media_type = 4;
  string digest = 5; /* "sha256:<hex>" */ int64 size_bytes = 6; bool truncated = 7; bool digest_verified = 8; }
message Link { string relation = 1; string trace_id = 2; string operation_id = 3; string event_id = 4; }
message Subject { string kind = 1; string id = 2; }
message Provenance { string source_kind = 1; string source_ref = 2; string observed_by = 3; string method = 4; Stamp observed_at = 5; }
message Sanitization { uint32 redactions = 1; uint32 truncations = 2; bool payload_replaced = 3; }
message Usage { uint64 input_tokens = 1; uint64 output_tokens = 2; uint64 duration_nanos = 3; }
message PathAttempt { string source = 1; string path = 2; string error_code = 3; }

message JournalRecord {
  uint32 schema_version = 1;        // 1
  string event_id = 2; string node_id = 3; string runtime_id = 4;
  uint64 stream_sequence = 5;
  RecordType type = 6;              // MUST agree with the set body member
  Durability durability = 7;        // class the writer used
  string stream_id = 8;
  oneof body { OperationStart start = 10; OperationEnd end = 11; Observation observation = 12; JournalHealth health = 13; }
}
message OperationStart {
  string trace_id = 1; string operation_id = 2; string parent_operation_id = 3;
  repeated Link links = 4;
  string operation_name = 5; Stamp at = 6; bytes json_metadata = 7;
  string actor_id = 8; string task_id = 9; string attempt_id = 10; string canonical_event_id = 11;
  repeated ArtifactRef artifacts = 12; Sanitization sanitization = 13;
}
message OperationEnd {
  string operation_id = 1; Stamp at = 2; Outcome outcome = 3; string error_code = 4; string error_summary = 5;
  bytes json_result = 6; Usage usage = 7; repeated ArtifactRef artifacts = 8; Sanitization sanitization = 9;
}
message Observation {
  string operation_id = 1; Stamp at = 2; string name = 3; ObservationKind kind = 4;
  string reason_code = 5; Subject subject = 6; Provenance provenance = 7; EvidenceState evidence = 8;
  repeated ArtifactRef artifacts = 9; repeated Link links = 10; Sanitization sanitization = 11;
  oneof payload { bytes json_payload = 20; google.protobuf.Any proto_payload = 21; }
}
message JournalHealth {
  HealthKind kind = 1; Stamp at = 2; uint64 dropped_count = 3; uint64 first_missing_sequence = 4; uint64 last_missing_sequence = 5;
  string detail_code = 6; string detail = 7; repeated PathAttempt attempts = 8; string selected_source = 9; int64 offset = 10;
}
message SegmentHeader {
  uint32 schema_version = 1; string node_id = 2; string runtime_id = 3; string stream_id = 4;
  uint32 segment_index = 5; uint64 first_sequence = 6; Stamp created_at = 7; uint32 writer_pid = 8;
  string writer_version = 9; uint32 max_record_bytes = 10; int64 mono_origin_wall_unix_nanos = 11;
}
```
`schema_version` = 1 (envelope) is independent of segment `format_version` = 1 (framing, §5). The ADR's illustrative `caused_by` strings are replaced by typed `Link`s (ADR marks its proto illustrative).

**Codec contract (`wire`, MUST):** one Go struct per message with exported fields plus `Unknown []byte`; `Marshal() ([]byte, error)` deterministic (ascending field number, proto3 zero values omitted, oneof member always emitted when set, repeated fields in order); `Unmarshal([]byte) error` tolerant: unknown fields append their raw `tag+value` bytes to `Unknown` and are re-emitted verbatim by `Marshal`; invalid UTF-8 in a string field -> `ErrInvalidUTF8`; truncated/overlong varint or length -> `ErrMalformed`; later oneof member overrides earlier (protobuf semantics); an unrecognised body field number leaves all body members nil, records the bytes in `Unknown`, and is NOT an error (future record types). `JournalRecord.Validate()` (separate from decode) requires: schema_version>=1, ids non-empty, sequence>=1, and `type` consistent with the set body member (a nil body with `type` outside 1..4 is "unknown record type", not invalid). Max decoded nesting depth: 3 (guard; reject deeper with `ErrMalformed`). Repeated message fields are capped by exported constants `MaxLinks`, `MaxArtifacts` and `MaxAttempts` (64 each; `OperationStart.links/artifacts`, `OperationEnd.artifacts`, `Observation.artifacts/links`, `JournalHealth.attempts`): `Unmarshal` returns `ErrMalformed` for the 65th entry (bounding the allocation a hostile 1 MiB record of empty entries can force) and `Marshal` returns `ErrInvalid` for more than the cap. Marshal of a record > `MaxRecordBytes` is rejected by the journal, not by `wire`.

## 5. Byte-level framing (format_version 1; all integers little-endian; CRC32C = Castagnoli polynomial, `crc32.MakeTable(crc32.Castagnoli)`)

**Segment header** (at offset 0):
```
off  size  field
0    8     magic = 89 44 43 4A 52 4E 4C 0A   ("\x89DCJRNL\n")
8    2     format_version = 1
10   2     flags = 0 (reader MUST reject nonzero reserved bits it does not know: finding BAD_HEADER)
12   4     meta_len  (1..4096)
16   N     meta = marshalled SegmentHeader (N = meta_len)
16+N 4     header_crc = CRC32C(bytes[0 .. 16+N))
```
Records start at `20+N`. **Record frame:**
```
0    4     sync = 46 52 4D 31 ("FRM1")
4    4     len      (1 <= len <= MaxRecordBytes)
8    4     crc = CRC32C( LE32(len) || payload )     // covers the length so a damaged length cannot masquerade as valid
12   len   payload = marshalled JournalRecord
```
Frame overhead 12 bytes. Limits (struct `journal.Limits`): `MaxRecordBytes` default 1 MiB (hard cap 8 MiB); `MaxSegmentBytes` default 64 MiB; `MaxHealthBytes` = 4096 (constant, not configurable). Validated, else `CategoryInvalidArgument`: `MaxRecordBytes >= MaxHealthBytes` and **`MaxSegmentBytes >= (20+4096) + 3*(12+MaxHealthBytes) + (12+MaxRecordBytes)`**, i.e. `16452 + MaxRecordBytes` (worst-case header with meta_len 4096, plus the at most three health frames that can precede a record in a fresh segment [segment-start health: STREAM_STARTED/RECOVERY_REOPEN/SEGMENT_ROTATED; WRITER_ERROR; DROPPED_DIAGNOSTICS], plus one maximal record). Worked numbers for tests: smallest legal `MaxRecordBytes=4096` requires `MaxSegmentBytes >= 20548`; rotation tests use `MaxRecordBytes=4096, MaxSegmentBytes=24576` with ~1 KiB bodies (about 20 records per segment); the defaults require `>= 1065028` (64 MiB satisfies). A segment never exceeds `MaxSegmentBytes`; rotation happens *before* the write that would exceed it (§6).
**Health cap:** every `JournalHealth` record is capped at `MaxHealthBytes=4096` marshalled: `detail` <= 1024 bytes (UTF-8-boundary truncation, suffix `...[truncated N bytes]`), `attempts` <= 8 (extras dropped, dropped count appended to `detail`), each `PathAttempt.path` <= 256 bytes; if still over the cap `attempts` is emptied. Capping is deterministic and done by the journal for every health record (callers cannot exceed it); health never yields `ErrRecordTooLarge`.

## 6. Journal writer (`journal.Writer`)

```go
type Durability uint8 // DurabilityCritical, DurabilityDiagnostic (maps 1:1 to wire.Durability)
type Config struct {
    Root       string        // trace root; Dir MUST be under Root; Open creates/syncs directories only at or below Root
    Dir        string        // <root>/nodes/<node>/<run>/<stream>; created 0700
    NodeID, RuntimeID, StreamID string
    Limits     Limits
    QueueSize  int           // diagnostic queue capacity, default 1024 (>=1)
    MaxBatch   int           // diagnostic records per mutex hold, default 64
    FlushEvery time.Duration // diagnostic fsync cadence; 0 disables the background ticker (tests call Flush)
    Clock      clock.Clock; IDs ids.Source  // required; wall stamps and event ids
    Mono       func() int64                // monotonic nanos since runtime start; required
    FS         FS                          // optional fault seam; nil = OS
    WriterVersion string
    // unexported test seams (set only by in-package tests): ownerUID func(os.FileInfo) (uid int, ok bool); selfUID func() int
}
func Open(ctx context.Context, cfg Config) (*Writer, error)
func (w *Writer) AppendCritical(ctx context.Context, rec *wire.JournalRecord) error // sync: written AND fsynced on nil return
func (w *Writer) AppendDiagnostic(rec *wire.JournalRecord) (accepted bool)          // non-blocking; never returns an I/O error
func (w *Writer) Flush(ctx context.Context) error                                   // drain queue, write, fsync
func (w *Writer) Stats() Stats  // Appended, Critical, Dropped, SegmentIndex, LastSequence, Broken bool, LastError string
func (w *Writer) Close(ctx context.Context) error                                    // idempotent
type FS interface {
    MkdirAll(string, os.FileMode) error; OpenFile(string, int, os.FileMode) (File, error); SyncDir(string) error
    ReadDir(string) ([]os.DirEntry, error); Lstat(string) (os.FileInfo, error); Open(string) (ReadFile, error)
}
type File interface { io.Writer; Sync() error; Close() error; Stat() (os.FileInfo, error) }
type ReadFile interface { io.ReaderAt; Stat() (os.FileInfo, error); Close() error }
```
`AppendCritical`/`AppendDiagnostic` take the record WITHOUT envelope identity fields; the writer fills `schema_version`, `event_id`, `node_id`, `runtime_id`, `stream_id`, `stream_sequence`, `type`, `durability` (callers cannot choose sequence). Caller sets only the body. A nil body is an invalid argument: `AppendCritical` returns `journal.ErrNilBody`; `AppendDiagnostic(nil)` returns false and counts a drop. After `Close` has completed nothing may open a segment: `Flush` re-checks `shut` under `w.mu` and returns `ErrClosed`, and `ensureActiveLocked`/`flushHealthLocked` refuse with `ErrClosed` on a shut writer.

**FS seam boundaries (normative):** the `LOCK` file is opened with `os.OpenFile` directly, NOT through `FS` (flock needs the real descriptor); fault tests therefore cannot inject LOCK open errors and cover that branch with real-filesystem cases (e.g. `LOCK` pre-created as a directory). Everything else goes through `FS`: A-10 injects via `FS.OpenFile` -> `File.Write/Sync` and `FS.SyncDir`/`FS.MkdirAll`; A-12 uses `ScanSegment` over crafted bytes (`io.ReaderAt`, no FS) and `ScanStream` over `FS.ReadDir`/`FS.Open`; A-13 uses the real OS FS plus `FS.ReadDir`/`FS.Open`; A-20 uses `FS.Lstat` and the `ownerUID` seam. Directory ownership uses the injectable `ownerUID(os.FileInfo) (uid, ok)` (`perm_unix.go`; `perm_other.go` returns `ok=false`, which skips the check) compared with `selfUID()` (default `os.Getuid`).

**Writer state (semantics):** `mu sync.Mutex` guards file, segment, size, `nextSeq`, pending-health set `{segStart, writerErr, dropped}`, `shut`; `cmu sync.RWMutex` guards `closed bool`; `queue chan` (cap `QueueSize`) is NEVER closed; `done chan struct{}` is closed exactly once by `Close`; `segmentIndex uint32` counts segment-creation attempts.

**Open (single-writer enforcement):** (1) `Lstat` the components from Root down to Dir to learn which directories this call will create; `FS.MkdirAll(Dir, 0700)`; `Lstat(Dir)` must be a real directory (not a symlink) and `ownerUID` (if `ok`) must equal `selfUID()` else `CategoryInvalidArgument`; then for **every directory newly created by this call** (outermost to innermost, never above `filepath.Dir(Root)`) `SyncDir(d)` and `SyncDir(filepath.Dir(d))`. (2) `os.OpenFile(Dir/LOCK, O_CREATE|O_RDWR, 0600)` directly; `flock(LOCK_EX|LOCK_NB)`; EWOULDBLOCK -> `errs.CategoryConflict` "stream already has a writer"; hold until Close (flock conflicts between separate descriptors even in one process, so same-process double-open is also refused). Non-unix: `Open` returns `ErrLockUnsupported` (recorder then degrades, §8.6). (3) `FS.ReadDir` existing segments: if any exist (reopen: tests/restart), run the scanner (§7, via `FS.Open`) to learn `lastSequence` and damage; never modify or truncate any existing file; set `segmentIndex = maxIndex` over all directory entries matching `segment-%06d.pbj` (even unparsable ones), then create the next segment with `first_sequence = lastSequence+1` and write `JournalHealth RECOVERY_REOPEN` (detail_code = finding codes, offset of first damage) as sequence `lastSequence+1`. Otherwise `segmentIndex=0`, first segment is index 1 and its first record is `JournalHealth STREAM_STARTED`. After the segment-start health, `Sync`. (4) Start the drain goroutine (and flush ticker if `FlushEvery>0`). Segment index above 999999 -> `ErrSegmentLimit` (writer broken).

**newSegmentLocked():** `w.segmentIndex++` BEFORE any I/O, on every attempt, successful or not; `OpenFile(segment-%06d.pbj, O_CREATE|O_EXCL|O_WRONLY|O_APPEND, 0600)`; write header (§5, `first_sequence = w.nextSeq`) in one `Write`; `file.Sync()`; `FS.SyncDir(Dir)`. Only then is the segment active. On failure at any step: close the handle best-effort, **never remove or truncate the leftover file** (the scanner reports a partial/empty segment as `BAD_HEADER`; its index is already consumed, so a retry uses index+1 and `O_EXCL` cannot collide), then `poisonLocked` and return the error. `openNextLocked()` = `newSegmentLocked()` then `flushPendingHealthLocked()` (segment-start health first). `rotateLocked()` = `Sync` and close the active segment, enqueue pending `SEGMENT_ROTATED`, `openNextLocked()`.

**Health write path (internal):** `writeFrameLocked(payload)` performs the single `Write` of one encoded frame, updates `size`, `nextSeq++`, `unsynced`; on error it calls `poisonLocked` and returns. It never rotates and never calls `appendLocked`. `flushPendingHealthLocked()` is the ONLY producer of health frames: in order segment-start, WRITER_ERROR, DROPPED_DIAGNOSTICS (delta = `dropped - reportedDropped`; `reportedDropped` advances only after a successful write), each built with envelope (`seq=nextSeq`, fresh event_id), capped (§5), marshalled and passed to `writeFrameLocked`. It requires an active segment whose capacity was reserved by the caller (see budget in §5).

**Append algorithm (shared by both classes; runs under `w.mu`; never used for health):**
```
appendLocked(body, class):
  if w.shut: return ErrClosed
  if w.active == nil: if err := w.openNextLocked(); err != nil { return err }       // recover from poisoning; pending health flushed into the fresh segment
  rec := fill(body, seq=w.nextSeq, event_id=IDs.New("evt"), class); payload := rec.Marshal()
  if len(payload) > Limits.MaxRecordBytes: return ErrRecordTooLarge                  // NOT poisoning; sequence NOT consumed
  est  := 12 + len(payload) + 10                                                     // seq varint may grow by <=10 bytes after re-fill
  pend := pendingHealthCount() * (12 + MaxHealthBytes)                               // worst case, includes a DROPPED item if dropped > reportedDropped
  if w.size + pend + est > Limits.MaxSegmentBytes:
       if err := w.rotateLocked(); err != nil { return err }                         // new segment holds SEGMENT_ROTATED etc. first (consumes sequences)
  else if err := w.flushPendingHealthLocked(); err != nil { return err }
  rec = fill(body, seq=w.nextSeq, event_id=IDs.New("evt"), class); payload = rec.Marshal()   // RE-FILL: health frames consumed sequences
  if len(payload) > Limits.MaxRecordBytes: return ErrRecordTooLarge
  return w.writeFrameLocked(encodeFrame(payload))     // fits: fresh segment = header + <=3 health + one maximal frame <= MaxSegmentBytes by §5 constraint
AppendCritical(ctx, body):
  if err := ctx.Err(); err != nil { return err }     // ctx is consulted ONLY here, before taking any lock; a started write/fsync is NEVER aborted
  cmu.RLock(); c := closed; cmu.RUnlock(); if c { return ErrClosed }
  w.mu.Lock(); defer w.mu.Unlock()
  err := appendLocked(body, Critical)
  if err == nil { if serr := w.active.Sync(); serr != nil { w.poisonLocked(serr); return wrap(serr) }; w.unsynced = false }
  return err
```
**poisonLocked:** close the active file, `active=nil`, `broken=true` (cleared only by a later successful `openNextLocked`), record `LastError`, set/refresh the single pending `WRITER_ERROR` item (coalesced: later errors update `detail`). The poisoned segment is never appended to again (a failed `fsync` means earlier unsynced diagnostics are not trustworthy; their loss shows as a sequence gap or torn tail). The next append attempts one fresh `openNextLocked`; if that also fails the append returns the error (critical callers see it; diagnostics are dropped with `dropped++`).
**Consistency after failures (documented, expected):** (a) `Sync` failing after a complete write: `AppendCritical` returns an error but the frame may be on disk unsynced, so the journal may show a START with no END; consistent because the caller refused the protected effect and START-without-END means "unresolved". (b) A `Write` reported as failed may nevertheless have left a complete frame in the poisoned segment; its sequence was not consumed, so the next segment reuses it and the scanner reports `SEGMENT_ORDER{Expected:S+1,Got:S}` (warning) at the new segment's boundary (section 7 precedence suppresses `SEQ_REGRESSION` for that segment's first record); this is expected and not corruption.

**Durability rules (normative):**
1. *Critical:* returns nil only after `write` of the complete frame AND `File.Sync()` succeeded (darwin: Go's `Sync` uses `F_FULLFSYNC`). The fsync also makes every earlier diagnostic record in that segment durable. Segment creation: header write + file `Sync` + `SyncDir` before the first record. Rotation: `Sync` the finished segment before creating the next.
2. *Diagnostic:* `AppendDiagnostic` does `cmu.RLock(); if closed {RUnlock; dropped++; return false}`, then a NON-BLOCKING send of a body copy on `queue` (`select` with `default`: full -> `dropped++`, return false), then `RUnlock`. The drain goroutine takes `w.mu` for at most `MaxBatch` records per hold (write only, no fsync; an `appendLocked` error drops that record with `dropped++`), then releases; it fsyncs only (a) when `FlushEvery` elapses and `unsynced`, (b) on `Flush`, (c) rotation, (d) `Close`. A critical append waits for at most one batch write, never for the queue or an fsync owned by diagnostics.
3. *Drop accounting:* `dropped` (atomic) is reported via `flushPendingHealthLocked` as one `DROPPED_DIAGNOSTICS{dropped_count=delta}` before the next record of either class; on `Close` it is written and synced. `Stats().Dropped` is cumulative.
4. A diagnostic record queued but not yet written at process death is lost without trace; only Critical records, and records followed by a later successful Critical append, are crash-durable.
5. **`Close(ctx)` lifecycle:** `cmu.Lock()`; if `closed` already, unlock and return nil (the first caller owns teardown; concurrent/second Close returns nil immediately); set `closed=true`; `close(done)`; `cmu.Unlock()`. Wait for the drain and ticker goroutines to exit, bounded by `ctx`. Then, under `w.mu`, the closer itself drains whatever remains in `queue` (non-blocking receive loop; skipped if `ctx` expired, remaining records counted in `dropped`), calls `flushPendingHealthLocked`, `Sync`, closes the segment, sets `shut=true`, `flock(LOCK_UN)` and closes LOCK. The queue is never closed, so no send can panic. If `ctx` expired the result is `ctx.Err()` joined with any I/O error, and the lock is still released (a write already in progress completes; it is never aborted). `Flush(ctx)`: `ctx.Err()` checked only before lock acquisition; it drains the records queued at call time in `MaxBatch` holds, then `flushPendingHealthLocked` and `Sync` under `w.mu`. Append after Close -> `ErrClosed` (critical) / `false` (diagnostic).
6. No retention or deletion in Phase 1 (E3). No file in a run directory is ever truncated, renamed or deleted by the writer.

## 7. Recovery scanner (`journal.Scan*`; read-only, no lock required, MUST NOT mutate files)

```go
type FindingCode string // TORN_TAIL, BAD_HEADER, BAD_SYNC, BAD_LENGTH, BAD_CRC, DECODE_ERROR, SEQ_GAP, SEQ_REGRESSION, SEGMENT_MISSING, SEGMENT_ORDER, RESYNC_ABANDONED, UNKNOWN_RECORD_TYPE, LIVE_TAIL
type Severity uint8 // SeverityInfo, SeverityWarning
type Finding struct { Code FindingCode; Severity Severity; Segment uint32; Offset int64; SkippedBytes int64; Expected, Got uint64; Detail string }
type ScanOptions struct { Limits Limits; MaxResyncBytes int64 /* default 16 MiB per damage region; ALSO bounds verified candidate payload bytes per region to 4*MaxResyncBytes */; AssumeLiveTail bool; PrevLastSequence uint64 /* ScanSegment only: seeds lastSeq (0 = first segment); ScanStream sets it itself */ }
type Scanned struct { Record *wire.JournalRecord; Segment uint32; Offset int64 }
type SegmentReport struct { Index uint32; Size int64; HeaderOK bool; FirstSequence uint64 /*from header*/; LastSequence, Records uint64; Findings []Finding; CleanEOF bool }
type StreamReport struct { Segments []SegmentReport; Findings []Finding /*all findings, ordered by (Segment, Offset), stream-level ones at Offset 0*/; Records, FirstSequence, LastSequence uint64; Clean bool }
func ScanSegment(r io.ReaderAt, size int64, segmentIndex uint32, opt ScanOptions, visit func(Scanned) error) (SegmentReport, error)
func ScanStream(fsys FS, dir string, opt ScanOptions, visit func(Scanned) error) (StreamReport, error) // orders by index; opens via FS.ReadDir/FS.Open
func WriterActive(dir string) (bool, error) // tries flock(LOCK_EX|LOCK_NB) on LOCK then releases
```
**Severity (fixed per code):** *info* = `UNKNOWN_RECORD_TYPE`, `LIVE_TAIL`; *warning* = every other code (`TORN_TAIL`, `BAD_HEADER`, `BAD_SYNC`, `BAD_LENGTH`, `BAD_CRC`, `DECODE_ERROR`, `SEQ_GAP`, `SEQ_REGRESSION`, `SEGMENT_MISSING`, `SEGMENT_ORDER`, `RESYNC_ABANDONED`). `Clean` is true iff there are zero warning findings (info never affects it; `SegmentReport.CleanEOF` = the segment ended exactly at a frame boundary with no warning in it). The error return is reserved for I/O errors on the scanner's own reads and for `visit` errors; data damage is NEVER an error return.

**Per-segment algorithm:**
```
parse header at 0: magic/format_version/flags/meta_len<=4096/header_crc/decode SegmentHeader/schema+index consistency
  any failure (incl. file shorter than header) -> Finding BAD_HEADER; segment contributes no records; stop segment (no resync: no trusted limits/sequence base)
o := headerEnd
loop:
  if o == size: break (clean EOF)
  if size-o < 12: TORN_TAIL(o, size-o); break
  if sync(o) != "FRM1": goto damaged(BAD_SYNC)
  len := LE32(o+4); if len==0 || len>MaxRecordBytes: goto damaged(BAD_LENGTH)
  if o+12+len > size: goto damaged(BAD_LENGTH)          // torn tail OR corrupt length; decided by resync result below
  if CRC32C(len||payload) != crc: goto damaged(BAD_CRC)
  rec := Unmarshal(payload); err or Validate fails: goto damaged(DECODE_ERROR)
  seqCheck(rec); visit(rec); o += 12+len; continue     // unknown record type: visit + info UNKNOWN_RECORD_TYPE
damaged(code):
  p := resync(o+1)   // byte-wise, bounded by MaxResyncBytes; first p>o where a candidate frame is fully valid: sync, len in range, within file,
                     //   first payload byte == 0x08 (schema_version tag; cheap pre-filter before the payload is read), CRC ok, decodes+validates, AND its node_id/runtime_id/stream_id equal the segment header's AND stream_sequence >= lastSeq
  if found p:   emit Finding{code, Offset:o, SkippedBytes:p-o}; o = p; continue      // later valid records still delivered
  if bound exhausted before EOF (the MaxResyncBytes window, or the per-region budget of 4*MaxResyncBytes candidate payload bytes read for verification; Detail names the latter): emit RESYNC_ABANDONED{Offset:o, SkippedBytes: scanned}; stop segment
  else (EOF reached, no valid frame): if code == BAD_LENGTH due to o+12+len > size, OR the remaining bytes are a strict prefix of a frame
                                         (valid sync, in-range len, incomplete payload) -> TORN_TAIL{o, size-o}
                                      else emit code with SkippedBytes=size-o;  stop segment
```
So `BAD_LENGTH` is reported only when a later frame is found (or len is zero/oversize), and `TORN_TAIL` only when EOF is reached; there is no combined code. CRC32C is not authentication: a valid frame embedded in a damaged record's payload that carries the segment's ids and `seq >= lastSeq` is accepted by resync (surfacing as `SEQ_GAP`); one with foreign ids or an older sequence is rejected (test A-19). Forgery is out of scope.

**Sequence checks and precedence.** `lastSeq` is stream-wide (`ScanStream` seeds it from the previous segment; `ScanSegment` from `PrevLastSequence`). `StreamReport.LastSequence` is the maximum over segments of the segment's last delivered sequence (`first_sequence-1` for a header-only segment), so a regressing segment never lowers it and `journal.Open` resumes above the maximum; a previous header-only segment still counts as the previous segment for the boundary check (seg1 header-only + seg2 `first_sequence=7` yields `SEQ_GAP{Expected:1,Got:7}`).
- Boundary (first record of segment k>1, evaluated from the header's `first_sequence` F): if indices skipped -> `SEGMENT_MISSING` only (no boundary `SEQ_GAP`). Else if `F == lastSeq+1` nothing; if `F <= lastSeq` -> `SEGMENT_ORDER{Expected:lastSeq+1, Got:F}`; if `F > lastSeq+1` -> `SEQ_GAP{Expected:lastSeq+1, Got:F, Detail:"segment boundary"}`. A `BAD_HEADER` segment yields no boundary check of its own; the next segment's check naturally reports the gap.
- The first record of a segment is then checked against F (not `lastSeq`), so one anomaly produces one finding; later records are checked against `lastSeq`:
  `seq == lastSeq+1` ok; `seq > lastSeq+1` -> `SEQ_GAP{Expected:lastSeq+1, Got:seq}` (record still delivered, `lastSeq=seq`); `seq <= lastSeq` -> `SEQ_REGRESSION` (delivered, `lastSeq` unchanged). `SEGMENT_ORDER` suppresses `SEQ_REGRESSION` for the first record of that segment when `rec.seq == F`.

`ScanStream` also reports `SEGMENT_MISSING` for a hole in segment indices, and downgrades a `TORN_TAIL` on the **last** segment to `LIVE_TAIL` (info) iff `AssumeLiveTail` and `WriterActive` is true; a `TORN_TAIL` in a non-final segment stays a warning and is expected after writer reopen/poisoning. Resync MUST NOT treat corruption as EOF or drop earlier valid records. Out-of-scope for the scanner: artifact existence, START/END joining (Phase 2).

## 8. Recorder layer (`internal/flightrec`, WU-B)

### 8.1 Identity in context (immutable; no recorder/writer/Op ever in `context.Context`)
```go
type SpanContext struct { TraceID, OperationID, ParentOperationID, NodeID, RuntimeID, StreamID string }
func FromContext(ctx context.Context) (SpanContext, bool)
func WithRemote(ctx context.Context, sc SpanContext) (context.Context, error) // cross-process hop; validates every non-empty id with ids.Valid, rejects the rest as CategoryInvalidArgument
func (sc SpanContext) MarshalText() ([]byte, error); func ParseSpanContext(string) (SpanContext, error) // "dc1;trc=..;op=..;nod=..;run=..;stm=.." ids-only wire form, <=512 bytes
```
Keys are private typed keys; values are copied on read. A remote context sets `ParentOperationID = remote OperationID` on the first local Start; `NodeID/RuntimeID` of a remote context are retained only as link metadata (never as this stream's identity).

### 8.2 Recorder, Starter, Op
```go
type Sink interface { // consumer-owned (defined in flightrec); journal.Writer satisfies it via thin adapter in recorder.go
    AppendCritical(ctx context.Context, body *wire.JournalRecord) error
    AppendDiagnostic(body *wire.JournalRecord) bool
    Flush(ctx context.Context) error
    Close(ctx context.Context) error
}
type Config struct { Sink Sink; NodeID, RuntimeID, StreamID string; Clock clock.Clock; IDs ids.Source; Mono func() int64; Sanitizer *Sanitizer /* nil=default */; Status Status }
func New(cfg Config) (*Recorder, error)            // strict: requires Sink, Clock, IDs, Mono, valid ids; else CategoryInvalidArgument
func NewNoop(st Status) *Recorder                  // degraded, no Sink
func (r *Recorder) Start(ctx context.Context, spec StartSpec) (context.Context, *Op, error)
func (r *Recorder) Observe(ctx context.Context, spec ObservationSpec) error // against current op in ctx; no op in ctx -> CategoryInvalidArgument
func (r *Recorder) Status() Status; func (r *Recorder) Health() []HealthEvent // bounded in-memory ring (256), always available even with no Sink
func (r *Recorder) Close(ctx context.Context) error
type Starter interface { Start(ctx context.Context, spec StartSpec) (context.Context, *Op, error) } // consumed by Run; consumers define their own
type StartSpec struct { Name string; Durability, EndDurability Durability; ActorID, TaskID, AttemptID, CanonicalEventID string; Links []Link; Metadata any; Artifacts []ArtifactRef }
type ObservationSpec struct { Name string; Kind ObservationKind; Durability Durability; ReasonCode string; Subject *Subject; Provenance *Provenance; Evidence EvidenceState; Payload any; Artifacts []ArtifactRef; Links []Link }
type Op struct{ /* unexported */ }
func (o *Op) ID() string; func (o *Op) TraceID() string
func (o *Op) SetResult(key string, v any)                 // concurrency-safe; <=64 keys, extra keys dropped+counted; applied via sanitizer at End
func (o *Op) AddArtifact(a ArtifactRef)                   // <=32; digest required to be "sha256:<64 hex>" if non-empty, else dropped+counted
func (o *Op) SetUsage(in, out uint64, dur time.Duration)
func (o *Op) Observe(spec ObservationSpec) error          // sets operation_id
func (o *Op) End(outcome Outcome, cause error) error      // exactly once
```
`Name` MUST match `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`, <= 96 bytes. `Link.Relation` MUST match the same charset (dots allowed, <= 48 bytes); well-known constants: `caused_by, follows_from, retry_of, fan_in, supersedes, evidence_for, derived_from, handoff_to`. Max 16 links/artifacts per record at Start/Observe; excess -> `CategoryInvalidArgument`.

**Start algorithm:**
```
validate spec (names, links, artifacts)                          -> CategoryInvalidArgument on failure (always returned, even for Diagnostic)
sc, ok := FromContext(ctx)
traceID := sc.TraceID if ok else IDs.New("trc"); parent := sc.OperationID if ok else ""
opID := IDs.New("op"); at := stamp(Clock.Now(), Mono())
meta, san := Sanitizer.JSON(spec.Metadata)
body := OperationStart{...};
nctx := withSpanContext(ctx, {traceID, opID, parent, NodeID, RuntimeID, StreamID})   // derived ctx keeps ctx cancellation/deadline
if r is noop or Sink==nil: count; return nctx, newOp(inert), nil
switch spec.Durability:
  Critical:   if err := ctx.Err(); err != nil { return ctx, nil, errs.Wrap(CategoryInvalidArgument, err) }   // cancelled ctx: nothing written; errors.Is(err, context.Canceled) holds
              if err := Sink.AppendCritical(ctx, body); err != nil { return ctx /*unchanged*/, nil, wrap(CategoryInternal, err) }   // caller MUST refuse the protected effect
  Diagnostic (default): if !Sink.AppendDiagnostic(body) { r.noteDrop() }; return nctx, op, nil                                    // never fails because of I/O
```
**Op.End algorithm:** `ended` is an `atomic.Bool` CAS; the loser returns `ErrAlreadyEnded` and writes nothing (anomaly counter `Stats.DuplicateEnd++`). The winner snapshots result map/artifacts/usage under the Op mutex (later `SetResult` is ignored), derives `error_code` (`errs.Error` category if present; `deadline_exceeded` for `context.DeadlineExceeded`; `canceled` for `context.Canceled`; `error` otherwise; `panic` for OUTCOME_PANIC) and `error_summary` = sanitized message truncated to 256 bytes (never the panic value; for panic only its dynamic type `%T`), sanitizes result, builds `OperationEnd`, then appends with `EndDurability` (default Diagnostic) using `ectx, cancel := context.WithTimeout(context.WithoutCancel(startCtx), EndTimeout)` (`EndTimeout`=5s; `startCtx` is the ctx passed to Start, held privately by the Op and never stored in a context) so a cancelled/expired caller context cannot prevent the END; the same derived ctx is used for every post-start Critical append (`Op.Observe` with Critical) — **OUTCOME_PANIC always uses Critical, best effort**. A failed critical END append returns the error; a dropped diagnostic END increments `Stats.EndDropped` and returns nil. The exported `End` rejects OUTCOME_PANIC (`CategoryInvalidArgument`): only `Run`'s recover path records it, via unexported `op.endPanic()`. If `outcome` is OUTCOME_UNSPECIFIED or `cause` contradicts `OUTCOME_COMPLETED` (non-nil cause with COMPLETED) -> `CategoryInvalidArgument`, nothing written, `ended` NOT consumed.

**Recorder `Stats` (uint64, atomic, exposed as `Status().Stats`):** `DuplicateEnd, EndDropped, EndFailed, ObserveAfterEnd, Dropped, ResultKeysDropped, ArtifactsDropped`.

### 8.3 `Run` and panic semantics (normative)
```go
func Run(ctx context.Context, rec Starter, spec StartSpec, fn func(context.Context, *Op) error) (err error) {
    ctx2, op, serr := rec.Start(ctx, spec)
    if serr != nil { return serr }                 // fn is NOT run
    returned := false
    defer func() {
        if returned { return }
        p := recover()                              // must be called directly by the deferred func
        if p == nil { return }                      // runtime.Goexit: no END; START stays unmatched => "unresolved"
        _ = op.endPanic()                          // truthful: we are unwinding a panic; failure is counted in Stats.EndFailed
        panic(p)                                    // re-panic the ORIGINAL value; never swallowed, never converted to success
    }()
    err = fn(ctx2, op)
    returned = true
    outcome := outcomeFor(err)                      // nil->Completed; errors.Is(err, context.Canceled)->Cancelled; else Failed
    if endErr := op.End(outcome, err); endErr != nil && !errors.Is(endErr, ErrAlreadyEnded) {
        err = errors.Join(err, fmt.Errorf("flightrec: record end of %q: %w", spec.Name, endErr))   // nil err + endErr => endErr; both => both preserved
    }
    return err
}
```
Re-panicking with `panic(p)` loses the original panic's stack trace (the new trace starts in the deferred func); this is accepted, the original value is preserved exactly, and `Run`'s doc comment states it. Rules: a named-return `defer End(err)` pattern is forbidden anywhere (GO_ENGINEERING_PRACTICES warning); a nil `error` is NEVER sufficient evidence of COMPLETED while unwinding. If `fn` called `op.End` itself, Run's End yields `ErrAlreadyEnded`, which is ignored. `errors.Is(result, ErrX)` on both the original and the END failure must work (`errors.Join`).

### 8.4 Sanitizer (`Sanitizer.JSON(v any) ([]byte, wire.Sanitization, error)` plus `Sanitizer.Scalar(kind, s) string`; applied to metadata, results, payloads, error summaries, health detail and every scalar string field BEFORE persistence)
**A. Scalar string fields.** `Subject.kind/id`, `Provenance.*`, `ArtifactRef.kind/locator/id`, `ActorID/TaskID/AttemptID/CanonicalEventID`, `ReasonCode`, `error_code`, `PathAttempt.source/path/error_code`, `Observation.name` MUST match `^[A-Za-z0-9._:/@=-]{0,128}$`; otherwise the value is replaced by `"[INVALID]"` and counted in `Sanitization.redactions`. Locators and paths (`ArtifactRef.locator`, `PathAttempt.path`, `Provenance.source_ref`) use the same charset with a **256-byte** cap instead of 128, additionally pass the value patterns (rule 3, incl. env-assignment) and are never opened, stat-ed or resolved by the recorder (paths containing spaces or other characters become `[INVALID]`; accepted limitation). `Name`/`Link.Relation` keep their stricter regexes (§8.2).
**B. JSON pipeline:** bound input (a `[]byte`/`string` value > 1 MiB is replaced up front), then `json.Marshal(v)` and the walk run inside `recover()` (a panicking `MarshalJSON` or walk -> payload replaced by `{"_unserializable":true}`, `payload_replaced=true`); marshalled bytes are capped at `MaxMarshalBytes` = 1 MiB, else the payload is replaced by `{"_truncated":true,"_original_bytes":N}`; decode with `UseNumber`, walk, re-encode (map keys sorted => deterministic). Defaults (`SanitizerConfig` may only ADD denylist entries/patterns or tighten bounds): `MaxStringBytes=256`, `MaxDepth=6`, `MaxEntries=64` per object/array, `MaxJSONBytes=16384`.
1. **Key denylist** (compare lowercased with `-_. ` removed; substring): `password, passwd, secret, token, apikey, authorization, credential, privatekey, cookie, bearer, sessionid` -> value `"[REDACTED]"`. **`token` allow-exceptions:** a normalized key ending in `tokens` (`input_tokens`, `output_tokens`, `max_tokens`, `total_tokens`, ...) or equal to `tokencount`, `tokenlimit`, `tokenusage` is NOT redacted for the `token` entry only; every other entry still applies (e.g. `secrettokens` is redacted via `secret`).
2. **Raw-content keys** (exact, normalized): `prompt, completion, source, contents, content, body, diff, stdout, stderr, env, environ, environment, configcontents, rawconfig` -> value `"[OMITTED:raw]"`. Raw content and environment values travel only as `ArtifactRef` (digest + locator). Phase 1 MUST NOT contain code that reads `os.Environ()`/`os.Getenv` values into a record (path resolution reads only the documented path variables and records the variable NAME).
3. **Value patterns** applied to every string and each whitespace-separated token: `protocol.LooksLikeSecret`, JWT (`eyJ[\w-]{10,}\.[\w-]{10,}\.[\w-]{5,}`), PEM (`-----BEGIN [A-Z ]*PRIVATE KEY-----`), URL userinfo (`://[^/\s:@]+:[^/\s@]+@`), `Authorization:` header text, **env-assignment `\b[A-Z][A-Z0-9_]{2,}=\S+`** -> replaced by `"[REDACTED]"` (token-level where possible). `sha256:<64 hex>` digests are exempt. Only counts are recorded, never hashes or lengths of the secret.
4. **Map keys** are sanitized by the same rules: denylist/raw rules use the original key; the emitted key must pass the scalar validator (<= 64 bytes) and value patterns, else it becomes `"[INVALID_KEY_<n>]"` (n = ordinal in sorted order, so collisions cannot merge entries).
5. **Bounds:** strings are truncated at a UTF-8 boundary with suffix `"...[truncated N bytes]"`; a string with a `'\n'` beyond its first line is replaced by `"[OMITTED:multiline <bucket>]"` where bucket is the smallest of `256b, 1k, 4k, 16k, over16k` holding its length (counted in `truncations`); arrays/objects over `MaxEntries` keep the first N (sorted keys) plus `"_truncated": <dropped>`; deeper than `MaxDepth` -> `"[DEPTH_LIMIT]"`; if the final JSON still exceeds `MaxJSONBytes` -> `{"_truncated":true,"_original_bytes":N}` and `payload_replaced=true`. `nil` input -> empty `bytes`.
6. Proto `Any` payloads are NOT accepted by the Phase-1 Recorder API (cannot be sanitized); the schema field is reserved for Phase 2/3.

### 8.5 Storage path resolution and bootstrap
```go
type ResolveInput struct { ExplicitDir string /*flag*/; Getenv func(string) string; UserHomeDir func() (string, error); TempDir func() string; UID func() int; FS PathFS /* MkdirAll, Lstat, Chmod, Stat owner */ }
type Resolution struct { Root, Source string; Attempts []Attempt; Fallback bool }   // Attempt{Source, Path, ErrCode}
func Resolve(in ResolveInput) (Resolution, error)  // error only if every candidate fails
```
Candidate order (first usable wins; each failed candidate adds an `Attempt` with a stable `ErrCode`: `not_absolute, mkdir_failed, symlink, not_owner, not_dir, perm_fixed_failed, unset, uid_unknown`):
1. `explicit_flag` `ExplicitDir`; 2. `env_trace_dir` `DEVCADENCE_TRACE_DIR`; 3. `env_home` `$DEVCADENCE_HOME/traces`; 4. `xdg_state` `$XDG_STATE_HOME/devcadence/traces` (absolute only); 5. `user_home` `<UserHomeDir>/.devcadence/traces`; 6. `temp` `<TempDir>/devcadence-trace-<uid>`.
`Fallback = (winner is not the first candidate that was *set*)` (i.e. some set candidate failed); sources 4-6 count as fallback only when a higher candidate failed. A candidate is usable when `MkdirAll(0700)` succeeds, `Lstat` shows a real directory (not a symlink) owned by uid (unix), and mode is made 0700 (`Chmod`; failure -> `perm_fixed_failed`). The temp candidate gets the same checks, closing the shared-/tmp pre-creation attack. Residual risk (documented, accepted): checks are Lstat-then-use (TOCTOU) in a shared directory, mitigated by 0700 + owner check + `O_EXCL` files + flock; if `UID()` returns a negative value (unknown), the temp candidate is SKIPPED with `ErrCode=uid_unknown`. Parity test (external test package) asserts steps 3/5 agree with `setup.ResolveHome()+"/traces"`.

```go
type BootstrapInput struct { Resolve ResolveInput; Limits journal.Limits; Clock clock.Clock; IDs ids.Source; Now func() time.Time /*for Mono origin*/; WriterVersion string }
func Bootstrap(ctx context.Context, in BootstrapInput) (*Recorder, Status)  // NEVER returns an error and NEVER panics
```
Bootstrap preconditions (earliest-startup, per owner's first-run addendum): it MUST NOT load DevCadence config, probe hardware/models, touch SQLite, spawn processes, or use the network; its only inputs are the flag value, process environment path variables, OS home/temp, and the clock. Steps: `Resolve` -> ensure/read `node-id` (else ephemeral, health `NODE_ID_EPHEMERAL`) -> new `runtime_id` -> `journal.Open` -> first record `JournalHealth STREAM_STARTED` (critical) carrying `selected_source`, all `Attempts` (source, path, error_code) and `PATH_FALLBACK` health when `Fallback`. Any failure (all candidates fail, lock unsupported, open error) yields `NewNoop(Status{Mode: ModeDegradedNoop, Reason: code, Attempts})` and a `DEGRADED_NOOP` entry in the in-memory health ring. The effective location is fixed for the process lifetime; later config can only affect the next run (E4). `Status{Mode (ModeJournal|ModeDegradedNoop), Root, Source, Fallback, Reason, Attempts, NodeID, RuntimeID, NodeIDEphemeral, Stats}` is the structure the Phase-2 exporter and `doctor` embed so a degraded recorder is itself visible in exports (live status JSON; on-disk journal exists only in ModeJournal).

**Bootstrap hardening (MUST):** the whole body runs under `defer func(){ if p := recover(); p != nil { ... } }()` returning `NewNoop(Status{Mode: ModeDegradedNoop, Reason: "bootstrap_panic"})` plus a `DEGRADED_NOOP` ring entry (the panic value is never recorded). Nil defaults: `Clock` -> `clock.System()`, `IDs` -> `ids.NewULIDSource()`, `Now` -> `time.Now`. The Mono origin is taken once, `origin := Now()`, and `Mono = func() int64 { return int64(Now().Sub(origin)) }` (identical to `time.Since(origin)` for the default `Now`, monotonic-clock based); `SegmentHeader.mono_origin_wall_unix_nanos = origin.UnixNano()`.

## 9. Failure / missing-input semantics

| Situation | Required behavior |
| --- | --- |
| Critical Start/Observe append fails | Return error (`CategoryInternal`, wrapped cause); ctx unchanged; no op; caller MUST NOT perform the protected effect. Writer poisoned+rotates on next call. |
| Diagnostic append, queue full / writer broken / noop | Drop, `Dropped++`, health event; Start/Observe/End return nil. Never blocks, never panics. |
| END critical append fails | `End` returns the error; `Run` joins it with the operation error. |
| Panic in `fn` | END(OUTCOME_PANIC) Critical best effort, original panic re-raised; END failure only counted. |
| `runtime.Goexit` in `fn` | No END; START unmatched = unresolved/unknown, not failed, not safe to retry. |
| Second `End` | `ErrAlreadyEnded`, no record, `DuplicateEnd++`. |
| `Observe` after END or no current op | `CategoryInvalidArgument`, no record (ended op: counted `ObserveAfterEnd`). |
| Record > MaxRecordBytes | `ErrRecordTooLarge`; not poisoning; sanitizer caps make this unreachable in normal use. |
| Second writer on same stream dir | `CategoryConflict` at `Open`. |
| `flock` unsupported (non-unix) | `ErrLockUnsupported`; Bootstrap -> degraded no-op. |
| Existing run dir with damage at reopen | Never modified; new segment; `RECOVERY_REOPEN` health records finding codes+offset. |
| Missing home / env unset / unwritable dir | Fallback chain §8.5, attempts recorded; all fail -> degraded no-op + health ring. |
| Remote SpanContext with invalid id | `WithRemote` -> `CategoryInvalidArgument`; local Start unaffected (new trace). |
| Unsanitizable/unserializable payload | Replaced by `{"_unserializable":true}`, `payload_replaced=true`; never fails Start/End. |
| Clock/ID source returns invalid id (`!ids.Valid`) | `Start` -> `CategoryInternal`; never persists malformed ids. |
| Unknown protobuf fields / unknown record type on read | Preserved / delivered as UNKNOWN_RECORD_TYPE info; never fatal. |
| Canonical transaction committed, diagnostic lost | Out of scope here; not detectable in Phase 1 (ADR D1 gap reporting is Phase 2/3, via `canonical_event_id`). |
| Caller ctx cancelled/expired at `End` or a post-start Critical append | Those appends use `WithTimeout(WithoutCancel(startCtx), 5s)`: END is still persisted (B-17). `AppendCritical`/`Flush` consult ctx only before taking `w.mu`; a started write/fsync is never aborted. |
| Critical Start with `ctx.Err() != nil` | `CategoryInvalidArgument` wrapping the ctx error before any write; nothing persisted; no op. |
| `Sync` fails after a complete write | `AppendCritical` error, writer poisoned; frame may be on disk unsynced: START without END, consistent (caller refused the effect). |
| `Write` fails (possibly leaving a complete frame) | Poison; sequence not consumed; next segment reuses it; scanner `SEGMENT_ORDER` (warning) at the next segment boundary is expected. |
| Segment creation fails (open/header/sync/dir-sync) | `segmentIndex` consumed; leftover file never removed (scanner `BAD_HEADER`); writer broken; next append retries with index+1. |
| Health record would exceed `MaxHealthBytes` | Deterministically capped (§5); never `ErrRecordTooLarge`. |
| `Close` racing `AppendDiagnostic` | No panic (queue never closed); late diagnostics refused (`false`, `Dropped++`); accepted ones drained by `Close`. |
| Sanitizer panic / marshalled > 1 MiB / scalar fails validator | `{"_unserializable":true}` / `{"_truncated":true,...}` / `"[INVALID]"`, counted; never fails Start/End. |
| Bootstrap panics | Degraded no-op, `Reason="bootstrap_panic"`. |

## 10. Acceptance scenarios mapped to tests (deterministic: `clock.NewFake`, `ids.NewSequential`, `t.TempDir`, no sleeps for correctness)

Helper-process tests (A-9 re-exec, A-14 SIGKILL) use `TestMain` and run helper mode ONLY when env `DEVCADENCE_FLIGHTREC_HELPER=<mode>` is set (documented in the test file); in the normal suite the variable is unset, only the parent side runs and no helper path executes `os.Exit`.

**WU-A**
- A-1 `wire` round-trip golden: committed hex vectors under `wire/testdata/` for one record of each type; Marshal is byte-stable; Unmarshal(Marshal(x)) == x.
- A-2 Unknown fields (extra numbers incl. nested messages and an unknown body number) survive Unmarshal->Marshal byte-for-byte; unknown body => nil members, no error.
- A-3 Cross-codec: test-only `descriptorpb`+`dynamicpb` descriptor of the schema decodes our bytes and vice versa for every message and field type.
- A-4 `.proto` text parity: regexp parse of `journal.proto` equals the descriptor table (names, numbers, types, enum values); fails if either changes alone.
- A-5 Malformed input: truncated varint/length, invalid UTF-8, overlong depth -> typed errors; `Validate` cases incl. type/body mismatch.
- A-6 Framing: header and frame byte layout asserted offset-by-offset against §5; CRC covers length (flip a length bit -> BAD_*).
- A-7 Writer happy path: N critical + M diagnostic -> `ScanStream` clean, sequences 1..N+M+health, event ids unique, envelope filled by writer.
- A-8 Rotation: `MaxRecordBytes=4096, MaxSegmentBytes=24576` -> multiple segments, `first_sequence` continuity, `SEGMENT_ROTATED` health first in new segment, no segment over limit.
- A-9 Single writer: second `Open` same dir (same process) -> `CategoryConflict`; helper process (env-guarded re-exec of the test binary) also refused; lock released by `Close` and by process exit.
- A-10 Durability via fault FS (`FS.OpenFile`->`File.Write/Sync`, `FS.SyncDir`, `FS.MkdirAll`): critical returns nil only after Sync (order Write->Sync); Sync failure -> error + poison + next append opens a new segment; short Write -> poison, sequence not consumed; header/dir-sync failures; diagnostic path performs zero Sync calls until Flush/tick/rotate/Close.
- A-11 Drop accounting: block drain (fake FS write gate), overfill queue -> `AppendDiagnostic` false, `Dropped` exact, `DROPPED_DIAGNOSTICS` with exact count after drain; critical append completes while queue is full and drain is gated between batches (bounded wait).
- A-12 Recovery matrix (crafted bytes via `ScanSegment`; stream cases via `ScanStream` over `FS.ReadDir/Open`): clean; torn header; torn payload tail (`TORN_TAIL`, EOF reached); length beyond EOF with a later valid frame (`BAD_LENGTH`, not torn); zero-filled tail; flipped payload byte mid-segment (later records delivered, `BAD_CRC` skipped bytes exact); bad sync; oversize length; corrupt length with valid following frame; sequence gap; duplicate/regression; boundary `SEQ_GAP` vs `SEGMENT_ORDER` vs `SEGMENT_MISSING` precedence; missing segment index; bad segment header; resync bound exceeded; unknown record type (info, `Clean` stays true); per-code severity and `Clean` truth table; last-segment tail with `AssumeLiveTail` and active/inactive writer.
- A-13 Reopen (real OS FS + `FS.ReadDir/Open`): write, truncate mid-frame, `Open` same dir -> original bytes untouched (digest compare), new segment, `RECOVERY_REOPEN`, sequence continues, torn tail of old segment reported as warning.
- A-14 Crash test: env-guarded helper process appends one critical START then SIGKILLs itself without defers; parent scans the files and finds the record.
- A-15 Segment budget: `MaxRecordBytes=4096, MaxSegmentBytes=20548` (exact minimum) with all three pending health items (SEGMENT_ROTATED, WRITER_ERROR via a prior Sync fault, DROPPED via a full queue) plus a maximal record -> segment <= `MaxSegmentBytes`; limits one byte below the constraint -> `CategoryInvalidArgument`; health cap (detail 4 KiB, 20 attempts) truncates deterministically to <= 4096 bytes.
- A-16 Rotation sequencing: the record that triggers rotation gets its sequence AFTER the new segment's health frames (re-fill), no duplicate/gap; the fault FS shows health frames never trigger a rotation or recursive append.
- A-17 Segment-creation failure: `FS.OpenFile`/header `Write`/`Sync` fails for segment k -> leftover file untouched, scanner `BAD_HEADER`, next attempt uses k+1 (no `O_EXCL` collision), sequences continue after recovery.
- A-18 Close/concurrency (`-race`): goroutines calling `AppendDiagnostic` race `Close` -> no panic, every accepted record is on disk after `Close` returns; `AppendCritical` with an already-cancelled ctx writes nothing; ctx cancelled while a write is gated -> the write still completes; `Close` with expired ctx returns ctx error and the lock is released.
- A-19 Resync candidate checks: a valid frame embedded in a damaged record's payload is rejected when its stream id is foreign or `seq < lastSeq`, and accepted (with `SEQ_GAP`) when ids and sequence fit (documented limitation).
- A-20 Directory safety: symlinked `Dir` rejected (`FS.Lstat`); wrong owner via injected `ownerUID`; `SyncDir` observed for every newly created ancestor up to Root and not above; `LOCK` pre-created as a directory -> `Open` error (real FS).

**WU-B**
- B-1 Concurrent children: 32 goroutines `Start` under one parent ctx -> same `trace_id`, distinct `operation_id`, `parent_operation_id` = parent, under `-race`; per-stream sequence gapless.
- B-2 Context hygiene: ctx contains only the `SpanContext` (probe with the private key; Recorder/Op/Sink unreachable); parent cancellation/deadline honored by the derived ctx.
- B-3 Fake sink: Recorder works with an in-memory `Sink`; deterministic ids/times asserted exactly.
- B-4 `Run` success -> START+END(COMPLETED); error -> END(FAILED, `error_code`); `context.Canceled` -> CANCELLED; `DeadlineExceeded` -> FAILED `deadline_exceeded`.
- B-5 `Run` panic: custom value and nil-`error` named-return pattern -> original panic value re-raised, END is OUTCOME_PANIC (never COMPLETED), `error_summary` only the type name. Panic + failing critical END -> panic still re-raised, `EndFailed==1`. Exported `End(OutcomePanic, ...)` -> `CategoryInvalidArgument`.
- B-6 `Run` error + failing END: `errors.Is` for both. Success + failing END: returns the END error.
- B-7 `Goexit` inside `fn` (goroutine): no END record, START present.
- B-8 `End` exactly-once under 16 racing goroutines: one END on disk, others `ErrAlreadyEnded`, `DuplicateEnd` counted; `SetResult` concurrent with End is race-clean, snapshot has no later key.
- B-9 Critical Start failure (fault sink): error returned, ctx unchanged, nil op, protected-effect stub not executed; diagnostic start with failing sink returns nil.
- B-10 Sanitizer table: each denylist key, raw-content key, value pattern, URL userinfo, JWT, PEM, env-assignment (`API_KEY=abc`), digest exemption, `*_tokens` allow-exceptions (`input_tokens`, `max_tokens` kept; `secret_tokens`, `auth_token` redacted), scalar validator (`[INVALID]`, locator 256 cap), multiline omission and bucket, map-key sanitization, truncation at UTF-8 boundary, depth/entries/total bound, panicking `MarshalJSON`, >1 MiB marshalled, unserializable value, determinism, counts. Fixed corpus of secrets scanned in the on-disk journal bytes MUST NOT appear.
- B-11 `Op.AddArtifact`/`ArtifactRef` conversion: digest format enforced; `digest_verified` false unless caller sets it.
- B-12 Path resolution table with fake env/home/tmp/FS: every step, every `ErrCode` (incl. `uid_unknown` skipping temp), symlink and wrong-owner candidates, relative paths, `Fallback`/`Attempts`; parity with `setup.ResolveHome`.
- B-13 Bootstrap: no config, no `HOME`, empty env, read-only home -> journal at temp fallback with `STREAM_STARTED` + `PATH_FALLBACK` recording attempts; node-id persisted/reused across two Bootstraps; ephemeral node id when node-id unwritable.
- B-14 All candidates fail -> degraded no-op: `Start/End/Observe` inert, `Status().Mode==ModeDegradedNoop`, `Health()` has `DEGRADED_NOOP`; Critical Start on a no-op recorder -> `CategoryInternal` ("recorder degraded").
- B-15 `Observe` carries reason_code/subject/provenance/evidence/links/artifacts round trip through disk; `Any` rejected.
- B-16 `Close`: drains, idempotent, appends after Close drop/refuse per class; lock released (reopen succeeds).
- B-17 Cancelled ctx: Start (Critical) under a live ctx, cancel it, then `End` -> END persisted on disk; Critical Start with an already-cancelled ctx -> error before any write, no records.
- B-18 Bootstrap defensive: nil `Clock`/`IDs`/`Now` take the defaults; an injected panicking `PathFS` -> degraded no-op with `Reason="bootstrap_panic"`, no panic escapes; `Mono` is monotonic non-decreasing from the origin.

**WU-B repair addendum (PR #100 review, owner first-run requirement).** (a) Sanitizer: a map key that is invalid or replaced default-denies its value; Unicode `Cf` runes are stripped from normalized keys; `*tokens` keys keep a value only when the name is an exact counter and the value is a JSON number; new free-text rules (Bearer/Basic, keyword pairs, userinfo, known prefixes anywhere, secret flags) and deny keys (`pwd`, `auth`, `accesskey`, `sshkey`, `signature`, `session`, `jwt`); `[]byte` values are omitted; depth overflow marks the payload replaced. (b) Bootstrap writes a last-resort `bootstrap-failure.json` sidecar (O_EXCL, 0600, at most 64 KiB, sanitized fields only; directory order resolved root, `<home>/.devcadence`, `<tmp>/devcadence-trace-<uid>`, `<tmp>`), exposed as `Status.SidecarPath` / `Status.SidecarError` and documented in OBSERVABILITY.md so an early startup failure is exportable offline; `node-id` reads are Lstat-gated and size-bounded. Tests: `sanitize_adversarial_test.go`, `bootstrap_sidecar_test.go`.

## 11. MUST / MUST NOT

MUST: (1) keep recorder, writer, Op and journal handles out of `context.Context` (GO_PRACTICES 5); (2) inject Clock, IDs, Mono, Sink, FS; no package-level mutable state, no `init()` side effects, no `time.Now()` or `rand` outside the injected defaults in `Bootstrap`'s composition; (3) enforce single writer per stream via flock; (4) write exactly the §5 bytes; (5) treat sequence as writer-assigned; (6) never truncate/rewrite journal files; (7) sanitize before persistence; (8) bound every input (sizes, counts, depth, queue); (9) return `errs` categories for caller-actionable errors; (10) pass `go vet`, `gofmt`, `-race`; (11) whole-module statement coverage % MUST NOT regress (guard tolerance 0.00 pp); target every reachable statement in the new packages and list each uncovered statement in the PR with justification (e.g. `lock_other.go`/`perm_other.go` are not compiled on linux); no `//nolint`-style exclusions, no skipped tests; (12) document exported APIs and the durability classes in package docs, including that `flock` is advisory and unreliable on NFS/some network filesystems (single-writer enforcement is best-effort there); (13) helper-process tests run helper mode only under the `DEVCADENCE_FLIGHTREC_HELPER` env guard (§10).
MUST NOT: (1) write to `internal/events`, `internal/storage` or SQLite, or reference canonical transitions; (2) instrument any existing subsystem (Phases 2/3); (3) log or persist environment values, config file contents, raw prompts, source or tokens; (4) add globals, service locators or a tracer in context; (5) add dependencies other than `google.golang.org/protobuf` (and its own `go.sum` closure); (6) edit `INVARIANTS.md`, `internal/cognition/compiler/*`, `.githooks/*`, `scripts/health/*`, CI workflow, ADRs; (7) claim exactly-once or cross-store atomicity; (8) treat START-without-END as failed or as safe to retry in any doc string; (9) swallow panics or convert them to returned errors; (10) delete or rotate-away data (no retention); (11) use `--no-verify` or weaken gates.
Local discretion (allowed): private helper names/layout, test helpers/decomposition, file splitting inside the declared packages, internal buffering structures, unexported fault-seam shapes provided the exported API in §6/§8 is unchanged.

## 12. File list and scope

Write (WU-A): `go.mod`, `go.sum`; `internal/flightrec/wire/{doc.go,types.go,codec.go,journal.proto,*_test.go,testdata/*}`; `internal/flightrec/journal/{doc.go,frame.go,writer.go,scan.go,fs.go,lock_unix.go,lock_other.go,perm_unix.go,perm_other.go,*_test.go}`.
Write (WU-B): `internal/flightrec/{doc.go,context.go,recorder.go,op.go,run.go,sanitize.go,paths.go,bootstrap.go,status.go,*_test.go,internal helper test binary files}`; docs: add a short "Flight recorder (Phase 1, implemented)" subsection to `docs/OBSERVABILITY.md` (layout, durability classes, degraded mode, sanitizer defaults, non-authority statement), update `docs/IMPLEMENTATION_PLAN.md` status line for WP-TRACE-1 if such a row exists; ADR-0026 status stays Proposed unless the owner accepts it (E0). Keep every added markdown link/anchor valid (`make docs-check`).
Read-only reference: `internal/clock`, `internal/ids`, `internal/errs`, `internal/protocol/{credentials.go,protocol.go}` (only the named symbols), `internal/execrt/lock.go`, `internal/setup/{home.go,lock_unix.go}`, `scripts/health/coverage-guard.sh`.
Out of scope for writes: everything else. New undeclared paths or domains require context re-resolution and an amended EWP (AGENTS.md §2, §7). No file under `schemas/` (the `.proto` lives under `internal/flightrec/wire/` so `schema_fixtures_test` and `schemas.go` are unaffected).

## 13. Validation (WU-A starts only after E0/E2 are settled, §14; record exact commands, exit status, versions, base/head SHAs, covered/total statements before and after)

```
make hooks-install && make hooks-check
git rev-parse HEAD; go version
go mod download && make mod-check            # no diff after adding google.golang.org/protobuf
make fmt-check diff-check vet
go test -count=1 ./internal/flightrec/...
go test -race -count=1 ./internal/flightrec/...
go test -count=1 -coverprofile=cover.out ./internal/flightrec/... && go tool cover -func=cover.out   # list uncovered statements (justify in PR); whole-module % must not regress
make test schemas docs-check
make coverage                                 # compare whole-module % with base (sh scripts/health/coverage-guard.sh <base> <tree> via precommit)
make race
```
`make update-goldens` is not run (no normative source changed). If the sandbox cannot reach the module proxy to add the dependency, STOP and escalate (E2); do not vendor by hand or hand-edit `go.sum`.

## 14. Escalation triggers, open items, out of scope

Escalate (return `CONTRADICTED_ASSUMPTION` with evidence) if: the ADR/GO_PRACTICES text conflicts with this contract in a way not listed below; `google.golang.org/protobuf` cannot be added with a clean `mod-check`; coverage-guard shows a whole-module regression you cannot cure with tests; any representability gap (a field in §4 cannot be expressed by the codec; a durability rule cannot be met by the FS seam); a need to touch an out-of-scope path, `INVARIANTS.md`, or the catalog; `flock` semantics differ on the target (e.g. network filesystems); any proposal to relax sanitizer defaults.

Decisions and open items (Principal decisions recorded; none blocks authoring):
- **E0 (gates start):** ADR-0026 and GO_ENGINEERING_PRACTICES live on `origin/docs/flight-recorder-adr-go-practices` (PR #94). E0 resolves when PR #94 merges to main; implementation starts after that (rebase base identity then).
- **E1 ACCEPTED:** `OUTCOME_PANIC` END is recorded from `Run` only (exported `End` rejects it), Critical best effort, then the original value is re-panicked (original stack is lost; accepted). `Goexit` leaves START unmatched.
- **E2 ACCEPTED:** add `google.golang.org/protobuf`, hand-written `protowire` codec, no generated code: checked-in generated code would regress the zero-tolerance whole-module coverage guard. Future switch to generated code is wire-compatible and needs a new EWP plus a CI codegen-drift step.
- **E3 ACCEPTED as documented limitation:** no retention in the MVP; disk use is unbounded per run. Retention or a hard per-run byte cap is required before Phase 3 broad instrumentation.
- **E4 ACCEPTED:** trace location is fixed at Bootstrap (before config); config affects only the next run.
- **E5 ACCEPTED:** "run id" is `runtime_id`; do not reuse `agent_run_id` (docs/OBSERVABILITY.md).
- **E6 ACCEPTED:** ADR D5 "important outcome" is expressed by callers choosing Critical `EndDurability`; Phase 1 has no third persisted class.

Out of scope (Phases 2/3 or later): offline exporter and `devcadence trace export`; START/END joining and orphan/duplicate analysis; artifact existence/digest verification; segment shipping/merging; instrumentation of MCP, task execution, Ollama, tools, validation, review, setup/routing; canonical-event linkage logic and mismatch reporting; `Any` payloads and descriptor sets; retention; Windows support; SQLite trace index; dashboards; CLI wiring.

## Appendix A. Forward-compat note (already in the §4 schema; Phases 2/3 must not need breaking changes)

- **Exporter (Phase 2):** `event_id` (dedup, reviewer retrieval), `node_id/runtime_id/stream_id/stream_sequence` (gap/orphan detection, `--run` selection), `RecordType` and `Durability` (confidence per record), `Stamp` wall+mono (clock-skew-tolerant ordering within a run; mono origin in `SegmentHeader`), `Sanitization` counters (redaction reporting), `ArtifactRef.digest/digest_verified/truncated` (must not imply presence or verification), `Link{relation,trace_id,operation_id,event_id}` (causal DAG, cross-trace, handoff), `OperationStart.canonical_event_id/task_id/attempt_id/actor_id` (canonical mismatch reporting), `JournalHealth` kinds incl. `RECOVERY_REOPEN`, `DROPPED_DIAGNOSTICS`, `DEGRADED_NOOP`, `PATH_FALLBACK` (completeness and trust downgrade), `Any` + `type_url` (`descriptor_missing` handling), `Unknown` preservation (newer-writer segments).
- **Setup instrumentation (Phase 3: environment discovery, capability assessment, config resolution, setup actions, readiness verification):** `Observation.kind=DECISION` + `reason_code` (stable machine reason), `subject{kind,id}` (e.g. `config_key`/`models.default`, `capability`/`gpu`, `readiness_check`/`ollama_reachable`), `provenance{source_kind,source_ref,observed_by,method,observed_at}` (where a config value or discovery fact came from, e.g. flag/env-var-name/file-path, never contents), `evidence` (`OBSERVED/DOCUMENTED/INFERRED/CONFIRMED/MISSING/UNKNOWN`: missing/unknown evidence is first-class, never absent), `artifacts` (digested probe output, setup plan, detected-hardware snapshot), `links` with relations `caused_by, follows_from, retry_of, supersedes, evidence_for, derived_from`, `json_payload` for structured attributes plus `proto_payload` for typed future payloads, `OperationEnd.usage` and `outcome` (action result), `operation_name` namespaces such as `setup.discover.environment`, `setup.assess.capability`, `setup.resolve.config`, `setup.action.apply`, `setup.verify.readiness`.
- **Reserved evolution rules:** field numbers never reused; new enums only append values; new record types use new `body` numbers >= 14; `schema_version` bumps only for incompatible semantics; `format_version` bumps only for framing changes; readers MUST tolerate unknown enum numbers, fields and record types.

## Known limitations / follow-ups (tracked, not part of WU-A)

- N5: the scanner bounds frames by `ScanOptions.Limits` rather than the segment header's `max_record_bytes`.
- N9: `prepareDir` does not check intermediate path components; `WriterActive` takes a brief flock that can cause a spurious `Conflict` for a concurrently opening writer; `ScanStream` `AssumeLiveTail` is unsupported on non-unix builds; unix build tags for the flock platforms need tightening.
- N10: duplicate singular message fields in the codec replace rather than merge (protobuf merges); the watchdog uses `time.After`.

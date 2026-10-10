// Package flightrec is the diagnostic flight recorder (WP-TRACE-1, ADR-0026).
//
// It records operation START/END, Observation and JournalHealth records into a
// crash-evident, append-only, single-writer journal (package journal, wire
// format in package wire). It is DIAGNOSTIC EVIDENCE ONLY: it never mutates
// canonical state, never references canonical transitions and is never an
// authority for task completion.
//
// # Identity
//
// Only an immutable SpanContext (ids, never services or handles) travels in a
// context.Context. The Recorder, Op, Sink and writer are injected explicitly.
//
// # Durability classes
//
// Critical appends (StartSpec.Durability, StartSpec.EndDurability,
// ObservationSpec.Durability) return nil only after the complete frame was
// written and fsynced; on error the caller MUST refuse the protected effect.
// Diagnostic appends (the default) are queued, never block and never fail the
// caller; a drop is counted in Stats.Dropped and reported as health. A record
// that is queued or written but not followed by a later successful Critical
// append, flush or Close can be lost on a crash without trace. END records and
// post-start Critical appends use
// context.WithTimeout(context.WithoutCancel(startCtx), EndTimeout) so a
// cancelled caller context cannot prevent them.
//
// A START without a matching END means unresolved, not failed and not safe to
// retry: it is what a crash, a runtime.Goexit or a lost diagnostic END leave.
// Exactly-once and cross-store atomicity are NOT claimed.
//
// # Degraded mode
//
// Bootstrap never fails and never panics. When no storage location is usable
// (or the journal cannot be opened) it returns a no-op recorder whose Status
// (Mode == ModeDegradedNoop, Reason, Attempts) and bounded in-memory Health
// ring remain available so a degraded recorder is itself visible.
//
// # Storage and platform notes
//
// The trace root is resolved once at Bootstrap (flag, DEVCADENCE_TRACE_DIR,
// $DEVCADENCE_HOME/traces, $XDG_STATE_HOME/devcadence/traces,
// ~/.devcadence/traces, $TMP/devcadence-trace-<uid>); see Resolve. Single-writer
// enforcement uses flock, which is advisory and unreliable on NFS and some
// network filesystems, where it is best effort. There is no retention in
// Phase 1: disk use is unbounded per run.
//
// # Sanitization
//
// Every payload, scalar string, error summary and health detail is sanitized
// before persistence (see Sanitizer); raw content and environment values travel
// only as ArtifactRef (digest plus locator).
package flightrec

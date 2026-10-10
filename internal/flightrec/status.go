package flightrec

import (
	"slices"
	"sync/atomic"

	"github.com/olostan/DevCadence/internal/flightrec/wire"
)

// Mode says whether a recorder writes a journal.
type Mode string

const (
	// ModeJournal records into an on-disk journal.
	ModeJournal Mode = "journal"
	// ModeDegradedNoop records nothing (no usable storage); Status and Health
	// remain available.
	ModeDegradedNoop Mode = "degraded_noop"
)

// Stable Reason codes of a degraded recorder.
const (
	ReasonNoUsablePath       = "no_usable_path"
	ReasonInvalidID          = "invalid_id"
	ReasonJournalOpenFailed  = "journal_open_failed"
	ReasonStreamStartFailure = "stream_started_failed"
	ReasonBootstrapPanic     = "bootstrap_panic"
)

// Attempt is one storage-path candidate tried by Resolve. Path is empty for an
// unset candidate. ErrCode is a stable code (see Resolve).
type Attempt struct {
	Source, Path, ErrCode string
}

// Stats are the recorder anomaly counters (atomic snapshot).
type Stats struct {
	// DuplicateEnd counts second End calls on an Op.
	DuplicateEnd uint64 `json:"duplicate_end"`
	// EndDropped counts diagnostic END records that were dropped.
	EndDropped uint64 `json:"end_dropped"`
	// EndFailed counts critical END appends that failed.
	EndFailed uint64 `json:"end_failed"`
	// ObserveAfterEnd counts Op.Observe calls after END.
	ObserveAfterEnd uint64 `json:"observe_after_end"`
	// Dropped counts diagnostic records dropped (queue full, closed, no-op).
	Dropped uint64 `json:"dropped"`
	// ResultKeysDropped counts SetResult keys beyond the cap.
	ResultKeysDropped uint64 `json:"result_keys_dropped"`
	// ArtifactsDropped counts artifacts dropped by Op.AddArtifact.
	ArtifactsDropped uint64 `json:"artifacts_dropped"`
}

// Status describes the recorder for the Phase-2 exporter and doctor: a
// degraded recorder is itself visible. The on-disk journal exists only in
// ModeJournal. Paths here are live in-memory values; they are sanitized when
// written into a journal record.
type Status struct {
	Mode            Mode
	Root, Source    string
	Fallback        bool
	Reason          string
	Attempts        []Attempt
	NodeID          string
	RuntimeID       string
	NodeIDEphemeral bool
	// SidecarPath is the bootstrap-failure sidecar written by a degraded
	// Bootstrap (empty otherwise); SidecarError is a stable code when no
	// directory was writable.
	SidecarPath, SidecarError string
	Stats                     Stats
}

// HealthEvent is one entry of the bounded in-memory health ring.
type HealthEvent struct {
	// Seq is the 1-based ordinal of the event in this recorder's lifetime.
	Seq      uint64
	Kind     wire.HealthKind
	Code     string
	Detail   string
	Attempts []Attempt
}

// healthRingSize bounds the in-memory health ring.
const healthRingSize = 256

type counters struct {
	duplicateEnd, endDropped, endFailed, observeAfterEnd, dropped, resultKeys, artifacts atomic.Uint64
}

func (c *counters) snapshot() Stats {
	return Stats{
		DuplicateEnd: c.duplicateEnd.Load(), EndDropped: c.endDropped.Load(), EndFailed: c.endFailed.Load(),
		ObserveAfterEnd: c.observeAfterEnd.Load(), Dropped: c.dropped.Load(),
		ResultKeysDropped: c.resultKeys.Load(), ArtifactsDropped: c.artifacts.Load(),
	}
}

// Status returns a snapshot of the recorder status including Stats.
func (r *Recorder) Status() Status {
	st := r.status
	st.Attempts = slices.Clone(st.Attempts)
	st.Stats = r.stats.snapshot()
	return st
}

// Health returns the bounded in-memory health ring, oldest first. It is
// available even when there is no Sink.
func (r *Recorder) Health() []HealthEvent {
	r.hmu.Lock()
	defer r.hmu.Unlock()
	out := make([]HealthEvent, 0, len(r.ring))
	for _, ev := range append(slices.Clone(r.ring[r.hnext:]), r.ring[:r.hnext]...) {
		ev.Attempts = slices.Clone(ev.Attempts)
		out = append(out, ev)
	}
	return out
}

func (r *Recorder) noteHealth(kind wire.HealthKind, code, detail string, attempts []Attempt) {
	var t tally
	ev := HealthEvent{Kind: kind, Code: code, Detail: r.san.text(&t, detail), Attempts: slices.Clone(attempts)}
	r.hmu.Lock()
	defer r.hmu.Unlock()
	r.hseq++
	ev.Seq = r.hseq
	if len(r.ring) < healthRingSize {
		r.ring = append(r.ring, ev)
		return
	}
	r.ring[r.hnext] = ev
	r.hnext = (r.hnext + 1) % healthRingSize
}

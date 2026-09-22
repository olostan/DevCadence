package state_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/state"
	"github.com/olostan/DevCadence/internal/testsupport"
)

// TestReductionIsAPureFunctionOfTheJournal is the concrete form of DCI-053.
// Reducing the same stream twice must produce byte-identical ProjectState,
// including generated_at and the revision identity — otherwise "rebuild the
// state" would produce something merely similar to what was lost.
func TestReductionIsAPureFunctionOfTheJournal(t *testing.T) {
	stream := testsupport.HappyPathScenario(t, "example").Stream()

	first, err := state.Reduce(stream)
	if err != nil {
		t.Fatalf("first reduce: %v", err)
	}
	second, err := state.Reduce(stream)
	if err != nil {
		t.Fatalf("second reduce: %v", err)
	}
	firstState, err := first.ProjectState()
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	secondState, err := second.ProjectState()
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	firstJSON, err := protocol.CanonicalJSON(firstState)
	if err != nil {
		t.Fatalf("canonicalise: %v", err)
	}
	secondJSON, err := protocol.CanonicalJSON(secondState)
	if err != nil {
		t.Fatalf("canonicalise: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("two reductions of one journal differ:\n%s\n%s", firstJSON, secondJSON)
	}
}

// TestIncrementalApplicationMatchesWholeStreamReduction shows that folding
// events one at a time is the same as reducing them together, which is what
// makes an incremental projection update safe to introduce later.
func TestIncrementalApplicationMatchesWholeStreamReduction(t *testing.T) {
	stream := testsupport.HappyPathScenario(t, "example").Stream()

	incremental := state.New()
	for i := range stream {
		if err := incremental.Apply(&stream[i]); err != nil {
			t.Fatalf("apply event %d: %v", stream[i].Seq, err)
		}
	}
	whole, err := state.Reduce(stream)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	incrementalState, err := incremental.ProjectState()
	if err != nil {
		t.Fatalf("render incremental: %v", err)
	}
	wholeState, err := whole.ProjectState()
	if err != nil {
		t.Fatalf("render whole: %v", err)
	}
	a, _ := protocol.CanonicalJSON(incrementalState)
	b, _ := protocol.CanonicalJSON(wholeState)
	if string(a) != string(b) {
		t.Fatalf("incremental and whole-stream reduction differ:\n%s\n%s", a, b)
	}
}

// TestStateRevisionTracksTheHighWatermark is the structural form of the
// consistency check in docs/PROJECT_STATE.md §15: the revision cannot drift
// from the journal position because it is derived from it.
func TestStateRevisionTracksTheHighWatermark(t *testing.T) {
	stream := testsupport.HappyPathScenario(t, "example").Stream()
	for i := range stream {
		projection, err := state.Reduce(stream[:i+1])
		if err != nil {
			t.Fatalf("reduce prefix of %d: %v", i+1, err)
		}
		projectState, err := projection.ProjectState()
		if err != nil {
			t.Fatalf("render prefix of %d: %v", i+1, err)
		}
		want := state.StateRevision(stream[i].Seq)
		if projectState.StateRevision != want {
			t.Fatalf("prefix of %d: revision = %s, want %s", i+1, projectState.StateRevision, want)
		}
		if projectState.EventHighWatermark == nil {
			t.Fatalf("prefix of %d: high-watermark is nil", i+1)
		}
		// generated_at comes from the last applied event, so reconstructing a
		// historical revision reproduces its timestamp exactly.
		if projectState.GeneratedAt.String() != stream[i].OccurredAt.String() {
			t.Fatalf("prefix of %d: generated_at = %s, want %s",
				i+1, projectState.GeneratedAt, stream[i].OccurredAt)
		}
	}
}

// TestEveryPrefixRendersAValidProjectState catches intermediate states that
// only happen to be valid at the end of a scenario.
func TestEveryPrefixRendersAValidProjectState(t *testing.T) {
	streams := map[string][]events.Event{
		"happy path": testsupport.HappyPathScenario(t, "example").Stream(),
		"blocked":    testsupport.BlockedScenario(t, "example").Stream(),
	}
	for name, stream := range streams {
		for i := range stream {
			projection, err := state.Reduce(stream[:i+1])
			if err != nil {
				t.Fatalf("%s: reduce prefix of %d: %v", name, i+1, err)
			}
			if _, err := projection.ProjectState(); err != nil {
				t.Fatalf("%s: prefix of %d rendered an invalid ProjectState: %v", name, i+1, err)
			}
		}
	}
}

package events_test

import (
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/testsupport"
)

func proposed() *events.WorkPackageProposed {
	return &events.WorkPackageProposed{
		TaskID: "tsk_1", WorkPackageID: "wp_0001", Version: 1,
		ProjectStateRevision: "ps_000000003", BaseCommit: "91acd8273f1", RecordDigest: "sha256:" + strings.Repeat("a", 64),
	}
}

func TestWorkPackageProposedValidation(t *testing.T) {
	if err := proposed().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*events.WorkPackageProposed){
		"task":     func(p *events.WorkPackageProposed) { p.TaskID = "" },
		"wp":       func(p *events.WorkPackageProposed) { p.WorkPackageID = "" },
		"version":  func(p *events.WorkPackageProposed) { p.Version = 0 },
		"revision": func(p *events.WorkPackageProposed) { p.ProjectStateRevision = "" },
		"base":     func(p *events.WorkPackageProposed) { p.BaseCommit = "" },
		"digest":   func(p *events.WorkPackageProposed) { p.RecordDigest = "" },
	} {
		p := proposed()
		mutate(p)
		if p.Validate() == nil {
			t.Errorf("%s: an incomplete proposal validated", name)
		}
	}
	if got := events.CorrelationFor(proposed()); got.TaskID != "tsk_1" || got.WorkPackageID != "wp_0001" {
		t.Fatalf("correlation %+v", got)
	}
}

// The reference checker refuses a proposal that describes a different record.
func TestWorkPackageProposedChecksTheStoredRecord(t *testing.T) {
	wp := testsupport.WorkPackage("example", "tsk_1", "wp_0001", 1)
	wp.ProjectStateRevision = "ps_000000003"
	raw, err := protocol.Marshal(wp)
	if err != nil {
		t.Fatal(err)
	}
	p := proposed()
	ref := p.ReferencedRecord()
	if ref.Kind != "EngineeringWorkPackage" || ref.ID != "wp_0001" || ref.Version != 1 || !ref.Claimed() {
		t.Fatalf("reference %+v", ref)
	}
	if err := p.CheckReferencedRecord(raw); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*events.WorkPackageProposed){
		"id":       func(p *events.WorkPackageProposed) { p.WorkPackageID = "wp_other" },
		"version":  func(p *events.WorkPackageProposed) { p.Version = 2 },
		"task":     func(p *events.WorkPackageProposed) { p.TaskID = "tsk_2" },
		"revision": func(p *events.WorkPackageProposed) { p.ProjectStateRevision = "ps_000000009" },
		"base":     func(p *events.WorkPackageProposed) { p.BaseCommit = "abcdef0" },
	} {
		bad := proposed()
		mutate(bad)
		if bad.CheckReferencedRecord(raw) == nil {
			t.Errorf("%s: a mismatching proposal was accepted", name)
		}
	}
	if p.CheckReferencedRecord([]byte(`{"nope":1}`)) == nil {
		t.Error("a non-record document was accepted")
	}
}

func TestHistoricalReadersRefuseTheNewEventType(t *testing.T) {
	// A reader that does not know the type refuses it rather than skipping it.
	_, err := events.DecodePayload("WorkPackageProposedFromTheFuture", []byte(`{}`))
	if err == nil {
		t.Fatal("an unknown event type was decoded")
	}
	if !events.Registered(events.TypeWorkPackageProposed) {
		t.Fatal("the proposal event is not registered")
	}
}

package compiler_test

import (
	"errors"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
)

func TestRule_Validation(t *testing.T) {
	// Missing or invalid SourceKind
	r1 := compiler.Rule{
		ID:             "R_NO_KIND",
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	}
	if err := r1.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for missing SourceKind, got %v", err)
	}

	// Missing ID
	r2 := compiler.Rule{
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	}
	if err := r2.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for missing ID, got %v", err)
	}

	// Invalid admission class
	r3 := compiler.Rule{
		ID:             "R1",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: "invalid_class",
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	}
	if err := r3.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid class, got %v", err)
	}

	// Capability default without capability
	r4 := compiler.Rule{
		ID:             "R2",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassCapabilityDefault,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	}
	if err := r4.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for missing capability, got %v", err)
	}

	// Valid rule
	rValid := compiler.Rule{
		ID:             "R3",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	}
	if err := rValid.Validate(); err != nil {
		t.Fatalf("expected valid rule, got %v", err)
	}
}

func TestCapabilityExclusion_Validation(t *testing.T) {
	// Missing capability
	e1 := compiler.CapabilityExclusion{
		DecisionID: "DEC-1",
		Revision:   "v1",
		Rationale:  "not needed",
	}
	if err := e1.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for missing capability, got %v", err)
	}

	// Missing decision ID
	e2 := compiler.CapabilityExclusion{
		Capability: "write",
		Revision:   "v1",
		Rationale:  "not needed",
	}
	if err := e2.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for missing decision ID, got %v", err)
	}

	// Missing revision
	e3 := compiler.CapabilityExclusion{
		Capability: "write",
		DecisionID: "DEC-1",
		Rationale:  "not needed",
	}
	if err := e3.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for missing revision, got %v", err)
	}

	// Missing rationale
	e4 := compiler.CapabilityExclusion{
		Capability: "write",
		DecisionID: "DEC-1",
		Revision:   "v1",
	}
	if err := e4.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for missing rationale, got %v", err)
	}

	// Valid exclusion
	eValid := compiler.CapabilityExclusion{
		Capability: "write",
		DecisionID: "DEC-1",
		Revision:   "v1",
		Rationale:  "read-only evaluation pass",
	}
	if err := eValid.Validate(); err != nil {
		t.Errorf("expected valid exclusion, got %v", err)
	}
}

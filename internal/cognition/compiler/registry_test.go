package compiler_test

import (
	"errors"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
)

func TestRuleRegistry_ValidationAndDuplicate(t *testing.T) {
	reg := compiler.NewRuleRegistry()

	// Missing or invalid SourceKind
	err := reg.Register(compiler.Rule{
		ID:             "R_NO_KIND",
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for missing SourceKind, got %v", err)
	}

	// Missing ID
	err = reg.Register(compiler.Rule{
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for missing ID, got %v", err)
	}

	// Invalid admission class
	err = reg.Register(compiler.Rule{
		ID:             "R1",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: "invalid_class",
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid class, got %v", err)
	}

	// Capability default without capability
	err = reg.Register(compiler.Rule{
		ID:             "R2",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassCapabilityDefault,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for missing capability, got %v", err)
	}

	// Valid registration
	err = reg.Register(compiler.Rule{
		ID:             "R3",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if err != nil {
		t.Fatalf("expected valid registration, got %v", err)
	}

	// Duplicate registration conflict
	err = reg.Register(compiler.Rule{
		ID:             "R3",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate registration, got %v", err)
	}
}

func TestRuleRegistry_DeepCopyImmutability(t *testing.T) {
	reg := compiler.NewRuleRegistry()

	domains := []string{"cognition"}
	roles := []string{"implementer"}
	dependsOn := []string{"DEP-1"}

	err := reg.Register(compiler.Rule{
		ID:             "RULE-IMMUTABLE",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassMapped,
		SourceDoc:      "docs/INVARIANTS.md",
		Revision:       "v1.0",
		Content:        "Rule testing slice immutability.",
		Domains:        domains,
		Roles:          roles,
		DependsOn:      dependsOn,
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// Mutate caller slices
	domains[0] = "mutated_domain"
	roles[0] = "mutated_role"
	dependsOn[0] = "mutated_dep"

	r, ok := reg.Get("RULE-IMMUTABLE")
	if !ok {
		t.Fatal("rule not found")
	}

	if r.Domains[0] != "cognition" {
		t.Errorf("Domains slice was mutated in backing storage! got %q, want cognition", r.Domains[0])
	}
	if r.Roles[0] != "implementer" {
		t.Errorf("Roles slice was mutated in backing storage! got %q, want implementer", r.Roles[0])
	}
	if r.DependsOn[0] != "DEP-1" {
		t.Errorf("DependsOn slice was mutated in backing storage! got %q, want DEP-1", r.DependsOn[0])
	}

	// Mutate returned slice
	r.Domains[0] = "another_mutation"
	r2, _ := reg.Get("RULE-IMMUTABLE")
	if r2.Domains[0] != "cognition" {
		t.Errorf("Get() exposed internal slice to mutation! got %q, want cognition", r2.Domains[0])
	}
}

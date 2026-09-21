package schema_test

import (
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/schema"
)

func TestDefaultSetCompilesAndIsCached(t *testing.T) {
	first, err := schema.Default()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	second, err := schema.Default()
	if err != nil {
		t.Fatalf("compile again: %v", err)
	}
	if first != second {
		t.Fatal("Default recompiled the schemas instead of reusing them")
	}
	if len(first.Names()) == 0 {
		t.Fatal("no schemas were compiled")
	}
}

func TestValidateRecordAcceptsAValidProjectState(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	watermark := "1"
	projectState := &protocol.ProjectState{
		SchemaVersion:      protocol.SchemaVersion1,
		ProjectID:          "example",
		StateRevision:      "ps_000000001",
		EventHighWatermark: &watermark,
		Milestone:          protocol.MilestoneState{ID: "M1", Title: "Domain core"},
		Validation:         protocol.ValidationState{Status: protocol.ValidationUnknown},
	}
	if err := set.ValidateRecord(projectState.RecordKind(), projectState); err != nil {
		t.Fatalf("a valid ProjectState was rejected: %v", err)
	}
}

func TestValidateRecordRejectsAnInvalidEnum(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	watermark := "1"
	projectState := &protocol.ProjectState{
		SchemaVersion:      protocol.SchemaVersion1,
		ProjectID:          "example",
		StateRevision:      "ps_000000001",
		EventHighWatermark: &watermark,
		Milestone:          protocol.MilestoneState{ID: "M1", Title: "Domain core"},
		Validation:         protocol.ValidationState{Status: protocol.ValidationStatus("probably_fine")},
	}
	if err := set.ValidateRecord(projectState.RecordKind(), projectState); err == nil {
		t.Fatal("an invalid validation status satisfied the schema")
	}
}

func TestUnknownSchemaIsNotFound(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := set.Schema("no-such-schema"); errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Fatalf("category = %s, want not_found", errs.CategoryOf(err))
	}
	if err := set.ValidateRecord("NoSuchRecord", struct{}{}); errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Fatalf("category = %s, want not_found", errs.CategoryOf(err))
	}
}

func TestNameForFile(t *testing.T) {
	name, err := schema.NameForFile("schemas/project-state.schema.json")
	if err != nil {
		t.Fatalf("NameForFile: %v", err)
	}
	if name != schema.NameProjectState {
		t.Fatalf("name = %s, want %s", name, schema.NameProjectState)
	}
	if _, err := schema.NameForFile("notes.txt"); err == nil {
		t.Fatal("a non-schema file name was accepted")
	}
}

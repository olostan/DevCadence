package tests_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/olostan/DevCadience/internal/protocol"
)

// TestSchemaTopLevelFieldsMatchTheGoTwin catches the drift that fixture tests
// cannot see.
//
// A fixture only exercises the fields it happens to use, so a property added
// to a schema with no counterpart on its Go type goes unnoticed until some
// document actually carries it — at which point strict decoding refuses a
// document the schema calls valid (DCI-092, ADR-0003). That is not a
// theoretical risk: `review` was added to project-state.schema.json while
// `protocol.ProjectState` had no such field, and nothing failed.
//
// The check is deliberately limited to top-level properties of the record
// schemas that have a Go twin. Schemas with no Go type at all — the M6
// review-convergence contracts — cannot diverge from a twin they do not have,
// and are covered instead by the awaitingImplementation list.
func TestSchemaTopLevelFieldsMatchTheGoTwin(t *testing.T) {
	for _, tc := range []struct {
		schema string
		record protocol.Record
	}{
		{"project-state.schema.json", &protocol.ProjectState{}},
		{"engineering-work-package.schema.json", &protocol.EngineeringWorkPackage{}},
		{"validation-result.schema.json", &protocol.ValidationResult{}},
		{"review-result.schema.json", &protocol.ReviewResult{}},
		{"specification-review-result.schema.json", &protocol.SpecificationReviewResult{}},
		{"evidence-packet.schema.json", &protocol.EvidencePacket{}},
		{"decision-record.schema.json", &protocol.DecisionRecord{}},
		{"lesson-candidate.schema.json", &protocol.LessonCandidate{}},
		{"problem-model.schema.json", &protocol.ProblemModel{}},
		{"ambiguity-ledger.schema.json", &protocol.AmbiguityLedger{}},
		{"product-decision.schema.json", &protocol.ProductDecision{}},
		{"requirement.schema.json", &protocol.Requirement{}},
		{"discovery-experiment.schema.json", &protocol.DiscoveryExperiment{}},
		{"specification-readiness.schema.json", &protocol.SpecificationReadiness{}},
	} {
		t.Run(tc.schema, func(t *testing.T) {
			published := topLevelProperties(t, tc.schema)
			declared := jsonFieldNames(reflect.TypeOf(tc.record).Elem())

			for _, name := range published {
				if !declared[name] {
					t.Errorf("schema publishes %q but %s has no such field; "+
						"strict decoding would refuse a document the schema accepts",
						name, tc.record.RecordKind())
				}
			}
			for name := range declared {
				if !contains(published, name) {
					t.Errorf("%s declares %q but the schema does not publish it; "+
						"the document would fail schema validation at the write boundary",
						tc.record.RecordKind(), name)
				}
			}
		})
	}
}

func topLevelProperties(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "schemas", name))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse schema %s: %v", name, err)
	}
	out := make([]string, 0, len(doc.Properties))
	for property := range doc.Properties {
		out = append(out, property)
	}
	sort.Strings(out)
	return out
}

func jsonFieldNames(t reflect.Type) map[string]bool {
	out := map[string]bool{}
	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		out[name] = true
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// Package tests holds cross-cutting suites that exercise several packages
// together: schema/fixture conformance and whole-lifecycle CLI behaviour.
//
// Package-local unit tests live next to the code they cover; these are the
// ones that would be artificial to place inside any one package.
package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/schema"
)

// fixtureDir is the published fixture corpus, relative to this package.
const fixtureDir = "../fixtures/protocol"

func TestEverySchemaCompiles(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("compile embedded schemas: %v", err)
	}
	// Every schema named in schemas/README.md must exist. A schema that was
	// documented but never added would otherwise go unnoticed.
	for _, name := range schema.AllNames() {
		if _, err := set.Schema(name); err != nil {
			t.Errorf("schema %s: %v", name, err)
		}
	}
}

// TestEveryRecordKindHasASchema keeps the Go types and the published schemas
// paired. A protocol type added without a schema, or a schema added without a
// type, fails here rather than at an integration boundary.
func TestEveryRecordKindHasASchema(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("compile embedded schemas: %v", err)
	}
	mapped := make(map[schema.Name]bool, len(schema.RecordKindToSchema))
	for kind, name := range schema.RecordKindToSchema {
		if _, err := set.Schema(name); err != nil {
			t.Errorf("record kind %s maps to missing schema %s", kind, name)
		}
		mapped[name] = true
	}
	for _, name := range set.Names() {
		if !mapped[name] {
			t.Errorf("schema %s has no Go record kind mapped to it", name)
		}
	}
}

// TestValidFixturesValidate is the positive half of the corpus contract.
func TestValidFixturesValidate(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("compile embedded schemas: %v", err)
	}
	files := fixtures(t, ".valid")
	if len(files) == 0 {
		t.Fatal("no valid fixtures found")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			name := schemaForFixture(t, file)
			document := read(t, file)
			if err := set.ValidateBytes(name, document); err != nil {
				t.Fatalf("fixture does not satisfy schema %s: %v", name, err)
			}
		})
	}
}

// TestInvalidFixturesAreRejected is the negative half. Without it the
// positive tests would pass against a schema that accepts anything.
func TestInvalidFixturesAreRejected(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("compile embedded schemas: %v", err)
	}
	files := fixtures(t, ".invalid-")
	if len(files) == 0 {
		t.Fatal("no invalid fixtures found")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			name := schemaForFixture(t, file)
			document := read(t, file)
			if err := set.ValidateBytes(name, document); err == nil {
				t.Fatalf("fixture was accepted by schema %s but must be rejected", name)
			}
		})
	}
}

// TestFixturesRoundTripWithoutSemanticLoss proves the twins agree on content,
// not only on shape: a fixture decoded into its Go type and re-encoded must
// still satisfy its schema and must carry the same information.
//
// Comparison is by informative content, not by bytes. Schema version 1.0
// treats an absent optional field, an explicit null and the JSON zero value
// of its type as the same statement — "nothing is asserted here" — and the Go
// encoding emits the shortest of those three. That equivalence is a durable
// compatibility decision, not a testing convenience; see
// docs/adr/0003-durable-record-compatibility.md. Anything that carries
// information is compared exactly.
func TestFixturesRoundTripWithoutSemanticLoss(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("compile embedded schemas: %v", err)
	}
	cases := []struct {
		file   string
		decode func([]byte) (protocol.Record, error)
	}{
		{"project-state.valid.json", decodeInto[protocol.ProjectState]},
		{"project-state.valid-uninitialised-repository.json", decodeInto[protocol.ProjectState]},
		{"engineering-work-package.valid.json", decodeInto[protocol.EngineeringWorkPackage]},
		{"evidence-packet.valid.json", decodeInto[protocol.EvidencePacket]},
		{"validation-result.valid.json", decodeInto[protocol.ValidationResult]},
		{"review-result.valid.json", decodeInto[protocol.ReviewResult]},
		{"decision-record.valid.json", decodeInto[protocol.DecisionRecord]},
		{"lesson-candidate.valid.json", decodeInto[protocol.LessonCandidate]},
		{"problem-model.valid.json", decodeInto[protocol.ProblemModel]},
		{"ambiguity-ledger.valid.json", decodeInto[protocol.AmbiguityLedger]},
		{"product-decision.valid.json", decodeInto[protocol.ProductDecision]},
		{"requirement.valid.json", decodeInto[protocol.Requirement]},
		{"discovery-experiment.valid.json", decodeInto[protocol.DiscoveryExperiment]},
		{"specification-readiness.valid.json", decodeInto[protocol.SpecificationReadiness]},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(fixtureDir, tc.file)
			original := read(t, path)
			record, err := tc.decode(original)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if record.SchemaVer() != protocol.SchemaVersion1 {
				t.Fatalf("schema_version = %q, want %s", record.SchemaVer(), protocol.SchemaVersion1)
			}
			reEncoded, err := protocol.Marshal(record)
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			if err := set.ValidateRecord(record.RecordKind(), record); err != nil {
				t.Fatalf("re-encoded record does not satisfy its schema: %v", err)
			}
			wantCanonical := canonicaliseInformative(t, original)
			gotCanonical := canonicaliseInformative(t, reEncoded)
			if wantCanonical != gotCanonical {
				t.Fatalf("round trip changed the document:\n have %s\n want %s", gotCanonical, wantCanonical)
			}

			// Decoding what we re-encoded must give back the same value, so
			// that repeated storage and retrieval cannot drift.
			again, err := tc.decode(reEncoded)
			if err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			secondEncoding, err := protocol.Marshal(again)
			if err != nil {
				t.Fatalf("re-encode twice: %v", err)
			}
			if string(secondEncoding) != string(reEncoded) {
				t.Fatalf("a second round trip changed the bytes:\n %s\n %s", secondEncoding, reEncoded)
			}
		})
	}
}

// decodeInto strictly decodes a fixture into T.
func decodeInto[T any, PT interface {
	*T
	protocol.Record
}](document []byte) (protocol.Record, error) {
	out := PT(new(T))
	if err := protocol.Unmarshal(document, out); err != nil {
		return nil, err
	}
	return out, nil
}

func fixtures(t *testing.T, marker string) []string {
	t.Helper()
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if strings.Contains(entry.Name(), marker) {
			out = append(out, filepath.Join(fixtureDir, entry.Name()))
		}
	}
	return out
}

// schemaForFixture derives the governing schema from the file-name prefix.
func schemaForFixture(t *testing.T, file string) schema.Name {
	t.Helper()
	base := filepath.Base(file)
	// AllNames is ordered longest first, so a fixture named
	// "product-decision.valid.json" cannot be mistaken for a shorter name
	// that happens to be a prefix.
	for _, candidate := range schema.AllNames() {
		if strings.HasPrefix(base, string(candidate)+".") {
			return candidate
		}
	}
	t.Fatalf("fixture %s does not name a schema; see fixtures/README.md", base)
	return ""
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return document
}

// canonicaliseInformative renders a document in canonical form after dropping
// the object members that assert nothing.
//
// The drop is symmetric, so it cannot hide a difference between two
// informative values; it only makes "absent", "null" and "zero" compare
// equal, which is exactly what the version 1.0 compatibility rule says they
// are.
func canonicaliseInformative(t *testing.T, document []byte) string {
	t.Helper()
	var generic any
	decoder := json.NewDecoder(strings.NewReader(string(document)))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		t.Fatalf("parse document: %v", err)
	}
	canonical, err := protocol.CanonicalJSON(dropUninformative(generic))
	if err != nil {
		t.Fatalf("canonicalise: %v", err)
	}
	return string(canonical)
}

// dropUninformative removes object members whose value is null, false, zero,
// the empty string, an empty array or an empty object.
func dropUninformative(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, member := range typed {
			cleaned := dropUninformative(member)
			if uninformative(cleaned) {
				continue
			}
			out[key] = cleaned
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			// Array elements are positional, so nothing is dropped from an
			// array: removing one would shift the rest.
			out = append(out, dropUninformative(item))
		}
		return out
	default:
		return value
	}
}

func uninformative(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case bool:
		return !typed
	case string:
		return typed == ""
	case json.Number:
		return typed.String() == "0"
	case map[string]any:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	}
	return false
}

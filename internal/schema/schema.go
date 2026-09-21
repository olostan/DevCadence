// Package schema compiles and applies the normative JSON Schemas under
// schemas/.
//
// ENGINEERING_STANDARDS.md §5 makes the JSON Schema normative at integration
// boundaries and the Go type its twin. This package is what lets CI prove the
// twins agree: it validates documents produced by the Go types against the
// published schemas, so a divergence fails a test instead of reaching an
// integration partner.
//
// Compilation uses github.com/santhosh-tekuri/jsonschema, a mature Draft
// 2020-12 implementation. Writing a schema compiler here would be a large,
// security-relevant subsystem with no project-specific value.
package schema

import (
	"encoding/json"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	devcadience "github.com/olostan/DevCadience"
	"github.com/olostan/DevCadience/internal/errs"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Name identifies a schema by its file name without the `.schema.json`
// suffix, e.g. "project-state".
type Name string

// Names of the schemas published in M0.
const (
	NameProjectState           Name = "project-state"
	NameEngineeringWorkPackage Name = "engineering-work-package"
	NameEvidencePacket         Name = "evidence-packet"
	NameValidationResult       Name = "validation-result"
	NameReviewResult           Name = "review-result"
	NameDecisionRecord         Name = "decision-record"
	NameLessonCandidate        Name = "lesson-candidate"
)

// Names of the discovery and specification schemas added by
// docs/adr/0001-discovery-specification-subsystem.md.
const (
	NameProblemModel           Name = "problem-model"
	NameAmbiguityLedger        Name = "ambiguity-ledger"
	NameProductDecision        Name = "product-decision"
	NameRequirement            Name = "requirement"
	NameDiscoveryExperiment    Name = "discovery-experiment"
	NameSpecificationReadiness Name = "specification-readiness"
)

// RecordKindToSchema maps a Go record kind to the schema that governs it.
// It is the explicit statement of which twin belongs to which, so that a new
// protocol type cannot be added without deciding on its schema.
var RecordKindToSchema = map[string]Name{
	"ProjectState":           NameProjectState,
	"EngineeringWorkPackage": NameEngineeringWorkPackage,
	"EvidencePacket":         NameEvidencePacket,
	"ValidationResult":       NameValidationResult,
	"ReviewResult":           NameReviewResult,
	"DecisionRecord":         NameDecisionRecord,
	"LessonCandidate":        NameLessonCandidate,
	"ProblemModel":           NameProblemModel,
	"AmbiguityLedger":        NameAmbiguityLedger,
	"ProductDecision":        NameProductDecision,
	"Requirement":            NameRequirement,
	"DiscoveryExperiment":    NameDiscoveryExperiment,
	"SpecificationReadiness": NameSpecificationReadiness,
}

// Set is a compiled collection of schemas.
type Set struct {
	compiled map[Name]*jsonschema.Schema
	names    []Name
}

var (
	defaultOnce sync.Once
	defaultSet  *Set
	defaultErr  error
)

// Default returns the schemas embedded in this build, compiling them once.
func Default() (*Set, error) {
	defaultOnce.Do(func() {
		defaultSet, defaultErr = Load(devcadience.SchemaFS, "schemas")
	})
	return defaultSet, defaultErr
}

// Load compiles every `*.schema.json` under dir in the supplied filesystem.
//
// All schemas are registered with the compiler before any is compiled, so
// that cross-schema `$ref`s resolve regardless of file order.
func Load(fsys fs.FS, dir string) (*Set, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "read schema directory %s", dir)
	}
	compiler := jsonschema.NewCompiler()
	// Format assertion is opt-in in Draft 2020-12 and in this library, which
	// means `format: "date-time"` is an annotation by default and a malformed
	// timestamp would validate. The schemas use format as a constraint the Go
	// types cannot express, so it is asserted here; without this the twin
	// representations would disagree exactly where the Go type is weakest
	// (a timestamp held as a string).
	compiler.AssertFormat()

	type pending struct {
		name Name
		id   string
	}
	var queue []pending
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "read schema %s", entry.Name())
		}
		document, err := jsonschema.UnmarshalJSON(strings.NewReader(string(body)))
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "schema %s is not valid JSON", entry.Name())
		}
		// Resources are registered under their file name so that a schema can
		// be addressed without depending on the $id host.
		resource := "devcadience:///" + entry.Name()
		if err := compiler.AddResource(resource, document); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "register schema %s", entry.Name())
		}
		queue = append(queue, pending{
			name: Name(strings.TrimSuffix(entry.Name(), ".schema.json")),
			id:   resource,
		})
	}
	if len(queue) == 0 {
		return nil, errs.New(errs.CategoryInternal, "no schemas found under %s", dir)
	}

	set := &Set{compiled: make(map[Name]*jsonschema.Schema, len(queue))}
	for _, item := range queue {
		compiledSchema, err := compiler.Compile(item.id)
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "compile schema %s", item.name)
		}
		set.compiled[item.name] = compiledSchema
		set.names = append(set.names, item.name)
	}
	sort.Slice(set.names, func(i, j int) bool { return set.names[i] < set.names[j] })
	return set, nil
}

// Names returns the compiled schema names in sorted order.
func (s *Set) Names() []Name { return append([]Name(nil), s.names...) }

// Schema returns one compiled schema.
func (s *Set) Schema(name Name) (*jsonschema.Schema, error) {
	compiledSchema, ok := s.compiled[name]
	if !ok {
		return nil, errs.New(errs.CategoryNotFound, "schema %s is not compiled into this build", name)
	}
	return compiledSchema, nil
}

// ValidateBytes checks a JSON document against the named schema.
func (s *Set) ValidateBytes(name Name, document []byte) error {
	compiledSchema, err := s.Schema(name)
	if err != nil {
		return err
	}
	value, err := jsonschema.UnmarshalJSON(strings.NewReader(string(document)))
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "document is not valid JSON")
	}
	if err := compiledSchema.Validate(value); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "document does not satisfy schema %s", name)
	}
	return nil
}

// ValidateValue serialises v and checks it against the named schema.
func (s *Set) ValidateValue(name Name, v any) error {
	document, err := json.Marshal(v)
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "serialise value for schema %s", name)
	}
	return s.ValidateBytes(name, document)
}

// ValidateRecord checks a Go protocol record against the schema registered
// for its kind. This is the check that keeps the two representations of a
// contract from drifting apart.
func (s *Set) ValidateRecord(kind string, v any) error {
	name, ok := RecordKindToSchema[kind]
	if !ok {
		return errs.New(errs.CategoryNotFound, "no schema is registered for record kind %s", kind)
	}
	return s.ValidateValue(name, v)
}

// AllNames returns every schema name this build knows, longest first so that
// a prefix match cannot pick a shorter name that is a prefix of a longer one.
func AllNames() []Name {
	names := []Name{
		NameProjectState, NameEngineeringWorkPackage, NameEvidencePacket,
		NameValidationResult, NameReviewResult, NameDecisionRecord,
		NameLessonCandidate, NameProblemModel, NameAmbiguityLedger,
		NameProductDecision, NameRequirement, NameDiscoveryExperiment,
		NameSpecificationReadiness,
	}
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})
	return names
}

// NameForFile derives a schema name from a file name.
func NameForFile(fileName string) (Name, error) {
	base := path.Base(fileName)
	if !strings.HasSuffix(base, ".schema.json") {
		return "", errs.New(errs.CategoryInvalidArgument,
			"%s is not a schema file (expected *.schema.json)", fileName)
	}
	return Name(strings.TrimSuffix(base, ".schema.json")), nil
}

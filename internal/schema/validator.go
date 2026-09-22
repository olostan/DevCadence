package schema

import (
	"github.com/olostan/DevCadence/internal/errs"
)

// RecordValidator is the write-boundary check that a durable record's
// serialised document satisfies its published JSON Schema.
//
// It exists as an interface so that the storage layer can enforce the check
// without owning the policy: storage knows that durable records are validated
// before they are committed, while which schema governs which record kind
// stays here, next to the schemas.
type RecordValidator interface {
	// ValidateDocument checks a canonical JSON document against the schema
	// registered for the record kind.
	ValidateDocument(kind string, document []byte) error
}

// Validator returns the RecordValidator backed by this Set.
func (s *Set) Validator() RecordValidator { return setValidator{set: s} }

type setValidator struct{ set *Set }

// ValidateDocument implements RecordValidator.
func (v setValidator) ValidateDocument(kind string, document []byte) error {
	name, ok := RecordKindToSchema[kind]
	if !ok {
		// A record kind with no registered schema cannot be checked, and an
		// unchecked durable record is exactly what this boundary exists to
		// prevent. Refusing it means a newly added protocol type must be
		// registered before it can be persisted, rather than silently
		// bypassing the twin-representation rule.
		return errs.New(errs.CategoryInternal,
			"record kind %s has no registered JSON Schema; register it in "+
				"internal/schema before persisting records of this kind", kind)
	}
	return v.set.ValidateBytes(name, document)
}

// DefaultValidator returns the validator over the schemas embedded in this
// build.
func DefaultValidator() (RecordValidator, error) {
	set, err := Default()
	if err != nil {
		return nil, err
	}
	return set.Validator(), nil
}

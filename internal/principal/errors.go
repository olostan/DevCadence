package principal

import "github.com/olostan/DevCadence/internal/errs"

// CodedError carries an internal error code, retryability indicator,
// evidence references, and diagnostic detail text.
type CodedError struct {
	code      string
	retryable bool
	refs      []string
	detail    string
}

// NewCodedError constructs a new CodedError.
func NewCodedError(code string, retryable bool, refs []string, detail string) *CodedError {
	var copiedRefs []string
	if refs != nil {
		copiedRefs = append([]string(nil), refs...)
	}
	return &CodedError{
		code:      code,
		retryable: retryable,
		refs:      copiedRefs,
		detail:    detail,
	}
}

func (e *CodedError) Error() string          { return e.code + ": " + e.detail }
func (e *CodedError) Code() string           { return e.code }
func (e *CodedError) Retryable() bool        { return e.retryable }
func (e *CodedError) EvidenceRefs() []string { return append([]string(nil), e.refs...) }
func (e *CodedError) Detail() string         { return e.detail }

// Unwrap returns an underlying categorised *errs.Error for compatibility with errs.CategoryOf.
func (e *CodedError) Unwrap() error {
	var cat errs.Category
	switch e.code {
	case CodeConflict:
		cat = errs.CategoryConflict
	case CodePolicyDenied:
		cat = errs.CategoryPolicyDenied
	case CodeInvalidArgument:
		cat = errs.CategoryInvalidArgument
	case CodeNotFound:
		cat = errs.CategoryNotFound
	case CodeIntegrity:
		cat = errs.CategoryIntegrity
	case CodeValidationFailed:
		cat = errs.CategoryValidationFailed
	case CodeContradictedAssumption:
		cat = errs.CategoryContradictedAssumption
	case CodeNeedsPrincipal:
		cat = errs.CategoryNeedsPrincipal
	case CodeModelUnavailable:
		cat = errs.CategoryModelUnavailable
	case CodeContextUnfit:
		cat = errs.CategoryContextUnfit
	default:
		cat = errs.CategoryInternal
	}
	return errs.New(cat, "%s", e.detail)
}

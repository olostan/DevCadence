package principal

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

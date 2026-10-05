package principal

import (
	"github.com/olostan/DevCadence/internal/errs"
)

// CallerContext is the trusted identity and grant set of one caller.
//
// It comes from a local protected launch binding, never from a request: it has
// no JSON tags and refuses to be marshalled or unmarshalled, so no wire
// document can mint or carry permission (I5, DCI-080/123/124). Unknown grants
// do not count as authority: Allows is an exact match against AllowedActions.
type CallerContext struct {
	PrincipalID      string
	ProjectID        string
	AllowedActions   []string
	PolicyRef        string
	MaxEvidenceBytes int
	MaxSnippetLines  int
	SourceDepth      string
}

// MarshalJSON refuses: CallerContext is not a wire object.
func (CallerContext) MarshalJSON() ([]byte, error) {
	return nil, errs.New(errs.CategoryPolicyDenied, "CallerContext is a trusted internal object and is never serialised")
}

// UnmarshalJSON refuses: a request can never supply a caller context.
func (*CallerContext) UnmarshalJSON([]byte) error {
	return errs.New(errs.CategoryPolicyDenied, "CallerContext cannot be supplied by a request")
}

// Validate checks that the binding is well formed. It does not grant anything.
func (c CallerContext) Validate() error {
	if err := ValidateID("principal_id", c.PrincipalID); err != nil {
		return err
	}
	if err := ValidateID("project_id", c.ProjectID); err != nil {
		return err
	}
	for _, action := range c.AllowedActions {
		if err := ValidateID("allowed_actions entry", action); err != nil {
			return err
		}
	}
	if c.PolicyRef != "" {
		if err := ValidateID("policy_ref", c.PolicyRef); err != nil {
			return err
		}
	}
	if c.SourceDepth != "" {
		if err := ValidateID("source_depth", c.SourceDepth); err != nil {
			return err
		}
	}
	if c.MaxEvidenceBytes < 0 || c.MaxSnippetLines < 0 {
		return errs.New(errs.CategoryInvalidArgument, "evidence and snippet limits must not be negative")
	}
	return nil
}

// Allows reports whether action is explicitly granted.
func (c CallerContext) Allows(action string) bool {
	for _, allowed := range c.AllowedActions {
		if allowed == action {
			return true
		}
	}
	return false
}

// Authorize decides whether the bound caller may perform action against the
// envelope's project. Absent, unknown or cross-project grants are denied.
func (c CallerContext) Authorize(meta CallMeta, action string) error {
	if err := c.Validate(); err != nil {
		return errs.Wrap(errs.CategoryPolicyDenied, err, "caller binding is not valid")
	}
	if c.ProjectID != meta.ProjectID {
		return errs.New(errs.CategoryPolicyDenied, "caller is not bound to project %s", meta.ProjectID)
	}
	if !c.Allows(action) {
		return errs.New(errs.CategoryPolicyDenied, "action %q is not granted to the caller", action)
	}
	return nil
}

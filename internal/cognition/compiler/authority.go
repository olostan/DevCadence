package compiler

import (
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// AdmissionClass specifies how a mandatory rule is deterministically admitted (ADR-0020 §2).
type AdmissionClass string

const (
	// AdmissionClassAlways is a small project-wide authority floor present in every invocation.
	AdmissionClassAlways AdmissionClass = "always"
	// AdmissionClassCapabilityDefault is admitted whenever the invocation can exercise the named capability.
	AdmissionClassCapabilityDefault AdmissionClass = "capability_default"
	// AdmissionClassMapped is admitted through normal task/role/action/path/domain/risk rules and dependency closure.
	AdmissionClassMapped AdmissionClass = "mapped"
)

// Valid reports whether the admission class is known.
func (c AdmissionClass) Valid() bool {
	switch c {
	case AdmissionClassAlways, AdmissionClassCapabilityDefault, AdmissionClassMapped:
		return true
	}
	return false
}

// AuthoritySourceKind identifies the layer of authority from which an invariant or normative constraint originates.
// Authority hierarchy (ADR-0020, PROTOCOLS §10B):
//  1. System (DevCadence engine rules / invariants)
//  2. Organization (enterprise / team policies - future)
//  3. Project (project-local invariants in .devcadence/INVARIANTS.md)
//  4. Task (current EWP contract / task constraints)
type AuthoritySourceKind string

const (
	AuthoritySourceKindSystem       AuthoritySourceKind = "system"
	AuthoritySourceKindOrganization AuthoritySourceKind = "organization"
	AuthoritySourceKindProject      AuthoritySourceKind = "project"
	AuthoritySourceKindTask         AuthoritySourceKind = "task"
)

// Valid reports whether the authority source kind is recognized.
func (k AuthoritySourceKind) Valid() bool {
	switch k {
	case AuthoritySourceKindSystem, AuthoritySourceKindOrganization, AuthoritySourceKindProject, AuthoritySourceKindTask:
		return true
	}
	return false
}

// AuthoritySource identifies an authoritative origin of normative invariants.
// Represents a discrete layer in the authority hierarchy (ADR-0020, PROTOCOLS §10B).
type AuthoritySource struct {
	SourceKind AuthoritySourceKind `json:"source_kind"`
	SourceID   string              `json:"source_id"`
	Revision   string              `json:"revision"`
	Digest     string              `json:"digest"`
}

// Rule defines an operative normative requirement or invariant.
type Rule struct {
	ID                 string              `json:"id"`
	SourceKind         AuthoritySourceKind `json:"source_kind,omitempty"` // Layer of authority: system, organization, project, task (default: system)
	AdmissionClass     AdmissionClass      `json:"admission_class"`
	SourceDoc          string              `json:"source_doc"`
	Revision           string              `json:"revision"`
	Content            string              `json:"content"`
	ContentDigest      string              `json:"content_digest"`
	Capability         string              `json:"capability,omitempty"`          // Required if class is capability_default (e.g., "write", "exec", "network")
	Domains            []string            `json:"domains,omitempty"`             // Mapped domains
	RiskTags           []string            `json:"risk_tags,omitempty"`           // Mapped risk tags
	Roles              []string            `json:"roles,omitempty"`               // Mapped roles
	Actions            []string            `json:"actions,omitempty"`             // Mapped actions
	PathPatterns       []string            `json:"path_patterns,omitempty"`       // Mapped file paths / globs
	DependsOn          []string            `json:"depends_on,omitempty"`          // Mandatory dependency edges
	SelectionRationale string              `json:"selection_rationale,omitempty"` // Explicit rationale populated upon admission (PROTOCOLS §10B)
}

// Validate checks Rule field constraints.
func (r Rule) Validate() error {
	const kind = "Rule"
	if strings.TrimSpace(r.ID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: id cannot be empty", kind)
	}
	if !r.SourceKind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid or empty source_kind %q; explicit authority layer required (system, organization, project, task)", kind, r.SourceKind)
	}
	if !r.AdmissionClass.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid admission_class %q", kind, r.AdmissionClass)
	}
	if strings.TrimSpace(r.SourceDoc) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: source_doc cannot be empty", kind)
	}
	if strings.TrimSpace(r.Revision) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: revision cannot be empty", kind)
	}
	if strings.TrimSpace(r.Content) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: content cannot be empty", kind)
	}
	if r.AdmissionClass == AdmissionClassCapabilityDefault {
		if strings.TrimSpace(r.Capability) == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: capability_default rule must declare non-empty capability", kind)
		}
		if _, err := NormalizeCapability(r.Capability); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: capability_default rule declares invalid capability %q: %v", kind, r.Capability, err)
		}
	}
	return nil
}

func (r Rule) deepCopy() Rule {
	out := r
	out.Domains = copyStringSlice(r.Domains)
	out.RiskTags = copyStringSlice(r.RiskTags)
	out.Roles = copyStringSlice(r.Roles)
	out.Actions = copyStringSlice(r.Actions)
	out.PathPatterns = copyStringSlice(r.PathPatterns)
	out.DependsOn = copyStringSlice(r.DependsOn)
	return out
}

func copyStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// CapabilityExclusion represents an explicit, revision-pinned decision that a capability
// does not apply to an invocation (ADR-0020 §2).
type CapabilityExclusion struct {
	Capability string `json:"capability"`
	DecisionID string `json:"decision_id"`
	Revision   string `json:"revision"`
	Rationale  string `json:"rationale"`
}

// Validate checks CapabilityExclusion fields.
func (e CapabilityExclusion) Validate() error {
	const kind = "CapabilityExclusion"
	if strings.TrimSpace(e.Capability) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: capability cannot be empty", kind)
	}
	if strings.TrimSpace(e.DecisionID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: decision_id cannot be empty", kind)
	}
	if strings.TrimSpace(e.Revision) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: revision cannot be empty", kind)
	}
	if strings.TrimSpace(e.Rationale) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: rationale cannot be empty", kind)
	}
	return nil
}

package compiler

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/olostan/DevCadence/internal/errs"
)

// CanonicalCatalogVersion identifies the frozen catalog specification version.
const CanonicalCatalogVersion = "v1.0"

// CanonicalCatalogRevision pins the git commit of the canonical rules baseline.
const CanonicalCatalogRevision = "d99e40c2107965b5af53a8c429aa6286f430ff8f"

// CanonicalDomains lists the standard domain vocabulary recognized by DevCadence.
var CanonicalDomains = []string{
	"cognition",
	"compiler",
	"cognitive_compiler",
	"context_admission",
	"deterministic_rule_mapping",
	"context_profiling",
	"prompt_projection",
	"evidence_leasing",
	"state_capsule",
	"optional_retrieval",
	"repository_execution",
	"review_and_convergence",
	"review",
	"discovery_and_specification",
	"implementation",
	"work_packages",
	"architectural_reasoning",
	"validation",
}

// computeRuleDigest calculates sha256:hex(content).
func computeRuleDigest(content string) string {
	hasher := sha256.New()
	hasher.Write([]byte(content))
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}

// CanonicalRules returns the canonical DevCadence mandatory rules materialized from
// INVARIANTS.md, AGENTS.md, and ADR-0020 (Finding 3, PROTOCOLS §10B).
func CanonicalRules() []Rule {
	rev := CanonicalCatalogRevision
	doc := "INVARIANTS.md"

	rules := []Rule{
		// Always authority floor
		{
			ID:             "DCI-018",
			AdmissionClass: AdmissionClassAlways,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Authority does not imply residency: normative rules must not be preloaded wholesale. Normative authority does not imply default prompt admission. Every substantial context object must be required by the task contract, selected by deterministic role/domain/risk mapping, or retrieved to resolve an explicit question with provenance.",
		},
		{
			ID:             "DCI-019",
			AdmissionClass: AdmissionClassAlways,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Delegated execution has no hidden requirements. Every execution-critical MUST/MUST-NOT requirement must be present verbatim in the bounded Execution Contract or deterministically admitted as an exact, revision-pinned normative clause before the affected action. Return CONTEXT_UNFIT if budget exceeded rather than truncating.",
		},
		{
			ID:             "DCI-131",
			AdmissionClass: AdmissionClassAlways,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Control-plane complexity does not imply prompt complexity. Lifecycle legality, authority checks, candidate identity, retry/budget enforcement and closure eligibility that can be decided deterministically MUST be enforced by the control plane rather than delegated to model interpretation. A cognition invocation MUST receive only the task-specific semantic obligations/state it needs, not the full DevCadence process model.",
			DependsOn:      []string{"DCI-132"},
		},
		{
			ID:             "DCI-132",
			AdmissionClass: AdmissionClassAlways,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Mandatory applicability is never similarity-ranked away. Execution-critical MUST/MUST-NOT applicability is decided by deterministic admission class plus task/role/path/domain/risk/action mapping and dependency closure. Every mandatory clause MUST have a deterministic admission path; embeddings, lexical ranking, rerankers or model judgment may improve optional retrieval but MUST NOT remove an applicable mandatory clause.",
		},
		{
			ID:             "DCI-133",
			AdmissionClass: AdmissionClassAlways,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Operative obligations are resident; rationale is retrievable. A model-visible execution-critical obligation must be present as exact revision-pinned content, not only a reference handle. Supporting rationale and large evidence remain progressively retrievable unless required for the current decision.",
		},

		// Capability defaults
		{
			ID:             "DCI-030",
			AdmissionClass: AdmissionClassCapabilityDefault,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Autonomous work is isolated. Autonomous changes occur in isolated Git worktrees/branches based on an explicit base commit.",
			Capability:     "write",
		},
		{
			ID:             "DCI-031",
			AdmissionClass: AdmissionClassCapabilityDefault,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Execution workers do not merge directly to main. Acceptance and integration are separate control-plane decisions regardless of whether worker inference is local or remote.",
			Capability:     "write",
		},
		{
			ID:             "DCI-033",
			AdmissionClass: AdmissionClassCapabilityDefault,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "External processes are controlled. Commands have explicit working directory, environment policy, timeout/cancellation and captured output. Agents do not receive unconstrained shell authority by default.",
			Capability:     "exec",
		},

		// Mapped: Principal / Specification
		{
			ID:             "DCI-004",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Principal self-challenge is mandatory. For systemic and architectural changes, the principal must explicitly challenge its initial solution, verify material assumptions, consider alternatives, and search for counterevidence before implementation.",
			Roles:          []string{"principal_engineer", "principal"},
			Domains:        []string{"architectural_reasoning", "discovery_and_specification"},
		},
		{
			ID:             "DCI-005",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Material assumptions are visible. A material assumption must be represented as an assumption until verified. Inference must not be serialized as fact.",
			Roles:          []string{"principal_engineer", "implementer", "worker"},
			Domains:        []string{"architectural_reasoning", "discovery_and_specification", "implementation"},
		},
		{
			ID:             "DCI-006",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Consultants are independent evidence sources. Consultant output is never automatically authoritative. For important independent review, consultants should receive neutral problem statements before seeing the principal’s proposed answer when feasible.",
			Roles:          []string{"principal_engineer", "reviewer"},
			Domains:        []string{"review", "review_and_convergence"},
		},
		{
			ID:             "DCI-008",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Product ambiguity is not silently resolved. A frontier principal, consultant, or local agent must not silently turn unresolved human intent into a confirmed requirement or architectural fact.",
			Roles:          []string{"principal_engineer"},
			Domains:        []string{"discovery_and_specification"},
		},
		{
			ID:             "DCI-009",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Human product authority is preserved. Goals, acceptable tradeoffs, privacy preferences, user-visible semantics, scope choices and other product-authority decisions belong to the human/product authority. Engineering agents may explain consequences but may not override them for implementation convenience.",
			Roles:          []string{"principal_engineer"},
			Domains:        []string{"discovery_and_specification"},
		},

		// Mapped: Evidence & Context
		{
			ID:             "DCI-011",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Raw evidence remains retrievable. Compression must not destroy provenance. Every material evidence claim should point to a retrievable source: file/line/symbol, Git object, command output, test artifact, external source, or consultant result.",
			Roles:          []string{"reviewer", "principal_engineer"},
			Domains:        []string{"evidence_leasing", "review", "review_and_convergence"},
		},
		{
			ID:             "DCI-012",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Model confidence is not evidence. Self-reported confidence may be metadata but cannot replace deterministic checks, provenance, independent agreement/disagreement, or explicit verification.",
			Roles:          []string{"reviewer", "implementer"},
			Domains:        []string{"review", "review_and_convergence", "validation"},
		},
		{
			ID:             "DCI-014",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Evidence depth is progressive. The system should support summary -> symbol/signature -> focused snippet -> diff -> full file -> direct exploration rather than immediately transferring the maximum context.",
			Domains:        []string{"evidence_leasing", "cognition", "compiler"},
		},

		// Mapped: Work Package & Execution
		{
			ID:             "DCI-020",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Non-trivial implementation starts from an Engineering Work Package. Systemic and architectural changes must not be delegated to an implementer from an unstructured chat instruction.",
			Roles:          []string{"principal_engineer"},
			Domains:        []string{"work_packages"},
			DependsOn:      []string{"DCI-021"},
		},
		{
			ID:             "DCI-021",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Work Packages encode how, not only what. Where it reduces ambiguity, the principal must include implementation strategy, algorithms, pseudocode, interface sketches, examples, failure cases and test strategy.",
			Roles:          []string{"principal_engineer"},
			Domains:        []string{"work_packages"},
		},
		{
			ID:             "DCI-023",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Execution workers may challenge assumptions. An implementer must be able to stop and report a contradicted blueprint assumption with evidence.",
			Roles:          []string{"worker", "implementer"},
			Domains:        []string{"implementation", "repository_execution"},
		},
		{
			ID:             "DCI-024",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Execution workers may not silently override MUST constraints. If the implementation cannot satisfy a MUST condition, the task is blocked or escalated.",
			Roles:          []string{"worker", "implementer"},
			Domains:        []string{"implementation", "repository_execution"},
		},
		{
			ID:             "DCI-025",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Task scope cannot silently expand. Material scope expansion creates a revised Work Package or a separate task.",
			Roles:          []string{"worker", "implementer"},
			Domains:        []string{"implementation", "repository_execution"},
		},
		{
			ID:             "DCI-032",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Every candidate change has lineage. At minimum: project-state revision, task ID, Work Package version, base commit, attempt ID, model/profile, candidate commit, validation results, review results and decision.",
			Roles:          []string{"worker", "implementer", "reviewer"},
			Domains:        []string{"repository_execution", "review_and_convergence"},
		},

		// Mapped: Review & Convergence
		{
			ID:             "DCI-134",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Attempted resolution is not independent verification. An implementer or author may report a fix attempt or challenge with evidence but cannot establish that its own resolution is correct. Findings close only through the configured independent verification/adjudication authority or deterministic proof.",
			Roles:          []string{"reviewer", "implementer", "worker"},
			Domains:        []string{"review", "review_and_convergence"},
		},
		{
			ID:             "DCI-135",
			AdmissionClass: AdmissionClassMapped,
			SourceDoc:      doc,
			Revision:       rev,
			Content:        "Review conversation is not canonical review state. Material findings, dispositions/resolutions, verification and closure state have stable identities outside chat transcripts. Equivalent restatements do not reopen adjudicated findings without materially new evidence, changed contract or a repair regression.",
			Roles:          []string{"reviewer"},
			Domains:        []string{"review", "review_and_convergence"},
		},
	}

	for i := range rules {
		if rules[i].ContentDigest == "" {
			rules[i].ContentDigest = computeRuleDigest(rules[i].Content)
		}
	}

	return rules
}

// NewCanonicalRuleRegistry initializes, populates, and freezes the canonical DevCadence
// RuleRegistry with all known domains and normative clauses (Finding 3, PROTOCOLS §10B).
func NewCanonicalRuleRegistry() (*RuleRegistry, error) {
	reg := NewRuleRegistry()

	// Register canonical domain vocabulary
	for _, d := range CanonicalDomains {
		if err := reg.RegisterKnownDomain(d); err != nil {
			return nil, err
		}
	}

	// Register canonical rules
	for _, r := range CanonicalRules() {
		if err := reg.Register(r); err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "failed to register canonical rule %s", r.ID)
		}
	}

	// Freeze registry: validates reverse-coverage and marks immutable
	if err := reg.Freeze(); err != nil {
		return nil, errs.Wrap(errs.CategoryValidationFailed, err, "canonical registry failed freeze / reverse-coverage validation")
	}

	return reg, nil
}

// MustNewCanonicalRuleRegistry initializes the canonical registry and panics on error.
func MustNewCanonicalRuleRegistry() *RuleRegistry {
	reg, err := NewCanonicalRuleRegistry()
	if err != nil {
		panic(err)
	}
	return reg
}

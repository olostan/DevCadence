package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	devcadence "github.com/olostan/DevCadence"
	"github.com/olostan/DevCadence/internal/errs"
)

// CanonicalCatalogID identifies the canonical system invariants catalog.
const CanonicalCatalogID = "canonical-dci-invariants"

// CanonicalCatalogVersion identifies the frozen catalog specification version.
const CanonicalCatalogVersion = "v1.0"

// CanonicalSourceRevision pins the git commit of the canonical INVARIANTS.md baseline.
const CanonicalSourceRevision = "d99e40c2107965b5af53a8c429aa6286f430ff8f"

// CanonicalMappingRevision pins the revision of the canonical invariant applicability mappings.
const CanonicalMappingRevision = "v1.0.0-m3c-2b"

// CanonicalCatalogRevision pins the git commit of the canonical rules baseline (backwards-compatible alias for CanonicalSourceRevision).
const CanonicalCatalogRevision = CanonicalSourceRevision

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
	"execution",
	"review_and_convergence",
	"review",
	"discovery_and_specification",
	"discovery",
	"implementation",
	"work_packages",
	"architectural_reasoning",
	"architecture",
	"validation",
	"state",
	"controlplane",
	"refactoring",
	"health",
	"learning",
	"security",
	"schemas",
	"protocol",
	"bootstrap",
	"adoption",
	"economics",
	"portfolio",
	"governance",
	"workflow",
	"scope",
	"brownfield",
	"documentation",
	"safety",
	"setup",
}

// InvariantMapping specifies the deterministic admission path for an invariant.
type InvariantMapping struct {
	AdmissionClass AdmissionClass
	Capability     string
	Domains        []string
	RiskTags       []string
	Roles          []string
	Actions        []string
	PathPatterns   []string
	DependsOn      []string
}

// CanonicalInvariantMappings pairs all 94 DCI invariants with their deterministic admission path.
var CanonicalInvariantMappings = map[string]InvariantMapping{
	// A. Intelligence-boundary invariants (DCI-001..DCI-009)
	"DCI-001": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition"}, Roles: []string{"principal_engineer", "architect"}},
	"DCI-002": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition"}, Roles: []string{"principal_engineer", "worker"}},
	"DCI-003": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "architectural_reasoning"}},
	"DCI-004": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "architectural_reasoning"}, Roles: []string{"principal_engineer"}},
	"DCI-005": {AdmissionClass: AdmissionClassAlways}, // Material assumptions are visible
	"DCI-006": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "review"}, Roles: []string{"principal_engineer", "reviewer"}},
	"DCI-007": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "architectural_reasoning"}},
	"DCI-008": {AdmissionClass: AdmissionClassMapped, Domains: []string{"discovery_and_specification", "discovery"}},
	"DCI-009": {AdmissionClass: AdmissionClassMapped, Domains: []string{"discovery_and_specification", "discovery"}},

	// B. Context and evidence invariants (DCI-010..DCI-019)
	"DCI-010": {AdmissionClass: AdmissionClassMapped, Domains: []string{"context_admission", "cognition"}, Roles: []string{"principal_engineer"}},
	"DCI-011": {AdmissionClass: AdmissionClassAlways}, // Raw evidence remains retrievable
	"DCI-012": {AdmissionClass: AdmissionClassAlways}, // Model confidence is not evidence
	"DCI-013": {AdmissionClass: AdmissionClassAlways}, // Facts and interpretations remain distinguishable
	"DCI-014": {AdmissionClass: AdmissionClassMapped, Domains: []string{"context_admission", "evidence_leasing"}},
	"DCI-015": {AdmissionClass: AdmissionClassMapped, Domains: []string{"discovery_and_specification", "context_admission"}},
	"DCI-016": {AdmissionClass: AdmissionClassMapped, Domains: []string{"discovery_and_specification", "architectural_reasoning"}},
	"DCI-017": {AdmissionClass: AdmissionClassMapped, Domains: []string{"context_admission", "discovery_and_specification"}},
	"DCI-018": {AdmissionClass: AdmissionClassAlways}, // Durable knowledge is not resident context
	"DCI-019": {AdmissionClass: AdmissionClassAlways}, // Delegated execution has no hidden requirements

	// C. Work-package invariants (DCI-020..DCI-025)
	"DCI-020": {AdmissionClass: AdmissionClassMapped, Domains: []string{"work_packages", "implementation"}},
	"DCI-021": {AdmissionClass: AdmissionClassMapped, Domains: []string{"work_packages"}},
	"DCI-022": {AdmissionClass: AdmissionClassMapped, Domains: []string{"work_packages"}},
	"DCI-023": {AdmissionClass: AdmissionClassMapped, Domains: []string{"work_packages"}, Roles: []string{"worker", "principal_engineer"}},
	"DCI-024": {AdmissionClass: AdmissionClassAlways}, // Execution workers may not silently override MUST constraints
	"DCI-025": {AdmissionClass: AdmissionClassAlways}, // Task scope cannot silently expand

	// D. Source-control and execution invariants (DCI-030..DCI-034)
	"DCI-030": {AdmissionClass: AdmissionClassCapabilityDefault, Capability: "write"}, // Autonomous work is isolated
	"DCI-031": {AdmissionClass: AdmissionClassCapabilityDefault, Capability: "write"}, // Execution workers do not merge directly to main
	"DCI-032": {AdmissionClass: AdmissionClassMapped, Domains: []string{"repository_execution", "implementation"}},
	"DCI-033": {AdmissionClass: AdmissionClassCapabilityDefault, Capability: "exec"},  // External processes are controlled
	"DCI-034": {AdmissionClass: AdmissionClassCapabilityDefault, Capability: "write"}, // Parallel work cannot share mutable working trees

	// E. Verification invariants (DCI-040..DCI-049)
	"DCI-040": {AdmissionClass: AdmissionClassMapped, Domains: []string{"validation", "review"}},
	"DCI-041": {AdmissionClass: AdmissionClassMapped, Domains: []string{"validation", "review"}},
	"DCI-042": {AdmissionClass: AdmissionClassMapped, Domains: []string{"review", "review_and_convergence"}},
	"DCI-043": {AdmissionClass: AdmissionClassMapped, Domains: []string{"review", "review_and_convergence"}},
	"DCI-044": {AdmissionClass: AdmissionClassMapped, Domains: []string{"review", "review_and_convergence"}},
	"DCI-045": {AdmissionClass: AdmissionClassMapped, Domains: []string{"validation", "repository_execution"}},
	"DCI-046": {AdmissionClass: AdmissionClassMapped, Domains: []string{"review", "review_and_convergence"}},
	"DCI-047": {AdmissionClass: AdmissionClassMapped, Domains: []string{"review", "review_and_convergence"}},
	"DCI-048": {AdmissionClass: AdmissionClassMapped, Domains: []string{"review", "review_and_convergence"}},
	"DCI-049": {AdmissionClass: AdmissionClassMapped, Domains: []string{"review", "review_and_convergence"}},

	// F. Architecture and governance invariants (DCI-050..DCI-055)
	"DCI-050": {AdmissionClass: AdmissionClassMapped, Domains: []string{"architectural_reasoning", "state"}},
	"DCI-051": {AdmissionClass: AdmissionClassMapped, Domains: []string{"architectural_reasoning"}},
	"DCI-052": {AdmissionClass: AdmissionClassMapped, Domains: []string{"state", "controlplane"}},
	"DCI-053": {AdmissionClass: AdmissionClassMapped, Domains: []string{"state", "controlplane"}},
	"DCI-054": {AdmissionClass: AdmissionClassMapped, Domains: []string{"state", "protocol"}},
	"DCI-055": {AdmissionClass: AdmissionClassMapped, Domains: []string{"state", "cognition"}},

	// G. Refactoring and health invariants (DCI-060..DCI-063)
	"DCI-060": {AdmissionClass: AdmissionClassMapped, Domains: []string{"refactoring", "health"}},
	"DCI-061": {AdmissionClass: AdmissionClassMapped, Domains: []string{"refactoring", "architectural_reasoning"}},
	"DCI-062": {AdmissionClass: AdmissionClassMapped, Domains: []string{"refactoring", "health"}},
	"DCI-063": {AdmissionClass: AdmissionClassMapped, Domains: []string{"refactoring", "health", "validation"}},

	// H. Learning invariants (DCI-070..DCI-074)
	"DCI-070": {AdmissionClass: AdmissionClassMapped, Domains: []string{"learning"}},
	"DCI-071": {AdmissionClass: AdmissionClassMapped, Domains: []string{"learning"}},
	"DCI-072": {AdmissionClass: AdmissionClassMapped, Domains: []string{"learning", "governance"}},
	"DCI-073": {AdmissionClass: AdmissionClassMapped, Domains: []string{"learning"}},
	"DCI-074": {AdmissionClass: AdmissionClassMapped, Domains: []string{"learning"}},

	// I. Security and trust invariants (DCI-080..DCI-084)
	"DCI-080": {AdmissionClass: AdmissionClassCapabilityDefault, Capability: "credentials"},
	"DCI-081": {AdmissionClass: AdmissionClassCapabilityDefault, Capability: "credentials"},
	"DCI-082": {AdmissionClass: AdmissionClassCapabilityDefault, Capability: "filesystem"},
	"DCI-083": {AdmissionClass: AdmissionClassCapabilityDefault, Capability: "exec"},
	"DCI-084": {AdmissionClass: AdmissionClassCapabilityDefault, Capability: "write"},

	// J. Documentation and compatibility invariants (DCI-090..DCI-093)
	"DCI-090": {AdmissionClass: AdmissionClassMapped, Domains: []string{"schemas", "protocol"}},
	"DCI-091": {AdmissionClass: AdmissionClassAlways}, // Prose and schema must agree
	"DCI-092": {AdmissionClass: AdmissionClassMapped, Domains: []string{"schemas", "protocol"}},
	"DCI-093": {AdmissionClass: AdmissionClassMapped, Domains: []string{"schemas", "state"}},

	// K. Bootstrap invariants (DCI-100..DCI-113)
	"DCI-100": {AdmissionClass: AdmissionClassMapped, Domains: []string{"bootstrap", "scope"}},
	"DCI-101": {AdmissionClass: AdmissionClassMapped, Domains: []string{"bootstrap", "scope"}},
	"DCI-102": {AdmissionClass: AdmissionClassMapped, Domains: []string{"bootstrap", "scope"}},
	"DCI-103": {AdmissionClass: AdmissionClassMapped, Domains: []string{"bootstrap", "safety"}},
	"DCI-104": {AdmissionClass: AdmissionClassMapped, Domains: []string{"bootstrap", "cognition"}},
	"DCI-105": {AdmissionClass: AdmissionClassMapped, Domains: []string{"bootstrap", "setup"}},
	"DCI-106": {AdmissionClass: AdmissionClassMapped, Domains: []string{"bootstrap", "setup"}},
	"DCI-107": {AdmissionClass: AdmissionClassMapped, Domains: []string{"bootstrap", "setup"}},
	"DCI-108": {AdmissionClass: AdmissionClassMapped, Domains: []string{"bootstrap", "setup"}},
	"DCI-109": {AdmissionClass: AdmissionClassMapped, Domains: []string{"adoption", "documentation"}},
	"DCI-110": {AdmissionClass: AdmissionClassMapped, Domains: []string{"adoption", "brownfield"}},
	"DCI-111": {AdmissionClass: AdmissionClassMapped, Domains: []string{"adoption", "documentation"}},
	"DCI-112": {AdmissionClass: AdmissionClassMapped, Domains: []string{"adoption", "documentation"}},
	"DCI-113": {AdmissionClass: AdmissionClassMapped, Domains: []string{"adoption", "governance"}},

	// L. Adaptive cognition portfolio invariants (DCI-120..DCI-130)
	"DCI-120": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "economics"}},
	"DCI-121": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "economics"}},
	"DCI-122": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "economics"}},
	"DCI-123": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "governance"}},
	"DCI-124": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "governance"}},
	"DCI-125": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "workflow"}},
	"DCI-126": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "economics"}},
	"DCI-127": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "portfolio"}},
	"DCI-128": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "portfolio"}},
	"DCI-129": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "portfolio"}},
	"DCI-130": {AdmissionClass: AdmissionClassMapped, Domains: []string{"cognition", "portfolio"}},

	// M. Cognitive invocation and review-ledger invariants (DCI-131..DCI-135)
	"DCI-131": {AdmissionClass: AdmissionClassMapped, Domains: []string{"compiler", "cognitive_compiler", "cognition"}},
	"DCI-132": {AdmissionClass: AdmissionClassMapped, Domains: []string{"compiler", "cognitive_compiler", "context_admission"}},
	"DCI-133": {AdmissionClass: AdmissionClassMapped, Domains: []string{"compiler", "cognitive_compiler", "context_admission"}},
	"DCI-134": {AdmissionClass: AdmissionClassMapped, Domains: []string{"review", "review_and_convergence"}},
	"DCI-135": {AdmissionClass: AdmissionClassMapped, Domains: []string{"review", "review_and_convergence"}},
}

// computeRuleDigest calculates sha256:hex(content).
func computeRuleDigest(content string) string {
	hasher := sha256.New()
	hasher.Write([]byte(content))
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}

var dciHeadingRegex = regexp.MustCompile(`(?m)^### (DCI-\d{3}) — (.*)$`)

// ParseInvariantsFromDoc extracts all 94 DCI invariants with verbatim text and headings from INVARIANTS.md.
func ParseInvariantsFromDoc(docText string, rev string) ([]Rule, error) {
	if strings.TrimSpace(docText) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "ParseInvariantsFromDoc: docText cannot be empty")
	}

	matches := dciHeadingRegex.FindAllStringSubmatchIndex(docText, -1)
	if len(matches) == 0 {
		return nil, errs.New(errs.CategoryNotFound, "ParseInvariantsFromDoc: no DCI invariants found in document")
	}

	var rules []Rule
	for i, match := range matches {
		dciID := docText[match[2]:match[3]]
		heading := strings.TrimSpace(docText[match[0]:match[1]])

		// Extract body between current heading and next heading (or ## section or EOF)
		bodyStart := match[1]
		bodyEnd := len(docText)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		bodyChunk := docText[bodyStart:bodyEnd]
		if subIdx := strings.Index(bodyChunk, "\n## "); subIdx != -1 {
			bodyChunk = bodyChunk[:subIdx]
		}
		verbatimBody := strings.TrimSpace(bodyChunk)
		if verbatimBody == "" {
			return nil, errs.New(errs.CategoryInvalidArgument, "ParseInvariantsFromDoc: invariant %s has empty body", dciID)
		}

		content := heading + "\n\n" + verbatimBody
		digest := computeRuleDigest(content)

		mapping, ok := CanonicalInvariantMappings[dciID]
		if !ok {
			return nil, errs.New(errs.CategoryNotFound, "ParseInvariantsFromDoc: no admission mapping defined for %s", dciID)
		}

		rule := Rule{
			ID:             dciID,
			AdmissionClass: mapping.AdmissionClass,
			SourceDoc:      "INVARIANTS.md",
			Revision:       rev,
			Content:        content,
			ContentDigest:  digest,
			Capability:     mapping.Capability,
			Domains:        mapping.Domains,
			RiskTags:       mapping.RiskTags,
			Roles:          mapping.Roles,
			Actions:        mapping.Actions,
			PathPatterns:   mapping.PathPatterns,
			DependsOn:      mapping.DependsOn,
		}

		if err := rule.Validate(); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "ParseInvariantsFromDoc: rule %s invalid", dciID)
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

// CanonicalRules returns all 94 canonical DevCadence mandatory rules extracted verbatim from INVARIANTS.md.
func CanonicalRules() []Rule {
	rules, err := ParseInvariantsFromDoc(devcadence.InvariantsDoc, CanonicalCatalogRevision)
	if err != nil {
		panic(fmt.Sprintf("failed to parse canonical invariants: %v", err))
	}
	return rules
}

// NewCanonicalRuleRegistry creates, populates, and freezes the authoritative RuleRegistry
// with all 94 DCI invariants extracted verbatim from INVARIANTS.md.
func NewCanonicalRuleRegistry() (*RuleRegistry, error) {
	reg := NewRuleRegistry()

	// 1. Register all canonical domains
	for _, d := range CanonicalDomains {
		if err := reg.RegisterKnownDomain(d); err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "register canonical domain %q", d)
		}
	}

	// 2. Parse all 94 invariants from embedded INVARIANTS.md
	rules, err := ParseInvariantsFromDoc(devcadence.InvariantsDoc, CanonicalSourceRevision)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "parse canonical invariants")
	}

	// 3. Verify runtime bidirectional set-equality between parsed rules and CanonicalInvariantMappings
	if len(rules) != len(CanonicalInvariantMappings) {
		return nil, errs.New(errs.CategoryValidationFailed,
			"bidirectional set equality failure: %d rules parsed from INVARIANTS.md, but %d entries in CanonicalInvariantMappings",
			len(rules), len(CanonicalInvariantMappings))
	}
	parsedSet := make(map[string]bool, len(rules))
	for _, r := range rules {
		parsedSet[r.ID] = true
	}
	for mappingID := range CanonicalInvariantMappings {
		if !parsedSet[mappingID] {
			return nil, errs.New(errs.CategoryValidationFailed,
				"bidirectional set equality failure: mapping entry %q has no corresponding parsed invariant in INVARIANTS.md",
				mappingID)
		}
	}

	// 4. Register all extracted rules
	for _, r := range rules {
		if err := reg.Register(r); err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "register rule %s", r.ID)
		}
	}

	// 5. Verify 100% deterministic reverse coverage
	if err := reg.VerifyReverseCoverage(); err != nil {
		return nil, errs.Wrap(errs.CategoryValidationFailed, err, "verify reverse coverage")
	}

	// 6. Set first-class catalog provenance metadata (SourceRevision and MappingRevision separated)
	if err := reg.SetCatalogMeta(CanonicalCatalogID, CanonicalCatalogVersion, CanonicalSourceRevision, CanonicalMappingRevision); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "set catalog metadata")
	}

	// 7. Freeze registry to compute deterministic NormativeSourceDigest, AuthorityProjectionDigest,
	// and CatalogDigest, and ensure absolute immutability.
	if err := reg.Freeze(); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "freeze canonical rule registry")
	}

	return reg, nil
}

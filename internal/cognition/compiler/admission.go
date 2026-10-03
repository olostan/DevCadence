package compiler

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// ValidateReverseCoverage performs reverse-coverage validation (ADR-0020 §2, DCI-132).
// It verifies that EVERY registered mandatory clause has a deterministic admission path
// (i.e. is not orphaned).
func (reg *RuleRegistry) ValidateReverseCoverage() error {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.validateReverseCoverageLocked()
}

// VerifyReverseCoverage verifies reverse coverage across all registered rules (alias for ValidateReverseCoverage).
func (reg *RuleRegistry) VerifyReverseCoverage() error {
	return reg.ValidateReverseCoverage()
}

func (reg *RuleRegistry) validateReverseCoverageLocked() error {
	// 1. Identify directly reachable rules
	directlyReachable := make(map[string]bool)
	for id, r := range reg.rules {
		switch r.AdmissionClass {
		case AdmissionClassAlways:
			directlyReachable[id] = true
		case AdmissionClassCapabilityDefault:
			if strings.TrimSpace(r.Capability) != "" {
				directlyReachable[id] = true
			}
		case AdmissionClassMapped:
			if len(r.Roles) > 0 || len(r.Domains) > 0 || len(r.RiskTags) > 0 || len(r.Actions) > 0 || len(r.PathPatterns) > 0 {
				directlyReachable[id] = true
			}
		}
	}

	// 2. Compute transitive closure of reachable rules
	reachable := make(map[string]bool, len(directlyReachable))
	for id := range directlyReachable {
		reachable[id] = true
	}

	changed := true
	for changed {
		changed = false
		for id := range reachable {
			r := reg.rules[id]
			for _, depID := range r.DependsOn {
				if !reachable[depID] {
					if _, exists := reg.rules[depID]; exists {
						reachable[depID] = true
						changed = true
					}
				}
			}
		}
	}

	// 3. Find any orphaned rules
	var orphans []string
	for id := range reg.rules {
		if !reachable[id] {
			orphans = append(orphans, id)
		}
	}

	if len(orphans) > 0 {
		sort.Strings(orphans)
		return errs.New(errs.CategoryValidationFailed,
			"reverse-coverage validation failed: orphaned mandatory rule(s) without admission path: %v", orphans)
	}
	return nil
}

// AdmissionParams defines inputs for resolving applicable mandatory rules.
type AdmissionParams struct {
	Role                 string
	Action               string
	Domains              []string
	RiskTags             []string
	Paths                []string
	ActiveCapabilities   []string
	CapabilityExclusions []CapabilityExclusion
	ExcludedCapabilities []string // Deprecated / unprovenanced strings: requires typed decision in CapabilityExclusions
	ExplicitRuleIDs      []string
}

// ResolveAdmittedRules deterministically admits rules matching criteria and computes
// full dependency closure over all transitive dependencies (ADR-0020 §2, DCI-132).
// Invariants enforced:
// - Enforces reverse-coverage validation on the rule registry.
// - Unknown required domains fail closed with errs.CategoryInvalidArgument.
// - Capability exclusion requires typed revision-pinned decision (CapabilityExclusion).
// - Contradictory "active + excluded" fails closed with errs.CategoryInvalidArgument.
// - Records explicit selection rationale for each admitted rule (SelectionRationale).
func (reg *RuleRegistry) ResolveAdmittedRules(params AdmissionParams) ([]Rule, error) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	// 1. Enforce reverse-coverage on the registry
	if !reg.frozen {
		if err := reg.validateReverseCoverageLocked(); err != nil {
			return nil, err
		}
	}

	// 2. Validate domain vocabulary: unknown domains fail closed
	if len(reg.knownDomains) > 0 {
		for i, d := range params.Domains {
			clean := strings.ToLower(strings.TrimSpace(d))
			if clean == "" {
				return nil, errs.New(errs.CategoryInvalidArgument, "domains[%d] cannot be empty", i)
			}
			if _, known := reg.knownDomains[clean]; !known {
				return nil, errs.New(errs.CategoryInvalidArgument, "unknown or unmapped required domain %q", d)
			}
		}
	}

	// 3. Process active and excluded capabilities
	activeCapsSet := make(map[string]bool, len(params.ActiveCapabilities))
	for _, c := range params.ActiveCapabilities {
		canon, err := NormalizeCapability(c)
		if err != nil {
			return nil, err
		}
		activeCapsSet[canon] = true
	}

	// Excluded capabilities must have typed provenance
	exclusions := make(map[string]CapabilityExclusion)
	for _, excl := range params.CapabilityExclusions {
		if err := excl.Validate(); err != nil {
			return nil, err
		}
		canon, err := NormalizeCapability(excl.Capability)
		if err != nil {
			return nil, err
		}
		// Fail closed on contradictory active + excluded
		if activeCapsSet[canon] {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"contradictory capability %q is both active and excluded (decision %s)", excl.Capability, excl.DecisionID)
		}
		exclusions[canon] = excl
	}

	// If legacy unprovenanced ExcludedCapabilities strings are supplied, verify they have typed decisions or fail closed
	if len(params.ExcludedCapabilities) > 0 {
		for _, capName := range params.ExcludedCapabilities {
			canon, err := NormalizeCapability(capName)
			if err != nil {
				return nil, err
			}
			if activeCapsSet[canon] {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"contradictory capability %q is both active and excluded", capName)
			}
			if _, hasTyped := exclusions[canon]; !hasTyped {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"unprovenanced exclusion of capability %q: exclusion requires typed, revision-pinned CapabilityExclusion", capName)
			}
		}
	}

	admittedMap := make(map[string]Rule)
	selectionRationale := make(map[string]string)

	domainsSet := make(map[string]bool, len(params.Domains))
	for _, d := range params.Domains {
		domainsSet[strings.ToLower(strings.TrimSpace(d))] = true
	}
	riskTagsSet := make(map[string]bool, len(params.RiskTags))
	for _, r := range params.RiskTags {
		riskTagsSet[strings.ToLower(strings.TrimSpace(r))] = true
	}

	targetRole := strings.ToLower(strings.TrimSpace(params.Role))
	targetAction := strings.ToLower(strings.TrimSpace(params.Action))

	// 4. Initial admission pass
	for id, rule := range reg.rules {
		switch rule.AdmissionClass {
		case AdmissionClassAlways:
			admittedMap[id] = rule
			selectionRationale[id] = "admitted by always authority floor (DCI-132)"
		case AdmissionClassCapabilityDefault:
			canonCap, err := NormalizeCapability(rule.Capability)
			if err != nil {
				canonCap = strings.ToLower(strings.TrimSpace(rule.Capability))
			}
			if _, isExcluded := exclusions[canonCap]; isExcluded {
				continue
			}
			if activeCapsSet[canonCap] {
				admittedMap[id] = rule
				selectionRationale[id] = fmt.Sprintf("admitted by active capability %q", rule.Capability)
			}
		case AdmissionClassMapped:
			matched := false
			rationale := ""

			// Check role match
			for _, r := range rule.Roles {
				if strings.ToLower(r) == targetRole {
					matched = true
					rationale = fmt.Sprintf("admitted by role mapping: %s", targetRole)
					break
				}
			}
			// Check action match
			if !matched && targetAction != "" {
				for _, a := range rule.Actions {
					if strings.ToLower(a) == targetAction {
						matched = true
						rationale = fmt.Sprintf("admitted by action mapping: %s", targetAction)
						break
					}
				}
			}
			// Check domain match
			if !matched {
				for _, d := range rule.Domains {
					if domainsSet[strings.ToLower(d)] {
						matched = true
						rationale = fmt.Sprintf("admitted by domain mapping: %s", d)
						break
					}
				}
			}
			// Check risk tag match
			if !matched {
				for _, rt := range rule.RiskTags {
					if riskTagsSet[strings.ToLower(rt)] {
						matched = true
						rationale = fmt.Sprintf("admitted by risk tag mapping: %s", rt)
						break
					}
				}
			}
			// Check path matches
			if !matched && len(params.Paths) > 0 {
				for _, p := range params.Paths {
					if matchPathPatterns(p, rule.PathPatterns) {
						matched = true
						rationale = fmt.Sprintf("admitted by path pattern mapping: %s", p)
						break
					}
				}
			}

			if matched {
				admittedMap[id] = rule
				selectionRationale[id] = rationale
			}
		}
	}

	// 5. Explicit rule inclusions (fail-closed if missing)
	for _, explicitID := range params.ExplicitRuleIDs {
		rule, ok := reg.rules[explicitID]
		if !ok {
			return nil, errs.New(errs.CategoryNotFound, "explicit mandatory rule %q not registered in rule registry", explicitID)
		}
		admittedMap[explicitID] = rule
		selectionRationale[explicitID] = "admitted by explicit rule ID inclusion"
	}

	// 6. Transitive dependency closure loop (fail-closed if dependency is missing)
	changed := true
	for changed {
		changed = false
		for _, rule := range admittedMap {
			for _, depID := range rule.DependsOn {
				if _, alreadyAdmitted := admittedMap[depID]; !alreadyAdmitted {
					depRule, ok := reg.rules[depID]
					if !ok {
						return nil, errs.New(errs.CategoryNotFound,
							"rule %q depends on unregistered rule %q", rule.ID, depID)
					}
					admittedMap[depID] = depRule
					selectionRationale[depID] = fmt.Sprintf("admitted by transitive dependency of %s", rule.ID)
					changed = true
				}
			}
		}
	}

	// Deterministic sorting by ID and attaching rationale
	result := make([]Rule, 0, len(admittedMap))
	for _, r := range admittedMap {
		ruleCopy := r.deepCopy()
		ruleCopy.SelectionRationale = selectionRationale[r.ID]
		result = append(result, ruleCopy)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}

func matchPathPatterns(targetPath string, patterns []string) bool {
	cleanTarget := path.Clean(targetPath)
	for _, pat := range patterns {
		cleanPat := path.Clean(pat)
		if cleanPat == "*" || cleanPat == cleanTarget {
			return true
		}
		if strings.HasSuffix(pat, "/*") {
			prefix := strings.TrimSuffix(pat, "/*")
			if cleanTarget == prefix || strings.HasPrefix(cleanTarget, prefix+"/") {
				return true
			}
		}
		if strings.HasSuffix(pat, "/...") {
			prefix := strings.TrimSuffix(pat, "/...")
			if cleanTarget == prefix || strings.HasPrefix(cleanTarget, prefix+"/") {
				return true
			}
		}
		matched, err := filepath.Match(pat, targetPath)
		if err == nil && matched {
			return true
		}
	}
	return false
}

package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
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

// Rule defines an operative normative requirement or invariant.
type Rule struct {
	ID                 string         `json:"id"`
	AdmissionClass     AdmissionClass `json:"admission_class"`
	SourceDoc          string         `json:"source_doc"`
	Revision           string         `json:"revision"`
	Content            string         `json:"content"`
	ContentDigest      string         `json:"content_digest"`
	Capability         string         `json:"capability,omitempty"`          // Required if class is capability_default (e.g., "write", "exec", "network")
	Domains            []string       `json:"domains,omitempty"`             // Mapped domains
	RiskTags           []string       `json:"risk_tags,omitempty"`           // Mapped risk tags
	Roles              []string       `json:"roles,omitempty"`               // Mapped roles
	Actions            []string       `json:"actions,omitempty"`             // Mapped actions
	PathPatterns       []string       `json:"path_patterns,omitempty"`       // Mapped file paths / globs
	DependsOn          []string       `json:"depends_on,omitempty"`          // Mandatory dependency edges
	SelectionRationale string         `json:"selection_rationale,omitempty"` // Explicit rationale populated upon admission (PROTOCOLS §10B)
}

// Validate checks Rule field constraints.
func (r Rule) Validate() error {
	const kind = "Rule"
	if strings.TrimSpace(r.ID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: id cannot be empty", kind)
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
	if r.AdmissionClass == AdmissionClassCapabilityDefault && strings.TrimSpace(r.Capability) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: capability_default rule must declare non-empty capability", kind)
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
// does not apply to an invocation (Finding 1, ADR-0020 §2).
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

// DeriveActiveCapabilities deterministically derives active capabilities from session, tool, and channel facts (Finding 1).
func DeriveActiveCapabilities(channel *protocol.AccessChannel, tools []string, declaredCaps []string) []string {
	seen := make(map[string]bool)

	if channel != nil {
		if channel.NativeWorktreeAccess {
			seen["write"] = true
			seen["filesystem"] = true
		}
		if channel.CredentialRefID != nil && *channel.CredentialRefID != "" {
			seen["credentials"] = true
		}
		if channel.Kind == protocol.ChannelDirectHTTPAPI || channel.Kind == protocol.ChannelRemoteAgentProxy {
			seen["network"] = true
		}
		if channel.SupportsTools {
			seen["tools"] = true
		}
	}

	for _, tool := range tools {
		t := strings.ToLower(tool)
		if strings.Contains(t, "bash") || strings.Contains(t, "exec") || strings.Contains(t, "command") || strings.Contains(t, "terminal") {
			seen["exec"] = true
		}
		if strings.Contains(t, "write") || strings.Contains(t, "replace") || strings.Contains(t, "edit") || strings.Contains(t, "create") {
			seen["write"] = true
		}
		if strings.Contains(t, "fetch") || strings.Contains(t, "http") || strings.Contains(t, "web") || strings.Contains(t, "curl") {
			seen["network"] = true
		}
	}

	for _, capName := range declaredCaps {
		c := strings.ToLower(strings.TrimSpace(capName))
		if c != "" {
			seen[c] = true
		}
	}

	result := make([]string, 0, len(seen))
	for c := range seen {
		result = append(result, c)
	}
	sort.Strings(result)
	return result
}

// RuleRegistry stores and validates the canonical corpus of normative rules.
type RuleRegistry struct {
	mu           sync.RWMutex
	rules        map[string]Rule
	knownDomains map[string]struct{}
	frozen       bool
}

// NewRuleRegistry creates an empty RuleRegistry.
func NewRuleRegistry() *RuleRegistry {
	return &RuleRegistry{
		rules:        make(map[string]Rule),
		knownDomains: make(map[string]struct{}),
	}
}

// RegisterKnownDomain adds a recognized domain identifier to the registry vocabulary.
func (reg *RuleRegistry) RegisterKnownDomain(domain string) error {
	clean := strings.ToLower(strings.TrimSpace(domain))
	if clean == "" {
		return errs.New(errs.CategoryInvalidArgument, "domain cannot be empty")
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.frozen {
		return errs.New(errs.CategoryConflict, "RuleRegistry is frozen; cannot register new domain")
	}
	reg.knownDomains[clean] = struct{}{}
	return nil
}

// IsDomainKnown reports whether the domain is part of the known domain vocabulary.
func (reg *RuleRegistry) IsDomainKnown(domain string) bool {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	clean := strings.ToLower(strings.TrimSpace(domain))
	_, ok := reg.knownDomains[clean]
	return ok
}

// Register adds a rule to the registry after computing its digest and validating constraints (Finding 9: deep copy).
func (reg *RuleRegistry) Register(r Rule) error {
	if err := r.Validate(); err != nil {
		return err
	}

	// Compute or verify content digest
	hasher := sha256.New()
	hasher.Write([]byte(r.Content))
	expectedDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if r.ContentDigest == "" {
		r.ContentDigest = expectedDigest
	} else if r.ContentDigest != expectedDigest {
		return errs.New(errs.CategoryInvalidArgument,
			"Rule %q: content_digest (%q) does not match sha256 of content (%q)", r.ID, r.ContentDigest, expectedDigest)
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()

	if reg.frozen {
		return errs.New(errs.CategoryConflict, "RuleRegistry is frozen; cannot register new rule")
	}

	if _, exists := reg.rules[r.ID]; exists {
		return errs.New(errs.CategoryConflict, "Rule %q already registered", r.ID)
	}

	// Add domains to known domains set
	for _, d := range r.Domains {
		clean := strings.ToLower(strings.TrimSpace(d))
		if clean != "" {
			reg.knownDomains[clean] = struct{}{}
		}
	}

	reg.rules[r.ID] = r.deepCopy()
	return nil
}

// Get retrieves a rule by ID (Finding 9: deep copy on egress).
func (reg *RuleRegistry) Get(id string) (Rule, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	r, ok := reg.rules[id]
	if !ok {
		return Rule{}, false
	}
	return r.deepCopy(), true
}

// All returns all registered rules sorted deterministically by ID (Finding 9: deep copy on egress).
func (reg *RuleRegistry) All() []Rule {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	result := make([]Rule, 0, len(reg.rules))
	for _, r := range reg.rules {
		result = append(result, r.deepCopy())
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// Freeze validates reverse-coverage and marks the registry immutable (Finding 2, 3).
func (reg *RuleRegistry) Freeze() error {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.frozen {
		return nil
	}
	if err := reg.validateReverseCoverageLocked(); err != nil {
		return err
	}
	reg.frozen = true
	return nil
}

// IsFrozen reports whether the registry has been frozen.
func (reg *RuleRegistry) IsFrozen() bool {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.frozen
}

// ValidateReverseCoverage performs reverse-coverage validation (ADR-0020 §2, DCI-132).
// It verifies that EVERY registered mandatory clause has a deterministic admission path
// (i.e. is not orphaned).
func (reg *RuleRegistry) ValidateReverseCoverage() error {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.validateReverseCoverageLocked()
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
// Invariants enforced (Findings 1, 2, 3, 9):
// - Enforces reverse-coverage validation on the rule registry.
// - Unknown required domains fail closed with errs.CategoryInvalidArgument.
// - Capability exclusion requires typed revision-pinned decision (CapabilityExclusion).
// - Contradictory "active + excluded" fails closed with errs.CategoryInvalidArgument.
// - Records explicit selection rationale for each admitted rule (SelectionRationale).
func (reg *RuleRegistry) ResolveAdmittedRules(params AdmissionParams) ([]Rule, error) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	// 1. Enforce reverse-coverage on the registry (Finding 2)
	if !reg.frozen {
		if err := reg.validateReverseCoverageLocked(); err != nil {
			return nil, err
		}
	}

	// 2. Validate domain vocabulary: unknown domains fail closed (Finding 2)
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

	// 3. Process active and excluded capabilities (Finding 1)
	activeCapsSet := make(map[string]bool, len(params.ActiveCapabilities))
	for _, c := range params.ActiveCapabilities {
		activeCapsSet[strings.ToLower(strings.TrimSpace(c))] = true
	}

	// Excluded capabilities must have typed provenance
	exclusions := make(map[string]CapabilityExclusion)
	for _, excl := range params.CapabilityExclusions {
		if err := excl.Validate(); err != nil {
			return nil, err
		}
		capLower := strings.ToLower(strings.TrimSpace(excl.Capability))
		// Fail closed on contradictory active + excluded
		if activeCapsSet[capLower] {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"contradictory capability %q is both active and excluded (decision %s)", excl.Capability, excl.DecisionID)
		}
		exclusions[capLower] = excl
	}

	// If legacy unprovenanced ExcludedCapabilities strings are supplied, verify they have typed decisions or fail closed
	if len(params.ExcludedCapabilities) > 0 {
		for _, capName := range params.ExcludedCapabilities {
			capLower := strings.ToLower(strings.TrimSpace(capName))
			if activeCapsSet[capLower] {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"contradictory capability %q is both active and excluded", capName)
			}
			if _, hasTyped := exclusions[capLower]; !hasTyped {
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
			capLower := strings.ToLower(rule.Capability)
			if _, isExcluded := exclusions[capLower]; isExcluded {
				continue
			}
			if activeCapsSet[capLower] {
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

	// Deterministic sorting by ID and attaching rationale (Finding 3, 9)
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

// CompileRequest defines all parameters necessary to compile a ContextManifest and ContextPack.
type CompileRequest struct {
	TaskID                string
	WorkPackageID         string
	WorkPackageRevision   int
	WorkPackageDigest     string
	Role                  string
	BaseCommit            string
	CandidateCommit       *string
	ProjectStateRevision  string
	MappingVersion        string
	SourceRevision        string
	ReadEnvelope          []string
	WriteScope            []string
	Domains               []string
	RiskTags              []string
	Action                string
	ActiveCapabilities    []string
	CapabilityExclusions  []CapabilityExclusion
	ExcludedCapabilities  []string
	Tools                 []string
	AccessChannel         *protocol.AccessChannel
	ExplicitRuleIDs       []string
	ExecutionContract     string
	Assumptions           []protocol.Assumption
	ExplicitQuestions     []string
	ExpansionTriggers     []string
	ContextProfile        *protocol.ContextProfile
	BudgetPoolID          string
	RecentToolExchanges   []string
	CandidateDiffManifest *string
	ValidationSummaries   []string
	ActiveLeaseIDs        []string
	Renderer              PromptRenderer
	ToolSchemas           []string
	HostFraming           string
}

// Compiler executes the Cognitive Invocation Compiler pipeline (ADR-0020 §2, PROTOCOLS §10B).
type Compiler struct {
	registry   *RuleRegistry
	leaseMgr   *EvidenceLeaseManager
	capsuleMgr *CapsuleManager
}

// NewCompiler constructs a Compiler with required registries and managers.
func NewCompiler(registry *RuleRegistry, leaseMgr *EvidenceLeaseManager, capsuleMgr *CapsuleManager) *Compiler {
	return &Compiler{
		registry:   registry,
		leaseMgr:   leaseMgr,
		capsuleMgr: capsuleMgr,
	}
}

// Compile compiles a validated ContextManifest and ContextPack.
// If the compiled pack or rendered prompt projection exceeds profile ceilings,
// it marks status as PackStatusContextUnfit and returns errs.CategoryContextUnfit
// without truncating mandatory requirements (DCI-019).
func (c *Compiler) Compile(ctx context.Context, req CompileRequest) (*protocol.ContextManifest, *protocol.ContextPack, error) {
	const kind = "CognitiveCompiler"

	// 1. Fail-closed request parameter validation (Finding 11)
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: task_id cannot be empty", kind)
	}
	if strings.TrimSpace(req.WorkPackageID) == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: work_package_id cannot be empty", kind)
	}
	if req.WorkPackageRevision < 1 {
		req.WorkPackageRevision = 1
	}
	if strings.TrimSpace(req.WorkPackageDigest) == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: work_package_digest cannot be empty", kind)
	}
	if !strings.HasPrefix(req.WorkPackageDigest, "sha256:") || len(req.WorkPackageDigest) != 71 {
		return nil, nil, errs.New(errs.CategoryInvalidArgument,
			"%s: work_package_digest must be sha256:<64 hex chars>, got %q", kind, req.WorkPackageDigest)
	}
	if strings.TrimSpace(req.Role) == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: role cannot be empty", kind)
	}
	if len(req.BaseCommit) < 7 {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: base_commit must be >= 7 characters, got %q", kind, req.BaseCommit)
	}
	if strings.TrimSpace(req.ExecutionContract) == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: execution_contract cannot be empty", kind)
	}
	if req.ContextProfile == nil {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: context_profile cannot be nil", kind)
	}
	if strings.TrimSpace(req.BudgetPoolID) == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: budget_pool_id cannot be empty", kind)
	}
	if strings.TrimSpace(req.MappingVersion) == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: mapping_version cannot be empty", kind)
	}
	if strings.TrimSpace(req.SourceRevision) == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: source_revision cannot be empty", kind)
	}
	if strings.TrimSpace(req.ProjectStateRevision) == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: project_state_revision cannot be empty", kind)
	}

	// Validate domain mapping presence
	for i, d := range req.Domains {
		if strings.TrimSpace(d) == "" {
			return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: domains[%d] cannot be empty", kind, i)
		}
	}

	// Derive active capabilities from channel and tool facts if present (Finding 1)
	if req.AccessChannel != nil || len(req.Tools) > 0 {
		req.ActiveCapabilities = DeriveActiveCapabilities(req.AccessChannel, req.Tools, req.ActiveCapabilities)
	}

	// 2. Deterministic rule admission & closure
	allPaths := append(append([]string(nil), req.ReadEnvelope...), req.WriteScope...)
	admittedRules, err := c.registry.ResolveAdmittedRules(AdmissionParams{
		Role:                 req.Role,
		Action:               req.Action,
		Domains:              req.Domains,
		RiskTags:             req.RiskTags,
		Paths:                allPaths,
		ActiveCapabilities:   req.ActiveCapabilities,
		CapabilityExclusions: req.CapabilityExclusions,
		ExcludedCapabilities: req.ExcludedCapabilities,
		ExplicitRuleIDs:      req.ExplicitRuleIDs,
	})
	if err != nil {
		return nil, nil, errs.Wrap(errs.CategoryOf(err), err, "%s: failed to resolve admitted rules", kind)
	}

	mandatoryRefs := make([]protocol.MandatoryClauseRef, len(admittedRules))
	normativeClausesText := make([]string, len(admittedRules))
	admittedObjectDigests := make(map[string]string)

	for i, r := range admittedRules {
		mandatoryRefs[i] = protocol.MandatoryClauseRef{
			ClauseID:           r.ID,
			SourceDoc:          r.SourceDoc,
			Revision:           r.Revision,
			ContentDigest:      r.ContentDigest,
			SelectionRationale: r.SelectionRationale,
		}
		normativeClausesText[i] = fmt.Sprintf("[%s] %s", r.ID, r.Content)
		admittedObjectDigests[r.ID] = r.ContentDigest
	}

	// 3. Assemble ContextManifest with selection provenance
	manifestID := fmt.Sprintf("manifest-%s-rev%d", req.TaskID, req.WorkPackageRevision)
	manifest := &protocol.ContextManifest{
		SchemaVersion:        protocol.SchemaVersion1,
		ManifestID:           manifestID,
		TaskID:               req.TaskID,
		WorkPackageID:        req.WorkPackageID,
		WorkPackageRevision:  req.WorkPackageRevision,
		WorkPackageDigest:    req.WorkPackageDigest,
		Role:                 req.Role,
		BaseCommit:           req.BaseCommit,
		CandidateCommit:      req.CandidateCommit,
		ProjectStateRevision: req.ProjectStateRevision,
		MappingVersion:       req.MappingVersion,
		SourceRevision:       req.SourceRevision,
		ReadEnvelope:         req.ReadEnvelope,
		WriteScope:           req.WriteScope,
		Domains:              req.Domains,
		RiskTags:             req.RiskTags,
		MandatoryClauses:     mandatoryRefs,
		InitialEvidenceRefs:  make([]string, 0),
		Assumptions:          req.Assumptions,
		ExplicitQuestions:    req.ExplicitQuestions,
		ExpansionTriggers:    req.ExpansionTriggers,
		AdmissionProvenance: []string{
			"compiler:deterministic_rule_admission_v1",
			fmt.Sprintf("mapping_version:%s", req.MappingVersion),
			fmt.Sprintf("source_revision:%s", req.SourceRevision),
		},
		ContextProfileID: req.ContextProfile.ProfileID,
		BudgetPoolID:     req.BudgetPoolID,
	}

	if err := manifest.Validate(); err != nil {
		return nil, nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: generated manifest failed validation", kind)
	}

	// 4. Gather Evidence Working Set leases with staleness and expiry rejection (Finding 7)
	evidenceWorkingSet := make([]protocol.EvidenceLease, 0)
	if c.leaseMgr != nil && len(req.ActiveLeaseIDs) > 0 {
		now := time.Now().UTC()
		for _, lid := range req.ActiveLeaseIDs {
			lease, ok := c.leaseMgr.GetLease(lid)
			if !ok {
				return nil, nil, errs.New(errs.CategoryNotFound, "%s: active lease %q not found", kind, lid)
			}
			// Reject released or invalidated leases
			if lease.Status != protocol.LeaseStatusActive {
				return nil, nil, errs.New(errs.CategoryValidationFailed,
					"%s: lease %q is not active (status: %q)", kind, lid, lease.Status)
			}
			// Reject expired leases
			if lease.ExpiresAt != nil && *lease.ExpiresAt != "" {
				expTime, err := time.Parse(time.RFC3339Nano, *lease.ExpiresAt)
				if err != nil {
					expTime, err = time.Parse(time.RFC3339, *lease.ExpiresAt)
				}
				if err == nil && now.After(expTime) {
					return nil, nil, errs.New(errs.CategoryValidationFailed,
						"%s: lease %q expired at %s", kind, lid, *lease.ExpiresAt)
				}
			}
			// Reject inconsistent source revision
			if lease.SourceRevision != "" && req.SourceRevision != "" && lease.SourceRevision != req.SourceRevision {
				return nil, nil, errs.New(errs.CategoryValidationFailed,
					"%s: lease %q source revision %q does not match compilation source revision %q",
					kind, lid, lease.SourceRevision, req.SourceRevision)
			}
			// Verify read authorization against read envelope
			if len(req.ReadEnvelope) > 0 && !IsPathAuthorized(lease.FilePath, req.ReadEnvelope) {
				return nil, nil, errs.New(errs.CategoryPolicyDenied,
					"%s: lease %q path %q is outside authorized read envelope", kind, lease.LeaseID, lease.FilePath)
			}
			evidenceWorkingSet = append(evidenceWorkingSet, lease)
			admittedObjectDigests[lease.LeaseID] = lease.ContentDigest
		}
	}
	sort.Slice(evidenceWorkingSet, func(i, j int) bool {
		return evidenceWorkingSet[i].LeaseID < evidenceWorkingSet[j].LeaseID
	})

	// 5. Gather Cognitive State Capsule with dependency validity check (Finding 7)
	var cognitiveState protocol.CognitiveStateCapsule
	if c.capsuleMgr != nil {
		cognitiveState = c.capsuleMgr.Snapshot()
		if c.leaseMgr != nil {
			for _, depID := range cognitiveState.EvidenceDependencies {
				depLease, ok := c.leaseMgr.GetLease(depID)
				if !ok {
					return nil, nil, errs.New(errs.CategoryValidationFailed,
						"%s: cognitive state references missing evidence lease %q", kind, depID)
				}
				if depLease.Status != protocol.LeaseStatusActive {
					return nil, nil, errs.New(errs.CategoryValidationFailed,
						"%s: cognitive state references stale/invalidated evidence lease %q (status: %q)",
						kind, depID, depLease.Status)
				}
			}
		}
	}

	// 6. Assemble Ephemeral Tail Block
	ephemeralTail := protocol.EphemeralTailBlock{
		RecentToolExchanges:   req.RecentToolExchanges,
		CandidateDiffManifest: req.CandidateDiffManifest,
		ValidationSummaries:   req.ValidationSummaries,
		CurrentAction:         req.Action,
	}
	if ephemeralTail.CurrentAction == "" {
		ephemeralTail.CurrentAction = fmt.Sprintf("Execute %s", req.WorkPackageID)
	}

	// 7. Token Accounting
	accountingMethod := req.ContextProfile.AccountingMethod
	uncertainty := req.ContextProfile.EstimateUncertaintyRatio

	roleCoreText := fmt.Sprintf("Role: %s. Operates strictly within execution bounds.", req.Role)
	roleTokens := EstimateTokens(roleCoreText, accountingMethod, uncertainty)
	contractTokens := EstimateTokens(req.ExecutionContract, accountingMethod, uncertainty)

	normativeTokens := 0
	for _, nc := range normativeClausesText {
		normativeTokens += EstimateTokens(nc, accountingMethod, uncertainty)
	}

	stateTokens := 0
	for _, h := range cognitiveState.Hypotheses {
		stateTokens += EstimateTokens(h, accountingMethod, uncertainty)
	}
	for _, t := range cognitiveState.ActiveTODOs {
		stateTokens += EstimateTokens(t, accountingMethod, uncertainty)
	}
	for _, d := range cognitiveState.IntermediateDecisions {
		stateTokens += EstimateTokens(d, accountingMethod, uncertainty)
	}
	for _, q := range cognitiveState.OpenQuestions {
		stateTokens += EstimateTokens(q, accountingMethod, uncertainty)
	}

	evidenceTokens := 0
	for _, l := range evidenceWorkingSet {
		evidenceTokens += l.TokenCount
	}

	tailTokens := EstimateTokens(ephemeralTail.CurrentAction, accountingMethod, uncertainty)
	for _, ex := range ephemeralTail.RecentToolExchanges {
		tailTokens += EstimateTokens(ex, accountingMethod, uncertainty)
	}
	if ephemeralTail.CandidateDiffManifest != nil {
		tailTokens += EstimateTokens(*ephemeralTail.CandidateDiffManifest, accountingMethod, uncertainty)
	}
	for _, vs := range ephemeralTail.ValidationSummaries {
		tailTokens += EstimateTokens(vs, accountingMethod, uncertainty)
	}

	outputReserveTokens := req.ContextProfile.OutputReserveTokens
	totalResidentTokens := roleTokens + contractTokens + normativeTokens + stateTokens + evidenceTokens + tailTokens

	tokenAccounting := protocol.TokenAccountingBreakdown{
		RoleTokens:          roleTokens,
		ContractTokens:      contractTokens,
		NormativeTokens:     normativeTokens,
		StateTokens:         stateTokens,
		EvidenceTokens:      evidenceTokens,
		TailTokens:          tailTokens,
		OutputReserveTokens: outputReserveTokens,
		TotalResidentTokens: totalResidentTokens,
		AccountingMethod:    accountingMethod,
	}

	// 8. Compute True Invocation Identity Digest (Finding 10)
	hasher := sha256.New()
	hasher.Write([]byte(manifestID))
	hasher.Write([]byte(roleCoreText))
	hasher.Write([]byte(req.ExecutionContract))
	for _, nc := range normativeClausesText {
		hasher.Write([]byte(nc))
	}
	for _, h := range cognitiveState.Hypotheses {
		hasher.Write([]byte("hyp:" + h))
	}
	for _, t := range cognitiveState.ActiveTODOs {
		hasher.Write([]byte("todo:" + t))
	}
	for _, d := range cognitiveState.IntermediateDecisions {
		hasher.Write([]byte("dec:" + d))
	}
	for _, q := range cognitiveState.OpenQuestions {
		hasher.Write([]byte("q:" + q))
	}
	for _, dep := range cognitiveState.EvidenceDependencies {
		hasher.Write([]byte("dep:" + dep))
	}
	for _, l := range evidenceWorkingSet {
		hasher.Write([]byte(l.LeaseID + ":" + l.ContentDigest))
	}
	hasher.Write([]byte("action:" + ephemeralTail.CurrentAction))
	for _, ex := range ephemeralTail.RecentToolExchanges {
		hasher.Write([]byte("ex:" + ex))
	}
	if ephemeralTail.CandidateDiffManifest != nil {
		hasher.Write([]byte("diff:" + *ephemeralTail.CandidateDiffManifest))
	}
	for _, vs := range ephemeralTail.ValidationSummaries {
		hasher.Write([]byte("val:" + vs))
	}
	hasher.Write([]byte("profile:" + req.ContextProfile.ProfileID))
	hasher.Write([]byte("budget:" + req.BudgetPoolID))

	packDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	admittedObjectDigests["manifest"] = req.WorkPackageDigest

	packID := fmt.Sprintf("pack-%s-%s", req.TaskID, packDigest[7:19])
	coverageSummary := fmt.Sprintf("Admitted %d mandatory clauses with 100%% deterministic closure", len(admittedRules))

	pack := &protocol.ContextPack{
		SchemaVersion:         protocol.SchemaVersion1,
		PackID:                packID,
		ManifestID:            manifestID,
		ManifestRevision:      req.WorkPackageRevision,
		RoleCore:              roleCoreText,
		ExecutionContract:     req.ExecutionContract,
		NormativeClauses:      normativeClausesText,
		CognitiveState:        cognitiveState,
		EvidenceWorkingSet:    evidenceWorkingSet,
		EphemeralTail:         ephemeralTail,
		TokenAccounting:       tokenAccounting,
		AdmittedObjectDigests: admittedObjectDigests,
		PackDigest:            packDigest,
		CoverageSummary:       coverageSummary,
		Status:                protocol.PackStatusReady,
	}

	// 9. Enforce Abstract ContextProfile Bounds (DCI-019)
	boundsErr := EnforceProfileBounds(pack, req.ContextProfile)
	if boundsErr != nil {
		pack.Status = protocol.PackStatusContextUnfit
		return manifest, pack, boundsErr
	}

	// 10. Enforce Final Endpoint Prompt Projection Bounds (Finding 4, PROTOCOLS §10B)
	renderer := req.Renderer
	if renderer == nil {
		renderer = NewTaggedMarkdownRenderer()
	}
	projection, err := renderer.Render(pack)
	if err != nil {
		return nil, nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to render prompt projection for bounds checking", kind)
	}

	additionalTokens := 0
	for _, ts := range req.ToolSchemas {
		additionalTokens += EstimateTokens(ts, accountingMethod, uncertainty)
	}
	if req.HostFraming != "" {
		additionalTokens += EstimateTokens(req.HostFraming, accountingMethod, uncertainty)
	}

	if projErr := EnforceProjectionBounds(projection, req.ContextProfile, additionalTokens); projErr != nil {
		pack.Status = protocol.PackStatusContextUnfit
		return manifest, pack, projErr
	}

	// 11. Validate complete pack
	if err := pack.Validate(); err != nil {
		return nil, nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: generated context pack failed validation", kind)
	}

	return manifest, pack, nil
}

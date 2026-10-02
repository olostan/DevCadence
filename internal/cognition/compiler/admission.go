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
	ID             string         `json:"id"`
	AdmissionClass AdmissionClass `json:"admission_class"`
	SourceDoc      string         `json:"source_doc"`
	Revision       string         `json:"revision"`
	Content        string         `json:"content"`
	ContentDigest  string         `json:"content_digest"`
	Capability     string         `json:"capability,omitempty"`    // Required if class is capability_default (e.g., "write", "network", "credentials")
	Domains        []string       `json:"domains,omitempty"`       // Mapped domains
	RiskTags       []string       `json:"risk_tags,omitempty"`     // Mapped risk tags
	Roles          []string       `json:"roles,omitempty"`         // Mapped roles
	Actions        []string       `json:"actions,omitempty"`       // Mapped actions
	PathPatterns   []string       `json:"path_patterns,omitempty"` // Mapped file paths / globs
	DependsOn      []string       `json:"depends_on,omitempty"`    // Mandatory dependency edges
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

// RuleRegistry stores and validates the canonical corpus of normative rules.
type RuleRegistry struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

// NewRuleRegistry creates an empty RuleRegistry.
func NewRuleRegistry() *RuleRegistry {
	return &RuleRegistry{
		rules: make(map[string]Rule),
	}
}

// Register adds a rule to the registry after computing its digest and validating constraints.
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

	if _, exists := reg.rules[r.ID]; exists {
		return errs.New(errs.CategoryConflict, "Rule %q already registered", r.ID)
	}
	reg.rules[r.ID] = r
	return nil
}

// Get retrieves a rule by ID.
func (reg *RuleRegistry) Get(id string) (Rule, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	r, ok := reg.rules[id]
	return r, ok
}

// All returns all registered rules sorted deterministically by ID.
func (reg *RuleRegistry) All() []Rule {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	result := make([]Rule, 0, len(reg.rules))
	for _, r := range reg.rules {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// ValidateReverseCoverage performs reverse-coverage validation (ADR-0020 §2, DCI-132).
// It verifies that EVERY registered mandatory clause has a deterministic admission path
// (i.e. is not orphaned). An orphan rule cannot be reached via 'always', 'capability_default',
// direct mappings, or transitive dependency closure.
func (reg *RuleRegistry) ValidateReverseCoverage() error {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

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
	ExcludedCapabilities []string
	ExplicitRuleIDs      []string
}

// ResolveAdmittedRules deterministically admits rules matching criteria and computes
// full dependency closure over all transitive dependencies (ADR-0020 §2, DCI-132).
func (reg *RuleRegistry) ResolveAdmittedRules(params AdmissionParams) ([]Rule, error) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	admittedMap := make(map[string]Rule)

	// Set lookups
	domainsSet := make(map[string]bool, len(params.Domains))
	for _, d := range params.Domains {
		domainsSet[strings.ToLower(strings.TrimSpace(d))] = true
	}
	riskTagsSet := make(map[string]bool, len(params.RiskTags))
	for _, r := range params.RiskTags {
		riskTagsSet[strings.ToLower(strings.TrimSpace(r))] = true
	}
	activeCapsSet := make(map[string]bool, len(params.ActiveCapabilities))
	for _, c := range params.ActiveCapabilities {
		activeCapsSet[strings.ToLower(strings.TrimSpace(c))] = true
	}
	excludedCapsSet := make(map[string]bool, len(params.ExcludedCapabilities))
	for _, c := range params.ExcludedCapabilities {
		excludedCapsSet[strings.ToLower(strings.TrimSpace(c))] = true
	}

	targetRole := strings.ToLower(strings.TrimSpace(params.Role))
	targetAction := strings.ToLower(strings.TrimSpace(params.Action))

	// 1. Initial admission pass
	for id, rule := range reg.rules {
		switch rule.AdmissionClass {
		case AdmissionClassAlways:
			admittedMap[id] = rule
		case AdmissionClassCapabilityDefault:
			capLower := strings.ToLower(rule.Capability)
			if activeCapsSet[capLower] && !excludedCapsSet[capLower] {
				admittedMap[id] = rule
			}
		case AdmissionClassMapped:
			// Check role match
			matched := false
			for _, r := range rule.Roles {
				if strings.ToLower(r) == targetRole {
					matched = true
					break
				}
			}
			// Check action match
			if !matched && targetAction != "" {
				for _, a := range rule.Actions {
					if strings.ToLower(a) == targetAction {
						matched = true
						break
					}
				}
			}
			// Check domain match
			if !matched {
				for _, d := range rule.Domains {
					if domainsSet[strings.ToLower(d)] {
						matched = true
						break
					}
				}
			}
			// Check risk tag match
			if !matched {
				for _, rt := range rule.RiskTags {
					if riskTagsSet[strings.ToLower(rt)] {
						matched = true
						break
					}
				}
			}
			// Check path matches
			if !matched && len(params.Paths) > 0 {
				for _, p := range params.Paths {
					if matchPathPatterns(p, rule.PathPatterns) {
						matched = true
						break
					}
				}
			}

			if matched {
				admittedMap[id] = rule
			}
		}
	}

	// 2. Explicit rule inclusions (fail-closed if missing)
	for _, explicitID := range params.ExplicitRuleIDs {
		rule, ok := reg.rules[explicitID]
		if !ok {
			return nil, errs.New(errs.CategoryNotFound, "explicit mandatory rule %q not registered in rule registry", explicitID)
		}
		admittedMap[explicitID] = rule
	}

	// 3. Dependency closure loop (fail-closed if dependency is missing)
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
					changed = true
				}
			}
		}
	}

	// Deterministic sorting by ID
	result := make([]Rule, 0, len(admittedMap))
	for _, r := range admittedMap {
		result = append(result, r)
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
	ExcludedCapabilities  []string
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
// If the compiled pack exceeds profile ceilings, it marks status as PackStatusContextUnfit
// and returns errs.CategoryContextUnfit without truncating mandatory requirements (DCI-019).
func (c *Compiler) Compile(ctx context.Context, req CompileRequest) (*protocol.ContextManifest, *protocol.ContextPack, error) {
	const kind = "CognitiveCompiler"

	// 1. Fail-closed request parameter validation
	if req.TaskID == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: task_id cannot be empty", kind)
	}
	if req.WorkPackageID == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: work_package_id cannot be empty", kind)
	}
	if req.WorkPackageRevision < 1 {
		req.WorkPackageRevision = 1
	}
	if req.WorkPackageDigest == "" {
		// Provide default empty sha256 if not specified
		req.WorkPackageDigest = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	}
	if req.Role == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: role cannot be empty", kind)
	}
	if len(req.BaseCommit) < 7 {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: base_commit must be >= 7 characters, got %q", kind, req.BaseCommit)
	}
	if req.ExecutionContract == "" {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: execution_contract cannot be empty", kind)
	}
	if req.ContextProfile == nil {
		return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: context_profile cannot be nil", kind)
	}
	if req.BudgetPoolID == "" {
		req.BudgetPoolID = "default_pool"
	}
	if req.MappingVersion == "" {
		req.MappingVersion = "v1.0"
	}
	if req.SourceRevision == "" {
		req.SourceRevision = req.BaseCommit
	}
	if req.ProjectStateRevision == "" {
		req.ProjectStateRevision = "rev-initial"
	}

	// Validate domain mapping presence
	for i, d := range req.Domains {
		if strings.TrimSpace(d) == "" {
			return nil, nil, errs.New(errs.CategoryInvalidArgument, "%s: domains[%d] cannot be empty", kind, i)
		}
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
			ClauseID:      r.ID,
			SourceDoc:     r.SourceDoc,
			Revision:      r.Revision,
			ContentDigest: r.ContentDigest,
		}
		normativeClausesText[i] = fmt.Sprintf("[%s] %s", r.ID, r.Content)
		admittedObjectDigests[r.ID] = r.ContentDigest
	}

	// 3. Assemble ContextManifest
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
		AdmissionProvenance:  []string{"compiler:deterministic_rule_admission_v1"},
		ContextProfileID:     req.ContextProfile.ProfileID,
		BudgetPoolID:         req.BudgetPoolID,
	}

	if err := manifest.Validate(); err != nil {
		return nil, nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: generated manifest failed validation", kind)
	}

	// 4. Gather Evidence Working Set leases
	evidenceWorkingSet := make([]protocol.EvidenceLease, 0)
	if c.leaseMgr != nil && len(req.ActiveLeaseIDs) > 0 {
		for _, lid := range req.ActiveLeaseIDs {
			lease, ok := c.leaseMgr.GetLease(lid)
			if !ok {
				return nil, nil, errs.New(errs.CategoryNotFound, "%s: active lease %q not found", kind, lid)
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

	// 5. Gather Cognitive State Capsule
	var cognitiveState protocol.CognitiveStateCapsule
	if c.capsuleMgr != nil {
		cognitiveState = c.capsuleMgr.Snapshot()
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

	// 8. Compute Pack Digest
	hasher := sha256.New()
	hasher.Write([]byte(manifestID))
	hasher.Write([]byte(roleCoreText))
	hasher.Write([]byte(req.ExecutionContract))
	for _, nc := range normativeClausesText {
		hasher.Write([]byte(nc))
	}
	for _, l := range evidenceWorkingSet {
		hasher.Write([]byte(l.ContentDigest))
	}
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

	// 9. Enforce ContextProfile Bounds (DCI-019)
	boundsErr := EnforceProfileBounds(pack, req.ContextProfile)
	if boundsErr != nil {
		pack.Status = protocol.PackStatusContextUnfit
		return manifest, pack, boundsErr
	}

	// 10. Validate complete pack
	if err := pack.Validate(); err != nil {
		return nil, nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: generated context pack failed validation", kind)
	}

	return manifest, pack, nil
}

package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// RuleRegistry stores and validates the canonical corpus of normative rules.
type RuleRegistry struct {
	mu           sync.RWMutex
	rules        map[string]Rule
	knownDomains map[string]struct{}
	frozen       bool

	catalogID                 string
	catalogVersion            string
	sourceRevision            string
	mappingRevision           string
	normativeSourceDigest     string
	authorityProjectionDigest string
	catalogDigest             string
}

// CatalogID returns the frozen catalog identifier.
func (reg *RuleRegistry) CatalogID() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.catalogID
}

// CatalogVersion returns the frozen catalog specification version.
func (reg *RuleRegistry) CatalogVersion() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.catalogVersion
}

// SourceRevision returns the git commit revision of the normative source document (e.g. INVARIANTS.md).
func (reg *RuleRegistry) SourceRevision() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.sourceRevision
}

// MappingRevision returns the revision of the canonical invariant applicability mappings.
func (reg *RuleRegistry) MappingRevision() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.mappingRevision
}

// CatalogRevision returns the git commit revision the catalog is pinned to (backwards-compatible alias for SourceRevision).
func (reg *RuleRegistry) CatalogRevision() string {
	return reg.SourceRevision()
}

// NormativeSourceDigest returns the cryptographic SHA-256 digest of all verbatim normative clauses.
func (reg *RuleRegistry) NormativeSourceDigest() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.normativeSourceDigest
}

// AuthorityProjectionDigest returns the cryptographic SHA-256 digest of all mapping semantics.
func (reg *RuleRegistry) AuthorityProjectionDigest() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.authorityProjectionDigest
}

// MappingDigest is an alias for AuthorityProjectionDigest.
func (reg *RuleRegistry) MappingDigest() string {
	return reg.AuthorityProjectionDigest()
}

// CatalogDigest returns the cryptographic SHA-256 digest authenticating both normative source and authority projection.
func (reg *RuleRegistry) CatalogDigest() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.catalogDigest
}

// SetCatalogMeta records catalog provenance metadata before the registry is frozen.
func (reg *RuleRegistry) SetCatalogMeta(id, version, sourceRevision, mappingRevision string) error {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.frozen {
		return errs.New(errs.CategoryConflict, "RuleRegistry is frozen; cannot set catalog meta")
	}
	reg.catalogID = id
	reg.catalogVersion = version
	reg.sourceRevision = sourceRevision
	reg.mappingRevision = mappingRevision
	return nil
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

// Freeze validates reverse-coverage, computes deterministic cryptographic digests
// (NormativeSourceDigest, AuthorityProjectionDigest, CatalogDigest), and marks the registry immutable (Finding 1, 2, 3).
func (reg *RuleRegistry) Freeze() error {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.frozen {
		return nil
	}
	if err := reg.validateReverseCoverageLocked(); err != nil {
		return err
	}

	ids := make([]string, 0, len(reg.rules))
	for id := range reg.rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// 1. NormativeSourceDigest: authenticates exact normative clause text & content digests
	srcHasher := sha256.New()
	for _, id := range ids {
		r := reg.rules[id]
		srcHasher.Write([]byte(r.ID + ":" + r.ContentDigest + "\n"))
	}
	reg.normativeSourceDigest = "sha256:" + hex.EncodeToString(srcHasher.Sum(nil))

	// 2. AuthorityProjectionDigest: authenticates exact mapping semantics (admission class, capabilities, domains, roles, etc.)
	// and incorporates the catalog mapping revision (Pass 4 Finding 2).
	projHasher := sha256.New()
	projHasher.Write([]byte("mapping_revision:" + reg.mappingRevision + "\n"))
	for _, id := range ids {
		r := reg.rules[id]
		projHasher.Write([]byte(canonicalRuleMappingSerialization(r) + "\n"))
	}
	reg.authorityProjectionDigest = "sha256:" + hex.EncodeToString(projHasher.Sum(nil))

	// 3. CatalogDigest: cryptographically authenticates BOTH normative source and authority projection
	catHasher := sha256.New()
	catHasher.Write([]byte(reg.normativeSourceDigest + ":" + reg.authorityProjectionDigest))
	reg.catalogDigest = "sha256:" + hex.EncodeToString(catHasher.Sum(nil))

	reg.frozen = true
	return nil
}

func canonicalRuleMappingSerialization(r Rule) string {
	domains := append([]string(nil), r.Domains...)
	sort.Strings(domains)
	roles := append([]string(nil), r.Roles...)
	sort.Strings(roles)
	risks := append([]string(nil), r.RiskTags...)
	sort.Strings(risks)
	actions := append([]string(nil), r.Actions...)
	sort.Strings(actions)
	paths := append([]string(nil), r.PathPatterns...)
	sort.Strings(paths)
	deps := append([]string(nil), r.DependsOn...)
	sort.Strings(deps)

	return fmt.Sprintf("id=%s|kind=%s|class=%s|cap=%s|domains=%s|roles=%s|risks=%s|actions=%s|paths=%s|deps=%s|rev=%s",
		r.ID,
		r.SourceKind,
		r.AdmissionClass,
		r.Capability,
		strings.Join(domains, ","),
		strings.Join(roles, ","),
		strings.Join(risks, ","),
		strings.Join(actions, ","),
		strings.Join(paths, ","),
		strings.Join(deps, ","),
		r.Revision,
	)
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

	// 3. Process active and excluded capabilities (Finding 1, Pass 4 Finding 1)
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
	RoleCore              string
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
	DeclaredTools         []ToolCapabilityInfo
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
// It strictly requires a non-nil, frozen RuleRegistry with validated provenance (Finding 3).
func NewCompiler(registry *RuleRegistry, leaseMgr *EvidenceLeaseManager, capsuleMgr *CapsuleManager) (*Compiler, error) {
	const kind = "CognitiveCompiler"
	if registry == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: rule registry cannot be nil", kind)
	}
	if !registry.IsFrozen() {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: rule registry must be frozen before compiler construction", kind)
	}
	if strings.TrimSpace(registry.CatalogID()) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: rule registry has empty catalog_id", kind)
	}
	if strings.TrimSpace(registry.CatalogRevision()) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: rule registry has empty catalog_revision", kind)
	}
	if strings.TrimSpace(registry.CatalogDigest()) == "" || !strings.HasPrefix(registry.CatalogDigest(), "sha256:") || len(registry.CatalogDigest()) != 71 {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: rule registry has invalid or empty catalog_digest %q", kind, registry.CatalogDigest())
	}
	return &Compiler{
		registry:   registry,
		leaseMgr:   leaseMgr,
		capsuleMgr: capsuleMgr,
	}, nil
}

// CompiledInvocation represents the complete, immutable result of compiling a ContextPack
// along with its exact endpoint projection and invocation identity (Finding 4, ADR-0020 §5).
type CompiledInvocation struct {
	Manifest         *protocol.ContextManifest `json:"manifest"`
	Pack             *protocol.ContextPack     `json:"pack"`
	Projection       PromptProjection          `json:"projection"`
	InvocationDigest string                    `json:"invocation_digest"`
}

// Compile compiles a validated ContextManifest and ContextPack.
// If the compiled pack or rendered prompt projection exceeds profile ceilings,
// it marks status as PackStatusContextUnfit and returns errs.CategoryContextUnfit
// without truncating mandatory requirements (DCI-019).
func (c *Compiler) Compile(ctx context.Context, req CompileRequest) (*protocol.ContextManifest, *protocol.ContextPack, error) {
	inv, err := c.CompileInvocation(ctx, req)
	if err != nil {
		if inv != nil {
			return inv.Manifest, inv.Pack, err
		}
		return nil, nil, err
	}
	return inv.Manifest, inv.Pack, nil
}

// CompileInvocation compiles a ContextPack and returns the complete, immutable CompiledInvocation
// containing the manifest, pack, exact rendered prompt projection, and InvocationDigest (Finding 4).
func (c *Compiler) CompileInvocation(ctx context.Context, req CompileRequest) (*CompiledInvocation, error) {
	const kind = "CognitiveCompiler"

	// 1. Fail-closed request parameter validation (Finding 11, Finding 8)
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: task_id cannot be empty", kind)
	}
	if strings.TrimSpace(req.WorkPackageID) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: work_package_id cannot be empty", kind)
	}
	if req.WorkPackageRevision < 1 {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: work_package_revision must be >= 1, got %d", kind, req.WorkPackageRevision)
	}
	if strings.TrimSpace(req.WorkPackageDigest) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: work_package_digest cannot be empty", kind)
	}
	if !strings.HasPrefix(req.WorkPackageDigest, "sha256:") || len(req.WorkPackageDigest) != 71 {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"%s: work_package_digest must be sha256:<64 hex chars>, got %q", kind, req.WorkPackageDigest)
	}
	if strings.TrimSpace(req.Role) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: role cannot be empty", kind)
	}
	if len(req.BaseCommit) < 7 {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: base_commit must be >= 7 characters, got %q", kind, req.BaseCommit)
	}
	if strings.TrimSpace(req.ExecutionContract) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: execution_contract cannot be empty", kind)
	}
	if req.ContextProfile == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: context_profile cannot be nil", kind)
	}
	if strings.TrimSpace(req.BudgetPoolID) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: budget_pool_id cannot be empty", kind)
	}
	if strings.TrimSpace(req.ProjectStateRevision) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: project_state_revision cannot be empty", kind)
	}

	// Validate domain mapping presence
	for i, d := range req.Domains {
		if strings.TrimSpace(d) == "" {
			return nil, errs.New(errs.CategoryInvalidArgument, "%s: domains[%d] cannot be empty", kind, i)
		}
	}

	// Authority & catalog provenance derived from frozen registry (Finding 2, Pass 4 Finding 2 & 3)
	catalogID := c.registry.CatalogID()
	catalogRevision := c.registry.CatalogRevision()
	mappingRevision := c.registry.MappingRevision()
	catalogDigest := c.registry.CatalogDigest()

	// MappingVersion is derived directly from registry, not caller-selected (Pass 4 Finding 3)
	mappingVersion := mappingRevision
	if strings.TrimSpace(req.MappingVersion) != "" && req.MappingVersion != mappingVersion {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"%s: request mapping_version %q does not match registry mapping_revision %q",
			kind, req.MappingVersion, mappingVersion)
	}

	// Strictly require explicit source_revision without falling back to catalog revision (Finding 2)
	if strings.TrimSpace(req.SourceRevision) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: source_revision cannot be empty", kind)
	}
	sourceRevision := req.SourceRevision

	// Validate tools and declared capabilities with 1:1 binding (Finding 5, Pass 4 Finding 1)
	hasSchemas := len(req.ToolSchemas) > 0
	hasDeclared := len(req.DeclaredTools) > 0
	hasLegacyTools := len(req.Tools) > 0

	if hasSchemas && hasLegacyTools {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"%s: request specifies both modern tool_schemas (%d) and legacy tools (%d); tool representations are mutually exclusive, fail closed",
			kind, len(req.ToolSchemas), len(req.Tools))
	}

	if hasSchemas {
		if !hasDeclared {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"%s: tool schemas provided (%d) but no typed ToolCapabilityInfo declarations provided; fail closed",
				kind, len(req.ToolSchemas))
		}
		schemaNames := make(map[string]int)
		for i, s := range req.ToolSchemas {
			sName, err := extractToolNameFromSchema(s)
			if err != nil {
				return nil, errs.Wrap(errs.CategoryInvalidArgument, err,
					"%s: tool_schemas[%d] failed name extraction", kind, i)
			}
			if _, exists := schemaNames[sName]; exists {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"%s: duplicate tool schema name %q in tool_schemas", kind, sName)
			}
			schemaNames[sName] = 1
		}

		declaredNames := make(map[string]int)
		for i, dt := range req.DeclaredTools {
			if err := dt.Validate(); err != nil {
				return nil, errs.Wrap(errs.CategoryInvalidArgument, err,
					"%s: declared_tools[%d] (%q) invalid", kind, i, dt.Name)
			}
			if _, exists := declaredNames[dt.Name]; exists {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"%s: duplicate tool declaration name %q in declared_tools", kind, dt.Name)
			}
			declaredNames[dt.Name] = 1
		}

		if len(schemaNames) != len(declaredNames) {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"%s: mismatch between tool schemas count (%d) and declared tools count (%d); 1:1 binding required",
				kind, len(schemaNames), len(declaredNames))
		}
		for sName := range schemaNames {
			if _, found := declaredNames[sName]; !found {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"%s: tool schema %q has no matching ToolCapabilityInfo declaration; fail closed", kind, sName)
			}
		}
		for dName := range declaredNames {
			if _, found := schemaNames[dName]; !found {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"%s: declared tool %q has no matching tool schema; fail closed", kind, dName)
			}
		}
	} else if hasLegacyTools {
		if !hasDeclared {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"%s: legacy tools provided (%d) but no typed ToolCapabilityInfo declarations provided; fail closed",
				kind, len(req.Tools))
		}
		legacyNames := make(map[string]int)
		for _, t := range req.Tools {
			if _, exists := legacyNames[t]; exists {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"%s: duplicate tool name %q in legacy tools", kind, t)
			}
			legacyNames[t] = 1
		}
		declaredNames := make(map[string]int)
		for i, dt := range req.DeclaredTools {
			if err := dt.Validate(); err != nil {
				return nil, errs.Wrap(errs.CategoryInvalidArgument, err,
					"%s: declared_tools[%d] (%q) invalid", kind, i, dt.Name)
			}
			if _, exists := declaredNames[dt.Name]; exists {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"%s: duplicate tool declaration name %q in declared_tools", kind, dt.Name)
			}
			declaredNames[dt.Name] = 1
		}
		if len(legacyNames) != len(declaredNames) {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"%s: mismatch between legacy tools count (%d) and declared tools count (%d); 1:1 binding required",
				kind, len(legacyNames), len(declaredNames))
		}
		for tName := range legacyNames {
			if _, found := declaredNames[tName]; !found {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"%s: legacy tool %q has no matching ToolCapabilityInfo declaration; fail closed", kind, tName)
			}
		}
		for dName := range declaredNames {
			if _, found := legacyNames[dName]; !found {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"%s: declared tool %q has no matching legacy tool; fail closed", kind, dName)
			}
		}
	} else if hasDeclared {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"%s: declared tools provided (%d) but neither ToolSchemas nor Tools are present in request; unbound declaration fails closed",
			kind, len(req.DeclaredTools))
	}

	derivedCaps, err := DeriveActiveCapabilities(req.AccessChannel, req.DeclaredTools, req.ActiveCapabilities)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: capability derivation failed", kind)
	}
	req.ActiveCapabilities = derivedCaps

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
		return nil, errs.Wrap(errs.CategoryOf(err), err, "%s: failed to resolve admitted rules", kind)
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
		MappingVersion:       mappingVersion,
		SourceRevision:       sourceRevision,
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
			fmt.Sprintf("catalog_id:%s", catalogID),
			fmt.Sprintf("catalog_revision:%s", catalogRevision),
			fmt.Sprintf("catalog_digest:%s", catalogDigest),
			fmt.Sprintf("mapping_version:%s", mappingVersion),
			fmt.Sprintf("source_revision:%s", sourceRevision),
		},
		ContextProfileID: req.ContextProfile.ProfileID,
		BudgetPoolID:     req.BudgetPoolID,
	}

	// 4. Gather Evidence Working Set leases with staleness and expiry rejection (Finding 7)
	evidenceWorkingSet := make([]protocol.EvidenceLease, 0)
	if c.leaseMgr != nil && len(req.ActiveLeaseIDs) > 0 {
		now := time.Now().UTC()
		for _, lid := range req.ActiveLeaseIDs {
			lease, ok := c.leaseMgr.GetLease(lid)
			if !ok {
				return nil, errs.New(errs.CategoryNotFound, "%s: active lease %q not found", kind, lid)
			}
			if err := validateLeaseFreshness(lease, sourceRevision, now, kind); err != nil {
				return nil, err
			}
			// Verify read authorization against read envelope
			if len(req.ReadEnvelope) > 0 && !IsPathAuthorized(lease.FilePath, req.ReadEnvelope) {
				return nil, errs.New(errs.CategoryPolicyDenied,
					"%s: lease %q path %q is outside authorized read envelope", kind, lease.LeaseID, lease.FilePath)
			}
			evidenceWorkingSet = append(evidenceWorkingSet, lease)
			admittedObjectDigests[lease.LeaseID] = lease.ContentDigest
			manifest.InitialEvidenceRefs = append(manifest.InitialEvidenceRefs, lease.LeaseID)
		}
	}
	sort.Slice(evidenceWorkingSet, func(i, j int) bool {
		return evidenceWorkingSet[i].LeaseID < evidenceWorkingSet[j].LeaseID
	})

	if err := manifest.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: generated manifest failed validation", kind)
	}

	// 5. Gather Cognitive State Capsule with dependency freshness check (Finding 7)
	var cognitiveState protocol.CognitiveStateCapsule
	if c.capsuleMgr != nil {
		cognitiveState = c.capsuleMgr.Snapshot()
		if c.leaseMgr != nil {
			now := time.Now().UTC()
			for _, depID := range cognitiveState.EvidenceDependencies {
				depLease, ok := c.leaseMgr.GetLease(depID)
				if !ok {
					return nil, errs.New(errs.CategoryValidationFailed,
						"%s: cognitive state references missing evidence lease %q", kind, depID)
				}
				if err := validateLeaseFreshness(depLease, sourceRevision, now, fmt.Sprintf("%s: cognitive state", kind)); err != nil {
					return nil, err
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

	// 7. Token Accounting (Finding 3, Finding 5)
	accountingMethod := req.ContextProfile.AccountingMethod
	uncertainty := req.ContextProfile.EstimateUncertaintyRatio

	if accountingMethod != protocol.AccountingApproximateEstimate {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"%s: profile accounting method %q requires a verified tokenizer engine; only %q is supported by the heuristic compiler estimator",
			kind, accountingMethod, protocol.AccountingApproximateEstimate)
	}

	roleCoreText := req.RoleCore
	if strings.TrimSpace(roleCoreText) == "" {
		roleCoreText = CanonicalRoleCore(req.Role)
	}

	roleTokens := EstimateTokensApprox(roleCoreText, uncertainty)
	contractTokens := EstimateTokensApprox(req.ExecutionContract, uncertainty)

	normativeTokens := 0
	for _, nc := range normativeClausesText {
		normativeTokens += EstimateTokensApprox(nc, uncertainty)
	}

	stateTokens := 0
	for _, h := range cognitiveState.Hypotheses {
		stateTokens += EstimateTokensApprox(h, uncertainty)
	}
	for _, t := range cognitiveState.ActiveTODOs {
		stateTokens += EstimateTokensApprox(t, uncertainty)
	}
	for _, d := range cognitiveState.IntermediateDecisions {
		stateTokens += EstimateTokensApprox(d, uncertainty)
	}
	for _, q := range cognitiveState.OpenQuestions {
		stateTokens += EstimateTokensApprox(q, uncertainty)
	}

	evidenceTokens := 0
	for _, l := range evidenceWorkingSet {
		evidenceTokens += l.TokenCount
	}

	tailTokens := EstimateTokensApprox(ephemeralTail.CurrentAction, uncertainty)
	for _, ex := range ephemeralTail.RecentToolExchanges {
		tailTokens += EstimateTokensApprox(ex, uncertainty)
	}
	if ephemeralTail.CandidateDiffManifest != nil {
		tailTokens += EstimateTokensApprox(*ephemeralTail.CandidateDiffManifest, uncertainty)
	}
	for _, vs := range ephemeralTail.ValidationSummaries {
		tailTokens += EstimateTokensApprox(vs, uncertainty)
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

	// 8. Compute ContextPackDigest (Finding 5: semantic pack state identity)
	admittedObjectDigests["manifest"] = req.WorkPackageDigest

	packDigest, err := ComputeContextPackDigest(&protocol.ContextPack{
		ManifestID:            manifestID,
		ManifestRevision:      req.WorkPackageRevision,
		RoleCore:              roleCoreText,
		ExecutionContract:     req.ExecutionContract,
		NormativeClauses:      normativeClausesText,
		CognitiveState:        cognitiveState,
		EvidenceWorkingSet:    evidenceWorkingSet,
		EphemeralTail:         ephemeralTail,
		AdmittedObjectDigests: admittedObjectDigests,
	})
	if err != nil {
		return nil, err
	}

	packID := fmt.Sprintf("pack-%s-%s", req.TaskID, packDigest[7:19])
	coverageSummary := fmt.Sprintf("Admitted %d mandatory clauses from %d total catalog invariants with 100%% deterministic reverse coverage", len(admittedRules), len(c.registry.rules))

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
		return &CompiledInvocation{Manifest: manifest, Pack: pack}, boundsErr
	}

	// 10. Enforce Final Endpoint Prompt Projection Bounds (Finding 4, PROTOCOLS §10B)
	renderer := req.Renderer
	if renderer == nil {
		renderer = NewTaggedMarkdownRenderer()
	}
	projection, err := renderer.Render(pack)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to render prompt projection for bounds checking", kind)
	}

	additionalTokens := 0
	for _, ts := range req.ToolSchemas {
		additionalTokens += EstimateTokensApprox(ts, uncertainty)
	}
	if req.HostFraming != "" {
		additionalTokens += EstimateTokensApprox(req.HostFraming, uncertainty)
	}

	if projErr := EnforceProjectionBounds(projection, req.ContextProfile, additionalTokens); projErr != nil {
		pack.Status = protocol.PackStatusContextUnfit
		return &CompiledInvocation{Manifest: manifest, Pack: pack, Projection: projection}, projErr
	}

	// 11. Compute InvocationDigest (Finding 5: true endpoint invocation identity, Pass 4 Finding 2)
	invocationDigest, err := ComputeInvocationDigest(
		pack.PackDigest,
		renderer.Format(),
		projection.SystemPrompt,
		projection.UserPrompt,
		req.ToolSchemas,
		req.HostFraming,
		catalogID,
		catalogRevision,
		mappingRevision,
		catalogDigest,
		req.ContextProfile,
	)
	if err != nil {
		return nil, err
	}
	pack.InvocationDigest = invocationDigest

	// 12. Validate complete pack
	if err := pack.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: generated context pack failed validation", kind)
	}

	return &CompiledInvocation{
		Manifest:         manifest,
		Pack:             pack,
		Projection:       projection,
		InvocationDigest: invocationDigest,
	}, nil
}

// CanonicalRoleCore returns the standard role core instructions for recognized roles.
func CanonicalRoleCore(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "principal_engineer":
		return "Role: Principal Engineer. Owns architectural integrity, problem models, work packaging, and systemic decisions. Operates under AGENTS.md, DCI-001..DCI-009, and PROTOCOLS §7/§10B."
	case "worker", "execution_worker":
		return "Role: Execution Worker. Executes authorized Engineering Work Package contracts within strict write and read envelopes. Operates under AGENTS.md, DCI-020..DCI-034, and PROTOCOLS §7."
	case "reviewer":
		return "Role: Reviewer. Provides independent, lens-specific review and verification. Does not author code or self-verify. Operates under AGENTS.md, DCI-040..DCI-049, and PROTOCOLS §8."
	default:
		return fmt.Sprintf("Role: %s. Operates strictly within execution bounds and applicable normative constraints.", role)
	}
}

// validateLeaseFreshness verifies that an evidence lease is active, not expired (strict RFC3339), and matches source revision.
func validateLeaseFreshness(lease protocol.EvidenceLease, expectedRevision string, now time.Time, contextMsg string) error {
	if lease.Status != protocol.LeaseStatusActive {
		return errs.New(errs.CategoryValidationFailed, "%s: lease %q is not active (status: %q)", contextMsg, lease.LeaseID, lease.Status)
	}
	if lease.ExpiresAt != nil && *lease.ExpiresAt != "" {
		expTime, err := time.Parse(time.RFC3339Nano, *lease.ExpiresAt)
		if err != nil {
			expTime, err = time.Parse(time.RFC3339, *lease.ExpiresAt)
		}
		if err != nil {
			return errs.New(errs.CategoryValidationFailed, "%s: lease %q has invalid RFC3339 expires_at %q: %v", contextMsg, lease.LeaseID, *lease.ExpiresAt, err)
		}
		if now.After(expTime) {
			return errs.New(errs.CategoryValidationFailed, "%s: lease %q expired at %s", contextMsg, lease.LeaseID, *lease.ExpiresAt)
		}
	}
	if lease.SourceRevision != "" && expectedRevision != "" && lease.SourceRevision != expectedRevision {
		return errs.New(errs.CategoryValidationFailed, "%s: lease %q source revision %q does not match expected revision %q", contextMsg, lease.LeaseID, lease.SourceRevision, expectedRevision)
	}
	return nil
}

// contextPackDigestInput defines the canonical typed semantic state for ContextPackDigest.
type contextPackDigestInput struct {
	ManifestID            string                         `json:"manifest_id"`
	ManifestRevision      int                            `json:"manifest_revision"`
	RoleCore              string                         `json:"role_core"`
	ExecutionContract     string                         `json:"execution_contract"`
	NormativeClauses      []string                       `json:"normative_clauses"`
	CognitiveState        protocol.CognitiveStateCapsule `json:"cognitive_state"`
	EvidenceWorkingSet    []protocol.EvidenceLease       `json:"evidence_working_set"`
	EphemeralTail         protocol.EphemeralTailBlock    `json:"ephemeral_tail"`
	AdmittedObjectDigests map[string]string              `json:"admitted_object_digests"`
}

// ComputeContextPackDigest computes the deterministic cryptographic hash of the typed semantic pack state.
func ComputeContextPackDigest(pack *protocol.ContextPack) (string, error) {
	input := contextPackDigestInput{
		ManifestID:            pack.ManifestID,
		ManifestRevision:      pack.ManifestRevision,
		RoleCore:              pack.RoleCore,
		ExecutionContract:     pack.ExecutionContract,
		NormativeClauses:      pack.NormativeClauses,
		CognitiveState:        pack.CognitiveState,
		EvidenceWorkingSet:    pack.EvidenceWorkingSet,
		EphemeralTail:         pack.EphemeralTail,
		AdmittedObjectDigests: pack.AdmittedObjectDigests,
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err, "failed to marshal context pack digest input")
	}
	hasher := sha256.New()
	hasher.Write(data)
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// invocationDigestInput defines the complete final endpoint projection state for InvocationDigest.
type invocationDigestInput struct {
	PackDigest      string   `json:"pack_digest"`
	RendererFormat  string   `json:"renderer_format"`
	SystemPrompt    string   `json:"system_prompt"`
	UserPrompt      string   `json:"user_prompt"`
	ToolSchemas     []string `json:"tool_schemas"`
	HostFraming     string   `json:"host_framing"`
	CatalogID       string   `json:"catalog_id"`
	CatalogRevision string   `json:"catalog_revision"`
	MappingRevision string   `json:"mapping_revision"`
	CatalogDigest   string   `json:"catalog_digest"`
	ProfileID       string   `json:"profile_id"`
	ProfileRevision int      `json:"profile_revision"`
	DeclaredWindow  int      `json:"declared_window_tokens"`
	RuntimeWindow   int      `json:"runtime_window_tokens"`
}

// ComputeInvocationDigest computes the deterministic cryptographic hash of the complete final endpoint projection.
func ComputeInvocationDigest(
	packDigest string,
	rendererFormat string,
	systemPrompt string,
	userPrompt string,
	toolSchemas []string,
	hostFraming string,
	catalogID string,
	catalogRevision string,
	mappingRevision string,
	catalogDigest string,
	profile *protocol.ContextProfile,
) (string, error) {
	sortedSchemas := make([]string, len(toolSchemas))
	copy(sortedSchemas, toolSchemas)
	sort.Strings(sortedSchemas)

	var profID string
	var profRev, declWin, runWin int
	if profile != nil {
		profID = profile.ProfileID
		profRev = profile.Revision
		declWin = profile.DeclaredWindowTokens
		runWin = profile.RuntimeWindowTokens
	}

	input := invocationDigestInput{
		PackDigest:      packDigest,
		RendererFormat:  rendererFormat,
		SystemPrompt:    systemPrompt,
		UserPrompt:      userPrompt,
		ToolSchemas:     sortedSchemas,
		HostFraming:     hostFraming,
		CatalogID:       catalogID,
		CatalogRevision: catalogRevision,
		MappingRevision: mappingRevision,
		CatalogDigest:   catalogDigest,
		ProfileID:       profID,
		ProfileRevision: profRev,
		DeclaredWindow:  declWin,
		RuntimeWindow:   runWin,
	}

	data, err := json.Marshal(input)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err, "failed to marshal invocation digest input")
	}
	hasher := sha256.New()
	hasher.Write(data)
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

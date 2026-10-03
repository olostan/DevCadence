package compiler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

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
// It strictly requires a non-nil, frozen RuleRegistry with validated provenance.
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
// along with its exact endpoint projection and invocation identity (ADR-0020 §5).
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

// CompileInvocation orchestrates the deterministic Cognitive Invocation Compiler pipeline,
// producing an immutable CompiledInvocation containing manifest, pack, endpoint projection,
// and InvocationDigest.
func (c *Compiler) CompileInvocation(ctx context.Context, req CompileRequest) (*CompiledInvocation, error) {
	const kind = "CognitiveCompiler"

	// Stage 1: Request parameter & catalog provenance validation
	sourceRevision, mappingVersion, err := validateCompileRequest(req, c.registry)
	if err != nil {
		return nil, err
	}

	// Stage 2: Tool capability binding & canonical effect derivation
	derivedCaps, err := validateToolBindingsAndDeriveCapabilities(&req)
	if err != nil {
		return nil, err
	}
	req.ActiveCapabilities = derivedCaps

	// Stage 3: Deterministic rule admission & dependency closure
	admittedRules, mandatoryRefs, normativeClausesText, admittedObjectDigests, err := admitMandatoryRules(c.registry, req)
	if err != nil {
		return nil, err
	}

	// Stage 4: Assemble ContextManifest
	manifest := assembleContextManifest(req, c.registry, mappingVersion, sourceRevision, mandatoryRefs)

	// Stage 5: Gather Evidence Working Set leases with freshness and read scope checks
	evidenceNow := time.Now().UTC()
	evidenceWorkingSet, err := collectEvidenceWorkingSet(c.leaseMgr, req.ActiveLeaseIDs, req.ReadEnvelope, sourceRevision, evidenceNow, admittedObjectDigests, manifest)
	if err != nil {
		return nil, err
	}

	if err := manifest.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: generated manifest failed validation", kind)
	}

	// Stage 6: Gather Cognitive State Capsule with dependency freshness checks
	cognitiveNow := time.Now().UTC()
	cognitiveState, err := collectCognitiveState(c.capsuleMgr, c.leaseMgr, sourceRevision, cognitiveNow)
	if err != nil {
		return nil, err
	}

	// Stage 7: Assemble Ephemeral Tail Block
	ephemeralTail := assembleEphemeralTailBlock(req)

	// Stage 8: Assemble ContextPack & enforce abstract profile bounds
	roleCoreText := req.RoleCore
	if strings.TrimSpace(roleCoreText) == "" {
		roleCoreText = CanonicalRoleCore(req.Role)
	}

	c.registry.mu.RLock()
	totalCatalogInvariants := len(c.registry.rules)
	c.registry.mu.RUnlock()

	pack, err := assembleContextPack(req, manifest.ManifestID, roleCoreText, normativeClausesText, cognitiveState, evidenceWorkingSet, ephemeralTail, admittedObjectDigests, len(admittedRules), totalCatalogInvariants)
	if err != nil {
		return nil, err
	}

	if boundsErr := EnforceProfileBounds(pack, req.ContextProfile); boundsErr != nil {
		pack.Status = protocol.PackStatusContextUnfit
		return &CompiledInvocation{Manifest: manifest, Pack: pack}, boundsErr
	}

	// Stage 9: Render endpoint PromptProjection & enforce final projection bounds
	projection, renderer, err := renderAndEnforceProjection(req, pack, req.ContextProfile.EstimateUncertaintyRatio)
	if err != nil {
		if errors.Is(err, errs.ErrContextUnfit) {
			pack.Status = protocol.PackStatusContextUnfit
			return &CompiledInvocation{Manifest: manifest, Pack: pack, Projection: projection}, err
		}
		return nil, err
	}

	// Stage 10: Compute InvocationDigest & validate completed pack
	invocationDigest, err := ComputeInvocationDigest(
		pack.PackDigest,
		renderer.Format(),
		projection.SystemPrompt,
		projection.UserPrompt,
		req.ToolSchemas,
		req.HostFraming,
		c.registry.CatalogID(),
		c.registry.CatalogRevision(),
		c.registry.MappingRevision(),
		c.registry.CatalogDigest(),
		req.ContextProfile,
	)
	if err != nil {
		return nil, err
	}
	pack.InvocationDigest = invocationDigest

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

// validateCompileRequest validates fail-closed request requirements and matches registry provenance.
func validateCompileRequest(req CompileRequest, reg *RuleRegistry) (sourceRevision string, mappingVersion string, err error) {
	const kind = "CognitiveCompiler"

	if strings.TrimSpace(req.TaskID) == "" {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: task_id cannot be empty", kind)
	}
	if strings.TrimSpace(req.WorkPackageID) == "" {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: work_package_id cannot be empty", kind)
	}
	if req.WorkPackageRevision < 1 {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: work_package_revision must be >= 1, got %d", kind, req.WorkPackageRevision)
	}
	if strings.TrimSpace(req.WorkPackageDigest) == "" {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: work_package_digest cannot be empty", kind)
	}
	if !strings.HasPrefix(req.WorkPackageDigest, "sha256:") || len(req.WorkPackageDigest) != 71 {
		return "", "", errs.New(errs.CategoryInvalidArgument,
			"%s: work_package_digest must be sha256:<64 hex chars>, got %q", kind, req.WorkPackageDigest)
	}
	if strings.TrimSpace(req.Role) == "" {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: role cannot be empty", kind)
	}
	if len(req.BaseCommit) < 7 {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: base_commit must be >= 7 characters, got %q", kind, req.BaseCommit)
	}
	if strings.TrimSpace(req.ExecutionContract) == "" {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: execution_contract cannot be empty", kind)
	}
	if req.ContextProfile == nil {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: context_profile cannot be nil", kind)
	}
	if strings.TrimSpace(req.BudgetPoolID) == "" {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: budget_pool_id cannot be empty", kind)
	}
	if strings.TrimSpace(req.ProjectStateRevision) == "" {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: project_state_revision cannot be empty", kind)
	}

	for i, d := range req.Domains {
		if strings.TrimSpace(d) == "" {
			return "", "", errs.New(errs.CategoryInvalidArgument, "%s: domains[%d] cannot be empty", kind, i)
		}
	}

	mappingVersion = reg.MappingRevision()
	if strings.TrimSpace(req.MappingVersion) != "" && req.MappingVersion != mappingVersion {
		return "", "", errs.New(errs.CategoryInvalidArgument,
			"%s: request mapping_version %q does not match registry mapping_revision %q",
			kind, req.MappingVersion, mappingVersion)
	}

	if strings.TrimSpace(req.SourceRevision) == "" {
		return "", "", errs.New(errs.CategoryInvalidArgument, "%s: source_revision cannot be empty", kind)
	}
	sourceRevision = req.SourceRevision

	return sourceRevision, mappingVersion, nil
}

// validateToolBindingsAndDeriveCapabilities ensures 1:1 binding between schemas/tools and declarations,
// and derives active capabilities.
func validateToolBindingsAndDeriveCapabilities(req *CompileRequest) ([]string, error) {
	const kind = "CognitiveCompiler"

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
		schemaNames := make(map[string]struct{}, len(req.ToolSchemas))
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
			schemaNames[sName] = struct{}{}
		}

		declaredNames, err := validateDeclaredTools(req.DeclaredTools, kind)
		if err != nil {
			return nil, err
		}
		if err := verifyOneToOneToolBinding(kind, "tool schema", "tool schemas", schemaNames, declaredNames); err != nil {
			return nil, err
		}
	} else if hasLegacyTools {
		if !hasDeclared {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"%s: legacy tools provided (%d) but no typed ToolCapabilityInfo declarations provided; fail closed",
				kind, len(req.Tools))
		}
		legacyNames := make(map[string]struct{}, len(req.Tools))
		for _, t := range req.Tools {
			if _, exists := legacyNames[t]; exists {
				return nil, errs.New(errs.CategoryInvalidArgument,
					"%s: duplicate tool name %q in legacy tools", kind, t)
			}
			legacyNames[t] = struct{}{}
		}

		declaredNames, err := validateDeclaredTools(req.DeclaredTools, kind)
		if err != nil {
			return nil, err
		}
		if err := verifyOneToOneToolBinding(kind, "legacy tool", "legacy tools", legacyNames, declaredNames); err != nil {
			return nil, err
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
	return derivedCaps, nil
}

func validateDeclaredTools(declared []ToolCapabilityInfo, kind string) (map[string]struct{}, error) {
	declaredNames := make(map[string]struct{}, len(declared))
	for i, dt := range declared {
		if err := dt.Validate(); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err,
				"%s: declared_tools[%d] (%q) invalid", kind, i, dt.Name)
		}
		if _, exists := declaredNames[dt.Name]; exists {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"%s: duplicate tool declaration name %q in declared_tools", kind, dt.Name)
		}
		declaredNames[dt.Name] = struct{}{}
	}
	return declaredNames, nil
}

func verifyOneToOneToolBinding(kind, toolLabel, countLabel string, toolNames, declaredNames map[string]struct{}) error {
	if len(toolNames) != len(declaredNames) {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: mismatch between %s count (%d) and declared tools count (%d); 1:1 binding required",
			kind, countLabel, len(toolNames), len(declaredNames))
	}
	for tName := range toolNames {
		if _, found := declaredNames[tName]; !found {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: %s %q has no matching ToolCapabilityInfo declaration; fail closed", kind, toolLabel, tName)
		}
	}
	for dName := range declaredNames {
		if _, found := toolNames[dName]; !found {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: declared tool %q has no matching %s; fail closed", kind, dName, toolLabel)
		}
	}
	return nil
}

// admitMandatoryRules executes deterministic rule admission and dependency closure.
func admitMandatoryRules(reg *RuleRegistry, req CompileRequest) ([]Rule, []protocol.MandatoryClauseRef, []string, map[string]string, error) {
	const kind = "CognitiveCompiler"

	allPaths := append(append([]string(nil), req.ReadEnvelope...), req.WriteScope...)
	admittedRules, err := reg.ResolveAdmittedRules(AdmissionParams{
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
		return nil, nil, nil, nil, errs.Wrap(errs.CategoryOf(err), err, "%s: failed to resolve admitted rules", kind)
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

	return admittedRules, mandatoryRefs, normativeClausesText, admittedObjectDigests, nil
}

// assembleContextManifest constructs the ContextManifest with comprehensive admission provenance.
func assembleContextManifest(req CompileRequest, reg *RuleRegistry, mappingVersion, sourceRevision string, mandatoryRefs []protocol.MandatoryClauseRef) *protocol.ContextManifest {
	manifestID := fmt.Sprintf("manifest-%s-rev%d", req.TaskID, req.WorkPackageRevision)
	return &protocol.ContextManifest{
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
			fmt.Sprintf("catalog_id:%s", reg.CatalogID()),
			fmt.Sprintf("catalog_revision:%s", reg.CatalogRevision()),
			fmt.Sprintf("catalog_digest:%s", reg.CatalogDigest()),
			fmt.Sprintf("mapping_version:%s", mappingVersion),
			fmt.Sprintf("source_revision:%s", sourceRevision),
		},
		ContextProfileID: req.ContextProfile.ProfileID,
		BudgetPoolID:     req.BudgetPoolID,
	}
}

// collectEvidenceWorkingSet gathers and validates active evidence leases.
func collectEvidenceWorkingSet(leaseMgr *EvidenceLeaseManager, activeLeaseIDs []string, readEnvelope []string, sourceRevision string, now time.Time, admittedObjectDigests map[string]string, manifest *protocol.ContextManifest) ([]protocol.EvidenceLease, error) {
	const kind = "CognitiveCompiler"

	evidenceWorkingSet := make([]protocol.EvidenceLease, 0)
	if leaseMgr != nil && len(activeLeaseIDs) > 0 {
		for _, lid := range activeLeaseIDs {
			lease, ok := leaseMgr.GetLease(lid)
			if !ok {
				return nil, errs.New(errs.CategoryNotFound, "%s: active lease %q not found", kind, lid)
			}
			if err := validateLeaseFreshness(lease, sourceRevision, now, kind); err != nil {
				return nil, err
			}
			if len(readEnvelope) > 0 && !IsPathAuthorized(lease.FilePath, readEnvelope) {
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
	return evidenceWorkingSet, nil
}

// collectCognitiveState takes a snapshot of the cognitive state capsule and validates referenced dependencies.
func collectCognitiveState(capsuleMgr *CapsuleManager, leaseMgr *EvidenceLeaseManager, sourceRevision string, now time.Time) (protocol.CognitiveStateCapsule, error) {
	const kind = "CognitiveCompiler"

	var cognitiveState protocol.CognitiveStateCapsule
	if capsuleMgr != nil {
		cognitiveState = capsuleMgr.Snapshot()
		if leaseMgr != nil {
			for _, depID := range cognitiveState.EvidenceDependencies {
				depLease, ok := leaseMgr.GetLease(depID)
				if !ok {
					return cognitiveState, errs.New(errs.CategoryValidationFailed,
						"%s: cognitive state references missing evidence lease %q", kind, depID)
				}
				if err := validateLeaseFreshness(depLease, sourceRevision, now, fmt.Sprintf("%s: cognitive state", kind)); err != nil {
					return cognitiveState, err
				}
			}
		}
	}
	return cognitiveState, nil
}

// assembleEphemeralTailBlock packages ephemeral tool history and current action for the pack.
func assembleEphemeralTailBlock(req CompileRequest) protocol.EphemeralTailBlock {
	ephemeralTail := protocol.EphemeralTailBlock{
		RecentToolExchanges:   req.RecentToolExchanges,
		CandidateDiffManifest: req.CandidateDiffManifest,
		ValidationSummaries:   req.ValidationSummaries,
		CurrentAction:         req.Action,
	}
	if ephemeralTail.CurrentAction == "" {
		ephemeralTail.CurrentAction = fmt.Sprintf("Execute %s", req.WorkPackageID)
	}
	return ephemeralTail
}

// assembleContextPack calculates token accounting and packages the semantic ContextPack.
func assembleContextPack(
	req CompileRequest,
	manifestID string,
	roleCoreText string,
	normativeClausesText []string,
	cognitiveState protocol.CognitiveStateCapsule,
	evidenceWorkingSet []protocol.EvidenceLease,
	ephemeralTail protocol.EphemeralTailBlock,
	admittedObjectDigests map[string]string,
	admittedRulesCount int,
	totalCatalogInvariants int,
) (*protocol.ContextPack, error) {
	const kind = "CognitiveCompiler"

	accountingMethod := req.ContextProfile.AccountingMethod
	uncertainty := req.ContextProfile.EstimateUncertaintyRatio

	if accountingMethod != protocol.AccountingApproximateEstimate {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"%s: profile accounting method %q requires a verified tokenizer engine; only %q is supported by the heuristic compiler estimator",
			kind, accountingMethod, protocol.AccountingApproximateEstimate)
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
	coverageSummary := fmt.Sprintf("Admitted %d mandatory clauses from %d total catalog invariants with 100%% deterministic reverse coverage", admittedRulesCount, totalCatalogInvariants)

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

	return pack, nil
}

// renderAndEnforceProjection renders the prompt projection and enforces profile projection limits.
func renderAndEnforceProjection(req CompileRequest, pack *protocol.ContextPack, uncertainty float64) (PromptProjection, PromptRenderer, error) {
	const kind = "CognitiveCompiler"

	renderer := req.Renderer
	if renderer == nil {
		renderer = NewTaggedMarkdownRenderer()
	}
	projection, err := renderer.Render(pack)
	if err != nil {
		return PromptProjection{}, nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to render prompt projection for bounds checking", kind)
	}

	additionalTokens := 0
	for _, ts := range req.ToolSchemas {
		additionalTokens += EstimateTokensApprox(ts, uncertainty)
	}
	if req.HostFraming != "" {
		additionalTokens += EstimateTokensApprox(req.HostFraming, uncertainty)
	}

	if projErr := EnforceProjectionBounds(projection, req.ContextProfile, additionalTokens); projErr != nil {
		return projection, renderer, projErr
	}

	return projection, renderer, nil
}

// CanonicalRoleCore returns standard role core instructions for recognized roles.
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

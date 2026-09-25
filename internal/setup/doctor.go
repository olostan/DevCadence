package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/credentials"
	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/principalhosts"
	"github.com/olostan/DevCadence/internal/protocol"
)

// DiagnosticFinding codes
const (
	FindingCodeGitReady              = "GIT_READY"
	FindingCodeGitNotFound           = "GIT_NOT_FOUND"
	FindingCodeStateRootReady        = "STATE_ROOT_READY"
	FindingCodeStateRootUnwritable   = "STATE_ROOT_UNWRITABLE"
	FindingCodeStateDirsMissing      = "STATE_DIRS_MISSING"
	FindingCodeHardwareReady         = "HARDWARE_READY"
	FindingCodeAcceleratorUnverified = "ACCELERATOR_UNVERIFIED"
	FindingCodeAcceleratorVerified   = "ACCELERATOR_VERIFIED"
	FindingCodeEndpointReady         = "ENDPOINT_READY"
	FindingCodeEndpointUnhealthy     = "ENDPOINT_UNHEALTHY"
	FindingCodeAuthExpired           = "AUTH_EXPIRED"
	FindingCodeNoCodingEndpoint      = "NO_CODING_ENDPOINT"
)

// Severity constants
const (
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// DoctorOptions configures the Doctor diagnostic engine.
type DoctorOptions struct {
	Clock            clock.Clock
	IDs              ids.Source
	HomeDir          string
	CognitionService *cognition.Service
	VerifyEndpointID string // If non-empty, authorizes targeted inference probe for this endpoint only
	Cache            *CacheManager
	Policy           *cognition.Policy
	// CredentialManager checks the CredentialRefs below and reports
	// AuthEvidence for BuildResourceInventory's credentials section. Nil
	// is a supported state (no credential checking performed, same
	// nil-safe pattern CognitionService already uses) — WP-M3B-5.
	CredentialManager *credentials.Manager
	// CredentialRefs are the operator-configured references to check.
	// Doctor never invents credential references of its own.
	CredentialRefs []protocol.CredentialRef
}

// Doctor executes non-invasive diagnostic checks across the environment, state root,
// cognition endpoints, and principal hosts.
type Doctor struct {
	clock            clock.Clock
	ids              ids.Source
	homeDir          string
	cognitionService *cognition.Service
	recommender      *ProfileRecommender
	verifyEndpointID string
	cache            *CacheManager
	policy           *cognition.Policy
	credManager      *credentials.Manager
	credRefs         []protocol.CredentialRef
}

// NewDoctor returns a Doctor engine.
func NewDoctor(opts DoctorOptions) (*Doctor, error) {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	if opts.IDs == nil {
		opts.IDs = ids.NewULIDSource()
	}
	if opts.HomeDir == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "resolve user home dir")
		}
		opts.HomeDir = filepath.Join(h, ".devcadence")
	}
	cache := opts.Cache
	if cache == nil {
		c, err := NewCacheManager(filepath.Join(opts.HomeDir, "state"), opts.Clock, DefaultCacheTTL)
		if err == nil {
			cache = c
		}
	}
	policy := opts.Policy
	if policy == nil {
		defPolicy := cognition.DefaultPolicy()
		policy = &defPolicy
	}
	return &Doctor{
		clock:            opts.Clock,
		ids:              opts.IDs,
		homeDir:          opts.HomeDir,
		cognitionService: opts.CognitionService,
		recommender:      NewProfileRecommender(),
		verifyEndpointID: opts.VerifyEndpointID,
		cache:            cache,
		policy:           policy,
		credManager:      opts.CredentialManager,
		credRefs:         opts.CredentialRefs,
	}, nil
}

// Run executes the diagnostics and synthesizes a DoctorReport.
func (d *Doctor) Run(ctx context.Context, scope protocol.ReadinessEvaluationScope, facts protocol.EnvironmentFacts) (*protocol.DoctorReport, error) {
	var findings []protocol.DiagnosticFinding

	fingerprint, err := environment.Fingerprint(facts)
	if err != nil {
		fingerprint = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	}

	// 1. State root checks
	stateFindings := d.checkStateRoot()
	findings = append(findings, stateFindings...)

	// 2. Git check
	gitFindings := d.checkGit(facts)
	findings = append(findings, gitFindings...)

	// 3. Hardware / Accelerator checks
	hwFindings := d.checkHardware(facts)
	findings = append(findings, hwFindings...)

	// 4. Principal hosts check
	hosts, hostFindings := d.checkPrincipalHosts(facts)
	findings = append(findings, hostFindings...)

	// 5. Cognition endpoints discovery
	endpoints, endpointFindings, cognProfile, evidenceStatus, err := d.discoverEndpoints(ctx, facts, fingerprint)
	if err != nil {
		return nil, err
	}
	findings = append(findings, endpointFindings...)

	if scope.EvidenceStatus == "" {
		scope.EvidenceStatus = evidenceStatus
	}

	// 6. Profile recommendation (informational UX labels only; de-authorized from canonical readiness and routing)
	recommendation := d.recommender.Recommend(RecommendationInput{
		Facts:            facts,
		Endpoints:        endpoints,
		CognitionProfile: cognProfile,
		Policy:           d.policy,
	})

	// Default target profile for compatibility if not explicitly scoped
	if scope.TargetProfile == nil && recommendation.SelectedProfile != nil {
		scope.TargetProfile = recommendation.SelectedProfile
	}

	// 7. Evaluate Readiness
	readiness := d.evaluateReadiness(scope, findings, endpoints, cognProfile, scope.TargetProfile)

	// 8. Build ResourceInventory
	inv, err := d.BuildResourceInventory(ctx, facts, fingerprint, endpoints, hosts)
	if err != nil {
		return nil, err
	}

	scopeReadiness := protocol.EvaluateScopeReadiness(findings, endpoints, hosts)

	report := &protocol.DoctorReport{
		SchemaVersion:       protocol.SchemaVersion1,
		ReportID:            d.ids.New("doc"),
		MachineFingerprint:  fingerprint,
		ObservedAt:          protocol.NewTimestamp(d.clock.Now()),
		EvaluationScope:     scope,
		Readiness:           readiness,
		Findings:            findings,
		ScopeReadiness:      scopeReadiness,
		ResourceInventory:   inv,
		RecommendedProfile:  &recommendation,
		DiscoveredEndpoints: endpoints,
		PrincipalHosts:      hosts,
	}

	if err := report.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "doctor report validation failed")
	}

	return report, nil
}

// BuildResourceInventory projects the facts, discovered endpoints, and
// principal hosts a call to Run already gathered into a
// protocol.ResourceInventory, and additionally checks any configured
// CredentialRefs. It performs no new hardware/endpoint discovery of its
// own — it is a pure projection over data the caller supplies, so it can
// be called with Run's own outputs without re-probing anything
// (WP-M3B-5 §5.2).
//
// A nil credManager produces an empty Credentials section, the same
// nil-safe degradation discoverEndpoints already uses for a nil
// cognitionService.
func (d *Doctor) BuildResourceInventory(
	ctx context.Context,
	facts protocol.EnvironmentFacts,
	fingerprint string,
	endpoints []protocol.CognitionEndpointSummary,
	hosts []protocol.PrincipalHostSummary,
) (*protocol.ResourceInventory, error) {
	logicalCores := 0
	if facts.CPU.LogicalCores != nil {
		logicalCores = *facts.CPU.LogicalCores
	}
	var totalMem *int64
	if facts.Memory.TotalBytes != nil {
		totalMem = facts.Memory.TotalBytes
	}

	var backends []protocol.BackendKind
	for _, c := range environment.AssessBackends(facts) {
		if c.Support == protocol.SupportSupported {
			backends = append(backends, c.Backend)
		}
	}

	inv := &protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        d.ids.New("inv"),
		MachineFingerprint: fingerprint,
		ObservedAt:         protocol.NewTimestamp(d.clock.Now()),
		Hardware: protocol.HardwareSummary{
			OSFamily:            facts.Host.Family,
			Arch:                facts.Host.Arch,
			LogicalCores:        logicalCores,
			TotalMemoryBytes:    totalMem,
			AcceleratorBackends: backends,
		},
		CognitionEndpoints: endpoints,
		PrincipalHosts:     hosts,
	}

	var findings []protocol.DiagnosticFinding
	findings = append(findings, d.checkStateRoot()...)
	findings = append(findings, d.checkGit(facts)...)
	findings = append(findings, d.checkHardware(facts)...)
	inv.Readiness = protocol.EvaluateScopeReadiness(findings, endpoints, hosts)

	if d.credManager != nil {
		for _, ref := range d.credRefs {
			// CheckCredential errors only on a structurally malformed ref
			// or an unknown CredentialRefKind — never on "the credential
			// isn't there", which is already a valid
			// Unavailable/Unauthenticated AuthEvidence, not a Go error.
			// A malformed *configured* reference is a real bug in
			// DoctorOptions.CredentialRefs, so it propagates and fails
			// BuildResourceInventory rather than being papered over with
			// a fabricated evidence entry (fail closed, AGENTS.md §16;
			// DCI-104).
			evidence, err := d.credManager.CheckCredential(ctx, ref)
			if err != nil {
				return nil, errs.Wrap(errs.CategoryInvalidArgument, err,
					"resource inventory: configured credential reference %q is invalid", ref.RefID)
			}
			inv.Credentials = append(inv.Credentials, protocol.CredentialInventoryEntry{
				Ref:      ref,
				Evidence: evidence,
			})
		}
	}

	if d.policy != nil {
		inv.Policy = &protocol.PolicySummary{
			MaxSourceExposure: d.policy.MaxSourceExposure,
			MaxCostClass:      d.policy.MaxCostClass,
		}
	}

	if err := inv.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "resource inventory validation failed")
	}

	return inv, nil
}

func (d *Doctor) checkStateRoot() []protocol.DiagnosticFinding {
	var findings []protocol.DiagnosticFinding
	info, err := os.Stat(d.homeDir)
	if err != nil {
		if os.IsNotExist(err) {
			remed := fmt.Sprintf("Run 'devcadence setup' to initialize %s", d.homeDir)
			findings = append(findings, protocol.DiagnosticFinding{
				Category:    "state",
				Severity:    SeverityWarning,
				Code:        FindingCodeStateDirsMissing,
				Title:       "DevCadence home directory missing",
				Detail:      fmt.Sprintf("State root %s does not exist", d.homeDir),
				Remediation: &remed,
			})
			return findings
		}
		findings = append(findings, protocol.DiagnosticFinding{
			Category: "state",
			Severity: SeverityError,
			Code:     FindingCodeStateRootUnwritable,
			Title:    "DevCadence home inaccessible",
			Detail:   fmt.Sprintf("Failed to inspect %s: %v", d.homeDir, err),
		})
		return findings
	}

	if !info.IsDir() {
		findings = append(findings, protocol.DiagnosticFinding{
			Category: "state",
			Severity: SeverityError,
			Code:     FindingCodeStateRootUnwritable,
			Title:    "DevCadence home is not a directory",
			Detail:   fmt.Sprintf("%s exists but is not a directory", d.homeDir),
		})
		return findings
	}

	// Test write access via a temporary check file, removing it immediately
	f, err := os.CreateTemp(d.homeDir, ".devcadence_write_check_*")
	if err != nil {
		findings = append(findings, protocol.DiagnosticFinding{
			Category: "state",
			Severity: SeverityError,
			Code:     FindingCodeStateRootUnwritable,
			Title:    "DevCadence home unwritable",
			Detail:   fmt.Sprintf("Cannot write to %s: %v", d.homeDir, err),
		})
		return findings
	}
	tmpName := f.Name()
	_ = f.Close()
	_ = os.Remove(tmpName)

	// Check required subdirectories (canonical: state, artifacts/setup, tmp)
	type reqDir struct {
		name string
		path string
	}
	reqDirs := []reqDir{
		{name: "state", path: filepath.Join(d.homeDir, "state")},
		{name: "artifacts/setup", path: filepath.Join(d.homeDir, "artifacts", "setup")},
		{name: "tmp", path: filepath.Join(d.homeDir, "tmp")},
	}
	var missing []string
	for _, rd := range reqDirs {
		s, err := os.Stat(rd.path)
		if err != nil || !s.IsDir() {
			if rd.name == "artifacts/setup" {
				if s2, err2 := os.Stat(filepath.Join(d.homeDir, "artifacts_setup")); err2 == nil && s2.IsDir() {
					continue
				}
			}
			missing = append(missing, rd.name)
		}
	}
	if len(missing) > 0 {
		remed := "Run 'devcadence setup' to create managed directories"
		findings = append(findings, protocol.DiagnosticFinding{
			Category:    "state",
			Severity:    SeverityWarning,
			Code:        FindingCodeStateDirsMissing,
			Title:       "Managed state subdirectories missing",
			Detail:      fmt.Sprintf("Missing directories under %s: %v", d.homeDir, missing),
			Remediation: &remed,
		})
	} else {
		findings = append(findings, protocol.DiagnosticFinding{
			Category: "state",
			Severity: SeverityInfo,
			Code:     FindingCodeStateRootReady,
			Title:    "DevCadence home ready",
			Detail:   fmt.Sprintf("State root %s is writable with all required subdirectories", d.homeDir),
		})
	}

	return findings
}

func (d *Doctor) checkGit(facts protocol.EnvironmentFacts) []protocol.DiagnosticFinding {
	var findings []protocol.DiagnosticFinding
	gitFound := false
	var gitVer string
	foundInFacts := false
	for _, sw := range facts.Software {
		if sw.ID == "git" {
			foundInFacts = true
			if sw.Installed {
				gitFound = true
				gitVer = sw.Version
			}
			break
		}
	}
	if !foundInFacts {
		if p, err := exec.LookPath("git"); err == nil && p != "" {
			gitFound = true
			gitVer = "available on PATH"
		}
	}

	if gitFound {
		findings = append(findings, protocol.DiagnosticFinding{
			Category: "environment",
			Severity: SeverityInfo,
			Code:     FindingCodeGitReady,
			Title:    "Git installed",
			Detail:   fmt.Sprintf("Git version %s available", gitVer),
		})
	} else {
		remed := "Install Git via system package manager or Xcode Command Line Tools"
		findings = append(findings, protocol.DiagnosticFinding{
			Category:    "environment",
			Severity:    SeverityError,
			Code:        FindingCodeGitNotFound,
			Title:       "Git not found",
			Detail:      "Git is required for repository isolation and worktree management",
			Remediation: &remed,
		})
	}
	return findings
}

func (d *Doctor) checkHardware(facts protocol.EnvironmentFacts) []protocol.DiagnosticFinding {
	var findings []protocol.DiagnosticFinding
	candidates := environment.AssessBackends(facts)
	hasAccelerator := false
	for _, c := range candidates {
		if c.Support == protocol.SupportSupported && c.Backend != protocol.BackendCPU {
			hasAccelerator = true
			break
		}
	}
	if hasAccelerator {
		findings = append(findings, protocol.DiagnosticFinding{
			Category: "hardware",
			Severity: SeverityInfo,
			Code:     FindingCodeHardwareReady,
			Title:    "Hardware acceleration supported",
			Detail:   fmt.Sprintf("Accelerator backends assessed: %d viable", len(candidates)),
		})
	} else {
		findings = append(findings, protocol.DiagnosticFinding{
			Category: "hardware",
			Severity: SeverityWarning,
			Code:     FindingCodeAcceleratorUnverified,
			Title:    "No supported hardware accelerator candidate",
			Detail:   "Local inference will run on CPU unless remote cognition is configured",
		})
	}
	return findings
}

func (d *Doctor) checkPrincipalHosts(facts protocol.EnvironmentFacts) ([]protocol.PrincipalHostSummary, []protocol.DiagnosticFinding) {
	var summaries []protocol.PrincipalHostSummary
	var findings []protocol.DiagnosticFinding

	inv := principalhosts.FromEnvironment(facts)
	for _, h := range inv.Hosts {
		summaries = append(summaries, protocol.PrincipalHostSummary{
			HostID:    string(h.ID),
			Installed: h.Installed,
			Path:      h.Path,
			Version:   h.Version,
		})
	}
	return summaries, findings
}

func (d *Doctor) discoverEndpoints(ctx context.Context, facts protocol.EnvironmentFacts, fingerprint string) ([]protocol.CognitionEndpointSummary, []protocol.DiagnosticFinding, *protocol.MachineCapabilityProfile, string, error) {
	var summaries []protocol.CognitionEndpointSummary
	var findings []protocol.DiagnosticFinding

	if d.cognitionService == nil {
		return summaries, findings, nil, "live", nil
	}

	evidenceStatus := "live"
	depth := protocol.DepthHealth
	var inferenceTargets []string
	if d.verifyEndpointID != "" {
		depth = protocol.DepthInference
		inferenceTargets = []string{d.verifyEndpointID}
	}

	var cachedEnv CacheEnvelope[protocol.MachineCapabilityProfile]
	var cachedProfile *protocol.MachineCapabilityProfile
	cachedExpired := false
	if d.cache != nil {
		if env, found, expired, err := ReadEnvelope[protocol.MachineCapabilityProfile](ctx, d.cache, protocol.CacheTargetMachineProfile, fingerprint); err == nil && found {
			if valErr := env.Data.Validate(); valErr == nil {
				cachedEnv = env
				cachedProfile = &cachedEnv.Data
				cachedExpired = expired
			}
		}
	}

	profile, err := d.cognitionService.Profile(ctx, cognition.ProfileInput{
		Facts:            facts,
		Depth:            depth,
		InferenceTargets: inferenceTargets,
	})
	if err != nil {
		return nil, nil, nil, "", err
	}
	if err := profile.Validate(); err != nil {
		return nil, nil, nil, "", err
	}

	activeProfile := &profile
	usedCachedInference := false

	if cachedProfile != nil && depth < protocol.DepthInference {
		combined := profile
		combined.Endpoints = make([]protocol.CognitionEndpoint, len(profile.Endpoints))
		copy(combined.Endpoints, profile.Endpoints)

		cachedMap := make(map[string]protocol.CognitionEndpoint, len(cachedProfile.Endpoints))
		for _, cep := range cachedProfile.Endpoints {
			cachedMap[cep.ID] = cep
		}

		for i := range combined.Endpoints {
			ep := &combined.Endpoints[i]
			cep, ok := cachedMap[ep.ID]
			if !ok {
				continue
			}

			// Merge verified acceleration: preserve verified acceleration and verified_at
			if cep.AccelerationVerified() && !ep.AccelerationVerified() {
				ep.Acceleration = cep.Acceleration
				usedCachedInference = true
			}

			// Merge deeper capabilities from cached inference probe
			for _, ccap := range cep.Capabilities {
				found := false
				for j, fcap := range ep.Capabilities {
					if fcap.Dimension == ccap.Dimension {
						found = true
						if provenanceRank(fcap.Provenance) < provenanceRank(ccap.Provenance) {
							ep.Capabilities[j] = ccap
							usedCachedInference = true
						}
						break
					}
				}
				if !found {
					ep.Capabilities = append(ep.Capabilities, ccap)
					if ccap.Provenance == protocol.ProvenanceEvaluated || ccap.Provenance == protocol.ProvenanceMeasured {
						usedCachedInference = true
					}
				}
			}

			// Merge structured output / tool use if cached passed
			if ep.StructuredOutput != protocol.FeatureProbePassed && cep.StructuredOutput == protocol.FeatureProbePassed {
				ep.StructuredOutput = cep.StructuredOutput
				usedCachedInference = true
			}
			if ep.ToolUse != protocol.FeatureProbePassed && cep.ToolUse == protocol.FeatureProbePassed {
				ep.ToolUse = cep.ToolUse
				usedCachedInference = true
			}
			if ep.ContextTokens == nil && cep.ContextTokens != nil {
				ep.ContextTokens = cep.ContextTokens
			}
		}

		if usedCachedInference {
			// Profile with verified acceleration requires probe depth >= inference
			combined.ProbeDepth = protocol.DepthInference
			if err := combined.Validate(); err == nil {
				activeProfile = &combined
			} else {
				usedCachedInference = false
				activeProfile = &profile
			}
		}
	}

	if d.verifyEndpointID != "" {
		evidenceStatus = "live"
	} else if usedCachedInference {
		if cachedExpired {
			evidenceStatus = "stale_inference_retained"
		} else {
			evidenceStatus = "refreshed_health"
		}
	} else {
		evidenceStatus = "live"
	}

	// Durably cache the discovered machine profile according to probe depth:
	// - Full inference probes write fresh cache with DefaultCacheTTL.
	// - Health probes with unexpired cached inference write back preserving the original inference expiration.
	// - Health probes with expired cached inference DO NOT write to cache (never refresh stale inference).
	// - Shallow probes without cached inference write fresh health profile with DefaultCacheTTL.
	if d.cache != nil {
		if d.verifyEndpointID != "" {
			_ = Write(ctx, d.cache, protocol.CacheTargetMachineProfile, fingerprint, *activeProfile, DefaultCacheTTL)
		} else if usedCachedInference {
			if !cachedExpired {
				_ = WriteWithExpiresAt(ctx, d.cache, protocol.CacheTargetMachineProfile, fingerprint, *activeProfile, cachedEnv.ExpiresAt.Time())
			}
		} else {
			_ = Write(ctx, d.cache, protocol.CacheTargetMachineProfile, fingerprint, *activeProfile, DefaultCacheTTL)
		}
	}

	hasCoding := false
	for _, ep := range activeProfile.Endpoints {
		var backend *protocol.BackendKind
		isVerified := false
		if ep.Acceleration != nil {
			backend = &ep.Acceleration.Backend
			isVerified = ep.AccelerationVerified()
		}
		summary := protocol.CognitionEndpointSummary{
			ID:                     ep.ID,
			Kind:                   ep.Kind,
			Locality:               ep.Locality,
			Health:                 ep.Health,
			Auth:                   ep.Auth,
			CostClass:              ep.CostClass,
			RequiredSourceExposure: ep.RequiredSourceExposure,
			AccelerationVerified:   isVerified,
			AccelerationBackend:    backend,
		}
		summaries = append(summaries, summary)

		if ep.Health == protocol.EndpointHealthReady {
			hasCoding = true
			findings = append(findings, protocol.DiagnosticFinding{
				Category: "cognition",
				Severity: SeverityInfo,
				Code:     FindingCodeEndpointReady,
				Title:    fmt.Sprintf("Endpoint ready: %s", ep.ID),
				Detail:   fmt.Sprintf("Kind %s, Locality %s, Health %s", ep.Kind, ep.Locality, ep.Health),
			})
		} else if ep.Auth == protocol.AuthExpired {
			remed := fmt.Sprintf("Re-authenticate CLI or update API credentials for %s", ep.ID)
			findings = append(findings, protocol.DiagnosticFinding{
				Category:    "auth",
				Severity:    SeverityWarning,
				Code:        FindingCodeAuthExpired,
				Title:       fmt.Sprintf("Authentication expired: %s", ep.ID),
				Detail:      fmt.Sprintf("Endpoint %s auth status is expired", ep.ID),
				Remediation: &remed,
			})
		} else {
			findings = append(findings, protocol.DiagnosticFinding{
				Category: "cognition",
				Severity: SeverityWarning,
				Code:     FindingCodeEndpointUnhealthy,
				Title:    fmt.Sprintf("Endpoint unhealthy: %s", ep.ID),
				Detail:   fmt.Sprintf("Endpoint %s health is %s", ep.ID, ep.Health),
			})
		}
	}

	if !hasCoding {
		remed := "Configure an authenticated CLI, remote API key, or install Ollama with a coding model"
		findings = append(findings, protocol.DiagnosticFinding{
			Category:    "cognition",
			Severity:    SeverityWarning,
			Code:        FindingCodeNoCodingEndpoint,
			Title:       "No healthy coding endpoint found",
			Detail:      "DevCadence requires at least one healthy cognition endpoint for code execution",
			Remediation: &remed,
		})
	}

	return summaries, findings, activeProfile, evidenceStatus, nil
}

func provenanceRank(p protocol.CapabilityProvenance) int {
	switch p {
	case protocol.ProvenanceEvaluated:
		return 3
	case protocol.ProvenanceMeasured:
		return 2
	case protocol.ProvenanceConfigured:
		return 1
	default:
		return 0
	}
}

func parseCognitionRole(role string) (cognition.Role, bool) {
	switch strings.ToLower(role) {
	case "scout":
		return cognition.RoleScout, true
	case "classifier":
		return cognition.RoleClassifier, true
	case "implementer", "implementation", "principal":
		return cognition.RoleImplementer, true
	case "correctness_reviewer", "reviewer":
		return cognition.RoleCorrectnessReviewer, true
	case "architecture_reviewer":
		return cognition.RoleArchitectureReviewer, true
	default:
		return "", false
	}
}

func (d *Doctor) evaluateReadiness(
	scope protocol.ReadinessEvaluationScope,
	findings []protocol.DiagnosticFinding,
	endpoints []protocol.CognitionEndpointSummary,
	cognProfile *protocol.MachineCapabilityProfile,
	targetProfile *protocol.DeploymentProfile,
) protocol.ReadinessStatus {
	// 1. Mandatory base dependencies (Git, state root, disk space, errors)
	for _, f := range findings {
		if f.Severity == SeverityError {
			return protocol.ReadinessActionRequired
		}
	}

	// 2. Classify candidate endpoints using full CognitionEndpoints
	var fullEndpoints []protocol.CognitionEndpoint
	if cognProfile != nil && len(cognProfile.Endpoints) > 0 {
		fullEndpoints = cognProfile.Endpoints
	} else {
		fullEndpoints = summariesToEndpoints(endpoints)
	}

	var localEndpoints []protocol.CognitionEndpoint
	var acceleratedLocal []protocol.CognitionEndpoint
	var remoteEndpoints []protocol.CognitionEndpoint

	for _, ep := range fullEndpoints {
		if ep.Health != protocol.EndpointHealthReady {
			continue
		}
		if ep.Locality == protocol.LocalityLocal || ep.Kind == protocol.EndpointLocalRuntime {
			localEndpoints = append(localEndpoints, ep)
			if ep.AccelerationVerified() {
				acceleratedLocal = append(acceleratedLocal, ep)
			}
		}
		if (ep.Locality == protocol.LocalityRemote || ep.Kind == protocol.EndpointRemoteAPI || ep.Kind == protocol.EndpointAuthenticatedCLI) && (ep.Auth == protocol.AuthAuthenticated || ep.Auth == protocol.AuthNotApplicable) {
			remoteEndpoints = append(remoteEndpoints, ep)
		}
	}

	// 3. Verify target profile constraints if a target profile is specified
	if targetProfile != nil {
		switch *targetProfile {
		case protocol.ProfileCloudCognition:
			if len(remoteEndpoints) == 0 {
				return protocol.ReadinessPartiallyReady
			}
		case protocol.ProfileOffline:
			if len(localEndpoints) == 0 {
				return protocol.ReadinessPartiallyReady
			}
		case protocol.ProfileLocalHeavy:
			if len(localEndpoints) == 0 {
				return protocol.ReadinessPartiallyReady
			}
			if len(acceleratedLocal) == 0 {
				// Local-heavy requires verified hardware acceleration (ADR-0014: PARTIALLY_READY)
				return protocol.ReadinessPartiallyReady
			}
		case protocol.ProfileHybridThin:
			if len(localEndpoints) == 0 || len(remoteEndpoints) == 0 {
				return protocol.ReadinessPartiallyReady
			}
		case protocol.ProfileCustom:
			if len(localEndpoints) == 0 && len(remoteEndpoints) == 0 {
				return protocol.ReadinessPartiallyReady
			}
		}
	} else {
		// When no target profile is specified, canonical readiness requires at least one ready cognition path
		if len(localEndpoints) == 0 && len(remoteEndpoints) == 0 {
			return protocol.ReadinessPartiallyReady
		}
	}

	// 4. Verify required roles under routing policy and capability evidence
	defaultReqs := cognition.DefaultRequirements()
	effPolicy := cognition.DefaultPolicy()
	if d.policy != nil {
		effPolicy = *d.policy
	}

	for _, roleStr := range scope.RequiredRoles {
		cRole, ok := parseCognitionRole(roleStr)
		if !ok {
			// Unknown role string fails readiness
			return protocol.ReadinessPartiallyReady
		}

		req, ok := defaultReqs[cRole]
		if !ok {
			return protocol.ReadinessPartiallyReady
		}

		var candidateEndpoints []protocol.CognitionEndpoint
		if targetProfile != nil {
			switch *targetProfile {
			case protocol.ProfileOffline:
				candidateEndpoints = localEndpoints
			case protocol.ProfileLocalHeavy:
				candidateEndpoints = localEndpoints
			case protocol.ProfileCloudCognition:
				candidateEndpoints = remoteEndpoints
			case protocol.ProfileHybridThin:
				switch cRole {
				case cognition.RoleScout, cognition.RoleClassifier:
					candidateEndpoints = localEndpoints
				case cognition.RoleImplementer, cognition.RoleArchitectureReviewer:
					candidateEndpoints = remoteEndpoints
				default:
					candidateEndpoints = append(append([]protocol.CognitionEndpoint(nil), localEndpoints...), remoteEndpoints...)
				}
			case protocol.ProfileCustom:
				candidateEndpoints = append(append([]protocol.CognitionEndpoint(nil), localEndpoints...), remoteEndpoints...)
			}
		} else {
			candidateEndpoints = append(append([]protocol.CognitionEndpoint(nil), localEndpoints...), remoteEndpoints...)
		}

		decision := cognition.Route(req, effPolicy, candidateEndpoints)
		if decision.Outcome != cognition.OutcomeSelected {
			return protocol.ReadinessPartiallyReady
		}
	}

	// 6. Evidence freshness: stale inference evidence limits readiness
	if scope.EvidenceStatus == "stale_inference_retained" {
		return protocol.ReadinessReadyWithReducedCap
	}

	// 7. Any warnings report reduced capability
	for _, f := range findings {
		if f.Severity == SeverityWarning {
			return protocol.ReadinessReadyWithReducedCap
		}
	}

	return protocol.ReadinessReady
}

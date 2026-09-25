package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	FindingCodeAuthUnauthenticated   = "AUTH_UNAUTHENTICATED"
	FindingCodeAuthUnknown           = "AUTH_UNKNOWN"
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
	// EndpointCredentialRefs is an explicit, operator-configured binding
	// from a discovered CognitionEndpoint's ID to the CredentialRef.RefID
	// (among CredentialRefs above) that verifies its authentication. Doctor
	// applies this when constructing CognitionEndpointSummary.CredentialRef
	// instead of ever guessing a binding from the endpoint ID itself — the
	// coding-CLI adapter deliberately never invents a CredentialRef, so
	// without an explicit binding here (or one a cognition adapter itself
	// declared), a discovered CLI endpoint's CredentialRef stays empty and
	// Planner will not generate a machine-verifiable endpoint_authenticated
	// remediation for it (independent-review follow-up on WP-M3B-5,
	// round-4 finding 1).
	EndpointCredentialRefs map[string]string
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
	endpointCredRefs map[string]string
	// credRefIndex is the set of RefIDs among credRefs, computed once in
	// NewDoctor (which already validated every entry structurally and every
	// endpointCredRefs value against it). discoverEndpoints uses it to
	// decide whether an adapter-declared CognitionEndpoint.CredentialRef is
	// safe to treat as machine-verifiable.
	credRefIndex map[string]bool
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

	// The endpoint->CredentialRef binding must be referentially valid, not
	// only syntactically valid: an EndpointCredentialRefs value that looks
	// like a well-formed RefID but names no actually-configured
	// CredentialRef would let Planner generate an endpoint_authenticated
	// condition the production checker can never resolve — the exact
	// "impossible plan" shape earlier rounds eliminated for the guessed-
	// locator case, reintroduced here via a typo'd binding instead
	// (independent-review follow-up on WP-M3B-5, round-5 finding 1).
	refIndex := make(map[string]bool, len(opts.CredentialRefs))
	for _, ref := range opts.CredentialRefs {
		if err := ref.Validate(); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "NewDoctor: DoctorOptions.CredentialRefs contains an invalid entry")
		}
		refIndex[ref.RefID] = true
	}
	for endpointID, refID := range opts.EndpointCredentialRefs {
		if !refIndex[refID] {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"NewDoctor: EndpointCredentialRefs[%q] names credential_ref_id %q, which is not among the configured CredentialRefs", endpointID, refID)
		}
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
		endpointCredRefs: opts.EndpointCredentialRefs,
		credRefIndex:     refIndex,
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

	// scope.TargetProfile is used exactly as the caller passed it — never
	// defaulted from recommendation.SelectedProfile. RecommendedProfile is
	// an informational UX label; silently promoting it here would let a
	// label the caller never chose gate canonical readiness, exactly the
	// authority ADR-0014 §92 forbids (independent-review follow-up on
	// WP-M3B-5, finding 1).

	// 7. Evaluate Readiness
	readiness := d.evaluateReadiness(scope, findings, endpoints, cognProfile, scope.TargetProfile)

	// 8. Build ResourceInventory — computed from exactly the facts/findings/
	// endpoints/hosts/profile this single Run call already gathered above,
	// never by re-probing (independent-review follow-up on WP-M3B-5,
	// finding 3). scopeReadiness is computed once and shared between the
	// report and the inventory so the two can never disagree (finding 4d).
	scopeReadiness := protocol.EvaluateScopeReadiness(findings, endpoints, hosts)
	inv, err := d.BuildResourceInventory(ctx, facts, fingerprint, endpoints, hosts, cognProfile, scopeReadiness)
	if err != nil {
		return nil, err
	}

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

// ObserveCredentials gathers CredentialInventoryEntry items for the Doctor's
// configured CredentialRefs. It is an observational phase that performs IO via
// the CredentialManager, separating live evidence gathering from the pure
// ProjectResourceInventory projection.
func (d *Doctor) ObserveCredentials(ctx context.Context) ([]protocol.CredentialInventoryEntry, error) {
	if d.credManager == nil {
		return nil, nil
	}
	sortedRefs := append([]protocol.CredentialRef(nil), d.credRefs...)
	sort.Slice(sortedRefs, func(i, j int) bool { return sortedRefs[i].RefID < sortedRefs[j].RefID })
	var entries []protocol.CredentialInventoryEntry
	for _, ref := range sortedRefs {
		// CheckCredential errors only on a structurally malformed ref
		// or an unknown CredentialRefKind — never on "the credential
		// isn't there", which is already a valid
		// Unavailable/Unauthenticated AuthEvidence, not a Go error.
		// A malformed *configured* reference is a real bug in
		// DoctorOptions.CredentialRefs, so it propagates and fails
		// rather than being papered over with a fabricated evidence entry
		// (fail closed, AGENTS.md §16; DCI-104).
		evidence, err := d.credManager.CheckCredential(ctx, ref)
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err,
				"resource inventory: configured credential reference %q is invalid", ref.RefID)
		}
		entries = append(entries, protocol.CredentialInventoryEntry{
			Ref:      ref,
			Evidence: evidence,
		})
	}
	return entries, nil
}

// ProjectResourceInventory is a pure, deterministic projection from already-observed
// facts, endpoints, hosts, profile, scope readiness, credential entries, and policy
// into a protocol.ResourceInventory. It performs no I/O, subprocess execution, or
// environment inspection.
//
// Collections are sorted into a stable order (endpoints/credentials by
// their ID, hosts by HostID, scopes by ScopeKind, backends alphabetically)
// before being stored, so two calls built from the same facts in different
// input orderings produce byte-identical inventories.
func ProjectResourceInventory(
	inventoryID string,
	observedAt time.Time,
	facts protocol.EnvironmentFacts,
	fingerprint string,
	endpoints []protocol.CognitionEndpointSummary,
	hosts []protocol.PrincipalHostSummary,
	cognProfile *protocol.MachineCapabilityProfile,
	scopeReadiness []protocol.ScopeReadiness,
	credentials []protocol.CredentialInventoryEntry,
	policy *cognition.Policy,
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
	sort.Slice(backends, func(i, j int) bool { return backends[i] < backends[j] })

	sortedEndpoints := append([]protocol.CognitionEndpointSummary(nil), endpoints...)
	sort.Slice(sortedEndpoints, func(i, j int) bool { return sortedEndpoints[i].ID < sortedEndpoints[j].ID })

	sortedHosts := append([]protocol.PrincipalHostSummary(nil), hosts...)
	sort.Slice(sortedHosts, func(i, j int) bool { return sortedHosts[i].HostID < sortedHosts[j].HostID })

	sortedScopeReadiness := append([]protocol.ScopeReadiness(nil), scopeReadiness...)
	sort.Slice(sortedScopeReadiness, func(i, j int) bool { return sortedScopeReadiness[i].Scope < sortedScopeReadiness[j].Scope })

	sortedCreds := append([]protocol.CredentialInventoryEntry(nil), credentials...)
	sort.Slice(sortedCreds, func(i, j int) bool { return sortedCreds[i].Ref.RefID < sortedCreds[j].Ref.RefID })

	inv := &protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        inventoryID,
		MachineFingerprint: fingerprint,
		ObservedAt:         protocol.NewTimestamp(observedAt),
		Hardware: protocol.HardwareSummary{
			OSFamily:            facts.Host.Family,
			Arch:                facts.Host.Arch,
			LogicalCores:        logicalCores,
			TotalMemoryBytes:    totalMem,
			AcceleratorBackends: backends,
		},
		CognitionEndpoints: sortedEndpoints,
		PrincipalHosts:     sortedHosts,
		Readiness:          sortedScopeReadiness,
		Credentials:        sortedCreds,
	}

	if cognProfile != nil {
		inv.Profile = &protocol.MachineProfileRef{
			ProfileID:          cognProfile.ProfileID,
			MachineFingerprint: cognProfile.MachineFingerprint,
			ObservedAt:         cognProfile.ObservedAt,
			ProbeDepth:         cognProfile.ProbeDepth,
		}
	}

	if policy != nil {
		inv.Policy = &protocol.PolicySummary{
			MaxSourceExposure: policy.MaxSourceExposure,
			MaxCostClass:      policy.MaxCostClass,
		}
	}

	if err := inv.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "resource inventory validation failed")
	}

	return inv, nil
}

// BuildResourceInventory coordinates credential observation for configured CredentialRefs
// and projects the already-observed facts, endpoints, hosts, profile, and scope readiness
// into a protocol.ResourceInventory via ProjectResourceInventory.
func (d *Doctor) BuildResourceInventory(
	ctx context.Context,
	facts protocol.EnvironmentFacts,
	fingerprint string,
	endpoints []protocol.CognitionEndpointSummary,
	hosts []protocol.PrincipalHostSummary,
	cognProfile *protocol.MachineCapabilityProfile,
	scopeReadiness []protocol.ScopeReadiness,
) (*protocol.ResourceInventory, error) {
	creds, err := d.ObserveCredentials(ctx)
	if err != nil {
		return nil, err
	}
	return ProjectResourceInventory(
		d.ids.New("inv"),
		d.clock.Now(),
		facts,
		fingerprint,
		endpoints,
		hosts,
		cognProfile,
		scopeReadiness,
		creds,
		d.policy,
	)
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
	// - Health probes with expired cached inference DO NOT refresh the mutable "latest" cache slot (never refresh stale inference).
	// - Shallow probes without cached inference write fresh health profile with DefaultCacheTTL.
	//
	// The immutable profiles/<profile_id> archive is written unconditionally,
	// separately from that "latest" TTL policy: every ResourceInventory.Profile
	// this Doctor run may go on to embed cites activeProfile.ProfileID, so that
	// exact observation must always be archived and recoverable — including on
	// the stale-inference-retained path, which deliberately skips the latest
	// cache refresh but must not also skip archival (independent-review
	// follow-up on WP-M3B-5, finding 1b). A failed archive write is fatal
	// rather than silently ignored (finding 1a): a ResourceInventory whose
	// Profile reference cannot resolve would violate the provenance guarantee
	// the reference exists to provide.
	if d.cache != nil {
		archiveTTL := DefaultCacheTTL
		archiveExpiresAt := d.clock.Now().Add(archiveTTL)
		if usedCachedInference && !cachedExpired {
			archiveExpiresAt = cachedEnv.ExpiresAt.Time()
		}
		if err := archiveProfile(ctx, d.cache, *activeProfile, archiveExpiresAt); err != nil {
			return nil, nil, nil, "", errs.Wrap(errs.CategoryInternal, err,
				"archive machine capability profile %s so its ResourceInventory reference remains resolvable", activeProfile.ProfileID)
		}

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
		// An explicit operator-configured binding (EndpointCredentialRefs)
		// takes precedence over whatever a cognition adapter opportunistically
		// declared on the endpoint itself: it is the deliberate, reviewed
		// source of truth, not a best-effort discovery byproduct
		// (independent-review follow-up on WP-M3B-5, round-4 finding 1).
		// An adapter-declared CredentialRef is only carried through as
		// machine-verifiable when it resolves to the same configured
		// CredentialRef set NewDoctor already validated
		// EndpointCredentialRefs against — an unrecognized adapter-supplied
		// value is diagnostic-only (left empty here), never treated as a
		// real binding Planner could turn into an unverifiable
		// endpoint_authenticated condition (independent-review follow-up on
		// WP-M3B-5, round-5 finding 1).
		credRef := ""
		if d.credRefIndex[ep.CredentialRef] {
			credRef = ep.CredentialRef
		}
		if bound, ok := d.endpointCredRefs[ep.ID]; ok && bound != "" {
			credRef = bound
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
			CredentialRef:          credRef,
		}
		summaries = append(summaries, summary)

		if protocol.EndpointViable(ep.Kind, ep.Locality, ep.Health, ep.Auth) {
			hasCoding = true
			findings = append(findings, protocol.DiagnosticFinding{
				Category: "cognition",
				Severity: SeverityInfo,
				Code:     FindingCodeEndpointReady,
				Title:    fmt.Sprintf("Endpoint ready: %s", ep.ID),
				Detail:   fmt.Sprintf("Kind %s, Locality %s, Health %s, Auth %s", ep.Kind, ep.Locality, ep.Health, ep.Auth),
			})
		} else if ep.Health == protocol.EndpointHealthReady {
			switch ep.Auth {
			case protocol.AuthExpired:
				remed := fmt.Sprintf("Re-authenticate CLI or update API credentials for %s", ep.ID)
				findings = append(findings, protocol.DiagnosticFinding{
					Category:    "auth",
					Severity:    SeverityWarning,
					Code:        FindingCodeAuthExpired,
					Title:       fmt.Sprintf("Authentication expired: %s", ep.ID),
					Detail:      fmt.Sprintf("Endpoint %s auth status is expired", ep.ID),
					Remediation: &remed,
				})
			case protocol.AuthUnauthenticated:
				remed := fmt.Sprintf("Authenticate CLI or configure API credentials for %s", ep.ID)
				findings = append(findings, protocol.DiagnosticFinding{
					Category:    "auth",
					Severity:    SeverityWarning,
					Code:        FindingCodeAuthUnauthenticated,
					Title:       fmt.Sprintf("Authentication missing: %s", ep.ID),
					Detail:      fmt.Sprintf("Endpoint %s is unauthenticated", ep.ID),
					Remediation: &remed,
				})
			case protocol.AuthUnknown:
				findings = append(findings, protocol.DiagnosticFinding{
					Category: "cognition",
					Severity: SeverityInfo,
					Code:     FindingCodeAuthUnknown,
					Title:    fmt.Sprintf("Endpoint authentication unverified: %s", ep.ID),
					Detail:   fmt.Sprintf("Endpoint %s health is ready, but its authentication has not been verified", ep.ID),
				})
			default:
				remed := fmt.Sprintf("Check authentication credentials for %s", ep.ID)
				findings = append(findings, protocol.DiagnosticFinding{
					Category:    "auth",
					Severity:    SeverityWarning,
					Code:        FindingCodeAuthUnauthenticated,
					Title:       fmt.Sprintf("Authentication invalid: %s", ep.ID),
					Detail:      fmt.Sprintf("Endpoint %s auth status is %s", ep.ID, ep.Auth),
					Remediation: &remed,
				})
			}
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
			Title:       "No viable coding endpoint found",
			Detail:      "DevCadence requires at least one viable (healthy and authenticated) cognition endpoint for code execution",
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

	// Viability (not mere health) is checked via the same
	// protocol.EndpointViable predicate protocol.EvaluateScopeReadiness
	// uses, so monolithic and scope-specific readiness cannot silently
	// disagree about what "usable" means (independent-review follow-up on
	// WP-M3B-5, finding 5).
	for _, ep := range fullEndpoints {
		if ep.Health != protocol.EndpointHealthReady {
			continue
		}
		isLocal := ep.Locality == protocol.LocalityLocal || ep.Kind == protocol.EndpointLocalRuntime
		isRemote := ep.Locality == protocol.LocalityRemote || ep.Kind == protocol.EndpointRemoteAPI || ep.Kind == protocol.EndpointAuthenticatedCLI
		viable := protocol.EndpointViable(ep.Kind, ep.Locality, ep.Health, ep.Auth)
		if isLocal && viable {
			localEndpoints = append(localEndpoints, ep)
			if ep.AccelerationVerified() {
				acceleratedLocal = append(acceleratedLocal, ep)
			}
		}
		if isRemote && viable {
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

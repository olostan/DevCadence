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

	// 6. Profile recommendation
	recommendation := d.recommender.Recommend(RecommendationInput{
		Facts:            facts,
		Endpoints:        endpoints,
		CognitionProfile: cognProfile,
		Policy:           d.policy,
	})

	// Ensure evaluated scope has a TargetProfile before checking READY (ADR-0014)
	if scope.TargetProfile == nil && recommendation.SelectedProfile != nil {
		scope.TargetProfile = recommendation.SelectedProfile
	}

	// 7. Evaluate Readiness
	readiness := d.evaluateReadiness(scope, findings, endpoints, scope.TargetProfile)

	report := &protocol.DoctorReport{
		SchemaVersion:       protocol.SchemaVersion1,
		ReportID:            d.ids.New("doc"),
		MachineFingerprint:  fingerprint,
		ObservedAt:          protocol.NewTimestamp(d.clock.Now()),
		EvaluationScope:     scope,
		Readiness:           readiness,
		Findings:            findings,
		RecommendedProfile:  &recommendation,
		DiscoveredEndpoints: endpoints,
		PrincipalHosts:      hosts,
	}

	if err := report.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "doctor report validation failed")
	}

	return report, nil
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
	} else if d.cache != nil {
		// Check if we have a valid cached profile matching the machine fingerprint
		if cached, found, err := Read[protocol.MachineCapabilityProfile](ctx, d.cache, protocol.CacheTargetMachineProfile, fingerprint); err == nil && found {
			evidenceStatus = "refreshed_health"
			hasVerifiedAcc := false
			for _, ep := range cached.Endpoints {
				if ep.AccelerationVerified() {
					hasVerifiedAcc = true
					break
				}
			}
			if !hasVerifiedAcc {
				evidenceStatus = "stale_inference_retained"
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

	// Durably cache the discovered machine profile
	if d.cache != nil {
		_ = Write(ctx, d.cache, protocol.CacheTargetMachineProfile, fingerprint, profile, DefaultCacheTTL)
	}

	hasCoding := false
	for _, ep := range profile.Endpoints {
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

	return summaries, findings, &profile, evidenceStatus, nil
}

func (d *Doctor) evaluateReadiness(scope protocol.ReadinessEvaluationScope, findings []protocol.DiagnosticFinding, endpoints []protocol.CognitionEndpointSummary, targetProfile *protocol.DeploymentProfile) protocol.ReadinessStatus {
	// 1. Mandatory base dependencies (Git, state root, disk space, errors)
	for _, f := range findings {
		if f.Severity == SeverityError {
			return protocol.ReadinessActionRequired
		}
	}

	// 2. If no target profile is specified or selected, we cannot be fully READY
	if targetProfile == nil {
		return protocol.ReadinessPartiallyReady
	}

	// 3. Classify ready endpoints
	var localEndpoints []protocol.CognitionEndpointSummary
	var acceleratedLocal []protocol.CognitionEndpointSummary
	var remoteEndpoints []protocol.CognitionEndpointSummary

	for _, ep := range endpoints {
		if ep.Health != protocol.EndpointHealthReady {
			continue
		}
		if ep.Locality == protocol.LocalityLocal || ep.Kind == protocol.EndpointLocalRuntime {
			localEndpoints = append(localEndpoints, ep)
			if ep.AccelerationVerified {
				acceleratedLocal = append(acceleratedLocal, ep)
			}
		}
		if (ep.Locality == protocol.LocalityRemote || ep.Kind == protocol.EndpointRemoteAPI || ep.Kind == protocol.EndpointAuthenticatedCLI) && ep.Auth == protocol.AuthAuthenticated {
			remoteEndpoints = append(remoteEndpoints, ep)
		}
	}

	// 4. Verify target profile constraints
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
			// Local-heavy requires verified hardware acceleration
			return protocol.ReadinessReadyWithReducedCap
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

	// 5. Verify required roles under target profile
	for _, role := range scope.RequiredRoles {
		roleLower := strings.ToLower(role)
		satisfied := false
		switch *targetProfile {
		case protocol.ProfileOffline:
			// Offline: strictly local endpoints
			for _, ep := range localEndpoints {
				switch roleLower {
				case "scout", "classifier":
					satisfied = true
				case "reviewer", "correctness_reviewer":
					satisfied = true
				case "principal", "implementer", "implementation", "architecture_reviewer":
					if ep.AccelerationVerified {
						satisfied = true
					}
				default:
					satisfied = true
				}
				if satisfied {
					break
				}
			}
		case protocol.ProfileCloudCognition:
			if len(remoteEndpoints) > 0 {
				satisfied = true
			}
		case protocol.ProfileHybridThin:
			switch roleLower {
			case "scout", "classifier":
				satisfied = len(localEndpoints) > 0
			case "reviewer", "correctness_reviewer":
				satisfied = len(localEndpoints) > 0 || len(remoteEndpoints) > 0
			case "principal", "implementer", "implementation", "architecture_reviewer":
				satisfied = len(remoteEndpoints) > 0
			default:
				satisfied = len(localEndpoints) > 0 || len(remoteEndpoints) > 0
			}
		case protocol.ProfileLocalHeavy:
			for _, ep := range localEndpoints {
				if ep.AccelerationVerified {
					satisfied = true
					break
				}
				if roleLower == "scout" || roleLower == "classifier" {
					satisfied = true
					break
				}
			}
		case protocol.ProfileCustom:
			satisfied = len(localEndpoints) > 0 || len(remoteEndpoints) > 0
		}
		if !satisfied {
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

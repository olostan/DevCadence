package setup

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/credentials"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
)

// TestEndToEnd_DoctorPlannerExecutor_ExplicitCredentialBinding is the
// independent-review follow-up on WP-M3B-5, round-4 finding 1's required
// end-to-end regression: it exercises the real Doctor -> Planner -> Executor
// service path (not hand-constructed CognitionEndpointSummary values) to
// prove the explicit endpoint->CredentialRef binding survives the whole
// chain and actually verifies, for an endpoint whose ID differs from its
// credential's locator.
func TestEndToEnd_DoctorPlannerExecutor_ExplicitCredentialBinding(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)
	seq := ids.NewSequential()
	tmpHome := t.TempDir()

	const (
		endpointID  = "cli:claude-code"
		credRefID   = "claude-cli-ref"
		credLocator = "claude-handle"
	)

	stub := &stubCognitionAdapter{
		endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     endpointID,
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Locality:               protocol.LocalityRemote,
				Health:                 protocol.EndpointHealthUnhealthy,
				Auth:                   protocol.AuthExpired,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				StructuredOutput:       protocol.FeatureDeclared,
				ToolUse:                protocol.FeatureDeclared,
				ObservedAt:             protocol.NewTimestamp(now),
				// The coding-CLI adapter deliberately never invents this
				// (see codingcli_test.go's "a credential reference was
				// invented" regression) — left empty here on purpose so the
				// only source of the binding is DoctorOptions.
				// EndpointCredentialRefs below.
			},
		},
	}
	service, err := cognition.NewService(cognition.Options{
		Adapters: []cognition.Adapter{stub},
		Clock:    clk,
		IDs:      seq,
	})
	if err != nil {
		t.Fatalf("cognition.NewService: %v", err)
	}

	authAdapter := &credentials.StaticCLIAuthAdapter{
		ID:     "adapter-claude",
		Target: credLocator,
		Status: protocol.AuthStatusAuthenticated,
		Detail: "claude session active",
	}
	credMgr, err := credentials.NewManager(credentials.Options{
		Clock:       clk,
		CLIAdapters: []credentials.CLISessionAuthAdapter{authAdapter},
	})
	if err != nil {
		t.Fatalf("credentials.NewManager: %v", err)
	}

	credRefs := []protocol.CredentialRef{
		{SchemaVersion: protocol.SchemaVersion1, RefID: credRefID, Kind: protocol.CredRefCLISession, Locator: credLocator},
	}

	doc, err := NewDoctor(DoctorOptions{
		Clock:             clk,
		IDs:               seq,
		HomeDir:           tmpHome,
		CognitionService:  service,
		CredentialManager: credMgr,
		CredentialRefs:    credRefs,
		// The explicit, operator-configured binding this finding requires:
		// endpoint ID -> CredentialRef.RefID, deliberately not equal to
		// either the endpoint ID or the credential's own Locator.
		EndpointCredentialRefs: map[string]string{endpointID: credRefID},
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	facts := protocol.EnvironmentFacts{
		Host:           protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Memory:         protocol.MemoryFacts{TotalBytes: int64Ptr(16 * 1024 * 1024 * 1024)},
		Virtualization: protocol.VirtualizationFacts{Container: protocol.ContainerNone},
		Software: []protocol.SoftwarePresence{
			{ID: "git", Category: protocol.SoftwareEngineering, Installed: true, Version: "2.45.0"},
		},
	}
	targetProfile := protocol.ProfileCloudCognition
	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  &targetProfile,
		RequiredRoles:  []string{"implementation"},
		EvidenceStatus: "live",
	}

	// Step 1: the real Doctor.Run path must carry the explicit binding into
	// the report's DiscoveredEndpoints, not merely the raw endpoint's own
	// (here empty) CredentialRef.
	report, err := doc.Run(ctx, scope, facts)
	if err != nil {
		t.Fatalf("doc.Run: %v", err)
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("report.Validate: %v", err)
	}
	var discovered *protocol.CognitionEndpointSummary
	for i := range report.DiscoveredEndpoints {
		if report.DiscoveredEndpoints[i].ID == endpointID {
			discovered = &report.DiscoveredEndpoints[i]
		}
	}
	if discovered == nil {
		t.Fatalf("expected DiscoveredEndpoints to contain %q", endpointID)
	}
	if discovered.CredentialRef != credRefID {
		t.Fatalf("DiscoveredEndpoints[%s].CredentialRef = %q, want %q (from DoctorOptions.EndpointCredentialRefs, not guessed)",
			endpointID, discovered.CredentialRef, credRefID)
	}

	// Step 2: the real Planner.Plan path must propagate that same RefID
	// into the generated reauthenticate action's condition.
	planner, err := NewPlanner(PlannerOptions{Clock: clk, IDs: seq})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	plan, err := planner.Plan(report, protocol.TargetAll, protocol.ProfileCloudCognition)
	if err != nil {
		t.Fatalf("planner.Plan: %v", err)
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("plan.Validate: %v", err)
	}
	var reauth *protocol.SetupAction
	for i := range plan.Actions {
		if plan.Actions[i].RecipeID == "recipe.manual.reauthenticate" {
			reauth = &plan.Actions[i]
		}
	}
	if reauth == nil {
		t.Fatalf("expected a reauthenticate action for the bound expired endpoint")
	}
	if got := reauth.Postconditions[0].EndpointAuthenticated.CredentialRefID; got != credRefID {
		t.Fatalf("Postconditions[0].CredentialRefID = %q, want %q", got, credRefID)
	}
	if got := reauth.Postconditions[0].EndpointAuthenticated.EndpointID; got != endpointID {
		t.Fatalf("Postconditions[0].EndpointID = %q, want %q (differs from credential locator %q by design)", got, endpointID, credLocator)
	}

	// Step 3: the real Executor.CheckPostconditions path must resolve that
	// same RefID against the actually configured credentials and report
	// the postcondition satisfied once auth evidence says authenticated.
	exec, err := NewExecutor(ExecutorOptions{
		Runner:            &fakeCommandRunner{},
		Home:              filepath.Join(tmpHome, "exec"),
		Clock:             clk,
		IDs:               seq,
		CredentialManager: credMgr,
		CredentialRefs:    credRefs,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	passed, detail, err := exec.CheckPostconditions(ctx, reauth.Postconditions)
	if err != nil {
		t.Fatalf("CheckPostconditions: %v", err)
	}
	if !passed {
		t.Fatalf("expected postcondition to pass via the resolved binding, got detail: %s", detail)
	}
}

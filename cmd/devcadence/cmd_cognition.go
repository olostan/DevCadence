package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/cognition/planner"
	"github.com/olostan/DevCadence/internal/cognition/plannerdriver"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// simpleDriverResolver provides a safe fallback resolver for CLI planning.
type simpleDriverResolver struct {
	drivers map[string]drivers.SessionDriver
	models  map[string]string
}

func (r *simpleDriverResolver) ResolveDriver(ctx context.Context, endpointID string) (drivers.SessionDriver, string, error) {
	if r.drivers == nil {
		return nil, "", nil
	}
	drv, ok := r.drivers[endpointID]
	if !ok || drv == nil {
		return nil, "", nil
	}
	modelID := ""
	if r.models != nil {
		modelID = r.models[endpointID]
	}
	return drv, modelID, nil
}

// runCognitionRecommend generates portfolio recommendations (WP-M3D-4, REQ-02).
func runCognitionRecommend(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("cognition recommend", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the portfolio recommendations as JSON")
	intentFlag := fs.String("intent", "", "target recommendation intent: minimum_spend, balanced, maximum_quality_within_policy, privacy_first")
	depthFlag := fs.String("depth", string(protocol.DepthHealth), "probe depth: inventory or health")

	if err := parseFlags(fs, e, args); err != nil {
		return err
	}

	depth, err := parseDepth(*depthFlag)
	if err != nil {
		return err
	}
	if depth == protocol.DepthInference {
		return errs.New(errs.CategoryInvalidArgument, "recommend does not run inference depth")
	}

	// 1. Discover local environment facts and doctor report
	doc, facts, err := e.buildDoctor(ctx, depth)
	if err != nil {
		return err
	}
	report, err := doc.Run(ctx, protocol.ReadinessEvaluationScope{EvidenceStatus: "live"}, facts)
	if err != nil {
		return err
	}
	if report.ResourceInventory == nil {
		return errs.New(errs.CategoryInternal, "failed to discover resource inventory")
	}

	// 2. Build intents
	var intents []protocol.RecommendationIntent
	if *intentFlag != "" {
		intent := protocol.RecommendationIntent(*intentFlag)
		if !intent.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "invalid recommendation intent %q", *intentFlag)
		}
		intents = []protocol.RecommendationIntent{intent}
	} else {
		intents = []protocol.RecommendationIntent{
			protocol.IntentMinimumSpend,
			protocol.IntentBalanced,
			protocol.IntentMaximumQualityWithinPolicy,
			protocol.IntentPrivacyFirst,
		}
	}

	profile, err := buildProfile(ctx, depth)
	if err != nil {
		return err
	}

	req := planner.Request{
		Inventory:       report.ResourceInventory,
		MachineProfile:  &profile,
		ContextProfiles: nil,
		Intents:         intents,
		Clock:           clock.System(),
	}

	// 3. Execute planning with graceful driver degradation
	res, err := plannerdriver.ExecutePlanning(ctx, req, &simpleDriverResolver{}, plannerdriver.ExecutionConfig{
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to execute portfolio planning")
	}

	if *asJSON {
		return writeJSON(e.stdout, res)
	}

	renderRecommendations(e.stdout, res)
	return nil
}

func renderRecommendations(w interface{ Write([]byte) (int, error) }, res *planner.Result) {
	fmt.Fprintf(w, "Cognition Portfolio Recommendations\n")
	fmt.Fprintf(w, "Outcome: %s (accepted: %d, rejections: %d)\n\n", res.Outcome, len(res.Accepted), len(res.Rejected))

	if len(res.Accepted) > 0 {
		fmt.Fprintf(w, "Accepted Recommendations:\n")
		for i, alt := range res.Accepted {
			fmt.Fprintf(w, "  %d. [%-30s] Confidence: %-6s Portfolio ID: %s\n",
				i+1, alt.Intent, alt.Confidence, alt.RecommendedPortfolio.PortfolioID)
			if len(alt.Tradeoffs) > 0 {
				fmt.Fprintf(w, "     Tradeoffs: %s\n", strings.Join(alt.Tradeoffs, ", "))
			}
			fmt.Fprintf(w, "     Role Bindings (%d):\n", len(alt.RecommendedPortfolio.RoleBindings))
			for _, rb := range alt.RecommendedPortfolio.RoleBindings {
				fmt.Fprintf(w, "       - %-12s -> endpoint=%-15s channel=%-15s pool=%-15s\n",
					rb.Role, rb.EndpointID, rb.ChannelID, rb.BudgetPoolID)
			}
			fmt.Fprintln(w)
		}
	}

	if len(res.Rejected) > 0 {
		fmt.Fprintf(w, "Rejections:\n")
		for i, rej := range res.Rejected {
			fmt.Fprintf(w, "  %d. [%-30s] Reason: %s (diagnostics: %d)\n",
				i+1, rej.Intent, rej.Reason, len(rej.Diagnostics))
		}
	}
}

// runCognitionExplain inspects the active portfolio or a file (WP-M3D-4, REQ-03).
func runCognitionExplain(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("cognition explain", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the portfolio as JSON")
	filePath := fs.String("file", "", "path to a portfolio JSON file to inspect")
	_ = fs.Bool("active", false, "inspect active portfolio (default if --file is omitted)")

	if err := parseFlags(fs, e, args); err != nil {
		return err
	}

	var portfolio *protocol.CognitionPortfolio
	var rec *cognition.ActivationRecord

	if *filePath != "" {
		data, err := os.ReadFile(*filePath)
		if err != nil {
			if os.IsNotExist(err) {
				return errs.New(errs.CategoryInvalidArgument, "portfolio file not found: %s", *filePath)
			}
			return errs.Wrap(errs.CategoryInvalidArgument, err, "failed to read portfolio file")
		}
		var p protocol.CognitionPortfolio
		if err := json.Unmarshal(data, &p); err != nil {
			return errs.Wrap(errs.CategoryIntegrity, err, "malformed portfolio JSON file")
		}
		portfolio = &p
	} else {
		// Read active portfolio from ActivationManager
		actDir := filepath.Join(e.homeDir(), "state")
		mgr, err := cognition.NewActivationManager(actDir, nil, clock.System())
		if err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to initialize activation manager")
		}
		p, currentRec, err := mgr.GetActivePortfolio(ctx)
		if err != nil {
			return err
		}
		portfolio = p
		rec = currentRec
	}

	if *asJSON {
		return writeJSON(e.stdout, portfolio)
	}

	renderPortfolioExplanation(e.stdout, portfolio, rec)
	return nil
}

func renderPortfolioExplanation(w interface{ Write([]byte) (int, error) }, p *protocol.CognitionPortfolio, rec *cognition.ActivationRecord) {
	fmt.Fprintf(w, "Cognition Portfolio: %s (rev %d)\n", p.PortfolioID, p.Revision)
	fmt.Fprintf(w, "Created At:          %s\n", p.CreatedAt)
	fmt.Fprintf(w, "Max Source Exposure: %s\n", p.MaxSourceExposure)
	if rec != nil {
		fmt.Fprintf(w, "Active Sequence:     %d (activation ID: %s, activated at: %s)\n",
			rec.Sequence, rec.ActivationID, rec.ActivatedAt)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "Access Channels (%d):\n", len(p.Channels))
	for _, ch := range p.Channels {
		fmt.Fprintf(w, "  - [%-15s] endpoint=%-15s kind=%-20s session=%s\n",
			ch.ChannelID, ch.EndpointID, ch.Kind, ch.SessionMode)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "Role Bindings (%d):\n", len(p.RoleBindings))
	for _, rb := range p.RoleBindings {
		fmt.Fprintf(w, "  - [%-12s] priority=%d endpoint=%-15s channel=%-15s pool=%-15s\n",
			rb.Role, rb.Priority, rb.EndpointID, rb.ChannelID, rb.BudgetPoolID)
		if len(rb.Fallbacks) > 0 {
			for _, fb := range rb.Fallbacks {
				fmt.Fprintf(w, "      fallback -> endpoint=%-15s channel=%-15s pool=%-15s\n",
					fb.EndpointID, fb.ChannelID, fb.BudgetPoolID)
			}
		}
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "Budget Pools (%d):\n", len(p.BudgetPools))
	for _, bp := range p.BudgetPools {
		fmt.Fprintf(w, "  - [%-15s] regime=%-15s limit=%d %s (period: %s)\n",
			bp.PoolID, bp.Regime, bp.HardLimit, bp.Unit, bp.Period)
	}
}

// runCognitionApply activates a portfolio or rolls back (WP-M3D-4, REQ-04).
func runCognitionApply(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("cognition apply", flag.ContinueOnError)
	filePath := fs.String("file", "", "path to the candidate portfolio JSON file to apply")
	rollbackFlag := fs.Bool("rollback", false, "roll back to a previous active portfolio")
	targetFlag := fs.String("target", "", "target activation ID for rollback (defaults to previous)")
	reasonFlag := fs.String("reason", "applied via CLI", "rationale for applying this portfolio change")
	asJSON := fs.Bool("json", false, "emit the activation record as JSON")

	if err := parseFlags(fs, e, args); err != nil {
		return err
	}

	if !*rollbackFlag {
		if *filePath == "" {
			return errs.New(errs.CategoryInvalidArgument, "--file is required when not rolling back")
		}
		if _, err := os.Stat(*filePath); err != nil {
			if os.IsNotExist(err) {
				return errs.New(errs.CategoryInvalidArgument, "portfolio file not found: %s", *filePath)
			}
			return errs.Wrap(errs.CategoryInvalidArgument, err, "failed to access candidate portfolio file")
		}
	}

	actDir := filepath.Join(e.homeDir(), "state")
	mgr, err := cognition.NewActivationManager(actDir, nil, clock.System())
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to initialize activation manager")
	}
	service := cognition.NewAdaptationService(mgr)

	valInput, err := buildCognitionValidationInput(ctx, e)
	if err != nil {
		return err
	}

	// Synthesize provisional context profiles for history records (for safe rollback revalidation)
	if history, hErr := mgr.GetActivationHistory(ctx); hErr == nil {
		for _, rec := range history {
			synthesizeContextProfiles(&rec.Portfolio, valInput.ContextProfiles)
		}
	}

	if *rollbackFlag {
		rec, err := service.Rollback(ctx, *targetFlag, &valInput)
		if err != nil {
			return err
		}
		if *asJSON {
			return writeJSON(e.stdout, rec)
		}
		fmt.Fprintf(e.stdout, "Successfully rolled back portfolio to %s (sequence: %d, activation: %s)\n",
			rec.PortfolioID, rec.Sequence, rec.ActivationID)
		return nil
	}

	if *filePath == "" {
		return errs.New(errs.CategoryInvalidArgument, "--file is required when not rolling back")
	}

	data, err := os.ReadFile(*filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return errs.New(errs.CategoryInvalidArgument, "portfolio file not found: %s", *filePath)
		}
		return errs.Wrap(errs.CategoryInvalidArgument, err, "failed to read candidate portfolio file")
	}

	var candidate protocol.CognitionPortfolio
	if err := json.Unmarshal(data, &candidate); err != nil {
		return errs.Wrap(errs.CategoryIntegrity, err, "malformed candidate portfolio JSON")
	}

	// Synthesize provisional context profiles for candidate portfolio
	synthesizeContextProfiles(&candidate, valInput.ContextProfiles)

	var base *protocol.CognitionPortfolio
	activeP, _, err := mgr.GetActivePortfolio(ctx)
	if err == nil {
		base = activeP
	}

	prop, err := cognition.CreateChangeProposal(cognition.TriggerManualProposal, *reasonFlag, base, candidate, time.Now())
	if err != nil {
		return err
	}

	valInput.Portfolio = &candidate
	rec, err := service.ProposeAndActivate(ctx, prop, valInput)
	if err != nil {
		return err
	}

	if *asJSON {
		return writeJSON(e.stdout, rec)
	}

	fmt.Fprintf(e.stdout, "Successfully activated portfolio %s (revision: %d, sequence: %d, activation: %s)\n",
		rec.PortfolioID, rec.PortfolioRevision, rec.Sequence, rec.ActivationID)
	return nil
}

// synthesizeContextProfiles synthesizes default provisional context profiles for any channels
// or role bindings in the portfolio that are not already present in the profiles map.
func synthesizeContextProfiles(p *protocol.CognitionPortfolio, profiles map[string]*protocol.ContextProfile) {
	if p == nil || profiles == nil {
		return
	}
	chMap := make(map[string]protocol.AccessChannel, len(p.Channels))
	for _, ch := range p.Channels {
		chMap[ch.ChannelID] = ch
	}

	for _, rb := range p.RoleBindings {
		if rb.ContextProfileID != "" && profiles[rb.ContextProfileID] == nil {
			ch := chMap[rb.ChannelID]
			prof, err := compiler.DefaultProvisionalProfileWithCapabilities(
				rb.EndpointID,
				rb.ChannelID,
				"provisional-model",
				32768,
				0,
				ch.ContextControl,
				ch.PrefixCache,
				"",
			)
			if err == nil {
				prof.ProfileID = rb.ContextProfileID
				profiles[rb.ContextProfileID] = prof
			}
		}
		for _, fb := range rb.Fallbacks {
			if fb.ContextProfileID != "" && profiles[fb.ContextProfileID] == nil {
				ch := chMap[fb.ChannelID]
				prof, err := compiler.DefaultProvisionalProfileWithCapabilities(
					fb.EndpointID,
					fb.ChannelID,
					"provisional-model",
					32768,
					0,
					ch.ContextControl,
					ch.PrefixCache,
					"",
				)
				if err == nil {
					prof.ProfileID = fb.ContextProfileID
					profiles[fb.ContextProfileID] = prof
				}
			}
		}
	}
}

// buildCognitionValidationInput resolves the ValidationInput for portfolio activation.
// If an inventory/validation fixture is supplied (via DEVCADENCE_COGNITION_FIXTURE or
// $DEVCADENCE_HOME/cognition-fixture.json), it uses the fixture. Otherwise it discovers
// live doctor facts and the local machine profile.
func buildCognitionValidationInput(ctx context.Context, e *env) (cognition.ValidationInput, error) {
	fixturePath := os.Getenv("DEVCADENCE_COGNITION_FIXTURE")
	if fixturePath == "" {
		homeFixture := filepath.Join(e.homeDir(), "cognition-fixture.json")
		if _, err := os.Stat(homeFixture); err == nil {
			fixturePath = homeFixture
		}
	}

	var valInput cognition.ValidationInput
	if fixturePath != "" {
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			return cognition.ValidationInput{}, errs.Wrap(errs.CategoryInternal, err, "failed to read cognition fixture")
		}
		if err := json.Unmarshal(data, &valInput); err != nil {
			return cognition.ValidationInput{}, errs.Wrap(errs.CategoryIntegrity, err, "failed to unmarshal cognition fixture")
		}
	} else {
		doc, facts, err := e.buildDoctor(ctx, protocol.DepthHealth)
		if err != nil {
			return cognition.ValidationInput{}, err
		}
		report, err := doc.Run(ctx, protocol.ReadinessEvaluationScope{EvidenceStatus: "live"}, facts)
		if err != nil {
			return cognition.ValidationInput{}, err
		}

		profile, err := buildProfile(ctx, protocol.DepthHealth)
		if err != nil {
			return cognition.ValidationInput{}, err
		}

		valInput = cognition.ValidationInput{
			MachineProfile:  &profile,
			Inventory:       report.ResourceInventory,
			ContextProfiles: nil,
			Policy:          nil,
			Clock:           clock.System(),
		}
	}

	if valInput.Clock == nil {
		valInput.Clock = clock.System()
	}
	if valInput.ContextProfiles == nil {
		valInput.ContextProfiles = make(map[string]*protocol.ContextProfile)
	}

	return valInput, nil
}

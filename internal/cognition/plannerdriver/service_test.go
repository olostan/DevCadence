package plannerdriver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/cognition/planner"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

type testResolver struct {
	drivers map[string]drivers.SessionDriver
	models  map[string]string
	errs    map[string]error
}

func (r *testResolver) ResolveDriver(_ context.Context, endpointID string) (drivers.SessionDriver, string, error) {
	if err, ok := r.errs[endpointID]; ok && err != nil {
		return nil, "", err
	}
	return r.drivers[endpointID], r.models[endpointID], nil
}

func validPlannerAlt(intent protocol.RecommendationIntent) map[string]any {
	p, _ := json.Marshal(makeTestPortfolio())
	var portfolio map[string]any
	_ = json.Unmarshal(p, &portfolio)
	return map[string]any{
		"intent":     string(intent),
		"portfolio":  portfolio,
		"rationale":  "because " + string(intent),
		"tradeoffs":  []string{"tradeoff " + string(intent)},
		"confidence": "high",
	}
}

func validPlannerOutput(intents ...protocol.RecommendationIntent) string {
	alts := make([]map[string]any, 0, len(intents))
	for _, i := range intents {
		alts = append(alts, validPlannerAlt(i))
	}
	b, _ := json.Marshal(map[string]any{"alternatives": alts})
	return string(b)
}

func makeBasePlannerRequest() planner.Request {
	return planner.Request{
		Inventory:       makeTestInventory(),
		MachineProfile:  makeTestMachineProfile(),
		ContextProfiles: makeTestContextProfiles(),
		BudgetStates: map[string]*protocol.BudgetState{
			"pool-sub": {
				SchemaVersion:    protocol.SchemaVersion1,
				PoolID:           "pool-sub",
				Status:           protocol.BudgetStatusHealthy,
				RemainingBalance: ptr[int64](100),
				ObservedAt:       "2026-10-04T00:00:00Z",
			},
		},
		Intents: []protocol.RecommendationIntent{protocol.IntentMinimumSpend},
		Clock:   clock.NewFake(time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), time.Second),
	}
}

func TestExecutePlanning_ACC01_ValidEndpointWorkingDriver(t *testing.T) {
	content := validPlannerOutput(protocol.IntentMinimumSpend)
	drv := newRec(okHandler(content))

	resolver := &testResolver{
		drivers: map[string]drivers.SessionDriver{"ep-local-01": drv},
		models:  map[string]string{"ep-local-01": "qwen-2.5-coder-32b"},
	}

	req := makeBasePlannerRequest()
	cfg := ExecutionConfig{
		Timeout:          5 * time.Second,
		IncludeErrorText: false,
	}

	res, err := ExecutePlanning(context.Background(), req, resolver, cfg)
	if err != nil {
		t.Fatalf("ExecutePlanning failed: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil result")
	}
	if res.Outcome != planner.OutcomeRecommended {
		t.Fatalf("expected OutcomeRecommended, got %s (detail: %s, rejected: %+v)", res.Outcome, res.Detail, res.Rejected)
	}
	if len(res.Accepted) != 1 {
		t.Fatalf("expected 1 accepted recommendation, got %d", len(res.Accepted))
	}
	rec := res.Accepted[0]
	if rec.Planner == nil {
		t.Fatalf("expected planner provenance, got nil")
	}
	if rec.Planner.EndpointID != "ep-local-01" {
		t.Fatalf("expected EndpointID ep-local-01, got %s", rec.Planner.EndpointID)
	}
	if rec.Planner.DriverID != drv.ID() {
		t.Fatalf("expected DriverID %s, got %s", drv.ID(), rec.Planner.DriverID)
	}
	if rec.Planner.ModelID != "qwen-2.5-coder-32b" {
		t.Fatalf("expected ModelID qwen-2.5-coder-32b, got %s", rec.Planner.ModelID)
	}
}

func TestExecutePlanning_ACC02_NoHealthyEndpoints(t *testing.T) {
	inv := makeTestInventory()
	// Mark all endpoints unhealthy
	for i := range inv.CognitionEndpoints {
		inv.CognitionEndpoints[i].Health = protocol.EndpointHealthUnhealthy
	}

	req := makeBasePlannerRequest()
	req.Inventory = inv

	resolver := &testResolver{}
	cfg := ExecutionConfig{Timeout: 5 * time.Second}

	res, err := ExecutePlanning(context.Background(), req, resolver, cfg)
	if err != nil {
		t.Fatalf("ExecutePlanning failed: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil result")
	}
	if res.Outcome != planner.OutcomeNoPlanner {
		t.Fatalf("expected OutcomeNoPlanner, got %s", res.Outcome)
	}
	if !strings.Contains(res.Detail, "no planning endpoint available") {
		t.Fatalf("expected detail about no planning endpoint, got %q", res.Detail)
	}
}

type worktreeDriver struct {
	*recDriver
}

func (w *worktreeDriver) Capabilities() drivers.DriverCapabilities {
	return drivers.DriverCapabilities{NativeWorktreeAccess: true}
}

func TestExecutePlanning_ACC03_NativeWorktreeAccessDegradesGracefully(t *testing.T) {
	drv := &worktreeDriver{recDriver: newRec(okHandler("OK"))}

	resolver := &testResolver{
		drivers: map[string]drivers.SessionDriver{
			"ep-local-01": drv,
			"ep-cli-01":   drv,
		},
		models: map[string]string{
			"ep-local-01": "model-1",
			"ep-cli-01":   "model-2",
		},
	}

	req := makeBasePlannerRequest()
	cfg := ExecutionConfig{Timeout: 5 * time.Second}

	res, err := ExecutePlanning(context.Background(), req, resolver, cfg)
	if err != nil {
		t.Fatalf("ExecutePlanning failed: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil result")
	}
	if res.Outcome != planner.OutcomeNoPlanner {
		t.Fatalf("expected graceful degradation to OutcomeNoPlanner, got %s", res.Outcome)
	}
}

func TestExecutePlanning_ACC04_TimeoutHonored(t *testing.T) {
	// Driver that hangs until context cancelled
	slowDriver := newRec(func(ctx context.Context, in drivers.TurnInput) (drivers.TurnResult, error) {
		<-ctx.Done()
		return drivers.TurnResult{}, ctx.Err()
	})

	resolver := &testResolver{
		drivers: map[string]drivers.SessionDriver{"ep-local-01": slowDriver},
		models:  map[string]string{"ep-local-01": "slow-model"},
	}

	req := makeBasePlannerRequest()
	cfg := ExecutionConfig{
		Timeout: 20 * time.Millisecond,
	}

	res, err := ExecutePlanning(context.Background(), req, resolver, cfg)
	if err != nil {
		t.Fatalf("ExecutePlanning returned unexpected err: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil result")
	}
	// Invocation failed outcome when timeout fires
	if res.Outcome != planner.OutcomeInvocationFailed {
		t.Fatalf("expected OutcomeInvocationFailed on timeout, got %s", res.Outcome)
	}
	if len(slowDriver.sessions) != 1 {
		t.Fatalf("expected 1 session started, got %d", len(slowDriver.sessions))
	}
	slowDriver.assertAllClosed(t)
}

func TestExecutePlanning_ACC05_NilGuards(t *testing.T) {
	req := makeBasePlannerRequest()
	resolver := &testResolver{}
	cfg := ExecutionConfig{Timeout: 5 * time.Second}

	// Nil inventory
	reqNilInv := req
	reqNilInv.Inventory = nil
	_, err := ExecutePlanning(context.Background(), reqNilInv, resolver, cfg)
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected InvalidArgument on nil inventory, got %v", err)
	}

	// Nil resolver
	_, err = ExecutePlanning(context.Background(), req, nil, cfg)
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected InvalidArgument on nil resolver, got %v", err)
	}
}

func TestExecutePlanning_FallbackToNextViableEndpointOnResolutionError(t *testing.T) {
	// First endpoint (ep-local-01) errors on resolution; second endpoint (ep-cli-01) succeeds
	content := validPlannerOutput(protocol.IntentMinimumSpend)
	drv := newRec(okHandler(content))

	resolver := &testResolver{
		drivers: map[string]drivers.SessionDriver{
			"ep-cli-01": drv,
		},
		models: map[string]string{
			"ep-cli-01": "claude-3-7-sonnet",
		},
		errs: map[string]error{
			"ep-local-01": errs.New(errs.CategoryInternal, "connection refused"),
		},
	}

	req := makeBasePlannerRequest()
	cfg := ExecutionConfig{Timeout: 5 * time.Second}

	res, err := ExecutePlanning(context.Background(), req, resolver, cfg)
	if err != nil {
		t.Fatalf("ExecutePlanning failed: %v", err)
	}
	if res == nil || res.Outcome != planner.OutcomeRecommended {
		t.Fatalf("expected OutcomeRecommended on fallback endpoint, got: %+v", res)
	}
	if len(res.Accepted) != 1 || res.Accepted[0].Planner.EndpointID != "ep-cli-01" {
		t.Fatalf("expected plan bound to fallback endpoint ep-cli-01, got: %+v", res.Accepted[0].Planner)
	}
}

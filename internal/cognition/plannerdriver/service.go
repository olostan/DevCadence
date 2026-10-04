package plannerdriver

import (
	"context"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/cognition/planner"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// DriverResolver resolves a session driver and model identifier for a given endpoint (WP-M3D-1C2).
type DriverResolver interface {
	ResolveDriver(ctx context.Context, endpointID string) (drivers.SessionDriver, string, error)
}

// ExecutionConfig configures the planning execution pipeline.
type ExecutionConfig struct {
	Timeout          time.Duration
	IncludeErrorText bool
}

// ExecutePlanning selects an eligible cognition endpoint from the inventory, binds its driver
// into a planner Invoker, and executes portfolio planning with graceful degradation (DCI-104).
func ExecutePlanning(ctx context.Context, req planner.Request, resolver DriverResolver, cfg ExecutionConfig) (*planner.Result, error) {
	if req.Inventory == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "plannerdriver: inventory is required")
	}
	if resolver == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "plannerdriver: resolver is required")
	}

	var selectedInvoker *Invoker
	for _, ep := range req.Inventory.CognitionEndpoints {
		if !protocol.EndpointViable(ep.Kind, ep.Locality, ep.Health, ep.Auth) {
			continue
		}
		driver, modelID, err := resolver.ResolveDriver(ctx, ep.ID)
		if err != nil || driver == nil {
			continue
		}
		if driver.Capabilities().NativeWorktreeAccess {
			continue
		}
		inv, err := NewInvoker(driver, Config{
			EndpointID:       ep.ID,
			ModelID:          modelID,
			Timeout:          cfg.Timeout,
			IncludeErrorText: cfg.IncludeErrorText,
		})
		if err != nil {
			continue
		}
		selectedInvoker = inv
		break
	}

	if selectedInvoker == nil {
		req.Invoker = nil
		return planner.Plan(ctx, req)
	}

	req.Invoker = selectedInvoker
	return planner.Plan(ctx, req)
}

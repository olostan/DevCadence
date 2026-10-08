// Package plannerdriver adapts a drivers.SessionDriver to the planner.Invoker
// interface. It runs one planner prompt as a single tool-less, worktree-less
// turn and reports Go-known provenance. It selects no endpoint, retries
// nothing and activates nothing (WP-M3D-1C1; DCI-054/055, DCI-124).
package plannerdriver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/cognition/planner"
	"github.com/olostan/DevCadence/internal/errs"
)

const (
	defaultSessionIDPrefix = "planner"
	turnID                 = "planner-turn-1"
	maxPausedReasonBytes   = 64
)

var _ planner.Invoker = (*Invoker)(nil)

// Config binds the endpoint and model a planner call uses. Values are passed
// verbatim; the adapter never normalizes or derives them.
type Config struct {
	EndpointID             string
	ModelID                string
	SessionIDPrefix        string
	Timeout                time.Duration
	IncludeErrorText       bool
	MaxOutputTokensPerCall int64
}

// Invoker is a planner.Invoker backed by a drivers.SessionDriver.
type Invoker struct {
	driver  drivers.SessionDriver
	cfg     Config
	counter atomic.Uint64
}

// NewInvoker validates cfg and returns an Invoker. A driver declaring native
// worktree access is rejected because it cannot be made tool-less by
// configuration.
func NewInvoker(driver drivers.SessionDriver, cfg Config) (*Invoker, error) {
	if driver == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "plannerdriver: driver is required")
	}
	if strings.TrimSpace(cfg.EndpointID) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "plannerdriver: endpoint id is required")
	}
	if strings.TrimSpace(cfg.ModelID) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "plannerdriver: model id is required")
	}
	if cfg.Timeout < 0 {
		return nil, errs.New(errs.CategoryInvalidArgument, "plannerdriver: timeout must not be negative")
	}
	if driver.Capabilities().NativeWorktreeAccess {
		return nil, errs.New(errs.CategoryInvalidArgument, "plannerdriver: driver declares native worktree access")
	}
	if cfg.SessionIDPrefix == "" {
		cfg.SessionIDPrefix = defaultSessionIDPrefix
	}
	return &Invoker{driver: driver, cfg: cfg}, nil
}

// Invoke runs inv.Prompt as one turn on a fresh session and closes the session.
func (i *Invoker) Invoke(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
	if err := ctx.Err(); err != nil {
		return planner.InvocationResult{}, err
	}
	n := i.counter.Add(1)
	sessionID := fmt.Sprintf("%s-%d", i.cfg.SessionIDPrefix, n)

	callCtx := ctx
	if i.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, i.cfg.Timeout)
		defer cancel()
	}

	maxTokens := i.cfg.MaxOutputTokensPerCall
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	session, err := i.driver.StartSession(callCtx, drivers.SessionConfig{
		SessionID:              sessionID,
		ModelID:                i.cfg.ModelID,
		MaxOutputTokensPerCall: maxTokens,
	})
	if err != nil {
		return planner.InvocationResult{}, i.driverError(callCtx, "start", err)
	}
	defer i.closeSession(ctx, session)

	turn, err := session.ExecuteTurn(callCtx, drivers.TurnInput{TurnID: turnID, Prompt: inv.Prompt})
	if cerr := callCtx.Err(); cerr != nil {
		return planner.InvocationResult{}, cerr
	}
	if err != nil {
		return planner.InvocationResult{}, i.driverError(callCtx, "execute", err)
	}
	if len(turn.ToolCalls) > 0 {
		return planner.InvocationResult{}, errs.New(errs.CategoryInvalidArgument, "planner session returned tool calls")
	}
	if turn.PausedReason != "" {
		msg := "planner session paused"
		if i.cfg.IncludeErrorText {
			msg += ": " + truncateUTF8(turn.PausedReason, maxPausedReasonBytes)
		}
		return planner.InvocationResult{}, errs.New(errs.CategoryInvalidTransition, "%s", msg)
	}
	if session.Status() != drivers.SessionStatusActive {
		return planner.InvocationResult{}, errs.New(errs.CategoryInvalidTransition, "planner session not active")
	}
	return planner.InvocationResult{
		Content:    turn.Content,
		EndpointID: i.cfg.EndpointID,
		DriverID:   session.DriverID(),
		ModelID:    i.cfg.ModelID,
	}, nil
}

// closeSession closes on a context that survives caller cancellation, bounded
// by the configured timeout.
func (i *Invoker) closeSession(ctx context.Context, session drivers.Session) {
	closeCtx := context.WithoutCancel(ctx)
	if i.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		closeCtx, cancel = context.WithTimeout(closeCtx, i.cfg.Timeout)
		defer cancel()
	}
	_ = session.Close(closeCtx)
}

func (i *Invoker) driverError(callCtx context.Context, stage string, err error) error {
	if cerr := callCtx.Err(); cerr != nil {
		return cerr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if i.cfg.IncludeErrorText {
		return errs.New(errs.CategoryOf(err), "planner driver call failed: %s: %s", stage, err.Error())
	}
	return errs.New(errs.CategoryOf(err), "planner driver call failed: %s", stage)
}

// truncateUTF8 cuts s to at most max bytes without splitting a rune.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

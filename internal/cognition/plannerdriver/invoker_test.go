package plannerdriver

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/cognition/planner"
	"github.com/olostan/DevCadence/internal/errs"
)

const turnKey = "planner-turn-1"

// recDriver records every session the wrapped driver hands out. The fake
// driver has no session accessor and ResumeSession would reopen a closed
// session, so closure is observed through the recorded Session values.
type recDriver struct {
	inner    *drivers.FakeDriver
	wrap     func(drivers.Session) drivers.Session
	startErr func(ctx context.Context) error
	mu       sync.Mutex
	sessions []drivers.Session
	starts   []string
}

func newRec(handler func(ctx context.Context, in drivers.TurnInput) (drivers.TurnResult, error)) *recDriver {
	f := drivers.NewFakeDriver("drv-fake")
	if handler != nil {
		f.SetTurnHandler(turnKey, handler)
	}
	return &recDriver{inner: f}
}

func (r *recDriver) ID() string                               { return r.inner.ID() }
func (r *recDriver) Capabilities() drivers.DriverCapabilities { return r.inner.Capabilities() }
func (r *recDriver) StartSession(ctx context.Context, cfg drivers.SessionConfig) (drivers.Session, error) {
	r.mu.Lock()
	r.starts = append(r.starts, cfg.SessionID)
	r.mu.Unlock()
	if r.startErr != nil {
		if err := r.startErr(ctx); err != nil {
			return nil, err
		}
	}
	s, err := r.inner.StartSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if r.wrap != nil {
		s = r.wrap(s)
	}
	r.mu.Lock()
	r.sessions = append(r.sessions, s)
	r.mu.Unlock()
	return s, nil
}
func (r *recDriver) ResumeSession(ctx context.Context, id string, cfg drivers.SessionConfig) (drivers.Session, error) {
	return r.inner.ResumeSession(ctx, id, cfg)
}

func (r *recDriver) only(t *testing.T) drivers.Session {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sessions) != 1 {
		t.Fatalf("recorded %d sessions, want 1", len(r.sessions))
	}
	return r.sessions[0]
}

func (r *recDriver) assertAllClosed(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sessions) == 0 {
		t.Fatalf("no session was started; closure check would be vacuous")
	}
	for _, s := range r.sessions {
		if st := rawStatus(s); st != drivers.SessionStatusClosed {
			t.Fatalf("session %s status %q, want closed", s.ID(), st)
		}
	}
}

// statusSession overrides Status() after the turn; rawStatus bypasses it.
type statusSession struct {
	drivers.Session
	after drivers.SessionStatus
	done  bool
	mu    sync.Mutex
}

func (s *statusSession) ExecuteTurn(ctx context.Context, in drivers.TurnInput) (drivers.TurnResult, error) {
	res, err := s.Session.ExecuteTurn(ctx, in)
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
	return res, err
}

func (s *statusSession) Status() drivers.SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return s.after
	}
	return s.Session.Status()
}

func rawStatus(s drivers.Session) drivers.SessionStatus {
	if ss, ok := s.(*statusSession); ok {
		return ss.Session.Status()
	}
	return s.Status()
}

func okHandler(content string) func(context.Context, drivers.TurnInput) (drivers.TurnResult, error) {
	return func(_ context.Context, in drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{TurnID: in.TurnID, Content: content}, nil
	}
}

func mustInvoker(t *testing.T, d drivers.SessionDriver, cfg Config) *Invoker {
	t.Helper()
	if cfg.EndpointID == "" {
		cfg.EndpointID = "ep-1"
	}
	if cfg.ModelID == "" {
		cfg.ModelID = "m-1"
	}
	inv, err := NewInvoker(d, cfg)
	if err != nil {
		t.Fatalf("NewInvoker: %v", err)
	}
	return inv
}

func TestPlannerDriverInvoker_ACC01_ResultProvenanceAndClose(t *testing.T) {
	d := newRec(okHandler("OK"))
	inv := mustInvoker(t, d, Config{})
	res, err := inv.Invoke(context.Background(), planner.Invocation{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	want := planner.InvocationResult{Content: "OK", EndpointID: "ep-1", DriverID: "drv-fake", ModelID: "m-1"}
	if res != want {
		t.Fatalf("got %+v want %+v", res, want)
	}
	d.assertAllClosed(t)
}

func TestPlannerDriverInvoker_ACC02_ToollessSessionAndVerbatimPrompt(t *testing.T) {
	var gotIn drivers.TurnInput
	d := newRec(func(_ context.Context, in drivers.TurnInput) (drivers.TurnResult, error) {
		gotIn = in
		return drivers.TurnResult{Content: "OK"}, nil
	})
	inv := mustInvoker(t, d, Config{ModelID: " m-1 ", EndpointID: " ep-1 "})
	prompt := "  exact prompt\nwith  spacing  "
	res, err := inv.Invoke(context.Background(), planner.Invocation{Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	if gotIn.Prompt != prompt || gotIn.TurnID != turnKey || len(gotIn.ToolResults) != 0 {
		t.Fatalf("turn input %+v", gotIn)
	}
	if res.ModelID != " m-1 " || res.EndpointID != " ep-1 " {
		t.Fatalf("ids must be verbatim, got %+v", res)
	}
	cfg := d.only(t).Config()
	if cfg.ModelID != " m-1 " {
		t.Fatalf("model %q", cfg.ModelID)
	}
	if cfg.Tools != nil || cfg.WorktreeScope != nil || cfg.Mediator != nil || cfg.Options != nil || cfg.SystemPrompt != "" {
		t.Fatalf("session config not tool-less: %+v", cfg)
	}
}

func TestPlannerDriverInvoker_ACC03_FreshSessionIDsAndPrefix(t *testing.T) {
	d := newRec(okHandler("OK"))
	inv := mustInvoker(t, d, Config{})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := inv.Invoke(cancelled, planner.Invocation{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled: %v", err)
	}
	if len(d.starts) != 0 {
		t.Fatalf("pre-cancelled call started a session: %v", d.starts)
	}
	for i := 0; i < 2; i++ {
		if _, err := inv.Invoke(context.Background(), planner.Invocation{}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(d.starts, []string{"planner-1", "planner-2"}) {
		t.Fatalf("ids %v", d.starts)
	}
	d2 := newRec(okHandler("OK"))
	inv2 := mustInvoker(t, d2, Config{SessionIDPrefix: "x"})
	if _, err := inv2.Invoke(context.Background(), planner.Invocation{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d2.starts, []string{"x-1"}) {
		t.Fatalf("ids %v", d2.starts)
	}
}

func TestPlannerDriverInvoker_ACC04_StartFailure(t *testing.T) {
	d := newRec(okHandler("OK"))
	// Occupy planner-1 so the adapter's first StartSession conflicts.
	if _, err := d.inner.StartSession(context.Background(), drivers.SessionConfig{SessionID: "planner-1", ModelID: "m", MaxOutputTokensPerCall: 4096}); err != nil {
		t.Fatal(err)
	}
	inv := mustInvoker(t, d, Config{})
	_, err := inv.Invoke(context.Background(), planner.Invocation{})
	if err == nil || errs.CategoryOf(err) != errs.CategoryConflict {
		t.Fatalf("want conflict, got %v", err)
	}
	if err.Error() == "" || !strings.Contains(err.Error(), "planner driver call failed: start") {
		t.Fatalf("stage missing: %v", err)
	}
	if strings.Contains(err.Error(), "already exists") {
		t.Fatalf("driver text leaked: %v", err)
	}
	// The failed start consumed id 1; the next call uses planner-2.
	if _, err := inv.Invoke(context.Background(), planner.Invocation{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.starts, []string{"planner-1", "planner-2"}) {
		t.Fatalf("ids %v", d.starts)
	}
	withText := mustInvoker(t, d, Config{IncludeErrorText: true, SessionIDPrefix: "planner"})
	// counter restarts at 1 for a new Invoker: planner-1 conflicts again.
	_, err = withText.Invoke(context.Background(), planner.Invocation{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("flag should include driver text: %v", err)
	}
}

func TestPlannerDriverInvoker_ACC05_ExecuteErrorHygiene(t *testing.T) {
	const secret = "sk-secret-token-123"
	h := func(context.Context, drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{}, errs.New(errs.CategoryModelUnavailable, "boom %s", secret)
	}
	d := newRec(h)
	inv := mustInvoker(t, d, Config{})
	_, err := inv.Invoke(context.Background(), planner.Invocation{})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("secret leaked or no error: %v", err)
	}
	if errs.CategoryOf(err) != errs.CategoryModelUnavailable || !strings.Contains(err.Error(), "planner driver call failed: execute") {
		t.Fatalf("category/stage: %v", err)
	}
	d.assertAllClosed(t)

	d2 := newRec(h)
	inv2 := mustInvoker(t, d2, Config{IncludeErrorText: true})
	_, err = inv2.Invoke(context.Background(), planner.Invocation{})
	if err == nil || !strings.Contains(err.Error(), secret) || errs.CategoryOf(err) != errs.CategoryModelUnavailable {
		t.Fatalf("flag should include text: %v", err)
	}
	d2.assertAllClosed(t)

	// A foreign error maps to Internal.
	d3 := newRec(func(context.Context, drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{}, errors.New("plain " + secret)
	})
	_, err = mustInvoker(t, d3, Config{}).Invoke(context.Background(), planner.Invocation{})
	if errs.CategoryOf(err) != errs.CategoryInternal || strings.Contains(err.Error(), secret) {
		t.Fatalf("foreign error: %v", err)
	}
}

func TestPlannerDriverInvoker_ACC06_CancelledContextRawAndClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := newRec(func(c context.Context, _ drivers.TurnInput) (drivers.TurnResult, error) {
		cancel()
		return drivers.TurnResult{}, errs.New(errs.CategoryInternal, "wrapped: %v", c.Err())
	})
	_, err := mustInvoker(t, d, Config{}).Invoke(ctx, planner.Invocation{})
	if err != context.Canceled {
		t.Fatalf("want raw context.Canceled, got %#v", err)
	}
	d.assertAllClosed(t)

	// Success returned after the caller cancelled is still the raw ctx error.
	ctx2, cancel2 := context.WithCancel(context.Background())
	d2 := newRec(func(context.Context, drivers.TurnInput) (drivers.TurnResult, error) {
		cancel2()
		return drivers.TurnResult{Content: "late"}, nil
	})
	_, err = mustInvoker(t, d2, Config{}).Invoke(ctx2, planner.Invocation{})
	if err != context.Canceled {
		t.Fatalf("want raw context.Canceled, got %#v", err)
	}
	d2.assertAllClosed(t)
}

func TestPlannerDriverInvoker_ACC07_ToolCallsRejected(t *testing.T) {
	d := newRec(func(context.Context, drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{Content: "x", ToolCalls: []drivers.ToolCall{{ID: "c", Name: "rm"}}}, nil
	})
	_, err := mustInvoker(t, d, Config{}).Invoke(context.Background(), planner.Invocation{})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument || !strings.Contains(err.Error(), "planner session returned tool calls") {
		t.Fatalf("got %v", err)
	}
	d.assertAllClosed(t)
}

func TestPlannerDriverInvoker_ACC08_PausedRejectedAndTruncated(t *testing.T) {
	const reason = "quota exhausted for tenant acme"
	h := func(context.Context, drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{Content: "x", PausedReason: reason}, nil
	}
	d := newRec(h)
	_, err := mustInvoker(t, d, Config{}).Invoke(context.Background(), planner.Invocation{})
	if err == nil || err.Error() == "" || strings.Contains(err.Error(), "quota") || !strings.Contains(err.Error(), "planner session paused") {
		t.Fatalf("got %v", err)
	}
	d.assertAllClosed(t)

	d2 := newRec(h)
	_, err = mustInvoker(t, d2, Config{IncludeErrorText: true}).Invoke(context.Background(), planner.Invocation{})
	if err == nil || !strings.Contains(err.Error(), "planner session paused: "+reason) {
		t.Fatalf("flag should append reason: %v", err)
	}
	d2.assertAllClosed(t)

	// Truncation: 70 bytes of 3-byte runes; cut at 64 must land on a rune boundary (63).
	long := strings.Repeat("€", 24) // 72 bytes
	d3 := newRec(func(context.Context, drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{PausedReason: long}, nil
	})
	_, err = mustInvoker(t, d3, Config{IncludeErrorText: true}).Invoke(context.Background(), planner.Invocation{})
	if err == nil {
		t.Fatal("want error")
	}
	_, tail, _ := strings.Cut(err.Error(), "planner session paused: ")
	if tail != strings.Repeat("€", 21) {
		t.Fatalf("tail %q (%d bytes)", tail, len(tail))
	}
	if got := truncateUTF8("abc", 64); got != "abc" {
		t.Fatalf("short string changed: %q", got)
	}
	if got := truncateUTF8(strings.Repeat("a", 100), 64); len(got) != 64 {
		t.Fatalf("ascii cut %d", len(got))
	}
}

func TestPlannerDriverInvoker_ACC06_StartStageContextErrorsAreRaw(t *testing.T) {
	// StartSession returns its own wrapped error after the caller cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	d := newRec(okHandler("OK"))
	d.startErr = func(context.Context) error {
		cancel()
		return errs.New(errs.CategoryInternal, "start wrapped")
	}
	if _, err := mustInvoker(t, d, Config{}).Invoke(ctx, planner.Invocation{}); err != context.Canceled {
		t.Fatalf("cancelled during start: %#v", err)
	}
	// The driver returns a bare context error although the call context is live.
	d2 := newRec(okHandler("OK"))
	d2.startErr = func(context.Context) error { return context.DeadlineExceeded }
	if _, err := mustInvoker(t, d2, Config{}).Invoke(context.Background(), planner.Invocation{}); err != context.DeadlineExceeded {
		t.Fatalf("driver ctx error must pass through raw: %#v", err)
	}
}

// closeCtxSession records the state of the context passed to Close.
type closeCtxSession struct {
	drivers.Session
	mu       sync.Mutex
	closeErr error
	closed   bool
}

func (s *closeCtxSession) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closeErr, s.closed = ctx.Err(), true
	s.mu.Unlock()
	return s.Session.Close(ctx)
}

func TestPlannerDriverInvoker_ACC09_CloseUsesLiveContext(t *testing.T) {
	for name, run := range map[string]func() (context.Context, Config){
		"timeout": func() (context.Context, Config) { return context.Background(), Config{Timeout: 10 * time.Millisecond} },
		"cancel": func() (context.Context, Config) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(10*time.Millisecond, cancel)
			return ctx, Config{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := newRec(func(c context.Context, _ drivers.TurnInput) (drivers.TurnResult, error) {
				<-c.Done()
				return drivers.TurnResult{}, c.Err()
			})
			var cs *closeCtxSession
			d.wrap = func(s drivers.Session) drivers.Session { cs = &closeCtxSession{Session: s}; return cs }
			ctx, cfg := run()
			if _, err := mustInvoker(t, d, cfg).Invoke(ctx, planner.Invocation{}); err == nil {
				t.Fatal("want ctx error")
			}
			cs.mu.Lock()
			defer cs.mu.Unlock()
			if !cs.closed || cs.closeErr != nil {
				t.Fatalf("closed=%v close ctx err=%v; Close must get a live context", cs.closed, cs.closeErr)
			}
		})
	}
}

func TestPlannerDriverInvoker_ACC09_TimeoutIsRawDeadline(t *testing.T) {
	t.Run("handler honors ctx", func(t *testing.T) {
		d := newRec(func(c context.Context, _ drivers.TurnInput) (drivers.TurnResult, error) {
			<-c.Done()
			return drivers.TurnResult{}, c.Err()
		})
		_, err := mustInvoker(t, d, Config{Timeout: 20 * time.Millisecond}).Invoke(context.Background(), planner.Invocation{})
		if err != context.DeadlineExceeded {
			t.Fatalf("got %#v", err)
		}
		d.assertAllClosed(t)
	})
	t.Run("handler ignores ctx and succeeds late", func(t *testing.T) {
		d := newRec(func(context.Context, drivers.TurnInput) (drivers.TurnResult, error) {
			time.Sleep(60 * time.Millisecond)
			return drivers.TurnResult{Content: "late"}, nil
		})
		_, err := mustInvoker(t, d, Config{Timeout: 10 * time.Millisecond}).Invoke(context.Background(), planner.Invocation{})
		if err != context.DeadlineExceeded {
			t.Fatalf("got %#v", err)
		}
		d.assertAllClosed(t)
	})
	t.Run("non-ctx error after deadline", func(t *testing.T) {
		d := newRec(func(context.Context, drivers.TurnInput) (drivers.TurnResult, error) {
			time.Sleep(60 * time.Millisecond)
			return drivers.TurnResult{}, errs.New(errs.CategoryInternal, "late failure")
		})
		_, err := mustInvoker(t, d, Config{Timeout: 10 * time.Millisecond}).Invoke(context.Background(), planner.Invocation{})
		if err != context.DeadlineExceeded {
			t.Fatalf("got %#v", err)
		}
	})
	t.Run("session closed even though call ctx expired", func(t *testing.T) {
		// The close context must not be the expired call context; the fake
		// ignores ctx on Close, so assert the recorded session reached closed.
		d := newRec(func(c context.Context, _ drivers.TurnInput) (drivers.TurnResult, error) {
			<-c.Done()
			return drivers.TurnResult{}, c.Err()
		})
		_, _ = mustInvoker(t, d, Config{Timeout: 10 * time.Millisecond}).Invoke(context.Background(), planner.Invocation{})
		d.assertAllClosed(t)
	})
}

func TestPlannerDriverInvoker_ACC10_NewInvokerValidation(t *testing.T) {
	good := drivers.NewFakeDriver("d")
	caps := drivers.NewFakeDriver("d").Capabilities()
	caps.NativeWorktreeAccess = true
	native := drivers.NewFakeDriver("n", drivers.FakeDriverOptions{Capabilities: &caps})
	cases := map[string]struct {
		d   drivers.SessionDriver
		cfg Config
	}{
		"nil driver":     {nil, Config{EndpointID: "e", ModelID: "m"}},
		"blank endpoint": {good, Config{EndpointID: "  ", ModelID: "m"}},
		"blank model":    {good, Config{EndpointID: "e", ModelID: "\t"}},
		"neg timeout":    {good, Config{EndpointID: "e", ModelID: "m", Timeout: -1}},
		"native access":  {native, Config{EndpointID: "e", ModelID: "m"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			inv, err := NewInvoker(c.d, c.cfg)
			if inv != nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
				t.Fatalf("got %v, %v", inv, err)
			}
		})
	}
}

func TestPlannerDriverInvoker_ACC11_ConcurrentInvokes(t *testing.T) {
	d := newRec(okHandler("OK"))
	inv := mustInvoker(t, d, Config{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := inv.Invoke(context.Background(), planner.Invocation{Prompt: "p"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	d.mu.Lock()
	seen := map[string]bool{}
	for _, id := range d.starts {
		seen[id] = true
	}
	d.mu.Unlock()
	if len(seen) != 16 {
		t.Fatalf("distinct ids %d", len(seen))
	}
	d.assertAllClosed(t)
}

func TestPlannerDriverInvoker_ACC12_PlanEndToEnd(t *testing.T) {
	d := newRec(okHandler(allAlts()))
	inv := mustInvoker(t, d, Config{EndpointID: "ep-planner", ModelID: "model-x"})
	res, err := planner.Plan(context.Background(), planner.Request{
		Inventory:       makeTestInventory(),
		MachineProfile:  makeTestMachineProfile(),
		ContextProfiles: makeTestContextProfiles(),
		Invoker:         inv,
		Clock:           clock.NewFake(time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC), time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != planner.OutcomeRecommended || len(res.Accepted) == 0 {
		t.Fatalf("outcome %q detail %q rejected %+v", res.Outcome, res.Detail, res.Rejected)
	}
	for _, rec := range res.Accepted {
		p := rec.Planner
		if p == nil || p.EndpointID != "ep-planner" || p.DriverID != "drv-fake" || p.ModelID != "model-x" {
			t.Fatalf("provenance %+v", p)
		}
	}
	d.assertAllClosed(t)
}

func TestPlannerDriverInvoker_ACC14_SessionNotActiveAfterTurn(t *testing.T) {
	for _, st := range []drivers.SessionStatus{
		drivers.SessionStatusPausedBudgetExceeded, drivers.SessionStatusError, drivers.SessionStatusClosed,
	} {
		t.Run(string(st), func(t *testing.T) {
			d := newRec(okHandler("OK"))
			d.wrap = func(s drivers.Session) drivers.Session { return &statusSession{Session: s, after: st} }
			_, err := mustInvoker(t, d, Config{}).Invoke(context.Background(), planner.Invocation{})
			if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidTransition || !strings.Contains(err.Error(), "planner session not active") {
				t.Fatalf("got %v", err)
			}
			d.assertAllClosed(t)
		})
	}
}

func TestPlannerDriverInvoker_EmptyContentReturnedAsIs(t *testing.T) {
	d := newRec(okHandler(""))
	res, err := mustInvoker(t, d, Config{}).Invoke(context.Background(), planner.Invocation{})
	if err != nil || res.Content != "" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

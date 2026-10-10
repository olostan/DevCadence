package flightrec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/flightrec/journal"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
	"github.com/olostan/DevCadence/internal/protocol"
)

type testKey struct{}

func id(prefix string, n int) string { return fmt.Sprintf("%s_%026d", prefix, n) }

var digestA = "sha256:" + strings.Repeat("ab", 32)

func mustStart(t *testing.T, r *Recorder, ctx context.Context, spec StartSpec) (context.Context, *Op) {
	t.Helper()
	nctx, op, err := r.Start(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	return nctx, op
}

// B-3: the recorder works over an in-memory Sink; ids and times are exact.
func TestStartAndEndRecordsExactly(t *testing.T) {
	sink := &memSink{}
	r := newRec(t, sink)
	spec := StartSpec{
		Name: "setup.discover.environment", ActorID: "actor-1", TaskID: "task_1", AttemptID: "att_1", CanonicalEventID: id("evt", 7),
		Links:     []Link{{Relation: RelationCausedBy, TraceID: id("trc", 9), OperationID: id("op", 9), EventID: id("evt", 9)}},
		Metadata:  map[string]any{"k": "v"},
		Artifacts: []ArtifactRef{{ID: "a1", Kind: "probe", Locator: "/x/y", MediaType: "text/plain", Digest: digestA, SizeBytes: 3}},
	}
	ctx, op := mustStart(t, r, context.Background(), spec)
	if op.ID() != id("op", 1) || op.TraceID() != id("trc", 1) {
		t.Fatalf("ids %s %s", op.ID(), op.TraceID())
	}
	sc, ok := FromContext(ctx)
	want := SpanContext{TraceID: id("trc", 1), OperationID: id("op", 1), NodeID: nodeID(), RuntimeID: runID(), StreamID: StreamMain}
	if !ok || sc != want {
		t.Fatalf("span context %+v", sc)
	}
	op.SetResult("files", 3)
	op.SetUsage(10, 20, time.Second)
	op.AddArtifact(ArtifactRef{ID: "r1", Kind: "out", Locator: "/o", Digest: digestA})
	if err := op.End(OutcomeCompleted, nil); err != nil {
		t.Fatal(err)
	}
	recs := sink.records()
	if len(recs) != 2 || sink.class[0] != Diagnostic || sink.class[1] != Diagnostic {
		t.Fatalf("records %d", len(recs))
	}
	s := recs[0].Start
	if s.TraceID != id("trc", 1) || s.OperationID != id("op", 1) || s.ParentOperationID != "" || s.OperationName != spec.Name ||
		s.ActorID != "actor-1" || s.TaskID != "task_1" || s.AttemptID != "att_1" || s.CanonicalEventID != id("evt", 7) ||
		string(s.JSONMetadata) != `{"k":"v"}` || s.Sanitization != nil {
		t.Fatalf("start %+v", s)
	}
	if s.At.WallUnixNanos != t0.UnixNano() || s.At.MonoNanos != 1 {
		t.Fatalf("start stamp %+v", s.At)
	}
	if l := s.Links; len(l) != 1 || l[0].Relation != "caused_by" || l[0].TraceID != id("trc", 9) || l[0].OperationID != id("op", 9) || l[0].EventID != id("evt", 9) {
		t.Fatalf("links %+v", s.Links)
	}
	if len(s.Artifacts) != 1 || s.Artifacts[0].Digest != digestA || s.Artifacts[0].DigestVerified || s.Artifacts[0].SizeBytes != 3 {
		t.Fatalf("artifacts %+v", s.Artifacts)
	}
	e := recs[1].End
	if e.OperationID != id("op", 1) || e.Outcome != wire.OutcomeCompleted || e.ErrorCode != "" || e.ErrorSummary != "" ||
		string(e.JSONResult) != `{"files":3}` || e.Usage.InputTokens != 10 || e.Usage.OutputTokens != 20 || e.Usage.DurationNanos != uint64(time.Second) ||
		len(e.Artifacts) != 1 || e.Artifacts[0].ID != "r1" {
		t.Fatalf("end %+v", e)
	}
	if e.At.WallUnixNanos != t0.Add(time.Microsecond).UnixNano() || e.At.MonoNanos != 2 {
		t.Fatalf("end stamp %+v", e.At)
	}
}

// B-2: the context holds only the immutable SpanContext, and cancellation and
// deadline of the parent are honored by the derived context.
func TestContextHygiene(t *testing.T) {
	r := newRec(t, &memSink{})
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, op := mustStart(t, r, parent, StartSpec{Name: "a.b"})
	if got, want := fmt.Sprint(ctx), "context.Background.WithCancel.WithValue(flightrec.spanKey, flightrec.SpanContext)"; got != want {
		t.Fatalf("context chain %q, want %q", got, want)
	}
	deadlined, dcancel := context.WithDeadline(context.Background(), t0.Add(time.Hour))
	defer dcancel()
	dctx, _ := mustStart(t, r, deadlined, StartSpec{Name: "a.b"})
	if dl, ok := dctx.Deadline(); !ok || !dl.Equal(t0.Add(time.Hour)) {
		t.Fatalf("deadline %v %v", dl, ok)
	}
	sc, _ := FromContext(ctx)
	sc.TraceID = "tampered" // a copy: the context is unaffected
	if again, _ := FromContext(ctx); again.TraceID != op.TraceID() {
		t.Fatalf("context mutated: %+v", again)
	}
	select {
	case <-ctx.Done():
		t.Fatal("derived context done before parent")
	default:
	}
	cancel()
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal(ctx.Err())
	}
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("empty context must not carry a span")
	}
}

// B-1: 32 goroutines Start under one parent context.
func TestConcurrentChildren(t *testing.T) {
	r, dir := journalRec(t)
	ctx, parent := mustStart(t, r, context.Background(), StartSpec{Name: "parent.op"})
	const n = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, op, err := r.Start(ctx, StartSpec{Name: "child.op"})
			if err != nil {
				t.Error(err)
				return
			}
			if err := op.End(OutcomeCompleted, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if err := parent.End(OutcomeCompleted, nil); err != nil {
		t.Fatal(err)
	}
	flushRec(t, r)
	recs := scanDir(t, dir) // also asserts a gapless, clean sequence
	starts := bodies(recs, wire.RecordTypeOperationStart)
	if len(starts) != n+1 {
		t.Fatalf("starts %d", len(starts))
	}
	seen := map[string]bool{}
	for _, rec := range starts {
		s := rec.Start
		seen[s.OperationID] = true
		if s.TraceID != parent.TraceID() {
			t.Fatalf("trace %s", s.TraceID)
		}
		if s.OperationName == "child.op" && s.ParentOperationID != parent.ID() {
			t.Fatalf("parent %s", s.ParentOperationID)
		}
	}
	if len(seen) != n+1 {
		t.Fatalf("distinct operation ids %d", len(seen))
	}
	if len(bodies(recs, wire.RecordTypeOperationEnd)) != n+1 {
		t.Fatal("missing ENDs")
	}
}

func TestRemoteContext(t *testing.T) {
	sink := &memSink{}
	r := newRec(t, sink)
	bad := SpanContext{TraceID: "nope"}
	ctx, err := WithRemote(context.Background(), bad)
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument || ctx != context.Background() {
		t.Fatalf("%v", err)
	}
	for _, sc := range []SpanContext{{StreamID: "Bad Stream"}, {ParentOperationID: "x"}} {
		if _, err := WithRemote(context.Background(), sc); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Fatalf("%+v: %v", sc, err)
		}
	}
	remote := SpanContext{TraceID: id("trc", 50), OperationID: id("op", 50), NodeID: id("nod", 50), RuntimeID: id("run", 50), StreamID: "other"}
	rctx, err := WithRemote(context.Background(), remote)
	if err != nil {
		t.Fatal(err)
	}
	lctx, op := mustStart(t, r, rctx, StartSpec{Name: "local.op"})
	s := sink.records()[0].Start
	if s.TraceID != remote.TraceID || s.ParentOperationID != remote.OperationID {
		t.Fatalf("start %+v", s)
	}
	local, _ := FromContext(lctx)
	if local.NodeID != nodeID() || local.RuntimeID != runID() || local.StreamID != StreamMain || local.ParentOperationID != remote.OperationID || op.TraceID() != remote.TraceID {
		t.Fatalf("local identity %+v", local)
	}
	// A remote context without a trace id starts a new local trace.
	rctx, _ = WithRemote(context.Background(), SpanContext{OperationID: id("op", 51)})
	_, op2 := mustStart(t, r, rctx, StartSpec{Name: "local.op"})
	if op2.TraceID() == "" || op2.TraceID() == remote.TraceID {
		t.Fatalf("trace %s", op2.TraceID())
	}
}

func TestSpanContextText(t *testing.T) {
	sc := SpanContext{TraceID: id("trc", 1), OperationID: id("op", 2), NodeID: id("nod", 3), RuntimeID: id("run", 4), StreamID: "main"}
	b, err := sc.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	want := "dc1;trc=" + id("trc", 1) + ";op=" + id("op", 2) + ";nod=" + id("nod", 3) + ";run=" + id("run", 4) + ";stm=main"
	if string(b) != want {
		t.Fatalf("got %s", b)
	}
	back, err := ParseSpanContext(string(b))
	if err != nil || back != sc {
		t.Fatalf("%+v %v", back, err)
	}
	// Empty ids are omitted; the parent id is not part of the wire form.
	sc2 := SpanContext{TraceID: id("trc", 1), ParentOperationID: id("op", 8)}
	if b, _ = sc2.MarshalText(); string(b) != "dc1;trc="+id("trc", 1) {
		t.Fatalf("got %s", b)
	}
	if _, err := (SpanContext{TraceID: "bad"}).MarshalText(); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatal(err)
	}
	long := SpanContext{TraceID: strings.Repeat("a", 300) + "_" + strings.Repeat("0", 26), OperationID: strings.Repeat("b", 300) + "_" + strings.Repeat("0", 26)}
	if _, err := long.MarshalText(); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatal(err)
	}
	for _, in := range []string{
		strings.Repeat("x", 513), "dc2;trc=" + id("trc", 1), "trc=" + id("trc", 1), "dc1;trc", "dc1;trc=", "dc1;xyz=" + id("trc", 1),
		"dc1;trc=" + id("trc", 1) + ";trc=" + id("trc", 2), "dc1;trc=bad", "dc1;stm=Not Valid",
	} {
		if _, err := ParseSpanContext(in); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("%q accepted: %v", in, err)
		}
	}
	if sc, err := ParseSpanContext("dc1"); err != nil || sc != (SpanContext{}) {
		t.Fatalf("%+v %v", sc, err)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	good := Config{Sink: &memSink{}, NodeID: nodeID(), RuntimeID: runID(), StreamID: StreamMain, Clock: newClockForTest(), IDs: newIDs(), Mono: seqMono()}
	if _, err := New(good); err != nil {
		t.Fatal(err)
	}
	custom := good
	custom.Sanitizer = DefaultSanitizer()
	if r, err := New(custom); err != nil || r.san != custom.Sanitizer {
		t.Fatal(err)
	}
	mutations := map[string]func(*Config){
		"sink": func(c *Config) { c.Sink = nil }, "clock": func(c *Config) { c.Clock = nil }, "ids": func(c *Config) { c.IDs = nil },
		"mono": func(c *Config) { c.Mono = nil }, "node": func(c *Config) { c.NodeID = "x" }, "run": func(c *Config) { c.RuntimeID = "" },
		"stream": func(c *Config) { c.StreamID = "Main!" },
	}
	for name, mutate := range mutations {
		cfg := good
		mutate(&cfg)
		if _, err := New(cfg); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

func TestStartValidation(t *testing.T) {
	r := newRec(t, &memSink{})
	links17 := make([]Link, 17)
	arts17 := make([]ArtifactRef, 17)
	for i := range links17 {
		links17[i] = Link{Relation: RelationFanIn}
	}
	specs := map[string]StartSpec{
		"empty name":     {Name: ""},
		"uppercase":      {Name: "Bad.Name"},
		"double dot":     {Name: "a..b"},
		"long name":      {Name: strings.Repeat("a", 97)},
		"durability":     {Name: "a.b", Durability: 9},
		"end durability": {Name: "a.b", EndDurability: 9},
		"17 links":       {Name: "a.b", Links: links17},
		"bad relation":   {Name: "a.b", Links: []Link{{Relation: "Caused-By"}}},
		"long relation":  {Name: "a.b", Links: []Link{{Relation: strings.Repeat("a", 49)}}},
		"bad link id":    {Name: "a.b", Links: []Link{{Relation: RelationCausedBy, EventID: "x"}}},
		"17 artifacts":   {Name: "a.b", Artifacts: arts17},
		"bad digest":     {Name: "a.b", Artifacts: []ArtifactRef{{Digest: "md5:abc"}}},
		"any metadata":   {Name: "a.b", Metadata: wire.Any{TypeURL: "t"}},
		"proto metadata": {Name: "a.b", Metadata: fakeProto{}},
	}
	for name, spec := range specs {
		for _, d := range []Durability{Diagnostic, Critical} {
			spec.Durability = max(spec.Durability, d)
			ctx := context.Background()
			nctx, op, err := r.Start(ctx, spec)
			if errs.CategoryOf(err) != errs.CategoryInvalidArgument || op != nil || nctx != ctx {
				t.Errorf("%s (%d): %v", name, d, err)
			}
		}
	}
}

func TestObserveValidation(t *testing.T) {
	r := newRec(t, &memSink{})
	ctx, op := mustStart(t, r, context.Background(), StartSpec{Name: "a.b"})
	links17 := make([]Link, 17)
	for i := range links17 {
		links17[i] = Link{Relation: RelationFanIn}
	}
	specs := map[string]ObservationSpec{
		"name":       {Name: "Bad"},
		"durability": {Name: "a.b", Durability: 9},
		"links":      {Name: "a.b", Links: links17},
		"artifacts":  {Name: "a.b", Artifacts: make([]ArtifactRef, 17)},
		"any":        {Name: "a.b", Payload: &wire.Any{}},
		"digest":     {Name: "a.b", Artifacts: []ArtifactRef{{Digest: "sha256:zz"}}},
	}
	for name, spec := range specs {
		if err := r.Observe(ctx, spec); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("Recorder.Observe %s: %v", name, err)
		}
		if err := op.Observe(spec); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("Op.Observe %s: %v", name, err)
		}
	}
	if err := r.Observe(context.Background(), ObservationSpec{Name: "a.b"}); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("no current op: %v", err)
	}
}

func TestInvalidIDSource(t *testing.T) {
	for _, prefix := range []string{"op", "trc"} {
		r, err := New(Config{
			Sink: &memSink{}, NodeID: nodeID(), RuntimeID: runID(), StreamID: StreamMain, Clock: newClockForTest(),
			IDs: testIDs{Sequential: newIDs(), bad: map[string]bool{prefix: true}}, Mono: seqMono(),
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		nctx, op, err := r.Start(ctx, StartSpec{Name: "a.b"})
		if errs.CategoryOf(err) != errs.CategoryInternal || op != nil || nctx != ctx {
			t.Fatalf("%s: %v", prefix, err)
		}
	}
}

// B-9: a failing Critical Start returns the error with ctx unchanged, and the
// protected effect is not executed; Diagnostic starts never fail on I/O.
func TestCriticalStartFailure(t *testing.T) {
	sink := &memSink{critHook: func(*wire.JournalRecord) error { return errors.New("disk full") }}
	r := newRec(t, sink)
	ctx := context.Background()
	effect := false
	err := Run(ctx, r, StartSpec{Name: "guard.effect", Durability: Critical}, func(context.Context, *Op) error {
		effect = true
		return nil
	})
	if errs.CategoryOf(err) != errs.CategoryInternal || effect {
		t.Fatalf("err %v effect %v", err, effect)
	}
	nctx, op, err := r.Start(ctx, StartSpec{Name: "guard.effect", Durability: Critical})
	if err == nil || op != nil || nctx != ctx {
		t.Fatalf("%v", err)
	}
	if len(sink.records()) != 0 {
		t.Fatal("nothing may be persisted")
	}
	sink.diagReject = true
	_, op, err = r.Start(ctx, StartSpec{Name: "diag.start"})
	if err != nil || op == nil {
		t.Fatalf("diagnostic start failed: %v", err)
	}
	if st := r.Status().Stats; st.Dropped != 1 {
		t.Fatalf("stats %+v", st)
	}
	if err := op.End(OutcomeCompleted, nil); err != nil {
		t.Fatal(err)
	}
	if st := r.Status().Stats; st.Dropped != 2 || st.EndDropped != 1 {
		t.Fatalf("stats %+v", st)
	}
	var drops int
	for _, ev := range r.Health() {
		if ev.Kind == wire.HealthDroppedDiagnostics {
			drops++
		}
	}
	if drops != 1 {
		t.Fatalf("health drop events %d (only the first drop is noted)", drops)
	}
}

// B-17: a cancelled caller context cannot prevent END.
func TestCancelledContext(t *testing.T) {
	sink := &memSink{}
	r := newRec(t, sink)
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), testKey{}, "kept"))
	nctx, op := mustStart(t, r, ctx, StartSpec{Name: "a.b", Durability: Critical, EndDurability: Critical})
	cancel()
	if err := op.End(OutcomeCancelled, context.Canceled); err != nil {
		t.Fatal(err)
	}
	last := len(sink.critCtx) - 1
	if sink.ctxErrs[last] != nil || !sink.ctxHasDL[last] {
		t.Fatalf("END context: err %v deadline %v", sink.ctxErrs[last], sink.ctxHasDL[last])
	}
	if sink.critCtx[last].Value(testKey{}) != "kept" {
		t.Fatal("END context lost the values of the Start context")
	}
	if recs := sink.records(); len(recs) != 2 || recs[1].End == nil || recs[1].End.Outcome != wire.OutcomeCancelled || recs[1].End.ErrorCode != "canceled" {
		t.Fatalf("records %+v", recs)
	}
	// Post-start critical observations use the same detached context.
	r2 := newRec(t, sink)
	pctx, pcancel := context.WithCancel(context.Background())
	_, op2 := mustStart(t, r2, pctx, StartSpec{Name: "a.b"})
	pcancel()
	if err := op2.Observe(ObservationSpec{Name: "a.note", Durability: Critical}); err != nil {
		t.Fatal(err)
	}
	if last := len(sink.critCtx) - 1; sink.ctxErrs[last] != nil || !sink.ctxHasDL[last] {
		t.Fatalf("observe context: err %v deadline %v", sink.ctxErrs[last], sink.ctxHasDL[last])
	}
	_ = nctx

	// A Critical Start with an already-cancelled ctx fails before any write.
	before := len(sink.records())
	dead, dcancel := context.WithCancel(context.Background())
	dcancel()
	rctx, rop, err := r.Start(dead, StartSpec{Name: "a.b", Durability: Critical})
	if err == nil || !errors.Is(err, context.Canceled) || errs.CategoryOf(err) != errs.CategoryInvalidArgument || rop != nil || rctx != dead {
		t.Fatalf("%v", err)
	}
	if len(sink.records()) != before {
		t.Fatal("a record was written for a cancelled critical start")
	}
	// A diagnostic start with a cancelled ctx still records (no I/O wait).
	if _, op3, err := r.Start(dead, StartSpec{Name: "a.b"}); err != nil || op3 == nil {
		t.Fatalf("%v", err)
	}
}

// B-8: End exactly once under racing goroutines; SetResult is race-clean and
// the snapshot has no later key.
func TestEndExactlyOnce(t *testing.T) {
	r, dir := journalRec(t)
	_, op := mustStart(t, r, context.Background(), StartSpec{Name: "race.end"})
	op.SetResult("before", 1)
	const enders, setters = 16, 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errsCh := make(chan error, enders)
	for i := 0; i < enders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errsCh <- op.End(OutcomeCompleted, nil)
		}()
	}
	for i := 0; i < setters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			op.SetResult(fmt.Sprintf("k%d", i), i)
		}()
	}
	close(start)
	wg.Wait()
	close(errsCh)
	var ok, dup int
	for err := range errsCh {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrAlreadyEnded):
			dup++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || dup != enders-1 {
		t.Fatalf("ok %d dup %d", ok, dup)
	}
	op.SetResult("late", 1)
	op.AddArtifact(ArtifactRef{ID: "late"})
	op.SetUsage(1, 1, 1)
	flushRec(t, r)
	end := onlyRecord(t, scanDir(t, dir), wire.RecordTypeOperationEnd).End
	var res map[string]any
	if err := json.Unmarshal(end.JSONResult, &res); err != nil {
		t.Fatal(err)
	}
	if _, late := res["late"]; late || res["before"] != float64(1) {
		t.Fatalf("result %v", res)
	}
	if len(end.Artifacts) != 0 || end.Usage != nil {
		t.Fatalf("late mutations leaked: %+v", end)
	}
	if st := r.Status().Stats; st.DuplicateEnd != enders-1 {
		t.Fatalf("stats %+v", st)
	}
}

func TestEndArgumentValidation(t *testing.T) {
	sink := &memSink{}
	r := newRec(t, sink)
	_, op := mustStart(t, r, context.Background(), StartSpec{Name: "a.b"})
	for name, call := range map[string]func() error{
		"panic":       func() error { return op.End(OutcomePanic, nil) },
		"unspecified": func() error { return op.End(0, nil) },
		"unknown":     func() error { return op.End(99, nil) },
		"cause":       func() error { return op.End(OutcomeCompleted, errors.New("x")) },
	} {
		if err := call(); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(sink.records()) != 1 {
		t.Fatal("rejected End calls must write nothing")
	}
	if err := op.End(OutcomeFailed, nil); err != nil { // the END was not consumed; FAILED without a cause is allowed
		t.Fatal(err)
	}
	if err := op.End(OutcomeCompleted, nil); !errors.Is(err, ErrAlreadyEnded) {
		t.Fatalf("%v", err)
	}
	if e := sink.records()[1].End; e.ErrorCode != "" || e.ErrorSummary != "" {
		t.Fatalf("%+v", e)
	}
}

func TestEndErrorCodesAndSanitizedSummary(t *testing.T) {
	long := errors.New(strings.Repeat("x", 300))
	cases := []struct {
		name    string
		outcome Outcome
		cause   error
		code    string
		summary string
	}{
		{"errs category", OutcomeFailed, errs.New(errs.CategoryNotFound, "gone"), "not_found", "not_found: gone"},
		{"wrapped errs", OutcomeFailed, fmt.Errorf("ctx: %w", errs.New(errs.CategoryPolicyDenied, "no")), "policy_denied", "ctx: policy_denied: no"},
		{"deadline", OutcomeFailed, fmt.Errorf("w: %w", context.DeadlineExceeded), "deadline_exceeded", "w: context deadline exceeded"},
		{"canceled", OutcomeCancelled, fmt.Errorf("w: %w", context.Canceled), "canceled", "w: context canceled"},
		{"plain", OutcomeFailed, errors.New("boom"), "error", "boom"},
		{"secret", OutcomeFailed, errors.New("dial https://user:pa55@host failed"), "error", "dial https://[REDACTED]@host failed"},
		{"multiline", OutcomeFailed, errors.New("a\nb"), "error", "[OMITTED:multiline 256b]"},
		{"long", OutcomeFailed, long, "error", strings.Repeat("x", 256) + "...[truncated 44 bytes]"},
		{"panicking Error", OutcomeFailed, panickyError{}, "error", "[error text unavailable]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sink := &memSink{}
			r := newRec(t, sink)
			_, op := mustStart(t, r, context.Background(), StartSpec{Name: "a.b"})
			if err := op.End(c.outcome, c.cause); err != nil {
				t.Fatal(err)
			}
			e := sink.records()[1].End
			if e.Outcome != c.outcome || e.ErrorCode != c.code || e.ErrorSummary != c.summary {
				t.Fatalf("end %+v", e)
			}
		})
	}
}

type panickyError struct{}

func (panickyError) Error() string { panic("no text for you") }

func TestEndDropAndFailureCounters(t *testing.T) {
	sink := &memSink{critHook: func(rec *wire.JournalRecord) error {
		if rec.End != nil {
			return errors.New("disk")
		}
		return nil
	}}
	r := newRec(t, sink)
	_, op := mustStart(t, r, context.Background(), StartSpec{Name: "a.b", EndDurability: Critical})
	if err := op.End(OutcomeCompleted, nil); err == nil || errs.CategoryOf(err) != errs.CategoryInternal {
		t.Fatalf("%v", err)
	}
	if st := r.Status().Stats; st.EndFailed != 1 || st.EndDropped != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestOpResultsArtifactsUsage(t *testing.T) {
	sink := &memSink{}
	r := newRec(t, sink)
	_, op := mustStart(t, r, context.Background(), StartSpec{Name: "a.b"})
	for i := 0; i < 70; i++ {
		op.SetResult(fmt.Sprintf("k%02d", i), i)
	}
	op.SetResult("k00", "overwritten") // an existing key can still be replaced at the cap
	for i := 0; i < 40; i++ {
		op.AddArtifact(ArtifactRef{ID: fmt.Sprintf("a%d", i), Digest: digestA})
	}
	op.AddArtifact(ArtifactRef{ID: "bad", Digest: "sha256:short"})
	op.AddArtifact(ArtifactRef{ID: "nodigest"}) // dropped only because the cap (32) is reached
	op.SetUsage(1, 2, -time.Second)
	if err := op.End(OutcomeCompleted, nil); err != nil {
		t.Fatal(err)
	}
	st := r.Status().Stats
	if st.ResultKeysDropped != 6 || st.ArtifactsDropped != 8+1+1 {
		t.Fatalf("%+v", st)
	}
	e := sink.records()[1].End
	var res map[string]any
	if err := json.Unmarshal(e.JSONResult, &res); err != nil {
		t.Fatal(err)
	}
	if len(res) != 64 || res["k00"] != "overwritten" || len(e.Artifacts) != 32 || e.Usage.DurationNanos != 0 {
		t.Fatalf("result keys %d artifacts %d usage %+v", len(res), len(e.Artifacts), e.Usage)
	}
	// With a dedicated op: an artifact without a digest is kept below the cap.
	_, op2 := mustStart(t, r, context.Background(), StartSpec{Name: "a.b"})
	op2.AddArtifact(ArtifactRef{ID: "nodigest", Kind: "k", Locator: "/l"})
	_ = op2.End(OutcomeCompleted, nil)
	if got := sink.records()[3].End.Artifacts; len(got) != 1 || got[0].Digest != "" {
		t.Fatalf("%+v", got)
	}
}

func TestArtifactFromProtocol(t *testing.T) {
	a := ArtifactFromProtocol(protocol.ArtifactRef{ID: "i", Kind: "k", Locator: "/l", MediaType: "text/plain", Digest: digestA, SizeBytes: 9, Truncated: true})
	want := ArtifactRef{ID: "i", Kind: "k", Locator: "/l", MediaType: "text/plain", Digest: digestA, SizeBytes: 9, Truncated: true}
	if a != want || a.DigestVerified {
		t.Fatalf("%+v", a)
	}
}

func TestObserveAfterEndAndCurrentOp(t *testing.T) {
	sink := &memSink{}
	r := newRec(t, sink)
	ctx, op := mustStart(t, r, context.Background(), StartSpec{Name: "a.b"})
	if err := op.Observe(ObservationSpec{Name: "a.note", Kind: ObservationProgress}); err != nil {
		t.Fatal(err)
	}
	if err := r.Observe(ctx, ObservationSpec{Name: "a.note2", Kind: ObservationFact, Evidence: EvidenceUnknown}); err != nil {
		t.Fatal(err)
	}
	if err := op.End(OutcomeCompleted, nil); err != nil {
		t.Fatal(err)
	}
	n := len(sink.records())
	if err := op.Observe(ObservationSpec{Name: "a.late"}); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("%v", err)
	}
	if len(sink.records()) != n || r.Status().Stats.ObserveAfterEnd != 1 {
		t.Fatal("observe after end must write nothing and be counted")
	}
	recs := bodies(sink.records(), wire.RecordTypeObservation)
	if len(recs) != 2 || recs[0].Observation.OperationID != op.ID() || recs[1].Observation.OperationID != op.ID() || recs[1].Observation.Evidence != wire.EvidenceUnknown {
		t.Fatalf("%+v", recs)
	}
	// Critical observation failures surface; diagnostic drops are counted.
	sink.critHook = func(*wire.JournalRecord) error { return errors.New("disk") }
	if err := r.Observe(ctx, ObservationSpec{Name: "a.crit", Durability: Critical}); errs.CategoryOf(err) != errs.CategoryInternal {
		t.Fatalf("%v", err)
	}
	sink.diagReject = true
	if err := r.Observe(ctx, ObservationSpec{Name: "a.dropped"}); err != nil {
		t.Fatal(err)
	}
	if r.Status().Stats.Dropped != 1 {
		t.Fatalf("%+v", r.Status().Stats)
	}
}

// B-15: an observation round-trips through the disk, Any is rejected.
func TestObservationRoundTripThroughDisk(t *testing.T) {
	r, dir := journalRec(t)
	ctx, op := mustStart(t, r, context.Background(), StartSpec{Name: "setup.assess.capability"})
	observed := t0.Add(5 * time.Second)
	spec := ObservationSpec{
		Name: "setup.assess.capability", Kind: ObservationDecision, Durability: Critical, ReasonCode: "gpu_absent",
		Subject:    &Subject{Kind: "capability", ID: "gpu"},
		Provenance: &Provenance{SourceKind: "env", SourceRef: "DEVCADENCE_HOME", ObservedBy: "probe", Method: "stat", ObservedAt: observed},
		Evidence:   EvidenceMissing, Payload: map[string]any{"count": 0, "password": "hunter2hunter2"},
		Artifacts: []ArtifactRef{{ID: "art1", Kind: "probe", Locator: "/x/y", MediaType: "application/json", Digest: digestA, SizeBytes: 5, Truncated: true, DigestVerified: true}},
		Links:     []Link{{Relation: RelationDerivedFrom, TraceID: id("trc", 5), OperationID: id("op", 5)}},
	}
	if err := r.Observe(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if err := r.Observe(ctx, ObservationSpec{Name: "a.b", Payload: wire.Any{}}); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("Any accepted: %v", err)
	}
	_ = op.End(OutcomeCompleted, nil)
	flushRec(t, r)
	o := onlyRecord(t, scanDir(t, dir), wire.RecordTypeObservation)
	if o.Durability != wire.DurabilityCritical {
		t.Fatalf("durability %v", o.Durability)
	}
	ob := o.Observation
	if ob.OperationID != op.ID() || ob.Name != spec.Name || ob.Kind != wire.ObservationDecision || ob.ReasonCode != "gpu_absent" ||
		ob.Subject.Kind != "capability" || ob.Subject.ID != "gpu" || ob.Evidence != wire.EvidenceMissing ||
		string(ob.JSONPayload) != `{"count":0,"password":"[REDACTED]"}` || ob.ProtoPayload != nil {
		t.Fatalf("observation %+v", ob)
	}
	p := ob.Provenance
	if p.SourceKind != "env" || p.SourceRef != "DEVCADENCE_HOME" || p.ObservedBy != "probe" || p.Method != "stat" || p.ObservedAt.WallUnixNanos != observed.UnixNano() {
		t.Fatalf("provenance %+v", p)
	}
	a := ob.Artifacts[0]
	if a.ID != "art1" || a.Locator != "/x/y" || a.MediaType != "application/json" || a.Digest != digestA || a.SizeBytes != 5 || !a.Truncated || !a.DigestVerified {
		t.Fatalf("artifact %+v", a)
	}
	if len(ob.Links) != 1 || ob.Links[0].Relation != "derived_from" || ob.Links[0].OperationID != id("op", 5) {
		t.Fatalf("links %+v", ob.Links)
	}
	if ob.Sanitization == nil || ob.Sanitization.Redactions != 1 {
		t.Fatalf("sanitization %+v", ob.Sanitization)
	}
}

// B-10 (end to end): a fixed corpus of secrets never reaches the journal bytes.
func TestSecretsNeverReachDisk(t *testing.T) {
	corpus := []string{
		"sk-live-ABCDEF123456", "hunter2hunter2", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcde12345",
		"PRIVATE KEY-----", "pa55word", "topsecretvalue", "ghp_abcdefghijklmnop", "s3cr3t-cookie",
	}
	r, dir := journalRec(t)
	ctx, op := mustStart(t, r, context.Background(), StartSpec{
		Name: "secret.check", ActorID: "ghp_abcdefghijklmnop", TaskID: "task with space",
		Metadata:  map[string]any{"password": "hunter2hunter2", "note": "use sk-live-ABCDEF123456 here", "cookie": "s3cr3t-cookie", "stdout": "raw"},
		Artifacts: []ArtifactRef{{ID: "a", Kind: "k", Locator: "/p/API_KEY=topsecretvalue", MediaType: "text/plain"}},
	})
	if err := r.Observe(ctx, ObservationSpec{
		Name: "secret.obs", ReasonCode: "sk-live-ABCDEF123456", Subject: &Subject{Kind: "k", ID: "ghp_abcdefghijklmnop"},
		Provenance: &Provenance{SourceKind: "file", SourceRef: "https://user:pa55word@host/x"},
		Payload:    map[string]any{"jwt": "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcde12345", "pem": "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----"},
	}); err != nil {
		t.Fatal(err)
	}
	op.SetResult("token", "sk-live-ABCDEF123456")
	op.AddArtifact(ArtifactRef{ID: "r", Kind: "k", Locator: "/q/PASSWORD=pa55word"})
	if err := op.End(OutcomeFailed, errors.New("login https://user:pa55word@host failed with token=hunter2hunter2")); err != nil {
		t.Fatal(err)
	}
	flushRec(t, r)
	scanDir(t, dir)
	raw := readAllJournalBytes(t, dir)
	for _, secret := range corpus {
		if strings.Contains(string(raw), secret) {
			t.Errorf("secret %q found in journal bytes", secret)
		}
	}
}

func TestHealthRecordCapsAttempts(t *testing.T) {
	sink := &memSink{}
	r := newRec(t, sink)
	var attempts []Attempt
	for i := 0; i < 70; i++ {
		attempts = append(attempts, Attempt{Source: "s", Path: fmt.Sprintf("/p/%d", i), ErrCode: "mkdir_failed"})
	}
	if err := r.recordHealth(context.Background(), Diagnostic, wire.HealthPathFallback, "path_fallback", "multi\nline", "temp", attempts); err != nil {
		t.Fatal(err)
	}
	h := sink.records()[0].Health
	if len(h.Attempts) != wire.MaxAttempts || !strings.Contains(h.Detail, "[6 attempts dropped]") || h.Kind != wire.HealthPathFallback ||
		h.DetailCode != "path_fallback" || h.SelectedSource != "temp" {
		t.Fatalf("%+v", h)
	}
	// The detail is sanitized (multiline omission) before persistence.
	if !strings.HasPrefix(h.Detail, "[OMITTED:multiline") || !strings.HasSuffix(h.Detail, "[6 attempts dropped]") {
		t.Fatalf("detail %q", h.Detail)
	}
}

func TestHealthRing(t *testing.T) {
	r := newRec(t, &memSink{})
	for i := 0; i < healthRingSize+10; i++ {
		r.noteHealth(wire.HealthWriterError, fmt.Sprintf("e%d", i), "d", []Attempt{{Source: "s"}})
	}
	h := r.Health()
	if len(h) != healthRingSize || h[0].Seq != 11 || h[len(h)-1].Seq != healthRingSize+10 || h[0].Code != "e10" {
		t.Fatalf("len %d first %+v last %+v", len(h), h[0], h[len(h)-1])
	}
	h[0].Attempts[0].Source = "mutated" // snapshots are copies
	if r.Health()[0].Attempts[0].Source != "s" {
		t.Fatal("ring aliased")
	}
	short := newRec(t, &memSink{})
	short.noteHealth(wire.HealthWriterError, "one", "", nil)
	short.noteHealth(wire.HealthWriterError, "two", "", nil)
	if h := short.Health(); len(h) != 2 || h[0].Code != "one" || h[1].Code != "two" {
		t.Fatalf("%+v", h)
	}
}

// B-16: Close drains, is idempotent, later appends refuse or drop per class
// and the lock is released.
func TestCloseDrainsAndReleasesLock(t *testing.T) {
	r, dir := journalRec(t)
	ctx, op := mustStart(t, r, context.Background(), StartSpec{Name: "a.b"})
	if err := op.End(OutcomeCompleted, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Start(ctx, StartSpec{Name: "a.b", Durability: Critical}); errs.CategoryOf(err) != errs.CategoryInternal || !errors.Is(err, journal.ErrClosed) {
		t.Fatalf("%v", err)
	}
	if _, op2, err := r.Start(ctx, StartSpec{Name: "a.b"}); err != nil || op2 == nil {
		t.Fatalf("%v", err)
	}
	if r.Status().Stats.Dropped != 1 {
		t.Fatalf("%+v", r.Status().Stats)
	}
	recs := scanDir(t, dir)
	if len(bodies(recs, wire.RecordTypeOperationStart)) != 1 || len(bodies(recs, wire.RecordTypeOperationEnd)) != 1 {
		t.Fatalf("accepted records must be on disk after Close: %d records", len(recs))
	}
	root := dir
	for i := 0; i < 4; i++ {
		root = filepath.Dir(root)
	}
	w, err := journal.Open(context.Background(), journal.Config{
		Root: root, Dir: dir, NodeID: nodeID(), RuntimeID: runID(), StreamID: StreamMain,
		Clock: newClockForTest(), IDs: newIDs(), Mono: seqMono(),
	})
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	if err := w.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Sink close errors propagate.
	sink := &memSink{closeErr: errors.New("close failed")}
	if err := newRec(t, sink).Close(context.Background()); err == nil || sink.closes != 1 {
		t.Fatalf("%v", err)
	}
}

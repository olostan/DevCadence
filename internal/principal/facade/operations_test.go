package facade_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
)

func newOps(t *testing.T, instance string) *facade.OperationRegistry {
	t.Helper()
	r, err := facade.NewOperationRegistry(instance)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

func codeOf(err error) string {
	var se principal.SemanticError
	_ = se
	return facade.ErrorEnvelope(err).Error.Code
}

// A11: handles carry the instance, the yield is not the deadline, and a
// restart loses every handle without replay.
func TestA11_OperationsYieldAndAreLostAcrossInstances(t *testing.T) {
	r := newOps(t, "")
	if r.InstanceID() == "" {
		t.Fatal("no instance id")
	}
	release := make(chan struct{})
	ref, err := r.Start("p", principal.KindDelegate, time.Minute, func(ctx context.Context) (string, error) {
		select {
		case <-release:
			return "done-handle", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.InstanceID != r.InstanceID() || ref.Status != principal.StatusRunning || ref.Validate() != nil {
		t.Fatalf("bad handle %+v", ref)
	}
	// The yield threshold returns a running handle; the operation continues.
	yielded := r.Wait(context.Background(), ref, 20*time.Millisecond)
	if yielded.Status != principal.StatusRunning {
		t.Fatalf("yield reported %s", yielded.Status)
	}
	st, err := r.Lookup("p", ref)
	if err != nil || st.Operation.Status != principal.StatusRunning || st.ResultHandle != "" {
		t.Fatalf("poll: %+v %v", st, err)
	}
	close(release)
	done := r.Wait(context.Background(), ref, 5*time.Second)
	if done.Status != principal.StatusCompleted {
		t.Fatalf("status %s", done.Status)
	}
	st, _ = r.Lookup("p", ref)
	if st.ResultHandle != "done-handle" || st.Error != nil {
		t.Fatalf("result %+v", st)
	}
	if _, err := r.Lookup("other-project", ref); codeOf(err) != principal.CodePolicyDenied {
		t.Fatalf("cross-project poll: %v", err)
	}

	// A restart is a new instance: the same id is lost, never replayed.
	restarted := newOps(t, "")
	if restarted.InstanceID() == r.InstanceID() {
		t.Fatal("instance ids collide")
	}
	if _, err := restarted.Lookup("p", ref); codeOf(err) != principal.CodeOperationLost {
		t.Fatalf("a handle survived a restart: %v", err)
	}
	forged := ref
	forged.InstanceID = "inst_forged"
	if _, err := r.Lookup("p", forged); codeOf(err) != principal.CodeOperationLost {
		t.Fatalf("a foreign instance resolved: %v", err)
	}
}

func TestOperationRegistryBounds(t *testing.T) {
	r := newOps(t, "inst_bounds")
	if _, err := r.Start("p", principal.KindReview, 0, func(context.Context) (string, error) { return "", nil }); codeOf(err) != principal.CodePolicyDenied {
		t.Fatalf("an unknown deadline was scheduled: %v", err)
	}
	if _, err := r.Start("p", "nonsense", time.Minute, func(context.Context) (string, error) { return "", nil }); err == nil {
		t.Fatal("an invalid kind was scheduled")
	}
	block := make(chan struct{})
	var active []facade.OperationRef
	for i := 0; i < facade.MaxActiveOperations; i++ {
		ref, err := r.Start("p", principal.KindValidate, time.Minute, func(ctx context.Context) (string, error) {
			select {
			case <-block:
			case <-ctx.Done():
			}
			return "", nil
		})
		if err != nil {
			t.Fatalf("operation %d: %v", i, err)
		}
		active = append(active, ref)
	}
	if _, err := r.Start("p", principal.KindValidate, time.Minute, func(context.Context) (string, error) { return "", nil }); codeOf(err) != principal.CodeContextUnfit {
		t.Fatalf("a full registry accepted work: %v", err)
	}
	for _, ref := range active {
		if st, err := r.Lookup("p", ref); err != nil || st.Operation.Status != principal.StatusRunning {
			t.Fatalf("an active operation was evicted: %v", err)
		}
	}
	close(block)
	for _, ref := range active {
		r.Wait(context.Background(), ref, 5*time.Second)
	}
	// Completed handles expire oldest first past the retention bound.
	var first facade.OperationRef
	for i := 0; i < facade.MaxCompletedOperations; i++ {
		ref, err := r.Start("p", principal.KindSnippet, time.Minute, func(context.Context) (string, error) { return "h", nil })
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = ref
		}
		r.Wait(context.Background(), ref, 5*time.Second)
	}
	if _, err := r.Lookup("p", active[0]); codeOf(err) != principal.CodeOperationLost {
		t.Fatalf("an expired handle still resolves: %v", err)
	}
	if _, err := r.Lookup("p", first); err != nil {
		t.Fatalf("a retained completed handle was lost: %v", err)
	}
}

func TestOperationFailuresAreNormalisedAndCloseCancels(t *testing.T) {
	r := newOps(t, "inst_close")
	failed, _ := r.Start("p", principal.KindReview, time.Minute, func(context.Context) (string, error) {
		return "", errors.New("provider said: sk-SECRET")
	})
	r.Wait(context.Background(), failed, 5*time.Second)
	st, _ := r.Lookup("p", failed)
	if st.Operation.Status != principal.StatusFailed || st.Error == nil || st.Error.Code != principal.CodeInternal || st.Error.Validate() != nil {
		t.Fatalf("unexpected failure %+v", st)
	}
	timedOut, _ := r.Start("p", principal.KindReview, 10*time.Millisecond, func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	r.Wait(context.Background(), timedOut, 5*time.Second)
	if st, _ := r.Lookup("p", timedOut); st.Operation.Status != principal.StatusCancelled {
		t.Fatalf("a deadline produced %s", st.Operation.Status)
	}

	var started sync.WaitGroup
	started.Add(1)
	running, _ := r.Start("p", principal.KindDelegate, time.Hour, func(ctx context.Context) (string, error) {
		started.Done()
		<-ctx.Done()
		return "", ctx.Err()
	})
	started.Wait()
	r.Close() // EOF or shutdown cancels owned operations and waits for them
	st, _ = r.Lookup("p", running)
	if st.Operation.Status != principal.StatusCancelled || st.ResultHandle != "" {
		t.Fatalf("close left %s", st.Operation.Status)
	}
	if _, err := r.Start("p", principal.KindDelegate, time.Minute, func(context.Context) (string, error) { return "", nil }); err == nil {
		t.Fatal("a closed registry accepted work")
	}
	if _, err := facade.NewOperationRegistry("bad\x00id"); err == nil {
		t.Fatal("an invalid instance id was accepted")
	}
}

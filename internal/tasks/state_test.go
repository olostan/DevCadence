package tasks_test

import (
	"errors"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/tasks"
)

// legalTransitions is the lifecycle of docs/LIFECYCLE.md §12, written out by
// hand rather than derived from the implementation. A test that read the
// adjacency list under test would pass no matter what that list said.
var legalTransitions = []struct {
	from, to tasks.State
}{
	{tasks.StateProposed, tasks.StateScouting},
	{tasks.StateProposed, tasks.StateDesigning},
	{tasks.StateScouting, tasks.StateDesigning},
	{tasks.StateDesigning, tasks.StateReady},
	{tasks.StateReady, tasks.StateRunning},
	{tasks.StateRunning, tasks.StateValidating},
	{tasks.StateValidating, tasks.StateRunning},
	{tasks.StateValidating, tasks.StateReviewing},
	{tasks.StateReviewing, tasks.StateRunning},
	{tasks.StateReviewing, tasks.StateAccepted},
	{tasks.StateAccepted, tasks.StateIntegrating},
	{tasks.StateIntegrating, tasks.StateIntegrationValidating},
	{tasks.StateIntegrationValidating, tasks.StateDone},
	{tasks.StateBlocked, tasks.StateDesigning},
	// Every active state may block.
	{tasks.StateProposed, tasks.StateBlocked},
	{tasks.StateScouting, tasks.StateBlocked},
	{tasks.StateDesigning, tasks.StateBlocked},
	{tasks.StateReady, tasks.StateBlocked},
	{tasks.StateRunning, tasks.StateBlocked},
	{tasks.StateValidating, tasks.StateBlocked},
	{tasks.StateReviewing, tasks.StateBlocked},
	{tasks.StateAccepted, tasks.StateBlocked},
	{tasks.StateIntegrating, tasks.StateBlocked},
	{tasks.StateIntegrationValidating, tasks.StateBlocked},
}

func TestEveryLegalTransitionIsPermitted(t *testing.T) {
	for _, tc := range legalTransitions {
		if !tasks.CanTransition(tc.from, tc.to) {
			t.Errorf("expected %s -> %s to be legal", tc.from, tc.to)
		}
		if err := tasks.CheckTransition("DC-001", tc.from, tc.to); err != nil {
			t.Errorf("CheckTransition(%s -> %s) = %v, want nil", tc.from, tc.to, err)
		}
	}
}

// TestOnlyDeclaredTransitionsArePermitted walks the complete cross product of
// states and asserts that everything not in legalTransitions is refused. This
// is what catches an accidentally widened adjacency list, which a table of
// hand-picked illegal cases would miss.
func TestOnlyDeclaredTransitionsArePermitted(t *testing.T) {
	legal := make(map[[2]tasks.State]bool, len(legalTransitions))
	for _, tc := range legalTransitions {
		legal[[2]tasks.State{tc.from, tc.to}] = true
	}
	for _, from := range tasks.AllStates() {
		for _, to := range tasks.AllStates() {
			want := legal[[2]tasks.State{from, to}]
			if got := tasks.CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s, %s) = %t, want %t", from, to, got, want)
			}
		}
	}
}

func TestIllegalTransitionsAreCategorised(t *testing.T) {
	cases := []struct {
		name     string
		from, to tasks.State
		category errs.Category
	}{
		{"skipping design", tasks.StateProposed, tasks.StateReady, errs.CategoryInvalidTransition},
		{"running without a work package", tasks.StateProposed, tasks.StateRunning, errs.CategoryInvalidTransition},
		{"done is terminal", tasks.StateDone, tasks.StateRunning, errs.CategoryInvalidTransition},
		{"done cannot block", tasks.StateDone, tasks.StateBlocked, errs.CategoryInvalidTransition},
		{"blocked resumes only through design", tasks.StateBlocked, tasks.StateRunning, errs.CategoryInvalidTransition},
		{"blocked cannot re-block", tasks.StateBlocked, tasks.StateBlocked, errs.CategoryInvalidTransition},
		{"integration cannot be skipped", tasks.StateAccepted, tasks.StateDone, errs.CategoryInvalidTransition},
		{"unknown target", tasks.StateReady, tasks.State("shipping"), errs.CategoryInvalidArgument},
		{"unknown stored state", tasks.State("sleeping"), tasks.StateReady, errs.CategoryIntegrity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tasks.CheckTransition("DC-001", tc.from, tc.to)
			if err == nil {
				t.Fatalf("CheckTransition(%s -> %s) = nil, want an error", tc.from, tc.to)
			}
			if got := errs.CategoryOf(err); got != tc.category {
				t.Fatalf("category = %s, want %s (%v)", got, tc.category, err)
			}
		})
	}
}

func TestInvalidTransitionErrorIsMatchable(t *testing.T) {
	err := tasks.CheckTransition("DC-001", tasks.StateProposed, tasks.StateDone)
	if !errors.Is(err, errs.ErrInvalidTransition) {
		t.Fatalf("errors.Is(err, ErrInvalidTransition) = false for %v", err)
	}
}

func TestDoneIsTheOnlyTerminalState(t *testing.T) {
	for _, s := range tasks.AllStates() {
		want := s == tasks.StateDone
		if got := s.Terminal(); got != want {
			t.Errorf("%s.Terminal() = %t, want %t", s, got, want)
		}
	}
	// A blocked task is waiting for a decision, not finished. If it were
	// terminal the control plane could silently abandon it, which DCI-045
	// forbids.
	if tasks.StateBlocked.Terminal() {
		t.Fatal("blocked must not be terminal")
	}
}

func TestAllowedIsStableAndSorted(t *testing.T) {
	for _, s := range tasks.AllStates() {
		first := tasks.Allowed(s)
		second := tasks.Allowed(s)
		if len(first) != len(second) {
			t.Fatalf("Allowed(%s) is not stable", s)
		}
		for i := range first {
			if first[i] != second[i] {
				t.Fatalf("Allowed(%s) is not stable: %v vs %v", s, first, second)
			}
			if i > 0 && first[i-1] > first[i] {
				t.Fatalf("Allowed(%s) is not sorted: %v", s, first)
			}
		}
	}
}

// TestMutatingAllowedDoesNotAffectTheStateMachine guards the adjacency list
// against a caller that appends to the returned slice.
func TestMutatingAllowedDoesNotAffectTheStateMachine(t *testing.T) {
	allowed := tasks.Allowed(tasks.StateReady)
	allowed = append(allowed, tasks.StateDone)
	_ = allowed
	if tasks.CanTransition(tasks.StateReady, tasks.StateDone) {
		t.Fatal("mutating the result of Allowed changed the state machine")
	}
}

func TestBucketMapping(t *testing.T) {
	cases := []struct {
		state     tasks.State
		authority protocol.DecisionAuthority
		want      []tasks.Bucket
	}{
		{tasks.StateProposed, "", nil},
		{tasks.StateScouting, "", []tasks.Bucket{tasks.BucketRunning}},
		{tasks.StateDesigning, "", []tasks.Bucket{tasks.BucketAwaitingPrincipal}},
		{tasks.StateReady, "", []tasks.Bucket{tasks.BucketReady}},
		{tasks.StateRunning, "", []tasks.Bucket{tasks.BucketRunning}},
		{tasks.StateValidating, "", []tasks.Bucket{tasks.BucketRunning}},
		{tasks.StateReviewing, "", []tasks.Bucket{tasks.BucketRunning}},
		{tasks.StateAccepted, "", nil},
		{tasks.StateIntegrating, "", []tasks.Bucket{tasks.BucketRunning}},
		{tasks.StateIntegrationValidating, "", []tasks.Bucket{tasks.BucketRunning}},
		{tasks.StateDone, "", nil},
		// A block the principal owns is both "stuck" and "mine to unstick".
		{tasks.StateBlocked, protocol.AuthorityPrincipal,
			[]tasks.Bucket{tasks.BucketBlocked, tasks.BucketAwaitingPrincipal}},
		{tasks.StateBlocked, protocol.AuthorityHuman, []tasks.Bucket{tasks.BucketBlocked}},
		{tasks.StateBlocked, protocol.AuthorityPolicy, []tasks.Bucket{tasks.BucketBlocked}},
	}
	for _, tc := range cases {
		got := tasks.Buckets(tc.state, tc.authority)
		if len(got) != len(tc.want) {
			t.Fatalf("Buckets(%s, %s) = %v, want %v", tc.state, tc.authority, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("Buckets(%s, %s) = %v, want %v", tc.state, tc.authority, got, tc.want)
			}
		}
	}
}

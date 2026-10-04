package planner

import (
	"encoding/json"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ACC-11 (WP-M3C-H3): the planner rejects an alternative whose binding references
// another endpoint's context profile, through the validator without planner changes.
func TestPortfolioPlanner_ContextProfileConsistency_ACC11_RejectsMismatchedProfile(t *testing.T) {
	a := alt(protocol.IntentBalanced)
	pf := makeTestPortfolio()
	pf.RoleBindings[0].ContextProfileID = "prof-cli-01"
	raw, err := json.Marshal(pf)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	a["portfolio"] = m
	res := mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(output(a))}))
	if res.Outcome != OutcomeAllRejected || len(res.Rejected) != 1 || res.Rejected[0].Reason != "validation_failed" {
		t.Fatalf("%+v", res)
	}
	n := 0
	for _, d := range res.Rejected[0].Diagnostics {
		if d.Code == cognition.CodeContextProfileMismatch {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one mismatch, got %d: %v", n, res.Rejected[0].Diagnostics)
	}
}

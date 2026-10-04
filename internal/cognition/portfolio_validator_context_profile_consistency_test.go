package cognition_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

// baseStandardPolicyDigest is the PolicyDigest of a default-policy validation of
// the standard fixture, captured on the base commit before WP-M3C-H3 (ACC-09).
const baseStandardPolicyDigest = "sha256:affbd4e0ef7533d503b079a70f487cec22ca61e71642893245994b0a75c0feca"

// dedicatedProfile adds a copy of prof-local-01 under a new id and binds only
// RoleBindings[0] to it, so a mutation of that profile affects one binding.
func dedicatedProfile(in *cognition.ValidationInput, mutate func(*protocol.ContextProfile)) {
	cp := *in.ContextProfiles["prof-local-01"]
	cp.ProfileID = "prof-dedicated"
	mutate(&cp)
	in.ContextProfiles["prof-dedicated"] = &cp
	in.Portfolio.RoleBindings[0].ContextProfileID = "prof-dedicated"
}

func TestContextProfileConsistency_ACC01_ConsistentPortfolio(t *testing.T) {
	res := cognition.NewPortfolioValidator().Validate(unknownStateInput(makeTestPortfolio(), nil))
	if !res.Valid || len(diagsWithCode(res, cognition.CodeContextProfileMismatch)) != 0 {
		t.Fatalf("unexpected: %v", res.Diagnostics)
	}
}

func TestContextProfileConsistency_ACC02_EndpointDiffers(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	dedicatedProfile(&in, func(p *protocol.ContextProfile) { p.EndpointID = "ep-cli-01" })
	res := cognition.NewPortfolioValidator().Validate(in)
	got := diagsWithCode(res, cognition.CodeContextProfileMismatch)
	if res.Valid || len(got) != 1 {
		t.Fatalf("valid=%v diags=%v", res.Valid, res.Diagnostics)
	}
	d := got[0]
	if d.Code != "CONTEXT_PROFILE_MISMATCH" || d.Target != "role_bindings[0]" || d.Condition != cognition.ConditionInvalid || d.ViolatedRule != "DCI-123" ||
		d.Observed != "endpoint_id=ep-cli-01 channel_id=chan-local-01" ||
		d.Required != "endpoint_id=ep-local-01 channel_id=chan-local-01" {
		t.Fatalf("unexpected diagnostic %+v", d)
	}
}

func TestContextProfileConsistency_ACC03_ChannelDiffers(t *testing.T) {
	for name, ch := range map[string]string{"different id": "chan-cli-01", "case only": "CHAN-LOCAL-01"} {
		t.Run(name, func(t *testing.T) {
			in := unknownStateInput(makeTestPortfolio(), nil)
			dedicatedProfile(&in, func(p *protocol.ContextProfile) { p.ChannelID = ch })
			res := cognition.NewPortfolioValidator().Validate(in)
			got := diagsWithCode(res, cognition.CodeContextProfileMismatch)
			if res.Valid || len(got) != 1 || got[0].Target != "role_bindings[0]" {
				t.Fatalf("valid=%v diags=%v", res.Valid, res.Diagnostics)
			}
		})
	}
}

func TestContextProfileConsistency_ACC04_BothDiffer(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	dedicatedProfile(&in, func(p *protocol.ContextProfile) { p.EndpointID, p.ChannelID = "ep-cli-01", "chan-cli-01" })
	res := cognition.NewPortfolioValidator().Validate(in)
	if got := diagsWithCode(res, cognition.CodeContextProfileMismatch); len(got) != 1 {
		t.Fatalf("want exactly one, got %v", got)
	}
}

func TestContextProfileConsistency_ACC05_FallbackMismatch(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.ContextProfiles["prof-fb"] = func() *protocol.ContextProfile {
		cp := *in.ContextProfiles["prof-cli-01"]
		cp.ProfileID = "prof-fb"
		cp.EndpointID = "ep-local-01"
		return &cp
	}()
	in.Portfolio.RoleBindings[0].Fallbacks[0].ContextProfileID = "prof-fb"
	res := cognition.NewPortfolioValidator().Validate(in)
	got := diagsWithCode(res, cognition.CodeContextProfileMismatch)
	if len(got) != 1 || got[0].Target != "role_bindings[0].fallbacks[0]" {
		t.Fatalf("got %v", got)
	}
	if got[0].Required != "endpoint_id=ep-cli-01 channel_id=chan-cli-01" {
		t.Fatalf("required %q", got[0].Required)
	}
}

func TestContextProfileConsistency_ACC05b_SecondFallback(t *testing.T) {
	build := func(firstBad, secondBad bool) cognition.ValidationInput {
		in := unknownStateInput(makeTestPortfolio(), nil)
		rb := &in.Portfolio.RoleBindings[0]
		rb.Fallbacks = append(rb.Fallbacks, rb.Fallbacks[0])
		for idx, bad := range []bool{firstBad, secondBad} {
			id := fmt.Sprintf("prof-fb-%d", idx)
			cp := *in.ContextProfiles["prof-cli-01"]
			cp.ProfileID = id
			if bad {
				cp.EndpointID = "ep-local-01"
			}
			in.ContextProfiles[id] = &cp
			rb.Fallbacks[idx].ContextProfileID = id
		}
		return in
	}
	res := cognition.NewPortfolioValidator().Validate(build(false, true))
	got := diagsWithCode(res, cognition.CodeContextProfileMismatch)
	if len(got) != 1 || got[0].Target != "role_bindings[0].fallbacks[1]" {
		t.Fatalf("second only: %v", got)
	}
	res = cognition.NewPortfolioValidator().Validate(build(true, true))
	got = diagsWithCode(res, cognition.CodeContextProfileMismatch)
	if len(got) != 2 || got[0].Target != "role_bindings[0].fallbacks[0]" || got[1].Target != "role_bindings[0].fallbacks[1]" {
		t.Fatalf("both: %v", got)
	}
}

func TestContextProfileConsistency_ACC06_NonexistentProfile(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.Portfolio.RoleBindings[0].ContextProfileID = "prof-nonexistent"
	res := cognition.NewPortfolioValidator().Validate(in)
	if len(diagsWithCode(res, cognition.CodeContextProfileNotFound)) != 1 || len(diagsWithCode(res, cognition.CodeContextProfileMismatch)) != 0 {
		t.Fatalf("diags %v", res.Diagnostics)
	}
}

func TestContextProfileConsistency_ACC07_MissingChannelStillChecked(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	cp := *in.ContextProfiles["prof-local-01"]
	cp.ProfileID = "prof-scout"
	cp.EndpointID = "ep-cli-01"
	in.ContextProfiles["prof-scout"] = &cp
	in.Portfolio.RoleBindings[1].ChannelID = "chan-missing"
	in.Portfolio.RoleBindings[1].ContextProfileID = "prof-scout"
	res := cognition.NewPortfolioValidator().Validate(in)
	got := diagsWithCode(res, cognition.CodeContextProfileMismatch)
	if len(got) != 1 || got[0].Target != "role_bindings[1]" {
		t.Fatalf("diags %v", res.Diagnostics)
	}
}

func TestContextProfileConsistency_ACC08_SharedProfileOneDiagnosticPerBinding(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	// Both bindings share prof-local-01; make that profile describe another endpoint.
	in.ContextProfiles["prof-local-01"].EndpointID = "ep-cli-01"
	res := cognition.NewPortfolioValidator().Validate(in)
	got := diagsWithCode(res, cognition.CodeContextProfileMismatch)
	if len(got) != 2 || got[0].Target != "role_bindings[0]" || got[1].Target != "role_bindings[1]" {
		t.Fatalf("diags %v", got)
	}
}

func TestContextProfileConsistency_ACC09_PolicyDigestUnchanged(t *testing.T) {
	def := cognition.DefaultValidationPolicy()
	res := cognition.NewPortfolioValidator().Validate(unknownStateInput(makeTestPortfolio(), &def))
	if res.PolicyDigest != baseStandardPolicyDigest {
		t.Fatalf("policy digest changed: %s", res.PolicyDigest)
	}
}

func TestContextProfileConsistency_ACC10_DeterministicOrder(t *testing.T) {
	build := func(reverse bool) cognition.ValidationInput {
		in := unknownStateInput(makeTestPortfolio(), nil)
		in.ContextProfiles["prof-local-01"].EndpointID = "ep-cli-01"
		in.ContextProfiles["prof-cli-01"].ChannelID = "chan-local-01"
		keys := []string{"prof-local-01", "prof-cli-01"}
		if reverse {
			keys = []string{"prof-cli-01", "prof-local-01"}
		}
		m := map[string]*protocol.ContextProfile{}
		for _, k := range keys {
			m[k] = in.ContextProfiles[k]
		}
		in.ContextProfiles = m
		return in
	}
	var first []cognition.PortfolioDiagnostic
	for i := 0; i < 20; i++ {
		res := cognition.NewPortfolioValidator().Validate(build(i%2 == 1))
		if res.Valid || len(diagsWithCode(res, cognition.CodeContextProfileMismatch)) != 3 {
			t.Fatalf("diags %v", res.Diagnostics)
		}
		if first == nil {
			first = res.Diagnostics
		} else if !reflect.DeepEqual(first, res.Diagnostics) {
			t.Fatalf("run %d differs:\n%v\n%v", i, first, res.Diagnostics)
		}
	}
}

func TestContextProfileConsistency_ACC14_NilProfileEntries(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.ContextProfiles["prof-local-01"] = nil
	in.ContextProfiles["prof-cli-01"] = nil
	res := cognition.NewPortfolioValidator().Validate(in) // must not panic
	notFound := diagsWithCode(res, cognition.CodeContextProfileNotFound)
	if len(notFound) != 3 || len(diagsWithCode(res, cognition.CodeContextProfileMismatch)) != 0 {
		t.Fatalf("diags %v", res.Diagnostics)
	}
	targets := map[string]bool{}
	for _, d := range notFound {
		targets[d.Target] = true
	}
	for _, want := range []string{"role_bindings[0](role=implementer)", "role_bindings[1](role=scout)", "role_bindings[0].fallbacks[0]"} {
		if !targets[want] {
			t.Fatalf("missing target %s in %s", want, fmt.Sprint(targets))
		}
	}
}

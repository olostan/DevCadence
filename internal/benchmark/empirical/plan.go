package empirical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/protocol"
)

// Operational ceilings from WP-M5-5. They are upper bounds, never granted spend.
const (
	ceilingRuns            = 120
	ceilingCallsPerRun     = 4
	ceilingTotalCalls      = 480
	ceilingRunSeconds      = 1800
	ceilingCampaignSeconds = 21600
)

var digestRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func validDigest(s string) bool { return digestRE.MatchString(s) }

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func bytesDigest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// decodeStrict rejects unknown fields, trailing content and duplicate-free
// ambiguity is left to the typed targets (no maps with duplicate keys survive
// encoding/json decoding of structs).
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing content after JSON value")
	}
	return nil
}

// PlanDigest returns the canonical digest of the entire plan (acyclic: the plan
// has no digest, authorization or result field).
func PlanDigest(plan CampaignPlan) (string, error) {
	return protocol.Digest(plan)
}

func isStrategy(s string) bool { return s == StrategyFullHistory || s == StrategyHybrid4 }

func syntheticMarker(vals ...string) bool {
	for _, v := range vals {
		l := strings.ToLower(v)
		for _, m := range []string{"fake", "scripted", "mock", "synthetic", "harness", "replay", "stub", "simulat"} {
			if strings.Contains(l, m) {
				return true
			}
		}
	}
	return false
}

func validEndpoint(e EndpointBinding) string {
	for name, v := range map[string]string{
		"endpoint_id": e.EndpointID, "driver_id": e.DriverID, "model_id": e.ModelID,
		"model_revision": e.ModelRevision, "channel_id": e.ChannelID,
		"runtime_version": e.RuntimeVersion, "subscription_quota_unit": e.SubscriptionQuotaUnit,
	} {
		if blank(v) {
			return name + " is blank"
		}
	}
	if !experiments.CapabilityClass(e.CapabilityClass).Valid() {
		return fmt.Sprintf("capability_class %q is not a known tier", e.CapabilityClass)
	}
	if !validDigest(e.ContextProfileDigest) || !validDigest(e.PolicyDigest) {
		return "context_profile_digest/policy_digest must be sha256 digests"
	}
	return ""
}

func validTierLimitations(ls []TierLimitation, requested map[string]bool) string {
	seen := map[string]bool{}
	for _, l := range ls {
		switch {
		case !requested[l.Tier]:
			return fmt.Sprintf("limitation tier %q is not requested", l.Tier)
		case seen[l.Tier]:
			return fmt.Sprintf("duplicate limitation tier %q", l.Tier)
		case l.Cause != CauseAbsent && l.Cause != CauseNotAuthorized && l.Cause != CauseUnavailable:
			return fmt.Sprintf("limitation cause %q is not closed vocabulary", l.Cause)
		case blank(l.EvidenceRef):
			return fmt.Sprintf("limitation for %q has no evidence_ref", l.Tier)
		}
		seen[l.Tier] = true
	}
	return ""
}

// pairKey identifies a Strategy 1 / Strategy 4 pair: everything but the strategy.
func pairKey(task, taskDigest, seed string, repetition int, e EndpointBinding) string {
	b, _ := protocol.CanonicalJSON([]any{task, taskDigest, seed, repetition, e})
	return string(b)
}

func finiteNonNeg(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) && f >= 0 }

func limitsProblem(l RunLimits) string {
	switch {
	case l.MaxTotalRuns < 0 || l.MaxCallsPerRun < 0 || l.MaxTotalCalls < 0 || l.MaxRunSeconds < 0 ||
		l.MaxCampaignSeconds < 0 || l.MaxSubscriptionCalls < 0 || l.MaxAPISpendMicroUSD < 0:
		return "negative cap"
	case !finiteNonNeg(l.MaxLocalComputeSeconds):
		return "non-finite or negative cap"
	case l.MaxTotalRuns > ceilingRuns || l.MaxCallsPerRun > ceilingCallsPerRun ||
		l.MaxTotalCalls > ceilingTotalCalls || l.MaxRunSeconds > ceilingRunSeconds ||
		l.MaxCampaignSeconds > ceilingCampaignSeconds:
		return "cap exceeds the WP-M5-5 operational ceiling"
	}
	return ""
}

// ValidatePlan returns every structural problem of a plan; empty means valid.
func ValidatePlan(p CampaignPlan) []string {
	var out []string
	add := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
	if p.SchemaVersion != SchemaVersion {
		add("schema_version must be %q", SchemaVersion)
	}
	if blank(p.CampaignID) || blank(p.SourceCommit) {
		add("campaign_id and source_commit are required")
	}
	if blank(p.VerifierSourceCommit) {
		add("verifier_source_commit is required")
	}
	if !validDigest(p.VerificationProfileDigest) {
		add("verification_profile_digest must be a sha256 digest")
	}
	if !validDigest(p.CorpusDigest) || !validDigest(p.CriteriaDigest) {
		add("corpus_digest and criteria_digest must be sha256 digests")
	}
	requested := map[string]bool{}
	if len(p.RequestedTiers) == 0 {
		add("requested_tiers is empty")
	}
	for i, t := range p.RequestedTiers {
		if !experiments.CapabilityClass(t).Valid() {
			add("requested tier %q is not a known tier", t)
		}
		if i > 0 && p.RequestedTiers[i-1] >= t {
			add("requested_tiers must be sorted and unique")
		}
		requested[t] = true
	}
	if m := validTierLimitations(p.MissingTiers, requested); m != "" {
		add("%s", m)
	}
	missing := map[string]bool{}
	for _, l := range p.MissingTiers {
		missing[l.Tier] = true
	}
	if m := limitsProblem(p.Limits); m != "" {
		add("limits: %s", m)
	}
	if len(p.Runs) > p.Limits.MaxTotalRuns || len(p.Runs) > ceilingRuns {
		add("%d planned runs exceed max_total_runs/ceiling", len(p.Runs))
	}

	ids := map[string]bool{}
	type pairState struct {
		ordinal map[string]int
		ewp     ClosedEWPBinding
		rep     int
	}
	pairs := map[string]*pairState{}
	tierRuns := map[string]int{}
	for i, r := range p.Runs {
		if r.Ordinal != i+1 {
			add("run %q ordinal %d must be %d (1..N without gaps)", r.RunID, r.Ordinal, i+1)
		}
		if blank(r.RunID) || ids[r.RunID] {
			add("run_id %q is blank or duplicate", r.RunID)
		}
		ids[r.RunID] = true
		if blank(r.TaskID) || blank(r.Seed) || !validDigest(r.TaskDigest) {
			add("run %q: task_id/seed/task_digest invalid", r.RunID)
		}
		if !isStrategy(r.Strategy) {
			add("run %q: strategy %q is not full_history/hybrid_4layer", r.RunID, r.Strategy)
		}
		if r.Repetition != 1 && r.Repetition != 2 {
			add("run %q: repetition %d must be 1 or 2", r.RunID, r.Repetition)
		}
		if m := validEndpoint(r.Endpoint); m != "" {
			add("run %q: %s", r.RunID, m)
		}
		tier := r.Endpoint.CapabilityClass
		if !requested[tier] || missing[tier] {
			add("run %q: tier %q is not a requested, available tier", r.RunID, tier)
		}
		tierRuns[tier]++
		if blank(r.EWP.ID) || r.EWP.Version < 1 || blank(r.EWP.BaseCommit) ||
			!validDigest(r.EWP.RecordDigest) || !validDigest(r.EWP.ContractDigest) {
			add("run %q: closed EWP binding invalid", r.RunID)
		}
		if syntheticMarker(r.Endpoint.EndpointID, r.Endpoint.DriverID, r.Endpoint.ModelID, r.Endpoint.ChannelID) {
			add("run %q: endpoint carries a synthetic/scripted marker", r.RunID)
		}
		k := pairKey(r.TaskID, r.TaskDigest, r.Seed, r.Repetition, r.Endpoint)
		ps := pairs[k]
		if ps == nil {
			ps = &pairState{ordinal: map[string]int{}, ewp: r.EWP, rep: r.Repetition}
			pairs[k] = ps
		}
		if _, dup := ps.ordinal[r.Strategy]; dup {
			add("run %q duplicates a (task,seed,endpoint,strategy,repetition) pair key", r.RunID)
		}
		ps.ordinal[r.Strategy] = r.Ordinal
		if ps.ewp != r.EWP {
			add("run %q: EWP binding differs within its strategy pair", r.RunID)
		}
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ps := pairs[k]
		a, okA := ps.ordinal[StrategyFullHistory]
		b, okB := ps.ordinal[StrategyHybrid4]
		if !okA || !okB {
			add("pair is missing a strategy (every pair needs full_history and hybrid_4layer)")
			continue
		}
		if (ps.rep == 1 && a > b) || (ps.rep == 2 && b > a) {
			add("pair order must be full_history first for repetition 1 and hybrid_4layer first for repetition 2")
		}
	}
	for _, t := range p.RequestedTiers {
		if !missing[t] && tierRuns[t] == 0 {
			add("requested tier %q has neither planned runs nor a limitation", t)
		}
	}
	return out
}

// validateAuthorization checks the authorization sidecar against the plan. It
// establishes structure and binding only; authenticity needs operatorAuthority.
func validateAuthorization(a CampaignAuthorization, plan CampaignPlan, planDigest string) []string {
	var out []string
	add := func(f string, args ...any) { out = append(out, fmt.Sprintf(f, args...)) }
	if a.Version != SchemaVersion {
		add("version must be %q", SchemaVersion)
	}
	if a.PlanDigest != planDigest {
		add("plan_digest does not equal the exact canonical plan digest")
	}
	if blank(a.AuthorizedBy) || !validDigest(a.IssuedPolicyDigest) {
		add("authorized_by and issued_policy_digest are required")
	}
	if _, err := time.Parse(time.RFC3339, a.Expiry); err != nil {
		add("expiry must be RFC3339")
	}
	lim := RunLimits{MaxTotalRuns: a.MaxTotalRuns, MaxCallsPerRun: a.MaxCallsPerRun, MaxTotalCalls: a.MaxTotalCalls,
		MaxRunSeconds: a.MaxRunSeconds, MaxCampaignSeconds: a.MaxCampaignSeconds, MaxAPISpendMicroUSD: a.MaxAPISpendMicroUSD,
		MaxSubscriptionCalls: a.MaxSubscriptionCalls, MaxLocalComputeSeconds: a.MaxLocalComputeSeconds}
	if m := limitsProblem(lim); m != "" {
		add("caps: %s", m)
	}
	pl := plan.Limits
	if a.MaxTotalRuns > pl.MaxTotalRuns || a.MaxCallsPerRun > pl.MaxCallsPerRun || a.MaxTotalCalls > pl.MaxTotalCalls ||
		a.MaxRunSeconds > pl.MaxRunSeconds || a.MaxCampaignSeconds > pl.MaxCampaignSeconds ||
		a.MaxAPISpendMicroUSD > pl.MaxAPISpendMicroUSD || a.MaxSubscriptionCalls > pl.MaxSubscriptionCalls ||
		a.MaxLocalComputeSeconds > pl.MaxLocalComputeSeconds ||
		(a.AllowMetered && !pl.AllowMetered) || (a.AllowUnknownSubscriptionQuota && !pl.AllowUnknownSubscriptionQuota) {
		add("authorized caps exceed the plan ceilings")
	}
	// Zero means no authority, so a lower cap must still cover the whole matrix:
	// an implicitly truncated matrix is never executed or admitted.
	if a.MaxTotalRuns < len(plan.Runs) || a.MaxCallsPerRun < 1 || a.MaxTotalCalls < len(plan.Runs) ||
		a.MaxRunSeconds < 1 || a.MaxCampaignSeconds < 1 {
		add("authorized caps cannot cover the declared run matrix (zero is no authority)")
	}
	allowed := map[string]bool{}
	for _, e := range a.AllowedEndpointBindings {
		allowed[bindingKey(e)] = true
	}
	for _, r := range plan.Runs {
		if !allowed[bindingKey(r.Endpoint)] {
			add("run %q endpoint binding is not authorized", r.RunID)
		}
		switch r.Endpoint.CapabilityClass {
		case string(experiments.CapabilityFrontierAPI):
			if !a.AllowMetered || !(a.MaxAPISpendMicroUSD > 0) {
				add("run %q needs an explicit metered grant with a positive spend cap", r.RunID)
			}
		case string(experiments.CapabilitySubscriptionCLI):
			if a.MaxSubscriptionCalls < 1 {
				add("run %q needs a positive subscription call cap", r.RunID)
			}
			if r.Endpoint.SubscriptionQuotaUnit == UnknownQuotaUnit && !a.AllowUnknownSubscriptionQuota {
				add("run %q has unknown subscription quota without explicit permission", r.RunID)
			}
		case string(experiments.CapabilityLocalSmall):
			if !(a.MaxLocalComputeSeconds > 0) {
				add("run %q needs a positive local compute cap", r.RunID)
			}
		}
	}
	for _, c := range []struct {
		name      string
		got, plan []string
	}{
		{"source classes", a.AllowedSourceClasses, plan.AllowedSourceClasses},
		{"network domains", a.AllowedNetworkDomains, plan.AllowedNetworkDomains},
		{"credential refs", a.CredentialRefs, plan.CredentialRefs},
	} {
		set := map[string]bool{}
		for _, v := range c.plan {
			set[v] = true
		}
		for _, v := range c.got {
			if !set[v] {
				add("authorized %s include %q not in the plan", c.name, v)
			}
		}
		set = map[string]bool{}
		for _, v := range c.got {
			set[v] = true
		}
		for _, v := range c.plan {
			if !set[v] {
				add("plan %s %q are not authorized", c.name, v)
			}
		}
	}
	return out
}

func bindingKey(e EndpointBinding) string {
	b, _ := protocol.CanonicalJSON(e)
	return string(b)
}

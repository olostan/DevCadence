package empirical

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/protocol"
)

// measurementUnits is the closed measurement vocabulary and required units.
var measurementUnits = map[string]string{
	"cumulative_input_tokens": "token",
	"cached_input_tokens":     "token",
	"output_tokens":           "token",
	"resident_peak_tokens":    "token",
	"api_spend_usd":           "USD",
	"subscription_quota":      "provider_unit",
	"local_compute_seconds":   "second",
	"wall_seconds":            "second",
	"repair_rounds":           "count",
	"principal_reentries":     "count",
}

// integralMeasurements must be whole nonnegative numbers.
var integralMeasurements = map[string]bool{
	"cumulative_input_tokens": true, "cached_input_tokens": true, "output_tokens": true,
	"resident_peak_tokens": true, "repair_rounds": true, "principal_reentries": true,
}

// admittedSet is verified data retained for honest gate replay.
type admittedSet struct {
	plan            CampaignPlan
	planDigest      string
	manifest        CampaignManifest
	snapshots       []telemetry.RunTelemetrySnapshot
	falsifications  map[string]*experiments.FalsificationResult
	comparable      map[string]bool
	fullCoverage    bool
	missingTiers    []TierLimitation
	outcomes        map[string]VerifiedOutcome
	verifierSources []string
}

// denyAuthority is the only production operatorAuthority: protected operator
// receipt issuance/verification (M5-R4) does not exist, so no authorization is
// authentic and admission fails closed before any verifier effect.
type denyAuthority struct{}

func (denyAuthority) VerifyAuthorization(context.Context, string, string, []byte) error {
	return errors.New("no protected operator-receipt verifier is accepted (M5-R4)")
}

// ValidateAdmission admits a post-run manifest as empirical evidence. It never
// sets Admitted from structure or digests alone: the plan, authorization
// authority and an independently injected verifier must all pass. With no
// accepted operator authority (current state) it always returns Admitted=false
// and makes zero verifier calls.
func ValidateAdmission(ctx context.Context, m CampaignManifest, r ArtifactResolver, v IndependentVerifier) (AdmissionResult, error) {
	return validateAdmission(ctx, m, r, v, denyAuthority{})
}

type collector struct {
	codes map[string]bool
	lims  []string
}

func (c *collector) code(code, f string, a ...any) {
	c.codes[code] = true
	c.lims = append(c.lims, fmt.Sprintf("%s: %s", code, fmt.Sprintf(f, a...)))
}

func fetch(ctx context.Context, r ArtifactResolver, ref, digest string) ([]byte, string, string) {
	if blank(ref) || !validDigest(digest) {
		return nil, ReasonSchemaInvalid, "reference or digest is missing/malformed"
	}
	if r == nil {
		return nil, ReasonArtifactUnavailable, "no artifact resolver"
	}
	b, err := r.ReadVerified(ctx, ref, digest)
	if err != nil {
		return nil, ReasonArtifactUnavailable, fmt.Sprintf("%s: %v", ref, err)
	}
	if bytesDigest(b) != digest {
		return nil, ReasonDigestMismatch, ref + " bytes do not match the declared digest"
	}
	return b, "", ""
}

func (c *collector) load(ctx context.Context, r ArtifactResolver, ref, digest string, into any) bool {
	b, code, why := fetch(ctx, r, ref, digest)
	if code != "" {
		c.code(code, "%s", why)
		return false
	}
	if into == nil {
		return true
	}
	if err := decodeStrict(b, into); err != nil {
		c.code(ReasonSchemaInvalid, "%s: %v", ref, err)
		return false
	}
	return true
}

func validateAdmission(ctx context.Context, m CampaignManifest, r ArtifactResolver, v IndependentVerifier, auth operatorAuthority) (AdmissionResult, error) {
	c := &collector{codes: map[string]bool{}}
	res := AdmissionResult{}
	finish := func() (AdmissionResult, error) {
		for k := range c.codes {
			res.ReasonCodes = append(res.ReasonCodes, k)
		}
		sort.Strings(res.ReasonCodes)
		res.Limitations = append(res.Limitations, c.lims...)
		res.Admitted = len(res.ReasonCodes) == 0 && res.admitted != nil && len(res.admitted.snapshots) > 0
		if res.admitted != nil && !res.Admitted {
			res.admitted = nil
		}
		return res, ctx.Err()
	}

	// 1. Strict plan.
	var plan CampaignPlan
	planOK := c.load(ctx, r, m.PlanRef, m.PlanDigest, &plan)
	if planOK {
		if canon, err := protocol.CanonicalJSON(plan); err != nil || bytesDigest(canon) != m.PlanDigest {
			c.code(ReasonDigestMismatch, "plan artifact is not the canonical encoding of its own digest")
			planOK = false
		}
	}
	if planOK {
		for _, p := range ValidatePlan(plan) {
			c.code(ReasonPlanInvalid, "%s", p)
		}
		if m.CampaignID != plan.CampaignID {
			c.code(ReasonPlanMismatch, "manifest campaign_id differs from the plan")
		}
	}
	if m.SchemaVersion != SchemaVersion {
		c.code(ReasonSchemaInvalid, "manifest schema_version must be %q", SchemaVersion)
	}
	if len(m.RegenerationCommand) == 0 {
		c.code(ReasonSchemaInvalid, "regeneration_command is required")
	}
	for _, a := range m.RegenerationCommand {
		if blank(a) {
			c.code(ReasonSchemaInvalid, "regeneration_command contains a blank argument")
		}
	}
	if !planOK || len(c.codes) > 0 {
		return finish()
	}

	// 2. Authorization binding (structure only; authenticity is checked below).
	var authz CampaignAuthorization
	var authBytes []byte
	if b, code, why := fetch(ctx, r, m.AuthorizationRef, m.AuthorizationDigest); code != "" {
		c.code(code, "authorization: %s", why)
	} else if err := decodeStrict(b, &authz); err != nil {
		c.code(ReasonSchemaInvalid, "authorization: %v", err)
	} else {
		authBytes = b
		for _, p := range validateAuthorization(authz, plan, m.PlanDigest) {
			c.code(ReasonAuthorizationBad, "%s", p)
		}
	}

	// 3. Manifest tier limitations and run-to-plan matching.
	requested := map[string]bool{}
	for _, t := range plan.RequestedTiers {
		requested[t] = true
	}
	if msg := validTierLimitations(m.MissingTiers, requested); msg != "" {
		c.code(ReasonSchemaInvalid, "manifest %s", msg)
	}
	have := map[string]TierLimitation{}
	for _, l := range m.MissingTiers {
		have[l.Tier] = l
	}
	for _, l := range plan.MissingTiers {
		if have[l.Tier] != l {
			c.code(ReasonPlanMismatch, "manifest drops or alters the planned limitation for tier %q", l.Tier)
		}
	}
	planned := map[string]PlannedRun{}
	for _, p := range plan.Runs {
		planned[p.RunID] = p
	}
	seen := map[string]bool{}
	lastOrdinal := 0
	runs := map[string]RunEvidence{}
	for _, run := range m.Runs {
		p, ok := planned[run.RunID]
		switch {
		case !ok:
			c.code(ReasonRunMismatch, "run %q is not in the approved plan", run.RunID)
			continue
		case seen[run.RunID]:
			c.code(ReasonRunMismatch, "run %q is reported more than once", run.RunID)
			continue
		}
		seen[run.RunID] = true
		runs[run.RunID] = run
		if p.Ordinal < lastOrdinal {
			c.code(ReasonRunMismatch, "run %q is out of plan order", run.RunID)
		}
		lastOrdinal = p.Ordinal
		if run.TaskID != p.TaskID || run.TaskDigest != p.TaskDigest || run.Seed != p.Seed ||
			run.Strategy != p.Strategy || run.Repetition != p.Repetition || run.Endpoint != p.Endpoint {
			c.code(ReasonRunMismatch, "run %q identity or endpoint binding differs from the plan", run.RunID)
		}
		checkRun(c, run)
	}
	res.Counts = countRuns(plan, m)
	var falsifications map[string]*experiments.FalsificationResult
	if !blank(m.FalsificationEvidenceRef) || m.FalsificationEvidenceDigest != "" {
		var fm map[string]*experiments.FalsificationResult
		if c.load(ctx, r, m.FalsificationEvidenceRef, m.FalsificationEvidenceDigest, &fm) {
			falsifications = fm
			for k, f := range fm {
				if f == nil {
					c.code(ReasonFalsificationBad, "falsification entry %q is null", k)
				}
			}
		}
	}

	// Per-run artifacts and snapshots of completed runs.
	snaps := map[string]telemetry.RunTelemetrySnapshot{}
	for _, p := range plan.Runs {
		run, ok := runs[p.RunID]
		if !ok || run.Status != StatusCompleted || len(c.codes) > 0 {
			continue
		}
		var s telemetry.RunTelemetrySnapshot
		if c.load(ctx, r, run.SnapshotRef, run.SnapshotDigest, &s) && checkSnapshot(c, run, s) &&
			c.load(ctx, r, run.CandidateArtifactRef, run.CandidateArtifactDigest, nil) &&
			c.load(ctx, r, run.VerifierReceiptRef, run.VerifierReceiptDigest, nil) {
			snaps[run.RunID] = s
		}
	}
	if len(c.codes) > 0 {
		return finish()
	}

	// 4. Operator authority: absent verifier means no authentic authorization and
	// no verifier effect is ever started.
	if err := auth.VerifyAuthorization(ctx, m.PlanDigest, m.AuthorizationDigest, authBytes); err != nil {
		c.code(ReasonOperatorAuthority, "%v", err)
	}
	completed := 0
	for _, run := range m.Runs {
		if run.Status == StatusCompleted {
			completed++
		}
	}
	if v == nil && completed > 0 {
		c.code(ReasonUnverifiedOutcome, "no independent verifier is available for completed runs")
	}
	if len(c.codes) > 0 {
		return finish()
	}

	// 5. Independent verification of each completed run (plan order).
	verified := map[string]VerifiedOutcome{}
	for _, p := range plan.Runs {
		run, ok := runs[p.RunID]
		if !ok || run.Status != StatusCompleted {
			continue
		}
		if ctx.Err() != nil {
			c.code(ReasonUnverifiedOutcome, "cancelled before run %q was verified", p.RunID)
			break
		}
		out, err := v.Verify(ctx, plan, p, run, r)
		if err != nil {
			c.code(ReasonUnverifiedOutcome, "verifier failed for run %q: %v", p.RunID, err)
			continue
		}
		if !crossCheckOutcome(c, m, run, out) {
			continue
		}
		if !crossCheckSnapshot(c, run, out, snaps[p.RunID]) {
			continue
		}
		verified[p.RunID] = out
	}
	if len(c.codes) > 0 {
		return finish()
	}

	// 6. Pairing: both strategies of identical task/seed/repetition/endpoint.
	byPair := map[string][]string{}
	for _, p := range plan.Runs {
		k := pairKey(p.TaskID, p.TaskDigest, p.Seed, p.Repetition, p.Endpoint)
		byPair[k] = append(byPair[k], p.RunID)
	}
	set := &admittedSet{plan: plan, planDigest: m.PlanDigest, manifest: m, falsifications: falsifications,
		missingTiers: m.MissingTiers, outcomes: map[string]VerifiedOutcome{}}
	admittedRuns := map[string]bool{}
	for _, p := range plan.Runs {
		ids := byPair[pairKey(p.TaskID, p.TaskDigest, p.Seed, p.Repetition, p.Endpoint)]
		complete := true
		for _, id := range ids {
			if _, ok := verified[id]; !ok {
				complete = false
			}
		}
		if complete {
			admittedRuns[p.RunID] = true
		}
	}
	for _, p := range plan.Runs {
		if admittedRuns[p.RunID] {
			set.snapshots = append(set.snapshots, snaps[p.RunID])
			set.outcomes[p.RunID] = verified[p.RunID]
			res.CompletedRunIDs = append(res.CompletedRunIDs, p.RunID)
		} else {
			res.ExcludedRunIDs = append(res.ExcludedRunIDs, p.RunID)
		}
		if _, ok := verified[p.RunID]; ok && !admittedRuns[p.RunID] {
			c.lims = append(c.lims, fmt.Sprintf("INCOMPLETE_PAIR: run %q is verified but its strategy partner is not; excluded from comparison", p.RunID))
		}
	}
	if n := len(plan.Runs) - len(m.Runs); n > 0 {
		c.lims = append(c.lims, fmt.Sprintf("INCOMPLETE_COVERAGE: %d planned runs are absent from the manifest (no zero-valued results manufactured)", n))
	}
	for _, l := range m.MissingTiers {
		c.lims = append(c.lims, fmt.Sprintf("MISSING_TIER: %s (%s, evidence %s); all-tier validation cannot be claimed", l.Tier, l.Cause, l.EvidenceRef))
	}
	set.fullCoverage = len(admittedRuns) == len(plan.Runs) && len(m.MissingTiers) == 0
	set.comparable = comparableMetrics(set, runs)
	for k := range set.comparable {
		res.ComparableResourceMetrics = append(res.ComparableResourceMetrics, k)
	}
	sort.Strings(res.ComparableResourceMetrics)
	if len(set.snapshots) == 0 {
		c.code(ReasonUnverifiedOutcome, "no independently verified completed paired run")
	}
	res.admitted = set
	return finish()
}

func countRuns(plan CampaignPlan, m CampaignManifest) RunCounts {
	n := RunCounts{Planned: len(plan.Runs), Reported: len(m.Runs)}
	n.Missing = n.Planned - n.Reported
	for _, run := range m.Runs {
		switch run.Status {
		case StatusCompleted:
			n.Completed++
			if run.Accepted {
				n.Accepted++
			}
		case StatusFailed:
			n.Failed++
		case StatusBlocked:
			n.Blocked++
		case StatusCancelled:
			n.Cancelled++
		case StatusSkipped:
			n.Skipped++
		}
	}
	n.Attempted = n.Reported - n.Skipped
	return n
}

// checkRun validates one produced run's own consistency (not plan matching).
func checkRun(c *collector, run RunEvidence) {
	switch run.Status {
	case StatusCompleted, StatusFailed, StatusBlocked, StatusCancelled, StatusSkipped:
	default:
		c.code(ReasonRunInvalid, "run %q status %q is not closed vocabulary", run.RunID, run.Status)
		return
	}
	if syntheticMarker(run.Endpoint.EndpointID, run.Endpoint.DriverID, run.Endpoint.ModelID, run.Endpoint.ChannelID,
		run.InvocationProducerID, run.VerifierProducerID) {
		c.code(ReasonSyntheticEvidence, "run %q carries a synthetic/scripted marker; synthetic evidence cannot be relabeled empirical", run.RunID)
	}
	if run.Accepted && run.Status != StatusCompleted {
		c.code(ReasonRunInvalid, "run %q is accepted but not completed", run.RunID)
	}
	checkMeasurements(c, run)
	if run.Status != StatusCompleted {
		return
	}
	for name, val := range map[string]string{
		"session_evidence_ref": run.SessionEvidenceRef, "candidate_artifact_ref": run.CandidateArtifactRef,
		"candidate_commit": run.CandidateCommit, "invocation_producer_id": run.InvocationProducerID,
		"verifier_producer_id": run.VerifierProducerID, "snapshot_ref": run.SnapshotRef,
		"verifier_receipt_ref": run.VerifierReceiptRef,
	} {
		if blank(val) {
			c.code(ReasonRunInvalid, "completed run %q lacks %s (a completion claim without actual candidate and receipt is not accepted work)", run.RunID, name)
		}
	}
	for name, val := range map[string]string{
		"prompt_digest": run.PromptDigest, "candidate_artifact_digest": run.CandidateArtifactDigest,
		"snapshot_digest": run.SnapshotDigest, "verifier_receipt_digest": run.VerifierReceiptDigest,
	} {
		if !validDigest(val) {
			c.code(ReasonRunInvalid, "completed run %q has no valid %s", run.RunID, name)
		}
	}
	if run.InvocationProducerID == run.VerifierProducerID {
		c.code(ReasonNotIndependent, "run %q: worker and verifier producer are the same", run.RunID)
	}
}

func checkMeasurements(c *collector, run RunEvidence) {
	if len(run.Measurements) != len(measurementUnits) {
		c.code(ReasonMeasurementInvalid, "run %q must carry exactly the %d closed measurement keys", run.RunID, len(measurementUnits))
	}
	keys := make([]string, 0, len(run.Measurements))
	for k := range run.Measurements {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		unit, ok := measurementUnits[k]
		if !ok {
			c.code(ReasonMeasurementInvalid, "run %q: unknown measurement key %q", run.RunID, k)
			continue
		}
		ms := run.Measurements[k]
		switch {
		case ms.Unit != unit:
			c.code(ReasonMeasurementInvalid, "run %q: %s unit must be %q", run.RunID, k, unit)
		case blank(ms.EvidenceRef):
			c.code(ReasonMeasurementInvalid, "run %q: %s has no evidence_ref", run.RunID, k)
		case ms.Known:
			if ms.Value == nil || !finiteNonNeg(*ms.Value) || ms.Provenance != ProvenanceMeasured ||
				(integralMeasurements[k] && *ms.Value != math.Trunc(*ms.Value)) {
				c.code(ReasonMeasurementInvalid, "run %q: known %s needs a finite nonnegative (integral where counted) value with measured provenance", run.RunID, k)
			}
			if k == "subscription_quota" && (blank(run.Endpoint.SubscriptionQuotaUnit) || run.Endpoint.SubscriptionQuotaUnit == UnknownQuotaUnit) {
				c.code(ReasonMeasurementInvalid, "run %q: known subscription_quota requires a pinned provider unit", run.RunID)
			}
		default:
			if ms.Value != nil || ms.Provenance != ProvenanceUnknown {
				c.code(ReasonMeasurementInvalid, "run %q: unknown %s must have a nil value and unknown provenance (never zero)", run.RunID, k)
			}
		}
	}
}

func hasUnknown(ms map[string]Measurement) bool {
	for _, m := range ms {
		if !m.Known {
			return true
		}
	}
	return false
}

// crossCheckOutcome compares trusted verifier output with the run and plan.
func crossCheckOutcome(c *collector, m CampaignManifest, run RunEvidence, o VerifiedOutcome) bool {
	ok := true
	bad := func(code, f string, a ...any) { ok = false; c.code(code, f, a...) }
	if o.RunID != run.RunID || o.PlanDigest != m.PlanDigest || o.CandidateDigest != run.CandidateArtifactDigest ||
		o.SnapshotDigest != run.SnapshotDigest || o.ReceiptDigest != run.VerifierReceiptDigest {
		bad(ReasonVerifierRejected, "run %q: verified outcome digests/identity differ from the run (receipt for another candidate or plan)", run.RunID)
	}
	if !validDigest(o.SessionDigest) || !validDigest(o.VerificationProfileDigest) || blank(o.VerifierSourceCommit) ||
		len(o.VerifiedCommandArtifactRefs) == 0 {
		bad(ReasonVerifierRejected, "run %q: verified outcome lacks session/profile/source/command evidence", run.RunID)
	}
	werr := o.Worker.Validate("worker", protocol.ProvenanceRoleImplementer)
	if werr != nil && o.Worker.Validate("worker", protocol.ProvenanceRoleReviewer) != nil {
		bad(ReasonNotIndependent, "run %q: worker provenance invalid: %v", run.RunID, werr)
	}
	if err := o.Verifier.Validate("verifier", protocol.ProvenanceRoleVerifier); err != nil {
		bad(ReasonNotIndependent, "run %q: verifier provenance invalid: %v", run.RunID, err)
	}
	if o.Worker.ActorID != run.InvocationProducerID || o.Verifier.ActorID != run.VerifierProducerID {
		bad(ReasonNotIndependent, "run %q: provenance actors differ from the run producer identities", run.RunID)
	}
	if !protocol.ActorsIndependent(o.Worker, o.Verifier) {
		bad(ReasonNotIndependent, "run %q: verifier is not independent of the worker", run.RunID)
	}
	if o.QualityVerdict != QualityAccepted && o.QualityVerdict != QualityRejected {
		bad(ReasonVerifierRejected, "run %q: quality verdict %q is not closed vocabulary", run.RunID, o.QualityVerdict)
	}
	if run.Accepted != (o.QualityVerdict == QualityAccepted) {
		bad(ReasonVerifierRejected, "run %q: accepted flag contradicts the independently verified verdict", run.RunID)
	}
	if o.SeededDefectTotal < 0 || o.SeededDefectsCaught < 0 || o.SeededDefectsCaught > o.SeededDefectTotal {
		bad(ReasonVerifierRejected, "run %q: seeded defect counts are inconsistent", run.RunID)
	}
	return ok
}

// checkSnapshot binds the legacy telemetry snapshot to the run's measurements so
// no numeric field can substitute zero for unknown (pre-verification, no effects).
func checkSnapshot(c *collector, run RunEvidence, s telemetry.RunTelemetrySnapshot) bool {
	ok := true
	bad := func(f string, a ...any) { ok = false; c.code(ReasonRunInvalid, f, a...) }
	if s.RunID != run.RunID || s.TaskID != run.TaskID || s.Strategy != run.Strategy || s.Capability != run.Endpoint.CapabilityClass {
		bad("run %q: snapshot identity differs from the run", run.RunID)
	}
	if s.Accepted != run.Accepted {
		bad("run %q: snapshot accepted flag differs from the run", run.RunID)
	}
	if hasUnknown(run.Measurements) && !s.AccountingUncertain {
		bad("run %q: snapshot hides unknown measurements (accounting_uncertain must be true)", run.RunID)
	}
	tokenFields := map[string]int64{
		"cumulative_input_tokens": s.CumulativeInputTokens, "cached_input_tokens": s.CachedTokens,
		"output_tokens": s.OutputTokens, "resident_peak_tokens": s.PeakResidentTokens,
	}
	for k, got := range tokenFields {
		ms, present := run.Measurements[k]
		if !present || ms.Known && (ms.Value == nil || float64(got) != *ms.Value) || !ms.Known && got != 0 {
			bad("run %q: snapshot %s does not equal the measurement (unknown stays a zero placeholder, never a value)", run.RunID, k)
		}
	}
	if w, present := run.Measurements["wall_seconds"]; !present || w.Known && (w.Value == nil || int64(s.Duration) != int64(math.Round(*w.Value*1e9))) ||
		!w.Known && s.Duration != 0 {
		bad("run %q: snapshot duration does not equal wall_seconds", run.RunID)
	}
	return ok
}

// crossCheckSnapshot binds the snapshot's defect fields to the verified outcome.
func crossCheckSnapshot(c *collector, run RunEvidence, o VerifiedOutcome, s telemetry.RunTelemetrySnapshot) bool {
	ok := true
	bad := func(f string, a ...any) { ok = false; c.code(ReasonRunInvalid, f, a...) }
	if s.DefectSeeded != (o.SeededDefectTotal > 0) {
		bad("run %q: snapshot defect_seeded differs from the verified seeded-defect count", run.RunID)
	} else if s.DefectSeeded && (s.DefectStatus == "detected" || s.DefectStatus == "prevented") != (o.SeededDefectsCaught == o.SeededDefectTotal) {
		bad("run %q: snapshot defect status differs from the verified catch count", run.RunID)
	}
	return ok
}

// comparableMetrics lists measurements known in every admitted run.
func comparableMetrics(set *admittedSet, runs map[string]RunEvidence) map[string]bool {
	out := map[string]bool{}
	for k := range measurementUnits {
		all := len(set.snapshots) > 0
		for id := range set.outcomes {
			if !runs[id].Measurements[k].Known {
				all = false
			}
		}
		if all {
			out[k] = true
		}
	}
	return out
}

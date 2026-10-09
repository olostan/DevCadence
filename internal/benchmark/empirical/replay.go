package empirical

import (
	"sort"
	"strings"

	"github.com/olostan/DevCadence/internal/benchmark/gate"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// Empirical conclusions are separate from the raw gate decision (R7).
const (
	ConclusionGo           = "go"
	ConclusionRevise       = "revise"
	ConclusionInconclusive = "inconclusive"
)

// VerifierIdentity records the live session and verifier behind an admitted run.
type VerifierIdentity struct {
	RunID                string `json:"run_id"`
	SessionDigest        string `json:"session_digest"`
	ReceiptDigest        string `json:"receipt_digest"`
	WorkerActorID        string `json:"worker_actor_id"`
	VerifierActorID      string `json:"verifier_actor_id"`
	VerifierSourceCommit string `json:"verifier_source_commit"`
}

// Report separates Admission, RawGateDecision and EmpiricalConclusion. It is
// pure data; writing m5-m4-empirical-report files belongs to the live campaign.
type Report struct {
	Provenance        string                     `json:"provenance"`
	ExecutionMode     string                     `json:"execution_mode,omitempty"`
	CampaignID        string                     `json:"campaign_id"`
	PlanDigest        string                     `json:"plan_digest"`
	SourceCommit      string                     `json:"source_commit"`
	Admission         AdmissionResult            `json:"admission"`
	RawGate           *gate.GateEvaluationResult `json:"raw_gate_decision"`
	RawGateSkipped    string                     `json:"raw_gate_skipped_reason,omitempty"`
	Conclusion        string                     `json:"empirical_conclusion"`
	ConclusionReasons []string                   `json:"conclusion_reasons"`
	Verifiers         []VerifierIdentity         `json:"verifiers"`
	MissingTiers      []TierLimitation           `json:"missing_tiers"`
}

// ReplayGate runs the unchanged M4 gate on the admitted snapshots only. A result
// that did not come from a successful ValidateAdmission is refused, criteria must
// equal the pinned defaults and the plan's CriteriaDigest, and unknown required
// token accounting skips the numeric gate rather than feeding zeros.
func ReplayGate(res AdmissionResult, criteria gate.GateCriteria) (*Report, error) {
	const kind = "ReplayGate"
	set := res.admitted
	if !res.Admitted || set == nil {
		return nil, errs.New(errs.CategoryPolicyDenied, "%s: evidence is not admitted as empirical (reasons %v)", kind, res.ReasonCodes)
	}
	if criteria != gate.DefaultM4GateCriteria() {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: criteria must equal the pinned predeclared defaults", kind)
	}
	if d, err := protocol.Digest(criteria); err != nil || d != set.plan.CriteriaDigest {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: criteria digest differs from the plan's CriteriaDigest", kind)
	}

	rep := &Report{
		Provenance: gate.EvidenceKindEmpiricalCampaign, CampaignID: set.plan.CampaignID,
		PlanDigest: set.planDigest, SourceCommit: set.plan.SourceCommit,
		Admission: res, MissingTiers: set.missingTiers, ConclusionReasons: []string{},
	}
	rep.Admission.admitted = nil
	for _, p := range set.plan.Runs {
		if o, ok := set.outcomes[p.RunID]; ok {
			rep.Verifiers = append(rep.Verifiers, VerifierIdentity{RunID: p.RunID, SessionDigest: o.SessionDigest,
				ReceiptDigest: o.ReceiptDigest, WorkerActorID: o.Worker.ActorID, VerifierActorID: o.Verifier.ActorID,
				VerifierSourceCommit: o.VerifierSourceCommit})
		}
	}

	if !(set.comparable["cumulative_input_tokens"] && set.comparable["resident_peak_tokens"]) {
		rep.RawGateSkipped = "required token accounting is unknown in at least one admitted run; the numeric gate is not fed zeros"
		rep.Conclusion = ConclusionInconclusive
		rep.ConclusionReasons = append(rep.ConclusionReasons, "resource comparison unavailable: unknown required token accounting")
		return rep, nil
	}

	agg, err := telemetry.Aggregate(set.snapshots)
	if err != nil {
		return nil, err
	}
	raw, err := gate.EvaluateM4Gate(agg, set.falsifications, criteria)
	if err != nil {
		return nil, err
	}
	drivers := map[string]bool{}
	for _, p := range set.plan.Runs {
		drivers[p.Endpoint.DriverID] = true
	}
	ids := make([]string, 0, len(drivers))
	for d := range drivers {
		ids = append(ids, d)
	}
	sort.Strings(ids)
	prov, err := gate.NewEvidenceProvenance(gate.EvidenceKindEmpiricalCampaign, strings.Join(ids, ","),
		set.plan.SourceCommit, strings.Join(set.manifest.RegenerationCommand, " "))
	if err != nil {
		return nil, err
	}
	raw.Provenance = prov
	rep.RawGate = raw

	applicable := 0
	for _, f := range set.falsifications {
		if f != nil && f.IsApplicable {
			applicable++
		}
	}
	switch raw.Decision {
	case gate.DecisionRevise:
		rep.Conclusion = ConclusionRevise
		rep.ConclusionReasons = append(rep.ConclusionReasons, "observed gate criteria failed on admitted evidence")
	case gate.DecisionGo:
		rep.Conclusion = ConclusionGo
		if applicable == 0 {
			rep.Conclusion = ConclusionInconclusive
			rep.ConclusionReasons = append(rep.ConclusionReasons, "no applicable falsification evidence: delegation floor unverified")
		}
		if !set.fullCoverage {
			rep.Conclusion = ConclusionInconclusive
			rep.ConclusionReasons = append(rep.ConclusionReasons, "declared coverage incomplete (absent/unpaired runs or missing tiers)")
		}
	default:
		rep.Conclusion = ConclusionInconclusive
		rep.ConclusionReasons = append(rep.ConclusionReasons, "raw gate is inconclusive")
	}
	return rep, nil
}

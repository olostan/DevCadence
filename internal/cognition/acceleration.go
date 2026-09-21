package cognition

import (
	"sort"
	"strings"

	"github.com/olostan/DevCadience/internal/protocol"
)

// Acceleration evaluation is the single place a backend may be called verified.
//
// It is a pure function. Given the same candidate assessment and the same probe
// signals it always reaches the same conclusion, which is what makes DCI-106
// testable rather than aspirational:
//
//	"GPU presence, driver presence or runtime installation is insufficient
//	 evidence that inference is accelerated."
//
// The rules below are deliberately conservative in one direction only. Every
// ambiguity resolves to unverified; nothing resolves to verified by default,
// and there is no code path that reaches verified without an authoritative
// offload signal from the runtime that ran the inference.

// AccelerationInput is everything the evaluation may consider.
type AccelerationInput struct {
	// Backend is the backend whose use is in question.
	Backend protocol.BackendKind
	// Candidate is the assessment of that backend on this machine, if any. It
	// bounds the result: a backend assessed unsupported cannot be verified,
	// whatever a signal claims.
	Candidate *protocol.AcceleratorCandidate
	// Signals are the observations from the probe. An empty list means nothing
	// was observed about the backend.
	Signals []protocol.AccelerationSignal
	// ProbeRan reports whether an inference probe actually executed. Without
	// it the result can never exceed the candidate's own state.
	ProbeRan bool
	// ProbeSucceeded reports whether that inference produced output. Inference
	// succeeding is necessary for verification and nowhere near sufficient.
	ProbeSucceeded bool
	// ObservedAt is the injected instant used as verified_at.
	ObservedAt     protocol.Timestamp
	RuntimeVersion string
	DeviceID       string
	ProbeRefs      []string
}

// EvaluateAcceleration derives the acceleration claim from the evidence.
func EvaluateAcceleration(in AccelerationInput) protocol.AccelerationEvidence {
	evidence := protocol.AccelerationEvidence{
		Backend:        in.Backend,
		Signals:        sortSignals(in.Signals),
		RuntimeVersion: in.RuntimeVersion,
		DeviceID:       in.DeviceID,
		ProbeRefs:      append([]string(nil), in.ProbeRefs...),
	}
	sort.Strings(evidence.ProbeRefs)

	// A backend this build believes cannot work here stays unsupported. A
	// signal claiming otherwise is recorded but does not promote the state:
	// the compatibility judgement and the observation disagree, and that is a
	// contradiction to surface rather than resolve by preferring one.
	if in.Candidate != nil && in.Candidate.Support == protocol.SupportUnsupported {
		evidence.State = protocol.StateUnsupported
		if offloadClaimed(in.Signals, in.Backend) {
			evidence.Conflicts = append(evidence.Conflicts,
				"a signal reports offload to "+string(in.Backend)+
					" while this build assesses that backend as unsupported on this device")
		}
		return finish(evidence)
	}

	// Without a probe, the state is whatever the candidate assessment
	// established — never more. This is the "GPU installed, runtime installed,
	// nothing proven" case.
	if !in.ProbeRan {
		evidence.State = candidateState(in.Candidate)
		return finish(evidence)
	}

	authoritativeOffload := false
	authoritativeCPUFallback := false
	contradiction := false
	for _, signal := range in.Signals {
		switch {
		case signal.Trust == protocol.TrustAuthoritative && signal.Backend == in.Backend && signal.Offloaded:
			authoritativeOffload = true
		case signal.Trust == protocol.TrustAuthoritative && signal.Offloaded == false &&
			(signal.Backend == in.Backend || signal.Backend == protocol.BackendCPU):
			// The runtime says it did not offload. That is a real answer, and
			// it is the CPU-fallback case a user most needs told about.
			authoritativeCPUFallback = true
		}
	}
	for _, signal := range in.Signals {
		if signal.Trust != protocol.TrustCorroborating || signal.Backend != in.Backend {
			continue
		}
		if authoritativeOffload && !signal.Offloaded {
			contradiction = true
			evidence.Conflicts = append(evidence.Conflicts,
				"the runtime reports offload to "+string(in.Backend)+" but "+signal.Source+
					" does not corroborate it")
		}
	}
	if authoritativeOffload && authoritativeCPUFallback {
		contradiction = true
		evidence.Conflicts = append(evidence.Conflicts,
			"authoritative signals disagree about whether "+string(in.Backend)+" was used")
	}

	switch {
	case !in.ProbeSucceeded:
		// The probe ran and inference did not work. Nothing about the backend
		// was established, so the candidate's state stands.
		evidence.State = candidateState(in.Candidate)
	case contradiction:
		// Contradicted evidence verifies nothing. Reporting the contradiction
		// is more useful than choosing an observer to believe.
		evidence.State = protocol.StateUnverified
	case authoritativeOffload:
		evidence.State = protocol.StateVerified
		verifiedAt := in.ObservedAt
		evidence.VerifiedAt = &verifiedAt
	case authoritativeCPUFallback:
		// Inference works and demonstrably did not use the intended backend.
		// This is a distinct, actionable state: the silent CPU fallback
		// ADR-0011 §4 exists to make visible.
		evidence.State = protocol.StateFailed
	default:
		// Inference worked but nothing authoritative described the backend.
		// "Probably GPU" is not an available answer.
		evidence.State = protocol.StateUnverified
	}
	return finish(evidence)
}

// candidateState clamps a candidate's state to what assessment alone may claim.
func candidateState(candidate *protocol.AcceleratorCandidate) protocol.AccelerationState {
	if candidate == nil {
		return protocol.StateUnknown
	}
	switch candidate.State {
	case protocol.StateVerified, protocol.StateFailed:
		// A candidate arriving here already claiming verification would mean
		// the assessment layer overreached; clamp rather than propagate.
		return protocol.StateUnverified
	case "":
		return protocol.StateUnknown
	default:
		return candidate.State
	}
}

func offloadClaimed(signals []protocol.AccelerationSignal, backend protocol.BackendKind) bool {
	for _, signal := range signals {
		if signal.Backend == backend && signal.Offloaded {
			return true
		}
	}
	return false
}

// finish imposes deterministic ordering and drops the verification instant from
// any state that is not verified, so the record cannot contradict itself.
func finish(evidence protocol.AccelerationEvidence) protocol.AccelerationEvidence {
	if evidence.State != protocol.StateVerified {
		evidence.VerifiedAt = nil
	}
	sort.Strings(evidence.Conflicts)
	evidence.Conflicts = dedupe(evidence.Conflicts)
	return evidence
}

func sortSignals(signals []protocol.AccelerationSignal) []protocol.AccelerationSignal {
	if len(signals) == 0 {
		return nil
	}
	out := append([]protocol.AccelerationSignal(nil), signals...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Backend < out[j].Backend
	})
	return out
}

func dedupe(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	var previous string
	for i, value := range values {
		if i > 0 && value == previous {
			continue
		}
		previous = value
		out = append(out, value)
	}
	return out
}

// Describe renders an acceleration claim for an operator.
//
// It always states the backend and the state, and never renders a bare "yes":
// the difference between verified, unverified and failed is the whole content
// of the claim.
func Describe(evidence *protocol.AccelerationEvidence) string {
	if evidence == nil {
		return "not applicable"
	}
	parts := []string{string(evidence.Backend) + ": " + string(evidence.State)}
	if len(evidence.Conflicts) > 0 {
		parts = append(parts, "conflicting evidence: "+strings.Join(evidence.Conflicts, "; "))
	}
	return strings.Join(parts, " — ")
}

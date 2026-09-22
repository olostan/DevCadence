package cognition_test

import (
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

func at() protocol.Timestamp {
	return protocol.NewTimestamp(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC))
}

func authoritative(backend protocol.BackendKind, offloaded bool) protocol.AccelerationSignal {
	return protocol.AccelerationSignal{
		Source: "runtime:residency", Trust: protocol.TrustAuthoritative,
		Backend: backend, Offloaded: offloaded, Statement: "runtime reported residency",
	}
}

func corroborating(backend protocol.BackendKind, offloaded bool) protocol.AccelerationSignal {
	return protocol.AccelerationSignal{
		Source: "vendor:telemetry", Trust: protocol.TrustCorroborating,
		Backend: backend, Offloaded: offloaded, Statement: "vendor telemetry",
	}
}

func indicative(backend protocol.BackendKind) protocol.AccelerationSignal {
	return protocol.AccelerationSignal{
		Source: "os:driver_loaded", Trust: protocol.TrustIndicative,
		Backend: backend, Offloaded: true, Statement: "driver module is loaded",
	}
}

// TestGPUDetectedButUnverifiedWithoutAProbe is DCI-106 stated as a test: every
// precondition met and the answer is still "not verified".
func TestGPUDetectedButUnverifiedWithoutAProbe(t *testing.T) {
	candidate := protocol.AcceleratorCandidate{
		Backend: protocol.BackendCUDA, Support: protocol.SupportSupported,
		State: protocol.StateRuntimeAvailable,
	}
	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend: protocol.BackendCUDA, Candidate: &candidate, ObservedAt: at(),
	})
	if evidence.State != protocol.StateRuntimeAvailable {
		t.Errorf("state = %q, want runtime_available", evidence.State)
	}
	if evidence.VerifiedAt != nil {
		t.Error("a verification instant was recorded without a probe")
	}
	if err := evidence.Validate(); err != nil {
		t.Fatalf("evidence violates the contract: %v", err)
	}
}

// TestIndicativeSignalsNeverVerify closes the "driver is loaded, therefore it is
// accelerated" path.
func TestIndicativeSignalsNeverVerify(t *testing.T) {
	candidate := protocol.AcceleratorCandidate{
		Backend: protocol.BackendVulkan, Support: protocol.SupportSupported,
		State: protocol.StateRuntimeAvailable,
	}
	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend:        protocol.BackendVulkan,
		Candidate:      &candidate,
		Signals:        []protocol.AccelerationSignal{indicative(protocol.BackendVulkan)},
		ProbeRan:       true,
		ProbeSucceeded: true,
		ObservedAt:     at(),
	})
	if evidence.State != protocol.StateUnverified {
		t.Errorf("state = %q, want unverified; indicative evidence must not verify", evidence.State)
	}
}

// TestInferenceSuccessAloneIsNotAcceleration is the other half of DCI-106:
// generation worked and nothing said where.
func TestInferenceSuccessAloneIsNotAcceleration(t *testing.T) {
	candidate := protocol.AcceleratorCandidate{
		Backend: protocol.BackendMetal, Support: protocol.SupportSupported,
		State: protocol.StateRuntimeAvailable,
	}
	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend: protocol.BackendMetal, Candidate: &candidate,
		ProbeRan: true, ProbeSucceeded: true, ObservedAt: at(),
	})
	if evidence.State != protocol.StateUnverified {
		t.Errorf("state = %q, want unverified", evidence.State)
	}
}

func TestAuthoritativeOffloadVerifies(t *testing.T) {
	candidate := protocol.AcceleratorCandidate{
		Backend: protocol.BackendMetal, Support: protocol.SupportSupported,
		State: protocol.StateRuntimeAvailable,
	}
	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend:        protocol.BackendMetal,
		Candidate:      &candidate,
		Signals:        []protocol.AccelerationSignal{authoritative(protocol.BackendMetal, true)},
		ProbeRan:       true,
		ProbeSucceeded: true,
		ObservedAt:     at(),
		RuntimeVersion: "0.12.3",
		DeviceID:       "apple:gpu",
	})
	if evidence.State != protocol.StateVerified {
		t.Fatalf("state = %q, want verified", evidence.State)
	}
	if evidence.VerifiedAt == nil {
		t.Error("verified without recording when")
	}
	if evidence.RuntimeVersion != "0.12.3" || evidence.DeviceID != "apple:gpu" {
		t.Error("the verification did not pin the software and hardware it applies to")
	}
	if err := evidence.Validate(); err != nil {
		t.Fatalf("verified evidence violates the contract: %v", err)
	}
}

// TestCorroborationStrengthensButIsNotRequired reflects the rule that an
// authoritative runtime signal suffices on its own.
func TestCorroborationStrengthensButIsNotRequired(t *testing.T) {
	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend: protocol.BackendCUDA,
		Signals: []protocol.AccelerationSignal{
			authoritative(protocol.BackendCUDA, true),
			corroborating(protocol.BackendCUDA, true),
		},
		ProbeRan: true, ProbeSucceeded: true, ObservedAt: at(),
	})
	if evidence.State != protocol.StateVerified {
		t.Errorf("state = %q, want verified", evidence.State)
	}
	if len(evidence.Conflicts) != 0 {
		t.Errorf("agreeing signals produced conflicts: %v", evidence.Conflicts)
	}
}

// TestCPUFallbackIsDetectedRatherThanHidden is the silent-fallback case
// ADR-0011 §4 exists to surface.
func TestCPUFallbackIsDetectedRatherThanHidden(t *testing.T) {
	candidate := protocol.AcceleratorCandidate{
		Backend: protocol.BackendCUDA, Support: protocol.SupportSupported,
		State: protocol.StateRuntimeAvailable,
	}
	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend:        protocol.BackendCUDA,
		Candidate:      &candidate,
		Signals:        []protocol.AccelerationSignal{authoritative(protocol.BackendCPU, false)},
		ProbeRan:       true,
		ProbeSucceeded: true,
		ObservedAt:     at(),
	})
	if evidence.State != protocol.StateFailed {
		t.Errorf("state = %q, want failed; the runtime said it used the cpu", evidence.State)
	}
	if evidence.VerifiedAt != nil {
		t.Error("a failed backend recorded a verification instant")
	}
}

// TestConflictingEvidenceStaysUnverifiedAndReportsTheContradiction is the
// "do not guess which observer to believe" rule.
func TestConflictingEvidenceStaysUnverifiedAndReportsTheContradiction(t *testing.T) {
	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend: protocol.BackendCUDA,
		Signals: []protocol.AccelerationSignal{
			authoritative(protocol.BackendCUDA, true),
			corroborating(protocol.BackendCUDA, false),
		},
		ProbeRan: true, ProbeSucceeded: true, ObservedAt: at(),
	})
	if evidence.State != protocol.StateUnverified {
		t.Errorf("state = %q, want unverified", evidence.State)
	}
	if len(evidence.Conflicts) == 0 {
		t.Fatal("the contradiction was not reported")
	}
	if err := evidence.Validate(); err != nil {
		t.Fatalf("evidence violates the contract: %v", err)
	}
}

// TestAnUnsupportedBackendCannotBeVerifiedByASignal keeps a claim from
// overriding the compatibility judgement silently.
func TestAnUnsupportedBackendCannotBeVerifiedByASignal(t *testing.T) {
	candidate := protocol.AcceleratorCandidate{
		Backend: protocol.BackendMetal, Support: protocol.SupportUnsupported,
		State: protocol.StateUnsupported,
	}
	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend:        protocol.BackendMetal,
		Candidate:      &candidate,
		Signals:        []protocol.AccelerationSignal{authoritative(protocol.BackendMetal, true)},
		ProbeRan:       true,
		ProbeSucceeded: true,
		ObservedAt:     at(),
	})
	if evidence.State != protocol.StateUnsupported {
		t.Errorf("state = %q, want unsupported", evidence.State)
	}
	if len(evidence.Conflicts) == 0 {
		t.Error("the disagreement between assessment and observation was not surfaced")
	}
}

// TestFailedInferenceDoesNotEstablishAnything checks that a probe that ran and
// broke leaves the candidate's own state standing.
func TestFailedInferenceDoesNotEstablishAnything(t *testing.T) {
	candidate := protocol.AcceleratorCandidate{
		Backend: protocol.BackendROCm, Support: protocol.SupportUncertain,
		State: protocol.StateCandidate,
	}
	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend: protocol.BackendROCm, Candidate: &candidate,
		ProbeRan: true, ProbeSucceeded: false, ObservedAt: at(),
	})
	if evidence.State != protocol.StateCandidate {
		t.Errorf("state = %q, want candidate", evidence.State)
	}
}

// TestEvaluationIsDeterministicRegardlessOfSignalOrder protects digest
// stability.
func TestEvaluationIsDeterministicRegardlessOfSignalOrder(t *testing.T) {
	forward := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend: protocol.BackendCUDA,
		Signals: []protocol.AccelerationSignal{
			authoritative(protocol.BackendCUDA, true), corroborating(protocol.BackendCUDA, true),
		},
		ProbeRan: true, ProbeSucceeded: true, ObservedAt: at(),
	})
	reversed := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend: protocol.BackendCUDA,
		Signals: []protocol.AccelerationSignal{
			corroborating(protocol.BackendCUDA, true), authoritative(protocol.BackendCUDA, true),
		},
		ProbeRan: true, ProbeSucceeded: true, ObservedAt: at(),
	})
	forwardDigest, err := protocol.Digest(forward)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	reversedDigest, err := protocol.Digest(reversed)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if forwardDigest != reversedDigest {
		t.Error("signal ordering changed the serialised evidence")
	}
}

// TestContractRefusesAFabricatedVerification is the durable-record guard: even
// if some future code path tried, the record cannot say "verified" without the
// evidence.
func TestContractRefusesAFabricatedVerification(t *testing.T) {
	verifiedAt := at()
	for name, evidence := range map[string]protocol.AccelerationEvidence{
		"no signals at all": {
			Backend: protocol.BackendCUDA, State: protocol.StateVerified, VerifiedAt: &verifiedAt,
		},
		"only an indicative signal": {
			Backend: protocol.BackendCUDA, State: protocol.StateVerified, VerifiedAt: &verifiedAt,
			Signals: []protocol.AccelerationSignal{indicative(protocol.BackendCUDA)},
		},
		"authoritative signal for another backend": {
			Backend: protocol.BackendCUDA, State: protocol.StateVerified, VerifiedAt: &verifiedAt,
			Signals: []protocol.AccelerationSignal{authoritative(protocol.BackendMetal, true)},
		},
		"verified with a recorded conflict": {
			Backend: protocol.BackendCUDA, State: protocol.StateVerified, VerifiedAt: &verifiedAt,
			Signals:   []protocol.AccelerationSignal{authoritative(protocol.BackendCUDA, true)},
			Conflicts: []string{"telemetry disagreed"},
		},
		"verified with no instant": {
			Backend: protocol.BackendCUDA, State: protocol.StateVerified,
			Signals: []protocol.AccelerationSignal{authoritative(protocol.BackendCUDA, true)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := evidence.Validate(); err == nil {
				t.Error("the contract accepted a verification the evidence does not support")
			}
		})
	}
}

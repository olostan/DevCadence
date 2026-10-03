package protocol_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

func confidencePtr(c protocol.FindingConfidence) *protocol.FindingConfidence {
	return &c
}

func baselineChain() (*protocol.ReviewFinding, *protocol.FindingResolution, *protocol.ResolutionVerification) {
	f := &protocol.ReviewFinding{
		SchemaVersion:      protocol.SchemaVersion1,
		FindingID:          "rf-001",
		ProjectID:          "proj-1",
		CampaignID:         "cmp-1",
		CandidateCommit:    "cand-1",
		WorkPackageID:      "WP-M3C-5",
		ContractRevision:   1,
		Severity:           protocol.SeverityHigh,
		Materiality:        protocol.MaterialityBlocking,
		Confidence:         confidencePtr(protocol.ConfidenceHigh),
		Claim:              "memory leak in session substrate",
		Impact:             "crashes runner on long workloads",
		VerificationMethod: "run soak test",
		WhyNow:             stringPtr("blocks campaign"),
		EvidenceRefs:       []string{"ev-1"},
		RequirementRefs:    []string{"DCI-134"},
		SourceObservations: []protocol.ObservationRef{
			{ReviewID: "rev-1", FindingIndex: 0},
		},
		Reviewer: protocol.ActorProvenance{
			ActorID:         "act-reviewer-1",
			InvocationID:    "ivk-rev-1",
			Role:            protocol.ProvenanceRoleReviewer,
			LineageActorIDs: []string{"act-prior"},
		},
		RecordedAt: "2026-10-03T12:00:00Z",
	}

	r := &protocol.FindingResolution{
		SchemaVersion:           protocol.SchemaVersion1,
		ResolutionID:            "rsl-1",
		ProjectID:               "proj-1",
		CampaignID:              "cmp-1",
		FindingID:               "rf-001",
		DispositionID:           "disp-1",
		ContractRevision:        1,
		AttemptNo:               1,
		Kind:                    protocol.ResolutionKindFixAttempted,
		TargetCandidateCommit:   "cand-1",
		ResolvedCandidateCommit: stringPtr("cand-2"),
		Rationale:               "closed leaked connections in defer",
		EvidenceRefs:            []string{"ev-2"},
		Producer: protocol.ActorProvenance{
			ActorID:         "act-producer-1",
			InvocationID:    "ivk-prod-1",
			Role:            protocol.ProvenanceRoleImplementer,
			LineageActorIDs: []string{"act-upstream-author"},
		},
		RecordedAt: "2026-10-03T12:30:00Z",
	}

	v := &protocol.ResolutionVerification{
		SchemaVersion:           protocol.SchemaVersion1,
		VerificationID:          "rvf-1",
		ProjectID:               "proj-1",
		CampaignID:              "cmp-1",
		FindingID:               "rf-001",
		ResolutionID:            "rsl-1",
		ContractRevision:        1,
		VerifiedCandidateCommit: "cand-2",
		Outcome:                 protocol.OutcomeVerifiedFixed,
		Rationale:               "verified soak test passed with no leak",
		EvidenceRefs:            []string{"ev-3"},
		Verifier: protocol.ActorProvenance{
			ActorID:         "act-verifier-1",
			InvocationID:    "ivk-ver-1",
			Role:            protocol.ProvenanceRoleVerifier,
			LineageActorIDs: []string{"act-prior-verifier"},
		},
		RecordedAt: "2026-10-03T13:00:00Z",
	}

	return f, r, v
}

// TestReviewLedger_ACC03_SchemaEnumSets verifies enum alignment between review-finding and finding-disposition.
func TestReviewLedger_ACC03_SchemaEnumSets(t *testing.T) {
	findingRaw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "review-finding.schema.json"))
	if err != nil {
		t.Fatalf("read review-finding schema: %v", err)
	}
	dispRaw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "finding-disposition.schema.json"))
	if err != nil {
		t.Fatalf("read finding-disposition schema: %v", err)
	}

	var findingSchema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(findingRaw, &findingSchema); err != nil {
		t.Fatalf("unmarshal review-finding schema: %v", err)
	}

	var dispSchema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(dispRaw, &dispSchema); err != nil {
		t.Fatalf("unmarshal finding-disposition schema: %v", err)
	}

	findingSeverity := findingSchema.Properties["severity"].Enum
	dispSeverity := dispSchema.Properties["severity"].Enum
	sort.Strings(findingSeverity)
	sort.Strings(dispSeverity)
	if !reflect.DeepEqual(findingSeverity, dispSeverity) {
		t.Errorf("severity enums differ:\n review-finding: %v\n finding-disposition: %v", findingSeverity, dispSeverity)
	}

	findingMateriality := findingSchema.Properties["materiality"].Enum
	dispMateriality := dispSchema.Properties["materiality"].Enum
	sort.Strings(findingMateriality)
	sort.Strings(dispMateriality)
	if !reflect.DeepEqual(findingMateriality, dispMateriality) {
		t.Errorf("materiality enums differ:\n review-finding: %v\n finding-disposition: %v", findingMateriality, dispMateriality)
	}

	confidenceEnum := findingSchema.Properties["confidence"].Enum
	sort.Strings(confidenceEnum)
	expectedConfidence := []string{"high", "low", "medium"}
	if !reflect.DeepEqual(confidenceEnum, expectedConfidence) {
		t.Errorf("confidence enum = %v, want %v", confidenceEnum, expectedConfidence)
	}
}

// TestReviewLedger_ACC04_BaselineChain tests that the baseline chain passes CheckVerification.
func TestReviewLedger_ACC04_BaselineChain(t *testing.T) {
	f, r, v := baselineChain()
	if err := protocol.CheckVerification(f, r, v); err != nil {
		t.Fatalf("expected baseline chain to pass CheckVerification, got: %v", err)
	}
}

// TestReviewLedger_ACC05_Mutations runs all 21 mutations of ACC-05.
func TestReviewLedger_ACC05_Mutations(t *testing.T) {
	cases := []struct {
		id       string
		mutate   func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification)
		wantPass bool
	}{
		{
			id: "ACC-05_a_same_actor_id",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.Verifier.ActorID = r.Producer.ActorID
			},
			wantPass: false,
		},
		{
			id: "ACC-05_b_same_invocation_id",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.Verifier.InvocationID = r.Producer.InvocationID
			},
			wantPass: false,
		},
		{
			id: "ACC-05_c_producer_in_verifier_lineage",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.Verifier.LineageActorIDs = append(v.Verifier.LineageActorIDs, r.Producer.ActorID)
			},
			wantPass: false,
		},
		{
			id: "ACC-05_d_verifier_in_producer_lineage",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				r.Producer.LineageActorIDs = append(r.Producer.LineageActorIDs, v.Verifier.ActorID)
			},
			wantPass: false,
		},
		{
			id: "ACC-05_e_empty_verifier_actor_id",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.Verifier.ActorID = ""
			},
			wantPass: false,
		},
		{
			id: "ACC-05_f_empty_producer_invocation_id",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				r.Producer.InvocationID = ""
			},
			wantPass: false,
		},
		{
			id: "ACC-05_g_verifier_equals_original_reviewer_fix_attempted",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.Verifier.ActorID = f.Reviewer.ActorID
				v.Verifier.InvocationID = "ivk-ver-fresh"
				v.Verifier.LineageActorIDs = nil
			},
			wantPass: true,
		},
		{
			id: "ACC-05_h_verifier_equals_original_reviewer_challenge",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				r.Kind = protocol.ResolutionKindChallenge
				r.ResolvedCandidateCommit = nil
				v.VerifiedCandidateCommit = f.CandidateCommit
				v.Outcome = protocol.OutcomeVerifiedDismissed
				v.Verifier.ActorID = f.Reviewer.ActorID
				v.Verifier.InvocationID = "ivk-ver-fresh"
				v.Verifier.LineageActorIDs = nil
			},
			wantPass: false,
		},
		{
			id: "ACC-05_i_reviewer_in_verifier_lineage_challenge",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				r.Kind = protocol.ResolutionKindChallenge
				r.ResolvedCandidateCommit = nil
				v.VerifiedCandidateCommit = f.CandidateCommit
				v.Outcome = protocol.OutcomeVerifiedDismissed
				v.Verifier.LineageActorIDs = []string{f.Reviewer.ActorID}
			},
			wantPass: false,
		},
		{
			id: "ACC-05_j_same_informational_refs_pass",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				ep := "endpoint-shared"
				sess := "session-shared"
				mod := "model-shared"
				r.Producer.EndpointRef = &ep
				r.Producer.SessionRef = &sess
				r.Producer.ModelRef = &mod
				v.Verifier.EndpointRef = &ep
				v.Verifier.SessionRef = &sess
				v.Verifier.ModelRef = &mod
			},
			wantPass: true,
		},
		{
			id: "ACC-05_k_verification_project_differs",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.ProjectID = "proj-other"
			},
			wantPass: false,
		},
		{
			id: "ACC-05_l_verification_campaign_differs",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.CampaignID = "cmp-other"
			},
			wantPass: false,
		},
		{
			id: "ACC-05_m_verification_finding_differs",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.FindingID = "rf-other"
			},
			wantPass: false,
		},
		{
			id: "ACC-05_n_verification_resolution_differs",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.ResolutionID = "rsl-other"
			},
			wantPass: false,
		},
		{
			id: "ACC-05_o_verification_contract_revision_differs",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.ContractRevision = 99
			},
			wantPass: false,
		},
		{
			id: "ACC-05_p_fix_attempted_commit_mismatch",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.VerifiedCandidateCommit = "cand-mismatch"
			},
			wantPass: false,
		},
		{
			id: "ACC-05_q_challenge_commit_mismatch",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				r.Kind = protocol.ResolutionKindChallenge
				r.ResolvedCandidateCommit = nil
				v.Outcome = protocol.OutcomeVerifiedDismissed
				v.VerifiedCandidateCommit = "cand-mismatch"
			},
			wantPass: false,
		},
		{
			id: "ACC-05_r_fix_attempted_verified_dismissed",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.Outcome = protocol.OutcomeVerifiedDismissed
			},
			wantPass: false,
		},
		{
			id: "ACC-05_s_challenge_verified_fixed",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				r.Kind = protocol.ResolutionKindChallenge
				r.ResolvedCandidateCommit = nil
				v.VerifiedCandidateCommit = f.CandidateCommit
				v.Outcome = protocol.OutcomeVerifiedFixed
			},
			wantPass: false,
		},
		{
			id: "ACC-05_t_resolution_target_commit_differs",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				r.TargetCandidateCommit = "cand-differs"
			},
			wantPass: false,
		},
		{
			id: "ACC-05_u_verifier_role_implementer",
			mutate: func(f *protocol.ReviewFinding, r *protocol.FindingResolution, v *protocol.ResolutionVerification) {
				v.Verifier.Role = protocol.ProvenanceRoleImplementer
			},
			wantPass: false,
		},
	}

	if len(cases) != 21 {
		t.Fatalf("expected exactly 21 cases for ACC-05, got %d", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			f, r, v := baselineChain()
			tc.mutate(f, r, v)
			err := protocol.CheckVerification(f, r, v)
			if tc.wantPass {
				if err != nil {
					t.Fatalf("%s: expected pass, got %v", tc.id, err)
				}
			} else {
				if err == nil {
					t.Fatalf("%s: expected failure, got nil", tc.id)
				}
				if got := errs.CategoryOf(err); got != errs.CategoryValidationFailed {
					t.Fatalf("%s: error category = %s, want CategoryValidationFailed (%v)", tc.id, got, err)
				}
			}
		})
	}
}

// TestReviewLedger_ACC06_StateDerivations tests all 14 rows of ACC-06.
func TestReviewLedger_ACC06_StateDerivations(t *testing.T) {
	cases := []struct {
		id        string
		build     func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification)
		wantState protocol.FindingResolutionState
		wantErr   bool
	}{
		{
			id: "ACC-06_a_no_resolutions_no_verifications",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				return nil, nil
			},
			wantState: protocol.StateUnresolved,
			wantErr:   false,
		},
		{
			id: "ACC-06_b_one_fix_no_verification",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r, _ := baselineChain()
				return []protocol.FindingResolution{*r}, nil
			},
			wantState: protocol.StateVerificationPending,
			wantErr:   false,
		},
		{
			id: "ACC-06_c_fix_verified_fixed",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r, v := baselineChain()
				return []protocol.FindingResolution{*r}, []protocol.ResolutionVerification{*v}
			},
			wantState: protocol.StateVerifiedFixed,
			wantErr:   false,
		},
		{
			id: "ACC-06_d_fix_not_resolved",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r, v := baselineChain()
				v.Outcome = protocol.OutcomeNotResolved
				return []protocol.FindingResolution{*r}, []protocol.ResolutionVerification{*v}
			},
			wantState: protocol.StateUnresolved,
			wantErr:   false,
		},
		{
			id: "ACC-06_e_attempt1_not_resolved_attempt2_verified_fixed",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r1, v1 := baselineChain()
				v1.Outcome = protocol.OutcomeNotResolved

				_, r2, v2 := baselineChain()
				r2.ResolutionID = "rsl-2"
				r2.AttemptNo = 2
				r2.Producer.InvocationID = "ivk-prod-2"
				r2.ResolvedCandidateCommit = stringPtr("cand-3")

				v2.VerificationID = "rvf-2"
				v2.ResolutionID = "rsl-2"
				v2.VerifiedCandidateCommit = "cand-3"
				v2.Verifier.InvocationID = "ivk-ver-2"
				v2.Outcome = protocol.OutcomeVerifiedFixed

				return []protocol.FindingResolution{*r1, *r2}, []protocol.ResolutionVerification{*v1, *v2}
			},
			wantState: protocol.StateVerifiedFixed,
			wantErr:   false,
		},
		{
			id: "ACC-06_f_attempt1_no_verification_and_attempt2_present",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r1, _ := baselineChain()

				_, r2, v2 := baselineChain()
				r2.ResolutionID = "rsl-2"
				r2.AttemptNo = 2
				r2.Producer.InvocationID = "ivk-prod-2"
				r2.ResolvedCandidateCommit = stringPtr("cand-3")

				v2.VerificationID = "rvf-2"
				v2.ResolutionID = "rsl-2"
				v2.VerifiedCandidateCommit = "cand-3"
				v2.Verifier.InvocationID = "ivk-ver-2"
				v2.Outcome = protocol.OutcomeVerifiedFixed

				return []protocol.FindingResolution{*r1, *r2}, []protocol.ResolutionVerification{*v2}
			},
			wantState: "",
			wantErr:   true,
		},
		{
			id: "ACC-06_g_attempt_numbers_1_and_3",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r1, _ := baselineChain()
				_, r3, _ := baselineChain()
				r3.ResolutionID = "rsl-3"
				r3.AttemptNo = 3
				return []protocol.FindingResolution{*r1, *r3}, nil
			},
			wantState: "",
			wantErr:   true,
		},
		{
			id: "ACC-06_h_duplicate_attempt_no",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r1, _ := baselineChain()
				_, r2, _ := baselineChain()
				r2.ResolutionID = "rsl-2"
				r2.AttemptNo = 1
				return []protocol.FindingResolution{*r1, *r2}, nil
			},
			wantState: "",
			wantErr:   true,
		},
		{
			id: "ACC-06_i_two_verifications_for_one_resolution",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r, v1 := baselineChain()
				_, _, v2 := baselineChain()
				v2.VerificationID = "rvf-2"
				v2.Verifier.InvocationID = "ivk-ver-2"
				return []protocol.FindingResolution{*r}, []protocol.ResolutionVerification{*v1, *v2}
			},
			wantState: "",
			wantErr:   true,
		},
		{
			id: "ACC-06_j_challenge_verified_dismissed",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r, v := baselineChain()
				r.Kind = protocol.ResolutionKindChallenge
				r.ResolvedCandidateCommit = nil
				v.VerifiedCandidateCommit = f.CandidateCommit
				v.Outcome = protocol.OutcomeVerifiedDismissed
				return []protocol.FindingResolution{*r}, []protocol.ResolutionVerification{*v}
			},
			wantState: protocol.StateVerifiedDismissed,
			wantErr:   false,
		},
		{
			id: "ACC-06_k_challenge_not_resolved",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r, v := baselineChain()
				r.Kind = protocol.ResolutionKindChallenge
				r.ResolvedCandidateCommit = nil
				v.VerifiedCandidateCommit = f.CandidateCommit
				v.Outcome = protocol.OutcomeNotResolved
				return []protocol.FindingResolution{*r}, []protocol.ResolutionVerification{*v}
			},
			wantState: protocol.StateUnresolved,
			wantErr:   false,
		},
		{
			id: "ACC-06_l_fix_attempted_re_adjudication_required",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r, v := baselineChain()
				v.Outcome = protocol.OutcomeReAdjudicationRequired
				return []protocol.FindingResolution{*r}, []protocol.ResolutionVerification{*v}
			},
			wantState: protocol.StateReAdjudicationRequired,
			wantErr:   false,
		},
		{
			id: "ACC-06_m_verification_fails_check_verification",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r, v := baselineChain()
				v.Verifier.ActorID = r.Producer.ActorID // Fails independence
				return []protocol.FindingResolution{*r}, []protocol.ResolutionVerification{*v}
			},
			wantState: "",
			wantErr:   true,
		},
		{
			id: "ACC-06_n_verification_referencing_unknown_resolution",
			build: func(f *protocol.ReviewFinding) ([]protocol.FindingResolution, []protocol.ResolutionVerification) {
				_, r, v := baselineChain()
				v.ResolutionID = "rsl-unknown"
				return []protocol.FindingResolution{*r}, []protocol.ResolutionVerification{*v}
			},
			wantState: "",
			wantErr:   true,
		},
	}

	if len(cases) != 14 {
		t.Fatalf("expected exactly 14 cases for ACC-06, got %d", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			f, _, _ := baselineChain()
			rs, vs := tc.build(f)
			state, err := protocol.DeriveFindingResolutionState(f, rs, vs)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s: expected error, got state %q", tc.id, state)
				}
				if got := errs.CategoryOf(err); got != errs.CategoryValidationFailed {
					t.Fatalf("%s: error category = %s, want CategoryValidationFailed (%v)", tc.id, got, err)
				}
			} else {
				if err != nil {
					t.Fatalf("%s: unexpected error: %v", tc.id, err)
				}
				if state != tc.wantState {
					t.Fatalf("%s: state = %q, want %q", tc.id, state, tc.wantState)
				}
			}
		})
	}
}

// TestReviewLedger_ACC07_CleanSessionReconstruction tests that state derivation
// needs only decoded durable records without shared pointers or transcript input.
func TestReviewLedger_ACC07_CleanSessionReconstruction(t *testing.T) {
	fOrig, rOrig, vOrig := baselineChain()

	// Direct derivation in memory
	directState, err := protocol.DeriveFindingResolutionState(fOrig, []protocol.FindingResolution{*rOrig}, []protocol.ResolutionVerification{*vOrig})
	if err != nil {
		t.Fatalf("direct derive: %v", err)
	}

	// Marshal each record to JSON bytes
	fBytes, err := protocol.Marshal(fOrig)
	if err != nil {
		t.Fatalf("marshal finding: %v", err)
	}
	rBytes, err := protocol.Marshal(rOrig)
	if err != nil {
		t.Fatalf("marshal resolution: %v", err)
	}
	vBytes, err := protocol.Marshal(vOrig)
	if err != nil {
		t.Fatalf("marshal verification: %v", err)
	}

	// Decode into brand new values (clean session)
	var fDecoded protocol.ReviewFinding
	if err := protocol.Unmarshal(fBytes, &fDecoded); err != nil {
		t.Fatalf("unmarshal finding: %v", err)
	}
	var rDecoded protocol.FindingResolution
	if err := protocol.Unmarshal(rBytes, &rDecoded); err != nil {
		t.Fatalf("unmarshal resolution: %v", err)
	}
	var vDecoded protocol.ResolutionVerification
	if err := protocol.Unmarshal(vBytes, &vDecoded); err != nil {
		t.Fatalf("unmarshal verification: %v", err)
	}

	cleanState, err := protocol.DeriveFindingResolutionState(&fDecoded, []protocol.FindingResolution{rDecoded}, []protocol.ResolutionVerification{vDecoded})
	if err != nil {
		t.Fatalf("clean session derive: %v", err)
	}

	if cleanState != directState {
		t.Fatalf("clean session state %q != in-memory state %q", cleanState, directState)
	}
	if cleanState != protocol.StateVerifiedFixed {
		t.Fatalf("clean session state = %q, want %q", cleanState, protocol.StateVerifiedFixed)
	}
}

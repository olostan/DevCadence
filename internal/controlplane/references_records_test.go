package controlplane_test

import (
	"context"
	"testing"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/testsupport"
)

// The rule under test: an event claiming a durable record is refused unless
// that record exists — already stored, or written in the same command — with
// the digest and the semantic identity the event asserts.
//
// Reducer lineage proves journal-internal consistency. Reference validation
// proves the referenced immutable evidence is really there. Neither implies
// the other, so both are tested, here and in lineage_test.go.

// driveToValidating takes a task to VALIDATING, the state an attempt
// validation acts on, with no validation recorded yet.
func driveToValidating(t *testing.T, h *testsupport.Harness) string {
	t.Helper()
	ctx := context.Background()
	taskID := designedTask(t, h)
	workPackage := testsupport.WorkPackage("example", taskID, "wp_0001", 1)
	for _, step := range []struct {
		payload events.Payload
		records []controlplane.RecordToStore
	}{
		{
			payload: approval(taskID, testsupport.Digest(t, workPackage), 1),
			records: []controlplane.RecordToStore{{Version: 1, Record: workPackage}},
		},
		{payload: &events.TaskDelegated{
			TaskID: taskID, WorkPackageID: "wp_0001", WorkerRole: "implementer",
		}},
		{payload: &events.AttemptStarted{
			TaskID: taskID, AttemptID: "att_0001", WorkPackageID: "wp_0001", WorkPackageVersion: 1,
			ProjectStateRevision: "ps_000000003", WorkerRole: "implementer",
		}},
		{payload: &events.CandidateProduced{
			TaskID: taskID, AttemptID: "att_0001",
			CandidateCommit: "cafebabe1234567", Summary: "done",
		}},
	} {
		if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
			ProjectID: "example", Payload: step.payload, Records: step.records,
		}); err != nil {
			t.Fatalf("append %s: %v", step.payload.Type(), err)
		}
	}
	return taskID
}

// designedTask drives a task to DESIGNING, the state work-package approval
// acts on, and returns its id.
func designedTask(t *testing.T, h *testsupport.Harness) string {
	t.Helper()
	ctx := context.Background()
	initProject(t, h)
	if _, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "Bounded reads",
		ChangeClass: protocol.ChangeSystemic,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID := mustTaskID(t, h, "DC-001")
	if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: "example",
		Payload:   &events.TaskDesignStarted{TaskID: taskID, Reason: "initial design"},
	}); err != nil {
		t.Fatalf("start design: %v", err)
	}
	return taskID
}

func approval(taskID, digest string, version int) *events.WorkPackageApproved {
	return &events.WorkPackageApproved{
		TaskID: taskID, WorkPackageID: "wp_0001", WorkPackageVersion: version,
		RecordDigest: digest, ProjectStateRevision: "ps_000000003",
		BaseCommit: "91acd8273f1", ChangeClass: protocol.ChangeSystemic,
	}
}

func refusesWith(t *testing.T, h *testsupport.Harness, in controlplane.AppendTypedEventInput, what string) {
	t.Helper()
	_, err := h.Service.AppendTypedEvent(context.Background(), in)
	if err == nil {
		t.Fatalf("%s was accepted", what)
	}
	if got := errs.CategoryOf(err); got != errs.CategoryIntegrity {
		t.Fatalf("category = %s, want integrity (%v)", got, err)
	}
}

func TestWorkPackageApprovalRequiresItsRecord(t *testing.T) {
	h := testsupport.NewHarness(t)
	taskID := designedTask(t, h)
	workPackage := testsupport.WorkPackage("example", taskID, "wp_0001", 1)

	t.Run("nonexistent record", func(t *testing.T) {
		refusesWith(t, h, controlplane.AppendTypedEventInput{
			ProjectID: "example",
			Payload:   approval(taskID, testsupport.Digest(t, workPackage), 1),
		}, "an approval naming a work package that was never stored")
	})

	t.Run("wrong digest", func(t *testing.T) {
		refusesWith(t, h, controlplane.AppendTypedEventInput{
			ProjectID: "example",
			Payload:   approval(taskID, "sha256:"+zeros(64), 1),
			Records:   []controlplane.RecordToStore{{Version: 1, Record: workPackage}},
		}, "an approval whose digest does not match the stored work package")
	})

	t.Run("wrong version", func(t *testing.T) {
		// The record is stored at version 1; the event claims version 2.
		refusesWith(t, h, controlplane.AppendTypedEventInput{
			ProjectID: "example",
			Payload:   approval(taskID, testsupport.Digest(t, workPackage), 2),
			Records:   []controlplane.RecordToStore{{Version: 1, Record: workPackage}},
		}, "an approval naming a version the store does not have")
	})
}

// TestWorkPackageSuppliedInTheSameCommandIsAccepted is the control, and the
// shape a principal authoring a blueprint actually uses.
func TestWorkPackageSuppliedInTheSameCommandIsAccepted(t *testing.T) {
	h := testsupport.NewHarness(t)
	taskID := designedTask(t, h)
	workPackage := testsupport.WorkPackage("example", taskID, "wp_0001", 1)

	if _, err := h.Service.AppendTypedEvent(context.Background(), controlplane.AppendTypedEventInput{
		ProjectID: "example",
		Payload:   approval(taskID, testsupport.Digest(t, workPackage), 1),
		Records:   []controlplane.RecordToStore{{Version: 1, Record: workPackage}},
	}); err != nil {
		t.Fatalf("an approval carrying its own work package was refused: %v", err)
	}
}

// TestPreviouslyStoredWorkPackageIsAccepted covers the other supported path:
// the record was written earlier and the event references it.
func TestPreviouslyStoredWorkPackageIsAccepted(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	taskID := designedTask(t, h)
	workPackage := testsupport.WorkPackage("example", taskID, "wp_0001", 1)

	// Store the record on an unrelated event, then approve without records.
	if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: "example",
		Payload:   &events.RiskRecorded{RiskID: "R-001", Severity: protocol.SeverityLow, Statement: "noted"},
		Records:   []controlplane.RecordToStore{{Version: 1, Record: workPackage}},
	}); err != nil {
		t.Fatalf("store work package: %v", err)
	}
	if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: "example",
		Payload:   approval(taskID, testsupport.Digest(t, workPackage), 1),
	}); err != nil {
		t.Fatalf("an approval referencing an already-stored work package was refused: %v", err)
	}
}

func TestValidationEvidenceMustExistAndAgree(t *testing.T) {
	const (
		attemptID = "att_0001"
		candidate = "cafebabe1234567"
	)
	for _, tc := range []struct {
		name  string
		what  string
		build func(t *testing.T, taskID string) (events.Payload, []controlplane.RecordToStore)
	}{
		{
			name: "nonexistent result",
			what: "a validation naming a result that was never stored",
			build: func(t *testing.T, taskID string) (events.Payload, []controlplane.RecordToStore) {
				result := testsupport.AttemptValidation("example", "val_0001", taskID, attemptID,
					candidate, protocol.ValidationPass)
				return validationEvent(taskID, attemptID, candidate,
					testsupport.Digest(t, result), protocol.ValidationPass), nil
			},
		},
		{
			name: "wrong digest",
			what: "a validation whose digest does not match its result",
			build: func(t *testing.T, taskID string) (events.Payload, []controlplane.RecordToStore) {
				result := testsupport.AttemptValidation("example", "val_0001", taskID, attemptID,
					candidate, protocol.ValidationPass)
				return validationEvent(taskID, attemptID, candidate, "sha256:"+zeros(64),
						protocol.ValidationPass),
					[]controlplane.RecordToStore{{Version: 1, Record: result}}
			},
		},
		{
			name: "identifier mismatch",
			what: "a validation whose result records a different validation id",
			build: func(t *testing.T, taskID string) (events.Payload, []controlplane.RecordToStore) {
				result := testsupport.AttemptValidation("example", "val_0002", taskID, attemptID,
					candidate, protocol.ValidationPass)
				event := validationEvent(taskID, attemptID, candidate,
					testsupport.Digest(t, result), protocol.ValidationPass)
				return event, []controlplane.RecordToStore{{Version: 1, Record: result}}
			},
		},
		{
			name: "status mismatch",
			what: "a passing validation summarising a failing result",
			build: func(t *testing.T, taskID string) (events.Payload, []controlplane.RecordToStore) {
				result := testsupport.AttemptValidation("example", "val_0001", taskID, attemptID,
					candidate, protocol.ValidationFail)
				event := validationEvent(taskID, attemptID, candidate,
					testsupport.Digest(t, result), protocol.ValidationPass)
				return event, []controlplane.RecordToStore{{Version: 1, Record: result}}
			},
		},
		{
			name: "commit mismatch",
			what: "a validation whose result names a different commit",
			build: func(t *testing.T, taskID string) (events.Payload, []controlplane.RecordToStore) {
				result := testsupport.AttemptValidation("example", "val_0001", taskID, attemptID,
					"0000000deadbee", protocol.ValidationPass)
				event := validationEvent(taskID, attemptID, candidate,
					testsupport.Digest(t, result), protocol.ValidationPass)
				return event, []controlplane.RecordToStore{{Version: 1, Record: result}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.NewHarness(t)
			taskID := driveToValidating(t, h)
			payload, records := tc.build(t, taskID)
			refusesWith(t, h, controlplane.AppendTypedEventInput{
				ProjectID: "example", Payload: payload, Records: records,
			}, tc.what)
		})
	}
}

func validationEvent(taskID, attemptID, commit, digest string,
	status protocol.ValidationOutcome) *events.ValidationCompleted {
	return &events.ValidationCompleted{
		TaskID: taskID, AttemptID: attemptID, ValidationID: "val_0001",
		Scope: events.ScopeAttempt, Status: status, Commit: commit, RecordDigest: digest,
	}
}

func TestReviewEvidenceMustExistAndAgree(t *testing.T) {
	const attemptID = "att_0001"
	for _, tc := range []struct {
		name  string
		what  string
		build func(t *testing.T) (events.Payload, []controlplane.RecordToStore)
	}{
		{
			name: "nonexistent result",
			what: "a review naming a result that was never stored",
			build: func(t *testing.T) (events.Payload, []controlplane.RecordToStore) {
				result := testsupport.Review("example", "rev_0001", attemptID, "wp_0001",
					protocol.DimensionCorrectness, protocol.VerdictPass)
				return reviewEvent(attemptID, testsupport.Digest(t, result),
					protocol.DimensionCorrectness, protocol.VerdictPass), nil
			},
		},
		{
			name: "wrong digest",
			what: "a review whose digest does not match its result",
			build: func(t *testing.T) (events.Payload, []controlplane.RecordToStore) {
				result := testsupport.Review("example", "rev_0001", attemptID, "wp_0001",
					protocol.DimensionCorrectness, protocol.VerdictPass)
				return reviewEvent(attemptID, "sha256:"+zeros(64),
						protocol.DimensionCorrectness, protocol.VerdictPass),
					[]controlplane.RecordToStore{{Version: 1, Record: result}}
			},
		},
		{
			name: "wrong attempt",
			what: "a review whose result is about a different attempt",
			build: func(t *testing.T) (events.Payload, []controlplane.RecordToStore) {
				result := testsupport.Review("example", "rev_0001", "att_9999", "wp_0001",
					protocol.DimensionCorrectness, protocol.VerdictPass)
				return reviewEvent(attemptID, testsupport.Digest(t, result),
						protocol.DimensionCorrectness, protocol.VerdictPass),
					[]controlplane.RecordToStore{{Version: 1, Record: result}}
			},
		},
		{
			name: "dimension mismatch",
			what: "a correctness review summarising a security review",
			build: func(t *testing.T) (events.Payload, []controlplane.RecordToStore) {
				result := testsupport.Review("example", "rev_0001", attemptID, "wp_0001",
					protocol.DimensionSecurity, protocol.VerdictPass)
				return reviewEvent(attemptID, testsupport.Digest(t, result),
						protocol.DimensionCorrectness, protocol.VerdictPass),
					[]controlplane.RecordToStore{{Version: 1, Record: result}}
			},
		},
		{
			name: "verdict mismatch",
			what: "a passing review summarising a failing result",
			build: func(t *testing.T) (events.Payload, []controlplane.RecordToStore) {
				result := testsupport.Review("example", "rev_0001", attemptID, "wp_0001",
					protocol.DimensionCorrectness, protocol.VerdictFail)
				return reviewEvent(attemptID, testsupport.Digest(t, result),
						protocol.DimensionCorrectness, protocol.VerdictPass),
					[]controlplane.RecordToStore{{Version: 1, Record: result}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.NewHarness(t)
			taskID := driveToReviewing(t, h)
			payload, records := tc.build(t)
			review := payload.(*events.ReviewCompleted)
			review.TaskID = taskID
			review.ReviewID = "rev_0002"
			for _, record := range records {
				if result, ok := record.Record.(*protocol.ReviewResult); ok {
					result.ReviewID = "rev_0002"
				}
			}
			// Recompute the digest for the cases that supply a real one.
			if review.RecordDigest != "sha256:"+zeros(64) && len(records) > 0 {
				review.RecordDigest = testsupport.Digest(t, records[0].Record)
			}
			refusesWith(t, h, controlplane.AppendTypedEventInput{
				ProjectID: "example", Payload: review, Records: records,
			}, tc.what)
		})
	}
}

func reviewEvent(attemptID, digest string, dimension protocol.ReviewDimension,
	verdict protocol.ReviewVerdict) *events.ReviewCompleted {
	return &events.ReviewCompleted{
		AttemptID: attemptID, ReviewID: "rev_0001", WorkPackageID: "wp_0001",
		Dimension: dimension, Verdict: verdict, RecordDigest: digest,
	}
}

// TestFailedReferenceValidationLeavesNothingBehind is the atomicity claim:
// a refused reference rolls back the record write, the journal append and the
// projection together.
func TestFailedReferenceValidationLeavesNothingBehind(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	taskID := designedTask(t, h)
	workPackage := testsupport.WorkPackage("example", taskID, "wp_0001", 1)

	before := snapshot(t, h)

	_, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: "example",
		// The record is offered, but the event claims a digest that is not
		// the one the store will compute for it.
		Payload: approval(taskID, "sha256:"+zeros(64), 1),
		Records: []controlplane.RecordToStore{{Version: 1, Record: workPackage}},
	})
	if err == nil {
		t.Fatal("an approval with a contradicting digest was committed")
	}

	if after := snapshot(t, h); after != before {
		t.Fatalf("a refused reference changed durable state: before %+v, after %+v", before, after)
	}
	// The offered record must not survive the rollback either.
	if _, err := h.Service.Record(ctx, "example", "EngineeringWorkPackage", "wp_0001", 1); err == nil {
		t.Fatal("the work package record survived the refused approval")
	}
}

// TestBaselineValidationEvidenceIsAccepted exercises the third scope, which
// touches no task and is the reason ValidationResult carries a subject rather
// than a bare attempt id.
func TestBaselineValidationEvidenceIsAccepted(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	initProject(t, h)

	result := testsupport.BaselineValidation("example", "val_base", "91acd8273f1", protocol.ValidationPass)
	if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: "example",
		Payload: &events.ValidationCompleted{
			ValidationID: "val_base", Scope: events.ScopeBaseline,
			Status: protocol.ValidationPass, Commit: "91acd8273f1",
			RecordDigest: testsupport.Digest(t, result),
		},
		Records: []controlplane.RecordToStore{{Version: 1, Record: result}},
	}); err != nil {
		t.Fatalf("baseline validation evidence was refused: %v", err)
	}
}

func zeros(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = '0'
	}
	return string(out)
}

var _ = storage.EventQuery{}

// TestReviewIsTiedToTheWorkPackageTheAttemptExecuted proves the three-way
// equality that makes a review evidence about *this* work:
//
//	ReviewCompleted.work_package_id == ReviewResult.work_package_id
//	                                == Attempt.work_package_id
//
// The first equality is the control plane's (the event and its record must
// agree); the second is the reducer's (the record must be about the blueprint
// the attempt actually ran against). Before this, a review whose durable
// record cited a different blueprint satisfied every check: the digest
// matched, and no repeated field mentioned the work package at all.
func TestReviewIsTiedToTheWorkPackageTheAttemptExecuted(t *testing.T) {
	const attemptID = "att_0001"

	t.Run("record names a different work package", func(t *testing.T) {
		h := testsupport.NewHarness(t)
		taskID := driveToReviewing(t, h)
		// The record says wp_elsewhere; the event says wp_0001, which is
		// what the attempt ran against. The digest is honest either way.
		result := testsupport.Review("example", "rev_0002", attemptID, "wp_elsewhere",
			protocol.DimensionCorrectness, protocol.VerdictPass)
		refusesWith(t, h, controlplane.AppendTypedEventInput{
			ProjectID: "example",
			Payload: &events.ReviewCompleted{
				TaskID: taskID, AttemptID: attemptID, ReviewID: "rev_0002",
				WorkPackageID: "wp_0001",
				Dimension:     protocol.DimensionCorrectness, Verdict: protocol.VerdictPass,
				RecordDigest: testsupport.Digest(t, result),
			},
			Records: []controlplane.RecordToStore{{Version: 1, Record: result}},
		}, "a review whose durable record judged a different blueprint")
	})

	t.Run("event names a work package the attempt never ran against", func(t *testing.T) {
		h := testsupport.NewHarness(t)
		taskID := driveToReviewing(t, h)
		// Here event and record agree with each other — and both are wrong
		// about the attempt, which the reducer is the only thing that knows.
		result := testsupport.Review("example", "rev_0002", attemptID, "wp_elsewhere",
			protocol.DimensionCorrectness, protocol.VerdictPass)
		refusesWith(t, h, controlplane.AppendTypedEventInput{
			ProjectID: "example",
			Payload: &events.ReviewCompleted{
				TaskID: taskID, AttemptID: attemptID, ReviewID: "rev_0002",
				WorkPackageID: "wp_elsewhere",
				Dimension:     protocol.DimensionCorrectness, Verdict: protocol.VerdictPass,
				RecordDigest: testsupport.Digest(t, result),
			},
			Records: []controlplane.RecordToStore{{Version: 1, Record: result}},
		}, "a review judging a blueprint the attempt never executed")
	})

	t.Run("a correct review is accepted", func(t *testing.T) {
		h := testsupport.NewHarness(t)
		taskID := driveToReviewing(t, h)
		result := testsupport.Review("example", "rev_0002", attemptID, "wp_0001",
			protocol.DimensionArchitecture, protocol.VerdictPass)
		if _, err := h.Service.AppendTypedEvent(context.Background(),
			controlplane.AppendTypedEventInput{
				ProjectID: "example",
				Payload: &events.ReviewCompleted{
					TaskID: taskID, AttemptID: attemptID, ReviewID: "rev_0002",
					WorkPackageID: "wp_0001",
					Dimension:     protocol.DimensionArchitecture, Verdict: protocol.VerdictPass,
					RecordDigest: testsupport.Digest(t, result),
				},
				Records: []controlplane.RecordToStore{{Version: 1, Record: result}},
			}); err != nil {
			t.Fatalf("a well-formed second review dimension was refused: %v", err)
		}
	})

	t.Run("a refused review leaves nothing behind", func(t *testing.T) {
		ctx := context.Background()
		h := testsupport.NewHarness(t)
		taskID := driveToReviewing(t, h)
		before := snapshot(t, h)

		result := testsupport.Review("example", "rev_0002", attemptID, "wp_elsewhere",
			protocol.DimensionCorrectness, protocol.VerdictPass)
		if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
			ProjectID: "example",
			Payload: &events.ReviewCompleted{
				TaskID: taskID, AttemptID: attemptID, ReviewID: "rev_0002",
				WorkPackageID: "wp_elsewhere",
				Dimension:     protocol.DimensionCorrectness, Verdict: protocol.VerdictPass,
				RecordDigest: testsupport.Digest(t, result),
			},
			Records: []controlplane.RecordToStore{{Version: 1, Record: result}},
		}); err == nil {
			t.Fatal("a review with a broken work-package link was committed")
		}
		if after := snapshot(t, h); after != before {
			t.Fatalf("a refused review changed durable state: before %+v, after %+v", before, after)
		}
		if _, err := h.Service.Record(ctx, "example", "ReviewResult", "rev_0002", 1); err == nil {
			t.Fatal("the review record survived the refused event")
		}
	})
}

// specReview builds a coherent specification-review record.
func specReview(reviewID, profile, summary string, gaps []string) *protocol.SpecificationReviewResult {
	return &protocol.SpecificationReviewResult{
		SchemaVersion:        protocol.SchemaVersion1,
		ReviewID:             reviewID,
		ProjectID:            "example",
		ProblemModelID:       "pm_0001",
		ProblemModelRevision: 1,
		Dimension:            protocol.SpecAmbiguity,
		ReviewerProfile:      profile,
		Verdict:              protocol.VerdictConcern,
		Findings: []protocol.SpecificationFinding{{
			Severity:            protocol.SeverityHigh,
			Statement:           `"offline" is undefined for first-time setup.`,
			WhyItMatters:        "It binds the distribution model.",
			ResolutionAuthority: protocol.ResolveByHuman,
		}},
		MaterialGapRefs: gaps,
		Summary:         summary,
	}
}

// TestSpecificationReviewReferencesItsOwnRecordKind is the point of the
// dedicated record: a specification review is evidence about intent, produced
// before any Work Package or Attempt exists, so it cannot be an
// implementation ReviewResult. That mismatch used to be hidden by the
// optional digest; now the claim is typed and checked.
func TestSpecificationReviewReferencesItsOwnRecordKind(t *testing.T) {
	ctx := context.Background()

	t.Run("a coherent specification review is accepted", func(t *testing.T) {
		h := testsupport.NewHarness(t)
		initProject(t, h)
		record := specReview("sr_0001", "local-strong-reviewer", "One ambiguity remains.", []string{"AQ-027"})
		if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
			ProjectID: "example",
			Payload: &events.SpecificationReviewCompleted{
				ReviewID: "sr_0001", ReviewerProfile: "local-strong-reviewer",
				MaterialGapsFound: []string{"AQ-027"}, Summary: "One ambiguity remains.",
				RecordDigest: testsupport.Digest(t, record),
			},
			Records: []controlplane.RecordToStore{{Version: 1, Record: record}},
		}); err != nil {
			t.Fatalf("a coherent specification review was refused: %v", err)
		}
	})

	t.Run("an implementation ReviewResult cannot stand in", func(t *testing.T) {
		h := testsupport.NewHarness(t)
		initProject(t, h)
		// A ReviewResult stored under the same id: the digest is real, but
		// the document is the wrong kind of evidence entirely.
		wrongKind := testsupport.Review("example", "sr_0002", "att_0001", "wp_0001",
			protocol.DimensionCorrectness, protocol.VerdictPass)
		refusesWith(t, h, controlplane.AppendTypedEventInput{
			ProjectID: "example",
			Payload: &events.SpecificationReviewCompleted{
				ReviewID: "sr_0002", ReviewerProfile: "local-strong-reviewer",
				Summary: "Nothing to report.", RecordDigest: testsupport.Digest(t, wrongKind),
			},
			Records: []controlplane.RecordToStore{{Version: 1, Record: wrongKind}},
		}, "a specification review backed by an implementation ReviewResult")
	})

	t.Run("gaps the record found must appear in the journal", func(t *testing.T) {
		h := testsupport.NewHarness(t)
		initProject(t, h)
		// The record opened AQ-027; the compact event reports none, which
		// would leave the question invisible to a journal-only reader.
		record := specReview("sr_0003", "local-strong-reviewer", "One ambiguity remains.", []string{"AQ-027"})
		refusesWith(t, h, controlplane.AppendTypedEventInput{
			ProjectID: "example",
			Payload: &events.SpecificationReviewCompleted{
				ReviewID: "sr_0003", ReviewerProfile: "local-strong-reviewer",
				Summary: "One ambiguity remains.", RecordDigest: testsupport.Digest(t, record),
			},
			Records: []controlplane.RecordToStore{{Version: 1, Record: record}},
		}, "a specification review omitting a gap its record found")
	})

	t.Run("an absent digest still asserts nothing", func(t *testing.T) {
		h := testsupport.NewHarness(t)
		initProject(t, h)
		if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
			ProjectID: "example",
			Payload: &events.SpecificationReviewCompleted{
				ReviewID: "sr_0004", ReviewerProfile: "local-strong-reviewer",
				Summary: "Reviewed during discovery; full document not yet written.",
			},
		}); err != nil {
			t.Fatalf("a specification review claiming no record was refused: %v", err)
		}
	})
}

// TestRepeatedMetadataMustAgreeWithTheRecord extends the cross-check to every
// fact the compact event repeats, not only the identifying ones.
//
// The approval event repeats the blueprint's baseline metadata, and that
// metadata is what makes a stale plan detectable (docs/PROJECT_STATE.md §7).
// A digest-valid work package stored against one baseline while the journal
// records another would make staleness undetectable in the case it exists to
// catch — and every identifying field would still agree.
func TestRepeatedMetadataMustAgreeWithTheRecord(t *testing.T) {
	for _, tc := range []struct {
		name   string
		break_ func(*events.WorkPackageApproved)
		what   string
	}{
		{
			name:   "project state revision",
			break_: func(a *events.WorkPackageApproved) { a.ProjectStateRevision = "ps_999999999" },
			what:   "an approval recording a different state revision than its blueprint",
		},
		{
			name:   "base commit",
			break_: func(a *events.WorkPackageApproved) { a.BaseCommit = "0000000deadbee" },
			what:   "an approval recording a different base commit than its blueprint",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.NewHarness(t)
			taskID := designedTask(t, h)
			workPackage := testsupport.WorkPackage("example", taskID, "wp_0001", 1)
			payload := approval(taskID, testsupport.Digest(t, workPackage), 1)
			tc.break_(payload)
			refusesWith(t, h, controlplane.AppendTypedEventInput{
				ProjectID: "example",
				Payload:   payload,
				Records:   []controlplane.RecordToStore{{Version: 1, Record: workPackage}},
			}, tc.what)
		})
	}
}

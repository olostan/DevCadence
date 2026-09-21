package controlplane

import (
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
)

// canonicalCorrelation derives an event's correlation identifiers from its
// typed payload and merges anything the caller supplied that the payload
// cannot express.
//
// Deriving centrally rather than trusting the caller matters because the
// journal indexes correlation columns: `task show`, escalation review and
// every future metric read them. An adapter that left them empty would append
// a state change that is invisible in its own task's history, and one that
// set them wrongly would attribute a change to the wrong task. Neither is
// something an adapter should be able to do, and docs/ARCHITECTURE.md §3
// keeps that judgement out of adapters anyway.
//
// A caller-supplied value is accepted only where the payload says nothing. A
// value that contradicts the payload is refused rather than silently
// overridden, because a contradiction means the caller believes something
// different from what it is recording, and picking a winner would hide that.
func canonicalCorrelation(payload events.Payload, supplied events.Correlation) (events.Correlation, error) {
	derived := events.CorrelationFor(payload)

	fields := []struct {
		name     string
		derived  *string
		supplied string
	}{
		{"milestone_id", &derived.MilestoneID, supplied.MilestoneID},
		{"task_id", &derived.TaskID, supplied.TaskID},
		{"work_package_id", &derived.WorkPackageID, supplied.WorkPackageID},
		{"attempt_id", &derived.AttemptID, supplied.AttemptID},
		{"agent_run_id", &derived.AgentRunID, supplied.AgentRunID},
		{"evidence_packet_id", &derived.EvidencePacketID, supplied.EvidencePacketID},
		{"validation_id", &derived.ValidationID, supplied.ValidationID},
		{"review_id", &derived.ReviewID, supplied.ReviewID},
		{"consultation_id", &derived.ConsultationID, supplied.ConsultationID},
		{"decision_id", &derived.DecisionID, supplied.DecisionID},
	}
	for _, field := range fields {
		switch {
		case field.supplied == "":
			// Nothing supplied: the derived value stands, including empty.
		case *field.derived == "":
			// The payload cannot express this identifier, so the caller's
			// value is the only source. agent_run_id and consultation_id are
			// the cases that exist today.
			*field.derived = field.supplied
		case *field.derived != field.supplied:
			return events.Correlation{}, errs.New(errs.CategoryInvalidArgument,
				"event correlation %s is %q in the payload but %q was supplied alongside it; "+
					"a correlation that contradicts its own event cannot be recorded",
				field.name, *field.derived, field.supplied)
		}
	}
	return derived, nil
}

package events

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"sync"

	"github.com/olostan/DevCadience/internal/errs"
)

// registry maps an event type to a factory producing a zero payload of the
// matching concrete type.
//
// A registry rather than a type switch keeps the journal open to the event
// types later milestones add (learning, health, consultants) without any
// package having to enumerate them, while still refusing anything unknown.
var registry = struct {
	mu    sync.RWMutex
	items map[Type]func() Payload
}{items: make(map[Type]func() Payload)}

// Register makes an event type appendable and decodable. It is called from
// package initialisers; registering the same type twice is a programming
// error and panics, because a duplicate registration means two payload shapes
// claim the same durable name.
func Register(t Type, factory func() Payload) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.items[t]; exists {
		panic("events: duplicate registration for event type " + string(t))
	}
	registry.items[t] = factory
}

// Registered reports whether the event type is known to this build.
func Registered(t Type) bool {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	_, ok := registry.items[t]
	return ok
}

// RegisteredTypes returns every known event type in sorted order.
func RegisteredTypes() []Type {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	out := make([]Type, 0, len(registry.items))
	for t := range registry.items {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// DecodePayload strictly decodes raw into the payload type registered for t.
//
// An unregistered type yields CategorySchemaVersionUnsupported rather than a
// generic parse error: encountering an event this build cannot interpret is a
// compatibility condition the operator must see, not a record to skip
// (DCI-092, DCI-093).
func DecodePayload(t Type, raw json.RawMessage) (Payload, error) {
	registry.mu.RLock()
	factory, ok := registry.items[t]
	registry.mu.RUnlock()
	if !ok {
		return nil, errs.New(errs.CategorySchemaVersionUnsupported,
			"event type %q is not known to this build; refusing to interpret it", string(t))
	}
	payload := factory()
	if len(raw) == 0 {
		return nil, errs.New(errs.CategoryInvalidArgument, "event type %s: payload is required", t)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(payload); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "decode payload for event type %s", t)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errs.New(errs.CategoryInvalidArgument, "decode payload for event type %s: trailing content", t)
	}
	return payload, nil
}

// CorrelationFor derives the correlation identifiers an event carries from
// its payload.
//
// It lives here rather than in the CLI so that every producer of an event
// correlates it the same way; docs/OBSERVABILITY.md §1 is only useful if the
// hierarchy is populated consistently.
func CorrelationFor(payload Payload) Correlation {
	switch p := payload.(type) {
	case *ProjectInitialized:
		return Correlation{MilestoneID: p.MilestoneID}
	case *MilestoneStarted:
		return Correlation{MilestoneID: p.MilestoneID}
	case *DecisionRecorded:
		return Correlation{DecisionID: p.DecisionID}
	case *TaskCreated:
		return Correlation{TaskID: p.TaskID, MilestoneID: p.MilestoneID}
	case *TaskScoutingStarted:
		return Correlation{TaskID: p.TaskID}
	case *TaskDesignStarted:
		return Correlation{TaskID: p.TaskID}
	case *WorkPackageApproved:
		return Correlation{TaskID: p.TaskID, WorkPackageID: p.WorkPackageID}
	case *TaskDelegated:
		return Correlation{TaskID: p.TaskID, WorkPackageID: p.WorkPackageID}
	case *AttemptStarted:
		return Correlation{TaskID: p.TaskID, AttemptID: p.AttemptID, WorkPackageID: p.WorkPackageID}
	case *CandidateProduced:
		return Correlation{TaskID: p.TaskID, AttemptID: p.AttemptID}
	case *AttemptBlocked:
		return Correlation{TaskID: p.TaskID, AttemptID: p.AttemptID}
	case *AttemptFailed:
		return Correlation{TaskID: p.TaskID, AttemptID: p.AttemptID}
	case *ValidationCompleted:
		return Correlation{TaskID: p.TaskID, AttemptID: p.AttemptID, ValidationID: p.ValidationID}
	case *ReviewCompleted:
		// The work package belongs here for the same reason the event now
		// carries it: a review is defined against the blueprint the attempt
		// executed, so review-level observability must be able to join the
		// two without reopening the record.
		return Correlation{
			TaskID: p.TaskID, AttemptID: p.AttemptID,
			WorkPackageID: p.WorkPackageID, ReviewID: p.ReviewID,
		}
	case *ChangeAccepted:
		return Correlation{TaskID: p.TaskID, AttemptID: p.AttemptID, WorkPackageID: p.WorkPackageID}
	case *ChangeRejected:
		return Correlation{TaskID: p.TaskID, AttemptID: p.AttemptID}
	case *EscalationRaised:
		return Correlation{TaskID: p.TaskID}
	case *IntegrationStarted:
		return Correlation{TaskID: p.TaskID}
	case *IntegrationValidationStarted:
		return Correlation{TaskID: p.TaskID}
	}
	return Correlation{}
}

// NewPayload allocates an empty payload of the registered type.
//
// It exists for checks that must reason about every event shape the build
// knows — the drift test that no payload carries a record digest without
// implementing RecordReferencing — without each of them reaching into the
// registry's internals.
func NewPayload(t Type) (Payload, error) {
	registry.mu.RLock()
	factory, ok := registry.items[t]
	registry.mu.RUnlock()
	if !ok {
		return nil, errs.New(errs.CategorySchemaVersionUnsupported,
			"event type %q is not registered in this build", string(t))
	}
	return factory(), nil
}

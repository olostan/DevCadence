package flightrec

import (
	"regexp"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
)

// Durability selects the append class of a record.
type Durability uint8

const (
	// Diagnostic (the zero value) records are queued, never block and never
	// fail the caller.
	Diagnostic Durability = iota
	// Critical records are written and fsynced before the call returns; on
	// error the caller MUST refuse the protected effect.
	Critical
)

// Outcome is the terminal outcome of an operation.
type Outcome = wire.Outcome

// Outcome values. OutcomePanic is recorded only by Run while unwinding a real
// panic; the exported End rejects it.
const (
	OutcomeCompleted = wire.OutcomeCompleted
	OutcomeFailed    = wire.OutcomeFailed
	OutcomeCancelled = wire.OutcomeCancelled
	OutcomePanic     = wire.OutcomePanic
)

// ObservationKind classifies an observation.
type ObservationKind = wire.ObservationKind

// Observation kinds.
const (
	ObservationProgress = wire.ObservationProgress
	ObservationDecision = wire.ObservationDecision
	ObservationFact     = wire.ObservationFact
	ObservationAudit    = wire.ObservationAudit
)

// EvidenceState is the epistemic state of an observed fact. Missing and
// unknown evidence is first-class.
type EvidenceState = wire.EvidenceState

// Evidence states.
const (
	EvidenceObserved   = wire.EvidenceObserved
	EvidenceDocumented = wire.EvidenceDocumented
	EvidenceInferred   = wire.EvidenceInferred
	EvidenceConfirmed  = wire.EvidenceConfirmed
	EvidenceMissing    = wire.EvidenceMissing
	EvidenceUnknown    = wire.EvidenceUnknown
)

// Well-known Link relations.
const (
	RelationCausedBy    = "caused_by"
	RelationFollowsFrom = "follows_from"
	RelationRetryOf     = "retry_of"
	RelationFanIn       = "fan_in"
	RelationSupersedes  = "supersedes"
	RelationEvidenceFor = "evidence_for"
	RelationDerivedFrom = "derived_from"
	RelationHandoffTo   = "handoff_to"
)

const (
	maxNameBytes         = 96
	maxRelationBytes     = 48
	maxSpecLinks         = 16
	maxSpecArtifacts     = 16
	maxOpArtifacts       = 32
	maxResultKeys        = 64
	maxHealthAttemptsOut = wire.MaxAttempts
)

// The Recorder/Op API limits stay within the wire caps, so Marshal can never
// return ErrInvalid for a repeated field: Start/Observe reject more than 16
// links/artifacts, Op.AddArtifact drops beyond 32 (counted in ArtifactsDropped)
// and health attempts are capped at wire.MaxAttempts (excess counted in the
// detail).
const (
	_ = uint(wire.MaxLinks - maxSpecLinks)
	_ = uint(wire.MaxArtifacts - maxOpArtifacts)
	_ = uint(wire.MaxArtifacts - maxSpecArtifacts)
)

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// Link is a typed causal reference to another trace, operation or event.
type Link struct {
	Relation    string
	TraceID     string
	OperationID string
	EventID     string
}

// ArtifactRef references an artifact by identity and digest. It never implies
// that the artifact exists or was verified: DigestVerified is false unless the
// caller sets it.
type ArtifactRef struct {
	ID             string
	Kind           string
	Locator        string
	MediaType      string
	Digest         string // "sha256:<64 hex>" or empty
	SizeBytes      int64
	Truncated      bool
	DigestVerified bool
}

// ArtifactFromProtocol converts a protocol.ArtifactRef; the digest is never
// marked verified.
func ArtifactFromProtocol(p protocol.ArtifactRef) ArtifactRef {
	return ArtifactRef{
		ID: p.ID, Kind: p.Kind, Locator: p.Locator, MediaType: p.MediaType,
		Digest: p.Digest, SizeBytes: p.SizeBytes, Truncated: p.Truncated,
	}
}

// Subject names the thing an observation is about.
type Subject struct{ Kind, ID string }

// Provenance records where an observed fact came from (never contents).
type Provenance struct {
	SourceKind, SourceRef, ObservedBy, Method string
	ObservedAt                                time.Time
}

// StartSpec describes an operation to start. Name must match
// ^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$ and be at most 96 bytes.
type StartSpec struct {
	Name                                         string
	Durability, EndDurability                    Durability
	ActorID, TaskID, AttemptID, CanonicalEventID string
	Links                                        []Link
	Metadata                                     any
	Artifacts                                    []ArtifactRef
}

// ObservationSpec describes an observation. Payload must not be a protobuf
// message or Any (Phase 1 cannot sanitize them).
type ObservationSpec struct {
	Name       string
	Kind       ObservationKind
	Durability Durability
	ReasonCode string
	Subject    *Subject
	Provenance *Provenance
	Evidence   EvidenceState
	Payload    any
	Artifacts  []ArtifactRef
	Links      []Link
}

func invalidArg(format string, args ...any) error {
	return errs.New(errs.CategoryInvalidArgument, "flightrec: "+format, args...)
}

func validDigest(d string) bool { return d == "" || digestRE.MatchString(d) }

func validateLinks(links []Link) error {
	if len(links) > maxSpecLinks {
		return invalidArg("more than %d links", maxSpecLinks)
	}
	for _, l := range links {
		if len(l.Relation) > maxRelationBytes || !nameRE.MatchString(l.Relation) {
			return invalidArg("invalid link relation")
		}
		for _, id := range [...]string{l.TraceID, l.OperationID, l.EventID} {
			if id != "" && !ids.Valid(id) {
				return invalidArg("invalid link id")
			}
		}
	}
	return nil
}

func validateArtifacts(arts []ArtifactRef) error {
	if len(arts) > maxSpecArtifacts {
		return invalidArg("more than %d artifacts", maxSpecArtifacts)
	}
	for _, a := range arts {
		if !validDigest(a.Digest) {
			return invalidArg("artifact digest must be sha256:<64 hex>")
		}
	}
	return nil
}

func validateCommon(name string, links []Link, arts []ArtifactRef, durs ...Durability) error {
	if len(name) > maxNameBytes || !nameRE.MatchString(name) {
		return invalidArg("invalid name")
	}
	for _, d := range durs {
		if d > Critical {
			return invalidArg("invalid durability")
		}
	}
	if err := validateLinks(links); err != nil {
		return err
	}
	return validateArtifacts(arts)
}

func (s *Sanitizer) links(in []Link) []wire.Link {
	var out []wire.Link
	for _, l := range in {
		out = append(out, wire.Link{Relation: l.Relation, TraceID: l.TraceID, OperationID: l.OperationID, EventID: l.EventID})
	}
	return out
}

func (s *Sanitizer) artifact(t *tally, a ArtifactRef) wire.ArtifactRef {
	return wire.ArtifactRef{
		ID: s.scalar(t, ScalarID, a.ID), Kind: s.scalar(t, ScalarID, a.Kind),
		Locator: s.scalar(t, ScalarLocator, a.Locator), MediaType: s.scalar(t, ScalarMediaType, a.MediaType),
		Digest: a.Digest, SizeBytes: a.SizeBytes, Truncated: a.Truncated, DigestVerified: a.DigestVerified,
	}
}

func (s *Sanitizer) artifacts(t *tally, in []ArtifactRef) []wire.ArtifactRef {
	var out []wire.ArtifactRef
	for _, a := range in {
		out = append(out, s.artifact(t, a))
	}
	return out
}

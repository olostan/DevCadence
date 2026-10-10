package wire

// SchemaVersion is the envelope schema version written by this package.
const SchemaVersion = 1

// Enumerations. Unknown numbers decode without error and are preserved.
type (
	// RecordType discriminates the body of a JournalRecord.
	RecordType int32
	// Durability is the class the writer used for a record.
	Durability int32
	// Outcome is the terminal outcome of an operation.
	Outcome int32
	// ObservationKind classifies an Observation.
	ObservationKind int32
	// EvidenceState is the epistemic state of an observed fact.
	EvidenceState int32
	// HealthKind classifies a JournalHealth record.
	HealthKind int32
)

// RecordType values.
const (
	RecordTypeUnspecified    RecordType = 0
	RecordTypeOperationStart RecordType = 1
	RecordTypeOperationEnd   RecordType = 2
	RecordTypeObservation    RecordType = 3
	RecordTypeJournalHealth  RecordType = 4
)

// Durability values.
const (
	DurabilityUnspecified Durability = 0
	DurabilityCritical    Durability = 1
	DurabilityDiagnostic  Durability = 2
)

// Outcome values.
const (
	OutcomeUnspecified Outcome = 0
	OutcomeCompleted   Outcome = 1
	OutcomeFailed      Outcome = 2
	OutcomeCancelled   Outcome = 3
	OutcomePanic       Outcome = 4
)

// ObservationKind values.
const (
	ObservationKindUnspecified ObservationKind = 0
	ObservationProgress        ObservationKind = 1
	ObservationDecision        ObservationKind = 2
	ObservationFact            ObservationKind = 3
	ObservationAudit           ObservationKind = 4
)

// EvidenceState values.
const (
	EvidenceUnspecified EvidenceState = 0
	EvidenceObserved    EvidenceState = 1
	EvidenceDocumented  EvidenceState = 2
	EvidenceInferred    EvidenceState = 3
	EvidenceConfirmed   EvidenceState = 4
	EvidenceMissing     EvidenceState = 5
	EvidenceUnknown     EvidenceState = 6
)

// HealthKind values.
const (
	HealthKindUnspecified    HealthKind = 0
	HealthStreamStarted      HealthKind = 1
	HealthSegmentRotated     HealthKind = 2
	HealthDroppedDiagnostics HealthKind = 3
	HealthWriterError        HealthKind = 4
	HealthRecoveryReopen     HealthKind = 5
	HealthPathFallback       HealthKind = 6
	HealthDegradedNoop       HealthKind = 7
	HealthNodeIDEphemeral    HealthKind = 8
)

// Stamp is a wall and monotonic instant. Wall time is informational; Mono is
// comparable only within one runtime.
type Stamp struct {
	WallUnixNanos int64
	MonoNanos     int64
	Unknown       []byte
}

// ArtifactRef references an artifact by identity and digest. It never implies
// that the artifact exists or was verified.
type ArtifactRef struct {
	ID             string
	Kind           string
	Locator        string
	MediaType      string
	Digest         string
	SizeBytes      int64
	Truncated      bool
	DigestVerified bool
	Unknown        []byte
}

// Link is a typed causal reference.
type Link struct {
	Relation    string
	TraceID     string
	OperationID string
	EventID     string
	Unknown     []byte
}

// Subject names the thing an observation is about.
type Subject struct {
	Kind    string
	ID      string
	Unknown []byte
}

// Provenance records where an observed fact came from.
type Provenance struct {
	SourceKind string
	SourceRef  string
	ObservedBy string
	Method     string
	ObservedAt *Stamp
	Unknown    []byte
}

// Sanitization counts the redactions applied before persistence.
type Sanitization struct {
	Redactions      uint32
	Truncations     uint32
	PayloadReplaced bool
	Unknown         []byte
}

// Usage reports resource use of an operation.
type Usage struct {
	InputTokens   uint64
	OutputTokens  uint64
	DurationNanos uint64
	Unknown       []byte
}

// PathAttempt is one storage-path candidate tried by the recorder.
type PathAttempt struct {
	Source    string
	Path      string
	ErrorCode string
	Unknown   []byte
}

// Any carries a type URL and serialized value, wire-compatible with
// google.protobuf.Any. The codec does not interpret it.
type Any struct {
	TypeURL string
	Value   []byte
	Unknown []byte
}

// JournalRecord is the envelope of every journal record. Set at most one body
// member (Start, End, Observation, Health); on decode the last one wins.
type JournalRecord struct {
	SchemaVersion  uint32
	EventID        string
	NodeID         string
	RuntimeID      string
	StreamSequence uint64
	Type           RecordType
	Durability     Durability
	StreamID       string
	Start          *OperationStart
	End            *OperationEnd
	Observation    *Observation
	Health         *JournalHealth
	Unknown        []byte
}

// OperationStart is the immutable identity of a started operation.
type OperationStart struct {
	TraceID           string
	OperationID       string
	ParentOperationID string
	Links             []Link
	OperationName     string
	At                *Stamp
	JSONMetadata      []byte
	ActorID           string
	TaskID            string
	AttemptID         string
	CanonicalEventID  string
	Artifacts         []ArtifactRef
	Sanitization      *Sanitization
	Unknown           []byte
}

// OperationEnd is the terminal record of an operation.
type OperationEnd struct {
	OperationID  string
	At           *Stamp
	Outcome      Outcome
	ErrorCode    string
	ErrorSummary string
	JSONResult   []byte
	Usage        *Usage
	Artifacts    []ArtifactRef
	Sanitization *Sanitization
	Unknown      []byte
}

// Observation is a point-in-time diagnostic observation. Set at most one of
// JSONPayload and ProtoPayload; on decode the last one wins.
type Observation struct {
	OperationID  string
	At           *Stamp
	Name         string
	Kind         ObservationKind
	ReasonCode   string
	Subject      *Subject
	Provenance   *Provenance
	Evidence     EvidenceState
	Artifacts    []ArtifactRef
	Links        []Link
	Sanitization *Sanitization
	JSONPayload  []byte
	ProtoPayload *Any
	Unknown      []byte
}

// JournalHealth reports the writer's own condition.
type JournalHealth struct {
	Kind                 HealthKind
	At                   *Stamp
	DroppedCount         uint64
	FirstMissingSequence uint64
	LastMissingSequence  uint64
	DetailCode           string
	Detail               string
	Attempts             []PathAttempt
	SelectedSource       string
	Offset               int64
	Unknown              []byte
}

// SegmentHeader is the metadata block at the start of every segment.
type SegmentHeader struct {
	SchemaVersion           uint32
	NodeID                  string
	RuntimeID               string
	StreamID                string
	SegmentIndex            uint32
	FirstSequence           uint64
	CreatedAt               *Stamp
	WriterPID               uint32
	WriterVersion           string
	MaxRecordBytes          uint32
	MonoOriginWallUnixNanos int64
	Unknown                 []byte
}

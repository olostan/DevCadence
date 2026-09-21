// Package protocol contains the strongly typed Go representations of the
// durable DevCadience contracts defined in docs/PROTOCOLS.md and published as
// JSON Schema under schemas/.
//
// The Go type and the JSON Schema are twin representations of one contract
// (ENGINEERING_STANDARDS.md §5). This package therefore contains no storage,
// transport or provider concerns: it is the model-independent semantic
// language of the system (DCI-054) and must remain importable by every other
// package without dragging in SQLite, a model runtime or an MCP transport.
package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/olostan/DevCadience/internal/errs"
)

// SchemaVersion1 is the only durable contract version this build writes.
//
// docs/PROTOCOLS.md §18: durable records retain their original schema
// version, and readers either support a version or fail explicitly.
const SchemaVersion1 = "1.0"

// SchemaVersion is the schema_version field carried by every durable record.
type SchemaVersion string

// Validate rejects versions this build cannot interpret. It never guesses:
// DCI-092 forbids silently lossy parsing of durable records.
func (v SchemaVersion) Validate(kind string) error {
	if v == SchemaVersion1 {
		return nil
	}
	if v == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: schema_version is required", kind)
	}
	return errs.New(errs.CategorySchemaVersionUnsupported,
		"%s: schema_version %q is not supported by this build (supported: %s)", kind, string(v), SchemaVersion1)
}

// ActorKind identifies the class of actor responsible for a durable record or
// event. Kinds are semantic roles, never provider names (DCI-054).
type ActorKind string

const (
	// ActorHuman is the project owner or another human operator.
	ActorHuman ActorKind = "human"
	// ActorPrincipal is a frontier principal engineer.
	ActorPrincipal ActorKind = "principal"
	// ActorControlPlane is DevCadience itself.
	ActorControlPlane ActorKind = "control_plane"
	// ActorLocalAgent is a local model acting in an engineering role.
	ActorLocalAgent ActorKind = "local_agent"
	// ActorConsultant is an external frontier consultant.
	ActorConsultant ActorKind = "consultant"
	// ActorTool is deterministic machinery: compiler, tests, linters, Git.
	ActorTool ActorKind = "tool"
)

// Valid reports whether the actor kind is one this build understands.
func (k ActorKind) Valid() bool {
	switch k {
	case ActorHuman, ActorPrincipal, ActorControlPlane, ActorLocalAgent, ActorConsultant, ActorTool:
		return true
	}
	return false
}

// Actor identifies who produced a record. ID is a stable role/profile handle,
// never a credential and never a raw provider API identity (DCI-081).
type Actor struct {
	Kind ActorKind `json:"kind"`
	ID   string    `json:"id"`
}

// Validate checks that the actor is usable for attribution.
func (a Actor) Validate() error {
	if !a.Kind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "actor.kind %q is not a known actor kind", string(a.Kind))
	}
	if a.ID == "" {
		return errs.New(errs.CategoryInvalidArgument, "actor.id is required")
	}
	return nil
}

// EvidenceRefKind enumerates the provenance kinds an evidence reference may
// point at. It mirrors raw_evidence_refs[].type in
// schemas/evidence-packet.schema.json.
type EvidenceRefKind string

const (
	EvidenceRefSource         EvidenceRefKind = "source"
	EvidenceRefSymbol         EvidenceRefKind = "symbol"
	EvidenceRefGit            EvidenceRefKind = "git"
	EvidenceRefCommand        EvidenceRefKind = "command"
	EvidenceRefTest           EvidenceRefKind = "test"
	EvidenceRefExternalSource EvidenceRefKind = "external_source"
	EvidenceRefConsultation   EvidenceRefKind = "consultation"
	EvidenceRefArtifact       EvidenceRefKind = "artifact"
)

// Valid reports whether the kind is defined by the schema.
func (k EvidenceRefKind) Valid() bool {
	switch k {
	case EvidenceRefSource, EvidenceRefSymbol, EvidenceRefGit, EvidenceRefCommand,
		EvidenceRefTest, EvidenceRefExternalSource, EvidenceRefConsultation, EvidenceRefArtifact:
		return true
	}
	return false
}

// EvidenceRef points from a compact claim back to retrievable raw evidence.
//
// DCI-011: compression must not destroy provenance. Locator is a scheme the
// evidence layer can resolve (for example "git:<sha>:path/file.go#L10-L24");
// Digest fixes the exact bytes that were observed (DCI-014, §14 of
// docs/SECURITY.md).
type EvidenceRef struct {
	ID      string          `json:"id"`
	Type    EvidenceRefKind `json:"type"`
	Locator string          `json:"locator"`
	Digest  *string         `json:"digest,omitempty"`
	Excerpt *string         `json:"excerpt,omitempty"`
}

// Validate checks the reference is resolvable in principle.
func (r EvidenceRef) Validate() error {
	if r.ID == "" {
		return errs.New(errs.CategoryInvalidArgument, "evidence ref id is required")
	}
	if !r.Type.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "evidence ref %s: unknown type %q", r.ID, string(r.Type))
	}
	if r.Locator == "" {
		return errs.New(errs.CategoryInvalidArgument, "evidence ref %s: locator is required", r.ID)
	}
	return nil
}

// ArtifactRef names a large artifact that lives outside SQLite.
//
// docs/ARCHITECTURE.md §10 keeps logs, diffs and model transcripts in an
// artifact store and only their metadata in the relational store. Durable
// records therefore carry references, never payloads.
type ArtifactRef struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Locator   string `json:"locator"`
	MediaType string `json:"media_type,omitempty"`
	Digest    string `json:"digest,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Validate checks the artifact reference is usable.
func (r ArtifactRef) Validate() error {
	if r.ID == "" {
		return errs.New(errs.CategoryInvalidArgument, "artifact ref id is required")
	}
	if r.Locator == "" {
		return errs.New(errs.CategoryInvalidArgument, "artifact ref %s: locator is required", r.ID)
	}
	return nil
}

// Timestamp is an RFC3339 UTC instant with microsecond resolution.
//
// It exists as a named type so that every durable record serialises time the
// same way. Microsecond truncation makes a durable record round-trip through
// JSON and SQLite byte-identically, which the ProjectState rebuild guarantee
// depends on (docs/adr/0005-deterministic-project-state-identity.md).
type Timestamp time.Time

// NewTimestamp normalises t for durable storage.
func NewTimestamp(t time.Time) Timestamp {
	return Timestamp(t.UTC().Truncate(time.Microsecond))
}

// Time returns the underlying instant.
func (t Timestamp) Time() time.Time { return time.Time(t) }

// String renders the canonical textual form.
func (t Timestamp) String() string {
	return time.Time(t).UTC().Format(timestampLayout)
}

// timestampLayout keeps a fixed number of fractional digits so that textual
// ordering matches chronological ordering.
const timestampLayout = "2006-01-02T15:04:05.000000Z"

// MarshalJSON writes the canonical textual form.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// UnmarshalJSON accepts any RFC3339 instant and normalises it.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "timestamp must be an RFC3339 string")
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "timestamp %q is not RFC3339", s)
	}
	*t = NewTimestamp(parsed)
	return nil
}

// Record is implemented by every durable protocol record. It lets generic
// code (storage, schema validation, CLI inspection) handle records without
// reflection or type switches over a closed set.
type Record interface {
	// RecordKind returns the schema name, e.g. "ProjectState".
	RecordKind() string
	// RecordID returns the record's stable identifier.
	RecordID() string
	// SchemaVer returns the schema_version the record was created under.
	// It is named SchemaVer rather than Version because some records carry
	// their own content version field (for example Work Package revisions).
	SchemaVer() SchemaVersion
	// Validate checks the semantic constraints the JSON Schema cannot express
	// and re-checks those it can, so that a record is never persisted in a
	// state the schema would reject.
	Validate() error
}

// ProjectScoped is implemented by durable records that belong to exactly one
// project.
//
// It is deliberately *not* part of Record. LessonCandidate carries no
// project_id because DCI-073 allows a lesson to be scoped beyond one project,
// so a mandatory project identity on every record would misstate the
// contract. Persistence uses this interface to check that a record's own
// declared project agrees with the project it is being written to, and simply
// has nothing to check for records that do not implement it.
type ProjectScoped interface {
	Record
	// ProjectOf returns the project the record declares itself to belong to.
	ProjectOf() string
}

// Marshal serialises a durable record after validating it.
//
// Validation on the write path is deliberate: DCI-092 forbids durable records
// that a conforming reader would have to reject or reinterpret.
func Marshal(r Record) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "marshal %s", r.RecordKind())
	}
	return data, nil
}

// Unmarshal decodes a durable record strictly.
//
// Unknown fields are an error rather than a silent loss (DCI-092). Callers
// that must preserve a record written by a newer build keep the original
// bytes; see docs/adr/0003-durable-record-compatibility.md.
func Unmarshal[T Record](data []byte, out T) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "decode %s", out.RecordKind())
	}
	// A durable record is exactly one JSON value; trailing content means the
	// stored bytes are not the record we think they are.
	if _, err := dec.Token(); err != io.EOF {
		return errs.New(errs.CategoryInvalidArgument, "decode %s: unexpected trailing content", out.RecordKind())
	}
	if err := out.SchemaVer().Validate(out.RecordKind()); err != nil {
		return err
	}
	return out.Validate()
}

// CanonicalJSON returns a stable serialisation suitable for hashing.
//
// ENGINEERING_STANDARDS.md §17 requires canonical JSON when hashing
// artifacts. Object keys are sorted, HTML escaping is disabled and numbers
// keep their original literal form, so the same logical value always produces
// the same bytes and therefore the same digest.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "canonical json: marshal")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "canonical json: normalise")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "canonical json: encode")
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Digest returns the SHA-256 of the canonical JSON form, prefixed with its
// algorithm so that stored digests remain interpretable if the algorithm ever
// changes (DCI-093).
func Digest(v any) (string, error) {
	canonical, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	return DigestBytes(canonical), nil
}

// DigestBytes returns the algorithm-prefixed SHA-256 of exact bytes.
//
// The durable digest contract is defined over the *stored canonical bytes*,
// not over a re-serialisation of the decoded value
// (docs/adr/0003-durable-record-compatibility.md). That is the stronger
// invariant: it detects any change to what was written, including one that
// would re-serialise identically, and it stays verifiable for a record this
// build cannot decode at all. Read paths therefore verify with DigestBytes
// over what the database returned, never by re-encoding the Go value.
func DigestBytes(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports an integrity error when canonical does not hash to the
// expected digest.
func VerifyDigest(kind, id, expected string, canonical []byte) error {
	if expected == "" {
		return errs.New(errs.CategoryIntegrity,
			"%s %s was stored without a digest, so its integrity cannot be verified", kind, id)
	}
	if actual := DigestBytes(canonical); actual != expected {
		return errs.New(errs.CategoryIntegrity,
			"%s %s fails its digest check: stored %s, computed %s; "+
				"the persisted bytes have changed since they were written", kind, id, expected, actual)
	}
	return nil
}

// requireNonEmpty is a small helper used by Validate implementations to keep
// their error messages uniform.
func requireNonEmpty(kind, field, value string) error {
	if value == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: %s is required", kind, field)
	}
	return nil
}

// requireMinItems enforces the minItems constraints the schemas declare.
func requireMinItems(kind, field string, n, min int) error {
	if n < min {
		return errs.New(errs.CategoryInvalidArgument, "%s: %s requires at least %d item(s), got %d", kind, field, min, n)
	}
	return nil
}

// enumError builds the error used when a closed enumeration is violated.
func enumError(kind, field, value string, allowed ...string) error {
	return errs.New(errs.CategoryInvalidArgument, "%s: %s %q is not one of %v", kind, field, value, allowed)
}

// ProjectOf implementations. Each simply returns the record's own project_id,
// which persistence checks against the project it is being written to.
func (s *ProjectState) ProjectOf() string           { return s.ProjectID }
func (w *EngineeringWorkPackage) ProjectOf() string { return w.ProjectID }
func (p *EvidencePacket) ProjectOf() string         { return p.ProjectID }
func (v *ValidationResult) ProjectOf() string       { return v.ProjectID }
func (r *ReviewResult) ProjectOf() string           { return r.ProjectID }
func (d *DecisionRecord) ProjectOf() string         { return d.ProjectID }
func (m *ProblemModel) ProjectOf() string           { return m.ProjectID }
func (l *AmbiguityLedger) ProjectOf() string        { return l.ProjectID }
func (d *ProductDecision) ProjectOf() string        { return d.ProjectID }
func (r *Requirement) ProjectOf() string            { return r.ProjectID }
func (e *DiscoveryExperiment) ProjectOf() string    { return e.ProjectID }
func (r *SpecificationReadiness) ProjectOf() string { return r.ProjectID }

// LessonCandidate deliberately has no ProjectOf: it carries no project_id,
// because a lesson may be promoted beyond the project that produced it
// (DCI-073). It is stored under the project that proposed it, and nothing
// cross-checks a field it does not have.

// NewRecord allocates an empty record of the named kind.
//
// The mapping is an explicit switch rather than a reflective registry so that
// adding a protocol type is a visible edit here, and so the set of durable
// kinds stays readable in one place. It lets a caller holding only a kind
// name — the CLI decoding a record supplied alongside an event — decode into
// the right typed document instead of a map.
func NewRecord(kind string) (Record, error) {
	switch kind {
	case "ProjectState":
		return &ProjectState{}, nil
	case "EngineeringWorkPackage":
		return &EngineeringWorkPackage{}, nil
	case "EvidencePacket":
		return &EvidencePacket{}, nil
	case "ValidationResult":
		return &ValidationResult{}, nil
	case "ReviewResult":
		return &ReviewResult{}, nil
	case "SpecificationReviewResult":
		return &SpecificationReviewResult{}, nil
	case "DecisionRecord":
		return &DecisionRecord{}, nil
	case "LessonCandidate":
		return &LessonCandidate{}, nil
	case "ProblemModel":
		return &ProblemModel{}, nil
	case "AmbiguityLedger":
		return &AmbiguityLedger{}, nil
	case "ProductDecision":
		return &ProductDecision{}, nil
	case "Requirement":
		return &Requirement{}, nil
	case "DiscoveryExperiment":
		return &DiscoveryExperiment{}, nil
	case "SpecificationReadiness":
		return &SpecificationReadiness{}, nil
	case "MachineCapabilityProfile":
		return &MachineCapabilityProfile{}, nil
	}
	return nil, errs.New(errs.CategoryInvalidArgument, "unknown record kind %q", kind)
}

package wire

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
)

// Codec errors. Wrap-checking with errors.Is is supported.
var (
	// ErrMalformed reports a truncated or overlong varint/length, or nesting
	// deeper than the supported depth.
	ErrMalformed = errors.New("wire: malformed message")
	// ErrInvalidUTF8 reports invalid UTF-8 in a string field.
	ErrInvalidUTF8 = errors.New("wire: invalid UTF-8 in string field")
	// ErrInvalid reports a decodable record that violates envelope semantics.
	ErrInvalid = errors.New("wire: invalid journal record")
)

// Caps on repeated message fields. A decoder rejects (ErrMalformed) and an
// encoder refuses (ErrInvalid) a message with more entries, which bounds the
// allocation a small hostile record can force (an empty entry costs 2 bytes on
// the wire but a full struct in memory).
const (
	// MaxLinks caps OperationStart.Links and Observation.Links.
	MaxLinks = 64
	// MaxArtifacts caps the Artifacts field of OperationStart, OperationEnd and Observation.
	MaxArtifacts = 64
	// MaxAttempts caps JournalHealth.Attempts.
	MaxAttempts = 64
)

// maxDepth is the maximum nesting of decoded messages below the root.
const maxDepth = 3

type encodable interface{ encode(e *encoder) }

type decodable interface {
	unmarshalAt(b []byte, depth int) error
}

// encoder appends deterministic protobuf wire bytes. The first error sticks.
type encoder struct {
	b   []byte
	err error
}

func marshal(m encodable) ([]byte, error) {
	var e encoder
	m.encode(&e)
	return e.b, e.err
}

// limit records ErrInvalid when a repeated field has more than max entries.
func (e *encoder) limit(field string, n, max int) {
	if n > max {
		e.err = errors.Join(e.err, fmt.Errorf("%w: %d %s entries exceed the cap of %d", ErrInvalid, n, field, max))
	}
}

func (e *encoder) tag(n protowire.Number, t protowire.Type) {
	e.b = protowire.AppendTag(e.b, n, t)
}

func (e *encoder) varint(n protowire.Number, v uint64) {
	if v != 0 {
		e.tag(n, protowire.VarintType)
		e.b = protowire.AppendVarint(e.b, v)
	}
}

func (e *encoder) int64(n protowire.Number, v int64) { e.varint(n, uint64(v)) }

func (e *encoder) boolean(n protowire.Number, v bool) {
	if v {
		e.varint(n, 1)
	}
}

func (e *encoder) str(n protowire.Number, s string) {
	if s == "" {
		return
	}
	if !utf8.ValidString(s) {
		e.err = errors.Join(e.err, ErrInvalidUTF8)
		return
	}
	e.tag(n, protowire.BytesType)
	e.b = protowire.AppendString(e.b, s)
}

// bytes emits a bytes field, omitting it when empty (proto3 implicit presence).
func (e *encoder) bytes(n protowire.Number, v []byte) {
	if len(v) > 0 {
		e.present(n, v)
	}
}

// present emits a bytes field even when empty (oneof members, messages).
func (e *encoder) present(n protowire.Number, v []byte) {
	e.tag(n, protowire.BytesType)
	e.b = protowire.AppendBytes(e.b, v)
}

func (e *encoder) msg(n protowire.Number, m encodable) {
	var sub encoder
	m.encode(&sub)
	e.err = errors.Join(e.err, sub.err)
	e.present(n, sub.b)
}

func (e *encoder) unknown(u []byte) { e.b = append(e.b, u...) }

// scan walks the fields of b. field reports whether it consumed the field;
// unconsumed fields (unknown numbers or unexpected wire types) are preserved
// verbatim in unknown.
func scan(b []byte, unknown *[]byte, field func(num protowire.Number, typ protowire.Type, v []byte) (bool, error)) error {
	for len(b) > 0 {
		num, typ, tn := protowire.ConsumeTag(b)
		if tn < 0 {
			return ErrMalformed
		}
		vn := protowire.ConsumeFieldValue(num, typ, b[tn:])
		if vn < 0 {
			return ErrMalformed
		}
		ok, err := field(num, typ, b[tn:tn+vn])
		if err != nil {
			return err
		}
		if !ok {
			*unknown = append(*unknown, b[:tn+vn]...)
		}
		b = b[tn+vn:]
	}
	return nil
}

func setInt[T ~int32 | ~int64 | ~uint32 | ~uint64](dst *T, typ protowire.Type, v []byte) (bool, error) {
	if typ != protowire.VarintType {
		return false, nil
	}
	x, _ := protowire.ConsumeVarint(v)
	*dst = T(x)
	return true, nil
}

func setBool(dst *bool, typ protowire.Type, v []byte) (bool, error) {
	if typ != protowire.VarintType {
		return false, nil
	}
	x, _ := protowire.ConsumeVarint(v)
	*dst = x != 0
	return true, nil
}

func lengthDelimited(typ protowire.Type, v []byte) ([]byte, bool) {
	if typ != protowire.BytesType {
		return nil, false
	}
	b, _ := protowire.ConsumeBytes(v)
	return b, true
}

func setStr(dst *string, typ protowire.Type, v []byte) (bool, error) {
	b, ok := lengthDelimited(typ, v)
	if !ok {
		return false, nil
	}
	if !utf8.Valid(b) {
		return true, ErrInvalidUTF8
	}
	*dst = string(b)
	return true, nil
}

func setBytes(dst *[]byte, typ protowire.Type, v []byte) (bool, error) {
	b, ok := lengthDelimited(typ, v)
	if !ok {
		return false, nil
	}
	*dst = append([]byte{}, b...)
	return true, nil
}

// decodeChild decodes a nested message value into child at depth+1.
func decodeChild(child decodable, typ protowire.Type, v []byte, depth int) (bool, error) {
	b, ok := lengthDelimited(typ, v)
	if !ok {
		return false, nil
	}
	if depth >= maxDepth {
		return true, fmt.Errorf("%w: nesting deeper than %d", ErrMalformed, maxDepth)
	}
	return true, child.unmarshalAt(b, depth+1)
}

func setPtr[T any, P interface {
	*T
	decodable
}](dst **T, typ protowire.Type, v []byte, depth int) (bool, error) {
	p := new(T)
	ok, err := decodeChild(P(p), typ, v, depth)
	if ok && err == nil {
		*dst = p
	}
	return ok, err
}

func addMsg[T any, P interface {
	*T
	decodable
}](dst *[]T, limit int, typ protowire.Type, v []byte, depth int) (bool, error) {
	if _, ok := lengthDelimited(typ, v); ok && len(*dst) >= limit {
		return true, fmt.Errorf("%w: more than %d entries in a repeated field", ErrMalformed, limit)
	}
	var x T
	ok, err := decodeChild(P(&x), typ, v, depth)
	if ok && err == nil {
		*dst = append(*dst, x)
	}
	return ok, err
}

// ---- Stamp ----

// Marshal returns the deterministic wire encoding of m.
func (m *Stamp) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *Stamp) Unmarshal(b []byte) error { *m = Stamp{}; return m.unmarshalAt(b, 0) }

func (m *Stamp) encode(e *encoder) {
	e.int64(1, m.WallUnixNanos)
	e.int64(2, m.MonoNanos)
	e.unknown(m.Unknown)
}

func (m *Stamp) unmarshalAt(b []byte, _ int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setInt(&m.WallUnixNanos, typ, v)
		case 2:
			return setInt(&m.MonoNanos, typ, v)
		}
		return false, nil
	})
}

// ---- ArtifactRef ----

// Marshal returns the deterministic wire encoding of m.
func (m *ArtifactRef) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *ArtifactRef) Unmarshal(b []byte) error { *m = ArtifactRef{}; return m.unmarshalAt(b, 0) }

func (m *ArtifactRef) encode(e *encoder) {
	e.str(1, m.ID)
	e.str(2, m.Kind)
	e.str(3, m.Locator)
	e.str(4, m.MediaType)
	e.str(5, m.Digest)
	e.int64(6, m.SizeBytes)
	e.boolean(7, m.Truncated)
	e.boolean(8, m.DigestVerified)
	e.unknown(m.Unknown)
}

func (m *ArtifactRef) unmarshalAt(b []byte, _ int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setStr(&m.ID, typ, v)
		case 2:
			return setStr(&m.Kind, typ, v)
		case 3:
			return setStr(&m.Locator, typ, v)
		case 4:
			return setStr(&m.MediaType, typ, v)
		case 5:
			return setStr(&m.Digest, typ, v)
		case 6:
			return setInt(&m.SizeBytes, typ, v)
		case 7:
			return setBool(&m.Truncated, typ, v)
		case 8:
			return setBool(&m.DigestVerified, typ, v)
		}
		return false, nil
	})
}

// ---- Link ----

// Marshal returns the deterministic wire encoding of m.
func (m *Link) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *Link) Unmarshal(b []byte) error { *m = Link{}; return m.unmarshalAt(b, 0) }

func (m *Link) encode(e *encoder) {
	e.str(1, m.Relation)
	e.str(2, m.TraceID)
	e.str(3, m.OperationID)
	e.str(4, m.EventID)
	e.unknown(m.Unknown)
}

func (m *Link) unmarshalAt(b []byte, _ int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setStr(&m.Relation, typ, v)
		case 2:
			return setStr(&m.TraceID, typ, v)
		case 3:
			return setStr(&m.OperationID, typ, v)
		case 4:
			return setStr(&m.EventID, typ, v)
		}
		return false, nil
	})
}

// ---- Subject ----

// Marshal returns the deterministic wire encoding of m.
func (m *Subject) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *Subject) Unmarshal(b []byte) error { *m = Subject{}; return m.unmarshalAt(b, 0) }

func (m *Subject) encode(e *encoder) {
	e.str(1, m.Kind)
	e.str(2, m.ID)
	e.unknown(m.Unknown)
}

func (m *Subject) unmarshalAt(b []byte, _ int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setStr(&m.Kind, typ, v)
		case 2:
			return setStr(&m.ID, typ, v)
		}
		return false, nil
	})
}

// ---- Provenance ----

// Marshal returns the deterministic wire encoding of m.
func (m *Provenance) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *Provenance) Unmarshal(b []byte) error { *m = Provenance{}; return m.unmarshalAt(b, 0) }

func (m *Provenance) encode(e *encoder) {
	e.str(1, m.SourceKind)
	e.str(2, m.SourceRef)
	e.str(3, m.ObservedBy)
	e.str(4, m.Method)
	if m.ObservedAt != nil {
		e.msg(5, m.ObservedAt)
	}
	e.unknown(m.Unknown)
}

func (m *Provenance) unmarshalAt(b []byte, depth int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setStr(&m.SourceKind, typ, v)
		case 2:
			return setStr(&m.SourceRef, typ, v)
		case 3:
			return setStr(&m.ObservedBy, typ, v)
		case 4:
			return setStr(&m.Method, typ, v)
		case 5:
			return setPtr(&m.ObservedAt, typ, v, depth)
		}
		return false, nil
	})
}

// ---- Sanitization ----

// Marshal returns the deterministic wire encoding of m.
func (m *Sanitization) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *Sanitization) Unmarshal(b []byte) error { *m = Sanitization{}; return m.unmarshalAt(b, 0) }

func (m *Sanitization) encode(e *encoder) {
	e.varint(1, uint64(m.Redactions))
	e.varint(2, uint64(m.Truncations))
	e.boolean(3, m.PayloadReplaced)
	e.unknown(m.Unknown)
}

func (m *Sanitization) unmarshalAt(b []byte, _ int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setInt(&m.Redactions, typ, v)
		case 2:
			return setInt(&m.Truncations, typ, v)
		case 3:
			return setBool(&m.PayloadReplaced, typ, v)
		}
		return false, nil
	})
}

// ---- Usage ----

// Marshal returns the deterministic wire encoding of m.
func (m *Usage) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *Usage) Unmarshal(b []byte) error { *m = Usage{}; return m.unmarshalAt(b, 0) }

func (m *Usage) encode(e *encoder) {
	e.varint(1, m.InputTokens)
	e.varint(2, m.OutputTokens)
	e.varint(3, m.DurationNanos)
	e.unknown(m.Unknown)
}

func (m *Usage) unmarshalAt(b []byte, _ int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setInt(&m.InputTokens, typ, v)
		case 2:
			return setInt(&m.OutputTokens, typ, v)
		case 3:
			return setInt(&m.DurationNanos, typ, v)
		}
		return false, nil
	})
}

// ---- PathAttempt ----

// Marshal returns the deterministic wire encoding of m.
func (m *PathAttempt) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *PathAttempt) Unmarshal(b []byte) error { *m = PathAttempt{}; return m.unmarshalAt(b, 0) }

func (m *PathAttempt) encode(e *encoder) {
	e.str(1, m.Source)
	e.str(2, m.Path)
	e.str(3, m.ErrorCode)
	e.unknown(m.Unknown)
}

func (m *PathAttempt) unmarshalAt(b []byte, _ int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setStr(&m.Source, typ, v)
		case 2:
			return setStr(&m.Path, typ, v)
		case 3:
			return setStr(&m.ErrorCode, typ, v)
		}
		return false, nil
	})
}

// ---- Any ----

// Marshal returns the deterministic wire encoding of m.
func (m *Any) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *Any) Unmarshal(b []byte) error { *m = Any{}; return m.unmarshalAt(b, 0) }

func (m *Any) encode(e *encoder) {
	e.str(1, m.TypeURL)
	e.bytes(2, m.Value)
	e.unknown(m.Unknown)
}

func (m *Any) unmarshalAt(b []byte, _ int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setStr(&m.TypeURL, typ, v)
		case 2:
			return setBytes(&m.Value, typ, v)
		}
		return false, nil
	})
}

// ---- JournalRecord ----

// Marshal returns the deterministic wire encoding of m.
func (m *JournalRecord) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *JournalRecord) Unmarshal(b []byte) error { *m = JournalRecord{}; return m.unmarshalAt(b, 0) }

func (m *JournalRecord) encode(e *encoder) {
	e.varint(1, uint64(m.SchemaVersion))
	e.str(2, m.EventID)
	e.str(3, m.NodeID)
	e.str(4, m.RuntimeID)
	e.varint(5, m.StreamSequence)
	e.varint(6, uint64(m.Type))
	e.varint(7, uint64(m.Durability))
	e.str(8, m.StreamID)
	if m.Start != nil {
		e.msg(10, m.Start)
	}
	if m.End != nil {
		e.msg(11, m.End)
	}
	if m.Observation != nil {
		e.msg(12, m.Observation)
	}
	if m.Health != nil {
		e.msg(13, m.Health)
	}
	e.unknown(m.Unknown)
}

func (m *JournalRecord) unmarshalAt(b []byte, depth int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setInt(&m.SchemaVersion, typ, v)
		case 2:
			return setStr(&m.EventID, typ, v)
		case 3:
			return setStr(&m.NodeID, typ, v)
		case 4:
			return setStr(&m.RuntimeID, typ, v)
		case 5:
			return setInt(&m.StreamSequence, typ, v)
		case 6:
			return setInt(&m.Type, typ, v)
		case 7:
			return setInt(&m.Durability, typ, v)
		case 8:
			return setStr(&m.StreamID, typ, v)
		case 10, 11, 12, 13:
			return m.setBody(num, typ, v, depth)
		}
		return false, nil
	})
}

// setBody decodes one oneof member; the previously set member is cleared.
func (m *JournalRecord) setBody(num protowire.Number, typ protowire.Type, v []byte, depth int) (bool, error) {
	var (
		st  *OperationStart
		en  *OperationEnd
		ob  *Observation
		he  *JournalHealth
		ok  bool
		err error
	)
	switch num {
	case 10:
		ok, err = setPtr(&st, typ, v, depth)
	case 11:
		ok, err = setPtr(&en, typ, v, depth)
	case 12:
		ok, err = setPtr(&ob, typ, v, depth)
	default:
		ok, err = setPtr(&he, typ, v, depth)
	}
	if ok && err == nil {
		m.Start, m.End, m.Observation, m.Health = st, en, ob, he
	}
	return ok, err
}

// BodyType reports the record type implied by the set body member, or
// RecordTypeUnspecified when no known body member is set.
func (m *JournalRecord) BodyType() RecordType {
	switch {
	case m.Start != nil:
		return RecordTypeOperationStart
	case m.End != nil:
		return RecordTypeOperationEnd
	case m.Observation != nil:
		return RecordTypeObservation
	case m.Health != nil:
		return RecordTypeJournalHealth
	}
	return RecordTypeUnspecified
}

func (m *JournalRecord) bodyCount() int {
	n := 0
	for _, set := range []bool{m.Start != nil, m.End != nil, m.Observation != nil, m.Health != nil} {
		if set {
			n++
		}
	}
	return n
}

// Validate checks envelope semantics: schema_version >= 1, non-empty
// identifiers, sequence >= 1 and a type that agrees with the set body member.
// A record with no body member and a type outside 1..4 is an unknown (future)
// record type and is valid.
func (m *JournalRecord) Validate() error {
	switch {
	case m.SchemaVersion < 1:
		return fmt.Errorf("%w: schema_version %d", ErrInvalid, m.SchemaVersion)
	case m.EventID == "" || m.NodeID == "" || m.RuntimeID == "" || m.StreamID == "":
		return fmt.Errorf("%w: missing identifier", ErrInvalid)
	case m.StreamSequence < 1:
		return fmt.Errorf("%w: stream_sequence %d", ErrInvalid, m.StreamSequence)
	case m.bodyCount() > 1:
		return fmt.Errorf("%w: more than one body member", ErrInvalid)
	}
	if body := m.BodyType(); body != RecordTypeUnspecified {
		if m.Type != body {
			return fmt.Errorf("%w: type %d disagrees with body %d", ErrInvalid, m.Type, body)
		}
	} else if m.Type >= RecordTypeOperationStart && m.Type <= RecordTypeJournalHealth {
		return fmt.Errorf("%w: type %d has no body", ErrInvalid, m.Type)
	}
	return nil
}

// ---- OperationStart ----

// Marshal returns the deterministic wire encoding of m.
func (m *OperationStart) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *OperationStart) Unmarshal(b []byte) error { *m = OperationStart{}; return m.unmarshalAt(b, 0) }

func (m *OperationStart) encode(e *encoder) {
	e.str(1, m.TraceID)
	e.str(2, m.OperationID)
	e.str(3, m.ParentOperationID)
	e.limit("Links", len(m.Links), MaxLinks)
	for i := range m.Links {
		e.msg(4, &m.Links[i])
	}
	e.str(5, m.OperationName)
	if m.At != nil {
		e.msg(6, m.At)
	}
	e.bytes(7, m.JSONMetadata)
	e.str(8, m.ActorID)
	e.str(9, m.TaskID)
	e.str(10, m.AttemptID)
	e.str(11, m.CanonicalEventID)
	e.limit("Artifacts", len(m.Artifacts), MaxArtifacts)
	for i := range m.Artifacts {
		e.msg(12, &m.Artifacts[i])
	}
	if m.Sanitization != nil {
		e.msg(13, m.Sanitization)
	}
	e.unknown(m.Unknown)
}

func (m *OperationStart) unmarshalAt(b []byte, depth int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setStr(&m.TraceID, typ, v)
		case 2:
			return setStr(&m.OperationID, typ, v)
		case 3:
			return setStr(&m.ParentOperationID, typ, v)
		case 4:
			return addMsg(&m.Links, MaxLinks, typ, v, depth)
		case 5:
			return setStr(&m.OperationName, typ, v)
		case 6:
			return setPtr(&m.At, typ, v, depth)
		case 7:
			return setBytes(&m.JSONMetadata, typ, v)
		case 8:
			return setStr(&m.ActorID, typ, v)
		case 9:
			return setStr(&m.TaskID, typ, v)
		case 10:
			return setStr(&m.AttemptID, typ, v)
		case 11:
			return setStr(&m.CanonicalEventID, typ, v)
		case 12:
			return addMsg(&m.Artifacts, MaxArtifacts, typ, v, depth)
		case 13:
			return setPtr(&m.Sanitization, typ, v, depth)
		}
		return false, nil
	})
}

// ---- OperationEnd ----

// Marshal returns the deterministic wire encoding of m.
func (m *OperationEnd) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *OperationEnd) Unmarshal(b []byte) error { *m = OperationEnd{}; return m.unmarshalAt(b, 0) }

func (m *OperationEnd) encode(e *encoder) {
	e.str(1, m.OperationID)
	if m.At != nil {
		e.msg(2, m.At)
	}
	e.varint(3, uint64(m.Outcome))
	e.str(4, m.ErrorCode)
	e.str(5, m.ErrorSummary)
	e.bytes(6, m.JSONResult)
	if m.Usage != nil {
		e.msg(7, m.Usage)
	}
	e.limit("Artifacts", len(m.Artifacts), MaxArtifacts)
	for i := range m.Artifacts {
		e.msg(8, &m.Artifacts[i])
	}
	if m.Sanitization != nil {
		e.msg(9, m.Sanitization)
	}
	e.unknown(m.Unknown)
}

func (m *OperationEnd) unmarshalAt(b []byte, depth int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setStr(&m.OperationID, typ, v)
		case 2:
			return setPtr(&m.At, typ, v, depth)
		case 3:
			return setInt(&m.Outcome, typ, v)
		case 4:
			return setStr(&m.ErrorCode, typ, v)
		case 5:
			return setStr(&m.ErrorSummary, typ, v)
		case 6:
			return setBytes(&m.JSONResult, typ, v)
		case 7:
			return setPtr(&m.Usage, typ, v, depth)
		case 8:
			return addMsg(&m.Artifacts, MaxArtifacts, typ, v, depth)
		case 9:
			return setPtr(&m.Sanitization, typ, v, depth)
		}
		return false, nil
	})
}

// ---- Observation ----

// Marshal returns the deterministic wire encoding of m.
func (m *Observation) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *Observation) Unmarshal(b []byte) error { *m = Observation{}; return m.unmarshalAt(b, 0) }

func (m *Observation) encode(e *encoder) {
	e.str(1, m.OperationID)
	if m.At != nil {
		e.msg(2, m.At)
	}
	e.str(3, m.Name)
	e.varint(4, uint64(m.Kind))
	e.str(5, m.ReasonCode)
	if m.Subject != nil {
		e.msg(6, m.Subject)
	}
	if m.Provenance != nil {
		e.msg(7, m.Provenance)
	}
	e.varint(8, uint64(m.Evidence))
	e.limit("Artifacts", len(m.Artifacts), MaxArtifacts)
	for i := range m.Artifacts {
		e.msg(9, &m.Artifacts[i])
	}
	e.limit("Links", len(m.Links), MaxLinks)
	for i := range m.Links {
		e.msg(10, &m.Links[i])
	}
	if m.Sanitization != nil {
		e.msg(11, m.Sanitization)
	}
	if m.JSONPayload != nil {
		e.present(20, m.JSONPayload)
	}
	if m.ProtoPayload != nil {
		e.msg(21, m.ProtoPayload)
	}
	e.unknown(m.Unknown)
}

func (m *Observation) unmarshalAt(b []byte, depth int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setStr(&m.OperationID, typ, v)
		case 2:
			return setPtr(&m.At, typ, v, depth)
		case 3:
			return setStr(&m.Name, typ, v)
		case 4:
			return setInt(&m.Kind, typ, v)
		case 5:
			return setStr(&m.ReasonCode, typ, v)
		case 6:
			return setPtr(&m.Subject, typ, v, depth)
		case 7:
			return setPtr(&m.Provenance, typ, v, depth)
		case 8:
			return setInt(&m.Evidence, typ, v)
		case 9:
			return addMsg(&m.Artifacts, MaxArtifacts, typ, v, depth)
		case 10:
			return addMsg(&m.Links, MaxLinks, typ, v, depth)
		case 11:
			return setPtr(&m.Sanitization, typ, v, depth)
		case 20, 21:
			return m.setPayload(num, typ, v, depth)
		}
		return false, nil
	})
}

// setPayload decodes one payload oneof member; the other member is cleared.
func (m *Observation) setPayload(num protowire.Number, typ protowire.Type, v []byte, depth int) (bool, error) {
	var (
		js  []byte
		pp  *Any
		ok  bool
		err error
	)
	if num == 20 {
		ok, err = setBytes(&js, typ, v)
	} else {
		ok, err = setPtr(&pp, typ, v, depth)
	}
	if ok && err == nil {
		m.JSONPayload, m.ProtoPayload = js, pp
	}
	return ok, err
}

// ---- JournalHealth ----

// Marshal returns the deterministic wire encoding of m.
func (m *JournalHealth) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *JournalHealth) Unmarshal(b []byte) error { *m = JournalHealth{}; return m.unmarshalAt(b, 0) }

func (m *JournalHealth) encode(e *encoder) {
	e.varint(1, uint64(m.Kind))
	if m.At != nil {
		e.msg(2, m.At)
	}
	e.varint(3, m.DroppedCount)
	e.varint(4, m.FirstMissingSequence)
	e.varint(5, m.LastMissingSequence)
	e.str(6, m.DetailCode)
	e.str(7, m.Detail)
	e.limit("Attempts", len(m.Attempts), MaxAttempts)
	for i := range m.Attempts {
		e.msg(8, &m.Attempts[i])
	}
	e.str(9, m.SelectedSource)
	e.int64(10, m.Offset)
	e.unknown(m.Unknown)
}

func (m *JournalHealth) unmarshalAt(b []byte, depth int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setInt(&m.Kind, typ, v)
		case 2:
			return setPtr(&m.At, typ, v, depth)
		case 3:
			return setInt(&m.DroppedCount, typ, v)
		case 4:
			return setInt(&m.FirstMissingSequence, typ, v)
		case 5:
			return setInt(&m.LastMissingSequence, typ, v)
		case 6:
			return setStr(&m.DetailCode, typ, v)
		case 7:
			return setStr(&m.Detail, typ, v)
		case 8:
			return addMsg(&m.Attempts, MaxAttempts, typ, v, depth)
		case 9:
			return setStr(&m.SelectedSource, typ, v)
		case 10:
			return setInt(&m.Offset, typ, v)
		}
		return false, nil
	})
}

// ---- SegmentHeader ----

// Marshal returns the deterministic wire encoding of m.
func (m *SegmentHeader) Marshal() ([]byte, error) { return marshal(m) }

// Unmarshal replaces m with the decoding of b.
func (m *SegmentHeader) Unmarshal(b []byte) error { *m = SegmentHeader{}; return m.unmarshalAt(b, 0) }

func (m *SegmentHeader) encode(e *encoder) {
	e.varint(1, uint64(m.SchemaVersion))
	e.str(2, m.NodeID)
	e.str(3, m.RuntimeID)
	e.str(4, m.StreamID)
	e.varint(5, uint64(m.SegmentIndex))
	e.varint(6, m.FirstSequence)
	if m.CreatedAt != nil {
		e.msg(7, m.CreatedAt)
	}
	e.varint(8, uint64(m.WriterPID))
	e.str(9, m.WriterVersion)
	e.varint(10, uint64(m.MaxRecordBytes))
	e.int64(11, m.MonoOriginWallUnixNanos)
	e.unknown(m.Unknown)
}

func (m *SegmentHeader) unmarshalAt(b []byte, depth int) error {
	return scan(b, &m.Unknown, func(num protowire.Number, typ protowire.Type, v []byte) (bool, error) {
		switch num {
		case 1:
			return setInt(&m.SchemaVersion, typ, v)
		case 2:
			return setStr(&m.NodeID, typ, v)
		case 3:
			return setStr(&m.RuntimeID, typ, v)
		case 4:
			return setStr(&m.StreamID, typ, v)
		case 5:
			return setInt(&m.SegmentIndex, typ, v)
		case 6:
			return setInt(&m.FirstSequence, typ, v)
		case 7:
			return setPtr(&m.CreatedAt, typ, v, depth)
		case 8:
			return setInt(&m.WriterPID, typ, v)
		case 9:
			return setStr(&m.WriterVersion, typ, v)
		case 10:
			return setInt(&m.MaxRecordBytes, typ, v)
		case 11:
			return setInt(&m.MonoOriginWallUnixNanos, typ, v)
		}
		return false, nil
	})
}

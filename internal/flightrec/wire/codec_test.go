package wire

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// A-1: committed golden vectors for one record of each type (plus the segment
// header) and byte-stable round trips for every fixture. Set
// DEVCADENCE_UPDATE_GOLDEN=1 only when the schema is changed deliberately; the
// vectors are independently validated by the dynamicpb cross-check (A-3).
func TestGoldenVectors(t *testing.T) {
	golden := map[string]codec{
		"record_start":       fixRecordStart(),
		"record_end":         fixRecordEnd(),
		"record_observation": fixRecordObservation(),
		"record_health":      fixRecordHealth(),
		"segment_header":     fixSegmentHeader(),
	}
	for name, v := range golden {
		t.Run(name, func(t *testing.T) {
			got, err := v.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", name+".hex")
			if os.Getenv("DEVCADENCE_UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, []byte(hex.EncodeToString(got)+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want, err := hex.DecodeString(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("golden mismatch for %s\n got %x\nwant %x", name, got, want)
			}
			back := fresh(v)
			if err := back.Unmarshal(want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(back, v) {
				t.Fatalf("decode of golden bytes differs from fixture")
			}
		})
	}
}

func TestRoundTripAndStability(t *testing.T) {
	for _, fx := range allFixtures() {
		t.Run(fx.name+"/"+fx.variant, func(t *testing.T) {
			a, err := fx.value.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			b, _ := fx.value.Marshal()
			if !bytes.Equal(a, b) {
				t.Fatal("Marshal is not byte-stable")
			}
			back := fresh(fx.value)
			if err := back.Unmarshal(a); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(back, fx.value) {
				t.Fatalf("round trip differs:\n%+v\n%+v", back, fx.value)
			}
			c, _ := back.Marshal()
			if !bytes.Equal(a, c) {
				t.Fatal("re-marshal differs")
			}
		})
	}
}

func TestAnyStandaloneRoundTrip(t *testing.T) {
	unk := protowire.AppendVarint(protowire.AppendTag(nil, 99, protowire.VarintType), 1)
	in := &Any{TypeURL: "type.googleapis.com/x.Y", Value: []byte{9}, Unknown: unk}
	b, err := in.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var out Any
	if err := out.Unmarshal(b); err != nil || !reflect.DeepEqual(&out, in) {
		t.Fatalf("round trip: %+v %v", out, err)
	}
}

func TestEmptyMessagePresence(t *testing.T) {
	o := &Observation{Subject: &Subject{}, JSONPayload: []byte{}}
	b, err := o.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var back Observation
	if err := back.Unmarshal(b); err != nil {
		t.Fatal(err)
	}
	if back.Subject == nil || back.JSONPayload == nil || len(back.JSONPayload) != 0 {
		t.Fatalf("presence lost: %+v", back)
	}
	if !reflect.DeepEqual(&back, o) {
		t.Fatalf("differs: %+v vs %+v", back, o)
	}
}

// A-2: unknown fields survive Unmarshal->Marshal byte-for-byte, including
// nested messages and an unknown body number.
func TestUnknownFieldsPreserved(t *testing.T) {
	unkVarint := protowire.AppendVarint(protowire.AppendTag(nil, 99, protowire.VarintType), 7)
	unkBytes := protowire.AppendBytes(protowire.AppendTag(nil, 98, protowire.BytesType), []byte("future"))
	var group []byte
	group = protowire.AppendTag(group, 97, protowire.StartGroupType)
	group = protowire.AppendVarint(protowire.AppendTag(group, 1, protowire.VarintType), 5)
	group = protowire.AppendTag(group, 97, protowire.EndGroupType)
	fixed := protowire.AppendFixed32(protowire.AppendTag(nil, 96, protowire.Fixed32Type), 1)
	fixed64 := protowire.AppendFixed64(protowire.AppendTag(nil, 95, protowire.Fixed64Type), 1)

	rec := fixRecordStart()
	rec.Start.At.Unknown = unkVarint
	rec.Start.Links[0].Unknown = unkBytes
	rec.Start.Unknown = append(append([]byte{}, group...), fixed...)
	rec.Unknown = fixed64
	// An unknown body number (a future record type) next to the envelope.
	rec.Start = nil
	rec.Type = 9
	rec.Unknown = append(protowire.AppendBytes(protowire.AppendTag(nil, 14, protowire.BytesType), []byte("future-body")), fixed64...)
	b, err := rec.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var back JournalRecord
	if err := back.Unmarshal(b); err != nil {
		t.Fatal(err)
	}
	if back.BodyType() != RecordTypeUnspecified || len(back.Unknown) == 0 {
		t.Fatalf("unknown body must leave members nil and be preserved: %+v", back)
	}
	if err := back.Validate(); err != nil {
		t.Fatalf("unknown record type is valid: %v", err)
	}
	again, err := back.Marshal()
	if err != nil || !bytes.Equal(again, b) {
		t.Fatalf("unknown body not preserved byte-for-byte: %x vs %x (%v)", again, b, err)
	}

	// Nested unknowns (including a group) in a known body.
	rec2 := fixRecordStart()
	rec2.Start.At.Unknown = unkVarint
	rec2.Start.Links[0].Unknown = unkBytes
	rec2.Start.Unknown = append(append([]byte{}, group...), fixed...)
	b2, err := rec2.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var back2 JournalRecord
	if err := back2.Unmarshal(b2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back2.Start.At.Unknown, unkVarint) || !bytes.Equal(back2.Start.Links[0].Unknown, unkBytes) {
		t.Fatalf("nested unknown lost: %+v", back2.Start)
	}
	again2, err := back2.Marshal()
	if err != nil || !bytes.Equal(again2, b2) {
		t.Fatalf("nested unknowns not preserved: %v", err)
	}
}

// Fields carried with an unexpected wire type are unknown, never errors.
func TestWrongWireTypeIsUnknown(t *testing.T) {
	for _, fx := range allFixtures() {
		for _, num := range []protowire.Number{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 20, 21} {
			in := protowire.AppendFixed32(protowire.AppendTag(nil, num, protowire.Fixed32Type), 0xdeadbeef)
			m := fresh(fx.value)
			if err := m.Unmarshal(in); err != nil {
				t.Fatalf("%s field %d: %v", fx.name, num, err)
			}
			out, err := m.Marshal()
			if err != nil || !bytes.Equal(out, in) {
				t.Fatalf("%s field %d: wrong-type field not preserved: %x %v", fx.name, num, out, err)
			}
		}
	}
}

func TestLateOneofMemberWins(t *testing.T) {
	start, _ := fixStart().Marshal()
	end, _ := fixEnd().Marshal()
	in := protowire.AppendBytes(protowire.AppendTag(nil, 10, protowire.BytesType), start)
	in = protowire.AppendBytes(protowire.AppendTag(in, 11, protowire.BytesType), end)
	var r JournalRecord
	if err := r.Unmarshal(in); err != nil {
		t.Fatal(err)
	}
	if r.Start != nil || r.End == nil {
		t.Fatalf("later member must win: %+v", r)
	}

	js := protowire.AppendBytes(protowire.AppendTag(nil, 20, protowire.BytesType), []byte("{}"))
	pp := protowire.AppendBytes(protowire.AppendTag(nil, 21, protowire.BytesType), nil)
	var o Observation
	if err := o.Unmarshal(append(append([]byte{}, js...), pp...)); err != nil {
		t.Fatal(err)
	}
	if o.JSONPayload != nil || o.ProtoPayload == nil {
		t.Fatalf("proto must win: %+v", o)
	}
	if err := o.Unmarshal(append(append([]byte{}, pp...), js...)); err != nil {
		t.Fatal(err)
	}
	if o.JSONPayload == nil || o.ProtoPayload != nil {
		t.Fatalf("json must win: %+v", o)
	}
}

// A-5: malformed input yields typed errors.
func TestMalformedInput(t *testing.T) {
	cases := map[string][]byte{
		"truncated tag":          {0x80},
		"zero field number":      {0x00, 0x00},
		"truncated varint":       {0x08, 0x80},
		"overlong varint":        append([]byte{0x08}, bytes.Repeat([]byte{0xff}, 11)...),
		"truncated length":       {0x0a, 0x05, 'a'},
		"length varint overflow": append([]byte{0x0a}, bytes.Repeat([]byte{0xff}, 11)...),
		"unterminated group":     protowire.AppendTag(nil, 9, protowire.StartGroupType),
	}
	for name, in := range cases {
		for _, fx := range allFixtures() {
			if err := fresh(fx.value).Unmarshal(in); !errors.Is(err, ErrMalformed) {
				t.Fatalf("%s on %s: got %v want ErrMalformed", name, fx.name, err)
			}
		}
	}
	// Errors from nested messages propagate through the parent.
	inner := []byte{0x08, 0x80}
	parent := protowire.AppendBytes(protowire.AppendTag(nil, 2, protowire.BytesType), inner)
	var o Observation
	if err := o.Unmarshal(parent); !errors.Is(err, ErrMalformed) {
		t.Fatalf("nested malformed: %v", err)
	}
}

func TestInvalidUTF8(t *testing.T) {
	bad := []byte{0x0a, 0x01, 0xff}
	var l Link
	if err := l.Unmarshal(bad); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("decode: %v", err)
	}
	nested := protowire.AppendBytes(protowire.AppendTag(nil, 6, protowire.BytesType), bad)
	var o Observation
	if err := o.Unmarshal(nested); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("nested decode: %v", err)
	}
	if _, err := (&Link{Relation: "\xff"}).Marshal(); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("encode: %v", err)
	}
	obs := &Observation{Subject: &Subject{Kind: "\xff"}}
	if _, err := obs.Marshal(); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("nested encode: %v", err)
	}
}

// The depth guard cannot be reached through the schema (the deepest message,
// Stamp inside Provenance inside Observation inside JournalRecord, sits at the
// limit and has no children), so it is exercised directly.
func TestDepthGuard(t *testing.T) {
	v := protowire.AppendBytes(nil, nil)
	var dst *Stamp
	if ok, err := setPtr(&dst, protowire.BytesType, v, maxDepth); !ok || !errors.Is(err, ErrMalformed) || dst != nil {
		t.Fatalf("depth %d must be rejected: ok=%v err=%v", maxDepth, ok, err)
	}
	if ok, err := setPtr(&dst, protowire.BytesType, v, maxDepth-1); !ok || err != nil || dst == nil {
		t.Fatalf("depth %d must be accepted: ok=%v err=%v", maxDepth-1, ok, err)
	}
	// The deepest schema path (record -> observation -> provenance -> stamp) works.
	rec := fixRecordObservation()
	b, _ := rec.Marshal()
	var back JournalRecord
	if err := back.Unmarshal(b); err != nil || back.Observation.Provenance.ObservedAt == nil {
		t.Fatalf("deepest path: %v", err)
	}
}

func TestValidate(t *testing.T) {
	mut := func(f func(*JournalRecord)) *JournalRecord {
		r := fixRecordStart()
		f(r)
		return r
	}
	bad := map[string]*JournalRecord{
		"schema version 0":     mut(func(r *JournalRecord) { r.SchemaVersion = 0 }),
		"missing event id":     mut(func(r *JournalRecord) { r.EventID = "" }),
		"missing node id":      mut(func(r *JournalRecord) { r.NodeID = "" }),
		"missing runtime id":   mut(func(r *JournalRecord) { r.RuntimeID = "" }),
		"missing stream id":    mut(func(r *JournalRecord) { r.StreamID = "" }),
		"sequence 0":           mut(func(r *JournalRecord) { r.StreamSequence = 0 }),
		"two bodies":           mut(func(r *JournalRecord) { r.End = fixEnd() }),
		"type/body mismatch":   mut(func(r *JournalRecord) { r.Type = RecordTypeOperationEnd }),
		"known type, no body":  mut(func(r *JournalRecord) { r.Start = nil }),
		"health type, no body": mut(func(r *JournalRecord) { r.Start = nil; r.Type = RecordTypeJournalHealth }),
	}
	for name, r := range bad {
		if err := r.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: got %v want ErrInvalid", name, err)
		}
	}
	good := []*JournalRecord{
		fixRecordStart(), fixRecordEnd(), fixRecordObservation(), fixRecordHealth(),
		mut(func(r *JournalRecord) { r.Start = nil; r.Type = 99 }),
		mut(func(r *JournalRecord) { r.Start = nil; r.Type = RecordTypeUnspecified }),
	}
	for i, r := range good {
		if err := r.Validate(); err != nil {
			t.Fatalf("good[%d]: %v", i, err)
		}
	}
}

func TestBodyType(t *testing.T) {
	want := map[RecordType]*JournalRecord{
		RecordTypeOperationStart: fixRecordStart(), RecordTypeOperationEnd: fixRecordEnd(),
		RecordTypeObservation: fixRecordObservation(), RecordTypeJournalHealth: fixRecordHealth(),
		RecordTypeUnspecified: {},
	}
	for typ, r := range want {
		if got := r.BodyType(); got != typ {
			t.Fatalf("BodyType = %d want %d", got, typ)
		}
	}
}

func TestUnmarshalResetsReceiver(t *testing.T) {
	r := fixRecordStart()
	if err := r.Unmarshal(nil); err != nil || !reflect.DeepEqual(r, &JournalRecord{}) {
		t.Fatalf("Unmarshal must reset: %+v %v", r, err)
	}
}

func TestRepeatedFieldCaps(t *testing.T) {
	repeat := func(field protowire.Number, n int) []byte {
		var b []byte
		for i := 0; i < n; i++ {
			b = protowire.AppendBytes(protowire.AppendTag(b, field, protowire.BytesType), nil)
		}
		return b
	}
	cases := []struct {
		name  string
		field protowire.Number
		max   int
		fresh func() interface{ Unmarshal([]byte) error }
		build func(n int) interface{ Marshal() ([]byte, error) }
	}{
		{"start links", 4, MaxLinks, func() interface{ Unmarshal([]byte) error } { return &OperationStart{} },
			func(n int) interface{ Marshal() ([]byte, error) } { return &OperationStart{Links: make([]Link, n)} }},
		{"start artifacts", 12, MaxArtifacts, func() interface{ Unmarshal([]byte) error } { return &OperationStart{} },
			func(n int) interface{ Marshal() ([]byte, error) } {
				return &OperationStart{Artifacts: make([]ArtifactRef, n)}
			}},
		{"end artifacts", 8, MaxArtifacts, func() interface{ Unmarshal([]byte) error } { return &OperationEnd{} },
			func(n int) interface{ Marshal() ([]byte, error) } {
				return &OperationEnd{Artifacts: make([]ArtifactRef, n)}
			}},
		{"observation artifacts", 9, MaxArtifacts, func() interface{ Unmarshal([]byte) error } { return &Observation{} },
			func(n int) interface{ Marshal() ([]byte, error) } {
				return &Observation{Artifacts: make([]ArtifactRef, n)}
			}},
		{"observation links", 10, MaxLinks, func() interface{ Unmarshal([]byte) error } { return &Observation{} },
			func(n int) interface{ Marshal() ([]byte, error) } { return &Observation{Links: make([]Link, n)} }},
		{"health attempts", 8, MaxAttempts, func() interface{ Unmarshal([]byte) error } { return &JournalHealth{} },
			func(n int) interface{ Marshal() ([]byte, error) } {
				return &JournalHealth{Attempts: make([]PathAttempt, n)}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.fresh().Unmarshal(repeat(c.field, c.max)); err != nil {
				t.Fatalf("at the cap: %v", err)
			}
			if err := c.fresh().Unmarshal(repeat(c.field, c.max+1)); !errors.Is(err, ErrMalformed) {
				t.Fatalf("over the cap on decode: %v", err)
			}
			// A hostile ~1 MiB record of empty entries is rejected early.
			if err := c.fresh().Unmarshal(repeat(c.field, 1<<19)); !errors.Is(err, ErrMalformed) {
				t.Fatalf("flood: %v", err)
			}
			if _, err := c.build(c.max).Marshal(); err != nil {
				t.Fatalf("encode at the cap: %v", err)
			}
			if _, err := c.build(c.max + 1).Marshal(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("over the cap on encode: %v", err)
			}
		})
	}
	// A wrong wire type is an unknown field, not a repeated entry.
	var s OperationStart
	if err := s.Unmarshal(protowire.AppendVarint(protowire.AppendTag(nil, 4, protowire.VarintType), 1)); err != nil || len(s.Links) != 0 {
		t.Fatalf("wrong wire type: %v %+v", err, s)
	}
}

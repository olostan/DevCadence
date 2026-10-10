// Package wire defines the on-disk record schema of the DevCadence diagnostic
// flight recorder and a hand-written protobuf wire-format codec for it.
//
// journal.proto in this directory is the normative schema (package
// devcadence.trace.v1). The codec is written over
// google.golang.org/protobuf/encoding/protowire instead of generated code so
// that no generated file enters the repository (ADR-0026, EWP WP-TRACE-1
// decision D-CODEC). The bytes produced are ordinary protobuf wire format and
// can be decoded by any protobuf implementation; drift between journal.proto,
// the codec and a reference descriptor is prevented by tests.
//
// Codec rules:
//
//   - Marshal is deterministic: known fields in ascending field-number order,
//     proto3 zero values omitted, a set oneof member always emitted, repeated
//     fields in order, then the preserved unknown fields.
//   - Unmarshal is tolerant: unknown fields, and fields with an unexpected wire
//     type, are preserved in Unknown and re-emitted verbatim by Marshal. A
//     later oneof member overrides an earlier one.
//   - Malformed varints or lengths yield ErrMalformed; invalid UTF-8 in a string
//     field yields ErrInvalidUTF8. Decoded nesting deeper than three levels is
//     rejected with ErrMalformed.
//   - JournalRecord.Validate checks envelope semantics separately from decoding.
//
// Reserved evolution rules: field numbers are never reused, enums only append
// values, new record types use body numbers 14 and above, and readers must
// tolerate unknown enum numbers, fields and record types.
package wire

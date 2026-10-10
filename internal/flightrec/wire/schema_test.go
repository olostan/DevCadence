package wire

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	_ "google.golang.org/protobuf/types/known/anypb" // registers google/protobuf/any.proto
)

// The reference schema table. A-3 builds a descriptor from it and cross-checks
// the hand-written codec against dynamicpb; A-4 compares it with the text of
// journal.proto, so changing the codec, the table or the .proto alone fails.

type fdef struct {
	name     string
	num      int32
	typ      string // scalar name, "enum:<Name>" or "msg:<Name>"
	repeated bool
	oneof    string
}

type mdef struct {
	name   string
	fields []fdef
}

const pkg = "devcadence.trace.v1"

var enumTable = map[string][]string{
	"RecordType":      {"RECORD_TYPE_UNSPECIFIED=0", "OPERATION_START=1", "OPERATION_END=2", "OBSERVATION=3", "JOURNAL_HEALTH=4"},
	"Durability":      {"DURABILITY_UNSPECIFIED=0", "DURABILITY_CRITICAL=1", "DURABILITY_DIAGNOSTIC=2"},
	"Outcome":         {"OUTCOME_UNSPECIFIED=0", "OUTCOME_COMPLETED=1", "OUTCOME_FAILED=2", "OUTCOME_CANCELLED=3", "OUTCOME_PANIC=4"},
	"ObservationKind": {"OBSERVATION_KIND_UNSPECIFIED=0", "PROGRESS=1", "DECISION=2", "FACT=3", "AUDIT=4"},
	"EvidenceState":   {"EVIDENCE_STATE_UNSPECIFIED=0", "OBSERVED=1", "DOCUMENTED=2", "INFERRED=3", "CONFIRMED=4", "MISSING=5", "UNKNOWN=6"},
	"HealthKind": {
		"HEALTH_KIND_UNSPECIFIED=0", "STREAM_STARTED=1", "SEGMENT_ROTATED=2", "DROPPED_DIAGNOSTICS=3", "WRITER_ERROR=4",
		"RECOVERY_REOPEN=5", "PATH_FALLBACK=6", "DEGRADED_NOOP=7", "NODE_ID_EPHEMERAL=8",
	},
}

func f(name string, num int32, typ string) fdef { return fdef{name: name, num: num, typ: typ} }
func rep(name string, num int32, typ string) fdef {
	return fdef{name: name, num: num, typ: typ, repeated: true}
}
func one(oneof, name string, num int32, typ string) fdef {
	return fdef{name: name, num: num, typ: typ, oneof: oneof}
}

var msgTable = []mdef{
	{"Stamp", []fdef{f("wall_unix_nanos", 1, "int64"), f("mono_nanos", 2, "int64")}},
	{"ArtifactRef", []fdef{
		f("id", 1, "string"), f("kind", 2, "string"), f("locator", 3, "string"), f("media_type", 4, "string"),
		f("digest", 5, "string"), f("size_bytes", 6, "int64"), f("truncated", 7, "bool"), f("digest_verified", 8, "bool"),
	}},
	{"Link", []fdef{f("relation", 1, "string"), f("trace_id", 2, "string"), f("operation_id", 3, "string"), f("event_id", 4, "string")}},
	{"Subject", []fdef{f("kind", 1, "string"), f("id", 2, "string")}},
	{"Provenance", []fdef{
		f("source_kind", 1, "string"), f("source_ref", 2, "string"), f("observed_by", 3, "string"), f("method", 4, "string"),
		f("observed_at", 5, "msg:Stamp"),
	}},
	{"Sanitization", []fdef{f("redactions", 1, "uint32"), f("truncations", 2, "uint32"), f("payload_replaced", 3, "bool")}},
	{"Usage", []fdef{f("input_tokens", 1, "uint64"), f("output_tokens", 2, "uint64"), f("duration_nanos", 3, "uint64")}},
	{"PathAttempt", []fdef{f("source", 1, "string"), f("path", 2, "string"), f("error_code", 3, "string")}},
	{"JournalRecord", []fdef{
		f("schema_version", 1, "uint32"), f("event_id", 2, "string"), f("node_id", 3, "string"), f("runtime_id", 4, "string"),
		f("stream_sequence", 5, "uint64"), f("type", 6, "enum:RecordType"), f("durability", 7, "enum:Durability"),
		f("stream_id", 8, "string"),
		one("body", "start", 10, "msg:OperationStart"), one("body", "end", 11, "msg:OperationEnd"),
		one("body", "observation", 12, "msg:Observation"), one("body", "health", 13, "msg:JournalHealth"),
	}},
	{"OperationStart", []fdef{
		f("trace_id", 1, "string"), f("operation_id", 2, "string"), f("parent_operation_id", 3, "string"),
		rep("links", 4, "msg:Link"), f("operation_name", 5, "string"), f("at", 6, "msg:Stamp"), f("json_metadata", 7, "bytes"),
		f("actor_id", 8, "string"), f("task_id", 9, "string"), f("attempt_id", 10, "string"), f("canonical_event_id", 11, "string"),
		rep("artifacts", 12, "msg:ArtifactRef"), f("sanitization", 13, "msg:Sanitization"),
	}},
	{"OperationEnd", []fdef{
		f("operation_id", 1, "string"), f("at", 2, "msg:Stamp"), f("outcome", 3, "enum:Outcome"), f("error_code", 4, "string"),
		f("error_summary", 5, "string"), f("json_result", 6, "bytes"), f("usage", 7, "msg:Usage"),
		rep("artifacts", 8, "msg:ArtifactRef"), f("sanitization", 9, "msg:Sanitization"),
	}},
	{"Observation", []fdef{
		f("operation_id", 1, "string"), f("at", 2, "msg:Stamp"), f("name", 3, "string"), f("kind", 4, "enum:ObservationKind"),
		f("reason_code", 5, "string"), f("subject", 6, "msg:Subject"), f("provenance", 7, "msg:Provenance"),
		f("evidence", 8, "enum:EvidenceState"), rep("artifacts", 9, "msg:ArtifactRef"), rep("links", 10, "msg:Link"),
		f("sanitization", 11, "msg:Sanitization"),
		one("payload", "json_payload", 20, "bytes"), one("payload", "proto_payload", 21, "msg:google.protobuf.Any"),
	}},
	{"JournalHealth", []fdef{
		f("kind", 1, "enum:HealthKind"), f("at", 2, "msg:Stamp"), f("dropped_count", 3, "uint64"),
		f("first_missing_sequence", 4, "uint64"), f("last_missing_sequence", 5, "uint64"), f("detail_code", 6, "string"),
		f("detail", 7, "string"), rep("attempts", 8, "msg:PathAttempt"), f("selected_source", 9, "string"), f("offset", 10, "int64"),
	}},
	{"SegmentHeader", []fdef{
		f("schema_version", 1, "uint32"), f("node_id", 2, "string"), f("runtime_id", 3, "string"), f("stream_id", 4, "string"),
		f("segment_index", 5, "uint32"), f("first_sequence", 6, "uint64"), f("created_at", 7, "msg:Stamp"),
		f("writer_pid", 8, "uint32"), f("writer_version", 9, "string"), f("max_record_bytes", 10, "uint32"),
		f("mono_origin_wall_unix_nanos", 11, "int64"),
	}},
}

var scalarTypes = map[string]descriptorpb.FieldDescriptorProto_Type{
	"int64":  descriptorpb.FieldDescriptorProto_TYPE_INT64,
	"uint32": descriptorpb.FieldDescriptorProto_TYPE_UINT32,
	"uint64": descriptorpb.FieldDescriptorProto_TYPE_UINT64,
	"bool":   descriptorpb.FieldDescriptorProto_TYPE_BOOL,
	"string": descriptorpb.FieldDescriptorProto_TYPE_STRING,
	"bytes":  descriptorpb.FieldDescriptorProto_TYPE_BYTES,
}

func qualify(name string) string {
	if strings.Contains(name, ".") {
		return "." + name
	}
	return "." + pkg + "." + name
}

func buildFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	fdp := &descriptorpb.FileDescriptorProto{
		Name: proto.String("journal.proto"), Package: proto.String(pkg), Syntax: proto.String("proto3"),
		Dependency: []string{"google/protobuf/any.proto"},
	}
	enumNames := make([]string, 0, len(enumTable))
	for n := range enumTable {
		enumNames = append(enumNames, n)
	}
	sort.Strings(enumNames)
	for _, n := range enumNames {
		ed := &descriptorpb.EnumDescriptorProto{Name: proto.String(n)}
		for _, v := range enumTable[n] {
			name, num, _ := strings.Cut(v, "=")
			x, err := strconv.Atoi(num)
			if err != nil {
				t.Fatal(err)
			}
			ed.Value = append(ed.Value, &descriptorpb.EnumValueDescriptorProto{Name: proto.String(name), Number: proto.Int32(int32(x))})
		}
		fdp.EnumType = append(fdp.EnumType, ed)
	}
	for _, m := range msgTable {
		md := &descriptorpb.DescriptorProto{Name: proto.String(m.name)}
		oneofIdx := map[string]int32{}
		for _, fd := range m.fields {
			p := &descriptorpb.FieldDescriptorProto{
				Name: proto.String(fd.name), Number: proto.Int32(fd.num),
				Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			}
			if fd.repeated {
				p.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
			}
			switch {
			case strings.HasPrefix(fd.typ, "enum:"):
				p.Type = descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum()
				p.TypeName = proto.String(qualify(strings.TrimPrefix(fd.typ, "enum:")))
			case strings.HasPrefix(fd.typ, "msg:"):
				p.Type = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
				p.TypeName = proto.String(qualify(strings.TrimPrefix(fd.typ, "msg:")))
			default:
				p.Type = scalarTypes[fd.typ].Enum()
			}
			if fd.oneof != "" {
				idx, ok := oneofIdx[fd.oneof]
				if !ok {
					idx = int32(len(md.OneofDecl))
					oneofIdx[fd.oneof] = idx
					md.OneofDecl = append(md.OneofDecl, &descriptorpb.OneofDescriptorProto{Name: proto.String(fd.oneof)})
				}
				p.OneofIndex = proto.Int32(idx)
			}
			md.Field = append(md.Field, p)
		}
		fdp.MessageType = append(fdp.MessageType, md)
	}
	file, err := protodesc.NewFile(fdp, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("build descriptor: %v", err)
	}
	return file
}

// A-3: the hand-written codec and an independent protobuf implementation agree
// in both directions for every message and field type.
func TestCrossCodecWithDynamicPB(t *testing.T) {
	file := buildFile(t)
	for _, fx := range allFixtures() {
		t.Run(fx.name+"/"+fx.variant, func(t *testing.T) {
			md := file.Messages().ByName(protoreflect.Name(fx.name))
			ours, err := fx.value.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			dm := dynamicpb.NewMessage(md)
			if err := proto.Unmarshal(ours, dm); err != nil {
				t.Fatalf("dynamicpb rejects our bytes: %v", err)
			}
			if len(dm.GetUnknown()) != 0 {
				t.Fatalf("dynamicpb left unknown fields: %x", dm.GetUnknown())
			}
			// Every populated schema field (one member per oneof) must be set.
			want := 0
			seenOneof := map[string]bool{}
			for _, m := range msgTable {
				if m.name != fx.name {
					continue
				}
				for _, fd := range m.fields {
					if fd.oneof == "" {
						want++
					} else if !seenOneof[fd.oneof] {
						seenOneof[fd.oneof] = true
						want++
					}
				}
			}
			got := 0
			dm.Range(func(protoreflect.FieldDescriptor, protoreflect.Value) bool { got++; return true })
			if got != want {
				t.Fatalf("populated fields: got %d want %d (fixture must set every field)", got, want)
			}
			theirs, err := proto.MarshalOptions{Deterministic: true}.Marshal(dm)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(ours, theirs) {
				t.Fatalf("byte mismatch\nours   %x\ntheirs %x", ours, theirs)
			}
			back := fresh(fx.value)
			if err := back.Unmarshal(theirs); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(back, fx.value) {
				t.Fatalf("decode of reference bytes differs:\n%+v\n%+v", back, fx.value)
			}
		})
	}
}

// ---- A-4: journal.proto text parity ----

var (
	commentRe = regexp.MustCompile(`(?m)//.*$|/\*.*?\*/`)
	fieldRe   = regexp.MustCompile(`(repeated\s+)?([A-Za-z0-9_.]+)\s+([a-z0-9_]+)\s*=\s*(\d+)\s*;`)
	enumRe    = regexp.MustCompile(`([A-Z0-9_]+)\s*=\s*(\d+)\s*;`)
	oneofRe   = regexp.MustCompile(`oneof\s+(\w+)\s*\{([^}]*)\}`)
)

// block returns the body of the brace block following header, plus the rest.
func blocks(src, keyword string) map[string]string {
	out := map[string]string{}
	re := regexp.MustCompile(`(?m)^` + keyword + `\s+(\w+)\s*\{`)
	for _, loc := range re.FindAllStringSubmatchIndex(src, -1) {
		name := src[loc[2]:loc[3]]
		depth, i := 1, loc[1]
		for ; depth > 0; i++ {
			switch src[i] {
			case '{':
				depth++
			case '}':
				depth--
			}
		}
		out[name] = src[loc[1] : i-1]
	}
	return out
}

func normType(t string) string {
	if _, ok := scalarTypes[t]; ok {
		return t
	}
	if _, ok := enumTable[t]; ok {
		return "enum:" + t
	}
	return "msg:" + t
}

func TestProtoTextMatchesDescriptorTable(t *testing.T) {
	raw, err := os.ReadFile("journal.proto")
	if err != nil {
		t.Fatal(err)
	}
	src := commentRe.ReplaceAllString(string(raw), "")
	if !strings.Contains(src, `package `+pkg+`;`) || !strings.Contains(src, `syntax = "proto3";`) {
		t.Fatal("package or syntax statement differs from the table")
	}

	gotEnums := map[string][]string{}
	for name, body := range blocks(src, "enum") {
		for _, m := range enumRe.FindAllStringSubmatch(body, -1) {
			gotEnums[name] = append(gotEnums[name], m[1]+"="+m[2])
		}
	}
	if !reflect.DeepEqual(gotEnums, enumTable) {
		t.Fatalf("enums differ:\nproto %v\ntable %v", gotEnums, enumTable)
	}

	gotMsgs := map[string][]string{}
	for name, body := range blocks(src, "message") {
		var lines []string
		for _, o := range oneofRe.FindAllStringSubmatch(body, -1) {
			for _, m := range fieldRe.FindAllStringSubmatch(o[2], -1) {
				lines = append(lines, fmt.Sprintf("%s=%s:%s:oneof=%s", m[3], m[4], normType(m[2]), o[1]))
			}
		}
		for _, m := range fieldRe.FindAllStringSubmatch(oneofRe.ReplaceAllString(body, ""), -1) {
			lines = append(lines, fmt.Sprintf("%s=%s:%s:rep=%v", m[3], m[4], normType(m[2]), m[1] != ""))
		}
		sort.Strings(lines)
		gotMsgs[name] = lines
	}
	wantMsgs := map[string][]string{}
	for _, m := range msgTable {
		var lines []string
		for _, fd := range m.fields {
			if fd.oneof != "" {
				lines = append(lines, fmt.Sprintf("%s=%d:%s:oneof=%s", fd.name, fd.num, fd.typ, fd.oneof))
				continue
			}
			lines = append(lines, fmt.Sprintf("%s=%d:%s:rep=%v", fd.name, fd.num, fd.typ, fd.repeated))
		}
		sort.Strings(lines)
		wantMsgs[m.name] = lines
	}
	if !reflect.DeepEqual(gotMsgs, wantMsgs) {
		for name := range wantMsgs {
			if !reflect.DeepEqual(gotMsgs[name], wantMsgs[name]) {
				t.Errorf("message %s differs:\nproto %v\ntable %v", name, gotMsgs[name], wantMsgs[name])
			}
		}
		t.Fatalf("message sets differ (proto has %d, table %d)", len(gotMsgs), len(wantMsgs))
	}
}

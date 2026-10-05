package principal_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/state"
)

const (
	digest = "sha256:abababababababababababababababababababababababababababababababab"
	commit = "0123456789abcdef0123456789abcdef01234567"
)

const fixtureDir = "../../fixtures/protocol"

func validMeta() principal.CallMeta {
	return principal.CallMeta{
		SchemaVersion: "1.0", ProjectID: "example",
		ExpectedStateRevision: "ps_000000012", CorrelationID: "corr-1",
	}
}

func validRef() principal.WorkPackageRef {
	return principal.WorkPackageRef{ID: "wp_1", Version: 1, Digest: digest, BaseCommit: commit}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestA1_MalformedWireDocumentsAreRefusedBeforeAnyCallback covers every
// refusal class of scenario A1 on every wire type.
func TestA1_MalformedWireDocumentsAreRefusedBeforeAnyCallback(t *testing.T) {
	meta := string(mustJSON(t, validMeta()))
	cases := []struct {
		name     string
		document string
		decode   func([]byte) error
		category errs.Category
	}{
		{"trailing JSON", meta + ` {}`, func(b []byte) error { _, err := principal.DecodeCallMeta(b); return err }, errs.CategoryInvalidArgument},
		{"trailing garbage", meta + `x`, func(b []byte) error { _, err := principal.DecodeCallMeta(b); return err }, errs.CategoryInvalidArgument},
		{"unknown key", strings.Replace(meta, `"project_id"`, `"extra":1,"project_id"`, 1), func(b []byte) error { _, err := principal.DecodeCallMeta(b); return err }, errs.CategoryInvalidArgument},
		{"explicit null", strings.Replace(meta, `"ps_000000012"`, `null`, 1), func(b []byte) error { _, err := principal.DecodeCallMeta(b); return err }, errs.CategoryInvalidArgument},
		{"schema version", strings.Replace(meta, `"1.0"`, `"1.1"`, 1), func(b []byte) error { _, err := principal.DecodeCallMeta(b); return err }, errs.CategorySchemaVersionUnsupported},
		{"missing correlation", `{"schema_version":"1.0","project_id":"example"}`, func(b []byte) error { _, err := principal.DecodeCallMeta(b); return err }, errs.CategoryInvalidArgument},
		{"not an object", `[]`, func(b []byte) error { _, err := principal.DecodeCallMeta(b); return err }, errs.CategoryInvalidArgument},
		{"bad digest", `{"id":"wp","version":1,"digest":"sha256:ABC","base_commit":"` + commit + `"}`, func(b []byte) error { _, err := principal.DecodeWorkPackageRef(b); return err }, errs.CategoryInvalidArgument},
		{"zero version", `{"id":"wp","version":0,"digest":"` + digest + `","base_commit":"` + commit + `"}`, func(b []byte) error { _, err := principal.DecodeWorkPackageRef(b); return err }, errs.CategoryInvalidArgument},
		{"short commit", `{"task_id":"t","attempt_id":"a","work_package":` + string(mustJSON(t, validRef())) + `,"commit":"cafe"}`, func(b []byte) error { _, err := principal.DecodeCandidateRef(b); return err }, errs.CategoryInvalidArgument},
		{"bad enum kind", `{"id":"o","instance_id":"i","kind":"execute","status":"queued"}`, func(b []byte) error { _, err := principal.DecodeOperationRef(b); return err }, errs.CategoryInvalidArgument},
		{"bad enum status", `{"id":"o","instance_id":"i","kind":"review","status":"paused"}`, func(b []byte) error { _, err := principal.DecodeOperationRef(b); return err }, errs.CategoryInvalidArgument},
		{"bad error code", `{"code":"BOOM","message":"x","evidence_refs":[],"retryable":false}`, func(b []byte) error { _, err := principal.DecodeSemanticError(b); return err }, errs.CategoryInvalidArgument},
		{"free-text error message", `{"code":"INTERNAL","message":"sqlite: database is locked","evidence_refs":[],"retryable":false}`, func(b []byte) error { _, err := principal.DecodeSemanticError(b); return err }, errs.CategoryInvalidArgument},
		{"missing evidence array", `{"code":"INTERNAL","message":"An internal error occurred.","retryable":false}`, func(b []byte) error { _, err := principal.DecodeSemanticError(b); return err }, errs.CategoryInvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.decode([]byte(tc.document))
			if err == nil {
				t.Fatal("the document was accepted")
			}
			if got := errs.CategoryOf(err); got != tc.category {
				t.Fatalf("category = %s, want %s (%v)", got, tc.category, err)
			}
		})
	}
}

func TestValidDocumentsRoundTrip(t *testing.T) {
	m, err := principal.DecodeCallMeta(mustJSON(t, validMeta()))
	if err != nil || m != validMeta() {
		t.Fatalf("call meta: %+v, %v", m, err)
	}
	ref, err := principal.DecodeWorkPackageRef(mustJSON(t, validRef()))
	if err != nil || ref != validRef() {
		t.Fatalf("work package ref: %+v, %v", ref, err)
	}
	candidate := principal.CandidateRef{TaskID: "t", AttemptID: "a", WorkPackage: validRef(), Commit: strings.Repeat("a", 64)}
	got, err := principal.DecodeCandidateRef(mustJSON(t, candidate))
	if err != nil || got != candidate {
		t.Fatalf("candidate: %+v, %v", got, err)
	}
	op := principal.OperationRef{ID: "o", InstanceID: "i", Kind: principal.KindReviewSpecification, Status: principal.StatusLost}
	gotOp, err := principal.DecodeOperationRef(mustJSON(t, op))
	if err != nil || gotOp != op {
		t.Fatalf("operation: %+v, %v", gotOp, err)
	}
	semantic := principal.NewSemanticError(principal.CodeStaleWorkPackage, nil, false)
	encoded := mustJSON(t, semantic)
	if !strings.Contains(string(encoded), `"evidence_refs":[]`) {
		t.Fatalf("a required array must be [] not null: %s", encoded)
	}
	gotErr, err := principal.DecodeSemanticError(encoded)
	if err != nil || gotErr.Code != semantic.Code {
		t.Fatalf("semantic error: %+v, %v", gotErr, err)
	}
}

func TestBootstrapAndMutationRevisionRules(t *testing.T) {
	m := validMeta()
	if err := m.ValidateMutation(); err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateBootstrap(); err == nil {
		t.Fatal("bootstrap accepted an expected revision")
	}
	m.ExpectedStateRevision = ""
	if err := m.ValidateMutation(); err == nil {
		t.Fatal("a mutation without an expected revision was accepted")
	}
	if err := m.ValidateBootstrap(); err != nil {
		t.Fatal(err)
	}
}

func TestIdentifierBounds(t *testing.T) {
	for name, value := range map[string]string{
		"blank": "", "spaces": "   ", "control": "a\x00b", "newline": "a\nb",
		"too long": strings.Repeat("a", principal.MaxIDBytes+1), "bad utf8": "a\xffb",
	} {
		if err := principal.ValidateID("id", value); err == nil {
			t.Errorf("%s identifier was accepted", name)
		}
	}
	if err := principal.ValidateID("id", strings.Repeat("a", principal.MaxIDBytes)); err != nil {
		t.Errorf("a 128 byte id was refused: %v", err)
	}
	// Surrounding spaces are validated, never silently trimmed.
	if err := principal.ValidateID("id", " a "); err != nil {
		t.Errorf("an id with surrounding spaces is valid and must be kept verbatim: %v", err)
	}
	if err := principal.ValidateID("id", strings.Repeat("é", 65)); err == nil {
		t.Error("the bound is in UTF-8 bytes, not runes")
	}
}

func TestStateRevisionSyntax(t *testing.T) {
	for _, bad := range []string{"", "ps_", "ps_12", "ps_00000000", "PS_000000001", "ps_00000000a", "ps_0000000010", "ps_ 00000001", "000000001", "ps_99999999999999999999"} {
		if _, err := principal.ParseStateRevision(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	for want, revision := range map[int64]string{0: "ps_000000000", 7: "ps_000000007", 999999999: "ps_999999999", 1000000000: "ps_1000000000"} {
		got, err := principal.ParseStateRevision(revision)
		if err != nil || got != want {
			t.Errorf("%s = %d, %v; want %d", revision, got, err, want)
		}
		if principal.FormatStateRevision(want) != revision || state.StateRevision(want) != revision {
			t.Errorf("format disagrees with internal/state for %d", want)
		}
	}
	if principal.EmptyStateRevision != state.StateRevision(0) {
		t.Fatal("the empty prefix identity drifted from internal/state")
	}
}

func TestCommitAndDigestFormats(t *testing.T) {
	if principal.ValidateCommit("c", strings.Repeat("a", 40)) != nil || principal.ValidateCommit("c", strings.Repeat("a", 64)) != nil {
		t.Fatal("a valid object id was refused")
	}
	for _, bad := range []string{"", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("A", 40), strings.Repeat("g", 40)} {
		if principal.ValidateCommit("c", bad) == nil {
			t.Errorf("commit %q was accepted", bad)
		}
	}
	for _, bad := range []string{"", "sha256:", "sha1:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("A", 64), "sha256:" + strings.Repeat("a", 63)} {
		if principal.ValidateDigest("d", bad) == nil {
			t.Errorf("digest %q was accepted", bad)
		}
	}
}

// TestA8_ModelSuppliedActorAndGrantsAreRefused proves a wire document cannot
// carry authority, and that the trusted CallerContext cannot be (de)serialised.
func TestA8_ModelSuppliedActorAndGrantsAreRefused(t *testing.T) {
	for _, extra := range []string{
		`"actor":{"kind":"human","id":"mallory"}`,
		`"allowed_actions":["accept"]`,
		`"principal_id":"p"`,
		`"policy_ref":"x"`,
		`"max_evidence_bytes":1`,
	} {
		document := `{"schema_version":"1.0","project_id":"example","correlation_id":"c",` + extra + `}`
		if _, err := principal.DecodeCallMeta([]byte(document)); err == nil {
			t.Errorf("a request carrying %s was accepted", extra)
		}
	}
	trusted := principal.CallerContext{PrincipalID: "p", ProjectID: "example", AllowedActions: []string{"investigate"}}
	if _, err := json.Marshal(trusted); err == nil {
		t.Error("CallerContext was serialised")
	}
	var decoded principal.CallerContext
	if err := json.Unmarshal([]byte(`{"PrincipalID":"p","AllowedActions":["accept"]}`), &decoded); err == nil {
		t.Error("CallerContext was decoded from a request")
	}
	if len(decoded.AllowedActions) != 0 {
		t.Error("a refused decode still minted grants")
	}
}

func TestCallerAuthorizationIsExactAndProjectBound(t *testing.T) {
	caller := principal.CallerContext{PrincipalID: "p", ProjectID: "example", AllowedActions: []string{"investigate"}}
	meta := validMeta()
	if err := caller.Authorize(meta, "investigate"); err != nil {
		t.Fatalf("a granted action was denied: %v", err)
	}
	for name, err := range map[string]error{
		"ungranted action": caller.Authorize(meta, "accept"),
		"prefix of grant":  caller.Authorize(meta, "invest"),
		"other project":    caller.Authorize(principal.CallMeta{SchemaVersion: "1.0", ProjectID: "other", CorrelationID: "c"}, "investigate"),
		"empty binding":    principal.CallerContext{}.Authorize(meta, "investigate"),
	} {
		if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("%s: err = %v, want policy_denied", name, err)
		}
	}
	if err := (principal.CallerContext{PrincipalID: "p", ProjectID: "x", MaxSnippetLines: -1}).Validate(); err == nil {
		t.Error("a negative limit was accepted")
	}
}

func TestSemanticErrorsCarryOnlyFixedMessages(t *testing.T) {
	if got := principal.NewSemanticError("NOT_A_CODE", nil, false); got.Code != principal.CodeInternal {
		t.Fatalf("unknown code produced %s", got.Code)
	}
	for _, code := range principal.ErrorCodes() {
		message, ok := principal.FixedMessage(code)
		if !ok || message == "" {
			t.Fatalf("code %s has no fixed message", code)
		}
		e := principal.NewSemanticError(code, []string{"ev_1"}, true)
		if err := e.Validate(); err != nil {
			t.Errorf("%s: %v", code, err)
		}
		if !strings.Contains(e.Error(), code) {
			t.Errorf("Error() omits the code: %s", e.Error())
		}
	}
	bad := principal.NewSemanticError(principal.CodeInternal, []string{"has\x00control"}, false)
	if bad.Validate() == nil {
		t.Error("a malformed evidence handle was accepted")
	}
	if len(principal.ErrorCodes()) != 17 {
		t.Errorf("the closed code set has %d members, want 17", len(principal.ErrorCodes()))
	}
}

// TestSchemasAreTheTwinOfTheGoTypes keeps the closed sets and fixed messages
// in the published schemas equal to the Go constants, and runs every fixture
// through both the schema and the strict Go decoder.
func TestSchemasAreTheTwinOfTheGoTypes(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	// Closed sets and messages: derive a document per member and require both sides to accept it.
	for _, code := range principal.ErrorCodes() {
		e := principal.NewSemanticError(code, nil, false)
		if err := set.ValidateBytes(schema.NamePrincipalSemanticError, mustJSON(t, e)); err != nil {
			t.Errorf("schema rejects %s with its fixed message: %v", code, err)
		}
		e.Message = "changed"
		if err := set.ValidateBytes(schema.NamePrincipalSemanticError, mustJSON(t, e)); err == nil {
			t.Errorf("schema accepts a free-text message for %s", code)
		}
	}
	for _, kind := range principal.OperationKinds() {
		for _, status := range principal.OperationStatuses() {
			op := principal.OperationRef{ID: "o", InstanceID: "i", Kind: kind, Status: status}
			if err := set.ValidateBytes(schema.NamePrincipalOperationRef, mustJSON(t, op)); err != nil {
				t.Errorf("schema rejects %s/%s: %v", kind, status, err)
			}
		}
	}
	decoders := map[schema.Name]func([]byte) error{
		schema.NamePrincipalCallMeta:       func(b []byte) error { _, err := principal.DecodeCallMeta(b); return err },
		schema.NamePrincipalWorkPackageRef: func(b []byte) error { _, err := principal.DecodeWorkPackageRef(b); return err },
		schema.NamePrincipalCandidateRef:   func(b []byte) error { _, err := principal.DecodeCandidateRef(b); return err },
		schema.NamePrincipalSemanticError:  func(b []byte) error { _, err := principal.DecodeSemanticError(b); return err },
		schema.NamePrincipalOperationRef:   func(b []byte) error { _, err := principal.DecodeOperationRef(b); return err },
	}
	files, err := filepath.Glob(filepath.Join(fixtureDir, "principal-*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no principal fixtures (%v)", err)
	}
	for _, file := range files {
		base := filepath.Base(file)
		name := schema.Name(base[:strings.Index(base, ".")])
		decode, ok := decoders[name]
		if !ok {
			t.Fatalf("fixture %s names no principal schema", base)
		}
		document, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		schemaErr := set.ValidateBytes(name, document)
		goErr := decode(document)
		if strings.Contains(base, ".valid") {
			if schemaErr != nil || goErr != nil {
				t.Errorf("%s: schema=%v go=%v, want both to accept", base, schemaErr, goErr)
			}
		} else if schemaErr == nil || goErr == nil {
			t.Errorf("%s: schema=%v go=%v, want both to refuse", base, schemaErr, goErr)
		}
	}
}

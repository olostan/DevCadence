package facade_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/schema"
)

// decoders maps each tool's hyphenated schema stem to its strict decoder and a
// response type the strict reader must accept.
var decoders = map[string]struct {
	request  func([]byte) error
	response func() any
}{
	"project-state": {func(b []byte) error { _, e := facade.DecodeProjectStateRequest(b); return e }, func() any { return &facade.ProjectStateResponse{} }},
	"investigate":   {func(b []byte) error { _, e := facade.DecodeInvestigateRequest(b); return e }, func() any { return &facade.InvestigateResponse{} }},
	"create-work-package": {func(b []byte) error { _, e := facade.DecodeCreateWorkPackageRequest(b); return e },
		func() any { return &facade.CreateWorkPackageResponse{} }},
	"delegate":         {func(b []byte) error { _, e := facade.DecodeDelegateRequest(b); return e }, func() any { return &facade.DelegateResponse{} }},
	"task-status":      {func(b []byte) error { _, e := facade.DecodeTaskStatusRequest(b); return e }, func() any { return &facade.TaskStatusResponse{} }},
	"validate":         {func(b []byte) error { _, e := facade.DecodeValidateRequest(b); return e }, func() any { return &facade.ValidateResponse{} }},
	"review":           {func(b []byte) error { _, e := facade.DecodeReviewRequest(b); return e }, func() any { return &facade.ReviewResponse{} }},
	"request-evidence": {func(b []byte) error { _, e := facade.DecodeRequestEvidenceRequest(b); return e }, func() any { return &facade.RequestEvidenceResponse{} }},
	"accept":           {func(b []byte) error { _, e := facade.DecodeAcceptRequest(b); return e }, func() any { return &facade.AcceptResponse{} }},
	"reject":           {func(b []byte) error { _, e := facade.DecodeRejectRequest(b); return e }, func() any { return &facade.RejectResponse{} }},
	"record-decision":  {func(b []byte) error { _, e := facade.DecodeRecordDecisionRequest(b); return e }, func() any { return &facade.RecordDecisionResponse{} }},
}

func fixtureFiles(t *testing.T, pattern string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "fixtures", "protocol", pattern))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures match %s (%v)", pattern, err)
	}
	return files
}

// A14: the Go readers and the published schemas agree on every fixture.
func TestFixturesAgreeWithTheStrictGoReaders(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	for tool, d := range decoders {
		for _, file := range fixtureFiles(t, "principal-"+tool+"-request.*.json") {
			raw, _ := os.ReadFile(file)
			readerErr := d.request(raw)
			schemaErr := set.ValidateBytes(schema.Name("principal-"+tool+"-request"), raw)
			valid := strings.Contains(filepath.Base(file), ".valid")
			if valid && (readerErr != nil || schemaErr != nil) {
				t.Errorf("%s: reader %v schema %v", filepath.Base(file), readerErr, schemaErr)
			}
			if !valid && readerErr == nil {
				t.Errorf("%s: the Go reader accepted an invalid fixture", filepath.Base(file))
			}
		}
		for _, file := range fixtureFiles(t, "principal-"+tool+"-response.valid*.json") {
			raw, _ := os.ReadFile(file)
			out := d.response()
			// Responses embed durable records, whose nullable members are a
			// documented equivalence of absence (ADR-0003); only unknown keys
			// are refused here.
			dec := json.NewDecoder(strings.NewReader(string(raw)))
			dec.DisallowUnknownFields()
			if err := dec.Decode(out); err != nil {
				t.Errorf("%s: %v", filepath.Base(file), err)
				continue
			}
			again, _ := json.Marshal(out)
			if err := set.ValidateBytes(schema.Name("principal-"+tool+"-response"), again); err != nil {
				t.Errorf("%s: re-encoded response violates the schema: %v", filepath.Base(file), err)
			}
		}
	}
}

func TestRequestsAreStrict(t *testing.T) {
	base := `{"schema_version":"1.0","project_id":"example","correlation_id":"c"}`
	for name, doc := range map[string]string{
		"actor in meta":       `{"meta":{"schema_version":"1.0","project_id":"example","correlation_id":"c","actor":"root"},"focus":"project"}`,
		"grants in meta":      `{"meta":{"schema_version":"1.0","project_id":"example","correlation_id":"c","allowed_actions":["accept"]},"focus":"project"}`,
		"top-level policy":    `{"meta":` + base + `,"policy_ref":"x"}`,
		"unknown key":         `{"meta":` + base + `,"surprise":1}`,
		"null optional":       `{"meta":` + base + `,"focus":null}`,
		"trailing data":       `{"meta":` + base + `} {}`,
		"not an object":       `[]`,
		"missing meta":        `{}`,
		"wrong schema":        `{"meta":{"schema_version":"2.0","project_id":"example","correlation_id":"c"}}`,
		"task id sans focus":  `{"meta":` + base + `,"task_id":"t"}`,
		"unknown focus":       `{"meta":` + base + `,"focus":"everything"}`,
		"bad at_revision":     `{"meta":` + base + `,"at_revision":"ps_12"}`,
		"truncated":           `{"meta":`,
		"duplicate selectors": `{"meta":` + base + `,"focus":"task"}`,
	} {
		if _, err := facade.DecodeProjectStateRequest([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := facade.DecodeProjectStateRequest([]byte(`{"meta":` + base + `}`)); err != nil {
		t.Fatalf("a minimal valid request was refused: %v", err)
	}
	huge := `{"meta":` + base + `,"focus":"` + strings.Repeat("a", facade.MaxRequestBytes) + `"}`
	if _, err := facade.DecodeProjectStateRequest([]byte(huge)); err == nil {
		t.Error("a request over 1 MiB was accepted")
	}
	mutation := `{"meta":` + base + `,"task_id":"t","work_package":{"id":"w","version":1,"digest":"sha256:` + strings.Repeat("a", 64) + `","base_commit":"abcdef0"}}`
	if _, err := facade.DecodeDelegateRequest([]byte(mutation)); err == nil {
		t.Error("a mutation without expected_state_revision was accepted")
	}
	both := `{"meta":` + base + `,"task_id":"t","operation":{"id":"o","instance_id":"i","kind":"validate","status":"running"}}`
	if _, err := facade.DecodeTaskStatusRequest([]byte(both)); err == nil {
		t.Error("task_status accepted both selectors")
	}
	neither := `{"meta":` + base + `}`
	if _, err := facade.DecodeTaskStatusRequest([]byte(neither)); err == nil {
		t.Error("task_status accepted no selector")
	}
}

func TestReviewDimensionsAndLimitsAreClosed(t *testing.T) {
	cand := `{"task_id":"t","attempt_id":"a","commit":"abcdef0","work_package":{"id":"w","version":1,"digest":"sha256:` + strings.Repeat("a", 64) + `","base_commit":"abcdef0"}}`
	meta := `{"schema_version":"1.0","project_id":"example","correlation_id":"c"}`
	review := func(dims string) error {
		_, err := facade.DecodeReviewRequest([]byte(`{"meta":` + meta + `,"candidate":` + cand + `,"dimensions":` + dims + `}`))
		return err
	}
	if err := review(`["correctness","specification"]`); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`[]`, `["style"]`, `["security","security"]`, `["correctness","contract","architecture","security","test_adequacy","maintainability","performance","specification","correctness"]`} {
		if review(bad) == nil {
			t.Errorf("dimensions %s accepted", bad)
		}
	}
	accept := func(vals, revs, reason string) error {
		_, err := facade.DecodeAcceptRequest([]byte(`{"meta":` + meta + `,"candidate":` + cand + `,"validation_ids":` + vals + `,"review_ids":` + revs + `,"reason":"` + reason + `"}`))
		return err
	}
	if accept(`["v"]`, `["r"]`, "ok") != nil || accept(`[]`, `["r"]`, "ok") == nil || accept(`["v","v"]`, `["r"]`, "ok") == nil ||
		accept(`["v"]`, `["r"]`, " ") == nil || accept(`["v"]`, `["r"]`, strings.Repeat("x", 2049)) == nil {
		t.Error("accept request limits are wrong")
	}
	reject := func(repairs string) error {
		_, err := facade.DecodeRejectRequest([]byte(`{"meta":{"schema_version":"1.0","project_id":"example","correlation_id":"c","expected_state_revision":"ps_000000001"},"candidate":` +
			cand + `,"reason":"r","requested_repairs":` + repairs + `}`))
		return err
	}
	many := "[" + strings.TrimSuffix(strings.Repeat(`"x",`, 33), ",") + "]"
	if reject(`[]`) != nil || reject(`["fix"]`) != nil || reject(many) == nil {
		t.Error("reject repair limits are wrong")
	}
}

func TestToolAndGrantVocabulary(t *testing.T) {
	if got := len(facade.ToolNames()); got != 11 {
		t.Fatalf("%d base tools", got)
	}
	if got := len(facade.DiscoveryToolNames()); got != 9 {
		t.Fatalf("%d discovery names", got)
	}
	seen := map[string]bool{}
	for _, n := range append(facade.ToolNames(), facade.DiscoveryToolNames()...) {
		if seen[n] || !facade.KnownAction(n) {
			t.Errorf("tool %s duplicated or unknown", n)
		}
		seen[n] = true
	}
	for _, alias := range []string{"discovery.write", "project_state.read", "*", "", "Accept", "list_resources"} {
		if facade.KnownAction(alias) {
			t.Errorf("%q is a grant", alias)
		}
	}
	// Each tool has a request and a response schema.
	set, _ := schema.Default()
	for _, name := range schema.PrincipalToolSchemaNames() {
		if _, err := set.Schema(name); err != nil {
			t.Error(err)
		}
	}
	if len(schema.PrincipalToolSchemaNames()) != 22 {
		t.Error("expected 22 principal tool schemas")
	}
}

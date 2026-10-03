package protocol_test

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

const recFixtureDir = "../../fixtures/protocol/"

func loadRecommendation(t *testing.T, name string) *protocol.PortfolioRecommendation {
	t.Helper()
	data, err := os.ReadFile(recFixtureDir + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rec := &protocol.PortfolioRecommendation{}
	if err := protocol.Unmarshal(data, rec); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return rec
}

func requireInvalidArgument(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryInvalidArgument {
		t.Fatalf("category: got %q, want %q", got, errs.CategoryInvalidArgument)
	}
}

func TestPortfolioRecommendationACC01LegacyRecordOmitsNewKeys(t *testing.T) {
	rec := loadRecommendation(t, "portfolio-recommendation.valid.json")
	first, err := protocol.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(first, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	for _, key := range []string{"set_id", "intent", "tradeoffs", "confidence", "planner"} {
		if _, ok := generic[key]; ok {
			t.Errorf("key %q must be omitted for a legacy record", key)
		}
	}
	again := &protocol.PortfolioRecommendation{}
	if err := protocol.Unmarshal(first, again); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	second, err := protocol.Marshal(again)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("marshal is not stable across a round trip")
	}
}

func TestPortfolioRecommendationACC02PlannerFixturePreservesFields(t *testing.T) {
	rec := loadRecommendation(t, "portfolio-recommendation.planner.valid.json")
	if rec.SetID != "set_m3d_001" || rec.Intent != protocol.IntentBalanced ||
		rec.Confidence != protocol.RecommendationConfidenceMedium ||
		len(rec.Tradeoffs) != 1 || rec.Planner == nil ||
		rec.Planner.EndpointID != "ep_claude_37_sonnet" || rec.Planner.DriverID != "drv_claude_cli" ||
		rec.Planner.ModelID != "claude-3-7-sonnet" || !strings.HasPrefix(rec.Planner.InvocationDigest, "sha256:") {
		t.Fatalf("fields not preserved: %+v", rec)
	}
	data, err := protocol.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	back := &protocol.PortfolioRecommendation{}
	if err := protocol.Unmarshal(data, back); err != nil {
		t.Fatalf("round trip decode: %v", err)
	}
	if !reflect.DeepEqual(rec, back) {
		t.Errorf("round trip lost information")
	}
}

func TestPortfolioRecommendationACC03InvalidFixturesRejectedByGoReader(t *testing.T) {
	for _, slug := range []string{
		"confidence-critical", "intent-unknown", "empty-tradeoff",
		"planner-missing-endpoint", "planner-missing-confidence", "planner-extra-field",
	} {
		t.Run(slug, func(t *testing.T) {
			data, err := os.ReadFile(recFixtureDir + "portfolio-recommendation.invalid-" + slug + ".json")
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			requireInvalidArgument(t, protocol.Unmarshal(data, &protocol.PortfolioRecommendation{}))
		})
	}
}

func TestPortfolioRecommendationACC04ConsistencyRule(t *testing.T) {
	t.Run("ACC-04a planner present requires each field", func(t *testing.T) {
		mutations := map[string]func(*protocol.PortfolioRecommendation){
			"intent":     func(r *protocol.PortfolioRecommendation) { r.Intent = "" },
			"confidence": func(r *protocol.PortfolioRecommendation) { r.Confidence = "" },
			"set_id":     func(r *protocol.PortfolioRecommendation) { r.SetID = "" },
			"tradeoffs":  func(r *protocol.PortfolioRecommendation) { r.Tradeoffs = nil },
		}
		for name, mutate := range mutations {
			t.Run(name, func(t *testing.T) {
				rec := loadRecommendation(t, "portfolio-recommendation.planner.valid.json")
				mutate(rec)
				requireInvalidArgument(t, rec.Validate())
			})
		}
	})
	t.Run("ACC-04b planner absent all optional", func(t *testing.T) {
		rec := loadRecommendation(t, "portfolio-recommendation.planner.valid.json")
		rec.Planner = nil
		rec.Intent, rec.Confidence, rec.SetID, rec.Tradeoffs = "", "", "", nil
		if err := rec.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("ACC-04c lone intent without planner is valid", func(t *testing.T) {
		rec := loadRecommendation(t, "portfolio-recommendation.planner.valid.json")
		rec.Planner = nil
		rec.Confidence, rec.SetID, rec.Tradeoffs = "", "", nil
		if err := rec.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("ACC-04d planner null decodes as absent", func(t *testing.T) {
		data, err := os.ReadFile(recFixtureDir + "portfolio-recommendation.planner.valid.json")
		if err != nil {
			t.Fatal(err)
		}
		var generic map[string]json.RawMessage
		if err := json.Unmarshal(data, &generic); err != nil {
			t.Fatal(err)
		}
		generic["planner"] = json.RawMessage("null")
		patched, err := json.Marshal(generic)
		if err != nil {
			t.Fatal(err)
		}
		rec := &protocol.PortfolioRecommendation{}
		if err := protocol.Unmarshal(patched, rec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Planner != nil {
			t.Error("planner must decode to nil")
		}
	})
	t.Run("ACC-04e empty planner object is present and invalid", func(t *testing.T) {
		rec := loadRecommendation(t, "portfolio-recommendation.planner.valid.json")
		rec.Planner = &protocol.PlannerProvenance{}
		requireInvalidArgument(t, rec.Validate())
	})
}

func TestPortfolioRecommendationACC05PlannerProvenanceTagsMatchSchema(t *testing.T) {
	data, err := os.ReadFile("../../schemas/portfolio-recommendation.schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc struct {
		Properties struct {
			Planner struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"planner"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	var schemaKeys []string
	for k := range doc.Properties.Planner.Properties {
		schemaKeys = append(schemaKeys, k)
	}
	var goKeys []string
	typ := reflect.TypeOf(protocol.PlannerProvenance{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		goKeys = append(goKeys, name)
	}
	sort.Strings(schemaKeys)
	sort.Strings(goKeys)
	if !reflect.DeepEqual(schemaKeys, goKeys) {
		t.Errorf("planner keys differ: schema %v, go %v", schemaKeys, goKeys)
	}
}

func TestPortfolioRecommendationACC06WhitespaceOnlyRejected(t *testing.T) {
	mutations := map[string]func(*protocol.PortfolioRecommendation){
		"set_id":      func(r *protocol.PortfolioRecommendation) { r.SetID = " " },
		"endpoint_id": func(r *protocol.PortfolioRecommendation) { r.Planner.EndpointID = " " },
		"tradeoff":    func(r *protocol.PortfolioRecommendation) { r.Tradeoffs = []string{"ok", " \t"} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			rec := loadRecommendation(t, "portfolio-recommendation.planner.valid.json")
			mutate(rec)
			requireInvalidArgument(t, rec.Validate())
		})
	}
}

func TestPortfolioRecommendationEnumsAndProvenanceValidation(t *testing.T) {
	for _, i := range []protocol.RecommendationIntent{
		protocol.IntentMinimumSpend, protocol.IntentBalanced,
		protocol.IntentMaximumQualityWithinPolicy, protocol.IntentPrivacyFirst,
	} {
		if !i.Valid() {
			t.Errorf("intent %q should be valid", i)
		}
	}
	if protocol.RecommendationIntent("cheapest").Valid() {
		t.Error("unknown intent must be invalid")
	}
	for _, c := range []protocol.RecommendationConfidence{
		protocol.RecommendationConfidenceHigh, protocol.RecommendationConfidenceMedium, protocol.RecommendationConfidenceLow,
	} {
		if !c.Valid() {
			t.Errorf("confidence %q should be valid", c)
		}
	}
	if protocol.RecommendationConfidence("critical").Valid() {
		t.Error("unknown confidence must be invalid")
	}
	for name, p := range map[string]protocol.PlannerProvenance{
		"endpoint": {DriverID: "d", InvocationDigest: "x"},
		"driver":   {EndpointID: "e", InvocationDigest: "x"},
		"digest":   {EndpointID: "e", DriverID: "d"},
	} {
		t.Run(name, func(t *testing.T) { requireInvalidArgument(t, p.Validate()) })
	}
	if err := (protocol.PlannerProvenance{EndpointID: "e", DriverID: "d", InvocationDigest: "not-a-sha"}).Validate(); err != nil {
		t.Errorf("model_id optional and digest format unconstrained: %v", err)
	}
	rec := loadRecommendation(t, "portfolio-recommendation.planner.valid.json")
	rec.Planner = nil
	rec.Intent = "cheapest"
	requireInvalidArgument(t, rec.Validate())
	rec.Intent = protocol.IntentBalanced
	rec.Confidence = "critical"
	requireInvalidArgument(t, rec.Validate())
}

package tests

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
)

const recommendationSchema = schema.Name("portfolio-recommendation")

func plannerBase(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(read(t, filepath.Join(fixtureDir, "portfolio-recommendation.planner.valid.json")), &doc); err != nil {
		t.Fatalf("decode base fixture: %v", err)
	}
	return doc
}

// checkBoth validates a mutated document against the schema and the Go reader.
func checkBoth(t *testing.T, doc map[string]any) (schemaErr, goErr error) {
	t.Helper()
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("compile embedded schemas: %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return set.ValidateBytes(recommendationSchema, raw), protocol.Unmarshal(raw, &protocol.PortfolioRecommendation{})
}

func TestPortfolioRecommendationSchemaRejectsPlannerConsistencyViolations(t *testing.T) {
	planner := func(d map[string]any) map[string]any { return d["planner"].(map[string]any) }
	cases := []struct {
		name       string
		mutate     func(d map[string]any)
		goAccepts  bool
		wantSchema bool // schema must reject
	}{
		{"missing intent", func(d map[string]any) { delete(d, "intent") }, false, true},
		{"missing tradeoffs", func(d map[string]any) { delete(d, "tradeoffs") }, false, true},
		{"missing set_id", func(d map[string]any) { delete(d, "set_id") }, false, true},
		{"missing confidence", func(d map[string]any) { delete(d, "confidence") }, false, true},
		{"planner missing driver_id", func(d map[string]any) { delete(planner(d), "driver_id") }, false, true},
		{"planner missing invocation_digest", func(d map[string]any) { delete(planner(d), "invocation_digest") }, false, true},
		{"planner missing endpoint_id", func(d map[string]any) { delete(planner(d), "endpoint_id") }, false, true},
		{"empty tradeoffs array", func(d map[string]any) { d["tradeoffs"] = []any{} }, false, true},
		{"empty set_id", func(d map[string]any) { d["set_id"] = "" }, false, true},
		{"empty planner.endpoint_id", func(d map[string]any) { planner(d)["endpoint_id"] = "" }, false, true},
		{"empty planner.driver_id", func(d map[string]any) { planner(d)["driver_id"] = "" }, false, true},
		{"empty planner.invocation_digest", func(d map[string]any) { planner(d)["invocation_digest"] = "" }, false, true},
		// Go decodes null as an absent pointer (INV-03 permits Go to be more lenient here);
		// the schema's type:object rejects it.
		{"planner null", func(d map[string]any) { d["planner"] = nil }, true, true},
	}
	for _, tc := range cases {
		t.Run("PortfolioRecommendation/"+tc.name, func(t *testing.T) {
			doc := plannerBase(t)
			tc.mutate(doc)
			schemaErr, goErr := checkBoth(t, doc)
			if tc.wantSchema && schemaErr == nil {
				t.Error("schema accepted a document it must reject")
			}
			if tc.goAccepts && goErr != nil {
				t.Errorf("Go reader must accept: %v", goErr)
			}
			if !tc.goAccepts && goErr == nil {
				t.Error("Go reader accepted a document it must reject")
			}
		})
	}
}

func TestPortfolioRecommendationEnumValuesPinnedInSchemaAndGo(t *testing.T) {
	intents := map[string]protocol.RecommendationIntent{
		"minimum_spend":                 protocol.IntentMinimumSpend,
		"balanced":                      protocol.IntentBalanced,
		"maximum_quality_within_policy": protocol.IntentMaximumQualityWithinPolicy,
		"privacy_first":                 protocol.IntentPrivacyFirst,
	}
	for literal, constant := range intents {
		t.Run("intent/"+literal, func(t *testing.T) {
			if string(constant) != literal {
				t.Fatalf("Go constant is %q, want %q", constant, literal)
			}
			doc := plannerBase(t)
			doc["intent"] = literal
			schemaErr, goErr := checkBoth(t, doc)
			if schemaErr != nil || goErr != nil {
				t.Errorf("schema=%v go=%v; both must accept", schemaErr, goErr)
			}
		})
	}
	confidences := map[string]protocol.RecommendationConfidence{
		"high":   protocol.RecommendationConfidenceHigh,
		"medium": protocol.RecommendationConfidenceMedium,
		"low":    protocol.RecommendationConfidenceLow,
	}
	for literal, constant := range confidences {
		t.Run("confidence/"+literal, func(t *testing.T) {
			if string(constant) != literal {
				t.Fatalf("Go constant is %q, want %q", constant, literal)
			}
			doc := plannerBase(t)
			doc["confidence"] = literal
			schemaErr, goErr := checkBoth(t, doc)
			if schemaErr != nil || goErr != nil {
				t.Errorf("schema=%v go=%v; both must accept", schemaErr, goErr)
			}
		})
	}
}

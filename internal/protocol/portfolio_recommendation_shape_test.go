package protocol_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
)

func TestPortfolioRecommendationEmptyEnumsAreNotValid(t *testing.T) {
	if protocol.RecommendationIntent("").Valid() {
		t.Error("empty intent must not be Valid()")
	}
	if protocol.RecommendationConfidence("").Valid() {
		t.Error("empty confidence must not be Valid()")
	}
}

func TestPortfolioRecommendationACC06WhitespaceOnlyDriverAndDigestRejected(t *testing.T) {
	mutations := map[string]func(*protocol.PortfolioRecommendation){
		"driver_id":         func(r *protocol.PortfolioRecommendation) { r.Planner.DriverID = " " },
		"invocation_digest": func(r *protocol.PortfolioRecommendation) { r.Planner.InvocationDigest = "\t " },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			rec := loadRecommendation(t, "portfolio-recommendation.planner.valid.json")
			mutate(rec)
			requireInvalidArgument(t, rec.Validate())
		})
	}
}

func TestPortfolioRecommendationMarshalShapeAndOrder(t *testing.T) {
	rec := loadRecommendation(t, "portfolio-recommendation.planner.valid.json")
	rec.Planner.ModelID = ""
	rec.Tradeoffs = []string{"same", "same"}
	if err := rec.Validate(); err != nil {
		t.Fatalf("duplicate tradeoffs must be accepted: %v", err)
	}
	data, err := protocol.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "model_id") {
		t.Error("empty model_id must be omitted")
	}
	prev := -1
	for _, key := range []string{`"capability_provenance"`, `"set_id"`, `"intent"`, `"tradeoffs"`, `"confidence"`, `"planner"`} {
		idx := strings.Index(text, key)
		if idx < 0 || idx <= prev {
			t.Fatalf("key %s missing or out of order (idx %d, prev %d)", key, idx, prev)
		}
		prev = idx
	}
	back := &protocol.PortfolioRecommendation{}
	if err := protocol.Unmarshal(data, back); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if !reflect.DeepEqual(rec.Tradeoffs, back.Tradeoffs) {
		t.Errorf("duplicate tradeoffs not preserved in order: %v", back.Tradeoffs)
	}
}

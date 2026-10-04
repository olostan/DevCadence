package plannerdriver

import (
	"encoding/json"

	"github.com/olostan/DevCadence/internal/protocol"
)

func alt(intent protocol.RecommendationIntent) map[string]any {
	p, _ := json.Marshal(makeTestPortfolio())
	var portfolio map[string]any
	_ = json.Unmarshal(p, &portfolio)
	return map[string]any{
		"intent":     string(intent),
		"portfolio":  portfolio,
		"rationale":  "because " + string(intent),
		"tradeoffs":  []string{"gives something up"},
		"confidence": "medium",
	}
}

// allAlts returns a valid planner output covering every recommendation intent.
func allAlts() string {
	var alts []map[string]any
	for _, i := range []protocol.RecommendationIntent{
		protocol.IntentMinimumSpend,
		protocol.IntentBalanced,
		protocol.IntentMaximumQualityWithinPolicy,
		protocol.IntentPrivacyFirst,
	} {
		alts = append(alts, alt(i))
	}
	b, _ := json.Marshal(map[string]any{"alternatives": alts})
	return string(b)
}

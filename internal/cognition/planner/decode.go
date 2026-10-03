package planner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// maxRule3Detail bounds the decoder error text copied into a rule 3 Detail.
const maxRule3Detail = 160

var canonicalIntentOrder = []protocol.RecommendationIntent{
	protocol.IntentMinimumSpend,
	protocol.IntentBalanced,
	protocol.IntentMaximumQualityWithinPolicy,
	protocol.IntentPrivacyFirst,
}

// canonicalizeIntents validates the requested intents (unknown or duplicate is
// an error) and returns them in canonical order; empty means all four.
func canonicalizeIntents(in []protocol.RecommendationIntent) ([]protocol.RecommendationIntent, error) {
	if len(in) == 0 {
		return append([]protocol.RecommendationIntent(nil), canonicalIntentOrder...), nil
	}
	seen := make(map[protocol.RecommendationIntent]bool, len(in))
	for _, i := range in {
		if !i.Valid() {
			return nil, errs.New(errs.CategoryInvalidArgument, "planner: unknown intent %q", string(i))
		}
		if seen[i] {
			return nil, errs.New(errs.CategoryInvalidArgument, "planner: duplicate intent %q", string(i))
		}
		seen[i] = true
	}
	out := make([]protocol.RecommendationIntent, 0, len(seen))
	for _, i := range canonicalIntentOrder {
		if seen[i] {
			out = append(out, i)
		}
	}
	return out, nil
}

type decodedAlternative struct {
	Intent     protocol.RecommendationIntent     `json:"intent"`
	Portfolio  protocol.CognitionPortfolio       `json:"portfolio"`
	Rationale  string                            `json:"rationale"`
	Tradeoffs  []string                          `json:"tradeoffs"`
	Confidence protocol.RecommendationConfidence `json:"confidence"`
}

type decodedEnvelope struct {
	Alternatives []json.RawMessage `json:"alternatives"`
}

// decodeOutput applies REQ-06 rules 1-6. On any violation it returns a Detail
// beginning "rule N:" and a nil map.
func decodeOutput(content string, requested []protocol.RecommendationIntent) (map[protocol.RecommendationIntent]decodedAlternative, string) {
	if len(content) > maxContentBytes {
		return nil, fmt.Sprintf("rule 1: content is %d bytes, limit is %d", len(content), maxContentBytes)
	}
	text, ok := stripFence(strings.TrimSpace(content))
	if !ok {
		return nil, "rule 2: malformed code fence"
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	var env decodedEnvelope
	if err := dec.Decode(&env); err != nil {
		return nil, "rule 3: " + truncateUTF8(err.Error(), maxRule3Detail)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, "rule 4: trailing data after the JSON value"
	}
	// Rule 5 is checked before any element is decoded so that memory stays
	// bounded by the content limit, not by the element count.
	if n := len(env.Alternatives); n < 1 || n > len(requested) {
		return nil, fmt.Sprintf("rule 5: %d alternatives, expected 1 to %d", n, len(requested))
	}
	decoded := make([]decodedAlternative, len(env.Alternatives))
	for i, raw := range env.Alternatives {
		ed := json.NewDecoder(bytes.NewReader(raw))
		ed.DisallowUnknownFields()
		if err := ed.Decode(&decoded[i]); err != nil {
			return nil, fmt.Sprintf("rule 3: alternatives[%d]: %s", i, truncateUTF8(err.Error(), maxRule3Detail))
		}
	}
	want := make(map[protocol.RecommendationIntent]bool, len(requested))
	for _, i := range requested {
		want[i] = true
	}
	alts := make(map[protocol.RecommendationIntent]decodedAlternative, len(decoded))
	for _, a := range decoded {
		if !a.Intent.Valid() {
			return nil, fmt.Sprintf("rule 6: invalid intent %q", string(a.Intent))
		}
		if !want[a.Intent] {
			return nil, fmt.Sprintf("rule 6: intent %q was not requested", string(a.Intent))
		}
		if _, dup := alts[a.Intent]; dup {
			return nil, fmt.Sprintf("rule 6: duplicate intent %q", string(a.Intent))
		}
		alts[a.Intent] = a
	}
	return alts, ""
}

// stripFence implements the rule 2 fence algorithm on already-trimmed text:
// text that does not start with a fence is returned as-is; otherwise it must be
// a single fenced block whose opening line is "```" or "```json" and whose last
// line is exactly "```".
func stripFence(text string) (string, bool) {
	if !strings.HasPrefix(text, "```") {
		return text, true
	}
	nl := strings.IndexByte(text, '\n')
	if nl < 0 {
		return "", false
	}
	label := strings.TrimSpace(strings.TrimRight(text[3:nl], "\r"))
	if label != "" && label != "json" {
		return "", false
	}
	rest := text[nl+1:]
	body, last := "", rest
	if lastNL := strings.LastIndexByte(rest, '\n'); lastNL >= 0 {
		body, last = rest[:lastNL], rest[lastNL+1:]
	}
	if strings.TrimRight(last, "\r") != "```" {
		return "", false
	}
	return body, true
}

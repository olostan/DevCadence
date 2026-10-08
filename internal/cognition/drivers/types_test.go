package drivers

import (
	"encoding/json"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
)

func TestTokenMeasurement_ZeroValueIsUnknown(t *testing.T) {
	var m TokenMeasurement
	if m.Known {
		t.Errorf("zero value TokenMeasurement should be unknown (Known: false), got known")
	}
	if m.Value != 0 {
		t.Errorf("zero value TokenMeasurement value should be 0, got %d", m.Value)
	}

	known := KnownMeasurement(10)
	if !known.Known || known.Value != 10 {
		t.Errorf("expected Known: true, Value: 10, got known=%v value=%d", known.Known, known.Value)
	}

	zero := KnownMeasurement(0)
	if !zero.Known || zero.Value != 0 {
		t.Errorf("expected Known: true, Value: 0, got known=%v value=%d", zero.Known, zero.Value)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected KnownMeasurement(-1) to panic")
		}
	}()
	_ = KnownMeasurement(-1)
}

func TestTokenUsage_ZeroValueIsUnknown(t *testing.T) {
	var u TokenUsage
	if u.Input.Known || u.Cached.Known || u.Output.Known {
		t.Errorf("zero value TokenUsage should have all fields unknown, got %+v", u)
	}
	if u.Complete() {
		t.Errorf("zero value TokenUsage Complete() should be false")
	}
	total, known := u.Total()
	if known {
		t.Errorf("zero value TokenUsage Total() should be unknown, got known with total=%d", total)
	}
	if total != 0 {
		t.Errorf("zero value TokenUsage Total() should be 0, got %d", total)
	}
}

func TestTokenUsage_KnownZeroUsage(t *testing.T) {
	u := KnownZeroUsage()
	if !u.Input.Known || u.Input.Value != 0 {
		t.Errorf("expected Input known 0, got %+v", u.Input)
	}
	if !u.Cached.Known || u.Cached.Value != 0 {
		t.Errorf("expected Cached known 0, got %+v", u.Cached)
	}
	if !u.Output.Known || u.Output.Value != 0 {
		t.Errorf("expected Output known 0, got %+v", u.Output)
	}
	if !u.Complete() {
		t.Errorf("KnownZeroUsage() Complete() should be true")
	}
	total, known := u.Total()
	if !known || total != 0 {
		t.Errorf("KnownZeroUsage() Total() should be known 0, got total=%d known=%v", total, known)
	}
}

func TestTokenUsage_KnownUsage(t *testing.T) {
	u := KnownUsage(100, 20, 50)
	if !u.Input.Known || u.Input.Value != 100 {
		t.Errorf("expected Input known 100, got %+v", u.Input)
	}
	if !u.Cached.Known || u.Cached.Value != 20 {
		t.Errorf("expected Cached known 20, got %+v", u.Cached)
	}
	if !u.Output.Known || u.Output.Value != 50 {
		t.Errorf("expected Output known 50, got %+v", u.Output)
	}
	if !u.Complete() {
		t.Errorf("KnownUsage Complete() should be true")
	}
	total, known := u.Total()
	if !known || total != 150 {
		t.Errorf("KnownUsage Total() should be known 150 (cached excluded), got total=%d known=%v", total, known)
	}
}

func TestTokenUsage_Total_CachedIndependence(t *testing.T) {
	// P0-4: {input known, cached unknown, output known} -> Total() known, Complete() == false
	u := TokenUsage{
		Input:  KnownMeasurement(100),
		Cached: TokenMeasurement{}, // unknown
		Output: KnownMeasurement(50),
	}
	total, known := u.Total()
	if !known || total != 150 {
		t.Errorf("expected Total() known 150 when Input and Output are known, got total=%d known=%v", total, known)
	}
	if u.Complete() {
		t.Errorf("expected Complete() == false when Cached is unknown")
	}

	// Input unknown, Output known -> Total() unknown
	uInUnknown := TokenUsage{
		Input:  TokenMeasurement{},
		Cached: KnownMeasurement(20),
		Output: KnownMeasurement(50),
	}
	total, known = uInUnknown.Total()
	if known {
		t.Errorf("expected Total() unknown when Input is unknown, got total=%d", total)
	}

	// Input known, Output unknown -> Total() unknown
	uOutUnknown := TokenUsage{
		Input:  KnownMeasurement(100),
		Cached: KnownMeasurement(20),
		Output: TokenMeasurement{},
	}
	total, known = uOutUnknown.Total()
	if known {
		t.Errorf("expected Total() unknown when Output is unknown, got total=%d", total)
	}
}

func TestTokenUsage_Add_PreservesKnownState(t *testing.T) {
	// P0-2: KnownZeroUsage().Add(KnownUsage(i, c, o)) equals KnownUsage(i, c, o)
	zero := KnownZeroUsage()
	u := KnownUsage(100, 20, 50)
	res := zero.Add(u)

	if !res.Input.Known || res.Input.Value != 100 {
		t.Errorf("expected Input known 100, got %+v", res.Input)
	}
	if !res.Cached.Known || res.Cached.Value != 20 {
		t.Errorf("expected Cached known 20, got %+v", res.Cached)
	}
	if !res.Output.Known || res.Output.Value != 50 {
		t.Errorf("expected Output known 50, got %+v", res.Output)
	}

	u2 := KnownUsage(50, 10, 25)
	sum := u.Add(u2)
	if sum.Input.Value != 150 || sum.Cached.Value != 30 || sum.Output.Value != 75 {
		t.Errorf("expected element-wise sum (150, 30, 75), got (%d, %d, %d)", sum.Input.Value, sum.Cached.Value, sum.Output.Value)
	}
	if !sum.Complete() {
		t.Errorf("sum of two known usages should be Complete()")
	}
}

func TestTokenUsage_Add_UnknownToKnownYieldsUnknown(t *testing.T) {
	// P0-3: an accumulator started from the Go zero value TokenUsage{} then given known usage -> result unknown
	zeroVal := TokenUsage{} // Go zero value, all unknown
	known := KnownUsage(100, 20, 50)

	res := zeroVal.Add(known)
	if res.Input.Known || res.Cached.Known || res.Output.Known {
		t.Errorf("adding known to unknown must yield unknown, got %+v", res)
	}

	res2 := known.Add(zeroVal)
	if res2.Input.Known || res2.Cached.Known || res2.Output.Known {
		t.Errorf("adding unknown to known must yield unknown, got %+v", res2)
	}

	// Partial unknown: Cached is unknown in operand
	partial := TokenUsage{
		Input:  KnownMeasurement(10),
		Cached: TokenMeasurement{}, // unknown
		Output: KnownMeasurement(5),
	}
	sumPartial := KnownZeroUsage().Add(partial)
	if !sumPartial.Input.Known || sumPartial.Input.Value != 10 {
		t.Errorf("Input should remain known 10, got %+v", sumPartial.Input)
	}
	if sumPartial.Cached.Known {
		t.Errorf("Cached should be unknown, got %+v", sumPartial.Cached)
	}
	if !sumPartial.Output.Known || sumPartial.Output.Value != 5 {
		t.Errorf("Output should remain known 5, got %+v", sumPartial.Output)
	}
}

func TestTokenUsage_JSON_Unmarshal(t *testing.T) {
	// JSON with non-negative integers
	data := []byte(`{"input_tokens": 100, "cached_tokens": 20, "output_tokens": 50}`)
	var u TokenUsage
	if err := json.Unmarshal(data, &u); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if !u.Input.Known || u.Input.Value != 100 {
		t.Errorf("expected Input known 100, got %+v", u.Input)
	}
	if !u.Cached.Known || u.Cached.Value != 20 {
		t.Errorf("expected Cached known 20, got %+v", u.Cached)
	}
	if !u.Output.Known || u.Output.Value != 50 {
		t.Errorf("expected Output known 50, got %+v", u.Output)
	}

	// Zero values in JSON result in Known: true, Value: 0
	zeroJSON := []byte(`{"input_tokens": 0, "cached_tokens": 0, "output_tokens": 0}`)
	var uZero TokenUsage
	if err := json.Unmarshal(zeroJSON, &uZero); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if !uZero.Input.Known || uZero.Input.Value != 0 {
		t.Errorf("expected Input known 0, got %+v", uZero.Input)
	}
	if !uZero.Cached.Known || uZero.Cached.Value != 0 {
		t.Errorf("expected Cached known 0, got %+v", uZero.Cached)
	}
	if !uZero.Output.Known || uZero.Output.Value != 0 {
		t.Errorf("expected Output known 0, got %+v", uZero.Output)
	}

	// Missing keys result in Known: false (P0-1)
	emptyJSON := []byte(`{}`)
	var uEmpty TokenUsage
	if err := json.Unmarshal(emptyJSON, &uEmpty); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if uEmpty.Input.Known || uEmpty.Cached.Known || uEmpty.Output.Known {
		t.Errorf("expected all unknown for empty JSON object, got %+v", uEmpty)
	}

	// Explicit null results in Known: false (P0-1)
	nullJSON := []byte(`{"input_tokens": null, "cached_tokens": null, "output_tokens": null}`)
	var uNull TokenUsage
	if err := json.Unmarshal(nullJSON, &uNull); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if uNull.Input.Known || uNull.Cached.Known || uNull.Output.Known {
		t.Errorf("expected all unknown for null fields, got %+v", uNull)
	}

	// Partial missing and null
	partialJSON := []byte(`{"input_tokens": 42, "cached_tokens": null}`)
	var uPartial TokenUsage
	if err := json.Unmarshal(partialJSON, &uPartial); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if !uPartial.Input.Known || uPartial.Input.Value != 42 {
		t.Errorf("expected Input known 42, got %+v", uPartial.Input)
	}
	if uPartial.Cached.Known {
		t.Errorf("expected Cached unknown, got %+v", uPartial.Cached)
	}
	if uPartial.Output.Known {
		t.Errorf("expected Output unknown, got %+v", uPartial.Output)
	}

	// Negative values must be rejected with errs.CategoryInvalidArgument (P0-5)
	negInputs := [][]byte{
		[]byte(`{"input_tokens": -1}`),
		[]byte(`{"cached_tokens": -5}`),
		[]byte(`{"output_tokens": -10}`),
	}
	for _, neg := range negInputs {
		var uNeg TokenUsage
		err := json.Unmarshal(neg, &uNeg)
		if err == nil {
			t.Errorf("expected error for negative tokens in %s, got nil", string(neg))
		}
		if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument for negative tokens in %s, got %v", string(neg), err)
		}
	}
}

func TestTokenUsage_JSON_Marshal(t *testing.T) {
	// All known
	u := KnownUsage(100, 20, 50)
	bytes, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	expected := `{"input_tokens":100,"cached_tokens":20,"output_tokens":50}`
	if string(bytes) != expected {
		t.Errorf("expected %s, got %s", expected, string(bytes))
	}

	// All unknown marshals to {} (P0-5)
	var uUnknown TokenUsage
	bytesUnknown, err := json.Marshal(uUnknown)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	if string(bytesUnknown) != "{}" {
		t.Errorf("expected {}, got %s", string(bytesUnknown))
	}

	// Partial known omits unknown fields
	uPartial := TokenUsage{
		Input:  KnownMeasurement(42),
		Output: KnownMeasurement(12),
	}
	bytesPartial, err := json.Marshal(uPartial)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	expectedPartial := `{"input_tokens":42,"output_tokens":12}`
	if string(bytesPartial) != expectedPartial {
		t.Errorf("expected %s, got %s", expectedPartial, string(bytesPartial))
	}

	// Round-trip
	var roundTrip TokenUsage
	if err := json.Unmarshal(bytesPartial, &roundTrip); err != nil {
		t.Fatalf("round trip unmarshal failed: %v", err)
	}
	if !roundTrip.Input.Known || roundTrip.Input.Value != 42 {
		t.Errorf("round trip Input mismatch: %+v", roundTrip.Input)
	}
	if roundTrip.Cached.Known {
		t.Errorf("round trip Cached should be unknown")
	}
	if !roundTrip.Output.Known || roundTrip.Output.Value != 12 {
		t.Errorf("round trip Output mismatch: %+v", roundTrip.Output)
	}
}

func TestSessionConfig_Validation(t *testing.T) {
	cfgValid := SessionConfig{
		SessionID:              "s1",
		ModelID:                "m1",
		MaxOutputTokensPerCall: 4096,
	}
	if err := cfgValid.Validate(); err != nil {
		t.Errorf("expected valid config, got %v", err)
	}

	cfgNegativeTokens := SessionConfig{
		SessionID:              "s1",
		ModelID:                "m1",
		MaxOutputTokensPerCall: -1,
	}
	if err := cfgNegativeTokens.Validate(); err == nil {
		t.Errorf("expected error for negative MaxOutputTokensPerCall, got nil")
	} else if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument, got %v", err)
	}

	cfgZeroTokens := SessionConfig{
		SessionID:              "s1",
		ModelID:                "m1",
		MaxOutputTokensPerCall: 0,
	}
	if err := cfgZeroTokens.Validate(); err != nil {
		t.Errorf("expected zero MaxOutputTokensPerCall to be valid (optional in SessionConfig), got %v", err)
	}
}

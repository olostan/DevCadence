package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
)

// These tests pin the renderers that the host-probing CLI tests only reach
// depending on what the machine running the suite happens to report. They use
// literal inputs so the covered statements do not vary with the host.

func TestOrUnknownBytesIsHostIndependent(t *testing.T) {
	i64 := func(v int64) *int64 { return &v }
	cases := []struct {
		name string
		in   *int64
		want string
	}{
		{"nil", nil, "unknown"},
		{"zero", i64(0), "0 B"},
		{"below one KiB", i64(1023), "1023 B"},
		{"exactly one KiB", i64(1024), "1.0 KiB"},
		{"one and a half KiB", i64(1536), "1.5 KiB"},
		{"one MiB", i64(1 << 20), "1.0 MiB"},
		{"one GiB", i64(1 << 30), "1.0 GiB"},
		{"one TiB", i64(1 << 40), "1.0 TiB"},
		{"beyond TiB stays in TiB", i64(2048 << 40), "2048.0 TiB"},
	}
	for _, tc := range cases {
		if got := orUnknownBytes(tc.in); got != tc.want {
			t.Errorf("%s: orUnknownBytes = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRenderEndpointOptionalSections(t *testing.T) {
	full := protocol.CognitionEndpoint{
		ID:           "ep-full",
		ModelID:      "model-x",
		Version:      "1.2.3",
		Acceleration: &protocol.AccelerationEvidence{Backend: "cpu", State: "unverified", Signals: []protocol.AccelerationSignal{{Source: "s", Trust: "observed", Backend: "cpu", Offloaded: false, Statement: "ran on cpu"}}},
		Capabilities: []protocol.GradedCapability{{Dimension: "coding", Grade: "good", Provenance: "declared"}},
		Measurements: []protocol.Measurement{{Name: "load_duration_ms", Value: 1.5, Unit: "ms"}},
		Findings: []protocol.DiscoveryFinding{
			{Component: "b.second", Detail: "second note"},
			{Component: "a.first", Detail: "first note"},
			{Component: "c.silent", Detail: ""},
		},
	}
	var verbose bytes.Buffer
	renderEndpoint(&verbose, full, true)
	out := verbose.String()
	for _, want := range []string{
		"model       model-x", "version     1.2.3", "accel       ", "offloaded=false ran on cpu",
		"capability\n", "coding", "measured", "load_duration_ms", "note        first note",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose render lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "first note") > strings.Index(out, "second note") {
		t.Errorf("findings are not sorted by component:\n%s", out)
	}
	if strings.Count(out, "note        ") != 2 {
		t.Errorf("an empty finding detail was rendered:\n%s", out)
	}

	var terse bytes.Buffer
	renderEndpoint(&terse, full, false)
	if strings.Contains(terse.String(), "measured") || strings.Contains(terse.String(), "offloaded=") {
		t.Errorf("non-verbose render leaked verbose detail:\n%s", terse.String())
	}

	var bare bytes.Buffer
	renderEndpoint(&bare, protocol.CognitionEndpoint{ID: "ep-bare"}, true)
	b := bare.String()
	for _, absent := range []string{"model       ", "version     ", "accel       ", "measured", "note        "} {
		if strings.Contains(b, absent) {
			t.Errorf("bare endpoint rendered %q:\n%s", absent, b)
		}
	}
	if !strings.Contains(b, "ungraded (no measurement or declaration)") {
		t.Errorf("bare endpoint does not say it is ungraded:\n%s", b)
	}
}

func TestRenderProfileAndCandidatesFromLiteralInput(t *testing.T) {
	var none bytes.Buffer
	renderProfile(&none, protocol.MachineCapabilityProfile{MachineFingerprint: "fp", Assessment: "a"})
	if !strings.Contains(none.String(), "none discovered") || strings.Contains(none.String(), "limitations") {
		t.Errorf("empty profile render wrong:\n%s", none.String())
	}

	var some bytes.Buffer
	renderProfile(&some, protocol.MachineCapabilityProfile{
		MachineFingerprint: "fp", Limitations: []string{"lim one"},
		Endpoints: []protocol.CognitionEndpoint{{ID: "ep1"}},
	})
	if !strings.Contains(some.String(), "limitations\n  - lim one") || !strings.Contains(some.String(), "ep1") {
		t.Errorf("profile render wrong:\n%s", some.String())
	}

	var cand bytes.Buffer
	renderCandidates(&cand, []protocol.AcceleratorCandidate{
		{Backend: "cuda", Support: "supported", State: "assessed", Reasons: []string{"why"}, RequiredSoftware: []string{"libx", "liby"}},
		{Backend: "cpu", Support: "supported", State: "assessed"},
	})
	c := cand.String()
	for _, want := range []string{"- why", "missing: libx, liby", "never reports a backend as verified"} {
		if !strings.Contains(c, want) {
			t.Errorf("candidates render lacks %q:\n%s", want, c)
		}
	}
	if strings.Count(c, "missing:") != 1 {
		t.Errorf("missing line rendered for a candidate without requirements:\n%s", c)
	}
}

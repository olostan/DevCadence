package empirical

import (
	"math"
	"testing"
	"time"
)

func validSessionEvidence() SessionEvidence {
	f := newFixture()
	r := f.runs[0]
	return SessionEvidence{
		SchemaVersion:              SchemaVersion,
		RunID:                      r.RunID,
		CampaignID:                 f.plan.CampaignID,
		AttemptID:                  "att-" + r.RunID,
		TaskDigest:                 r.TaskDigest,
		Endpoint:                   r.Endpoint,
		PromptDigest:               r.PromptDigest,
		ContextManifestDigest:      dg("manifest"),
		InvocationProvenanceDigest: dg("provenance"),
		ExecutionPolicyDigest:      dg("policy"),
		AuthorizationDigest:        dg("auth"),
		StartedAt:                  time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		FinishedAt:                 time.Date(2026, 1, 1, 10, 5, 0, 0, time.UTC),
		DriverOutcome:              StatusCompleted,
		Turns:                      1,
		Usage:                      r.Measurements,
	}
}

func TestUSDToMicroUSD(t *testing.T) {
	cases := []struct {
		name    string
		usd     float64
		want    int64
		wantErr bool
	}{
		{"zero", 0.0, 0, false},
		{"subcent", 0.000001, 1, false},
		{"round down", 0.0000014, 1, false},
		{"round up", 0.0000016, 2, false},
		{"ten dollars", 10.0, 10000000, false},
		{"fractional dollars", 12.345678, 12345678, false},
		{"negative", -0.01, 0, true},
		{"nan", math.NaN(), 0, true},
		{"inf pos", math.Inf(1), 0, true},
		{"inf neg", math.Inf(-1), 0, true},
		{"overflow max float64", math.MaxFloat64, 0, true},
		{"overflow limit", MaxAPISpendUSD + 1.0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := USDToMicroUSD(tc.usd)
			if (err != nil) != tc.wantErr {
				t.Fatalf("USDToMicroUSD(%v) error = %v, wantErr = %v", tc.usd, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("USDToMicroUSD(%v) = %v, want %v", tc.usd, got, tc.want)
			}
		})
	}
}

func TestMicroUSDToUSD(t *testing.T) {
	usd := MicroUSDToUSD(12345678)
	if usd != 12.345678 {
		t.Fatalf("MicroUSDToUSD(12345678) = %v, want 12.345678", usd)
	}
	if MicroUSDToUSD(0) != 0.0 {
		t.Fatalf("MicroUSDToUSD(0) = %v, want 0.0", MicroUSDToUSD(0))
	}
}

func TestSessionEvidenceValidate(t *testing.T) {
	t.Run("valid baseline", func(t *testing.T) {
		s := validSessionEvidence()
		if err := s.Validate(); err != nil {
			t.Fatalf("valid session evidence failed validation: %v", err)
		}
		if s.Version() != SchemaVersion {
			t.Fatalf("Version() = %q, want %q", s.Version(), SchemaVersion)
		}
		if !s.EndedAt().Equal(s.FinishedAt) {
			t.Fatalf("EndedAt() = %v, want %v", s.EndedAt(), s.FinishedAt)
		}
		if s.EndpointBinding() != s.Endpoint {
			t.Fatalf("EndpointBinding() differs")
		}
	})

	t.Run("schema version invalid", func(t *testing.T) {
		s := validSessionEvidence()
		s.SchemaVersion = "2.0"
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for bad schema version")
		}
	})

	t.Run("required fields blank", func(t *testing.T) {
		for _, field := range []string{"run_id", "campaign_id", "attempt_id"} {
			s := validSessionEvidence()
			switch field {
			case "run_id":
				s.RunID = ""
			case "campaign_id":
				s.CampaignID = ""
			case "attempt_id":
				s.AttemptID = ""
			}
			if err := s.Validate(); err == nil {
				t.Fatalf("expected error for blank %s", field)
			}
		}
	})

	t.Run("invalid digests", func(t *testing.T) {
		for _, d := range []string{
			"task_digest", "prompt_digest", "context_manifest_digest",
			"invocation_provenance_digest", "execution_policy_digest", "authorization_digest",
		} {
			s := validSessionEvidence()
			switch d {
			case "task_digest":
				s.TaskDigest = "bad"
			case "prompt_digest":
				s.PromptDigest = "bad"
			case "context_manifest_digest":
				s.ContextManifestDigest = "bad"
			case "invocation_provenance_digest":
				s.InvocationProvenanceDigest = "bad"
			case "execution_policy_digest":
				s.ExecutionPolicyDigest = "bad"
			case "authorization_digest":
				s.AuthorizationDigest = "bad"
			}
			if err := s.Validate(); err == nil {
				t.Fatalf("expected error for invalid %s", d)
			}
		}
	})

	t.Run("invalid endpoint", func(t *testing.T) {
		s := validSessionEvidence()
		s.Endpoint.ModelID = ""
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for empty endpoint model ID")
		}
	})

	t.Run("timestamps", func(t *testing.T) {
		s := validSessionEvidence()
		s.StartedAt = time.Time{}
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for zero started_at")
		}

		s = validSessionEvidence()
		s.FinishedAt = time.Time{}
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for zero finished_at")
		}

		s = validSessionEvidence()
		s.FinishedAt = s.StartedAt.Add(-time.Second)
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for finished_at before started_at")
		}
	})

	t.Run("driver outcome enum", func(t *testing.T) {
		s := validSessionEvidence()
		for _, outcome := range []string{StatusCompleted, "error", StatusCancelled, "limit_reached"} {
			s.DriverOutcome = outcome
			if err := s.Validate(); err != nil {
				t.Fatalf("valid outcome %q got error: %v", outcome, err)
			}
		}
		s.DriverOutcome = "unknown_status"
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for invalid driver outcome")
		}
	})

	t.Run("negative turns", func(t *testing.T) {
		s := validSessionEvidence()
		s.Turns = -1
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for negative turns")
		}
	})

	t.Run("usage checks", func(t *testing.T) {
		s := validSessionEvidence()
		s.Usage = nil
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for nil usage")
		}

		s = validSessionEvidence()
		delete(s.Usage, "api_spend_usd")
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for missing usage key")
		}

		s = validSessionEvidence()
		s.Usage["api_spend_usd"] = Measurement{Unit: "token", Provenance: ProvenanceUnknown, EvidenceRef: "why"}
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for incorrect unit")
		}

		s = validSessionEvidence()
		s.Usage["api_spend_usd"] = Measurement{Unit: "USD", Provenance: ProvenanceUnknown, EvidenceRef: ""}
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for empty evidence_ref")
		}

		s = validSessionEvidence()
		val := -1.0
		s.Usage["api_spend_usd"] = Measurement{Known: true, Value: &val, Unit: "USD", Provenance: ProvenanceMeasured, EvidenceRef: "why"}
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for negative known measurement value")
		}
	})

	t.Run("unprovable limits", func(t *testing.T) {
		s := validSessionEvidence()
		s.UnprovableLimits = []string{"max_cumulative_input_tokens", "max_cumulative_output_tokens"}
		if err := s.Validate(); err != nil {
			t.Fatalf("valid sorted unprovable limits failed: %v", err)
		}

		s.UnprovableLimits = []string{"unknown_limit"}
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for invalid limit name")
		}

		// Unsorted
		s.UnprovableLimits = []string{"max_cumulative_output_tokens", "max_cumulative_input_tokens"}
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for unsorted unprovable limits")
		}

		// Duplicated
		s.UnprovableLimits = []string{"max_cumulative_input_tokens", "max_cumulative_input_tokens"}
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for duplicated unprovable limits")
		}
	})
}

func TestSessionEvidenceDigestStability(t *testing.T) {
	s1 := validSessionEvidence()
	d1, err := s1.Digest()
	if err != nil {
		t.Fatalf("s1.Digest() error: %v", err)
	}

	dTop, err := SessionEvidenceDigest(s1)
	if err != nil || dTop != d1 {
		t.Fatalf("SessionEvidenceDigest mismatch: got %v, want %v", dTop, d1)
	}

	// Local timezone conversion check: Digest normalizes to UTC
	loc, err := time.LoadLocation("America/New_York")
	if err == nil {
		s2 := s1
		s2.StartedAt = s1.StartedAt.In(loc)
		s2.FinishedAt = s1.FinishedAt.In(loc)
		d2, err := s2.Digest()
		if err != nil {
			t.Fatalf("s2.Digest() error: %v", err)
		}
		if d1 != d2 {
			t.Fatalf("digest changed across timezone representations: d1=%s d2=%s", d1, d2)
		}
	}

	// Invalid session evidence returns error on Digest()
	sBad := s1
	sBad.RunID = ""
	if _, err := sBad.Digest(); err == nil {
		t.Fatal("expected error computing digest of invalid session evidence")
	}
}

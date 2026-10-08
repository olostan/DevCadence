package empirical

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/olostan/DevCadence/internal/protocol"
)

// Allowed unprovable limit names (closed vocabulary from MeterLimits keys).
var allowedUnprovableLimits = map[string]bool{
	"max_cumulative_input_tokens":  true,
	"max_cumulative_cached_tokens": true,
	"max_cumulative_output_tokens": true,
	"max_cumulative_total_tokens":  true,
}

// SessionEvidence records live execution provenance and measurements for an
// empirical session (WP-M5-R3 Part B, Amendment 7).
type SessionEvidence struct {
	SchemaVersion              string                 `json:"schema_version"`
	RunID                      string                 `json:"run_id"`
	CampaignID                 string                 `json:"campaign_id"`
	AttemptID                  string                 `json:"attempt_id"`
	TaskDigest                 string                 `json:"task_digest"`
	Endpoint                   EndpointBinding        `json:"endpoint"`
	PromptDigest               string                 `json:"prompt_digest"`
	ContextManifestDigest      string                 `json:"context_manifest_digest"`
	InvocationProvenanceDigest string                 `json:"invocation_provenance_digest"`
	ExecutionPolicyDigest      string                 `json:"execution_policy_digest"`
	AuthorizationDigest        string                 `json:"authorization_digest"`
	StartedAt                  time.Time              `json:"started_at"`
	FinishedAt                 time.Time              `json:"finished_at"`
	DriverOutcome              string                 `json:"driver_outcome"`
	Turns                      int                    `json:"turns"`
	Usage                      map[string]Measurement `json:"usage"`
	UnprovableLimits           []string               `json:"unprovable_limits,omitempty"`
}

// Version returns the schema version for callers that query Version().
func (s SessionEvidence) Version() string { return s.SchemaVersion }

// EndedAt returns FinishedAt for backwards compatibility with draft naming.
func (s SessionEvidence) EndedAt() time.Time { return s.FinishedAt }

// EndpointBinding returns the bound endpoint.
func (s SessionEvidence) EndpointBinding() EndpointBinding { return s.Endpoint }

// Validate checks that the session evidence conforms to the strict schema contract.
func (s SessionEvidence) Validate() error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q, got %q", SchemaVersion, s.SchemaVersion)
	}
	if blank(s.RunID) {
		return errors.New("run_id is required")
	}
	if blank(s.CampaignID) {
		return errors.New("campaign_id is required")
	}
	if blank(s.AttemptID) {
		return errors.New("attempt_id is required")
	}
	for name, val := range map[string]string{
		"task_digest":                  s.TaskDigest,
		"prompt_digest":                s.PromptDigest,
		"context_manifest_digest":      s.ContextManifestDigest,
		"invocation_provenance_digest": s.InvocationProvenanceDigest,
		"execution_policy_digest":      s.ExecutionPolicyDigest,
		"authorization_digest":         s.AuthorizationDigest,
	} {
		if !validDigest(val) {
			return fmt.Errorf("%s must be a valid sha256 digest, got %q", name, val)
		}
	}
	if msg := validEndpoint(s.Endpoint); msg != "" {
		return fmt.Errorf("endpoint: %s", msg)
	}
	if s.StartedAt.IsZero() {
		return errors.New("started_at is required")
	}
	if s.FinishedAt.IsZero() {
		return errors.New("finished_at is required")
	}
	if s.FinishedAt.Before(s.StartedAt) {
		return fmt.Errorf("finished_at (%s) is before started_at (%s)",
			s.FinishedAt.Format(time.RFC3339), s.StartedAt.Format(time.RFC3339))
	}
	switch s.DriverOutcome {
	case StatusCompleted, "error", StatusCancelled, "limit_reached":
	default:
		return fmt.Errorf("driver_outcome %q is not closed vocabulary (completed|error|cancelled|limit_reached)", s.DriverOutcome)
	}
	if s.Turns < 0 {
		return fmt.Errorf("turns must be nonnegative, got %d", s.Turns)
	}
	if len(s.Usage) != len(measurementUnits) {
		return fmt.Errorf("usage must carry exactly the %d closed measurement keys, got %d",
			len(measurementUnits), len(s.Usage))
	}
	for k, unit := range measurementUnits {
		ms, ok := s.Usage[k]
		if !ok {
			return fmt.Errorf("missing usage measurement key %q", k)
		}
		if ms.Unit != unit {
			return fmt.Errorf("usage %s unit must be %q, got %q", k, unit, ms.Unit)
		}
		if blank(ms.EvidenceRef) {
			return fmt.Errorf("usage %s has no evidence_ref", k)
		}
		if ms.Known {
			if ms.Value == nil || !finiteNonNeg(*ms.Value) || ms.Provenance != ProvenanceMeasured ||
				(integralMeasurements[k] && *ms.Value != math.Trunc(*ms.Value)) {
				return fmt.Errorf("usage known %s needs a finite nonnegative (integral where counted) value with measured provenance", k)
			}
			if k == "subscription_quota" && (blank(s.Endpoint.SubscriptionQuotaUnit) || s.Endpoint.SubscriptionQuotaUnit == UnknownQuotaUnit) {
				return errors.New("usage known subscription_quota requires a pinned provider unit")
			}
		} else {
			if ms.Value != nil || ms.Provenance != ProvenanceUnknown {
				return fmt.Errorf("usage unknown %s must have a nil value and unknown provenance (never zero)", k)
			}
		}
	}
	// Validate unprovable limits: sorted, unique, allowed vocabulary.
	for i, lim := range s.UnprovableLimits {
		if !allowedUnprovableLimits[lim] {
			return fmt.Errorf("unprovable_limit %q is not a known limit name", lim)
		}
		if i > 0 && s.UnprovableLimits[i-1] >= lim {
			return fmt.Errorf("unprovable_limits must be sorted and deduplicated (got %q before %q)",
				s.UnprovableLimits[i-1], lim)
		}
	}
	return nil
}

// Digest computes the canonical sha256 digest of the session evidence.
func (s SessionEvidence) Digest() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	copy := s
	copy.StartedAt = s.StartedAt.UTC()
	copy.FinishedAt = s.FinishedAt.UTC()
	return protocol.Digest(copy)
}

// SessionEvidenceDigest is a top-level helper to derive the canonical digest.
func SessionEvidenceDigest(s SessionEvidence) (string, error) {
	return s.Digest()
}

// MaxAPISpendUSD is the maximum USD amount convertible to micro-USD without int64 overflow.
const MaxAPISpendUSD = float64(math.MaxInt64) / 1e6

// USDToMicroUSD converts a USD amount to integer micro-USD.
// It returns an error if the amount is NaN, infinite, negative, or overflows int64.
func USDToMicroUSD(usd float64) (int64, error) {
	if math.IsNaN(usd) || math.IsInf(usd, 0) {
		return 0, errors.New("usd amount must be finite")
	}
	if usd < 0 {
		return 0, errors.New("usd amount must be nonnegative")
	}
	if usd > MaxAPISpendUSD {
		return 0, errors.New("usd amount overflows int64 micro-USD")
	}
	micro := math.Round(usd * 1e6)
	if micro > float64(math.MaxInt64) {
		return 0, errors.New("usd amount overflows int64 micro-USD")
	}
	return int64(micro), nil
}

// MicroUSDToUSD converts integer micro-USD to float64 USD.
func MicroUSDToUSD(micro int64) float64 {
	return float64(micro) / 1e6
}

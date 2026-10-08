package execpolicy_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

func sampleResolvedEndpoint() execpolicy.ResolvedEndpoint {
	return execpolicy.ResolvedEndpoint{
		EndpointID:      "ep-1",
		ModelID:         "claude-3-5-sonnet",
		Provider:        "anthropic",
		ModelFamily:     "claude-3-5",
		AccountRef:      "account-1",
		Kind:            protocol.EndpointAuthenticatedCLI,
		Locality:        protocol.LocalityRemote,
		PolicyDigest:    "policy-digest-1",
		PortfolioDigest: "portfolio-digest-1",
		Channel: protocol.AccessChannel{
			SchemaVersion:         protocol.SchemaVersion1,
			ChannelID:             "chan-1",
			EndpointID:            "ep-1",
			Kind:                  protocol.ChannelCLISubprocess,
			SessionMode:           protocol.SessionStatelessPerCall,
			ContextControl:        protocol.ContextControlExactStateless,
			PrefixCache:           protocol.PrefixCacheNone,
			MaxConcurrentRequests: 2,
		},
		ContextProfile: protocol.ContextProfile{
			ProfileID:  "prof-1",
			EndpointID: "ep-1",
			ChannelID:  "chan-1",
			ModelRef:   "claude-3-5-sonnet",
		},
		Limits: sampleLimits(),
	}
}

func TestEndpointObservation_Validate(t *testing.T) {
	validObs := execpolicy.EndpointObservation{
		ModelRevision:  "20241022",
		RuntimeVersion: "1.2.3",
		DriverID:       "driver-cli-1",
	}

	if err := validObs.Validate(); err != nil {
		t.Fatalf("expected valid observation, got %v", err)
	}

	t.Run("missing model_revision", func(t *testing.T) {
		o := validObs
		o.ModelRevision = ""
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
	})

	t.Run("missing runtime_version", func(t *testing.T) {
		o := validObs
		o.RuntimeVersion = ""
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
	})

	t.Run("missing driver_id", func(t *testing.T) {
		o := validObs
		o.DriverID = ""
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
	})
}

func TestBind_SuccessAndDeterminism(t *testing.T) {
	ep := sampleResolvedEndpoint()
	obs := execpolicy.EndpointObservation{
		ModelRevision:  "rev-20241022",
		RuntimeVersion: "v1.2.3",
		DriverID:       "driver-cli-1",
	}

	bound1, err := execpolicy.Bind(ep, obs)
	if err != nil {
		t.Fatalf("unexpected Bind error: %v", err)
	}

	if bound1.ModelRevision != "rev-20241022" {
		t.Errorf("ModelRevision = %q, want rev-20241022", bound1.ModelRevision)
	}
	if bound1.RuntimeVersion != "v1.2.3" {
		t.Errorf("RuntimeVersion = %q, want v1.2.3", bound1.RuntimeVersion)
	}
	if bound1.DriverID != "driver-cli-1" {
		t.Errorf("DriverID = %q, want driver-cli-1", bound1.DriverID)
	}
	if bound1.BindingDigest == "" {
		t.Fatal("expected non-empty BindingDigest")
	}

	// Determinism check
	bound2, err := execpolicy.Bind(ep, obs)
	if err != nil {
		t.Fatalf("unexpected Bind error: %v", err)
	}
	if bound1.BindingDigest != bound2.BindingDigest {
		t.Errorf("BindingDigest not deterministic: %q vs %q", bound1.BindingDigest, bound2.BindingDigest)
	}

	// Changing driver ID changes binding digest
	obsDifferentDriver := obs
	obsDifferentDriver.DriverID = "driver-cli-2"
	boundDifferent, err := execpolicy.Bind(ep, obsDifferentDriver)
	if err != nil {
		t.Fatalf("unexpected Bind error: %v", err)
	}
	if bound1.BindingDigest == boundDifferent.BindingDigest {
		t.Errorf("expected different BindingDigest for different driver ID, got same %q", bound1.BindingDigest)
	}

	// Changing model revision changes binding digest
	obsDifferentRev := obs
	obsDifferentRev.ModelRevision = "rev-99999999"
	boundDifferentRev, err := execpolicy.Bind(ep, obsDifferentRev)
	if err != nil {
		t.Fatalf("unexpected Bind error: %v", err)
	}
	if bound1.BindingDigest == boundDifferentRev.BindingDigest {
		t.Errorf("expected different BindingDigest for different model revision, got same %q", bound1.BindingDigest)
	}
}

func TestBind_FailClosedOnMissingMetadata(t *testing.T) {
	ep := sampleResolvedEndpoint()

	t.Run("missing model_revision", func(t *testing.T) {
		obs := execpolicy.EndpointObservation{
			ModelRevision:  "",
			RuntimeVersion: "v1.0.0",
			DriverID:       "driver-1",
		}
		_, err := execpolicy.Bind(ep, obs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
		if !strings.Contains(err.Error(), "model-revision-unknown") {
			t.Errorf("expected error message to contain 'model-revision-unknown', got %q", err.Error())
		}
	})

	t.Run("missing runtime_version", func(t *testing.T) {
		obs := execpolicy.EndpointObservation{
			ModelRevision:  "rev-1",
			RuntimeVersion: "",
			DriverID:       "driver-1",
		}
		_, err := execpolicy.Bind(ep, obs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
		if !strings.Contains(err.Error(), "runtime-version-unknown") {
			t.Errorf("expected error message to contain 'runtime-version-unknown', got %q", err.Error())
		}
	})

	t.Run("missing driver_id", func(t *testing.T) {
		obs := execpolicy.EndpointObservation{
			ModelRevision:  "rev-1",
			RuntimeVersion: "v1.0.0",
			DriverID:       "",
		}
		_, err := execpolicy.Bind(ep, obs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
		if !strings.Contains(err.Error(), "driver-id-unknown") {
			t.Errorf("expected error message to contain 'driver-id-unknown', got %q", err.Error())
		}
	})
}

func TestAssertDriverID(t *testing.T) {
	obs := execpolicy.EndpointObservation{
		ModelRevision:  "rev-1",
		RuntimeVersion: "v1.0.0",
		DriverID:       "driver-loopback-1",
	}

	t.Run("matching driver id passes", func(t *testing.T) {
		if err := execpolicy.AssertDriverID("driver-loopback-1", obs); err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("mismatched driver id fails closed", func(t *testing.T) {
		err := execpolicy.AssertDriverID("driver-loopback-expected", obs)
		if err == nil {
			t.Fatal("expected mismatch error, got nil")
		}
		coded, ok := err.(*principal.CodedError)
		if !ok {
			t.Fatalf("expected *principal.CodedError, got %T: %v", err, err)
		}
		if coded.Code() != principal.CodeModelUnavailable {
			t.Errorf("Code = %q, want %q", coded.Code(), principal.CodeModelUnavailable)
		}
		if len(coded.EvidenceRefs()) == 0 || coded.EvidenceRefs()[0] != "driver-id-mismatch" {
			t.Errorf("EvidenceRefs = %v, want [driver-id-mismatch]", coded.EvidenceRefs())
		}
		if !strings.Contains(coded.Detail(), "driver-loopback-expected") || !strings.Contains(coded.Detail(), "driver-loopback-1") {
			t.Errorf("Detail unexpected: %q", coded.Detail())
		}
	})
}

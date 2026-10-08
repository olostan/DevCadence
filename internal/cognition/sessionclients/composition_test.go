package sessionclients_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/cognition/sessionclients"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

func sampleResolvedEndpoint(kind protocol.EndpointKind) execpolicy.ResolvedEndpoint {
	return execpolicy.ResolvedEndpoint{
		EndpointID:      "ep-ollama-1",
		ModelID:         "llama3:latest",
		Provider:        "ollama",
		ModelFamily:     "llama3",
		AccountRef:      "local-account",
		Kind:            kind,
		Locality:        protocol.LocalityLocal,
		PolicyDigest:    "policy-digest-12345",
		PortfolioDigest: "portfolio-digest-67890",
		Channel: protocol.AccessChannel{
			SchemaVersion:         protocol.SchemaVersion1,
			ChannelID:             "chan-ollama-1",
			EndpointID:            "ep-ollama-1",
			Kind:                  protocol.ChannelDirectHTTPAPI,
			SessionMode:           protocol.SessionStatelessPerCall,
			ContextControl:        protocol.ContextControlExactStateless,
			PrefixCache:           protocol.PrefixCacheExplicit,
			MaxConcurrentRequests: 2,
		},
		ContextProfile: protocol.ContextProfile{
			ProfileID:  "prof-ollama-1",
			EndpointID: "ep-ollama-1",
			ChannelID:  "chan-ollama-1",
			ModelRef:   "llama3:latest",
		},
		Limits: sampleLimits(),
	}
}

func TestComposition_New_Validation(t *testing.T) {
	t.Run("valid loopback URLs in options", func(t *testing.T) {
		comp, err := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{
				"ep-1": "http://127.0.0.1:11434",
				"ep-2": "http://[::1]:11434",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if comp == nil {
			t.Fatal("expected non-nil Composition")
		}
	})

	t.Run("rejects non-loopback URL in options", func(t *testing.T) {
		_, err := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{
				"ep-1": "http://example.com:11434",
			},
		})
		if err == nil || !errors.Is(err, errs.ErrPolicyDenied) {
			t.Errorf("expected ErrPolicyDenied for non-loopback in options, got %v", err)
		}
	})
}

// Acceptance Scenario A11:
// Remote/CLI return driver-not-implemented with zero network;
// loopback Open sends only GET /api/version and GET /api/tags;
// a missing/empty digest, version or driver id -> empty observation and Bind denies;
// Observed.DriverID == opened.Driver.ID() and changing the driver id changes BindingDigest;
// BindingFor of an unbound endpoint errors and every field matches the source table.
func TestComposition_AcceptanceScenarioA11(t *testing.T) {
	t.Run("remote and CLI kinds return driver-not-implemented with zero network", func(t *testing.T) {
		comp, err := sessionclients.New(sessionclients.Options{})
		if err != nil {
			t.Fatalf("unexpected New error: %v", err)
		}

		deniedKinds := []protocol.EndpointKind{
			protocol.EndpointAuthenticatedCLI,
			protocol.EndpointRemoteAPI,
			protocol.EndpointKind("unknown_kind"),
		}

		for _, kind := range deniedKinds {
			ep := sampleResolvedEndpoint(kind)
			_, err := comp.Open(context.Background(), ep)
			if err == nil {
				t.Fatalf("expected error for kind %q, got nil", kind)
			}

			var coded *principal.CodedError
			if !errors.As(err, &coded) {
				t.Fatalf("expected *principal.CodedError for kind %q, got %T: %v", kind, err, err)
			}
			if coded.Code() != principal.CodeModelUnavailable {
				t.Errorf("Code = %q, want %q", coded.Code(), principal.CodeModelUnavailable)
			}
			refs := coded.EvidenceRefs()
			foundRef := false
			for _, r := range refs {
				if r == "driver-not-implemented" {
					foundRef = true
					break
				}
			}
			if !foundRef {
				t.Errorf("expected ref 'driver-not-implemented' in %v", refs)
			}
		}
	})

	t.Run("loopback Open executes only GET /api/version and GET /api/tags", func(t *testing.T) {
		var versionCalls, tagsCalls, otherCalls int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/version":
				atomic.AddInt32(&versionCalls, 1)
				_ = json.NewEncoder(w).Encode(map[string]any{"version": "0.1.32"})
			case r.Method == http.MethodGet && r.URL.Path == "/api/tags":
				atomic.AddInt32(&tagsCalls, 1)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"models": []map[string]any{
						{
							"name":   "llama3:latest",
							"model":  "llama3:latest",
							"digest": "365c0bd3c000a45d28dd41f479a500350203b80f010317355113d8ac5bc15084",
						},
					},
				})
			default:
				atomic.AddInt32(&otherCalls, 1)
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		comp, err := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{
				ep.EndpointID: server.URL,
			},
		})
		if err != nil {
			t.Fatalf("unexpected New error: %v", err)
		}

		opened, err := comp.Open(context.Background(), ep)
		if err != nil {
			t.Fatalf("unexpected Open error: %v", err)
		}

		if atomic.LoadInt32(&versionCalls) != 1 {
			t.Errorf("versionCalls = %d, want 1", atomic.LoadInt32(&versionCalls))
		}
		if atomic.LoadInt32(&tagsCalls) != 1 {
			t.Errorf("tagsCalls = %d, want 1", atomic.LoadInt32(&tagsCalls))
		}
		if atomic.LoadInt32(&otherCalls) != 0 {
			t.Errorf("otherCalls = %d, want 0", atomic.LoadInt32(&otherCalls))
		}

		// Verify observation and driver invariant
		if opened.Driver == nil {
			t.Fatal("opened.Driver is nil")
		}
		if opened.Observed.DriverID != opened.Driver.ID() {
			t.Errorf("Observed.DriverID (%q) != opened.Driver.ID() (%q)", opened.Observed.DriverID, opened.Driver.ID())
		}
		if err := execpolicy.AssertDriverID(opened.Driver.ID(), opened.Observed); err != nil {
			t.Errorf("AssertDriverID failed: %v", err)
		}
		if opened.Observed.RuntimeVersion != "0.1.32" {
			t.Errorf("RuntimeVersion = %q, want '0.1.32'", opened.Observed.RuntimeVersion)
		}
		wantRevision := "sha256:365c0bd3c000a45d28dd41f479a500350203b80f010317355113d8ac5bc15084"
		if opened.Observed.ModelRevision != wantRevision {
			t.Errorf("ModelRevision = %q, want %q", opened.Observed.ModelRevision, wantRevision)
		}

		// Bind endpoint
		bound, err := execpolicy.Bind(ep, opened.Observed)
		if err != nil {
			t.Fatalf("unexpected Bind error: %v", err)
		}
		if bound.BindingDigest == "" {
			t.Fatal("expected non-empty BindingDigest")
		}
		if bound.DriverID != opened.Driver.ID() {
			t.Errorf("bound.DriverID = %q, want %q", bound.DriverID, opened.Driver.ID())
		}
		if bound.RuntimeVersion != "0.1.32" {
			t.Errorf("bound.RuntimeVersion = %q, want '0.1.32'", bound.RuntimeVersion)
		}
		if bound.ModelRevision != wantRevision {
			t.Errorf("bound.ModelRevision = %q, want %q", bound.ModelRevision, wantRevision)
		}
	})

	t.Run("changing driver id changes BindingDigest", func(t *testing.T) {
		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		obsA := execpolicy.EndpointObservation{
			DriverID:       "driver-a",
			RuntimeVersion: "1.0.0",
			ModelRevision:  "sha256:aaaa",
		}
		obsB := execpolicy.EndpointObservation{
			DriverID:       "driver-b",
			RuntimeVersion: "1.0.0",
			ModelRevision:  "sha256:aaaa",
		}

		boundA, errA := execpolicy.Bind(ep, obsA)
		if errA != nil {
			t.Fatalf("Bind A failed: %v", errA)
		}
		boundB, errB := execpolicy.Bind(ep, obsB)
		if errB != nil {
			t.Fatalf("Bind B failed: %v", errB)
		}

		if boundA.BindingDigest == boundB.BindingDigest {
			t.Fatalf("expected different BindingDigest for different DriverID, got %q", boundA.BindingDigest)
		}
	})

	t.Run("missing metadata yields empty observation and Bind denies", func(t *testing.T) {
		t.Run("missing runtime version", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/version" {
					_ = json.NewEncoder(w).Encode(map[string]any{"version": ""}) // empty version
				} else if r.URL.Path == "/api/tags" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"models": []map[string]any{
							{"name": "llama3:latest", "digest": "365c0bd3c000a45d28dd41f479a500350203b80f010317355113d8ac5bc15084"},
						},
					})
				}
			}))
			defer server.Close()

			ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
			comp, _ := sessionclients.New(sessionclients.Options{
				LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
			})
			opened, err := comp.Open(context.Background(), ep)
			if err != nil {
				t.Fatalf("unexpected Open error: %v", err)
			}
			if opened.Observed.RuntimeVersion != "" {
				t.Errorf("expected empty RuntimeVersion, got %q", opened.Observed.RuntimeVersion)
			}

			_, bindErr := execpolicy.Bind(ep, opened.Observed)
			if bindErr == nil || !strings.Contains(bindErr.Error(), "runtime-version-unknown") {
				t.Errorf("expected runtime-version-unknown, got %v", bindErr)
			}
		})

		t.Run("version too long (> 64 bytes)", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/version" {
					_ = json.NewEncoder(w).Encode(map[string]any{"version": strings.Repeat("v", 65)})
				} else if r.URL.Path == "/api/tags" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"models": []map[string]any{
							{"name": "llama3:latest", "digest": "365c0bd3c000a45d28dd41f479a500350203b80f010317355113d8ac5bc15084"},
						},
					})
				}
			}))
			defer server.Close()

			ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
			comp, _ := sessionclients.New(sessionclients.Options{
				LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
			})
			opened, err := comp.Open(context.Background(), ep)
			if err != nil {
				t.Fatalf("unexpected Open error: %v", err)
			}
			if opened.Observed.RuntimeVersion != "" {
				t.Errorf("expected empty RuntimeVersion for >64 bytes, got %q", opened.Observed.RuntimeVersion)
			}

			_, bindErr := execpolicy.Bind(ep, opened.Observed)
			if bindErr == nil || !strings.Contains(bindErr.Error(), "runtime-version-unknown") {
				t.Errorf("expected runtime-version-unknown, got %v", bindErr)
			}
		})

		t.Run("missing model in tags", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/version" {
					_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.0.0"})
				} else if r.URL.Path == "/api/tags" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"models": []map[string]any{
							{"name": "different-model", "digest": "365c0bd3c000a45d28dd41f479a500350203b80f010317355113d8ac5bc15084"},
						},
					})
				}
			}))
			defer server.Close()

			ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
			comp, _ := sessionclients.New(sessionclients.Options{
				LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
			})
			opened, err := comp.Open(context.Background(), ep)
			if err != nil {
				t.Fatalf("unexpected Open error: %v", err)
			}
			if opened.Observed.ModelRevision != "" {
				t.Errorf("expected empty ModelRevision, got %q", opened.Observed.ModelRevision)
			}

			_, bindErr := execpolicy.Bind(ep, opened.Observed)
			if bindErr == nil || !strings.Contains(bindErr.Error(), "model-revision-unknown") {
				t.Errorf("expected model-revision-unknown, got %v", bindErr)
			}
		})

		t.Run("invalid digest in tags", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/version" {
					_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.0.0"})
				} else if r.URL.Path == "/api/tags" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"models": []map[string]any{
							{"name": "llama3:latest", "digest": "not-valid-hex!"},
						},
					})
				}
			}))
			defer server.Close()

			ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
			comp, _ := sessionclients.New(sessionclients.Options{
				LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
			})
			opened, err := comp.Open(context.Background(), ep)
			if err != nil {
				t.Fatalf("unexpected Open error: %v", err)
			}
			if opened.Observed.ModelRevision != "" {
				t.Errorf("expected empty ModelRevision, got %q", opened.Observed.ModelRevision)
			}

			_, bindErr := execpolicy.Bind(ep, opened.Observed)
			if bindErr == nil || !strings.Contains(bindErr.Error(), "model-revision-unknown") {
				t.Errorf("expected model-revision-unknown, got %v", bindErr)
			}
		})
	})
}

func TestBindingFor_SourceTableAndMutations(t *testing.T) {
	ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
	obs := execpolicy.EndpointObservation{
		DriverID:       "driver-ollama-1",
		RuntimeVersion: "0.1.32",
		ModelRevision:  "sha256:365c0bd3c000a45d28dd41f479a500350203b80f010317355113d8ac5bc15084",
	}

	bound, err := execpolicy.Bind(ep, obs)
	if err != nil {
		t.Fatalf("unexpected Bind error: %v", err)
	}

	binding, err := sessionclients.BindingFor(bound)
	if err != nil {
		t.Fatalf("unexpected BindingFor error: %v", err)
	}

	// Validate exact fields against source table
	if binding.EndpointID != bound.EndpointID {
		t.Errorf("EndpointID = %q, want %q", binding.EndpointID, bound.EndpointID)
	}
	if binding.DriverID != bound.DriverID {
		t.Errorf("DriverID = %q, want %q", binding.DriverID, bound.DriverID)
	}
	if binding.ModelID != bound.ModelID {
		t.Errorf("ModelID = %q, want %q", binding.ModelID, bound.ModelID)
	}
	if binding.ModelRevision != bound.ModelRevision {
		t.Errorf("ModelRevision = %q, want %q", binding.ModelRevision, bound.ModelRevision)
	}
	if binding.ChannelID != bound.Channel.ChannelID {
		t.Errorf("ChannelID = %q, want %q", binding.ChannelID, bound.Channel.ChannelID)
	}
	if binding.CapabilityClass != "local_small" {
		t.Errorf("CapabilityClass = %q, want 'local_small'", binding.CapabilityClass)
	}
	if binding.RuntimeVersion != bound.RuntimeVersion {
		t.Errorf("RuntimeVersion = %q, want %q", binding.RuntimeVersion, bound.RuntimeVersion)
	}
	wantCtxDigest, _ := protocol.Digest(bound.ContextProfile)
	if binding.ContextProfileDigest != wantCtxDigest {
		t.Errorf("ContextProfileDigest = %q, want %q", binding.ContextProfileDigest, wantCtxDigest)
	}
	if binding.PolicyDigest != bound.PolicyDigest {
		t.Errorf("PolicyDigest = %q, want %q", binding.PolicyDigest, bound.PolicyDigest)
	}
	if binding.SubscriptionQuotaUnit != "unknown" {
		t.Errorf("SubscriptionQuotaUnit = %q, want 'unknown'", binding.SubscriptionQuotaUnit)
	}

	t.Run("capability class mappings for all kinds", func(t *testing.T) {
		kinds := map[protocol.EndpointKind]string{
			protocol.EndpointLocalRuntime:     "local_small",
			protocol.EndpointAuthenticatedCLI: "subscription_cli",
			protocol.EndpointRemoteAPI:        "frontier_api",
		}
		for k, wantClass := range kinds {
			b := bound
			b.Kind = k
			bBinding, err := sessionclients.BindingFor(b)
			if err != nil {
				t.Fatalf("unexpected BindingFor error for %v: %v", k, err)
			}
			if bBinding.CapabilityClass != wantClass {
				t.Errorf("Kind %v: CapabilityClass = %q, want %q", k, bBinding.CapabilityClass, wantClass)
			}
		}

		bInvalid := bound
		bInvalid.Kind = protocol.EndpointKind("unsupported")
		_, err := sessionclients.BindingFor(bInvalid)
		if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for invalid kind, got %v", err)
		}
	})

	t.Run("unbound endpoint returns error", func(t *testing.T) {
		t.Run("empty BindingDigest", func(t *testing.T) {
			unbound := bound
			unbound.BindingDigest = ""
			_, err := sessionclients.BindingFor(unbound)
			if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument for empty BindingDigest, got %v", err)
			}
		})

		t.Run("empty RuntimeVersion", func(t *testing.T) {
			unbound := bound
			unbound.RuntimeVersion = ""
			_, err := sessionclients.BindingFor(unbound)
			if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument for empty RuntimeVersion, got %v", err)
			}
		})

		t.Run("empty DriverID", func(t *testing.T) {
			unbound := bound
			unbound.DriverID = ""
			_, err := sessionclients.BindingFor(unbound)
			if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument for empty DriverID, got %v", err)
			}
		})

		t.Run("empty ModelRevision", func(t *testing.T) {
			unbound := bound
			unbound.ModelRevision = ""
			_, err := sessionclients.BindingFor(unbound)
			if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument for empty ModelRevision, got %v", err)
			}
		})
	})

	t.Run("mutations alter output fields", func(t *testing.T) {
		// Mutating DriverID
		bDriver := bound
		bDriver.DriverID = "driver-mutated"
		resDriver, _ := sessionclients.BindingFor(bDriver)
		if resDriver.DriverID != "driver-mutated" {
			t.Errorf("DriverID not mutated: %q", resDriver.DriverID)
		}

		// Mutating ChannelID
		bChan := bound
		bChan.Channel.ChannelID = "chan-mutated"
		resChan, _ := sessionclients.BindingFor(bChan)
		if resChan.ChannelID != "chan-mutated" {
			t.Errorf("ChannelID not mutated: %q", resChan.ChannelID)
		}

		// Mutating RuntimeVersion
		bVer := bound
		bVer.RuntimeVersion = "2.0.0"
		resVer, _ := sessionclients.BindingFor(bVer)
		if resVer.RuntimeVersion != "2.0.0" {
			t.Errorf("RuntimeVersion not mutated: %q", resVer.RuntimeVersion)
		}

		// Mutating ContextProfile
		bProf := bound
		bProf.ContextProfile.ModelRef = "mutated-model"
		resProf, _ := sessionclients.BindingFor(bProf)
		mutatedDigest, _ := protocol.Digest(bProf.ContextProfile)
		if resProf.ContextProfileDigest != mutatedDigest {
			t.Errorf("ContextProfileDigest not mutated: %q vs %q", resProf.ContextProfileDigest, mutatedDigest)
		}
	})
}

func TestComposition_DriverSessionEndToEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "0.1.32"})
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{
						"name":   "llama3:latest",
						"digest": "365c0bd3c000a45d28dd41f479a500350203b80f010317355113d8ac5bc15084",
					},
				},
			})
		case "/api/chat":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "llama3:latest",
				"message": map[string]any{
					"role":    "assistant",
					"content": "Turn completed successfully!",
				},
				"done":              true,
				"prompt_eval_count": 15,
				"eval_count":        25,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
	comp, err := sessionclients.New(sessionclients.Options{
		LoopbackBaseURLs: map[string]string{
			ep.EndpointID: server.URL,
		},
	})
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}

	opened, err := comp.Open(context.Background(), ep)
	if err != nil {
		t.Fatalf("unexpected Open error: %v", err)
	}

	session, err := opened.Driver.StartSession(context.Background(), drivers.SessionConfig{
		SessionID:              "sess-test-1",
		ModelID:                ep.ModelID,
		MaxOutputTokensPerCall: 2048,
	})
	if err != nil {
		t.Fatalf("unexpected StartSession error: %v", err)
	}
	defer func() { _ = session.Close(context.Background()) }()

	turnRes, err := session.ExecuteTurn(context.Background(), drivers.TurnInput{
		TurnID: "turn-1",
		Prompt: "Hello assistant",
	})
	if err != nil {
		t.Fatalf("unexpected ExecuteTurn error: %v", err)
	}

	if turnRes.Content != "Turn completed successfully!" {
		t.Errorf("turnRes.Content = %q, want 'Turn completed successfully!'", turnRes.Content)
	}
}

func TestComposition_BindingFor_Method(t *testing.T) {
	comp, err := sessionclients.New(sessionclients.Options{})
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}
	ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
	obs := execpolicy.EndpointObservation{
		DriverID:       "driver-1",
		RuntimeVersion: "1.0",
		ModelRevision:  "sha256:1234",
	}
	bound, _ := execpolicy.Bind(ep, obs)
	binding, err := comp.BindingFor(bound)
	if err != nil {
		t.Fatalf("unexpected BindingFor error: %v", err)
	}
	if binding.DriverID != "driver-1" {
		t.Errorf("DriverID = %q, want 'driver-1'", binding.DriverID)
	}
}

func TestComposition_Open_ProbeErrors(t *testing.T) {
	t.Run("/api/version returns 500 error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/version" {
				http.Error(w, "server error", http.StatusInternalServerError)
			}
		}))
		defer server.Close()

		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		comp, _ := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
		})
		_, err := comp.Open(context.Background(), ep)
		if err == nil || !errors.Is(err, errs.ErrModelUnavailable) {
			t.Errorf("expected ErrModelUnavailable, got %v", err)
		}
	})

	t.Run("/api/version returns invalid json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/version" {
				_, _ = w.Write([]byte("invalid json"))
			}
		}))
		defer server.Close()

		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		comp, _ := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
		})
		_, err := comp.Open(context.Background(), ep)
		if err == nil || !errors.Is(err, errs.ErrProbeFailed) {
			t.Errorf("expected ErrProbeFailed, got %v", err)
		}
	})

	t.Run("/api/tags returns 500 error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/version" {
				_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.0"})
			} else if r.URL.Path == "/api/tags" {
				http.Error(w, "server error", http.StatusInternalServerError)
			}
		}))
		defer server.Close()

		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		comp, _ := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
		})
		_, err := comp.Open(context.Background(), ep)
		if err == nil || !errors.Is(err, errs.ErrModelUnavailable) {
			t.Errorf("expected ErrModelUnavailable, got %v", err)
		}
	})

	t.Run("/api/tags returns invalid json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/version" {
				_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.0"})
			} else if r.URL.Path == "/api/tags" {
				_, _ = w.Write([]byte("invalid json"))
			}
		}))
		defer server.Close()

		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		comp, _ := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
		})
		_, err := comp.Open(context.Background(), ep)
		if err == nil || !errors.Is(err, errs.ErrProbeFailed) {
			t.Errorf("expected ErrProbeFailed, got %v", err)
		}
	})

	t.Run("/api/version connection error on closed port", func(t *testing.T) {
		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		comp, _ := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{ep.EndpointID: "http://127.0.0.1:54321"},
		})
		_, err := comp.Open(context.Background(), ep)
		if err == nil || !errors.Is(err, errs.ErrModelUnavailable) {
			t.Errorf("expected ErrModelUnavailable, got %v", err)
		}
	})

	t.Run("/api/tags connection error on closed connection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/version" {
				_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.0"})
				return
			}
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}
		}))
		defer server.Close()

		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		comp, _ := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
		})
		_, err := comp.Open(context.Background(), ep)
		if err == nil || !errors.Is(err, errs.ErrModelUnavailable) {
			t.Errorf("expected ErrModelUnavailable, got %v", err)
		}
	})

	t.Run("default loopback URL used when endpoint not in map", func(t *testing.T) {
		comp, _ := sessionclients.New(sessionclients.Options{})
		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		ep.EndpointID = "unmapped-ep"
		// Exercises default loopback URL fallback
		_, _ = comp.Open(context.Background(), ep)
	})
}

func TestComposition_ProbeVersion_ControlCharacters(t *testing.T) {
	t.Run("ASCII control characters are stripped from version", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/version":
				// Version containing null byte, unit separator, and DEL (0x7f)
				_ = json.NewEncoder(w).Encode(map[string]any{"version": "0.1.32\x00\x1f\x7f"})
			case "/api/tags":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"models": []map[string]any{
						{"name": "llama3:latest", "digest": "365c0bd3c000a45d28dd41f479a500350203b80f010317355113d8ac5bc15084"},
					},
				})
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		comp, err := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
		})
		if err != nil {
			t.Fatalf("unexpected New error: %v", err)
		}

		opened, err := comp.Open(context.Background(), ep)
		if err != nil {
			t.Fatalf("unexpected Open error: %v", err)
		}
		if opened.Observed.RuntimeVersion != "0.1.32" {
			t.Errorf("RuntimeVersion = %q, want '0.1.32'", opened.Observed.RuntimeVersion)
		}
	})

	t.Run("version with only control characters yields empty observation and Bind denies", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/version":
				_ = json.NewEncoder(w).Encode(map[string]any{"version": "\x00\x1f\x7f\t\r\n"})
			case "/api/tags":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"models": []map[string]any{
						{"name": "llama3:latest", "digest": "365c0bd3c000a45d28dd41f479a500350203b80f010317355113d8ac5bc15084"},
					},
				})
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		comp, err := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
		})
		if err != nil {
			t.Fatalf("unexpected New error: %v", err)
		}

		opened, err := comp.Open(context.Background(), ep)
		if err != nil {
			t.Fatalf("unexpected Open error: %v", err)
		}
		if opened.Observed.RuntimeVersion != "" {
			t.Errorf("expected empty RuntimeVersion, got %q", opened.Observed.RuntimeVersion)
		}

		_, bindErr := execpolicy.Bind(ep, opened.Observed)
		if bindErr == nil || !strings.Contains(bindErr.Error(), "runtime-version-unknown") {
			t.Errorf("expected runtime-version-unknown, got %v", bindErr)
		}
	})
}

func TestComposition_ProbeTags_HexDigestValidation(t *testing.T) {
	testCases := []struct {
		name       string
		digest     string
		wantRev    string
		expectDeny bool
	}{
		{
			name:       "short hex digest (<64 hex) rejected",
			digest:     "sha256:abcd1234",
			wantRev:    "",
			expectDeny: true,
		},
		{
			name:       "long hex digest (>64 hex) rejected",
			digest:     "sha256:" + strings.Repeat("a", 65),
			wantRev:    "",
			expectDeny: true,
		},
		{
			name:       "64-char non-hex characters rejected",
			digest:     "sha256:" + strings.Repeat("z", 64),
			wantRev:    "",
			expectDeny: true,
		},
		{
			name:       "raw 64-char hex without prefix accepted",
			digest:     strings.Repeat("a", 64),
			wantRev:    "sha256:" + strings.Repeat("a", 64),
			expectDeny: false,
		},
		{
			name:       "valid sha256: prefix with 64 hex characters accepted",
			digest:     "sha256:" + strings.Repeat("f", 64),
			wantRev:    "sha256:" + strings.Repeat("f", 64),
			expectDeny: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/version":
					_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.0.0"})
				case "/api/tags":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"models": []map[string]any{
							{"name": "llama3:latest", "digest": tc.digest},
						},
					})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
			comp, err := sessionclients.New(sessionclients.Options{
				LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
			})
			if err != nil {
				t.Fatalf("unexpected New error: %v", err)
			}

			opened, err := comp.Open(context.Background(), ep)
			if err != nil {
				t.Fatalf("unexpected Open error: %v", err)
			}
			if opened.Observed.ModelRevision != tc.wantRev {
				t.Errorf("ModelRevision = %q, want %q", opened.Observed.ModelRevision, tc.wantRev)
			}

			_, bindErr := execpolicy.Bind(ep, opened.Observed)
			if tc.expectDeny {
				if bindErr == nil || !strings.Contains(bindErr.Error(), "model-revision-unknown") {
					t.Errorf("expected model-revision-unknown, got %v", bindErr)
				}
			} else {
				if bindErr != nil {
					t.Errorf("unexpected Bind error: %v", bindErr)
				}
			}
		})
	}
}

func TestComposition_Open_ProbeTimeout_MaxDurationSeconds(t *testing.T) {
	t.Run("probeVersion times out when MaxDurationSeconds is reached", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/version" {
				<-r.Context().Done()
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		ep.Limits.MaxDurationSeconds = 1 // 1 second timeout

		comp, err := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
		})
		if err != nil {
			t.Fatalf("unexpected New error: %v", err)
		}

		start := time.Now()
		_, err = comp.Open(context.Background(), ep)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
		if !errors.Is(err, errs.ErrProbeTimeout) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected ErrProbeTimeout or DeadlineExceeded, got %v", err)
		}
		if elapsed < 800*time.Millisecond || elapsed > 3*time.Second {
			t.Errorf("elapsed duration %v unexpected for 1s MaxDurationSeconds", elapsed)
		}
	})

	t.Run("probeTags times out when MaxDurationSeconds is reached", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/version":
				_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.0.0"})
			case "/api/tags":
				<-r.Context().Done()
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		ep := sampleResolvedEndpoint(protocol.EndpointLocalRuntime)
		ep.Limits.MaxDurationSeconds = 1 // 1 second timeout

		comp, err := sessionclients.New(sessionclients.Options{
			LoopbackBaseURLs: map[string]string{ep.EndpointID: server.URL},
		})
		if err != nil {
			t.Fatalf("unexpected New error: %v", err)
		}

		start := time.Now()
		_, err = comp.Open(context.Background(), ep)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
		if !errors.Is(err, errs.ErrProbeTimeout) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected ErrProbeTimeout or DeadlineExceeded, got %v", err)
		}
		if elapsed < 800*time.Millisecond || elapsed > 3*time.Second {
			t.Errorf("elapsed duration %v unexpected for 1s MaxDurationSeconds", elapsed)
		}
	})
}

package remoteapi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/cognition/remoteapi"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

func observedAt() protocol.Timestamp {
	return protocol.NewTimestamp(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC))
}

func input(depth protocol.ProbeDepth) cognition.DiscoveryInput {
	return cognition.DiscoveryInput{Depth: depth, ObservedAt: observedAt()}
}

// twoClassClient exposes two endpoints from one provider in different cost
// classes, which is the case a provider-keyed cost model would get wrong.
func twoClassClient() *remoteapi.StaticClient {
	tokens := 128000
	return &remoteapi.StaticClient{
		Endpoints: []remoteapi.Description{
			{
				EndpointID: "economy", Provider: "acme", ModelID: "acme-small",
				ModelFamily: "acme", CostClass: protocol.CostRemoteEconomy,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				Auth:                   protocol.AuthAuthenticated,
				ContextTokens:          &tokens, StructuredOutputDeclared: true,
			},
			{
				EndpointID: "frontier", Provider: "acme", ModelID: "acme-large",
				ModelFamily: "acme", CostClass: protocol.CostFrontierExpensive,
				RequiredSourceExposure: protocol.ExposureSemanticEvidenceOnly,
				Auth:                   protocol.AuthAuthenticated,
			},
		},
		Answers: map[string]string{
			"economy":  `{"ok": true}`,
			"frontier": `{"ok": true}`,
		},
	}
}

func TestNoConfiguredClientYieldsNoEndpoints(t *testing.T) {
	adapter := remoteapi.New(remoteapi.Options{})
	endpoints, err := adapter.Discover(context.Background(), input(protocol.DepthInference))
	if err != nil {
		t.Fatalf("no configured remote api must not be an error: %v", err)
	}
	if len(endpoints) != 0 {
		t.Errorf("endpoints = %+v", endpoints)
	}
}

func TestInventoryDepthMakesNoCall(t *testing.T) {
	client := twoClassClient()
	adapter := remoteapi.New(remoteapi.Options{Clients: []remoteapi.Client{client}})
	endpoints, err := adapter.Discover(context.Background(), input(protocol.DepthInventory))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(endpoints) != 0 {
		t.Errorf("an inventory-depth query described remote endpoints: %+v", endpoints)
	}
}

// TestOneProviderCanExposeSeveralCostClasses is the reason cost belongs to the
// endpoint rather than the provider.
func TestOneProviderCanExposeSeveralCostClasses(t *testing.T) {
	adapter := remoteapi.New(remoteapi.Options{Clients: []remoteapi.Client{twoClassClient()}})
	endpoints, err := adapter.Discover(context.Background(), input(protocol.DepthHealth))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	byID := map[string]protocol.CognitionEndpoint{}
	for _, endpoint := range endpoints {
		byID[endpoint.ID] = endpoint
		if endpoint.Provider != "acme" {
			t.Errorf("%s provider = %q", endpoint.ID, endpoint.Provider)
		}
		// Describing an endpoint is not evidence that it answers.
		if endpoint.Health != protocol.EndpointHealthUnverified {
			t.Errorf("%s health = %q, want unverified", endpoint.ID, endpoint.Health)
		}
		if len(endpoint.Capabilities) != 0 {
			t.Errorf("%s was graded from its own self-description: %+v", endpoint.ID, endpoint.Capabilities)
		}
		if endpoint.Acceleration != nil {
			t.Errorf("%s carries local acceleration evidence", endpoint.ID)
		}
		if err := endpoint.Validate(); err != nil {
			t.Errorf("%s violates the contract: %v", endpoint.ID, err)
		}
	}
	if byID["api:economy"].CostClass != protocol.CostRemoteEconomy {
		t.Errorf("economy cost = %q", byID["api:economy"].CostClass)
	}
	if byID["api:frontier"].CostClass != protocol.CostFrontierExpensive {
		t.Errorf("frontier cost = %q", byID["api:frontier"].CostClass)
	}
	// Model family is retained for the later independence question.
	if byID["api:economy"].ModelFamily != "acme" {
		t.Errorf("model family was dropped")
	}
	if byID["api:economy"].ContextTokens == nil || *byID["api:economy"].ContextTokens != 128000 {
		t.Errorf("context tokens = %v", byID["api:economy"].ContextTokens)
	}
}

func TestAnAnsweringEndpointBecomesReady(t *testing.T) {
	client := twoClassClient()
	adapter := remoteapi.New(remoteapi.Options{Clients: []remoteapi.Client{client}})
	result, err := adapter.Probe(context.Background(),
		cognition.RemoteEndpoint("api:economy", "acme", protocol.CostRemoteEconomy),
		cognition.ProbeRequest{RequireStructuredOutput: true})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Status != protocol.FindingObserved {
		t.Fatalf("status = %q, detail = %q", result.Status, result.Detail)
	}
	if result.StructuredOutput != protocol.FeatureProbePassed {
		t.Errorf("structured output = %q", result.StructuredOutput)
	}
	// Token counts come from the provider, not from the response length.
	byName := map[string]float64{}
	for _, measurement := range result.Measurements {
		byName[measurement.Name] = measurement.Value
	}
	if byName["prompt_tokens"] != 12 || byName["generation_tokens"] != 8 {
		t.Errorf("measurements = %v", byName)
	}
	if result.Backend != "" || len(result.Signals) != 0 {
		t.Error("a remote endpoint produced local acceleration evidence")
	}
}

func TestUnauthenticatedAndTimedOutProvidersAreDistinguished(t *testing.T) {
	for name, tc := range map[string]struct {
		err      error
		want     protocol.FindingStatus
		wantAuth protocol.AuthStatus
	}{
		"unauthenticated": {
			errs.New(errs.CategoryUnauthenticated, "401"), protocol.FindingError, protocol.AuthUnauthenticated,
		},
		"timeout": {
			errs.New(errs.CategoryProbeTimeout, "deadline"), protocol.FindingTimeout, "",
		},
		"other failure": {
			errs.New(errs.CategoryProbeFailed, "503"), protocol.FindingError, "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := twoClassClient()
			client.Errors = map[string]error{"economy": tc.err}
			adapter := remoteapi.New(remoteapi.Options{Clients: []remoteapi.Client{client}})
			result, err := adapter.Probe(context.Background(),
				cognition.RemoteEndpoint("api:economy", "acme", protocol.CostRemoteEconomy),
				cognition.ProbeRequest{})
			if err != nil {
				t.Fatalf("a provider failure must be a fact: %v", err)
			}
			if result.Status != tc.want {
				t.Errorf("status = %q, want %q", result.Status, tc.want)
			}
			if result.Auth != tc.wantAuth {
				t.Errorf("auth = %q, want %q", result.Auth, tc.wantAuth)
			}
		})
	}
}

// TestASecretShapedReferenceIsRefusedAtTheBoundary keeps a raw key from a badly
// written provider adapter out of a durable record.
func TestASecretShapedReferenceIsRefusedAtTheBoundary(t *testing.T) {
	for name, description := range map[string]remoteapi.Description{
		"api key as credential ref": {EndpointID: "x", CredentialRef: "sk-live-abcdefghij"},
		"long token as account ref": {EndpointID: "x", AccountRef: strings.Repeat("q", 200)},
	} {
		t.Run(name, func(t *testing.T) {
			client := &remoteapi.StaticClient{Endpoints: []remoteapi.Description{description}}
			adapter := remoteapi.New(remoteapi.Options{Clients: []remoteapi.Client{client}})
			_, err := adapter.Discover(context.Background(), input(protocol.DepthHealth))
			if err == nil {
				t.Fatal("a secret-shaped reference passed the boundary")
			}
			if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
				t.Errorf("category = %q", errs.CategoryOf(err))
			}
		})
	}
}

// TestTheProbeSendsOnlyTheSyntheticPrompt is the no-source guarantee.
func TestTheProbeSendsOnlyTheSyntheticPrompt(t *testing.T) {
	client := twoClassClient()
	adapter := remoteapi.New(remoteapi.Options{Clients: []remoteapi.Client{client}})
	if _, err := adapter.Probe(context.Background(),
		cognition.RemoteEndpoint("api:economy", "acme", protocol.CostRemoteEconomy),
		cognition.ProbeRequest{}); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(client.Prompts) != 1 {
		t.Fatalf("prompts = %v", client.Prompts)
	}
	if client.Prompts[0] != cognition.SyntheticProbePrompt {
		t.Errorf("prompt = %q, want the synthetic prompt", client.Prompts[0])
	}
}

func TestCancellationIsPropagated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := twoClassClient()
	adapter := remoteapi.New(remoteapi.Options{Clients: []remoteapi.Client{client}})
	result, err := adapter.Probe(ctx,
		cognition.RemoteEndpoint("api:economy", "acme", protocol.CostRemoteEconomy),
		cognition.ProbeRequest{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Status != protocol.FindingTimeout {
		t.Errorf("status = %q, want timeout after cancellation", result.Status)
	}
}

// TestADescribeFailureIsReportedNotSwallowed keeps a broken client visible while
// the service isolates it.
func TestADescribeFailureIsReportedNotSwallowed(t *testing.T) {
	client := &remoteapi.StaticClient{DescribeErr: errs.New(errs.CategoryProbeFailed, "dns failure")}
	adapter := remoteapi.New(remoteapi.Options{Clients: []remoteapi.Client{client}})
	if _, err := adapter.Discover(context.Background(), input(protocol.DepthHealth)); err == nil {
		t.Fatal("a failing client was silently ignored")
	}
}

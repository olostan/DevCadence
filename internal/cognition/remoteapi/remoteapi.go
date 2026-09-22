// Package remoteapi is the adapter boundary for direct remote model APIs.
//
// M3A implements the boundary, not a provider. That is a deliberate scope
// choice: the domain question — can a remote API be discovered, described,
// health-checked, cost-classed, privacy-constrained and routed like any other
// endpoint? — is answered by the boundary and a deterministic Client, and
// implementing one vendor's HTTP surface would prove nothing further while
// adding a dependency and a credential path this milestone does not need.
//
// The boundary's job is to keep provider concerns out of the core. A real
// provider adapter imports its SDK *here*, in a package nothing in
// internal/protocol, internal/cognition or the control plane imports back; the
// core only ever sees protocol types (DCI-055). A boundary test asserts that.
//
// No credential ever passes through a ProbeRequest or lands in an endpoint
// record. A Client is constructed with whatever authentication it needs, and the
// endpoint carries only an opaque CredentialRef (DCI-081).
package remoteapi

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// AdapterID is the stable adapter identity and the prefix of its endpoint ids.
const AdapterID = "api"

// Description is what a provider client discloses about one endpoint.
//
// It is what a *client* knows, not what DevCadence concludes: capability grades
// are absent because no provider's self-description is evidence of capability
// (DCI-012). An operator who knows better declares grades through
// cognition.Declaration, where they are recorded as configuration.
type Description struct {
	// EndpointID is the suffix; the adapter prefixes it with AdapterID.
	EndpointID string
	Provider   string
	ModelID    string
	// ModelFamily is retained so M6 can avoid treating two frontends over the
	// same underlying model as independent reviewers.
	ModelFamily string
	// AccountRef is an opaque, non-secret account handle. Never a key.
	AccountRef string
	// CredentialRef is an opaque reference to a credential held elsewhere.
	CredentialRef string
	// Auth is what the client knows about its own authentication. A client that
	// cannot tell reports AuthUnknown.
	Auth protocol.AuthStatus
	// CostClass and RequiredSourceExposure are the endpoint's policy
	// properties. Cost belongs to the endpoint, not the provider: one provider
	// commonly offers several models in different classes.
	CostClass              protocol.CostClass
	RequiredSourceExposure protocol.SourceExposure
	ContextTokens          *int
	// StructuredOutput and ToolUse are what the provider documents. They are
	// recorded as FeatureDeclared until a probe confirms them.
	StructuredOutputDeclared bool
	ToolUseDeclared          bool
}

// Completion is a client's answer to a synthetic probe.
type Completion struct {
	Text string
	// PromptTokens and GenerationTokens are the provider's own counts. They are
	// recorded only when the provider supplied them; nothing is estimated.
	PromptTokens     int
	GenerationTokens int
	Duration         time.Duration
}

// Client is one provider's implementation.
//
// A real client wraps a provider SDK. It must not accept repository content:
// Complete is called only with the synthetic probe prompt.
type Client interface {
	// Describe lists the endpoints this client can serve.
	Describe(ctx context.Context) ([]Description, error)
	// Complete runs one small completion for a health probe.
	Complete(ctx context.Context, endpointID, prompt string, maxTokens int) (Completion, error)
}

// Options configures an Adapter.
type Options struct {
	Clients []Client
}

// Adapter adapts remote API clients to the CognitionEndpoint contract.
type Adapter struct {
	clients []Client
}

// New returns an Adapter. Zero clients is valid: a machine with no configured
// remote API simply has no remote endpoints.
func New(opts Options) *Adapter {
	return &Adapter{clients: append([]Client(nil), opts.Clients...)}
}

// ID implements cognition.Adapter.
func (a *Adapter) ID() string { return AdapterID }

// Discover implements cognition.Adapter.
func (a *Adapter) Discover(ctx context.Context, in cognition.DiscoveryInput) ([]protocol.CognitionEndpoint, error) {
	var endpoints []protocol.CognitionEndpoint
	for _, client := range a.clients {
		if !in.Depth.AtLeast(protocol.DepthHealth) {
			// Describing may require a network call, which an inventory-depth
			// query must not make.
			continue
		}
		described, err := client.Describe(ctx)
		if err != nil {
			// One failing client must not cost the others their endpoints
			// (DCI-104); the service records the adapter-level finding.
			return endpoints, err
		}
		for _, description := range described {
			endpoint, err := toEndpoint(description, in.ObservedAt)
			if err != nil {
				return endpoints, err
			}
			endpoints = append(endpoints, endpoint)
		}
	}
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].ID < endpoints[j].ID })
	return endpoints, nil
}

// toEndpoint converts a description, refusing anything secret-shaped.
func toEndpoint(description Description, observedAt protocol.Timestamp) (protocol.CognitionEndpoint, error) {
	if description.EndpointID == "" {
		return protocol.CognitionEndpoint{},
			errs.New(errs.CategoryInvalidArgument, "remoteapi: a description requires an endpoint id")
	}
	// A client that hands over a raw key instead of a reference is refused at
	// the boundary, before the value can reach a durable record (DCI-081).
	for field, value := range map[string]string{
		"credential_ref": description.CredentialRef,
		"account_ref":    description.AccountRef,
	} {
		if looksLikeSecret(value) {
			return protocol.CognitionEndpoint{}, errs.New(errs.CategoryInvalidArgument,
				"remoteapi: %s for %s looks like a secret value; it must be an opaque reference",
				field, description.EndpointID)
		}
	}
	endpoint := cognition.RemoteEndpoint(AdapterID+":"+description.EndpointID,
		description.Provider, orUnknownCost(description.CostClass))
	endpoint.ModelID = description.ModelID
	endpoint.ModelFamily = description.ModelFamily
	endpoint.AccountRef = description.AccountRef
	endpoint.CredentialRef = description.CredentialRef
	endpoint.ObservedAt = observedAt
	if description.Auth != "" {
		endpoint.Auth = description.Auth
	}
	if description.RequiredSourceExposure != "" {
		endpoint.RequiredSourceExposure = description.RequiredSourceExposure
	}
	if description.ContextTokens != nil {
		tokens := *description.ContextTokens
		endpoint.ContextTokens = &tokens
	}
	if description.StructuredOutputDeclared {
		endpoint.StructuredOutput = protocol.FeatureDeclared
	}
	if description.ToolUseDeclared {
		endpoint.ToolUse = protocol.FeatureDeclared
	}
	// Health stops at unverified: a provider describing an endpoint is not
	// evidence that it answers.
	endpoint.Health = protocol.EndpointHealthUnverified
	return endpoint, nil
}

// Probe implements cognition.Adapter.
func (a *Adapter) Probe(
	ctx context.Context,
	endpoint protocol.CognitionEndpoint,
	req cognition.ProbeRequest,
) (cognition.ProbeResult, error) {
	req = req.Normalise()
	result := cognition.ProbeResult{Status: protocol.FindingUnsupported, EvidenceMediaType: "text/plain"}
	id := strings.TrimPrefix(endpoint.ID, AdapterID+":")
	for _, client := range a.clients {
		described, err := client.Describe(ctx)
		if err != nil {
			continue
		}
		if !servesEndpoint(described, id) {
			continue
		}
		completion, err := client.Complete(ctx, id, req.Prompt, req.MaxTokens)
		if err != nil {
			switch errs.CategoryOf(err) {
			case errs.CategoryUnauthenticated:
				result.Status = protocol.FindingError
				result.Auth = protocol.AuthUnauthenticated
				result.Detail = "the provider rejected the request as unauthenticated"
			case errs.CategoryProbeTimeout:
				result.Status = protocol.FindingTimeout
				result.Detail = "the provider did not answer within its time bound"
			default:
				result.Status = protocol.FindingError
				result.Detail = "the provider did not answer"
			}
			return result, nil
		}
		result.Status = protocol.FindingObserved
		result.ModelID = endpoint.ModelID
		result.Detail = "the provider answered a synthetic prompt containing no repository content"
		result.RawEvidence = []byte(completion.Text)
		result.Measurements = measurements(completion)
		if req.RequireStructuredOutput {
			result.StructuredOutput = structuredOutcome(completion.Text)
		}
		// No Backend and no Signals: inference is remote, so there is no local
		// acceleration to claim.
		return result, nil
	}
	result.Detail = "no configured client serves this endpoint"
	return result, nil
}

func measurements(completion Completion) []protocol.Measurement {
	var out []protocol.Measurement
	if completion.Duration > 0 {
		out = append(out, protocol.Measurement{
			Name: "request_duration_ms", Value: float64(completion.Duration.Milliseconds()),
			Unit: "ms", Source: "remote_api",
		})
	}
	if completion.PromptTokens > 0 {
		out = append(out, protocol.Measurement{
			Name: "prompt_tokens", Value: float64(completion.PromptTokens),
			Unit: "tokens", Source: "remote_api",
		})
	}
	if completion.GenerationTokens > 0 {
		out = append(out, protocol.Measurement{
			Name: "generation_tokens", Value: float64(completion.GenerationTokens),
			Unit: "tokens", Source: "remote_api",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func structuredOutcome(text string) protocol.FeatureSupport {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, "\"ok\"") {
		return protocol.FeatureProbePassed
	}
	return protocol.FeatureProbeFailed
}

func servesEndpoint(described []Description, id string) bool {
	for _, description := range described {
		if description.EndpointID == id {
			return true
		}
	}
	return false
}

func orUnknownCost(cost protocol.CostClass) protocol.CostClass {
	if cost == "" {
		return protocol.CostUnknown
	}
	return cost
}

func looksLikeSecret(value string) bool {
	if len(value) > 128 {
		return true
	}
	lowered := strings.ToLower(value)
	for _, prefix := range []string{"sk-", "sk_", "pat_", "ghp_", "bearer ", "aws_"} {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return false
}

// StaticClient is a deterministic Client for tests and for proving the boundary.
//
// It is not a mock of a particular provider; it is a complete, if trivial,
// implementation of the contract, which is what lets the remote-API path be
// exercised offline with no credentials (ENGINEERING_STANDARDS.md §19).
type StaticClient struct {
	Endpoints []Description
	// Answers maps endpoint id to the text Complete returns.
	Answers map[string]string
	// Errors maps endpoint id to a failure Complete returns.
	Errors map[string]error
	// DescribeErr makes Describe fail.
	DescribeErr error
	// Prompts records every prompt sent, so a test can assert that no
	// repository content left the process.
	Prompts []string
}

// Describe implements Client.
func (c *StaticClient) Describe(context.Context) ([]Description, error) {
	if c.DescribeErr != nil {
		return nil, c.DescribeErr
	}
	return c.Endpoints, nil
}

// Complete implements Client.
func (c *StaticClient) Complete(ctx context.Context, endpointID, prompt string, _ int) (Completion, error) {
	c.Prompts = append(c.Prompts, prompt)
	if err := ctx.Err(); err != nil {
		return Completion{}, errs.Wrap(errs.CategoryProbeTimeout, err, "remoteapi: cancelled")
	}
	if err, ok := c.Errors[endpointID]; ok {
		return Completion{}, err
	}
	answer, ok := c.Answers[endpointID]
	if !ok {
		return Completion{}, errs.New(errs.CategoryProbeFailed, "remoteapi: no scripted answer for %s", endpointID)
	}
	return Completion{
		Text: answer, PromptTokens: 12, GenerationTokens: 8, Duration: 120 * time.Millisecond,
	}, nil
}

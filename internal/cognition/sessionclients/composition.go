package sessionclients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/olostan/DevCadence/internal/benchmark/empirical"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

var _ execpolicy.DriverFactory = (*Composition)(nil)

// Composition implements execpolicy.DriverFactory for loopback local runtimes.
type Composition struct {
	loopbackBaseURLs map[string]string
	clock            clock.Clock
	httpClient       *http.Client
}

// New constructs a Composition factory.
func New(opts Options) (*Composition, error) {
	urls := make(map[string]string, len(opts.LoopbackBaseURLs))
	for epID, u := range opts.LoopbackBaseURLs {
		if err := validateLoopbackURL(u); err != nil {
			return nil, err
		}
		urls[epID] = strings.TrimSuffix(u, "/")
	}

	clk := opts.Clock
	if clk == nil {
		clk = clock.System()
	}

	return &Composition{
		loopbackBaseURLs: urls,
		clock:            clk,
		httpClient:       newLoopbackHTTPClient(),
	}, nil
}

// Open opens a driver session for the resolved endpoint, probing metadata on loopback runtimes.
func (c *Composition) Open(ctx context.Context, ep execpolicy.ResolvedEndpoint) (execpolicy.OpenedEndpoint, error) {
	switch ep.Kind {
	case protocol.EndpointAuthenticatedCLI, protocol.EndpointRemoteAPI:
		return execpolicy.OpenedEndpoint{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"driver-not-implemented"},
			fmt.Sprintf("endpoint kind %q not implemented: first slice loopback local runtime only", ep.Kind),
		)
	case protocol.EndpointLocalRuntime:
		// Supported; proceed below.
	default:
		return execpolicy.OpenedEndpoint{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"driver-not-implemented"},
			fmt.Sprintf("unsupported endpoint kind %q", ep.Kind),
		)
	}

	baseURL := c.loopbackBaseURLs[ep.EndpointID]
	if baseURL == "" {
		baseURL = DefaultLoopbackBaseURL
	}
	if err := validateLoopbackURL(baseURL); err != nil {
		return execpolicy.OpenedEndpoint{}, err
	}

	ver, err := c.probeVersion(ctx, baseURL)
	if err != nil {
		return execpolicy.OpenedEndpoint{}, err
	}

	digest, err := c.probeTags(ctx, baseURL, ep.ModelID)
	if err != nil {
		return execpolicy.OpenedEndpoint{}, err
	}

	driverID := ep.DriverID
	if driverID == "" && ep.EndpointID != "" {
		driverID = "ollama:" + ep.EndpointID
	}

	client, err := NewLoopbackClient(baseURL, ep.ModelID, ep.Limits, c.httpClient)
	if err != nil {
		return execpolicy.OpenedEndpoint{}, err
	}

	driver, err := drivers.NewDirectAPIDriver(driverID, client)
	if err != nil {
		return execpolicy.OpenedEndpoint{}, err
	}

	obs := execpolicy.EndpointObservation{
		DriverID:       driver.ID(),
		RuntimeVersion: ver,
		ModelRevision:  digest,
	}

	return execpolicy.OpenedEndpoint{
		Driver:   driver,
		Observed: obs,
	}, nil
}

// BindingFor derives the canonical empirical.EndpointBinding for a bound ResolvedEndpoint.
func BindingFor(ep execpolicy.ResolvedEndpoint) (empirical.EndpointBinding, error) {
	if ep.BindingDigest == "" || ep.RuntimeVersion == "" || ep.DriverID == "" || ep.ModelRevision == "" {
		return empirical.EndpointBinding{}, errs.New(errs.CategoryInvalidArgument,
			"endpoint is unbound: binding_digest, runtime_version, driver_id, and model_revision must be non-empty")
	}

	var capClass string
	switch ep.Kind {
	case protocol.EndpointLocalRuntime:
		capClass = "local_small"
	case protocol.EndpointAuthenticatedCLI:
		capClass = "subscription_cli"
	case protocol.EndpointRemoteAPI:
		capClass = "frontier_api"
	default:
		return empirical.EndpointBinding{}, errs.New(errs.CategoryInvalidArgument,
			"unsupported endpoint kind %q for capability class", ep.Kind)
	}

	ctxDigest, err := protocol.Digest(ep.ContextProfile)
	if err != nil {
		return empirical.EndpointBinding{}, errs.Wrap(errs.CategoryInvalidArgument, err, "compute context profile digest")
	}

	return empirical.EndpointBinding{
		EndpointID:            ep.EndpointID,
		DriverID:              ep.DriverID,
		ModelID:               ep.ModelID,
		ModelRevision:         ep.ModelRevision,
		ChannelID:             ep.Channel.ChannelID,
		CapabilityClass:       capClass,
		RuntimeVersion:        ep.RuntimeVersion,
		ContextProfileDigest:  ctxDigest,
		PolicyDigest:          ep.PolicyDigest,
		SubscriptionQuotaUnit: empirical.UnknownQuotaUnit,
	}, nil
}

// BindingFor delegates to the package-level BindingFor function.
func (c *Composition) BindingFor(ep execpolicy.ResolvedEndpoint) (empirical.EndpointBinding, error) {
	return BindingFor(ep)
}

func (c *Composition) probeVersion(ctx context.Context, baseURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/version", nil)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInvalidArgument, err, "build /api/version request")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", errs.Wrap(errs.CategoryModelUnavailable, err, "probe /api/version failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", errs.New(errs.CategoryModelUnavailable, "/api/version returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return "", errs.Wrap(errs.CategoryProbeFailed, err, "read /api/version response")
	}

	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", errs.Wrap(errs.CategoryProbeFailed, err, "decode /api/version response")
	}

	ver := strings.TrimSpace(v.Version)
	if len(ver) < 1 || len(ver) > 64 {
		return "", nil // missing/invalid version yields empty string observation
	}
	return ver, nil
}

func (c *Composition) probeTags(ctx context.Context, baseURL, modelID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInvalidArgument, err, "build /api/tags request")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", errs.Wrap(errs.CategoryModelUnavailable, err, "probe /api/tags failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", errs.New(errs.CategoryModelUnavailable, "/api/tags returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return "", errs.Wrap(errs.CategoryProbeFailed, err, "read /api/tags response")
	}

	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &tags); err != nil {
		return "", errs.Wrap(errs.CategoryProbeFailed, err, "decode /api/tags response")
	}

	for _, m := range tags.Models {
		if m.Name == modelID || m.Model == modelID {
			rawDigest := strings.TrimSpace(m.Digest)
			hexDigest := strings.TrimPrefix(rawDigest, "sha256:")
			if hexDigest != "" && isHexString(hexDigest) {
				return "sha256:" + hexDigest, nil
			}
			return "", nil // empty or invalid digest yields empty revision
		}
	}
	return "", nil // model not found yields empty revision
}

func isHexString(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

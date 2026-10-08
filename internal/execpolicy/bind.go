package execpolicy

import (
	"context"
	"fmt"
	"strings"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

// EndpointObservation carries runtime observations captured when opening an endpoint driver.
type EndpointObservation struct {
	ModelRevision  string `json:"model_revision"`
	RuntimeVersion string `json:"runtime_version"`
	DriverID       string `json:"driver_id"`
}

// Validate checks that all observation fields are non-empty.
func (o EndpointObservation) Validate() error {
	const kind = "EndpointObservation"
	if strings.TrimSpace(o.ModelRevision) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: model_revision is required", kind)
	}
	if strings.TrimSpace(o.RuntimeVersion) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: runtime_version is required", kind)
	}
	if strings.TrimSpace(o.DriverID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: driver_id is required", kind)
	}
	return nil
}

// OpenedEndpoint represents an opened endpoint handle with its runtime observations.
type OpenedEndpoint struct {
	Driver   drivers.SessionDriver
	Observed EndpointObservation
}

// DriverFactory opens a driver session for a resolved endpoint.
type DriverFactory interface {
	Open(context.Context, ResolvedEndpoint) (OpenedEndpoint, error)
}

// Bind combines a ResolvedEndpoint with its runtime EndpointObservation to produce a bound ResolvedEndpoint.
// It computes a deterministic BindingDigest over the bound endpoint.
func Bind(ep ResolvedEndpoint, obs EndpointObservation) (ResolvedEndpoint, error) {
	if obs.ModelRevision == "" {
		return ResolvedEndpoint{}, errs.New(errs.CategoryInvalidArgument, "model-revision-unknown")
	}
	if obs.RuntimeVersion == "" {
		return ResolvedEndpoint{}, errs.New(errs.CategoryInvalidArgument, "runtime-version-unknown")
	}
	if obs.DriverID == "" {
		return ResolvedEndpoint{}, errs.New(errs.CategoryInvalidArgument, "driver-id-unknown")
	}

	bound := ep
	bound.ModelRevision = obs.ModelRevision
	bound.RuntimeVersion = obs.RuntimeVersion
	bound.DriverID = obs.DriverID
	bound.BindingDigest = ""

	canonical, err := protocol.CanonicalJSON(bound)
	if err != nil {
		return ResolvedEndpoint{}, errs.Wrap(errs.CategoryInvalidArgument, err, "compute binding digest")
	}
	bound.BindingDigest = protocol.DigestBytes(canonical)

	return bound, nil
}

// AssertDriverID enforces invariant SC-4: that the driver id reported in observations matches the actual opened driver id.
func AssertDriverID(openedDriverID string, obs EndpointObservation) error {
	if obs.DriverID != openedDriverID {
		return principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"driver-id-mismatch"},
			fmt.Sprintf("driver id mismatch: opened %q vs observed %q", openedDriverID, obs.DriverID),
		)
	}
	return nil
}

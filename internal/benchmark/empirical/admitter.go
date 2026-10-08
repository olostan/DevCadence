package empirical

import (
	"context"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
)

// AdmitterOptions sets the dependencies for an Admitter (WP-M5-R3 Part B).
type AdmitterOptions struct {
	Authority OperatorAuthority
	Verifier  IndependentVerifier
	Resolver  ArtifactResolver
	Clock     clock.Clock
}

// Admitter is the production admission gatekeeper for empirical campaign evidence
// (WP-M5-R3 Part B). It verifies operator authority windows, matches session evidence
// and plan digests, ensures spend compliance, and validates independent verifier execution.
type Admitter struct {
	authority OperatorAuthority
	verifier  IndependentVerifier
	resolver  ArtifactResolver
	clock     clock.Clock
}

// NewAdmitter constructs a new Admitter.
// Nil Authority, Verifier, Resolver, or Clock is refused.
func NewAdmitter(opts AdmitterOptions) (*Admitter, error) {
	if opts.Authority == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "admitter: authority is required")
	}
	if opts.Verifier == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "admitter: verifier is required")
	}
	if opts.Resolver == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "admitter: resolver is required")
	}
	if opts.Clock == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "admitter: clock is required")
	}
	return &Admitter{
		authority: opts.Authority,
		verifier:  opts.Verifier,
		resolver:  opts.Resolver,
		clock:     opts.Clock,
	}, nil
}

// Admit executes the complete empirical admission pipeline on a campaign manifest.
func (a *Admitter) Admit(ctx context.Context, m CampaignManifest) (AdmissionResult, error) {
	return validateAdmission(ctx, m, a.resolver, a.verifier, a.authority, a.clock)
}

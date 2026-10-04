package cognition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ChangeTrigger defines the recognized reasons for proposing a portfolio change (ADR-0018 §11).
type ChangeTrigger string

const (
	TriggerResourceChange   ChangeTrigger = "resource_change"
	TriggerPolicyUpdate     ChangeTrigger = "policy_update"
	TriggerManualProposal   ChangeTrigger = "manual_proposal"
	TriggerEvaluatedOutcome ChangeTrigger = "evaluated_outcome"
)

// Valid checks if the ChangeTrigger is one of the recognized values.
func (t ChangeTrigger) Valid() bool {
	switch t {
	case TriggerResourceChange, TriggerPolicyUpdate, TriggerManualProposal, TriggerEvaluatedOutcome:
		return true
	}
	return false
}

// PortfolioChangeProposal articulates a proposed portfolio adaptation and its rationale.
type PortfolioChangeProposal struct {
	ProposalID         string                      `json:"proposal_id"`
	Trigger            ChangeTrigger               `json:"trigger"`
	Rationale          string                      `json:"rationale"`
	BasePortfolioID    string                      `json:"base_portfolio_id"`
	CandidatePortfolio protocol.CognitionPortfolio `json:"candidate_portfolio"`
	Diff               PortfolioDiff               `json:"diff"`
	ProposedAt         string                      `json:"proposed_at"`
}

// CreateChangeProposal constructs and validates a PortfolioChangeProposal (REQ-04, REQ-05).
func CreateChangeProposal(trigger ChangeTrigger, rationale string, base *protocol.CognitionPortfolio, candidate protocol.CognitionPortfolio, now time.Time) (*PortfolioChangeProposal, error) {
	if !trigger.Valid() {
		return nil, errs.New(errs.CategoryInvalidArgument, "invalid change trigger: %q", trigger)
	}
	if strings.TrimSpace(rationale) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "proposal rationale is required and cannot be empty")
	}
	if err := candidate.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "candidate portfolio validation failed")
	}

	diff, err := DiffPortfolios(base, &candidate)
	if err != nil {
		return nil, err
	}

	baseID := ""
	if base != nil {
		baseID = base.PortfolioID
	}

	timeStr := now.UTC().Format(time.RFC3339)
	hashInput := fmt.Sprintf("%s:%s:%s:%s", trigger, rationale, candidate.PortfolioID, timeStr)
	h := sha256.Sum256([]byte(hashInput))
	proposalID := fmt.Sprintf("prop-%s-%s", now.UTC().Format("20060102T150405Z"), hex.EncodeToString(h[:4]))

	return &PortfolioChangeProposal{
		ProposalID:         proposalID,
		Trigger:            trigger,
		Rationale:          rationale,
		BasePortfolioID:    baseID,
		CandidatePortfolio: candidate,
		Diff:               *diff,
		ProposedAt:         timeStr,
	}, nil
}

// AdaptationService coordinates portfolio change proposals, validation, and atomic lineage activation.
type AdaptationService struct {
	mgr *ActivationManager
}

// NewAdaptationService creates an AdaptationService backed by an ActivationManager.
func NewAdaptationService(mgr *ActivationManager) *AdaptationService {
	return &AdaptationService{
		mgr: mgr,
	}
}

// ProposeAndActivate verifies and activates a portfolio change proposal atomically (REQ-06, REQ-07).
func (s *AdaptationService) ProposeAndActivate(ctx context.Context, proposal *PortfolioChangeProposal, valInput ValidationInput) (*ActivationRecord, error) {
	if proposal == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "proposal is required")
	}
	if s.mgr == nil {
		return nil, errs.New(errs.CategoryInternal, "activation manager is not configured")
	}

	// 1. Enforce candidate binding: Go forces valInput.Portfolio to match proposal.CandidatePortfolio
	valInput.Portfolio = &proposal.CandidatePortfolio

	// 2. Lineage verification to prevent stale / race adaptations
	lineage, err := s.mgr.GetLineage(ctx)
	if err != nil && errs.CategoryOf(err) != errs.CategoryNotFound {
		return nil, err
	}

	if lineage != nil && lineage.CurrentPortfolioID != "" {
		if proposal.BasePortfolioID != lineage.CurrentPortfolioID {
			return nil, errs.New(errs.CategoryConflict,
				"adaptation rejected: proposal base portfolio ID %q does not match current active portfolio %q",
				proposal.BasePortfolioID, lineage.CurrentPortfolioID)
		}
	} else if proposal.BasePortfolioID != "" {
		// No active portfolio exists, but proposal expects a non-empty base
		return nil, errs.New(errs.CategoryConflict,
			"adaptation rejected: proposal expects base portfolio ID %q, but no active portfolio exists",
			proposal.BasePortfolioID)
	}

	// 3. Atomically activate via ActivationManager
	return s.mgr.Activate(ctx, valInput)
}

// Rollback restores a prior active configuration with mandatory revalidation (REQ-06, REQ-08).
func (s *AdaptationService) Rollback(ctx context.Context, targetActivationID string, revalInput *ValidationInput) (*ActivationRecord, error) {
	if s.mgr == nil {
		return nil, errs.New(errs.CategoryInternal, "activation manager is not configured")
	}
	if revalInput == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "revalidation input is required for rollback")
	}

	if strings.TrimSpace(targetActivationID) == "" {
		return s.mgr.RollbackToPrevious(ctx, revalInput)
	}
	return s.mgr.RollbackToActivation(ctx, targetActivationID, revalInput)
}

package cognition

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

const (
	ActivePortfolioFileName   = "active-portfolio.json"
	LineageFileName           = "active-portfolio.lineage.json"
	HistoryDirName            = "portfolio-history"
	PendingActivationFileName = "active-portfolio.pending.json"
)

// ActivationRecord represents an immutable point-in-time portfolio activation event.
type ActivationRecord struct {
	ActivationID               string                      `json:"activation_id"`
	Sequence                   int                         `json:"sequence"`
	ActivatedAt                string                      `json:"activated_at"`
	PortfolioID                string                      `json:"portfolio_id"`
	PortfolioRevision          int                         `json:"portfolio_revision"`
	Portfolio                  protocol.CognitionPortfolio `json:"portfolio"`
	CandidateDigest            string                      `json:"candidate_digest"`
	InventoryDigest            string                      `json:"inventory_digest"`
	PolicyDigest               string                      `json:"policy_digest,omitempty"`
	PreviousActivationID       string                      `json:"previous_activation_id,omitempty"`
	PreviousPortfolioID        string                      `json:"previous_portfolio_id,omitempty"`
	IsRollback                 bool                        `json:"is_rollback,omitempty"`
	RollbackTargetActivationID string                      `json:"rollback_target_activation_id,omitempty"`
}

// PortfolioLineage tracks the active portfolio identity and activation chain.
type PortfolioLineage struct {
	CurrentActivationID      string             `json:"current_activation_id"`
	CurrentSequence          int                `json:"current_sequence"`
	CurrentPortfolioID       string             `json:"current_portfolio_id"`
	CurrentPortfolioRevision int                `json:"current_portfolio_revision"`
	ActivatedAt              string             `json:"activated_at"`
	PreviousActivationID     string             `json:"previous_activation_id,omitempty"`
	PreviousPortfolioID      string             `json:"previous_portfolio_id,omitempty"`
	InventoryDigest          string             `json:"inventory_digest,omitempty"`
	CandidateDigest          string             `json:"candidate_digest,omitempty"`
	PolicyDigest             string             `json:"policy_digest,omitempty"`
	History                  []ActivationRecord `json:"history,omitempty"`
}

// PendingActivation holds an in-flight activation transaction for crash consistency.
type PendingActivation struct {
	Record          ActivationRecord `json:"record"`
	Lineage         PortfolioLineage `json:"lineage"`
	HistoryFileName string           `json:"history_file_name"`
}

// ActivationManager coordinates deterministic validation, atomic versioned activation,
// and safe rollback of cognition portfolios.
type ActivationManager struct {
	mu             sync.Mutex
	dir            string
	validator      *PortfolioValidator
	clock          clock.Clock
	postRenameHook func(targetPath string) error
	syncDirHook    func(dirPath string) error
}

// NewActivationManager creates an ActivationManager storing state in dir and performs startup recovery.
func NewActivationManager(dir string, validator *PortfolioValidator, clk clock.Clock) (*ActivationManager, error) {
	if dir == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "activation manager: dir is required")
	}
	if validator == nil {
		validator = NewPortfolioValidator()
	}
	if clk == nil {
		clk = clock.System()
	}
	if err := os.MkdirAll(filepath.Join(dir, HistoryDirName), 0755); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "failed to create history directory")
	}
	mgr := &ActivationManager{
		dir:       dir,
		validator: validator,
		clock:     clk,
	}
	if err := mgr.recoverStartupLocked(); err != nil {
		return nil, err
	}
	return mgr, nil
}

// Dir returns the base directory used for activation files.
func (m *ActivationManager) Dir() string {
	return m.dir
}

// Activate deterministically validates and atomically activates a candidate portfolio.
func (m *ActivationManager) Activate(ctx context.Context, input ValidationInput) (*ActivationRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if input.Clock == nil {
		input.Clock = m.clock
	}

	// 1. Deterministic validation
	result := m.validator.Validate(input)
	if !result.Valid {
		return nil, result.Err()
	}

	// 2. Load current lineage to check freshness and determine sequence
	lineage, err := m.loadLineageLocked()
	if err != nil && errs.CategoryOf(err) != errs.CategoryNotFound {
		return nil, err
	}

	// Freshness check: if candidate was validated with a stale inventory compared to current
	if lineage != nil && lineage.InventoryDigest != "" && result.InventoryDigest != "" {
		if input.Inventory != nil && lineage.InventoryDigest != result.InventoryDigest {
			// Inventory changed since last active activation - this is normal when updating inventory.
			// But if the validation input itself has a mismatch between inventory and machine profile:
			if input.MachineProfile != nil && input.Inventory.MachineFingerprint != input.MachineProfile.MachineFingerprint {
				return nil, errs.New(errs.CategoryConflict,
					"activation rejected: inventory machine fingerprint %q does not match profile machine fingerprint %q",
					input.Inventory.MachineFingerprint, input.MachineProfile.MachineFingerprint)
			}
		}
	}

	seq := 1
	prevActID := ""
	prevPortID := ""
	if lineage != nil {
		seq = lineage.CurrentSequence + 1
		prevActID = lineage.CurrentActivationID
		prevPortID = lineage.CurrentPortfolioID
	}

	now := m.clock.Now().UTC()
	actID := fmt.Sprintf("act-%s-%04d", now.Format("20060102T150405Z"), seq)

	record := ActivationRecord{
		ActivationID:         actID,
		Sequence:             seq,
		ActivatedAt:          now.Format(time.RFC3339),
		PortfolioID:          input.Portfolio.PortfolioID,
		PortfolioRevision:    input.Portfolio.Revision,
		Portfolio:            *input.Portfolio,
		CandidateDigest:      result.CandidateDigest,
		InventoryDigest:      result.InventoryDigest,
		PolicyDigest:         result.PolicyDigest,
		PreviousActivationID: prevActID,
		PreviousPortfolioID:  prevPortID,
	}

	// 3. Atomically persist activation
	if err := m.persistActivationLocked(record, lineage); err != nil {
		return nil, err
	}

	return &record, nil
}

// RollbackToPrevious restores the immediately preceding known-good activation.
func (m *ActivationManager) RollbackToPrevious(ctx context.Context, revalidationInput *ValidationInput) (*ActivationRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if revalidationInput == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "rollback: revalidation input is mandatory")
	}

	lineage, err := m.loadLineageLocked()
	if err != nil {
		return nil, err
	}
	if lineage == nil || lineage.PreviousActivationID == "" {
		return nil, errs.New(errs.CategoryNotFound, "rollback failed: no previous active portfolio found in lineage")
	}

	return m.rollbackToLocked(ctx, lineage.PreviousActivationID, lineage, revalidationInput)
}

// RollbackToActivation restores a specific historical activation by its activation ID.
func (m *ActivationManager) RollbackToActivation(ctx context.Context, targetActivationID string, revalidationInput *ValidationInput) (*ActivationRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if targetActivationID == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "rollback: target_activation_id is required")
	}
	if revalidationInput == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "rollback: revalidation input is mandatory")
	}

	lineage, err := m.loadLineageLocked()
	if err != nil {
		return nil, err
	}
	if lineage == nil {
		return nil, errs.New(errs.CategoryNotFound, "rollback failed: no active portfolio lineage found")
	}

	return m.rollbackToLocked(ctx, targetActivationID, lineage, revalidationInput)
}

func (m *ActivationManager) rollbackToLocked(ctx context.Context, targetActID string, lineage *PortfolioLineage, revalidationInput *ValidationInput) (*ActivationRecord, error) {
	if revalidationInput == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "rollback: revalidation input is mandatory")
	}

	var targetRecord *ActivationRecord
	for _, rec := range lineage.History {
		if rec.ActivationID == targetActID {
			r := rec
			targetRecord = &r
			break
		}
	}
	if targetRecord == nil {
		// Try reading directly from history directory
		rec, err := m.readHistoryRecord(targetActID)
		if err != nil {
			return nil, errs.New(errs.CategoryNotFound, "rollback failed: target activation %q not found in history: %v", targetActID, err)
		}
		targetRecord = rec
	}

	// Mandatory revalidation against current environment and policy state
	valIn := *revalidationInput
	valIn.Portfolio = &targetRecord.Portfolio
	if valIn.Clock == nil {
		valIn.Clock = m.clock
	}
	res := m.validator.Validate(valIn)
	if !res.Valid {
		return nil, errs.Wrap(errs.CategoryValidationFailed, res.Err(),
			"rollback target %q is no longer valid against current environment/policy", targetActID)
	}

	now := m.clock.Now().UTC()
	seq := lineage.CurrentSequence + 1
	newActID := fmt.Sprintf("act-%s-%04d", now.Format("20060102T150405Z"), seq)

	newRecord := ActivationRecord{
		ActivationID:               newActID,
		Sequence:                   seq,
		ActivatedAt:                now.Format(time.RFC3339),
		PortfolioID:                targetRecord.PortfolioID,
		PortfolioRevision:          targetRecord.PortfolioRevision,
		Portfolio:                  targetRecord.Portfolio,
		CandidateDigest:            res.CandidateDigest,
		InventoryDigest:            res.InventoryDigest,
		PolicyDigest:               res.PolicyDigest,
		PreviousActivationID:       lineage.CurrentActivationID,
		PreviousPortfolioID:        lineage.CurrentPortfolioID,
		IsRollback:                 true,
		RollbackTargetActivationID: targetActID,
	}

	if err := m.persistActivationLocked(newRecord, lineage); err != nil {
		return nil, err
	}

	return &newRecord, nil
}

// GetActivePortfolio retrieves the currently active portfolio and activation metadata.
func (m *ActivationManager) GetActivePortfolio(ctx context.Context) (*protocol.CognitionPortfolio, *ActivationRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	activePath := filepath.Join(m.dir, ActivePortfolioFileName)
	data, err := os.ReadFile(activePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, errs.New(errs.CategoryNotFound, "no active portfolio found at %s", activePath)
		}
		return nil, nil, errs.Wrap(errs.CategoryInternal, err, "failed to read active portfolio")
	}

	var p protocol.CognitionPortfolio
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, nil, errs.Wrap(errs.CategoryIntegrity, err, "malformed active portfolio JSON")
	}

	lineage, err := m.loadLineageLocked()
	if err != nil {
		return &p, nil, nil
	}

	var currentRec *ActivationRecord
	if lineage != nil && len(lineage.History) > 0 {
		currentRec = &lineage.History[len(lineage.History)-1]
	}

	return &p, currentRec, nil
}

// GetActivationHistory returns the chronological list of all activation records.
func (m *ActivationManager) GetActivationHistory(ctx context.Context) ([]ActivationRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	lineage, err := m.loadLineageLocked()
	if err != nil {
		if errs.CategoryOf(err) == errs.CategoryNotFound {
			return nil, nil
		}
		return nil, err
	}
	if lineage == nil {
		return nil, nil
	}
	out := make([]ActivationRecord, len(lineage.History))
	copy(out, lineage.History)
	return out, nil
}

// GetLineage returns the current portfolio lineage metadata.
func (m *ActivationManager) GetLineage(ctx context.Context) (*PortfolioLineage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadLineageLocked()
}

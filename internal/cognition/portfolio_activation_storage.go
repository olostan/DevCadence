package cognition

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

func (m *ActivationManager) persistActivationLocked(record ActivationRecord, prevLineage *PortfolioLineage) error {
	var existingHistory []ActivationRecord
	if prevLineage != nil {
		existingHistory = prevLineage.History
	}
	historyCopy := make([]ActivationRecord, len(existingHistory), len(existingHistory)+1)
	copy(historyCopy, existingHistory)
	historyCopy = append(historyCopy, record)

	// Sort history by sequence
	sort.SliceStable(historyCopy, func(i, j int) bool {
		return historyCopy[i].Sequence < historyCopy[j].Sequence
	})

	newLineage := PortfolioLineage{
		CurrentActivationID:      record.ActivationID,
		CurrentSequence:          record.Sequence,
		CurrentPortfolioID:       record.PortfolioID,
		CurrentPortfolioRevision: record.PortfolioRevision,
		ActivatedAt:              record.ActivatedAt,
		PreviousActivationID:     record.PreviousActivationID,
		PreviousPortfolioID:      record.PreviousPortfolioID,
		InventoryDigest:          record.InventoryDigest,
		CandidateDigest:          record.CandidateDigest,
		PolicyDigest:             record.PolicyDigest,
		History:                  historyCopy,
	}

	historyFileName := fmt.Sprintf("activation-%06d-%s.json", record.Sequence, record.ActivationID)

	// 1. Stage pending activation intent for crash consistency
	pending := PendingActivation{
		Record:          record,
		Lineage:         newLineage,
		HistoryFileName: historyFileName,
	}
	pendingBytes, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to serialize pending activation")
	}
	pendingPath := filepath.Join(m.dir, PendingActivationFileName)
	if err := m.atomicWriteFile(pendingPath, pendingBytes, 0644); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to write pending activation intent")
	}

	// Helper for synchronous rollback if component writes fail after intent is staged
	rollbackOnFailure := func(origErr error) error {
		_ = os.Remove(filepath.Join(m.dir, HistoryDirName, historyFileName))
		if prevLineage == nil || len(existingHistory) == 0 {
			if err := os.Remove(filepath.Join(m.dir, ActivePortfolioFileName)); err != nil && !os.IsNotExist(err) {
				return errs.Wrap(errs.CategoryInternal, origErr, "rollback remove active failed: %v", err)
			}
			if err := os.Remove(filepath.Join(m.dir, LineageFileName)); err != nil && !os.IsNotExist(err) {
				return errs.Wrap(errs.CategoryInternal, origErr, "rollback remove lineage failed: %v", err)
			}
		} else {
			prevRec := existingHistory[len(existingHistory)-1]
			prevPortfolioBytes, err := json.MarshalIndent(prevRec.Portfolio, "", "  ")
			if err != nil {
				return errs.Wrap(errs.CategoryInternal, origErr, "rollback marshal portfolio failed: %v", err)
			}
			if err := m.atomicWriteFile(filepath.Join(m.dir, ActivePortfolioFileName), prevPortfolioBytes, 0644); err != nil {
				return errs.Wrap(errs.CategoryInternal, origErr, "rollback restore portfolio failed: %v", err)
			}
			prevLineageBytes, err := json.MarshalIndent(prevLineage, "", "  ")
			if err != nil {
				return errs.Wrap(errs.CategoryInternal, origErr, "rollback marshal lineage failed: %v", err)
			}
			if err := m.atomicWriteFile(filepath.Join(m.dir, LineageFileName), prevLineageBytes, 0644); err != nil {
				return errs.Wrap(errs.CategoryInternal, origErr, "rollback restore lineage failed: %v", err)
			}
		}
		if err := m.syncDir(m.dir); err != nil {
			return errs.Wrap(errs.CategoryInternal, origErr, "rollback sync dir failed: %v", err)
		}
		if err := os.Remove(pendingPath); err != nil && !os.IsNotExist(err) {
			return errs.Wrap(errs.CategoryInternal, origErr, "rollback remove pending failed: %v", err)
		}
		if err := m.syncDir(m.dir); err != nil {
			return errs.Wrap(errs.CategoryInternal, origErr, "rollback final sync dir failed: %v", err)
		}
		return origErr
	}

	// 2. Write historical snapshot in history/ directory
	historyPath := filepath.Join(m.dir, HistoryDirName, historyFileName)
	historyBytes, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return rollbackOnFailure(errs.Wrap(errs.CategoryInternal, err, "failed to serialize activation record"))
	}
	if err := m.atomicWriteFile(historyPath, historyBytes, 0644); err != nil {
		return rollbackOnFailure(errs.Wrap(errs.CategoryInternal, err, "failed to write activation history record"))
	}

	// 3. Write active-portfolio.json (pure CognitionPortfolio JSON conforming to schema)
	portfolioBytes, err := json.MarshalIndent(record.Portfolio, "", "  ")
	if err != nil {
		return rollbackOnFailure(errs.Wrap(errs.CategoryInternal, err, "failed to serialize active portfolio"))
	}
	activePath := filepath.Join(m.dir, ActivePortfolioFileName)
	if err := m.atomicWriteFile(activePath, portfolioBytes, 0644); err != nil {
		return rollbackOnFailure(errs.Wrap(errs.CategoryInternal, err, "failed to write active-portfolio.json"))
	}

	// 4. Write active-portfolio.lineage.json
	lineageBytes, err := json.MarshalIndent(newLineage, "", "  ")
	if err != nil {
		return rollbackOnFailure(errs.Wrap(errs.CategoryInternal, err, "failed to serialize lineage"))
	}
	lineagePath := filepath.Join(m.dir, LineageFileName)
	if err := m.atomicWriteFile(lineagePath, lineageBytes, 0644); err != nil {
		return rollbackOnFailure(errs.Wrap(errs.CategoryInternal, err, "failed to write active-portfolio.lineage.json"))
	}

	// 5. Commit complete: remove transaction intent only after all writes succeed
	if err := os.Remove(pendingPath); err != nil && !os.IsNotExist(err) {
		return errs.Wrap(errs.CategoryInternal, err, "failed to remove pending activation intent")
	}
	if err := m.syncDir(m.dir); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to sync directory after activation")
	}

	return nil
}

func (m *ActivationManager) recoverStartupLocked() error {
	pendingPath := filepath.Join(m.dir, PendingActivationFileName)
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		if os.IsNotExist(err) {
			return m.cleanTempFilesLocked()
		}
		return errs.Wrap(errs.CategoryInternal, err, "failed to read pending activation journal")
	}

	var pending PendingActivation
	if err := json.Unmarshal(data, &pending); err != nil {
		return errs.Wrap(errs.CategoryIntegrity, err, "corrupted pending activation journal")
	}

	// Roll forward any uncommitted components with strict error checking
	histPath := filepath.Join(m.dir, HistoryDirName, pending.HistoryFileName)
	histBytes, err := json.MarshalIndent(pending.Record, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to marshal recovery history record")
	}
	if err := m.atomicWriteFile(histPath, histBytes, 0644); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to recover history record")
	}

	activePath := filepath.Join(m.dir, ActivePortfolioFileName)
	activeBytes, err := json.MarshalIndent(pending.Record.Portfolio, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to marshal recovery active portfolio")
	}
	if err := m.atomicWriteFile(activePath, activeBytes, 0644); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to recover active portfolio")
	}

	lineagePath := filepath.Join(m.dir, LineageFileName)
	lineageBytes, err := json.MarshalIndent(pending.Lineage, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to marshal recovery lineage")
	}
	if err := m.atomicWriteFile(lineagePath, lineageBytes, 0644); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to recover lineage")
	}

	// Commit recovery: remove pending intent only after all recovery writes have succeeded
	if err := os.Remove(pendingPath); err != nil && !os.IsNotExist(err) {
		return errs.Wrap(errs.CategoryInternal, err, "failed to remove pending journal after recovery")
	}
	if err := m.syncDir(m.dir); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to sync directory after recovery")
	}

	return m.cleanTempFilesLocked()
}

func (m *ActivationManager) cleanTempFilesLocked() error {
	entries, err := os.ReadDir(m.dir)
	if err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp.") {
				_ = os.Remove(filepath.Join(m.dir, e.Name()))
			}
		}
	}
	histEntries, err := os.ReadDir(filepath.Join(m.dir, HistoryDirName))
	if err == nil {
		for _, e := range histEntries {
			if strings.Contains(e.Name(), ".tmp.") {
				_ = os.Remove(filepath.Join(m.dir, HistoryDirName, e.Name()))
			}
		}
	}
	return nil
}

func (m *ActivationManager) loadLineageLocked() (*PortfolioLineage, error) {
	lineagePath := filepath.Join(m.dir, LineageFileName)
	data, err := os.ReadFile(lineagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errs.New(errs.CategoryNotFound, "lineage file not found")
		}
		return nil, errs.Wrap(errs.CategoryInternal, err, "failed to read lineage file")
	}

	var lineage PortfolioLineage
	if err := json.Unmarshal(data, &lineage); err != nil {
		return nil, errs.Wrap(errs.CategoryIntegrity, err, "malformed lineage file")
	}
	return &lineage, nil
}

func (m *ActivationManager) readHistoryRecord(activationID string) (*ActivationRecord, error) {
	histDir := filepath.Join(m.dir, HistoryDirName)
	entries, err := os.ReadDir(histDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), activationID) {
			data, err := os.ReadFile(filepath.Join(histDir, entry.Name()))
			if err != nil {
				return nil, err
			}
			var rec ActivationRecord
			if err := json.Unmarshal(data, &rec); err != nil {
				return nil, err
			}
			return &rec, nil
		}
	}
	return nil, os.ErrNotExist
}

// SetPostRenameHookForTesting sets a hook invoked immediately after atomic rename for testing failure modes.
func (m *ActivationManager) SetPostRenameHookForTesting(hook func(targetPath string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postRenameHook = hook
}

// SetSyncDirHookForTesting sets a hook invoked during directory sync for testing failure modes.
func (m *ActivationManager) SetSyncDirHookForTesting(hook func(dirPath string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncDirHook = hook
}

func (m *ActivationManager) syncDir(dirPath string) error {
	d, err := os.Open(dirPath)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return err
	}
	if m != nil && m.syncDirHook != nil {
		if err := m.syncDirHook(dirPath); err != nil {
			return err
		}
	}
	return nil
}

// atomicWriteFile safely writes data to targetPath by writing to a sibling temp file,
// syncing, and atomically renaming it into place, followed by directory fsync.
func (m *ActivationManager) atomicWriteFile(targetPath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(targetPath)
	tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(targetPath)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName) // Clean up if rename fails

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return err
	}
	if m != nil && m.postRenameHook != nil {
		if err := m.postRenameHook(targetPath); err != nil {
			return err
		}
	}
	return m.syncDir(dir)
}

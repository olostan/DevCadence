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

func (m *ActivationManager) persistActivationLocked(record ActivationRecord, existingHistory []ActivationRecord) error {
	historyCopy := append(existingHistory, record)

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
	if err := atomicWriteFile(pendingPath, pendingBytes, 0644); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to write pending activation intent")
	}

	// 2. Write historical snapshot in history/ directory
	historyPath := filepath.Join(m.dir, HistoryDirName, historyFileName)
	historyBytes, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to serialize activation record")
	}
	if err := atomicWriteFile(historyPath, historyBytes, 0644); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to write activation history record")
	}

	// 3. Write active-portfolio.json (pure CognitionPortfolio JSON conforming to schema)
	portfolioBytes, err := json.MarshalIndent(record.Portfolio, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to serialize active portfolio")
	}
	activePath := filepath.Join(m.dir, ActivePortfolioFileName)
	if err := atomicWriteFile(activePath, portfolioBytes, 0644); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to write active-portfolio.json")
	}

	// 4. Write active-portfolio.lineage.json
	lineageBytes, err := json.MarshalIndent(newLineage, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to serialize lineage")
	}
	lineagePath := filepath.Join(m.dir, LineageFileName)
	if err := atomicWriteFile(lineagePath, lineageBytes, 0644); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to write active-portfolio.lineage.json")
	}

	// 5. Commit complete: remove transaction intent
	_ = os.Remove(pendingPath)
	syncDir(m.dir)

	return nil
}

func (m *ActivationManager) recoverStartupLocked() error {
	pendingPath := filepath.Join(m.dir, PendingActivationFileName)
	if data, err := os.ReadFile(pendingPath); err == nil {
		var pending PendingActivation
		if err := json.Unmarshal(data, &pending); err == nil {
			// Roll forward any uncommitted components of the pending activation
			histPath := filepath.Join(m.dir, HistoryDirName, pending.HistoryFileName)
			histBytes, _ := json.MarshalIndent(pending.Record, "", "  ")
			_ = atomicWriteFile(histPath, histBytes, 0644)

			activePath := filepath.Join(m.dir, ActivePortfolioFileName)
			activeBytes, _ := json.MarshalIndent(pending.Record.Portfolio, "", "  ")
			_ = atomicWriteFile(activePath, activeBytes, 0644)

			lineagePath := filepath.Join(m.dir, LineageFileName)
			lineageBytes, _ := json.MarshalIndent(pending.Lineage, "", "  ")
			_ = atomicWriteFile(lineagePath, lineageBytes, 0644)
		}
		_ = os.Remove(pendingPath)
		syncDir(m.dir)
	}

	// Clean up any stale sibling temp files (.tmp.*)
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

func syncDir(dirPath string) {
	d, err := os.Open(dirPath)
	if err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// atomicWriteFile safely writes data to targetPath by writing to a sibling temp file,
// syncing, and atomically renaming it into place, followed by directory fsync.
func atomicWriteFile(targetPath string, data []byte, perm os.FileMode) error {
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
	syncDir(dir)
	return nil
}

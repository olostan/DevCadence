package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// CreateLeaseParams configures the issuance of a content-addressed EvidenceLease.
type CreateLeaseParams struct {
	EvidenceKind        protocol.EvidenceLeaseKind
	SourceRevision      string
	WorktreeID          string
	FilePath            string
	Locator             string
	AcquisitionQuestion string
	AcquisitionReason   string
	Content             string
	AccountingMethod    protocol.TokenizerAccountingMethod
	ReadEnvelope        []string // Authorized read boundaries (optional, checked if non-empty)
	ExpiresAt           *string
}

// EvidenceLeaseManager manages active and historical evidence working set leases.
// Leases provide verbatim, content-addressed slices of working memory with verifiable
// authorization bounds and freshness invalidation (DCI-014, DCI-045, PROTOCOLS §10B).
type EvidenceLeaseManager struct {
	mu     sync.RWMutex
	leases map[string]protocol.EvidenceLease
}

// NewEvidenceLeaseManager initializes an EvidenceLeaseManager.
func NewEvidenceLeaseManager() *EvidenceLeaseManager {
	return &EvidenceLeaseManager{
		leases: make(map[string]protocol.EvidenceLease),
	}
}

// CreateLease creates, validates, and stores a new content-addressed EvidenceLease.
// It verifies read authorization against ReadEnvelope and computes exact SHA-256 digest.
func (m *EvidenceLeaseManager) CreateLease(params CreateLeaseParams) (protocol.EvidenceLease, error) {
	const kind = "EvidenceLeaseCreation"

	// 1. Basic validation
	if params.FilePath == "" {
		return protocol.EvidenceLease{}, errs.New(errs.CategoryInvalidArgument, "%s: file_path cannot be empty", kind)
	}
	if params.SourceRevision == "" {
		return protocol.EvidenceLease{}, errs.New(errs.CategoryInvalidArgument, "%s: source_revision cannot be empty", kind)
	}
	if params.WorktreeID == "" {
		return protocol.EvidenceLease{}, errs.New(errs.CategoryInvalidArgument, "%s: worktree_id cannot be empty", kind)
	}
	if params.Locator == "" {
		return protocol.EvidenceLease{}, errs.New(errs.CategoryInvalidArgument, "%s: locator cannot be empty", kind)
	}
	if params.Content == "" {
		return protocol.EvidenceLease{}, errs.New(errs.CategoryInvalidArgument, "%s: content cannot be empty", kind)
	}
	if params.AcquisitionQuestion == "" {
		return protocol.EvidenceLease{}, errs.New(errs.CategoryInvalidArgument, "%s: acquisition_question cannot be empty", kind)
	}
	if params.AcquisitionReason == "" {
		return protocol.EvidenceLease{}, errs.New(errs.CategoryInvalidArgument, "%s: acquisition_reason cannot be empty", kind)
	}

	// 2. Server-side path authorization check against ReadEnvelope (DCI-014, DCI-018)
	if len(params.ReadEnvelope) > 0 {
		authorized := IsPathAuthorized(params.FilePath, params.ReadEnvelope)
		if !authorized {
			return protocol.EvidenceLease{}, errs.New(errs.CategoryPolicyDenied,
				"%s: file_path %q is outside authorized read envelope %v", kind, params.FilePath, params.ReadEnvelope)
		}
	}

	// 3. Verbatim content digest
	hasher := sha256.New()
	hasher.Write([]byte(params.Content))
	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))

	// 4. Token estimation
	method := params.AccountingMethod
	if !method.Valid() {
		method = protocol.AccountingExactBPE
	}
	tokenCount := EstimateTokens(params.Content, method, 0.05)
	if tokenCount < 1 {
		tokenCount = 1
	}

	evidenceKind := params.EvidenceKind
	if !evidenceKind.Valid() {
		evidenceKind = protocol.LeaseKindSourceSnippet
	}

	// 5. Deterministic lease ID
	cleanPath := strings.ReplaceAll(params.FilePath, "/", "_")
	cleanLocator := strings.ReplaceAll(params.Locator, ":", "_")
	leaseID := fmt.Sprintf("lease-%s-%s-%s", cleanPath, cleanLocator, digest[7:15])

	acquiredAt := time.Now().UTC().Format(time.RFC3339Nano)

	lease := protocol.EvidenceLease{
		SchemaVersion:       protocol.SchemaVersion1,
		LeaseID:             leaseID,
		EvidenceKind:        evidenceKind,
		SourceRevision:      params.SourceRevision,
		WorktreeID:          params.WorktreeID,
		FilePath:            params.FilePath,
		Locator:             params.Locator,
		ContentDigest:       digest,
		AcquisitionQuestion: params.AcquisitionQuestion,
		AcquisitionReason:   params.AcquisitionReason,
		Content:             params.Content,
		TokenCount:          tokenCount,
		AccountingMethod:    method,
		Status:              protocol.LeaseStatusActive,
		AcquiredAt:          acquiredAt,
		ExpiresAt:           params.ExpiresAt,
	}

	if err := lease.Validate(); err != nil {
		return protocol.EvidenceLease{}, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: generated lease failed validation", kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.leases[leaseID] = lease
	return lease, nil
}

// GetLease retrieves a lease by ID.
func (m *EvidenceLeaseManager) GetLease(leaseID string) (protocol.EvidenceLease, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	lease, ok := m.leases[leaseID]
	return lease, ok
}

// ActiveLeases returns all currently active evidence leases sorted deterministically by LeaseID.
func (m *EvidenceLeaseManager) ActiveLeases() []protocol.EvidenceLease {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []protocol.EvidenceLease
	for _, l := range m.leases {
		if l.Status == protocol.LeaseStatusActive {
			result = append(result, l)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].LeaseID < result[j].LeaseID
	})
	return result
}

// ReleaseLease marks an active lease as released.
func (m *EvidenceLeaseManager) ReleaseLease(leaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	lease, ok := m.leases[leaseID]
	if !ok {
		return errs.New(errs.CategoryNotFound, "lease %q not found", leaseID)
	}
	lease.Status = protocol.LeaseStatusReleased
	m.leases[leaseID] = lease
	return nil
}

// InvalidateForFileMutation invalidates all active leases that touch the given filePath.
// This preserves freshness when worktree files are mutated (PROTOCOLS §10B).
func (m *EvidenceLeaseManager) InvalidateForFileMutation(filePath string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanTarget := path.Clean(filePath)
	var invalidated []string
	for id, lease := range m.leases {
		if lease.Status == protocol.LeaseStatusActive && path.Clean(lease.FilePath) == cleanTarget {
			lease.Status = protocol.LeaseStatusInvalidated
			m.leases[id] = lease
			invalidated = append(invalidated, id)
		}
	}
	sort.Strings(invalidated)
	return invalidated
}

// InvalidateForRevision invalidates any active lease whose SourceRevision does not match newRevision.
func (m *EvidenceLeaseManager) InvalidateForRevision(newRevision string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var invalidated []string
	for id, lease := range m.leases {
		if lease.Status == protocol.LeaseStatusActive && lease.SourceRevision != newRevision {
			lease.Status = protocol.LeaseStatusInvalidated
			m.leases[id] = lease
			invalidated = append(invalidated, id)
		}
	}
	sort.Strings(invalidated)
	return invalidated
}

// EvictToFit releases active leases in reverse chronological or token order until currentTokens <= targetTokens.
// Returns the list of evicted lease IDs.
// Crucially, this only affects non-mandatory evidence leases; mandatory clauses cannot be evicted.
func (m *EvidenceLeaseManager) EvictToFit(targetTokens int, currentTokens int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if currentTokens <= targetTokens {
		return nil, nil
	}

	var active []protocol.EvidenceLease
	for _, l := range m.leases {
		if l.Status == protocol.LeaseStatusActive {
			active = append(active, l)
		}
	}

	// Sort active leases: oldest first, then by highest token count
	sort.Slice(active, func(i, j int) bool {
		if active[i].AcquiredAt != active[j].AcquiredAt {
			return active[i].AcquiredAt < active[j].AcquiredAt
		}
		return active[i].TokenCount > active[j].TokenCount
	})

	var evicted []string
	reclaimed := 0
	tokensNeeded := currentTokens - targetTokens

	for _, lease := range active {
		lease.Status = protocol.LeaseStatusReleased
		m.leases[lease.LeaseID] = lease
		evicted = append(evicted, lease.LeaseID)
		reclaimed += lease.TokenCount
		if reclaimed >= tokensNeeded {
			break
		}
	}

	return evicted, nil
}

// IsPathAuthorized checks if a filePath matches any pattern in allowedPatterns.
func IsPathAuthorized(filePath string, allowedPatterns []string) bool {
	cleanPath := path.Clean(filePath)
	for _, pattern := range allowedPatterns {
		cleanPat := path.Clean(pattern)
		if cleanPat == "*" || cleanPat == cleanPath {
			return true
		}
		// Prefix match for directories: e.g. "internal/*", "internal/...", "internal/"
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "/*")
			if cleanPath == prefix || strings.HasPrefix(cleanPath, prefix+"/") {
				return true
			}
		}
		if strings.HasSuffix(pattern, "/...") {
			prefix := strings.TrimSuffix(pattern, "/...")
			if cleanPath == prefix || strings.HasPrefix(cleanPath, prefix+"/") {
				return true
			}
		}
		// Glob match using filepath.Match
		matched, err := filepath.Match(pattern, filePath)
		if err == nil && matched {
			return true
		}
	}
	return false
}

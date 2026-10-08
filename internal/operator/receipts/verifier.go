package receipts

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// TrustAnchor defines an enrolled operator public key.
type TrustAnchor struct {
	AnchorID     string            `json:"anchor_id"`
	HumanActorID string            `json:"human_actor_id"`
	PublicKey    ed25519.PublicKey `json:"public_key"`
	NotBefore    time.Time         `json:"not_before"`
	NotAfter     time.Time         `json:"not_after"`
	Purposes     []Purpose         `json:"purposes"`
}

// Validate checks TrustAnchor fields.
func (ta TrustAnchor) Validate() error {
	const kind = "TrustAnchor"
	if strings.TrimSpace(ta.AnchorID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: anchor_id is required", kind)
	}
	if strings.TrimSpace(ta.HumanActorID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: human_actor_id is required", kind)
	}
	if len(ta.PublicKey) != ed25519.PublicKeySize {
		return errs.New(errs.CategoryInvalidArgument, "%s: public_key must be %d bytes, got %d", kind, ed25519.PublicKeySize, len(ta.PublicKey))
	}
	if ta.NotAfter.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: not_after is required", kind)
	}
	if !ta.NotBefore.IsZero() && ta.NotAfter.Before(ta.NotBefore) {
		return errs.New(errs.CategoryInvalidArgument, "%s: not_after (%s) must be after not_before (%s)", kind, ta.NotAfter, ta.NotBefore)
	}
	if len(ta.Purposes) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: purposes is required", kind)
	}
	for _, p := range ta.Purposes {
		if !p.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid purpose %q in purposes", kind, p)
		}
	}
	return nil
}

// RevocationList holds a set of revoked receipt IDs and the timestamp when the list was generated.
type RevocationList struct {
	RevokedIDs map[string]time.Time `json:"revoked_ids"`
	RevokedAt  time.Time            `json:"revoked_at"`
}

// IsRevoked reports whether receiptID is revoked.
func (rl *RevocationList) IsRevoked(receiptID string) bool {
	if rl == nil || rl.RevokedIDs == nil {
		return false
	}
	_, ok := rl.RevokedIDs[receiptID]
	return ok
}

// Verifier is the interface for verifying operator ingress receipts.
type Verifier interface {
	Verify(ctx context.Context, req Request) (Verified, error)
}

// VerifierOption configures an InMemoryVerifier.
type VerifierOption func(*InMemoryVerifier)

// WithRevocationList attaches a RevocationList to the InMemoryVerifier.
func WithRevocationList(rl *RevocationList) VerifierOption {
	return func(v *InMemoryVerifier) {
		v.revocations = rl
	}
}

// WithClock attaches a Clock to the InMemoryVerifier.
func WithClock(c clock.Clock) VerifierOption {
	return func(v *InMemoryVerifier) {
		v.clock = c
	}
}

// WithReceipts attaches in-memory receipts to the InMemoryVerifier.
func WithReceipts(rcpts ...Receipt) VerifierOption {
	return func(v *InMemoryVerifier) {
		v.receipts = append(v.receipts, rcpts...)
	}
}

// WithReceiptsDir attaches a receipts directory to the InMemoryVerifier.
func WithReceiptsDir(dir string) VerifierOption {
	return func(v *InMemoryVerifier) {
		v.receiptsDir = dir
	}
}

// InMemoryVerifier verifies operator ingress receipts using in-memory pinned anchors.
type InMemoryVerifier struct {
	anchors     map[string]TrustAnchor
	receipts    []Receipt
	receiptsDir string
	revocations *RevocationList
	clock       clock.Clock
}

var _ Verifier = (*InMemoryVerifier)(nil)

// NewVerifier creates a new InMemoryVerifier with pinned trust anchors.
func NewVerifier(anchors []TrustAnchor, opts ...VerifierOption) (*InMemoryVerifier, error) {
	if len(anchors) == 0 {
		return nil, errs.New(errs.CategoryInvalidArgument, "verifier: at least one trust anchor is required")
	}
	anchorsMap := make(map[string]TrustAnchor, len(anchors))
	for _, a := range anchors {
		if err := a.Validate(); err != nil {
			return nil, err
		}
		if _, exists := anchorsMap[a.AnchorID]; exists {
			return nil, errs.New(errs.CategoryInvalidArgument, "verifier: duplicate anchor_id %q", a.AnchorID)
		}
		anchorsMap[a.AnchorID] = a
	}
	v := &InMemoryVerifier{
		anchors: anchorsMap,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(v)
		}
	}
	return v, nil
}

// NewVerifierWithReceipts creates an InMemoryVerifier with pinned trust anchors and initial receipts.
func NewVerifierWithReceipts(anchors []TrustAnchor, receipts []Receipt, opts ...VerifierOption) (*InMemoryVerifier, error) {
	v, err := NewVerifier(anchors, opts...)
	if err != nil {
		return nil, err
	}
	v.receipts = append(v.receipts, receipts...)
	return v, nil
}

// SetRevocationList sets or updates the active revocation list.
func (v *InMemoryVerifier) SetRevocationList(rl *RevocationList) {
	v.revocations = rl
}

// Verify verifies a receipt request against in-memory anchors.
func (v *InMemoryVerifier) Verify(ctx context.Context, req Request) (Verified, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	reqTime := req.Time
	if reqTime.IsZero() && v.clock != nil {
		reqTime = v.clock.Now()
	}
	if reqTime.IsZero() {
		reqTime = time.Now().UTC()
	}

	var receipt Receipt
	if req.Receipt.Statement.ReceiptID != "" {
		receipt = req.Receipt
	} else {
		receiptsDir := req.ReceiptsDir
		if receiptsDir == "" {
			receiptsDir = v.receiptsDir
		}

		var candidates []Receipt
		candidates = append(candidates, v.receipts...)

		if receiptsDir != "" {
			entries, err := os.ReadDir(receiptsDir)
			if err == nil {
				var jsonFiles []string
				for _, entry := range entries {
					if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
						jsonFiles = append(jsonFiles, entry.Name())
					}
				}
				sort.Strings(jsonFiles)
				if len(jsonFiles) > 256 {
					jsonFiles = jsonFiles[:256]
				}
				for _, name := range jsonFiles {
					filePath := filepath.Join(receiptsDir, name)
					data, err := os.ReadFile(filePath)
					if err != nil || len(data) > 64*1024 {
						continue
					}
					decR := json.NewDecoder(bytes.NewReader(data))
					decR.DisallowUnknownFields()
					var r Receipt
					if err := decR.Decode(&r); err != nil {
						continue
					}
					if _, err := decR.Token(); err != io.EOF {
						continue
					}
					candidates = append(candidates, r)
				}
			}
		}

		if req.ReceiptID != "" {
			var found *Receipt
			for _, r := range candidates {
				if r.Statement.ReceiptID == req.ReceiptID {
					cand := r
					found = &cand
					break
				}
			}
			if found == nil {
				return nil, errs.New(errs.CategoryNotFound, "receipt %s not found", req.ReceiptID)
			}
			receipt = *found
		} else {
			var bestCandidate *Receipt
			for _, r := range candidates {
				stmt := r.Statement
				if stmt.Purpose != req.Purpose || (req.ProjectID != "" && stmt.ProjectID != req.ProjectID) {
					continue
				}
				if stmt.Subject != req.Subject {
					continue
				}
				if req.SubjectDigest != "" && stmt.SubjectDigest != req.SubjectDigest {
					continue
				}
				if v.revocations != nil && v.revocations.IsRevoked(stmt.ReceiptID) {
					continue
				}
				anchor, ok := v.anchors[stmt.AnchorID]
				if !ok {
					continue
				}
				if err := verifyStatementCommon(stmt, r.Signature, req, anchor, reqTime); err != nil {
					continue
				}
				if bestCandidate == nil || stmt.IssuedAt.After(bestCandidate.Statement.IssuedAt) ||
					(stmt.IssuedAt.Equal(bestCandidate.Statement.IssuedAt) && stmt.ReceiptID > bestCandidate.Statement.ReceiptID) {
					cand := r
					bestCandidate = &cand
				}
			}
			if bestCandidate == nil {
				return nil, errs.New(errs.CategoryPolicyDenied, "receipt-missing: no verifying receipt found for subject")
			}
			receipt = *bestCandidate
		}
	}

	stmt := receipt.Statement

	if v.revocations != nil && v.revocations.IsRevoked(stmt.ReceiptID) {
		return nil, errs.New(errs.CategoryPolicyDenied, "receipt %s has been revoked", stmt.ReceiptID)
	}

	anchor, ok := v.anchors[stmt.AnchorID]
	if !ok {
		return nil, errs.New(errs.CategoryPolicyDenied, "trust anchor %q not found", stmt.AnchorID)
	}

	if err := verifyStatementCommon(stmt, receipt.Signature, req, anchor, reqTime); err != nil {
		return nil, err
	}

	return verifiedReceipt{
		stmt:  stmt,
		valid: true,
	}, nil
}

// FileOptions configures a filesystem-protected FileVerifier.
type FileOptions struct {
	OperatorDir      string
	ReceiptsDir      string
	TrustedOwnerUIDs []uint32
	OperatorUID      uint32
	Clock            clock.Clock
	Runner           *process.Runner
	CheckACL         func(path string) error
	RootDir          string
}

// FileVerifier verifies operator ingress receipts using filesystem-protected trust anchors and revocation lists.
type FileVerifier struct {
	operatorDir      string
	receiptsDir      string
	trustedOwnerUIDs []uint32
	operatorUID      uint32
	clock            clock.Clock
	runner           *process.Runner
	checkACL         func(path string) error
	rootDir          string

	protOpts ProtectionOptions

	anchorsPath        string
	revokedPath        string
	pinnedAnchorsBytes []byte
	pinnedAnchorsHash  string
	anchors            map[string]TrustAnchor
}

var _ Verifier = (*FileVerifier)(nil)

// NewFileVerifier creates a new FileVerifier with strict R2 filesystem protection checks.
func NewFileVerifier(opts FileOptions) (*FileVerifier, error) {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}

	euid := uint32(getEUID())
	if euid == 0 {
		return nil, errs.New(errs.CategoryPolicyDenied, "refusing protected path check as root")
	}
	if opts.OperatorUID == euid {
		return nil, errs.New(errs.CategoryPolicyDenied, "operator UID cannot equal verifier euid %d", euid)
	}

	if len(opts.TrustedOwnerUIDs) == 0 {
		opts.TrustedOwnerUIDs = []uint32{0, opts.OperatorUID}
	}

	hasOperator := false
	for _, u := range opts.TrustedOwnerUIDs {
		if u == opts.OperatorUID {
			hasOperator = true
		}
		if u == euid {
			return nil, errs.New(errs.CategoryPolicyDenied, "trusted owner UID %d cannot equal verifier euid %d", u, euid)
		}
	}
	if !hasOperator {
		return nil, errs.New(errs.CategoryInvalidArgument, "trusted owner UIDs must contain OperatorUID %d", opts.OperatorUID)
	}

	if opts.OperatorDir == "" {
		if env := os.Getenv("DEVCADENCE_OPERATOR_DIR"); env != "" {
			opts.OperatorDir = env
		} else {
			switch runtime.GOOS {
			case "linux":
				opts.OperatorDir = "/etc/devcadence-operator"
			case "darwin":
				opts.OperatorDir = "/Library/Application Support/DevCadence/operator"
			default:
				return nil, errs.New(errs.CategoryPolicyDenied, "platform %s not supported for FileVerifier", runtime.GOOS)
			}
		}
	}
	opts.OperatorDir = filepath.Clean(opts.OperatorDir)

	if opts.ReceiptsDir == "" {
		if home := os.Getenv("DEVCADENCE_HOME"); home != "" {
			opts.ReceiptsDir = filepath.Join(home, "receipts")
		}
	}
	if opts.ReceiptsDir != "" {
		opts.ReceiptsDir = filepath.Clean(opts.ReceiptsDir)
	}

	protOpts := ProtectionOptions{
		OperatorUID:      opts.OperatorUID,
		TrustedOwnerUIDs: opts.TrustedOwnerUIDs,
		CheckACL:         opts.CheckACL,
		Runner:           opts.Runner,
		RootDir:          opts.RootDir,
	}

	anchorsPath := filepath.Join(opts.OperatorDir, "anchors.json")
	revokedPath := filepath.Join(opts.OperatorDir, "revoked.json")

	if err := CheckPathProtection(opts.OperatorDir, protOpts); err != nil {
		return nil, errs.Wrap(errs.CategoryPolicyDenied, err, "anchor-unprotected: operator dir protection check failed")
	}
	if err := CheckPathProtection(anchorsPath, protOpts); err != nil {
		return nil, errs.Wrap(errs.CategoryPolicyDenied, err, "anchor-unprotected: anchors.json protection check failed")
	}
	if err := CheckPathProtection(revokedPath, protOpts); err != nil {
		return nil, errs.Wrap(errs.CategoryPolicyDenied, err, "anchor-unprotected: revoked.json protection check failed")
	}

	rawAnchors, err := os.ReadFile(anchorsPath)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryNotFound, err, "cannot read anchors.json")
	}
	if len(rawAnchors) > 64*1024 {
		return nil, errs.New(errs.CategoryPolicyDenied, "anchors.json exceeds 64 KiB cap")
	}
	h := sha256.Sum256(rawAnchors)
	pinnedHash := hex.EncodeToString(h[:])

	var ad struct {
		Version string        `json:"version"`
		Anchors []TrustAnchor `json:"anchors"`
	}
	decA := json.NewDecoder(bytes.NewReader(rawAnchors))
	decA.DisallowUnknownFields()
	if err := decA.Decode(&ad); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "failed to decode anchors.json")
	}
	if _, err := decA.Token(); err != io.EOF {
		return nil, errs.New(errs.CategoryInvalidArgument, "unexpected trailing content in anchors.json")
	}
	if ad.Version != "1.0" {
		return nil, errs.New(errs.CategoryInvalidArgument, "anchors.json version must be 1.0, got %q", ad.Version)
	}
	if len(ad.Anchors) == 0 {
		return nil, errs.New(errs.CategoryInvalidArgument, "anchors.json must contain at least one anchor")
	}
	anchorsMap := make(map[string]TrustAnchor, len(ad.Anchors))
	for _, a := range ad.Anchors {
		if err := a.Validate(); err != nil {
			return nil, err
		}
		if _, exists := anchorsMap[a.AnchorID]; exists {
			return nil, errs.New(errs.CategoryInvalidArgument, "duplicate anchor_id %q in anchors.json", a.AnchorID)
		}
		anchorsMap[a.AnchorID] = a
	}

	rawRevoked, err := os.ReadFile(revokedPath)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryNotFound, err, "cannot read revoked.json")
	}
	if len(rawRevoked) > 64*1024 {
		return nil, errs.New(errs.CategoryPolicyDenied, "revoked.json exceeds 64 KiB cap")
	}
	var rd struct {
		Version    string   `json:"version"`
		ReceiptIDs []string `json:"receipt_ids"`
		AnchorIDs  []string `json:"anchor_ids"`
	}
	decRev := json.NewDecoder(bytes.NewReader(rawRevoked))
	decRev.DisallowUnknownFields()
	if err := decRev.Decode(&rd); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "failed to decode revoked.json")
	}
	if _, err := decRev.Token(); err != io.EOF {
		return nil, errs.New(errs.CategoryInvalidArgument, "unexpected trailing content in revoked.json")
	}
	if rd.Version != "1.0" {
		return nil, errs.New(errs.CategoryInvalidArgument, "revoked.json version must be 1.0, got %q", rd.Version)
	}

	return &FileVerifier{
		operatorDir:        opts.OperatorDir,
		receiptsDir:        opts.ReceiptsDir,
		trustedOwnerUIDs:   opts.TrustedOwnerUIDs,
		operatorUID:        opts.OperatorUID,
		clock:              opts.Clock,
		runner:             opts.Runner,
		checkACL:           opts.CheckACL,
		rootDir:            opts.RootDir,
		protOpts:           protOpts,
		anchorsPath:        anchorsPath,
		revokedPath:        revokedPath,
		pinnedAnchorsBytes: rawAnchors,
		pinnedAnchorsHash:  pinnedHash,
		anchors:            anchorsMap,
	}, nil
}

// Verify verifies a receipt request against pinned anchors and fresh revocation list.
func (v *FileVerifier) Verify(ctx context.Context, req Request) (Verified, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	reqTime := req.Time
	if reqTime.IsZero() && v.clock != nil {
		reqTime = v.clock.Now()
	}
	if reqTime.IsZero() {
		reqTime = time.Now().UTC()
	}

	// 1. Re-run R2 protection check on OperatorDir, anchors.json, and revoked.json
	if err := CheckPathProtection(v.operatorDir, v.protOpts); err != nil {
		return nil, errs.Wrap(errs.CategoryPolicyDenied, err, "anchor-unprotected: operator dir protection check failed")
	}
	if err := CheckPathProtection(v.anchorsPath, v.protOpts); err != nil {
		return nil, errs.Wrap(errs.CategoryPolicyDenied, err, "anchor-unprotected: anchors.json protection check failed")
	}
	if err := CheckPathProtection(v.revokedPath, v.protOpts); err != nil {
		return nil, errs.Wrap(errs.CategoryPolicyDenied, err, "anchor-unprotected: revoked.json protection check failed")
	}

	// 2. Re-verify SHA256 of anchors.json matches pinned value
	rawAnchors, err := os.ReadFile(v.anchorsPath)
	if err != nil || len(rawAnchors) > 64*1024 {
		return nil, errs.New(errs.CategoryPolicyDenied, "anchors-changed: cannot read anchors.json")
	}
	h := sha256.Sum256(rawAnchors)
	currHash := hex.EncodeToString(h[:])
	if currHash != v.pinnedAnchorsHash {
		return nil, errs.New(errs.CategoryPolicyDenied, "anchors-changed: anchors.json modified since verifier construction")
	}

	// 3. Read and strict decode revoked.json fresh
	rawRevoked, err := os.ReadFile(v.revokedPath)
	if err != nil || len(rawRevoked) > 64*1024 {
		return nil, errs.New(errs.CategoryPolicyDenied, "revocation-unavailable: cannot read revoked.json")
	}
	var revDoc struct {
		Version    string   `json:"version"`
		ReceiptIDs []string `json:"receipt_ids"`
		AnchorIDs  []string `json:"anchor_ids"`
	}
	decRev := json.NewDecoder(bytes.NewReader(rawRevoked))
	decRev.DisallowUnknownFields()
	if err := decRev.Decode(&revDoc); err != nil {
		return nil, errs.Wrap(errs.CategoryPolicyDenied, err, "revocation-unavailable: invalid revoked.json")
	}
	if _, err := decRev.Token(); err != io.EOF {
		return nil, errs.New(errs.CategoryPolicyDenied, "revocation-unavailable: trailing content in revoked.json")
	}
	if revDoc.Version != "1.0" {
		return nil, errs.New(errs.CategoryPolicyDenied, "revocation-unavailable: revoked.json version must be 1.0, got %q", revDoc.Version)
	}

	// 4. Resolve receipts directory
	receiptsDir := v.receiptsDir
	if req.ReceiptsDir != "" {
		receiptsDir = req.ReceiptsDir
	}
	if receiptsDir == "" {
		if home := os.Getenv("DEVCADENCE_HOME"); home != "" {
			receiptsDir = filepath.Join(home, "receipts")
		}
	}

	var receipt Receipt

	if req.ReceiptID != "" {
		if strings.Contains(req.ReceiptID, "/") || strings.Contains(req.ReceiptID, "\\") || strings.Contains(req.ReceiptID, "..") {
			return nil, errs.New(errs.CategoryInvalidArgument, "invalid receipt_id %q", req.ReceiptID)
		}
		if receiptsDir == "" && req.Receipt.Statement.ReceiptID == req.ReceiptID {
			receipt = req.Receipt
		} else {
			if receiptsDir == "" {
				return nil, errs.New(errs.CategoryNotFound, "receipts directory not configured")
			}
			path := filepath.Join(receiptsDir, req.ReceiptID+".json")
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, errs.Wrap(errs.CategoryNotFound, err, "receipt-missing: receipt %s not found", req.ReceiptID)
			}
			if len(data) > 64*1024 {
				return nil, errs.New(errs.CategoryPolicyDenied, "receipt-invalid: receipt %s exceeds 64 KiB cap", req.ReceiptID)
			}
			decR := json.NewDecoder(bytes.NewReader(data))
			decR.DisallowUnknownFields()
			var r Receipt
			if err := decR.Decode(&r); err != nil {
				return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "receipt-invalid: invalid receipt JSON")
			}
			if _, err := decR.Token(); err != io.EOF {
				return nil, errs.New(errs.CategoryInvalidArgument, "receipt-invalid: trailing content in receipt JSON")
			}
			if r.Statement.ReceiptID != req.ReceiptID {
				return nil, errs.New(errs.CategoryPolicyDenied, "receipt_id mismatch: file has %q, request has %q", r.Statement.ReceiptID, req.ReceiptID)
			}
			receipt = r
		}
	} else {
		// Subject discovery
		if receiptsDir == "" && req.Receipt.Statement.ReceiptID != "" {
			receipt = req.Receipt
		} else {
			if receiptsDir == "" {
				return nil, errs.New(errs.CategoryNotFound, "receipt-missing: receipts directory not configured")
			}
			entries, err := os.ReadDir(receiptsDir)
			if err != nil {
				return nil, errs.Wrap(errs.CategoryNotFound, err, "receipt-missing: cannot read receipts directory")
			}
			var jsonFiles []string
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
					jsonFiles = append(jsonFiles, entry.Name())
				}
			}
			sort.Strings(jsonFiles)
			if len(jsonFiles) > 256 {
				jsonFiles = jsonFiles[:256]
			}

			var bestCandidate *Receipt
			for _, name := range jsonFiles {
				filePath := filepath.Join(receiptsDir, name)
				data, err := os.ReadFile(filePath)
				if err != nil || len(data) > 64*1024 {
					continue
				}
				decR := json.NewDecoder(bytes.NewReader(data))
				decR.DisallowUnknownFields()
				var r Receipt
				if err := decR.Decode(&r); err != nil {
					continue
				}
				if _, err := decR.Token(); err != io.EOF {
					continue
				}

				stmt := r.Statement
				if stmt.Purpose != req.Purpose || stmt.ProjectID != req.ProjectID {
					continue
				}
				if stmt.Subject != req.Subject {
					continue
				}
				if req.SubjectDigest != "" && stmt.SubjectDigest != req.SubjectDigest {
					continue
				}

				// Check revocation
				isRevoked := false
				for _, rid := range revDoc.ReceiptIDs {
					if rid == stmt.ReceiptID {
						isRevoked = true
						break
					}
				}
				for _, aid := range revDoc.AnchorIDs {
					if aid == stmt.AnchorID {
						isRevoked = true
						break
					}
				}
				if isRevoked {
					continue
				}

				// Check anchor exists
				anchor, ok := v.anchors[stmt.AnchorID]
				if !ok {
					continue
				}

				// Check full verification
				if err := verifyStatementCommon(stmt, r.Signature, req, anchor, reqTime); err != nil {
					continue
				}

				if bestCandidate == nil || stmt.IssuedAt.After(bestCandidate.Statement.IssuedAt) ||
					(stmt.IssuedAt.Equal(bestCandidate.Statement.IssuedAt) && stmt.ReceiptID > bestCandidate.Statement.ReceiptID) {
					candidate := r
					bestCandidate = &candidate
				}
			}

			if bestCandidate == nil {
				return nil, errs.New(errs.CategoryPolicyDenied, "receipt-missing: no verifying receipt found for subject")
			}
			receipt = *bestCandidate
		}
	}

	// Revocation check
	for _, rid := range revDoc.ReceiptIDs {
		if rid == receipt.Statement.ReceiptID {
			return nil, errs.New(errs.CategoryPolicyDenied, "receipt-revoked: receipt %s has been revoked", receipt.Statement.ReceiptID)
		}
	}
	for _, aid := range revDoc.AnchorIDs {
		if aid == receipt.Statement.AnchorID {
			return nil, errs.New(errs.CategoryPolicyDenied, "receipt-revoked: anchor %s has been revoked", receipt.Statement.AnchorID)
		}
	}

	anchor, ok := v.anchors[receipt.Statement.AnchorID]
	if !ok {
		return nil, errs.New(errs.CategoryPolicyDenied, "trust anchor %q not found", receipt.Statement.AnchorID)
	}

	if err := verifyStatementCommon(receipt.Statement, receipt.Signature, req, anchor, reqTime); err != nil {
		return nil, err
	}

	return verifiedReceipt{
		stmt:  receipt.Statement,
		valid: true,
	}, nil
}

func verifyStatementCommon(stmt Statement, sig string, req Request, anchor TrustAnchor, reqTime time.Time) error {
	if err := stmt.Validate(); err != nil {
		return errs.Wrap(errs.CategoryPolicyDenied, err, "receipt-invalid: statement validation failed")
	}

	if stmt.Purpose != req.Purpose {
		return errs.New(errs.CategoryPolicyDenied,
			"purpose mismatch: receipt statement has %q, request requires %q", stmt.Purpose, req.Purpose)
	}

	if stmt.ProjectID != req.ProjectID {
		return errs.New(errs.CategoryPolicyDenied,
			"project_id mismatch: receipt statement has %q, request requires %q", stmt.ProjectID, req.ProjectID)
	}

	if stmt.Subject.Kind != req.Subject.Kind || stmt.Subject.ID != req.Subject.ID || stmt.Subject.Version != req.Subject.Version {
		return errs.New(errs.CategoryPolicyDenied,
			"subject mismatch: receipt statement has %+v, request requires %+v", stmt.Subject, req.Subject)
	}

	if req.SubjectDigest != "" && stmt.SubjectDigest != req.SubjectDigest {
		return errs.New(errs.CategoryPolicyDenied,
			"subject_digest mismatch: receipt statement has %q, request requires %q", stmt.SubjectDigest, req.SubjectDigest)
	}

	if req.InputDigest != "" && stmt.InputDigest != req.InputDigest {
		return errs.New(errs.CategoryPolicyDenied,
			"input_digest mismatch: receipt statement has %q, request has %q", stmt.InputDigest, req.InputDigest)
	}

	if req.Text != "" && stmt.Text != req.Text {
		return errs.New(errs.CategoryPolicyDenied,
			"text mismatch: receipt statement has %q, request has %q", stmt.Text, req.Text)
	}

	expectedTextDigest := protocol.DigestBytes([]byte(stmt.Text))
	if stmt.TextDigest != expectedTextDigest {
		return errs.New(errs.CategoryPolicyDenied,
			"text_digest invalid: computed %s, statement has %s", expectedTextDigest, stmt.TextDigest)
	}

	if anchor.HumanActorID != stmt.HumanActorID {
		return errs.New(errs.CategoryPolicyDenied,
			"human_actor_id mismatch: anchor has %q, statement has %q", anchor.HumanActorID, stmt.HumanActorID)
	}

	if !anchor.NotBefore.IsZero() && reqTime.Before(anchor.NotBefore) {
		return errs.New(errs.CategoryPolicyDenied,
			"trust anchor %q not yet valid: not_before is %s, request time is %s", anchor.AnchorID, anchor.NotBefore, reqTime)
	}
	if !anchor.NotAfter.IsZero() && reqTime.After(anchor.NotAfter) {
		return errs.New(errs.CategoryPolicyDenied,
			"trust anchor %q expired at %s (request time: %s)", anchor.AnchorID, anchor.NotAfter, reqTime)
	}

	if len(anchor.Purposes) > 0 {
		hasPurpose := false
		for _, p := range anchor.Purposes {
			if p == req.Purpose {
				hasPurpose = true
				break
			}
		}
		if !hasPurpose {
			return errs.New(errs.CategoryPolicyDenied,
				"trust anchor %q does not authorize purpose %q", anchor.AnchorID, req.Purpose)
		}
	}

	if reqTime.Before(stmt.IssuedAt.Add(-5 * time.Minute)) {
		return errs.New(errs.CategoryPolicyDenied,
			"receipt %s not yet valid: issued at %s, request time is %s", stmt.ReceiptID, stmt.IssuedAt, reqTime)
	}
	if reqTime.After(stmt.NotAfter) {
		return errs.New(errs.CategoryPolicyDenied,
			"receipt %s expired at %s (request time: %s)", stmt.ReceiptID, stmt.NotAfter, reqTime)
	}

	payload, err := stmt.SigningPayload()
	if err != nil {
		return errs.Wrap(errs.CategoryPolicyDenied, err, "failed to compute receipt signing payload")
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return errs.Wrap(errs.CategoryPolicyDenied, err, "failed to decode signature base64")
	}
	if len(sigBytes) != ed25519.SignatureSize || !ed25519.Verify(anchor.PublicKey, payload, sigBytes) {
		return errs.New(errs.CategoryPolicyDenied, "signature verification failed for receipt %s", stmt.ReceiptID)
	}

	return nil
}

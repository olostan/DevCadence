package receipts

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// TrustAnchor defines an enrolled operator public key.
type TrustAnchor struct {
	AnchorID     string            `json:"anchor_id"`
	HumanActorID string            `json:"human_actor_id"`
	PublicKey    ed25519.PublicKey `json:"public_key"`
	NotAfter     time.Time         `json:"not_after"`
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

// Verifier verifies operator ingress receipts against pinned trust anchors and revocation lists.
type Verifier struct {
	anchors     map[string]TrustAnchor
	revocations *RevocationList
}

// VerifierOption configures a Verifier.
type VerifierOption func(*Verifier)

// WithRevocationList attaches a RevocationList to the Verifier.
func WithRevocationList(rl *RevocationList) VerifierOption {
	return func(v *Verifier) {
		v.revocations = rl
	}
}

// NewVerifier creates a new Verifier with pinned trust anchors.
func NewVerifier(anchors []TrustAnchor, opts ...VerifierOption) (*Verifier, error) {
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
	v := &Verifier{
		anchors: anchorsMap,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(v)
		}
	}
	return v, nil
}

// SetRevocationList sets or updates the active revocation list.
func (v *Verifier) SetRevocationList(rl *RevocationList) {
	v.revocations = rl
}

// Verify verifies a receipt request against pinned anchors and policy rules.
// It fails closed with typed errs.CategoryPolicyDenied or CategoryInvalidArgument.
func (v *Verifier) Verify(ctx context.Context, req Request) (Verified, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	stmt := req.Receipt.Statement

	// Purpose match
	if stmt.Purpose != req.Purpose {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"purpose mismatch: receipt statement has %q, request requires %q", stmt.Purpose, req.Purpose)
	}

	// ProjectID match
	if stmt.ProjectID != req.ProjectID {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"project_id mismatch: receipt statement has %q, request requires %q", stmt.ProjectID, req.ProjectID)
	}

	// Subject match (Kind, ID, Version)
	if stmt.Subject.Kind != req.Subject.Kind || stmt.Subject.ID != req.Subject.ID || stmt.Subject.Version != req.Subject.Version {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"subject mismatch: receipt statement has %+v, request requires %+v", stmt.Subject, req.Subject)
	}

	// InputDigest match if specified in request
	if req.InputDigest != "" && stmt.InputDigest != req.InputDigest {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"input_digest mismatch: receipt statement has %q, request has %q", stmt.InputDigest, req.InputDigest)
	}

	// Text match if specified in request
	if req.Text != "" && stmt.Text != req.Text {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"text mismatch: receipt statement has %q, request has %q", stmt.Text, req.Text)
	}

	// Digest verification
	expectedTextDigest := protocol.DigestBytes([]byte(stmt.Text))
	if stmt.TextDigest != expectedTextDigest {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"text_digest invalid: computed %s, statement has %s", expectedTextDigest, stmt.TextDigest)
	}

	// Anchor lookup
	anchor, ok := v.anchors[stmt.AnchorID]
	if !ok {
		return nil, errs.New(errs.CategoryPolicyDenied, "trust anchor %q not found", stmt.AnchorID)
	}

	// Anchor HumanActorID match
	if anchor.HumanActorID != stmt.HumanActorID {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"human_actor_id mismatch: anchor has %q, statement has %q", anchor.HumanActorID, stmt.HumanActorID)
	}

	// Anchor expiration check
	if req.Time.After(anchor.NotAfter) {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"trust anchor %q expired at %s (request time: %s)", anchor.AnchorID, anchor.NotAfter, req.Time)
	}

	// Revocation check
	if v.revocations != nil && v.revocations.IsRevoked(stmt.ReceiptID) {
		return nil, errs.New(errs.CategoryPolicyDenied, "receipt %s has been revoked", stmt.ReceiptID)
	}

	// Statement time window check: req.Time >= IssuedAt && req.Time <= NotAfter
	if req.Time.Before(stmt.IssuedAt) {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"receipt %s not yet valid: issued at %s, request time is %s", stmt.ReceiptID, stmt.IssuedAt, req.Time)
	}
	if req.Time.After(stmt.NotAfter) {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"receipt %s expired at %s (request time: %s)", stmt.ReceiptID, stmt.NotAfter, req.Time)
	}

	// Cryptographic signature check
	payload, err := stmt.SigningPayload()
	if err != nil {
		return nil, errs.Wrap(errs.CategoryPolicyDenied, err, "failed to compute receipt signing payload")
	}

	sigBytes, err := base64.StdEncoding.DecodeString(req.Receipt.Signature)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryPolicyDenied, err, "failed to decode signature base64")
	}

	if !ed25519.Verify(anchor.PublicKey, payload, sigBytes) {
		return nil, errs.New(errs.CategoryPolicyDenied, "signature verification failed for receipt %s", stmt.ReceiptID)
	}

	return verifiedReceipt{
		stmt:  stmt,
		valid: true,
	}, nil
}

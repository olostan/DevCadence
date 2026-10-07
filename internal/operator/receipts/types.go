package receipts

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// Purpose defines the closed set of operator ingress receipt purposes.
type Purpose string

const (
	PurposeDiscoveryProductDecision    Purpose = "discovery.product_decision"
	PurposeDiscoveryRequirementConfirm Purpose = "discovery.requirement_confirm"
	PurposeDiscoveryLedgerResolution   Purpose = "discovery.ledger_resolution"
	PurposeDiscoveryReflection         Purpose = "discovery.reflection"
	PurposeDiscoveryAcceptedRisk       Purpose = "discovery.accepted_risk"
	PurposeHostPlanApply               Purpose = "host.plan_apply"
	PurposeAcceptancePolicyActivate    Purpose = "acceptance.policy_activate"
	PurposeExecutionPolicyActivate     Purpose = "execution.policy_activate"
	PurposeEmpiricalCampaignAuthorize  Purpose = "empirical.campaign_authorize"
)

// Valid reports whether the purpose is in the closed set.
func (p Purpose) Valid() bool {
	switch p {
	case PurposeDiscoveryProductDecision,
		PurposeDiscoveryRequirementConfirm,
		PurposeDiscoveryLedgerResolution,
		PurposeDiscoveryReflection,
		PurposeDiscoveryAcceptedRisk,
		PurposeHostPlanApply,
		PurposeAcceptancePolicyActivate,
		PurposeExecutionPolicyActivate,
		PurposeEmpiricalCampaignAuthorize:
		return true
	}
	return false
}

// Use defines whether a receipt is consumed once or represents a reusable grant.
type Use string

const (
	UseOnce  Use = "once"
	UseGrant Use = "grant"
)

// Valid reports whether the use is valid.
func (u Use) Valid() bool {
	switch u {
	case UseOnce, UseGrant:
		return true
	}
	return false
}

// Subject binds a receipt to a specific target entity and version.
type Subject struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// Validate checks non-empty Kind, ID, and Version >= 1.
func (s Subject) Validate() error {
	const kind = "Subject"
	if strings.TrimSpace(s.Kind) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: kind is required", kind)
	}
	if strings.TrimSpace(s.ID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: id is required", kind)
	}
	if s.Version < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: version must be >= 1, got %d", kind, s.Version)
	}
	return nil
}

// Statement is the operator-authorized claim before signature.
type Statement struct {
	Version       string    `json:"version"`
	ReceiptID     string    `json:"receipt_id"`
	Purpose       Purpose   `json:"purpose"`
	Use           Use       `json:"use"`
	ProjectID     string    `json:"project_id"`
	Subject       Subject   `json:"subject"`
	SubjectDigest string    `json:"subject_digest"`
	InputDigest   string    `json:"input_digest"`
	Text          string    `json:"text"`
	TextDigest    string    `json:"text_digest"`
	HumanActorID  string    `json:"human_actor_id"`
	AnchorID      string    `json:"anchor_id"`
	IssuedAt      time.Time `json:"issued_at"`
	NotAfter      time.Time `json:"not_after"`
}

// Validate enforces all fields, time ranges (NotAfter.After(IssuedAt)), and ReceiptID prefix rcpt_.
func (s Statement) Validate() error {
	const kind = "Statement"
	if s.Version != "1.0" {
		return errs.New(errs.CategoryInvalidArgument, "%s: version must be 1.0, got %q", kind, s.Version)
	}
	if !strings.HasPrefix(s.ReceiptID, "rcpt_") || len(strings.TrimSpace(s.ReceiptID)) <= 5 {
		return errs.New(errs.CategoryInvalidArgument, "%s: receipt_id must start with rcpt_, got %q", kind, s.ReceiptID)
	}
	if !s.Purpose.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid purpose %q", kind, string(s.Purpose))
	}
	if !s.Use.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid use %q", kind, string(s.Use))
	}
	if strings.TrimSpace(s.ProjectID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: project_id is required", kind)
	}
	if err := s.Subject.Validate(); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "%s: invalid subject", kind)
	}
	if strings.TrimSpace(s.SubjectDigest) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: subject_digest is required", kind)
	}
	if strings.TrimSpace(s.InputDigest) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: input_digest is required", kind)
	}
	if strings.TrimSpace(s.Text) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: text is required", kind)
	}
	if strings.TrimSpace(s.TextDigest) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: text_digest is required", kind)
	}
	if expected := protocol.DigestBytes([]byte(s.Text)); s.TextDigest != expected {
		return errs.New(errs.CategoryInvalidArgument, "%s: text_digest mismatch (expected %s, got %s)", kind, expected, s.TextDigest)
	}
	if strings.TrimSpace(s.HumanActorID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: human_actor_id is required", kind)
	}
	if strings.TrimSpace(s.AnchorID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: anchor_id is required", kind)
	}
	if s.IssuedAt.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: issued_at is required", kind)
	}
	if s.NotAfter.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: not_after is required", kind)
	}
	if !s.NotAfter.After(s.IssuedAt) {
		return errs.New(errs.CategoryInvalidArgument, "%s: not_after (%s) must be after issued_at (%s)", kind, s.NotAfter, s.IssuedAt)
	}
	return nil
}

// ReceiptSignatureDomain separates receipt signatures from other domain messages.
const ReceiptSignatureDomain = "devcadence-receipt/1\n"

// CanonicalBytes returns the canonical JSON encoding of Statement.
func (s Statement) CanonicalBytes() ([]byte, error) {
	return protocol.CanonicalJSON(s)
}

// SigningPayload returns "devcadence-receipt/1\n" concatenated with canonical Statement bytes.
func (s Statement) SigningPayload() ([]byte, error) {
	canonicalBytes, err := s.CanonicalBytes()
	if err != nil {
		return nil, err
	}
	payload := make([]byte, len(ReceiptSignatureDomain)+len(canonicalBytes))
	copy(payload, ReceiptSignatureDomain)
	copy(payload[len(ReceiptSignatureDomain):], canonicalBytes)
	return payload, nil
}

// Sign signs the Statement with the provided Ed25519 private key, producing a Receipt.
func (s Statement) Sign(privKey ed25519.PrivateKey) (Receipt, error) {
	if err := s.Validate(); err != nil {
		return Receipt{}, err
	}
	payload, err := s.SigningPayload()
	if err != nil {
		return Receipt{}, err
	}
	sig := ed25519.Sign(privKey, payload)
	return Receipt{
		Statement: s,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}, nil
}

// Receipt bundles a validated Statement and its base64 Ed25519 signature.
type Receipt struct {
	Statement Statement `json:"statement"`
	Signature string    `json:"signature"`
}

// Validate checks statement validity and base64 signature encoding.
func (r Receipt) Validate() error {
	const kind = "Receipt"
	if err := r.Statement.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.Signature) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: signature is required", kind)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "%s: signature must be valid base64", kind)
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return errs.New(errs.CategoryInvalidArgument, "%s: signature must be %d bytes, got %d", kind, ed25519.SignatureSize, len(sigBytes))
	}
	return nil
}

// Request bundles verification arguments.
type Request struct {
	Receipt     Receipt   `json:"receipt"`
	Purpose     Purpose   `json:"purpose"`
	ProjectID   string    `json:"project_id"`
	Subject     Subject   `json:"subject"`
	InputDigest string    `json:"input_digest"`
	Text        string    `json:"text"`
	Time        time.Time `json:"time"`
}

// Validate checks request completeness.
func (r Request) Validate() error {
	const kind = "Request"
	if err := r.Receipt.Validate(); err != nil {
		return err
	}
	if !r.Purpose.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid purpose %q", kind, string(r.Purpose))
	}
	if strings.TrimSpace(r.ProjectID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: project_id is required", kind)
	}
	if err := r.Subject.Validate(); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "%s: invalid subject", kind)
	}
	if r.Time.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: time is required", kind)
	}
	return nil
}

// Consumption records a single consumption or audit use of an ingress receipt.
type Consumption struct {
	SchemaVersion protocol.SchemaVersion `json:"schema_version"`
	ReceiptID     string                 `json:"receipt_id"`
	ProjectID     string                 `json:"project_id"`
	HumanActorID  string                 `json:"human_actor_id"`
	Purpose       Purpose                `json:"purpose"`
	EffectKind    string                 `json:"effect_kind"`
	EffectID      string                 `json:"effect_id"`
	ConsumedAt    time.Time              `json:"consumed_at"`
}

// RecordKind implements protocol.Record.
func (c *Consumption) RecordKind() string { return "ReceiptConsumption" }

// RecordID implements protocol.Record.
func (c *Consumption) RecordID() string { return c.ReceiptID }

// SchemaVer implements protocol.Record.
func (c *Consumption) SchemaVer() protocol.SchemaVersion { return c.SchemaVersion }

// ProjectOf implements protocol.ProjectScoped.
func (c *Consumption) ProjectOf() string { return c.ProjectID }

// Validate checks Consumption constraints.
func (c *Consumption) Validate() error {
	const kind = "ReceiptConsumption"
	if err := c.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if !strings.HasPrefix(c.ReceiptID, "rcpt_") {
		return errs.New(errs.CategoryInvalidArgument, "%s: receipt_id must start with rcpt_, got %q", kind, c.ReceiptID)
	}
	if strings.TrimSpace(c.ProjectID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: project_id is required", kind)
	}
	if strings.TrimSpace(c.HumanActorID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: human_actor_id is required", kind)
	}
	if !c.Purpose.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid purpose %q", kind, string(c.Purpose))
	}
	if strings.TrimSpace(c.EffectKind) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: effect_kind is required", kind)
	}
	if strings.TrimSpace(c.EffectID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: effect_id is required", kind)
	}
	if c.ConsumedAt.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: consumed_at is required", kind)
	}
	return nil
}

func init() {
	protocol.RegisterCustomRecord("ReceiptConsumption", func() protocol.Record {
		return &Consumption{}
	})
}

// Verified represents a cryptographically and policy verified receipt.
type Verified interface {
	Statement() Statement
	IsValid() bool
}

type verifiedReceipt struct {
	stmt  Statement
	valid bool
}

func (v verifiedReceipt) Statement() Statement {
	if !v.valid {
		return Statement{}
	}
	return v.stmt
}

func (v verifiedReceipt) IsValid() bool {
	return v.valid
}

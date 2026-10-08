package receipts

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestReceipt_Validate(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_types_01"
	actorID := "operator-alice"

	validStmt := validStatement(pub, anchorID, actorID)
	validRcpt, err := validStmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign valid statement: %v", err)
	}

	tests := []struct {
		name    string
		modify  func(r *Receipt)
		wantErr bool
		cat     errs.Category
	}{
		{
			name:    "valid receipt",
			modify:  nil,
			wantErr: false,
		},
		{
			name: "invalid statement version",
			modify: func(r *Receipt) {
				r.Statement.Version = "2.0"
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "invalid statement receipt ID",
			modify: func(r *Receipt) {
				r.Statement.ReceiptID = "invalid_id"
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "empty signature",
			modify: func(r *Receipt) {
				r.Signature = ""
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "whitespace signature",
			modify: func(r *Receipt) {
				r.Signature = "   "
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "malformed base64 signature",
			modify: func(r *Receipt) {
				r.Signature = "!!!not-valid-base64!!!"
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "signature length too short",
			modify: func(r *Receipt) {
				short := make([]byte, 32)
				r.Signature = base64.StdEncoding.EncodeToString(short)
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "signature length too long",
			modify: func(r *Receipt) {
				long := make([]byte, 128)
				r.Signature = base64.StdEncoding.EncodeToString(long)
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rcpt := validRcpt
			if tc.modify != nil {
				tc.modify(&rcpt)
			}
			err := rcpt.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.cat != "" && errs.CategoryOf(err) != tc.cat {
					t.Errorf("category = %v, want %v", errs.CategoryOf(err), tc.cat)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConsumption_ValidateAndRecord(t *testing.T) {
	now := time.Now().UTC()
	validConsumption := func() *Consumption {
		return &Consumption{
			SchemaVersion: protocol.SchemaVersion1,
			ReceiptID:     "rcpt_01j7abc1234567890abcdef123",
			ProjectID:     "test-project",
			HumanActorID:  "operator-alice",
			Purpose:       PurposeHostPlanApply,
			SubjectDigest: "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
			InputDigest:   "",
			AnchorID:      "anchor_01",
			EffectKind:    "host_plan_apply",
			EffectID:      "effect_123",
			ConsumedAt:    now,
		}
	}

	// Verify protocol.Record and ProjectScoped interface methods
	c := validConsumption()
	if c.RecordKind() != "ReceiptConsumption" {
		t.Errorf("RecordKind = %q, want ReceiptConsumption", c.RecordKind())
	}
	if c.RecordID() != c.ReceiptID {
		t.Errorf("RecordID = %q, want %q", c.RecordID(), c.ReceiptID)
	}
	if c.SchemaVer() != protocol.SchemaVersion1 {
		t.Errorf("SchemaVer = %v, want %v", c.SchemaVer(), protocol.SchemaVersion1)
	}
	if c.ProjectOf() != "test-project" {
		t.Errorf("ProjectOf = %q, want %q", c.ProjectOf(), "test-project")
	}

	// Verify factory registration
	rec, err := protocol.NewRecord("ReceiptConsumption")
	if err != nil {
		t.Fatalf("NewRecord(ReceiptConsumption): %v", err)
	}
	if rec.RecordKind() != "ReceiptConsumption" {
		t.Errorf("NewRecord kind = %q, want ReceiptConsumption", rec.RecordKind())
	}

	tests := []struct {
		name    string
		modify  func(c *Consumption)
		wantErr bool
		cat     errs.Category
	}{
		{
			name:    "valid consumption",
			modify:  nil,
			wantErr: false,
		},
		{
			name: "invalid schema version",
			modify: func(c *Consumption) {
				c.SchemaVersion = "2.0"
			},
			wantErr: true,
			cat:     errs.CategorySchemaVersionUnsupported,
		},
		{
			name: "missing rcpt_ prefix on receipt ID",
			modify: func(c *Consumption) {
				c.ReceiptID = "not_rcpt_prefix"
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "empty project ID",
			modify: func(c *Consumption) {
				c.ProjectID = "  "
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "empty human actor ID",
			modify: func(c *Consumption) {
				c.HumanActorID = ""
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "invalid purpose",
			modify: func(c *Consumption) {
				c.Purpose = "invalid.purpose"
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "empty subject digest",
			modify: func(c *Consumption) {
				c.SubjectDigest = " "
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "empty anchor ID",
			modify: func(c *Consumption) {
				c.AnchorID = ""
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "empty effect kind",
			modify: func(c *Consumption) {
				c.EffectKind = " "
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "empty effect ID",
			modify: func(c *Consumption) {
				c.EffectID = ""
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "zero consumed at",
			modify: func(c *Consumption) {
				c.ConsumedAt = time.Time{}
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cons := validConsumption()
			if tc.modify != nil {
				tc.modify(cons)
			}
			err := cons.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.cat != "" && errs.CategoryOf(err) != tc.cat {
					t.Errorf("category = %v, want %v", errs.CategoryOf(err), tc.cat)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestRequest_Validate(t *testing.T) {
	validRequest := func() Request {
		return Request{
			ReceiptID:     "rcpt_01j7abc1234567890abcdef123",
			Purpose:       PurposeHostPlanApply,
			ProjectID:     "test-project",
			Subject:       Subject{Kind: "HostPlan", ID: "sha256:1111", Version: 1},
			SubjectDigest: "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
			InputDigest:   "",
		}
	}

	tests := []struct {
		name    string
		modify  func(r *Request)
		wantErr bool
		cat     errs.Category
	}{
		{
			name:    "valid request with receipt ID",
			modify:  nil,
			wantErr: false,
		},
		{
			name: "valid request without receipt ID",
			modify: func(r *Request) {
				r.ReceiptID = ""
			},
			wantErr: false,
		},
		{
			name: "invalid purpose",
			modify: func(r *Request) {
				r.Purpose = "unknown.purpose"
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "empty project ID",
			modify: func(r *Request) {
				r.ProjectID = "  "
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "invalid subject empty kind",
			modify: func(r *Request) {
				r.Subject.Kind = ""
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "invalid subject empty id",
			modify: func(r *Request) {
				r.Subject.ID = ""
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "invalid subject zero version",
			modify: func(r *Request) {
				r.Subject.Version = 0
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "empty subject digest",
			modify: func(r *Request) {
				r.SubjectDigest = "   "
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
		{
			name: "invalid receipt ID format",
			modify: func(r *Request) {
				r.ReceiptID = "not-a-valid-receipt-id"
			},
			wantErr: true,
			cat:     errs.CategoryInvalidArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			if tc.modify != nil {
				tc.modify(&req)
			}
			err := req.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.cat != "" && errs.CategoryOf(err) != tc.cat {
					t.Errorf("category = %v, want %v", errs.CategoryOf(err), tc.cat)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestPurpose_SpecsAndHelpers(t *testing.T) {
	purposes := []struct {
		purpose            Purpose
		wantUse            Use
		wantMaxValidity    time.Duration
		wantRequiresDigest bool
	}{
		{
			purpose:            PurposeDiscoveryProductDecision,
			wantUse:            UseOnce,
			wantMaxValidity:    time.Hour,
			wantRequiresDigest: true,
		},
		{
			purpose:            PurposeDiscoveryRequirementConfirm,
			wantUse:            UseOnce,
			wantMaxValidity:    time.Hour,
			wantRequiresDigest: true,
		},
		{
			purpose:            PurposeDiscoveryLedgerResolution,
			wantUse:            UseOnce,
			wantMaxValidity:    time.Hour,
			wantRequiresDigest: false,
		},
		{
			purpose:            PurposeDiscoveryReflection,
			wantUse:            UseOnce,
			wantMaxValidity:    time.Hour,
			wantRequiresDigest: true,
		},
		{
			purpose:            PurposeDiscoveryAcceptedRisk,
			wantUse:            UseOnce,
			wantMaxValidity:    time.Hour,
			wantRequiresDigest: true,
		},
		{
			purpose:            PurposeHostPlanApply,
			wantUse:            UseOnce,
			wantMaxValidity:    time.Hour,
			wantRequiresDigest: false,
		},
		{
			purpose:            PurposeAcceptancePolicyActivate,
			wantUse:            UseGrant,
			wantMaxValidity:    30 * 24 * time.Hour,
			wantRequiresDigest: false,
		},
		{
			purpose:            PurposeExecutionPolicyActivate,
			wantUse:            UseGrant,
			wantMaxValidity:    30 * 24 * time.Hour,
			wantRequiresDigest: false,
		},
		{
			purpose:            PurposeEmpiricalCampaignAuthorize,
			wantUse:            UseGrant,
			wantMaxValidity:    7 * 24 * time.Hour,
			wantRequiresDigest: false,
		},
	}

	for _, p := range purposes {
		t.Run(string(p.purpose), func(t *testing.T) {
			if !p.purpose.Valid() {
				t.Fatalf("expected purpose %s to be valid", p.purpose)
			}
			use, err := ExpectedUseForPurpose(p.purpose)
			if err != nil {
				t.Fatalf("ExpectedUseForPurpose error: %v", err)
			}
			if use != p.wantUse {
				t.Errorf("ExpectedUseForPurpose = %v, want %v", use, p.wantUse)
			}
			maxVal, err := MaxValidityForPurpose(p.purpose)
			if err != nil {
				t.Fatalf("MaxValidityForPurpose error: %v", err)
			}
			if maxVal != p.wantMaxValidity {
				t.Errorf("MaxValidityForPurpose = %v, want %v", maxVal, p.wantMaxValidity)
			}
			reqDigest := RequiresInputDigest(p.purpose)
			if reqDigest != p.wantRequiresDigest {
				t.Errorf("RequiresInputDigest = %v, want %v", reqDigest, p.wantRequiresDigest)
			}
		})
	}

	// Unknown purpose edge cases
	unknown := Purpose("unknown.nonexistent")
	if unknown.Valid() {
		t.Errorf("expected unknown purpose Valid() to be false")
	}
	if _, err := ExpectedUseForPurpose(unknown); err == nil {
		t.Errorf("expected error from ExpectedUseForPurpose(unknown), got nil")
	}
	if _, err := MaxValidityForPurpose(unknown); err == nil {
		t.Errorf("expected error from MaxValidityForPurpose(unknown), got nil")
	}
	if RequiresInputDigest(unknown) {
		t.Errorf("expected RequiresInputDigest(unknown) to be false")
	}

	// Use validation
	if !UseOnce.Valid() {
		t.Errorf("UseOnce.Valid() = false")
	}
	if !UseGrant.Valid() {
		t.Errorf("UseGrant.Valid() = false")
	}
	if Use("other").Valid() {
		t.Errorf("Use('other').Valid() = true")
	}

	// Receipt ID validation
	if !IsValidReceiptID("rcpt_01j7abc1234567890abcdef123") {
		t.Errorf("valid receipt ID rejected")
	}
	if IsValidReceiptID("rcpt_short") {
		t.Errorf("short receipt ID accepted")
	}
	if IsValidReceiptID("invalid_prefix1234567890abcdef123") {
		t.Errorf("invalid prefix accepted")
	}
	if IsValidReceiptID("rcpt_UPPERCASE1234567890abcdef123") {
		t.Errorf("uppercase receipt ID accepted")
	}

	// verifiedReceipt coverage
	stmt := Statement{ReceiptID: "rcpt_01j7abc1234567890abcdef123"}
	vrValid := verifiedReceipt{stmt: stmt, valid: true}
	if !vrValid.IsValid() {
		t.Errorf("expected vrValid.IsValid() to be true")
	}
	if vrValid.Statement().ReceiptID != stmt.ReceiptID {
		t.Errorf("vrValid.Statement().ReceiptID = %q, want %q", vrValid.Statement().ReceiptID, stmt.ReceiptID)
	}

	vrInvalid := verifiedReceipt{stmt: stmt, valid: false}
	if vrInvalid.IsValid() {
		t.Errorf("expected vrInvalid.IsValid() to be false")
	}
	if vrInvalid.Statement().ReceiptID != "" {
		t.Errorf("expected empty statement for invalid verifiedReceipt, got %q", vrInvalid.Statement().ReceiptID)
	}
}

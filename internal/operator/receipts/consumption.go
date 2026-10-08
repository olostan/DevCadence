package receipts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

type consumeOnceGuard struct {
	receiptID string
}

// Check checks for receipt replay in the controlplane batch transaction.
func (g *consumeOnceGuard) Check(ctx context.Context, view controlplane.BatchReadView) error {
	_, err := view.Record(ctx, "ReceiptConsumption", g.receiptID, 1)
	if err == nil {
		return errs.New(errs.CategoryConflict, "receipt-replayed: receipt %s has already been consumed", g.receiptID)
	}
	if errs.CategoryOf(err) == errs.CategoryNotFound {
		return nil
	}
	return errs.Wrap(errs.CategoryInternal, err, "failed to check receipt consumption replay status for %s", g.receiptID)
}

// ConsumeOnce binds a single-use verified receipt to an effect within a controlplane batch.
// It returns a BatchGuard to prevent replay and a RecordToStore to commit alongside the effect.
// It refuses if v is invalid or if the receipt was issued for UseGrant.
func ConsumeOnce(v Verified, effectKind, effectID string) (controlplane.BatchGuard, controlplane.RecordToStore, error) {
	if v == nil || !v.IsValid() {
		return nil, controlplane.RecordToStore{}, errs.New(errs.CategoryPolicyDenied, "receipt is not valid")
	}
	stmt := v.Statement()
	if stmt.Use == UseGrant {
		return nil, controlplane.RecordToStore{}, errs.New(errs.CategoryPolicyDenied,
			"cannot ConsumeOnce a grant receipt (receipt use is %q)", stmt.Use)
	}
	if strings.TrimSpace(effectKind) == "" {
		return nil, controlplane.RecordToStore{}, errs.New(errs.CategoryInvalidArgument, "effectKind is required")
	}
	if strings.TrimSpace(effectID) == "" {
		return nil, controlplane.RecordToStore{}, errs.New(errs.CategoryInvalidArgument, "effectID is required")
	}

	guard := &consumeOnceGuard{
		receiptID: stmt.ReceiptID,
	}

	record := controlplane.RecordToStore{
		Version: 1,
		Record: &Consumption{
			SchemaVersion: protocol.SchemaVersion1,
			ReceiptID:     stmt.ReceiptID,
			ProjectID:     stmt.ProjectID,
			HumanActorID:  stmt.HumanActorID,
			Purpose:       stmt.Purpose,
			SubjectDigest: stmt.SubjectDigest,
			InputDigest:   stmt.InputDigest,
			AnchorID:      stmt.AnchorID,
			EffectKind:    effectKind,
			EffectID:      effectID,
			ConsumedAt:    time.Now().UTC(),
		},
	}

	return guard, record, nil
}

// AuditGrantUse creates a durable audit record for a verified grant receipt usage.
// It refuses if v is invalid or if the receipt was not issued for UseGrant.
func AuditGrantUse(v Verified, effectKind, effectID string) (controlplane.RecordToStore, error) {
	if v == nil || !v.IsValid() {
		return controlplane.RecordToStore{}, errs.New(errs.CategoryPolicyDenied, "receipt is not valid")
	}
	stmt := v.Statement()
	if stmt.Use != UseGrant {
		return controlplane.RecordToStore{}, errs.New(errs.CategoryPolicyDenied,
			"cannot AuditGrantUse on non-grant receipt (receipt use is %q, want %q)", stmt.Use, UseGrant)
	}
	if strings.TrimSpace(effectKind) == "" {
		return controlplane.RecordToStore{}, errs.New(errs.CategoryInvalidArgument, "effectKind is required")
	}
	if strings.TrimSpace(effectID) == "" {
		return controlplane.RecordToStore{}, errs.New(errs.CategoryInvalidArgument, "effectID is required")
	}

	record := controlplane.RecordToStore{
		Version: 1,
		Record: &Consumption{
			SchemaVersion: protocol.SchemaVersion1,
			ReceiptID:     fmt.Sprintf("%s:%s", stmt.ReceiptID, effectID),
			ProjectID:     stmt.ProjectID,
			HumanActorID:  stmt.HumanActorID,
			Purpose:       stmt.Purpose,
			SubjectDigest: stmt.SubjectDigest,
			InputDigest:   stmt.InputDigest,
			AnchorID:      stmt.AnchorID,
			EffectKind:    effectKind,
			EffectID:      effectID,
			ConsumedAt:    time.Now().UTC(),
		},
	}

	return record, nil
}

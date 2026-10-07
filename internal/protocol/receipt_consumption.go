package protocol

import (
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
)

// ReceiptConsumption is the durable record of a single consumption of an operator ingress receipt.
type ReceiptConsumption struct {
	SchemaVersion SchemaVersion `json:"schema_version"`
	ReceiptID     string        `json:"receipt_id"`
	ProjectID     string        `json:"project_id"`
	HumanActorID  string        `json:"human_actor_id"`
	Purpose       string        `json:"purpose"`
	EffectKind    string        `json:"effect_kind"`
	EffectID      string        `json:"effect_id"`
	ConsumedAt    time.Time     `json:"consumed_at"`
}

// RecordKind implements Record.
func (r *ReceiptConsumption) RecordKind() string { return "ReceiptConsumption" }

// RecordID implements Record.
func (r *ReceiptConsumption) RecordID() string { return r.ReceiptID }

// SchemaVer implements Record.
func (r *ReceiptConsumption) SchemaVer() SchemaVersion { return r.SchemaVersion }

// ProjectOf implements ProjectScoped.
func (r *ReceiptConsumption) ProjectOf() string { return r.ProjectID }

// Validate validates ReceiptConsumption fields.
func (r *ReceiptConsumption) Validate() error {
	const kind = "ReceiptConsumption"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if !strings.HasPrefix(r.ReceiptID, "rcpt_") {
		return errs.New(errs.CategoryInvalidArgument, "%s: receipt_id must start with rcpt_, got %q", kind, r.ReceiptID)
	}
	if strings.TrimSpace(r.ProjectID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: project_id is required", kind)
	}
	if strings.TrimSpace(r.HumanActorID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: human_actor_id is required", kind)
	}
	if strings.TrimSpace(r.Purpose) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: purpose is required", kind)
	}
	if strings.TrimSpace(r.EffectKind) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: effect_kind is required", kind)
	}
	if strings.TrimSpace(r.EffectID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: effect_id is required", kind)
	}
	if r.ConsumedAt.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: consumed_at is required", kind)
	}
	return nil
}

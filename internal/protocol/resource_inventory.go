package protocol

import (
	"github.com/olostan/DevCadence/internal/errs"
)

// HardwareSummary is a compact, non-duplicative projection of
// EnvironmentFacts. It deliberately does not re-embed the full facts
// struct — those are looked up by MachineFingerprint when needed, per
// ADR-0013's "machine profiles are computed rather than persisted"
// discipline, which this type follows rather than becoming a second
// source of truth for hardware facts.
type HardwareSummary struct {
	OSFamily            OSFamily      `json:"os_family"`
	Arch                string        `json:"arch"`
	LogicalCores        int           `json:"logical_cores"`
	TotalMemoryBytes    *int64        `json:"total_memory_bytes,omitempty"`
	AcceleratorBackends []BackendKind `json:"accelerator_backends,omitempty"`
}

// Validate checks the hardware summary is well-formed.
func (h HardwareSummary) Validate() error {
	const kind = "HardwareSummary"
	if !h.OSFamily.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid os_family %q", kind, string(h.OSFamily))
	}
	if h.Arch == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: arch is required", kind)
	}
	if h.LogicalCores < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: logical_cores must not be negative", kind)
	}
	if h.TotalMemoryBytes != nil && *h.TotalMemoryBytes < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: total_memory_bytes must not be negative", kind)
	}
	return nil
}

// CredentialInventoryEntry pairs a credential reference with its most
// recent authentication evidence — both exactly the WP-M3B-4 types, so
// this introduces no new secret-adjacent field (DCI-081).
type CredentialInventoryEntry struct {
	Ref      CredentialRef `json:"ref"`
	Evidence AuthEvidence  `json:"evidence"`
}

// Validate checks the entry and that the evidence actually describes the
// paired reference (same RefID/Kind), so an inventory can never pair a
// reference with evidence that was observed for a different credential.
func (e CredentialInventoryEntry) Validate() error {
	const kind = "CredentialInventoryEntry"
	if err := e.Ref.Validate(); err != nil {
		return err
	}
	if err := e.Evidence.Validate(); err != nil {
		return err
	}
	if e.Evidence.RefID != e.Ref.RefID {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: evidence.ref_id %q does not match ref.ref_id %q", kind, e.Evidence.RefID, e.Ref.RefID)
	}
	if e.Evidence.Kind != e.Ref.Kind {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: evidence.kind %q does not match ref.kind %q", kind, e.Evidence.Kind, e.Ref.Kind)
	}
	return nil
}

// PolicySummary is the "available economic/policy metadata" the
// WP-M3B-5 scope card asks for, at the fidelity that actually exists
// today (the WP-M3A cognition.Policy routing policy) — not the M3C
// EconomicRegime/BudgetPool types, which are not yet built. A nil
// PolicySummary on ResourceInventory means no active policy override;
// callers apply the same default-policy fallback evaluateReadiness and
// ProfileRecommender.Recommend already use.
type PolicySummary struct {
	MaxSourceExposure SourceExposure `json:"max_source_exposure"`
	MaxCostClass      CostClass      `json:"max_cost_class"`
}

// Validate checks the policy summary is well-formed.
func (p PolicySummary) Validate() error {
	const kind = "PolicySummary"
	if !p.MaxSourceExposure.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid max_source_exposure %q", kind, string(p.MaxSourceExposure))
	}
	if !p.MaxCostClass.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid max_cost_class %q", kind, string(p.MaxCostClass))
	}
	return nil
}

// ResourceInventory is a deterministic, point-in-time snapshot of the
// facts M3C/M3D's Portfolio Planner will read — never a decision itself
// (ADR-0014 §92, ADR-0018). It references MachineCapabilityProfile and
// CognitionEndpointSummary by the same provenance-carrying shapes those
// types already use (ADR-0011) rather than re-deriving capability
// grades, and follows ADR-0013's "computed, not persisted" discipline:
// callers key freshness off MachineFingerprint/ObservedAt exactly as
// DoctorReport already does. It has no Recommend/Select/Score method —
// it decides nothing; a future consumer reads it and decides.
type ResourceInventory struct {
	SchemaVersion      SchemaVersion              `json:"schema_version"`
	InventoryID        string                     `json:"inventory_id"`
	MachineFingerprint string                     `json:"machine_fingerprint"`
	ObservedAt         Timestamp                  `json:"observed_at"`
	Hardware           HardwareSummary            `json:"hardware"`
	CognitionEndpoints []CognitionEndpointSummary `json:"cognition_endpoints,omitempty"`
	PrincipalHosts     []PrincipalHostSummary     `json:"principal_hosts,omitempty"`
	Credentials        []CredentialInventoryEntry `json:"credentials,omitempty"`
	Policy             *PolicySummary             `json:"policy,omitempty"`
}

// RecordKind implements Record.
func (r *ResourceInventory) RecordKind() string { return "ResourceInventory" }

// RecordID implements Record.
func (r *ResourceInventory) RecordID() string { return r.InventoryID }

// SchemaVer implements Record.
func (r *ResourceInventory) SchemaVer() SchemaVersion { return r.SchemaVersion }

// Validate checks the inventory is well-formed and internally consistent.
func (r *ResourceInventory) Validate() error {
	const kind = "ResourceInventory"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "inventory_id", r.InventoryID); err != nil {
		return err
	}
	if !hexSha256Regex.MatchString(r.MachineFingerprint) {
		return errs.New(errs.CategoryInvalidArgument, "%s: machine_fingerprint must be sha256 hex, got %q", kind, r.MachineFingerprint)
	}
	if r.ObservedAt.Time().IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: observed_at is required", kind)
	}
	if err := r.Hardware.Validate(); err != nil {
		return err
	}
	for _, ep := range r.CognitionEndpoints {
		if ep.ID == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: cognition endpoint id cannot be empty", kind)
		}
		if !ep.Kind.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid cognition endpoint kind %q", kind, ep.Kind)
		}
	}
	for _, h := range r.PrincipalHosts {
		if err := h.Validate(); err != nil {
			return err
		}
	}
	for _, c := range r.Credentials {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	if r.Policy != nil {
		if err := r.Policy.Validate(); err != nil {
			return err
		}
	}
	return nil
}

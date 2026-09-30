package protocol

import "github.com/olostan/DevCadence/internal/errs"

// EconomicRegime defines billing and consumption semantics (ADR-0018 §3).
type EconomicRegime string

const (
	RegimeLocalCompute         EconomicRegime = "local_compute"
	RegimeSubscriptionQuota    EconomicRegime = "subscription_quota"
	RegimeMeteredAPI           EconomicRegime = "metered_api"
	RegimePrepaidCredits       EconomicRegime = "prepaid_credits"
	RegimeEnterpriseAllocation EconomicRegime = "enterprise_allocation"
	RegimeUnknownCustom        EconomicRegime = "unknown_custom"
)

// Valid reports whether the economic regime is defined by the schema.
func (r EconomicRegime) Valid() bool {
	switch r {
	case RegimeLocalCompute, RegimeSubscriptionQuota, RegimeMeteredAPI,
		RegimePrepaidCredits, RegimeEnterpriseAllocation, RegimeUnknownCustom:
		return true
	}
	return false
}

// BudgetUnit is the dimensional unit of metering.
type BudgetUnit string

const (
	UnitUSDCents     BudgetUnit = "usd_cents"
	UnitTokenCredits BudgetUnit = "token_credits"
	UnitTokens       BudgetUnit = "tokens"
	UnitRequests     BudgetUnit = "requests"
	UnitSeconds      BudgetUnit = "seconds"
)

// Valid reports whether the budget unit is defined by the schema.
func (u BudgetUnit) Valid() bool {
	switch u {
	case UnitUSDCents, UnitTokenCredits, UnitTokens, UnitRequests, UnitSeconds:
		return true
	}
	return false
}

// BudgetPeriod is the window over which a budget applies.
type BudgetPeriod string

const (
	PeriodRollingHour  BudgetPeriod = "rolling_hour"
	PeriodRollingDay   BudgetPeriod = "rolling_day"
	PeriodBillingCycle BudgetPeriod = "billing_cycle"
	PeriodPerAttempt   BudgetPeriod = "per_attempt"
	PeriodPerTask      BudgetPeriod = "per_task"
)

// Valid reports whether the budget period is defined by the schema.
func (p BudgetPeriod) Valid() bool {
	switch p {
	case PeriodRollingHour, PeriodRollingDay, PeriodBillingCycle, PeriodPerAttempt, PeriodPerTask:
		return true
	}
	return false
}

// BudgetPool tracks resource allocations without conflating them with model identity (ADR-0018 §3).
type BudgetPool struct {
	SchemaVersion            SchemaVersion  `json:"schema_version"`
	PoolID                   string         `json:"pool_id"`
	Name                     string         `json:"name"`
	Regime                   EconomicRegime `json:"regime"`
	Unit                     BudgetUnit     `json:"unit"`
	HardLimit                int64          `json:"hard_limit"`
	SoftAlertLimit           int64          `json:"soft_alert_limit"`
	Period                   BudgetPeriod   `json:"period"`
	AllowOverage             bool           `json:"allow_overage"`
	FallbackAllowedToMetered bool           `json:"fallback_allowed_to_metered"`
}

// RecordKind implements Record.
func (b *BudgetPool) RecordKind() string { return "BudgetPool" }

// RecordID implements Record.
func (b *BudgetPool) RecordID() string { return b.PoolID }

// SchemaVer implements Record.
func (b *BudgetPool) SchemaVer() SchemaVersion { return b.SchemaVersion }

// Validate enforces BudgetPool constraints, including the mandatory no-silent-fallback rule.
func (b *BudgetPool) Validate() error {
	const kind = "BudgetPool"
	if err := b.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "pool_id", b.PoolID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "name", b.Name); err != nil {
		return err
	}
	if !b.Regime.Valid() {
		return enumError(kind, "regime", string(b.Regime),
			string(RegimeLocalCompute), string(RegimeSubscriptionQuota), string(RegimeMeteredAPI),
			string(RegimePrepaidCredits), string(RegimeEnterpriseAllocation), string(RegimeUnknownCustom))
	}
	if !b.Unit.Valid() {
		return enumError(kind, "unit", string(b.Unit),
			string(UnitUSDCents), string(UnitTokenCredits), string(UnitTokens), string(UnitRequests), string(UnitSeconds))
	}
	if !b.Period.Valid() {
		return enumError(kind, "period", string(b.Period),
			string(PeriodRollingHour), string(PeriodRollingDay), string(PeriodBillingCycle), string(PeriodPerAttempt), string(PeriodPerTask))
	}
	if b.HardLimit < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: hard_limit cannot be negative, got %d", kind, b.HardLimit)
	}
	if b.SoftAlertLimit < 0 || b.SoftAlertLimit > b.HardLimit {
		return errs.New(errs.CategoryInvalidArgument, "%s: soft_alert_limit (%d) must be between 0 and hard_limit (%d)", kind, b.SoftAlertLimit, b.HardLimit)
	}
	// ADR-0018 §9 & DCI-104: Loss of local/subscription/prepaid/enterprise quota never
	// authorizes fallback to metered billing.
	if b.FallbackAllowedToMetered && (b.Regime == RegimeLocalCompute || b.Regime == RegimeSubscriptionQuota ||
		b.Regime == RegimePrepaidCredits || b.Regime == RegimeEnterpriseAllocation) {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: fallback_allowed_to_metered is forbidden for regime %q (no silent paid fallback per ADR-0018 §9, DCI-104)",
			kind, string(b.Regime))
	}
	return nil
}

// BudgetPoolStatus expresses current pool capacity.
type BudgetPoolStatus string

const (
	BudgetStatusHealthy           BudgetPoolStatus = "healthy"
	BudgetStatusSoftLimitExceeded BudgetPoolStatus = "soft_limit_exceeded"
	BudgetStatusExhausted         BudgetPoolStatus = "exhausted"
)

// Valid reports whether the budget status is known.
func (s BudgetPoolStatus) Valid() bool {
	switch s {
	case BudgetStatusHealthy, BudgetStatusSoftLimitExceeded, BudgetStatusExhausted:
		return true
	}
	return false
}

// BudgetState captures live pool balance and status.
type BudgetState struct {
	PoolID           string           `json:"pool_id"`
	CurrentUsage     int64            `json:"current_usage"`
	RemainingBalance int64            `json:"remaining_balance"`
	PeriodStart      string           `json:"period_start"`
	PeriodEnd        string           `json:"period_end"`
	Status           BudgetPoolStatus `json:"status"`
}

// Validate checks BudgetState fields.
func (b BudgetState) Validate() error {
	const kind = "BudgetState"
	if err := requireNonEmpty(kind, "pool_id", b.PoolID); err != nil {
		return err
	}
	if b.CurrentUsage < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: current_usage cannot be negative", kind)
	}
	if b.RemainingBalance < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: remaining_balance cannot be negative", kind)
	}
	if !b.Status.Valid() {
		return enumError(kind, "status", string(b.Status),
			string(BudgetStatusHealthy), string(BudgetStatusSoftLimitExceeded), string(BudgetStatusExhausted))
	}
	if err := requireNonEmpty(kind, "period_start", b.PeriodStart); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "period_end", b.PeriodEnd); err != nil {
		return err
	}
	return nil
}

// ResourceState captures machine-level compute availability.
type ResourceState struct {
	HostID                  string `json:"host_id"`
	Timestamp               string `json:"timestamp"`
	AvailableGPUMemoryBytes int64  `json:"available_gpu_memory_bytes"`
	AvailableRAMBytes       int64  `json:"available_ram_bytes"`
	MaxConcurrentSlots      int    `json:"max_concurrent_slots"`
	ActiveSlots             int    `json:"active_slots"`
}

// Validate checks ResourceState fields.
func (r ResourceState) Validate() error {
	const kind = "ResourceState"
	if err := requireNonEmpty(kind, "host_id", r.HostID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "timestamp", r.Timestamp); err != nil {
		return err
	}
	if r.AvailableGPUMemoryBytes < 0 || r.AvailableRAMBytes < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: memory bytes cannot be negative", kind)
	}
	if r.MaxConcurrentSlots < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_concurrent_slots must be >= 1", kind)
	}
	if r.ActiveSlots < 0 || r.ActiveSlots > r.MaxConcurrentSlots {
		return errs.New(errs.CategoryInvalidArgument, "%s: active_slots must be between 0 and max_concurrent_slots", kind)
	}
	return nil
}

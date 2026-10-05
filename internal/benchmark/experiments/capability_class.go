package experiments

// CapabilityClass identifies the cognition capability tier of an endpoint or model (REQ-01).
type CapabilityClass string

const (
	// CapabilityLocalSmall represents local smaller models (e.g. 7B-14B parameter range).
	CapabilityLocalSmall CapabilityClass = "local_small"
	// CapabilitySubscriptionCLI represents CLI-based interactive subscription tools.
	CapabilitySubscriptionCLI CapabilityClass = "subscription_cli"
	// CapabilityFrontierAPI represents frontier cloud APIs (e.g. flagship models).
	CapabilityFrontierAPI CapabilityClass = "frontier_api"
)

// Valid reports whether the capability class is known (REQ-01).
func (c CapabilityClass) Valid() bool {
	switch c {
	case CapabilityLocalSmall, CapabilitySubscriptionCLI, CapabilityFrontierAPI:
		return true
	default:
		return false
	}
}

// ContractCompletenessLevel identifies the completeness level of an EWP specification (REQ-02).
type ContractCompletenessLevel string

const (
	// ContractBaseline represents an open-scope, underspecified baseline contract.
	ContractBaseline ContractCompletenessLevel = "baseline_incomplete"
	// ContractImplementationReady represents a fully specified, zero-ambiguity contract.
	ContractImplementationReady ContractCompletenessLevel = "implementation_ready"
)

// Valid reports whether the contract completeness level is known (REQ-02).
func (c ContractCompletenessLevel) Valid() bool {
	switch c {
	case ContractBaseline, ContractImplementationReady:
		return true
	default:
		return false
	}
}

// Package facade is the host-neutral application facade of the semantic
// Principal interface (WP-M5-2).
//
// It owns every semantic decision: the MCP adapter only decodes strictly,
// binds the trusted caller, dispatches here and normalises the response (I1).
// The facade holds no authority of its own: the caller comes from a local
// protected launch binding, every operation is checked against a policy
// resolver, absent runtime ports deny without side effects, and acceptance is
// hard-disabled in this slice (I8, I9).
package facade

// Base tool names. They are also the canonical grant names: a grant is the
// exact tool name, never an alias.
const (
	ToolProjectState      = "project_state"
	ToolInvestigate       = "investigate"
	ToolCreateWorkPackage = "create_work_package"
	ToolDelegate          = "delegate"
	ToolTaskStatus        = "task_status"
	ToolValidate          = "validate"
	ToolReview            = "review"
	ToolRequestEvidence   = "request_evidence"
	ToolAccept            = "accept"
	ToolReject            = "reject"
	ToolRecordDecision    = "record_decision"
)

// ToolNames returns the eleven base tools in specification order.
func ToolNames() []string {
	return []string{
		ToolProjectState, ToolInvestigate, ToolCreateWorkPackage, ToolDelegate, ToolTaskStatus,
		ToolValidate, ToolReview, ToolRequestEvidence, ToolAccept, ToolReject, ToolRecordDecision,
	}
}

// DiscoveryToolNames returns the discovery tool names WP-M3/WP3 will add. They
// are valid grants for a binding but this facade registers none of them.
func DiscoveryToolNames() []string {
	return []string{
		"initialize_project", "discovery_state", "record_problem_model", "record_ambiguities",
		"record_product_decision", "record_requirements", "record_discovery_experiment",
		"review_specification", "record_specification_readiness",
	}
}

// KnownAction reports whether name is a canonical base or discovery grant.
func KnownAction(name string) bool {
	for _, set := range [][]string{ToolNames(), DiscoveryToolNames()} {
		for _, n := range set {
			if n == name {
				return true
			}
		}
	}
	return false
}

// Evidence kinds of request_evidence.
const (
	EvidenceSummary = "summary"
	EvidenceSearch  = "search"
	EvidenceSymbol  = "symbol"
	EvidenceSnippet = "snippet"
	EvidenceDiff    = "diff"
)

// Source depths a binding may grant, in increasing order.
const (
	DepthSummary = "summary"
	DepthSymbol  = "symbol"
	DepthSnippet = "snippet"
)

// Hard caps of the evidence and snippet interface.
const (
	MaxEvidenceBytes   = 8192
	MaxSnippetLines    = 200
	MaxQuestionBytes   = 2048
	MaxQueryBytes      = 256
	MaxReasonBytes     = 2048
	MaxHitReasonBytes  = 1024
	MaxScopePaths      = 16
	MaxMatches         = 20
	MaxRepairs         = 32
	MaxRequestBytes    = 1 << 20
	MaxResponseBytes   = 32 << 10
	MaxTaskAttemptList = 20
)

// Focus values of project_state.
const (
	FocusProject   = "project"
	FocusTask      = "task"
	FocusDiscovery = "discovery"
)

// Review dimensions.
func reviewDimensions() []string {
	return []string{"correctness", "contract", "architecture", "security", "test_adequacy",
		"maintainability", "performance", "specification"}
}

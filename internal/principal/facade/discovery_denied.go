package facade

import (
	"context"

	"github.com/olostan/DevCadence/internal/principal"
)

// DiscoveryWriteUnavailableRef is the evidence handle of every refused
// discovery write.
const DiscoveryWriteUnavailableRef = "discovery-write-unavailable"

// DiscoveryWrite is the fail-closed entry for the eight discovery write tools
// of WP-M5-3 (discovery_state is the read, served by project_state focus
// discovery). Nothing is registered with MCP; this method exists so the denial
// is one tested, deterministic path rather than an absence.
//
// After the project, exact-grant and policy checks it always refuses with zero
// journal or storage effect: confirming human intent, recording product
// decisions and persisting model-authored requirements need the protected
// operator ingress and human-receipt verifier (M5-R4), which do not exist, and
// model output, arguments or grants can never stand in for them.
func (s *Service) DiscoveryWrite(ctx context.Context, caller CallerContext, meta CallMeta, tool string) Envelope {
	if !isDiscoveryWriteTool(tool) {
		return s.refuse(tool, meta, "", invalid("%q is not a discovery write tool", tool))
	}
	if err := s.admit(ctx, caller, meta, tool); err != nil {
		return s.refuse(tool, meta, "", err)
	}
	code := principal.CodeNeedsPrincipal
	switch tool {
	case "record_product_decision", "record_requirements":
		code = principal.CodeNeedsHuman // product authority belongs to the human
	case "review_specification":
		code = principal.CodeModelUnavailable // no independent review runtime
	}
	return s.refuse(tool, meta, "", coded(code, false, []string{DiscoveryWriteUnavailableRef}, "discovery writes are disabled"))
}

func isDiscoveryWriteTool(tool string) bool {
	for _, n := range DiscoveryToolNames() {
		if n == tool && n != "discovery_state" {
			return true
		}
	}
	return false
}

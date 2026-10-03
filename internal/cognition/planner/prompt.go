package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

const (
	promptFirstLine = "devcadence-planner-prompt/1"
	promptAdvisory  = "Your output is advisory. Deterministic validation decides whether any alternative is usable."
	promptIDsRule   = "IDs must come from the lists above"
)

// plannerExampleAlternative is a minimal valid alternative showing every
// required field with its exact JSON key. Identity, revision, timestamps and
// portfolio schema_version are assigned by Go and are therefore not requested.
const plannerExampleAlternative = `{"intent":"balanced","portfolio":{"max_source_exposure":"focused_snippets","channels":[{"schema_version":"1.0","channel_id":"chan-example","endpoint_id":"ep-example","kind":"local_daemon_socket","session_mode":"stateless_per_call","context_control":"exact_stateless","prefix_cache":"none","supports_streaming":true,"supports_tools":false,"native_worktree_access":false,"max_concurrent_requests":1},{"schema_version":"1.0","channel_id":"chan-fallback","endpoint_id":"ep-fallback","kind":"cli_subprocess","session_mode":"resumable_session","context_control":"append_only","prefix_cache":"implicit","supports_streaming":true,"supports_tools":true,"native_worktree_access":true,"max_concurrent_requests":1}],"role_bindings":[{"role":"implementer","endpoint_id":"ep-example","channel_id":"chan-example","budget_pool_id":"pool-example","context_profile_id":"profile-example","priority":1,"fallbacks":[{"endpoint_id":"ep-fallback","channel_id":"chan-fallback","budget_pool_id":"pool-example","context_profile_id":"profile-fallback"}]}],"budget_pools":[{"schema_version":"1.0","pool_id":"pool-example","name":"Example pool","regime":"local_compute","unit":"seconds","hard_limit":3600,"soft_alert_limit":3000,"period":"rolling_day","allow_overage":false,"fallback_allowed_to_metered":false}]},"rationale":"Why this portfolio fits the stated intent.","tradeoffs":["What this alternative gives up."],"confidence":"medium"}`

// BuildPrompt builds the deterministic planning prompt and its digest. It
// validates Inventory and Intents exactly as Plan does and needs neither Clock
// nor Invoker. The prompt carries identifiers and facts only: never credential
// inventory, credential references, live budget/resource values or the raw
// machine profile (DCI-124).
func BuildPrompt(req Request) (string, string, error) {
	if req.Inventory == nil {
		return "", "", errs.New(errs.CategoryInvalidArgument, "planner: inventory is required")
	}
	intents, err := canonicalizeIntents(req.Intents)
	if err != nil {
		return "", "", err
	}
	inv := req.Inventory

	endpoints := make([]protocol.CognitionEndpointSummary, len(inv.CognitionEndpoints))
	copy(endpoints, inv.CognitionEndpoints)
	for i := range endpoints {
		endpoints[i].CredentialRef = ""
	}

	policy := cognition.DefaultValidationPolicy()
	if req.Policy != nil {
		policy = *req.Policy
	}
	if len(policy.RoleRequirements) == 0 {
		policy.RoleRequirements = cognition.DefaultRequirements()
	}

	profileIDs := make([]string, 0, len(req.ContextProfiles))
	for id := range req.ContextProfiles {
		profileIDs = append(profileIDs, id)
	}
	sort.Strings(profileIDs)
	profiles := make([]map[string]any, 0, len(profileIDs))
	for _, id := range profileIDs {
		p := req.ContextProfiles[id]
		if p == nil {
			profiles = append(profiles, map[string]any{"profile_id": id})
			continue
		}
		profiles = append(profiles, map[string]any{
			"profile_id":               id,
			"endpoint_id":              p.EndpointID,
			"channel_id":               p.ChannelID,
			"observed_context_control": p.ObservedContextControl,
		})
	}
	poolIDs := make([]string, 0, len(req.BudgetStates))
	for id := range req.BudgetStates {
		poolIDs = append(poolIDs, id)
	}
	sort.Strings(poolIDs)

	project := map[string]any{
		"languages": normalizeStrings(req.Project.Languages),
		"risk_tags": normalizeStrings(req.Project.RiskTags),
	}

	var b strings.Builder
	line := func(label string, v any) error {
		raw, err := protocol.CanonicalJSON(v)
		if err != nil {
			return err
		}
		b.WriteString(label)
		b.WriteString(": ")
		b.Write(raw)
		b.WriteString("\n")
		return nil
	}

	b.WriteString(promptFirstLine + "\n")
	b.WriteString("You are the portfolio planner. Propose alternative cognition portfolios, one per requested intent, as one JSON object.\n\n")
	steps := []struct {
		label string
		value any
	}{
		{"requested intents", intents},
		{"cognition endpoints", endpoints},
		{"hardware", inv.Hardware},
		{"profile", inv.Profile},
		{"readiness", inv.Readiness},
		{"policy summary", inv.Policy},
		{"valid context_profile_id values", profiles},
		{"budget pool ids that have live state", poolIDs},
		{"effective validation policy", policy},
		{"project characteristics", project},
	}
	for _, s := range steps {
		if err := line(s.label, s.value); err != nil {
			return "", "", errs.Wrap(errs.CategoryInternal, err, "planner: serialize prompt section %q", s.label)
		}
	}

	b.WriteString("\nOutput contract:\n")
	b.WriteString("- Return exactly one JSON object and nothing else: {\"alternatives\":[{\"intent\":...,\"portfolio\":...,\"rationale\":...,\"tradeoffs\":[...],\"confidence\":...}]}.\n")
	b.WriteString("- Between 1 and the number of requested intents alternatives; each intent must be one of the requested intents and appear at most once.\n")
	b.WriteString("- confidence is one of \"high\", \"medium\", \"low\". rationale is a non-empty string. tradeoffs is a non-empty array of non-empty strings.\n")
	b.WriteString("- Unknown JSON keys are rejected. The output may be wrapped in a single ```json fenced block, with no other text.\n")
	b.WriteString("- Do not supply portfolio_id, revision, created_at or portfolio schema_version; they are assigned by the system and any value is ignored.\n")
	b.WriteString("- Every channel and budget pool needs \"schema_version\":\"1.0\".\n")
	b.WriteString("Minimal valid example alternative:\n")
	b.WriteString(plannerExampleAlternative + "\n")
	b.WriteString(promptIDsRule + ". Do not invent endpoint, channel, context profile or budget pool identifiers.\n\n")
	b.WriteString(promptAdvisory + "\n")

	prompt := b.String()
	sum := sha256.Sum256([]byte(prompt))
	return prompt, "sha256:" + hex.EncodeToString(sum[:]), nil
}

// normalizeStrings returns a sorted, de-duplicated, non-nil copy.
func normalizeStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// PromptProjection is the endpoint-specific serialized representation of a compiled ContextPack.
// Per PROTOCOLS §10B and ADR-0020 §5:
// Canonical ContextPack semantics are renderer-neutral. A PromptProjection is an ephemeral
// endpoint-specific serialization of one validated pack.
type PromptProjection struct {
	Format       string `json:"format"` // "tagged_markdown" or "json"
	SystemPrompt string `json:"system_prompt"`
	UserPrompt   string `json:"user_prompt"`
	Digest       string `json:"digest"`
}

// PromptRenderer converts a canonical protocol.ContextPack into an endpoint-specific PromptProjection.
type PromptRenderer interface {
	Format() string
	Render(pack *protocol.ContextPack) (PromptProjection, error)
}

// TaggedMarkdownRenderer renders a ContextPack into structured, human-readable tagged sections
// with strict delimiter escaping to prevent evidence contents from breaking into instruction space (DCI-133, ADR-0020 §5).
type TaggedMarkdownRenderer struct{}

// NewTaggedMarkdownRenderer creates a new TaggedMarkdownRenderer.
func NewTaggedMarkdownRenderer() *TaggedMarkdownRenderer {
	return &TaggedMarkdownRenderer{}
}

// Format returns the format identifier.
func (r *TaggedMarkdownRenderer) Format() string {
	return "tagged_markdown"
}

// Render renders the pack using tagged XML-style markdown blocks with delimiter safety.
func (r *TaggedMarkdownRenderer) Render(pack *protocol.ContextPack) (PromptProjection, error) {
	const kind = "TaggedMarkdownRenderer"
	if pack == nil {
		return PromptProjection{}, errs.New(errs.CategoryInvalidArgument, "%s: pack cannot be nil", kind)
	}
	if pack.Status != protocol.PackStatusReady {
		return PromptProjection{}, errs.New(errs.CategoryValidationFailed, "%s: cannot render pack with non-ready status %q", kind, pack.Status)
	}
	if err := pack.Validate(); err != nil {
		return PromptProjection{}, errs.Wrap(errs.CategoryValidationFailed, err, "%s: pack failed validation", kind)
	}

	// 1. System Prompt encapsulates Role Core and high-level behavioral boundaries
	var sysBuilder strings.Builder
	sysBuilder.WriteString("<role_core>\n")
	sysBuilder.WriteString(strings.TrimSpace(pack.RoleCore))
	sysBuilder.WriteString("\n</role_core>\n")

	// 2. User Prompt encapsulates Execution Contract, Mandatory Obligations, Cognitive State, Evidence, and Tail
	var userBuilder strings.Builder

	// Execution Contract (Authoritative & Bounded)
	userBuilder.WriteString("<execution_contract>\n")
	userBuilder.WriteString(strings.TrimSpace(pack.ExecutionContract))
	userBuilder.WriteString("\n</execution_contract>\n\n")

	// Mandatory Obligations (Operative, revision-pinned clauses)
	userBuilder.WriteString("<mandatory_obligations>\n")
	for _, clause := range pack.NormativeClauses {
		userBuilder.WriteString(fmt.Sprintf("- %s\n", strings.TrimSpace(clause)))
	}
	userBuilder.WriteString("</mandatory_obligations>\n\n")

	// Cognitive State Capsule (Derived hypotheses, active TODOs, intermediate decisions)
	userBuilder.WriteString("<cognitive_state>\n")
	if len(pack.CognitiveState.Hypotheses) > 0 {
		userBuilder.WriteString("  <hypotheses>\n")
		for _, h := range pack.CognitiveState.Hypotheses {
			userBuilder.WriteString(fmt.Sprintf("    - %s\n", html.EscapeString(h)))
		}
		userBuilder.WriteString("  </hypotheses>\n")
	}
	if len(pack.CognitiveState.ActiveTODOs) > 0 {
		userBuilder.WriteString("  <active_todos>\n")
		for _, t := range pack.CognitiveState.ActiveTODOs {
			userBuilder.WriteString(fmt.Sprintf("    - %s\n", html.EscapeString(t)))
		}
		userBuilder.WriteString("  </active_todos>\n")
	}
	if len(pack.CognitiveState.IntermediateDecisions) > 0 {
		userBuilder.WriteString("  <intermediate_decisions>\n")
		for _, d := range pack.CognitiveState.IntermediateDecisions {
			userBuilder.WriteString(fmt.Sprintf("    - %s\n", html.EscapeString(d)))
		}
		userBuilder.WriteString("  </intermediate_decisions>\n")
	}
	if len(pack.CognitiveState.OpenQuestions) > 0 {
		userBuilder.WriteString("  <open_questions>\n")
		for _, q := range pack.CognitiveState.OpenQuestions {
			userBuilder.WriteString(fmt.Sprintf("    - %s\n", html.EscapeString(q)))
		}
		userBuilder.WriteString("  </open_questions>\n")
	}
	userBuilder.WriteString("</cognitive_state>\n\n")

	// Evidence Working Set (Content-addressed leases with delimiter escaping)
	userBuilder.WriteString("<evidence_working_set>\n")
	for _, lease := range pack.EvidenceWorkingSet {
		// Delimiter safety: Escape XML special characters in evidence content to prevent closing tag injection
		safeContent := EscapeEvidenceDelimiters(lease.Content)
		userBuilder.WriteString(fmt.Sprintf("  <evidence_lease id=%q kind=%q file=%q locator=%q digest=%q>\n",
			html.EscapeString(lease.LeaseID),
			html.EscapeString(string(lease.EvidenceKind)),
			html.EscapeString(lease.FilePath),
			html.EscapeString(lease.Locator),
			html.EscapeString(lease.ContentDigest)))
		userBuilder.WriteString(safeContent)
		userBuilder.WriteString("\n  </evidence_lease>\n")
	}
	userBuilder.WriteString("</evidence_working_set>\n\n")

	// Ephemeral Tail Block (Recent tool exchanges and current action)
	userBuilder.WriteString("<ephemeral_tail>\n")
	if len(pack.EphemeralTail.RecentToolExchanges) > 0 {
		userBuilder.WriteString("  <recent_tool_exchanges>\n")
		for _, ex := range pack.EphemeralTail.RecentToolExchanges {
			userBuilder.WriteString(fmt.Sprintf("    - %s\n", EscapeEvidenceDelimiters(ex)))
		}
		userBuilder.WriteString("  </recent_tool_exchanges>\n")
	}
	if pack.EphemeralTail.CandidateDiffManifest != nil {
		userBuilder.WriteString("  <candidate_diff_manifest>\n")
		userBuilder.WriteString(EscapeEvidenceDelimiters(*pack.EphemeralTail.CandidateDiffManifest))
		userBuilder.WriteString("\n  </candidate_diff_manifest>\n")
	}
	if len(pack.EphemeralTail.ValidationSummaries) > 0 {
		userBuilder.WriteString("  <validation_summaries>\n")
		for _, vs := range pack.EphemeralTail.ValidationSummaries {
			userBuilder.WriteString(fmt.Sprintf("    - %s\n", EscapeEvidenceDelimiters(vs)))
		}
		userBuilder.WriteString("  </validation_summaries>\n")
	}
	userBuilder.WriteString(fmt.Sprintf("  <current_action>%s</current_action>\n",
		html.EscapeString(pack.EphemeralTail.CurrentAction)))
	userBuilder.WriteString("</ephemeral_tail>\n")

	sysPrompt := sysBuilder.String()
	userPrompt := userBuilder.String()

	// Calculate SHA-256 digest of projection
	hasher := sha256.New()
	hasher.Write([]byte(sysPrompt))
	hasher.Write([]byte(userPrompt))
	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))

	return PromptProjection{
		Format:       r.Format(),
		SystemPrompt: sysPrompt,
		UserPrompt:   userPrompt,
		Digest:       digest,
	}, nil
}

// JSONRenderer renders a ContextPack as a structured JSON object.
type JSONRenderer struct{}

// NewJSONRenderer creates a new JSONRenderer.
func NewJSONRenderer() *JSONRenderer {
	return &JSONRenderer{}
}

// Format returns the format identifier.
func (r *JSONRenderer) Format() string {
	return "json"
}

// Render serializes the pack to JSON. JSON string escaping guarantees delimiter safety.
func (r *JSONRenderer) Render(pack *protocol.ContextPack) (PromptProjection, error) {
	const kind = "JSONRenderer"
	if pack == nil {
		return PromptProjection{}, errs.New(errs.CategoryInvalidArgument, "%s: pack cannot be nil", kind)
	}
	if pack.Status != protocol.PackStatusReady {
		return PromptProjection{}, errs.New(errs.CategoryValidationFailed, "%s: cannot render pack with non-ready status %q", kind, pack.Status)
	}
	if err := pack.Validate(); err != nil {
		return PromptProjection{}, errs.Wrap(errs.CategoryValidationFailed, err, "%s: pack failed validation", kind)
	}

	packCopy := *pack
	packCopy.InvocationDigest = ""

	payload, err := json.MarshalIndent(&packCopy, "", "  ")
	if err != nil {
		return PromptProjection{}, errs.Wrap(errs.CategoryInternal, err, "%s: failed to marshal pack to json", kind)
	}

	userPrompt := string(payload)
	sysPrompt := fmt.Sprintf("Execute work package within context bounds. Role: %s", pack.RoleCore)

	hasher := sha256.New()
	hasher.Write([]byte(sysPrompt))
	hasher.Write([]byte(userPrompt))
	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))

	return PromptProjection{
		Format:       r.Format(),
		SystemPrompt: sysPrompt,
		UserPrompt:   userPrompt,
		Digest:       digest,
	}, nil
}

// EscapeEvidenceDelimiters ensures that evidence content cannot break out of its XML container.
// Specifically:
// 1. It replaces literal '</' with '&lt;/' so closing tags cannot terminate container tags.
// 2. It replaces '<execution_contract>' and other instruction injection tags.
// 3. It escapes all XML angle brackets to html entities '&lt;' and '&gt;' if they attempt to simulate tags.
func EscapeEvidenceDelimiters(content string) string {
	// First, replace any closing tags matching XML container tags
	s := content
	// Replace all occurrences of '</' with '&lt;/'
	s = strings.ReplaceAll(s, "</", "&lt;/")
	// Replace all occurrences of opening container tags emitted by TaggedMarkdownRenderer
	s = strings.ReplaceAll(s, "<execution_contract>", "&lt;execution_contract&gt;")
	s = strings.ReplaceAll(s, "<execution_contract ", "&lt;execution_contract ")
	s = strings.ReplaceAll(s, "<mandatory_obligations>", "&lt;mandatory_obligations&gt;")
	s = strings.ReplaceAll(s, "<mandatory_obligations ", "&lt;mandatory_obligations ")
	s = strings.ReplaceAll(s, "<role_core>", "&lt;role_core&gt;")
	s = strings.ReplaceAll(s, "<role_core ", "&lt;role_core ")
	s = strings.ReplaceAll(s, "<cognitive_state>", "&lt;cognitive_state&gt;")
	s = strings.ReplaceAll(s, "<cognitive_state ", "&lt;cognitive_state ")
	s = strings.ReplaceAll(s, "<hypotheses>", "&lt;hypotheses&gt;")
	s = strings.ReplaceAll(s, "<hypotheses ", "&lt;hypotheses ")
	s = strings.ReplaceAll(s, "<active_todos>", "&lt;active_todos&gt;")
	s = strings.ReplaceAll(s, "<active_todos ", "&lt;active_todos ")
	s = strings.ReplaceAll(s, "<intermediate_decisions>", "&lt;intermediate_decisions&gt;")
	s = strings.ReplaceAll(s, "<intermediate_decisions ", "&lt;intermediate_decisions ")
	s = strings.ReplaceAll(s, "<open_questions>", "&lt;open_questions&gt;")
	s = strings.ReplaceAll(s, "<open_questions ", "&lt;open_questions ")
	s = strings.ReplaceAll(s, "<evidence_working_set>", "&lt;evidence_working_set&gt;")
	s = strings.ReplaceAll(s, "<evidence_working_set ", "&lt;evidence_working_set ")
	s = strings.ReplaceAll(s, "<evidence_lease", "&lt;evidence_lease")
	s = strings.ReplaceAll(s, "<ephemeral_tail>", "&lt;ephemeral_tail&gt;")
	s = strings.ReplaceAll(s, "<ephemeral_tail ", "&lt;ephemeral_tail ")
	s = strings.ReplaceAll(s, "<recent_tool_exchanges>", "&lt;recent_tool_exchanges&gt;")
	s = strings.ReplaceAll(s, "<recent_tool_exchanges ", "&lt;recent_tool_exchanges ")
	s = strings.ReplaceAll(s, "<candidate_diff_manifest>", "&lt;candidate_diff_manifest&gt;")
	s = strings.ReplaceAll(s, "<candidate_diff_manifest ", "&lt;candidate_diff_manifest ")
	s = strings.ReplaceAll(s, "<validation_summaries>", "&lt;validation_summaries&gt;")
	s = strings.ReplaceAll(s, "<validation_summaries ", "&lt;validation_summaries ")
	s = strings.ReplaceAll(s, "<current_action>", "&lt;current_action&gt;")
	s = strings.ReplaceAll(s, "<current_action ", "&lt;current_action ")
	return s
}

package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// contextPackDigestInput defines the canonical typed semantic state for ContextPackDigest.
type contextPackDigestInput struct {
	ManifestID            string                         `json:"manifest_id"`
	ManifestRevision      int                            `json:"manifest_revision"`
	RoleCore              string                         `json:"role_core"`
	ExecutionContract     string                         `json:"execution_contract"`
	NormativeClauses      []string                       `json:"normative_clauses"`
	CognitiveState        protocol.CognitiveStateCapsule `json:"cognitive_state"`
	EvidenceWorkingSet    []protocol.EvidenceLease       `json:"evidence_working_set"`
	EphemeralTail         protocol.EphemeralTailBlock    `json:"ephemeral_tail"`
	AdmittedObjectDigests map[string]string              `json:"admitted_object_digests"`
}

// ComputeContextPackDigest computes the deterministic cryptographic hash of the typed semantic pack state.
func ComputeContextPackDigest(pack *protocol.ContextPack) (string, error) {
	input := contextPackDigestInput{
		ManifestID:            pack.ManifestID,
		ManifestRevision:      pack.ManifestRevision,
		RoleCore:              pack.RoleCore,
		ExecutionContract:     pack.ExecutionContract,
		NormativeClauses:      pack.NormativeClauses,
		CognitiveState:        pack.CognitiveState,
		EvidenceWorkingSet:    pack.EvidenceWorkingSet,
		EphemeralTail:         pack.EphemeralTail,
		AdmittedObjectDigests: pack.AdmittedObjectDigests,
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err, "failed to marshal context pack digest input")
	}
	hasher := sha256.New()
	hasher.Write(data)
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// invocationDigestInput defines the complete final endpoint projection state for InvocationDigest.
type invocationDigestInput struct {
	PackDigest      string   `json:"pack_digest"`
	RendererFormat  string   `json:"renderer_format"`
	SystemPrompt    string   `json:"system_prompt"`
	UserPrompt      string   `json:"user_prompt"`
	ToolSchemas     []string `json:"tool_schemas"`
	HostFraming     string   `json:"host_framing"`
	CatalogID       string   `json:"catalog_id"`
	CatalogRevision string   `json:"catalog_revision"`
	MappingRevision string   `json:"mapping_revision"`
	CatalogDigest   string   `json:"catalog_digest"`
	ProfileID       string   `json:"profile_id"`
	ProfileRevision int      `json:"profile_revision"`
	DeclaredWindow  int      `json:"declared_window_tokens"`
	RuntimeWindow   int      `json:"runtime_window_tokens"`
}

// ComputeInvocationDigest computes the deterministic cryptographic hash of the complete final endpoint projection.
func ComputeInvocationDigest(
	packDigest string,
	rendererFormat string,
	systemPrompt string,
	userPrompt string,
	toolSchemas []string,
	hostFraming string,
	catalogID string,
	catalogRevision string,
	mappingRevision string,
	catalogDigest string,
	profile *protocol.ContextProfile,
) (string, error) {
	sortedSchemas := make([]string, len(toolSchemas))
	copy(sortedSchemas, toolSchemas)
	sort.Strings(sortedSchemas)

	var profID string
	var profRev, declWin, runWin int
	if profile != nil {
		profID = profile.ProfileID
		profRev = profile.Revision
		declWin = profile.DeclaredWindowTokens
		runWin = profile.RuntimeWindowTokens
	}

	input := invocationDigestInput{
		PackDigest:      packDigest,
		RendererFormat:  rendererFormat,
		SystemPrompt:    systemPrompt,
		UserPrompt:      userPrompt,
		ToolSchemas:     sortedSchemas,
		HostFraming:     hostFraming,
		CatalogID:       catalogID,
		CatalogRevision: catalogRevision,
		MappingRevision: mappingRevision,
		CatalogDigest:   catalogDigest,
		ProfileID:       profID,
		ProfileRevision: profRev,
		DeclaredWindow:  declWin,
		RuntimeWindow:   runWin,
	}

	data, err := json.Marshal(input)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err, "failed to marshal invocation digest input")
	}
	hasher := sha256.New()
	hasher.Write(data)
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

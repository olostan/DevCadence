package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ollamaDefaultRegistryHost is Ollama's own implicit default registry — a
// model reference is only prefixed with AllowedSource when it names
// something other than this, since `ollama pull <tag>` already resolves
// against this host with no prefix needed.
const ollamaDefaultRegistryHost = "registry.ollama.ai"

// OllamaAdapter implements LocalModelRuntimeAdapter for the Ollama local
// runtime. It has no special status relative to other adapters (e.g.
// MLXAdapter): ModelRuntimeRegistry dispatches to it purely by matching
// Runtime() == "ollama", the same mechanism used for every other runtime.
type OllamaAdapter struct{}

// Runtime implements LocalModelRuntimeAdapter.
func (OllamaAdapter) Runtime() string { return "ollama" }

// ollamaModelEntry mirrors just the fields this file needs from one entry of
// Ollama's GET /api/tags response.
type ollamaModelEntry struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// ollamaTagsResponse mirrors just the fields this file needs from Ollama's
// GET /api/tags response — a minimal, independent parse rather than a
// dependency on internal/cognition/ollama's richer adapter, which is built
// around discovery/probing, not a single tag/digest existence check.
type ollamaTagsResponse struct {
	Models []ollamaModelEntry `json:"models"`
}

// fetchOllamaTags queries the local Ollama API's model list. Shared by
// ModelPresent (a live Condition check) and EnsureModel (post-pull
// supply-chain verification), so the two can never silently disagree about
// what "present" means.
func fetchOllamaTags(ctx context.Context, baseURL string) (ollamaTagsResponse, error) {
	client := &http.Client{Timeout: evaluatorNetworkTimeout}
	url := baseURL + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ollamaTagsResponse{}, errs.Wrap(errs.CategoryInternal, err, "build ollama tags request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return ollamaTagsResponse{}, errs.Wrap(errs.CategoryConflict, err, "ollama not reachable at %s", url)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ollamaTagsResponse{}, errs.New(errs.CategoryConflict, "ollama tags request returned status %d", resp.StatusCode)
	}
	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return ollamaTagsResponse{}, errs.Wrap(errs.CategoryInternal, err, "decode ollama tags response")
	}
	return tags, nil
}

func findOllamaModel(tags ollamaTagsResponse, modelRef string) (ollamaModelEntry, bool) {
	for _, m := range tags.Models {
		if m.Name == modelRef {
			return m, true
		}
	}
	return ollamaModelEntry{}, false
}

// normalizeDigest strips an optional "sha256:" prefix so two digests can be
// compared as bare hex.
func normalizeDigest(d string) string { return strings.TrimPrefix(d, "sha256:") }

// ModelPresent implements LocalModelRuntimeAdapter.
func (OllamaAdapter) ModelPresent(ctx context.Context, deps EvaluatorDeps, op *protocol.ModelPresentOperand) (bool, string, error) {
	tags, err := fetchOllamaTags(ctx, deps.ollamaBaseURL())
	if err != nil {
		// Unreachable/unhealthy Ollama means the condition does not
		// currently hold, not that evaluation itself failed — the caller
		// (an executor precondition/postcondition check) should see "not
		// satisfied," not error out on a transient connectivity gap.
		return false, err.Error(), nil
	}

	entry, found := findOllamaModel(tags, op.ModelRef)
	if !found {
		return false, fmt.Sprintf("model %s not present", op.ModelRef), nil
	}
	// Exact normalized-digest equality only — a prefix match would accept
	// any digest sharing a prefix with the expected one, which is not
	// verification of the immutable revision the protocol field represents.
	if normalizeDigest(entry.Digest) != normalizeDigest(op.ResolvedRevision) {
		return false, fmt.Sprintf("model %s present but digest %s does not match expected %s", op.ModelRef, entry.Digest, op.ResolvedRevision), nil
	}
	return true, fmt.Sprintf("model %s present with matching digest", op.ModelRef), nil
}

// ollamaPullRef resolves the model reference actually passed to `ollama
// pull`: bare ModelRef when AllowedSource is Ollama's own implicit default
// (the common case today), otherwise explicitly prefixed with the declared
// source host so the approved plan's registry bound is actually what gets
// requested, not just recorded as metadata.
func ollamaPullRef(p *protocol.EnsureLocalModelParams) string {
	if p.AllowedSource == "" || p.AllowedSource == ollamaDefaultRegistryHost {
		return p.ModelRef
	}
	return p.AllowedSource + "/" + p.ModelRef
}

// EnsureModel implements LocalModelRuntimeAdapter.
func (OllamaAdapter) EnsureModel(ctx context.Context, deps applierDeps, p *protocol.EnsureLocalModelParams, captureOutput bool, verifiedExecutablePath string) (bool, string, *process.Result, *protocol.ArtifactRef, error) {
	if deps.runner == nil {
		return false, "", nil, nil, errs.New(errs.CategoryInvalidArgument, "OllamaAdapter.EnsureModel: requires a CommandRunner")
	}
	// The exact binary run here must be the one an executable_verified
	// precondition already checked (digest/version) — never a bare name
	// re-resolved from PATH, which could silently name a different binary
	// than the one just verified (ADR-0014 §1).
	if verifiedExecutablePath == "" {
		return false, "", nil, nil, errs.New(errs.CategoryPolicyDenied,
			"OllamaAdapter.EnsureModel: requires an executable_verified precondition binding the exact ollama binary to run")
	}

	spec := process.Spec{
		Executable: verifiedExecutablePath,
		Args:       []string{"pull", ollamaPullRef(p)},
		Dir:        homeOrTemp(deps.home),
		Env:        process.BaseEnv(),
		Timeout:    ollamaPullTimeout,
	}
	result, err := deps.runner.Run(ctx, spec)
	if err != nil {
		return false, "", nil, nil, err
	}
	artifact, artErr := maybeCaptureOutput(ctx, deps, "ensure_local_model_ollama", result, captureOutput)
	if artErr != nil {
		return false, "", nil, nil, artErr
	}
	if !result.Success() {
		return false, fmt.Sprintf("ollama pull %s failed (exit %d)", p.ModelRef, result.ExitCode), &result, artifact, nil
	}

	// Bounded supply chain (ADR-0014 §7): the pull exiting 0 only means the
	// CLI reported success, not that what landed matches the approved
	// digest/size. Verify against the live Ollama API before calling the
	// operation a mutation, so a digest/size mismatch is caught by the
	// operation itself, not only (after the fact, if at all) by a separate
	// postcondition the plan happens to also declare.
	tags, tagsErr := fetchOllamaTags(ctx, deps.ollamaBase())
	if tagsErr != nil {
		return false, "", &result, artifact, errs.Wrap(errs.CategoryConflict, tagsErr, "ollama pull %s reported success but the resulting model could not be verified", p.ModelRef)
	}
	entry, found := findOllamaModel(tags, p.ModelRef)
	if !found {
		return false, fmt.Sprintf("ollama pull %s reported success but the model is not present afterward", p.ModelRef), &result, artifact, nil
	}
	if normalizeDigest(entry.Digest) != normalizeDigest(p.ResolvedRevision) {
		return false, fmt.Sprintf("ollama pull %s produced digest %s, which does not match the approved resolved_revision %s", p.ModelRef, entry.Digest, p.ResolvedRevision), &result, artifact, nil
	}
	if p.ExpectedSizeBytes > 0 && entry.Size != p.ExpectedSizeBytes {
		return false, fmt.Sprintf("ollama pull %s produced size %d bytes, which does not match the approved expected_size_bytes %d", p.ModelRef, entry.Size, p.ExpectedSizeBytes), &result, artifact, nil
	}

	return true, fmt.Sprintf("pulled %s via ollama, digest and size verified against the approved plan", p.ModelRef), &result, artifact, nil
}

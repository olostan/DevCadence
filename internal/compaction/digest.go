package compaction

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
)

// ObservedFact records an empirical fact with its evidence locator.
type ObservedFact struct {
	Statement   string `json:"statement"`
	EvidenceRef string `json:"evidence_ref"`
	Verified    bool   `json:"verified"`
}

// Hypothesis records a working model hypothesis and its status.
type Hypothesis struct {
	Statement string `json:"statement"`
	Status    string `json:"status"` // "testing", "confirmed", "disproven"
}

// Decision records an engineering decision and its authoritative origin.
type Decision struct {
	Statement      string `json:"statement"`
	AuthorizedBy   string `json:"authorized_by"`
	ReferenceValid bool   `json:"reference_valid,omitempty"`
	Verified       bool   `json:"verified"`
}

// RejectedHypothesis records an approach tested and abandoned, with reason.
type RejectedHypothesis struct {
	Statement string `json:"statement"`
	Reason    string `json:"reason"`
}

// TrajectoryDigest separates lossy model-derived trajectory information by epistemic status (ADR-0016).
type TrajectoryDigest struct {
	ObservedFacts           []ObservedFact       `json:"observed_facts"`
	Hypotheses              []Hypothesis         `json:"hypotheses"`
	Decisions               []Decision           `json:"decisions"`
	UnresolvedUncertainties []string             `json:"unresolved_uncertainties"`
	RejectedHypotheses      []RejectedHypothesis `json:"rejected_hypotheses"`
}

// ValidateEvidenceAndAuthority verifies model-authored evidence references against the artifact
// store and decision IDs against authoritative project records (ADR-0016).
func (d *TrajectoryDigest) ValidateEvidenceAndAuthority(store *artifacts.Store, knownDecisionIDs map[string]bool) {
	for i := range d.ObservedFacts {
		fact := &d.ObservedFacts[i]
		if fact.EvidenceRef != "" && store != nil {
			if _, err := store.Get(fact.EvidenceRef); err == nil {
				fact.Verified = true
				continue
			}
		}
		fact.Verified = false
	}

	for i := range d.Decisions {
		dec := &d.Decisions[i]
		if dec.AuthorizedBy != "" && knownDecisionIDs != nil {
			if knownDecisionIDs[dec.AuthorizedBy] {
				dec.ReferenceValid = true
				// Reference existence is not verified authorization of arbitrary model-authored text.
				// A model's paraphrase or arbitrary instruction is left unverified (DCI-041).
				dec.Verified = false
				continue
			}
		}
		dec.ReferenceValid = false
		dec.Verified = false
	}
}

// FormatAsUserMessage renders the TrajectoryDigest as a lower-trust user message,
// NEVER placing it in system instructions (ADR-0016).
func (d *TrajectoryDigest) FormatAsUserMessage() Message {
	data, _ := json.MarshalIndent(d, "", "  ")
	content := fmt.Sprintf("## Trajectory Digest (Episodic Context Summary)\n```json\n%s\n```", string(data))
	return Message{
		Role:      RoleUser,
		Content:   content,
		Protected: true,
		IsDigest:  true,
	}
}

// DigestInput provides bounded history to a summarizer.
type DigestInput struct {
	Session             *ExecutionSession
	MessagesToSummarize []Message
}

// Summarizer produces a lossy trajectory digest from earlier session turns.
type Summarizer interface {
	Summarize(ctx context.Context, policy SessionPolicy, input DigestInput, maxTokens int) (TrajectoryDigest, error)
}

// BaseSummarizer enforces privacy inheritance and boundedness before delegation.
type BaseSummarizer struct {
	Backend Summarizer
	IsLocal bool
}

// Summarize checks locality and policy inheritance before delegating.
func (s *BaseSummarizer) Summarize(ctx context.Context, policy SessionPolicy, input DigestInput, maxTokens int) (TrajectoryDigest, error) {
	if policy.Locality == LocalityLocalOnly && !s.IsLocal {
		return TrajectoryDigest{}, errs.New(errs.CategoryPolicyDenied,
			"summarizer: session policy is local_only but summarizer endpoint is remote (ADR-0016)")
	}
	if s.Backend == nil {
		return TrajectoryDigest{}, errs.New(errs.CategoryInternal, "summarizer backend is nil")
	}
	return s.Backend.Summarize(ctx, policy, input, maxTokens)
}

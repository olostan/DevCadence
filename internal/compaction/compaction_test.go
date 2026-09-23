package compaction

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
)

func setupTestStore(t *testing.T) *artifacts.Store {
	t.Helper()
	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestCompactionAdmissionRefusesUnknownLimit(t *testing.T) {
	session := &ExecutionSession{
		SystemPrompt: "You are an assistant",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
		},
	}

	budget := TokenBudget{
		MaxRequestTokens: 0, // Unknown/unprovided
		MaxOutputTokens:  1000,
	}

	err := AdmitOrCompact(context.Background(), session, budget, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("Expected admission to fail when MaxRequestTokens is unknown/zero")
	}
	if errs.CategoryOf(err) != errs.CategoryUnsupported {
		t.Errorf("Expected CategoryUnsupported, got: %v", err)
	}
}

func TestCompactionStatefulRearming(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	state := &AdmissionState{}

	// Create session with tool results that yield < 5% reduction when pruned
	// (e.g. short outputs)
	session := &ExecutionSession{
		SystemPrompt: "System",
		Messages: []Message{
			{Role: RoleUser, Content: strings.Repeat("u", 1400)},
			{Role: RoleAssistant, Content: "calling tool"},
			{Role: RoleTool, Content: strings.Repeat("x", 120), ToolCallID: "call_1"}, // small prune
			{Role: RoleUser, Content: strings.Repeat("u", 1400)},
			{Role: RoleAssistant, Content: "current"},
		},
	}

	budget := TokenBudget{
		MaxRequestTokens: 1000, // C = 1000. Load is ~750 tokens (> 75%)
		MaxOutputTokens:  100,
	}

	_ = AdmitOrCompact(ctx, session, budget, state, store, nil, nil)

	// Since reduction was small, Tier 1 should now be marked exhausted
	if !state.Tier1Exhausted {
		t.Errorf("Expected Tier 1 to be marked exhausted when reduction is < 5%%")
	}

	// Next, simulate load dropping below 65% (reset watermark: 650 tokens)
	session.Messages = session.Messages[:2]
	budgetReset := TokenBudget{
		MaxRequestTokens: 2000, // C = 2000. Total load << 65%
		MaxOutputTokens:  50,
	}

	_ = AdmitOrCompact(ctx, session, budgetReset, state, store, nil, nil)

	if state.Tier1Exhausted {
		t.Errorf("Expected Tier 1 rearming to reset Tier1Exhausted when load <= 65%%")
	}
}

func TestUniversalFinalAdmissionGuard(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Giant session that cannot fit in C = 500 tokens
	session := &ExecutionSession{
		SystemPrompt: strings.Repeat("massive prompt ", 100),
		Messages: []Message{
			{Role: RoleUser, Content: strings.Repeat("massive text ", 100)},
		},
	}

	budget := TokenBudget{
		MaxRequestTokens: 500,
		MaxOutputTokens:  100,
	}

	err := AdmitOrCompact(ctx, session, budget, nil, store, nil, nil)
	if err == nil {
		t.Fatal("Expected Universal Final Admission Guard to halt turn when total > C")
	}
	if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("Expected CategoryPolicyDenied, got: %v", err)
	}
	if !strings.Contains(err.Error(), "context_budget_exceeded") {
		t.Errorf("Expected context_budget_exceeded error, got: %v", err)
	}
}

func TestProtectedContextIntegrity(t *testing.T) {
	amendments := []Amendment{
		{AuthorityLevel: 1, JournalSeq: 5, Directive: "low authority, later seq"},
		{AuthorityLevel: 3, JournalSeq: 10, Directive: "highest authority, seq 10"},
		{AuthorityLevel: 2, JournalSeq: 2, Directive: "mid authority, seq 2"},
		{AuthorityLevel: 3, JournalSeq: 3, Directive: "highest authority, earlier seq"},
	}

	SortAmendments(amendments)

	// Expected order:
	// 1. Level 3, Seq 3
	// 2. Level 3, Seq 10
	// 3. Level 2, Seq 2
	// 4. Level 1, Seq 5
	if amendments[0].Directive != "highest authority, earlier seq" {
		t.Errorf("Expected amendments[0] to be highest authority, earlier seq, got: %s", amendments[0].Directive)
	}
	if amendments[1].Directive != "highest authority, seq 10" {
		t.Errorf("Expected amendments[1] to be highest authority, seq 10, got: %s", amendments[1].Directive)
	}
	if amendments[2].Directive != "mid authority, seq 2" {
		t.Errorf("Expected amendments[2] to be mid authority, seq 2, got: %s", amendments[2].Directive)
	}
	if amendments[3].Directive != "low authority, later seq" {
		t.Errorf("Expected amendments[3] to be low authority, later seq, got: %s", amendments[3].Directive)
	}
}

func TestAtomicToolGroupPreservation(t *testing.T) {
	// Valid group: assistant has tool call call_1, tool message has ToolCallID call_1
	validMessages := []Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{
				{ID: "call_1", Name: "read_file"},
			},
		},
		{
			Role:       RoleTool,
			ToolCallID: "call_1",
			Content:    "file content",
		},
	}
	if err := ValidateAtomicToolGroups(validMessages); err != nil {
		t.Errorf("Expected valid tool group to pass validation, got: %v", err)
	}

	// Orphaned tool result
	orphanedMessages := []Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{
				{ID: "call_different", Name: "read_file"},
			},
		},
		{
			Role:       RoleTool,
			ToolCallID: "call_orphaned",
			Content:    "orphan content",
		},
	}
	if err := ValidateAtomicToolGroups(orphanedMessages); err == nil {
		t.Errorf("Expected error for orphaned tool result")
	}
}

type fakeRemoteSummarizer struct{}

func (f fakeRemoteSummarizer) Summarize(ctx context.Context, policy SessionPolicy, input DigestInput, maxTokens int) (TrajectoryDigest, error) {
	return TrajectoryDigest{}, nil
}

func TestSummarizerPrivacyInheritance(t *testing.T) {
	base := &BaseSummarizer{
		Backend: fakeRemoteSummarizer{},
		IsLocal: false, // Remote endpoint
	}

	policy := SessionPolicy{
		Locality: LocalityLocalOnly,
	}

	_, err := base.Summarize(context.Background(), policy, DigestInput{}, 1000)
	if err == nil {
		t.Fatal("Expected error when local_only session routes to remote summarizer")
	}
	if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("Expected CategoryPolicyDenied, got: %v", err)
	}
}

func TestDigestAuthorityValidation(t *testing.T) {
	store := setupTestStore(t)
	putRes, err := store.PutBytes(context.Background(), "test_proj", "evidence", "text/plain", []byte("real content"), 0)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	knownDecisions := map[string]bool{
		"dec_turn_4": true,
	}

	digest := TrajectoryDigest{
		ObservedFacts: []ObservedFact{
			{Statement: "Valid fact", EvidenceRef: putRes.Ref.Locator},
			{Statement: "Bogus fact", EvidenceRef: "artifact:test_proj:0000000000000000000000000000000000000000000000000000000000000000"},
		},
		Decisions: []Decision{
			{Statement: "Valid decision", AuthorizedBy: "dec_turn_4"},
			{Statement: "Fabricated decision", AuthorizedBy: "dec_nonexistent"},
		},
	}

	digest.ValidateEvidenceAndAuthority(store, knownDecisions)

	if !digest.ObservedFacts[0].Verified {
		t.Errorf("Expected ObservedFacts[0] to be verified")
	}
	if digest.ObservedFacts[1].Verified {
		t.Errorf("Expected ObservedFacts[1] to NOT be verified")
	}

	if !digest.Decisions[0].ReferenceValid {
		t.Errorf("Expected Decisions[0] to have ReferenceValid = true")
	}
	if digest.Decisions[0].Verified {
		t.Errorf("Expected Decisions[0] paraphrase to NOT be verified")
	}
	if digest.Decisions[1].ReferenceValid {
		t.Errorf("Expected Decisions[1] to have ReferenceValid = false")
	}
	if digest.Decisions[1].Verified {
		t.Errorf("Expected Decisions[1] to NOT be verified")
	}
}

func TestStaleVerificationMarking(t *testing.T) {
	checkpoint := WorkspaceCheckpoint{
		BaseCommit:     "commit_1",
		CurrentTreeSHA: "tree_aaa",
		TestEvidence: []TestEvidenceRecord{
			{CheckID: "check-unit", TreeSHA: "tree_aaa", Passed: true, IsStale: false},
		},
	}

	// Verify initially fresh
	if checkpoint.TestEvidence[0].IsStale {
		t.Errorf("Expected initial evidence to be fresh")
	}

	// Files changed: new tree SHA
	checkpoint.RefreshStaleMarks("tree_bbb")

	if !checkpoint.TestEvidence[0].IsStale {
		t.Errorf("Expected evidence to be marked STALE after tree SHA changed")
	}
}

func TestPruneToolResultsNilStorePreservesContent(t *testing.T) {
	originalContent := "line 1 of very important log output that must not be deleted\nline 2 of output"
	session := &ExecutionSession{
		Policy: SessionPolicy{ProjectID: "proj_test"},
		Messages: []Message{
			{Role: RoleTool, Content: originalContent},
			{Role: RoleUser, Content: "next turn"},
			{Role: RoleAssistant, Content: "reply"},
		},
	}

	// store is nil -> pruning must be skipped, leaving content intact
	pruned, err := PruneToolResults(context.Background(), session, nil)
	if err != nil {
		t.Fatalf("PruneToolResults: %v", err)
	}
	if pruned != 0 {
		t.Errorf("Expected 0 pruned messages with nil store, got %d", pruned)
	}
	if session.Messages[0].Content != originalContent {
		t.Errorf("Expected original content preserved, got %q", session.Messages[0].Content)
	}
	if session.Messages[0].IsPruned {
		t.Errorf("Expected is_pruned = false")
	}
}

type fakeMockSummarizer struct {
	digest TrajectoryDigest
}

func (f fakeMockSummarizer) Summarize(ctx context.Context, policy SessionPolicy, input DigestInput, maxTokens int) (TrajectoryDigest, error) {
	return f.digest, nil
}

func TestTier2PreservesAtomicToolGroups(t *testing.T) {
	session := &ExecutionSession{
		SystemPrompt: "sys",
		Policy:       SessionPolicy{ProjectID: "proj"},
		Messages: []Message{
			{Role: RoleUser, Content: "step 1: please search"},
			{Role: RoleAssistant, Content: "searching...", ToolCalls: []ToolCall{{ID: "tc-1", Name: "grep"}}},
			{Role: RoleTool, ToolCallID: "tc-1", Content: "grep results here..."},
			{Role: RoleAssistant, Content: "step 1 complete. Now step 2.", ToolCalls: []ToolCall{{ID: "tc-2", Name: "read_file"}}},
			{Role: RoleTool, ToolCallID: "tc-2", Content: "file content here..."},
			{Role: RoleAssistant, Content: "step 2 complete"},
		},
	}

	summarizer := fakeMockSummarizer{
		digest: TrajectoryDigest{
			ObservedFacts: []ObservedFact{{Statement: "step 1 done"}},
		},
	}

	// Trigger Tier 2 by setting low budget
	budget := TokenBudget{
		MaxRequestTokens: 100,
		MaxOutputTokens:  10,
	}

	err := AdmitOrCompact(context.Background(), session, budget, nil, nil, summarizer, nil)
	if err != nil {
		t.Fatalf("AdmitOrCompact: %v", err)
	}

	// Verify atomic tool group preservation on resulting messages
	if err := ValidateAtomicToolGroups(session.Messages); err != nil {
		t.Fatalf("Tier 2 produced broken tool group trajectory: %v", err)
	}
}

func TestTier2SingleCanonicalDigest(t *testing.T) {
	longText := strings.Repeat("detailed conversation turn about system architecture. ", 5)
	session := &ExecutionSession{
		SystemPrompt: "sys",
		Policy:       SessionPolicy{ProjectID: "proj"},
		Messages: []Message{
			{Role: RoleUser, Content: "step 1 " + longText},
			{Role: RoleAssistant, Content: "step 1 reply " + longText},
			{Role: RoleUser, Content: "step 2 " + longText},
			{Role: RoleAssistant, Content: "step 2 reply " + longText},
		},
	}

	summarizer := fakeMockSummarizer{
		digest: TrajectoryDigest{
			ObservedFacts: []ObservedFact{{Statement: "compacted"}},
		},
	}

	budget := TokenBudget{
		MaxRequestTokens: 320,
		MaxOutputTokens:  10,
	}

	// First pass
	if err := AdmitOrCompact(context.Background(), session, budget, nil, nil, summarizer, nil); err != nil {
		t.Fatalf("Pass 1: %v", err)
	}

	// Add more turns
	session.Messages = append(session.Messages,
		Message{Role: RoleUser, Content: "step 3 " + longText},
		Message{Role: RoleAssistant, Content: "step 3 reply " + longText},
	)

	// Second pass
	if err := AdmitOrCompact(context.Background(), session, budget, nil, nil, summarizer, nil); err != nil {
		t.Fatalf("Pass 2: %v", err)
	}

	// Count digest messages
	digestCount := 0
	for _, m := range session.Messages {
		if strings.HasPrefix(m.Content, "## Trajectory Digest") {
			digestCount++
		}
	}

	if digestCount != 1 {
		t.Fatalf("Expected exactly 1 canonical digest message, found %d", digestCount)
	}
}

func TestTier2DigestModelVerifiedFlagIsUntrusted(t *testing.T) {
	longText := strings.Repeat("turn content ", 25)
	session := &ExecutionSession{
		SystemPrompt: "sys",
		Policy:       SessionPolicy{ProjectID: "proj"},
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1 " + longText},
			{Role: RoleAssistant, Content: "msg 2 " + longText},
			{Role: RoleUser, Content: "msg 3 " + longText},
			{Role: RoleAssistant, Content: "msg 4 " + longText},
		},
		KnownDecisionIDs: map[string]bool{
			"legit-dec": true,
		},
	}

	// Model falsely claims Verified: true for bogus decision and bogus evidence
	summarizer := fakeMockSummarizer{
		digest: TrajectoryDigest{
			ObservedFacts: []ObservedFact{
				{Statement: "Bogus fact", EvidenceRef: "fake:ref", Verified: true},
			},
			Decisions: []Decision{
				{Statement: "Bogus decision", AuthorizedBy: "bogus-dec", Verified: true},
				{Statement: "Legit decision", AuthorizedBy: "legit-dec", Verified: true},
			},
		},
	}

	budget := TokenBudget{
		MaxRequestTokens: 350,
		MaxOutputTokens:  10,
	}

	if err := AdmitOrCompact(context.Background(), session, budget, nil, nil, summarizer, nil); err != nil {
		t.Fatalf("AdmitOrCompact: %v", err)
	}

	if session.Digest == nil {
		t.Fatalf("Expected digest to be set")
	}

	// Fact with fake ref must be stripped of verified
	if session.Digest.ObservedFacts[0].Verified {
		t.Errorf("Model-supplied verified flag on bogus fact was not reset to false")
	}
	// Decision with bogus ID must have ReferenceValid = false and Verified = false
	if session.Digest.Decisions[0].ReferenceValid || session.Digest.Decisions[0].Verified {
		t.Errorf("Model-supplied bogus decision should not have valid reference or be verified")
	}
	// Decision with legit ID should have ReferenceValid = true, but Verified = false (unverified paraphrase)
	if !session.Digest.Decisions[1].ReferenceValid {
		t.Errorf("Legitimate decision reference should be marked valid")
	}
	if session.Digest.Decisions[1].Verified {
		t.Errorf("Model-authored decision statement must not be marked verified solely from ID existence")
	}
}

func TestClosureKeepsProtectedToolGroupWhole(t *testing.T) {
	longText := strings.Repeat("detail ", 40)
	session := &ExecutionSession{
		SystemPrompt: "system",
		Messages: []Message{
			{Role: RoleUser, Content: "early question " + longText},
			{Role: RoleAssistant, Content: "early answer " + longText},
			{
				Role:      RoleAssistant,
				Content:   "call " + longText,
				Protected: true,
				ToolCalls: []ToolCall{{ID: "call1", Name: "fetch"}},
			},
			{
				Role:       RoleTool,
				Content:    "result " + longText,
				ToolCallID: "call1",
				Protected:  false, // Unprotected result in protected tool group
			},
			{
				Role:    RoleUser,
				Content: "user message " + longText,
			},
			{
				Role:    RoleAssistant,
				Content: "final reply " + longText,
			},
		},
	}

	summarizer := fakeMockSummarizer{
		digest: TrajectoryDigest{
			ObservedFacts: []ObservedFact{{Statement: "Summarized facts"}},
		},
	}

	budget := TokenBudget{
		MaxRequestTokens: 400,
		MaxOutputTokens:  10,
	}

	if err := AdmitOrCompact(context.Background(), session, budget, nil, nil, summarizer, nil); err != nil {
		t.Fatalf("AdmitOrCompact: %v", err)
	}

	// Verify that if call1 is retained, its matching tool result is ALSO retained
	hasCall1 := false
	hasResult1 := false
	for _, m := range session.Messages {
		if m.Role == RoleAssistant {
			for _, tc := range m.ToolCalls {
				if tc.ID == "call1" {
					hasCall1 = true
				}
			}
		} else if m.Role == RoleTool && m.ToolCallID == "call1" {
			hasResult1 = true
		}
	}

	if hasCall1 && !hasResult1 {
		t.Fatalf("Protected tool call retained but unprotected tool result was discarded!")
	}
}

func TestClosurePreservesProtectedUserWithDigestHeading(t *testing.T) {
	longText := strings.Repeat("context ", 40)
	protectedCorrection := "## Trajectory Digest\nCorrection: stop deployment and preserve this instruction."
	session := &ExecutionSession{
		SystemPrompt: "system",
		Messages: []Message{
			{Role: RoleUser, Content: "earlier step " + longText},
			{Role: RoleAssistant, Content: "earlier response " + longText},
			{Role: RoleUser, Content: protectedCorrection, Protected: true},
			{Role: RoleAssistant, Content: "ack " + longText},
		},
	}

	summarizer := fakeMockSummarizer{
		digest: TrajectoryDigest{
			ObservedFacts: []ObservedFact{{Statement: "digest summary"}},
		},
	}

	budget := TokenBudget{
		MaxRequestTokens: 200,
		MaxOutputTokens:  10,
	}

	if err := AdmitOrCompact(context.Background(), session, budget, nil, nil, summarizer, nil); err != nil {
		t.Fatalf("AdmitOrCompact: %v", err)
	}

	foundProtected := false
	for _, m := range session.Messages {
		if m.Content == protectedCorrection {
			foundProtected = true
			if !m.Protected {
				t.Errorf("Protected flag lost on user correction message")
			}
		}
	}

	if !foundProtected {
		t.Fatalf("Protected user message with '## Trajectory Digest' heading was improperly deleted!")
	}
}

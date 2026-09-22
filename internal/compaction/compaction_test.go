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

	if !digest.Decisions[0].Verified {
		t.Errorf("Expected Decisions[0] to be verified")
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

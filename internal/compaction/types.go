package compaction

import (
	"time"
)

// LocalityPolicy specifies model endpoint locality constraint (ADR-0011, ADR-0016).
type LocalityPolicy string

const (
	LocalityLocalOnly LocalityPolicy = "local_only"
	LocalityHybrid    LocalityPolicy = "hybrid"
	LocalityRemote    LocalityPolicy = "remote_allowed"
)

// SessionPolicy configures privacy, locality, and cost inheritance for a session.
type SessionPolicy struct {
	Locality LocalityPolicy `json:"locality"`
	ProjectID string        `json:"project_id"`
}

// Amendment represents an authorized assignment change, correction, or directive.
type Amendment struct {
	AuthorityLevel int       `json:"authority_level"` // higher = higher precedence
	JournalSeq     uint64    `json:"journal_seq"`     // monotonic event journal sequence
	Directive      string    `json:"directive"`
	Timestamp      time.Time `json:"timestamp"`
}

// AuthorizedAssignment represents the immutable engineering assignment and its amendments.
type AuthorizedAssignment struct {
	WorkPackageID string      `json:"work_package_id"`
	Title         string      `json:"title"`
	Objective     string      `json:"objective"`
	Invariants    []string    `json:"invariants"`
	Constraints   []string    `json:"constraints"`
	Amendments    []Amendment `json:"amendments"`
}

// TestEvidenceRecord ties a verification outcome to an exact tree SHA.
type TestEvidenceRecord struct {
	CheckID string `json:"check_id"`
	TreeSHA string `json:"tree_sha"`
	Passed  bool   `json:"passed"`
	IsStale bool   `json:"is_stale"`
}

// WorkspaceCheckpoint captures the concrete repository and verification state.
type WorkspaceCheckpoint struct {
	BaseCommit     string               `json:"base_commit"`
	HeadCommit     string               `json:"head_commit"`
	WorktreeID     string               `json:"worktree_id"`
	CurrentTreeSHA string               `json:"current_tree_sha"`
	StagedFiles    []string             `json:"staged_files"`
	UnstagedFiles  []string             `json:"unstaged_files"`
	UntrackedFiles []string             `json:"untracked_files"`
	TestEvidence   []TestEvidenceRecord `json:"test_evidence"`
}

// RefreshStaleMarks inspects all test evidence records and flags those
// whose validated tree SHA does not match currentTreeSHA as STALE (ADR-0016).
func (wc *WorkspaceCheckpoint) RefreshStaleMarks(currentTreeSHA string) {
	wc.CurrentTreeSHA = currentTreeSHA
	for i := range wc.TestEvidence {
		if wc.TestEvidence[i].TreeSHA != currentTreeSHA {
			wc.TestEvidence[i].IsStale = true
		} else {
			wc.TestEvidence[i].IsStale = false
		}
	}
}

// ToolCall represents a model-generated tool invocation.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// MessageRole represents the message sender.
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

// Message represents an atomic turn in the execution session.
type Message struct {
	Role        MessageRole `json:"role"`
	Content     string      `json:"content"`
	ToolCalls   []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID  string      `json:"tool_call_id,omitempty"`
	ContentRef  string      `json:"content_ref,omitempty"`
	IsPruned    bool        `json:"is_pruned,omitempty"`
	Protected   bool        `json:"protected,omitempty"`
	GroupID     string      `json:"group_id,omitempty"`     // Links assistant tool_calls with matching tool results
	Authority   int         `json:"authority,omitempty"`    // Authority level
	JournalSeq  uint64      `json:"journal_seq,omitempty"` // Monotonic sequence
}

// ExecutionSession holds the conversation history and active state.
type ExecutionSession struct {
	SystemPrompt        string
	Assignment          AuthorizedAssignment
	Checkpoint          WorkspaceCheckpoint
	Digest              *TrajectoryDigest
	Messages            []Message
	ActiveOperationIDs  []string
	KnownDecisionIDs    map[string]bool
	Policy              SessionPolicy
}

// Package drivers implements the cognition session execution substrate,
// normalizing model interaction, session lifecycles, streaming events,
// tool mediation, and silent multi-dimensional metering across heterogeneous endpoints
// (direct APIs, local runtimes, and authenticated CLIs).
package drivers

import (
	"encoding/json"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/tools"
)

// DriverCapabilities expresses the operational and context controllability of a driver.
// It maps directly to protocol.AccessChannel capabilities (ADR-0018, ADR-0019).
type DriverCapabilities struct {
	Kind              protocol.ChannelKind    `json:"kind"`
	SessionMode       protocol.SessionMode    `json:"session_mode"`
	ContextControl    protocol.ContextControl `json:"context_control"`
	PrefixCache       protocol.PrefixCache    `json:"prefix_cache"`
	SupportsStreaming bool                    `json:"supports_streaming"`
	SupportsTools     bool                    `json:"supports_tools"`
	// NativeWorktreeAccess indicates whether the execution substrate guarantees kernel-level
	// or sandbox-enforced filesystem containment to the session's worktree.
	// NOTE: Setting process CWD/Dir alone does NOT provide filesystem containment.
	// Unless an explicit verified sandbox provider is active, NativeWorktreeAccess MUST be false,
	// and write mutations must be routed through mediated DevCadence tools (ToolMediator) for verified containment.
	NativeWorktreeAccess  bool `json:"native_worktree_access"`
	MaxConcurrentRequests int  `json:"max_concurrent_requests"`
}

// Validate ensures that driver capabilities conform to defined protocol types and limits.
func (c DriverCapabilities) Validate() error {
	const kind = "DriverCapabilities"
	if !c.Kind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid channel kind %q", kind, c.Kind)
	}
	if !c.SessionMode.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid session mode %q", kind, c.SessionMode)
	}
	if !c.ContextControl.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid context control %q", kind, c.ContextControl)
	}
	if !c.PrefixCache.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid prefix cache %q", kind, c.PrefixCache)
	}
	if c.MaxConcurrentRequests < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_concurrent_requests must be >= 1, got %d", kind, c.MaxConcurrentRequests)
	}
	return nil
}

// SessionStatus tracks the lifecycle status of a cognition session.
type SessionStatus string

const (
	// SessionStatusActive indicates the session is ready for turns.
	SessionStatusActive SessionStatus = "active"
	// SessionStatusPausedBudgetExceeded indicates the session is gracefully paused due to budget exhaustion.
	SessionStatusPausedBudgetExceeded SessionStatus = "PAUSED_BUDGET_EXCEEDED"
	// SessionStatusClosed indicates the session has been closed and released.
	SessionStatusClosed SessionStatus = "closed"
	// SessionStatusError indicates an unrecoverable runtime error occurred.
	SessionStatusError SessionStatus = "error"
)

// Valid reports whether the session status is known.
func (s SessionStatus) Valid() bool {
	switch s {
	case SessionStatusActive, SessionStatusPausedBudgetExceeded, SessionStatusClosed, SessionStatusError:
		return true
	}
	return false
}

// PathExtractor extracts relative filesystem paths from a tool's JSON arguments.
type PathExtractor func(args json.RawMessage) ([]string, error)

// ToolDefinition defines a callable tool with a JSON schema for arguments.
type ToolDefinition struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Parameters     json.RawMessage `json:"parameters,omitempty"`
	PathParameters []string        `json:"path_parameters,omitempty"` // Explicit parameter names that contain paths
	PathExtractor  PathExtractor   `json:"-"`                         // Optional custom path extractor
	MutatesFiles   bool            `json:"mutates_files,omitempty"`   // Declares whether this tool modifies files in the worktree
}

// LoopDetectorSnapshot preserves the internal state of SemanticLoopDetector across checkpoints.
type LoopDetectorSnapshot struct {
	ConsecutiveFailedKey   string              `json:"consecutive_failed_key"`
	ConsecutiveFailedCount int                 `json:"consecutive_failed_count"`
	FileEditHashes         map[string][]string `json:"file_edit_hashes,omitempty"`
	FileOscillationsCount  map[string]int      `json:"file_oscillations_count,omitempty"`
}

// ToolCall represents a model-issued request to execute a tool.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult represents the output of an executed tool call.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

// TokenMeasurement represents an optionally known token quantity.
// The zero value TokenMeasurement{} represents an UNKNOWN measurement (Known: false, Value: 0).
type TokenMeasurement struct {
	Known bool  `json:"known"`
	Value int64 `json:"value"`
}

// KnownMeasurement constructs a known token measurement.
// It requires value >= 0.
func KnownMeasurement(value int64) TokenMeasurement {
	if value < 0 {
		panic("KnownMeasurement: value must be non-negative")
	}
	return TokenMeasurement{
		Known: true,
		Value: value,
	}
}

// TokenUsage captures the measured tokens for a turn or cumulative session.
// Cached is typically a subset of Input and is excluded from Total().
type TokenUsage struct {
	Input  TokenMeasurement
	Cached TokenMeasurement
	Output TokenMeasurement
}

// KnownUsage constructs a TokenUsage where all three measurements are known.
func KnownUsage(input, cached, output int64) TokenUsage {
	return TokenUsage{
		Input:  KnownMeasurement(input),
		Cached: KnownMeasurement(cached),
		Output: KnownMeasurement(output),
	}
}

// KnownZeroUsage returns the identity accumulator TokenUsage where Input,
// Cached, and Output are all known zero measurements (KnownMeasurement(0)).
func KnownZeroUsage() TokenUsage {
	return TokenUsage{
		Input:  KnownMeasurement(0),
		Cached: KnownMeasurement(0),
		Output: KnownMeasurement(0),
	}
}

func addMeasurement(a, b TokenMeasurement) TokenMeasurement {
	if a.Known && b.Known {
		return TokenMeasurement{
			Known: true,
			Value: a.Value + b.Value,
		}
	}
	return TokenMeasurement{}
}

// Add returns the element-wise sum of two TokenUsages.
// Per field, the resulting measurement is Known iff both operands are Known,
// and values are summed only when both are Known.
func (u TokenUsage) Add(other TokenUsage) TokenUsage {
	return TokenUsage{
		Input:  addMeasurement(u.Input, other.Input),
		Cached: addMeasurement(u.Cached, other.Cached),
		Output: addMeasurement(u.Output, other.Output),
	}
}

// Total returns the sum of Input and Output tokens, and whether the total is known.
// The total is known iff both Input and Output are Known.
// Cached is typically a subset of Input and is excluded from Total.
func (u TokenUsage) Total() (int64, bool) {
	if u.Input.Known && u.Output.Known {
		return u.Input.Value + u.Output.Value, true
	}
	return 0, false
}

// Complete reports whether all three measurements (Input, Cached, Output) are known.
func (u TokenUsage) Complete() bool {
	return u.Input.Known && u.Cached.Known && u.Output.Known
}

type tokenUsageWire struct {
	InputTokens  *int64 `json:"input_tokens,omitempty"`
	CachedTokens *int64 `json:"cached_tokens,omitempty"`
	OutputTokens *int64 `json:"output_tokens,omitempty"`
}

// MarshalJSON serializes TokenUsage into JSON.
// Known fields serialize as non-negative integers; unknown fields are omitted.
func (u TokenUsage) MarshalJSON() ([]byte, error) {
	var wire tokenUsageWire
	if u.Input.Known {
		if u.Input.Value < 0 {
			return nil, errs.New(errs.CategoryInvalidArgument, "input_tokens cannot be negative: %d", u.Input.Value)
		}
		v := u.Input.Value
		wire.InputTokens = &v
	}
	if u.Cached.Known {
		if u.Cached.Value < 0 {
			return nil, errs.New(errs.CategoryInvalidArgument, "cached_tokens cannot be negative: %d", u.Cached.Value)
		}
		v := u.Cached.Value
		wire.CachedTokens = &v
	}
	if u.Output.Known {
		if u.Output.Value < 0 {
			return nil, errs.New(errs.CategoryInvalidArgument, "output_tokens cannot be negative: %d", u.Output.Value)
		}
		v := u.Output.Value
		wire.OutputTokens = &v
	}
	return json.Marshal(wire)
}

// UnmarshalJSON deserializes TokenUsage from JSON.
// Non-negative integers decode to Known: true. Missing keys or null decode to Known: false.
// Negative integers are rejected with errs.CategoryInvalidArgument.
func (u *TokenUsage) UnmarshalJSON(data []byte) error {
	var wire tokenUsageWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "failed unmarshaling TokenUsage")
	}
	var res TokenUsage
	if wire.InputTokens != nil {
		if *wire.InputTokens < 0 {
			return errs.New(errs.CategoryInvalidArgument, "input_tokens cannot be negative: %d", *wire.InputTokens)
		}
		res.Input = KnownMeasurement(*wire.InputTokens)
	}
	if wire.CachedTokens != nil {
		if *wire.CachedTokens < 0 {
			return errs.New(errs.CategoryInvalidArgument, "cached_tokens cannot be negative: %d", *wire.CachedTokens)
		}
		res.Cached = KnownMeasurement(*wire.CachedTokens)
	}
	if wire.OutputTokens != nil {
		if *wire.OutputTokens < 0 {
			return errs.New(errs.CategoryInvalidArgument, "output_tokens cannot be negative: %d", *wire.OutputTokens)
		}
		res.Output = KnownMeasurement(*wire.OutputTokens)
	}
	*u = res
	return nil
}

// TurnInput is the input payload for an individual turn.
type TurnInput struct {
	TurnID      string       `json:"turn_id"`
	Prompt      string       `json:"prompt,omitempty"`
	ToolResults []ToolResult `json:"tool_results,omitempty"`
}

// TurnResult is the structured result returned after a turn execution.
type TurnResult struct {
	TurnID       string        `json:"turn_id"`
	Content      string        `json:"content"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	Usage        TokenUsage    `json:"usage"`
	Duration     time.Duration `json:"duration"`
	PausedReason string        `json:"paused_reason,omitempty"`
}

// DriverEventKind distinguishes kinds of streaming driver events.
type DriverEventKind string

const (
	EventContentDelta  DriverEventKind = "content_delta"
	EventToolCall      DriverEventKind = "tool_call"
	EventToolResult    DriverEventKind = "tool_result"
	EventTurnCompleted DriverEventKind = "turn_completed"
	EventSessionPaused DriverEventKind = "session_paused"
	EventSessionClosed DriverEventKind = "session_closed"
	EventError         DriverEventKind = "error"
)

// DriverEvent is an observation emitted during session execution or streaming.
type DriverEvent struct {
	Kind       DriverEventKind `json:"kind"`
	SessionID  string          `json:"session_id"`
	TurnID     string          `json:"turn_id,omitempty"`
	Delta      string          `json:"delta,omitempty"`
	ToolCall   *ToolCall       `json:"tool_call,omitempty"`
	ToolResult *ToolResult     `json:"tool_result,omitempty"`
	Usage      *TokenUsage     `json:"usage,omitempty"`
	Error      string          `json:"error,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
}

// SessionConfig configures the initialization or resumption of a session.
type SessionConfig struct {
	SessionID              string            `json:"session_id"`
	ModelID                string            `json:"model_id"`
	SystemPrompt           string            `json:"system_prompt,omitempty"`
	Tools                  []ToolDefinition  `json:"tools,omitempty"`
	WorktreeScope          *tools.Scope      `json:"worktree_scope,omitempty"`
	Mediator               ToolMediator      `json:"-"`
	Options                map[string]string `json:"options,omitempty"`
	MaxOutputTokensPerCall int64             `json:"max_output_tokens_per_call,omitempty"`
}

// Validate checks the basic validity of a SessionConfig.
func (cfg SessionConfig) Validate() error {
	const kind = "SessionConfig"
	if cfg.SessionID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: session_id cannot be empty", kind)
	}
	if cfg.ModelID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: model_id cannot be empty", kind)
	}
	if cfg.MaxOutputTokensPerCall < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_output_tokens_per_call cannot be negative: %d", kind, cfg.MaxOutputTokensPerCall)
	}
	return nil
}

// DeepCopy returns an immutable, detached deep copy of SessionConfig.
func (cfg SessionConfig) DeepCopy() SessionConfig {
	cp := cfg
	if cfg.Tools != nil {
		cp.Tools = make([]ToolDefinition, len(cfg.Tools))
		for i, t := range cfg.Tools {
			var pathParams []string
			if t.PathParameters != nil {
				pathParams = append([]string(nil), t.PathParameters...)
			}
			cp.Tools[i] = ToolDefinition{
				Name:           t.Name,
				Description:    t.Description,
				Parameters:     append([]byte(nil), t.Parameters...),
				PathParameters: pathParams,
				PathExtractor:  t.PathExtractor,
				MutatesFiles:   t.MutatesFiles,
			}
		}
	}
	if cfg.WorktreeScope != nil {
		scopeCp := *cfg.WorktreeScope
		cp.WorktreeScope = &scopeCp
	}
	if cfg.Options != nil {
		cp.Options = make(map[string]string, len(cfg.Options))
		for k, v := range cfg.Options {
			cp.Options[k] = v
		}
	}
	return cp
}

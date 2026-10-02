package drivers

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
)

// PauseReasonBudgetExceeded is the canonical reason string for budget suspension (ADR-0019 §2, PROTOCOLS §10B).
const PauseReasonBudgetExceeded = "PAUSED_BUDGET_EXCEEDED"

// ErrBudgetExceeded is returned when an operation is rejected because a budget limit was crossed.
var ErrBudgetExceeded = errs.New(errs.CategoryNeedsPrincipal, "execution paused: %s", PauseReasonBudgetExceeded)

// MeterLimits defines silent outer bounds on cognitive execution.
// Prompts must NEVER disclose these limits or inject turn countdowns (ADR-0019 §2).
type MeterLimits struct {
	MaxCumulativeInputTokens  int64              `json:"max_cumulative_input_tokens,omitempty"`
	MaxCumulativeCachedTokens int64              `json:"max_cumulative_cached_tokens,omitempty"`
	MaxCumulativeOutputTokens int64              `json:"max_cumulative_output_tokens,omitempty"`
	MaxCumulativeTotalTokens  int64              `json:"max_cumulative_total_tokens,omitempty"`
	MaxDurationPerOp          time.Duration      `json:"max_duration_per_op,omitempty"`
	MaxCumulativeDuration     time.Duration      `json:"max_cumulative_duration,omitempty"`
	MaxCumulativeToolCalls    int                `json:"max_cumulative_tool_calls,omitempty"`
	LoopConfig                LoopDetectorConfig `json:"loop_config"`
}

// MeterSnapshot represents an immutable checkpoint of metering metrics and suspension state.
type MeterSnapshot struct {
	SessionID           string        `json:"session_id"`
	TurnCount           int           `json:"turn_count"`
	CumulativeUsage     TokenUsage    `json:"cumulative_usage"`
	CumulativeDuration  time.Duration `json:"cumulative_duration"`
	LastOpDuration      time.Duration `json:"last_op_duration"`
	CumulativeToolCalls int           `json:"cumulative_tool_calls"`
	Status              SessionStatus `json:"status"`
	PausedReason        string        `json:"paused_reason,omitempty"`
	ExceededDimension   string        `json:"exceeded_dimension,omitempty"`
	EscalationRequired  bool          `json:"escalation_required"`
	CheckpointTimestamp time.Time     `json:"checkpoint_timestamp"`
}

// SilentMeter tracks multi-dimensional cognitive resource consumption and enforces
// outer limits silently without polluting model prompts.
type SilentMeter struct {
	mu                  sync.Mutex
	sessionID           string
	limits              MeterLimits
	turnCount           int
	cumulativeUsage     TokenUsage
	cumulativeDuration  time.Duration
	lastOpDuration      time.Duration
	cumulativeToolCalls int
	status              SessionStatus
	pausedReason        string
	exceededDimension   string
	escalationRequired  bool
	loopDetector        *SemanticLoopDetector
	recentToolCalls     map[string]ToolCall
	nowFn               func() time.Time
}

// NewSilentMeter constructs a new SilentMeter for the session.
func NewSilentMeter(sessionID string, limits MeterLimits) *SilentMeter {
	loopConfig := limits.LoopConfig
	if loopConfig.MaxConsecutiveFailedCalls <= 0 {
		loopConfig.MaxConsecutiveFailedCalls = 3
	}
	if loopConfig.MaxOscillatingEdits <= 0 {
		loopConfig.MaxOscillatingEdits = 2
	}
	return &SilentMeter{
		sessionID:       sessionID,
		limits:          limits,
		status:          SessionStatusActive,
		loopDetector:    NewSemanticLoopDetector(loopConfig),
		recentToolCalls: make(map[string]ToolCall),
		nowFn:           time.Now,
	}
}

// SetNowFunc overrides time source for deterministic testing.
func (m *SilentMeter) SetNowFunc(fn func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nowFn = fn
}

// RecordOperationStart checks if the session is eligible to start a new turn.
func (m *SilentMeter) RecordOperationStart(ctx context.Context) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status == SessionStatusPausedBudgetExceeded {
		return time.Time{}, errs.Wrap(errs.CategoryNeedsPrincipal, ErrBudgetExceeded,
			"session %s is paused (%s: %s)", m.sessionID, m.pausedReason, m.exceededDimension)
	}
	if m.status == SessionStatusClosed {
		return time.Time{}, errs.New(errs.CategoryInvalidTransition, "session %s is closed", m.sessionID)
	}

	return m.nowFn(), nil
}

// RecordOperationEnd records consumed resources and checks whether any budget limit was breached.
func (m *SilentMeter) RecordOperationEnd(start time.Time, usage TokenUsage, toolCalls []ToolCall, toolResults []ToolResult) (bool, MeterSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	opDuration := m.nowFn().Sub(start)
	m.lastOpDuration = opDuration
	m.cumulativeDuration += opDuration
	m.turnCount++
	m.cumulativeUsage = m.cumulativeUsage.Add(usage)
	m.cumulativeToolCalls += len(toolCalls)

	// Check semantic loop on tool results
	for _, res := range toolResults {
		call, ok := m.recentToolCalls[res.ToolCallID]
		if !ok {
			for _, tc := range toolCalls {
				if tc.ID == res.ToolCallID {
					call = tc
					ok = true
					break
				}
			}
		}
		if !ok {
			call = ToolCall{
				ID:   res.ToolCallID,
				Name: res.Name,
			}
		}
		if loopDetected, reason := m.loopDetector.RecordToolCall(call, res.IsError); loopDetected {
			m.tripPause("semantic_loop_repeated_tool_failures", reason)
			return true, m.snapshotLocked()
		}
	}

	// Record newly emitted toolCalls for future toolResults
	for _, tc := range toolCalls {
		m.recentToolCalls[tc.ID] = tc
	}
	if len(m.recentToolCalls) > 50 {
		m.recentToolCalls = make(map[string]ToolCall)
		for _, tc := range toolCalls {
			m.recentToolCalls[tc.ID] = tc
		}
	}

	// Check wall-clock limits
	if m.limits.MaxDurationPerOp > 0 && opDuration > m.limits.MaxDurationPerOp {
		m.tripPause("max_duration_per_op", fmt.Sprintf("operation duration %v exceeded limit %v", opDuration, m.limits.MaxDurationPerOp))
		return true, m.snapshotLocked()
	}
	if m.limits.MaxCumulativeDuration > 0 && m.cumulativeDuration > m.limits.MaxCumulativeDuration {
		m.tripPause("max_cumulative_duration", fmt.Sprintf("cumulative duration %v exceeded limit %v", m.cumulativeDuration, m.limits.MaxCumulativeDuration))
		return true, m.snapshotLocked()
	}

	// Check token limits
	if m.limits.MaxCumulativeInputTokens > 0 && m.cumulativeUsage.InputTokens > m.limits.MaxCumulativeInputTokens {
		m.tripPause("max_cumulative_input_tokens", fmt.Sprintf("cumulative input tokens %d exceeded limit %d", m.cumulativeUsage.InputTokens, m.limits.MaxCumulativeInputTokens))
		return true, m.snapshotLocked()
	}
	if m.limits.MaxCumulativeCachedTokens > 0 && m.cumulativeUsage.CachedTokens > m.limits.MaxCumulativeCachedTokens {
		m.tripPause("max_cumulative_cached_tokens", fmt.Sprintf("cumulative cached tokens %d exceeded limit %d", m.cumulativeUsage.CachedTokens, m.limits.MaxCumulativeCachedTokens))
		return true, m.snapshotLocked()
	}
	if m.limits.MaxCumulativeOutputTokens > 0 && m.cumulativeUsage.OutputTokens > m.limits.MaxCumulativeOutputTokens {
		m.tripPause("max_cumulative_output_tokens", fmt.Sprintf("cumulative output tokens %d exceeded limit %d", m.cumulativeUsage.OutputTokens, m.limits.MaxCumulativeOutputTokens))
		return true, m.snapshotLocked()
	}
	if m.limits.MaxCumulativeTotalTokens > 0 && m.cumulativeUsage.Total() > m.limits.MaxCumulativeTotalTokens {
		m.tripPause("max_cumulative_total_tokens", fmt.Sprintf("cumulative total tokens %d exceeded limit %d", m.cumulativeUsage.Total(), m.limits.MaxCumulativeTotalTokens))
		return true, m.snapshotLocked()
	}

	// Check tool call count limit
	if m.limits.MaxCumulativeToolCalls > 0 && m.cumulativeToolCalls > m.limits.MaxCumulativeToolCalls {
		m.tripPause("max_cumulative_tool_calls", fmt.Sprintf("cumulative tool calls %d exceeded limit %d", m.cumulativeToolCalls, m.limits.MaxCumulativeToolCalls))
		return true, m.snapshotLocked()
	}

	return false, m.snapshotLocked()
}

// RecordFileEdit checks for oscillating file modifications.
func (m *SilentMeter) RecordFileEdit(path string, content []byte) (bool, MeterSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	hash := HashContent(content)
	if loopDetected, reason := m.loopDetector.RecordFileEdit(path, hash); loopDetected {
		m.tripPause("semantic_loop_oscillating_edits", reason)
		return true, m.snapshotLocked()
	}

	return false, m.snapshotLocked()
}

func (m *SilentMeter) tripPause(dimension, detail string) {
	m.status = SessionStatusPausedBudgetExceeded
	m.pausedReason = PauseReasonBudgetExceeded
	m.exceededDimension = dimension
	m.escalationRequired = true
}

func (m *SilentMeter) snapshotLocked() MeterSnapshot {
	return MeterSnapshot{
		SessionID:           m.sessionID,
		TurnCount:           m.turnCount,
		CumulativeUsage:     m.cumulativeUsage,
		CumulativeDuration:  m.cumulativeDuration,
		LastOpDuration:      m.lastOpDuration,
		CumulativeToolCalls: m.cumulativeToolCalls,
		Status:              m.status,
		PausedReason:        m.pausedReason,
		ExceededDimension:   m.exceededDimension,
		EscalationRequired:  m.escalationRequired,
		CheckpointTimestamp: m.nowFn(),
	}
}

// Checkpoint returns a current immutable snapshot of the meter state.
func (m *SilentMeter) Checkpoint() MeterSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

// Status returns current session status.
func (m *SilentMeter) Status() SessionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// IsPaused returns true if execution has paused due to budget exhaustion.
func (m *SilentMeter) IsPaused() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status == SessionStatusPausedBudgetExceeded
}

// MeteredSession wraps an underlying Session with silent multi-dimensional metering.
type MeteredSession struct {
	session Session
	meter   *SilentMeter
}

// NewMeteredSession wraps a session with metering.
func NewMeteredSession(session Session, limits MeterLimits) *MeteredSession {
	return &MeteredSession{
		session: session,
		meter:   NewSilentMeter(session.ID(), limits),
	}
}

// ID returns the session ID.
func (s *MeteredSession) ID() string { return s.session.ID() }

// DriverID returns the driver ID.
func (s *MeteredSession) DriverID() string { return s.session.DriverID() }

// Config returns the session configuration.
func (s *MeteredSession) Config() SessionConfig { return s.session.Config() }

// Meter returns the underlying SilentMeter.
func (s *MeteredSession) Meter() *SilentMeter { return s.meter }

// Status returns the session status, honoring meter pause state.
func (s *MeteredSession) Status() SessionStatus {
	if s.meter.IsPaused() {
		return SessionStatusPausedBudgetExceeded
	}
	return s.session.Status()
}

// Close closes the session.
func (s *MeteredSession) Close(ctx context.Context) error {
	return s.session.Close(ctx)
}

// ExecuteTurn executes a turn while tracking metrics and enforcing bounds.
// Note: Prompt is passed through untouched without countdown injection.
func (s *MeteredSession) ExecuteTurn(ctx context.Context, input TurnInput) (TurnResult, error) {
	start, err := s.meter.RecordOperationStart(ctx)
	if err != nil {
		return TurnResult{
			TurnID:       input.TurnID,
			PausedReason: PauseReasonBudgetExceeded,
		}, err
	}

	result, turnErr := s.session.ExecuteTurn(ctx, input)
	if turnErr != nil {
		return result, turnErr
	}

	paused, _ := s.meter.RecordOperationEnd(start, result.Usage, result.ToolCalls, input.ToolResults)
	if paused {
		result.PausedReason = PauseReasonBudgetExceeded
	}

	return result, nil
}

// StreamTurn performs a streaming turn wrapped with silent metering.
func (s *MeteredSession) StreamTurn(ctx context.Context, input TurnInput) (EventStream, error) {
	start, err := s.meter.RecordOperationStart(ctx)
	if err != nil {
		return nil, err
	}

	rawStream, streamErr := s.session.StreamTurn(ctx, input)
	if streamErr != nil {
		return nil, streamErr
	}

	outStream := NewChannelEventStream(16)
	go func() {
		defer rawStream.Close()
		var cumulativeTurnUsage TokenUsage
		var emittedToolCalls []ToolCall

		for {
			ev, recvErr := rawStream.Recv()
			if recvErr != nil {
				if recvErr == io.EOF {
					// Turn ended normally
					paused, _ := s.meter.RecordOperationEnd(start, cumulativeTurnUsage, emittedToolCalls, input.ToolResults)
					if paused {
						outStream.Send(DriverEvent{
							Kind:      EventSessionPaused,
							SessionID: s.ID(),
							TurnID:    input.TurnID,
							Delta:     PauseReasonBudgetExceeded,
							Timestamp: time.Now(),
						})
					}
					outStream.CloseWithError(nil)
					return
				}
				outStream.CloseWithError(recvErr)
				return
			}

			if ev.Usage != nil {
				cumulativeTurnUsage = cumulativeTurnUsage.Add(*ev.Usage)
			}
			if ev.ToolCall != nil {
				emittedToolCalls = append(emittedToolCalls, *ev.ToolCall)
			}

			if !outStream.Send(ev) {
				return
			}
		}
	}()

	return outStream, nil
}

// MeteredDriver wraps any SessionDriver with silent metering.
type MeteredDriver struct {
	driver SessionDriver
	limits MeterLimits
}

// NewMeteredDriver wraps a SessionDriver with outer budget limits.
func NewMeteredDriver(driver SessionDriver, limits MeterLimits) *MeteredDriver {
	return &MeteredDriver{
		driver: driver,
		limits: limits,
	}
}

// ID returns driver ID.
func (d *MeteredDriver) ID() string { return d.driver.ID() }

// Capabilities returns driver capabilities.
func (d *MeteredDriver) Capabilities() DriverCapabilities { return d.driver.Capabilities() }

// StartSession creates a metered session.
func (d *MeteredDriver) StartSession(ctx context.Context, cfg SessionConfig) (Session, error) {
	session, err := d.driver.StartSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return NewMeteredSession(session, d.limits), nil
}

// ResumeSession resumes a metered session.
func (d *MeteredDriver) ResumeSession(ctx context.Context, sessionID string, cfg SessionConfig) (Session, error) {
	session, err := d.driver.ResumeSession(ctx, sessionID, cfg)
	if err != nil {
		return nil, err
	}
	return NewMeteredSession(session, d.limits), nil
}

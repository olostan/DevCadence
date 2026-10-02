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

// RestoreFromSnapshot hydrates meter metrics from an existing durable snapshot.
func (m *SilentMeter) RestoreFromSnapshot(snap MeterSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionID = snap.SessionID
	m.turnCount = snap.TurnCount
	m.cumulativeUsage = snap.CumulativeUsage
	m.cumulativeDuration = snap.CumulativeDuration
	m.lastOpDuration = snap.LastOpDuration
	m.cumulativeToolCalls = snap.CumulativeToolCalls
	m.status = snap.Status
	m.pausedReason = snap.PausedReason
	m.exceededDimension = snap.ExceededDimension
	m.escalationRequired = snap.EscalationRequired
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
	meter := NewSilentMeter(session.ID(), limits)
	return NewMeteredSessionWithMeter(session, meter)
}

// NewMeteredSessionWithMeter wraps a session using a pre-existing or restored meter.
func NewMeteredSessionWithMeter(session Session, meter *SilentMeter) *MeteredSession {
	ms := &MeteredSession{
		session: session,
		meter:   meter,
	}
	// Wire file edit listener on mediator if present so oscillating edits are actively tracked
	if mediator := session.Config().Mediator; mediator != nil {
		mediator.OnFileEdit(func(path string, content []byte) {
			meter.RecordFileEdit(path, content)
		})
	}
	return ms
}

// ID returns the session ID.
func (s *MeteredSession) ID() string { return s.session.ID() }

// DriverID returns the driver ID.
func (s *MeteredSession) DriverID() string { return s.session.DriverID() }

// Config returns an immutable deep copy of session configuration.
func (s *MeteredSession) Config() SessionConfig { return s.session.Config().DeepCopy() }

// Meter returns the underlying SilentMeter.
func (s *MeteredSession) Meter() *SilentMeter { return s.meter }

// Status returns the session status, honoring closed status before paused state.
func (s *MeteredSession) Status() SessionStatus {
	if s.session.Status() == SessionStatusClosed {
		return SessionStatusClosed
	}
	if s.meter.IsPaused() {
		return SessionStatusPausedBudgetExceeded
	}
	return s.session.Status()
}

// Close closes the session.
func (s *MeteredSession) Close(ctx context.Context) error {
	return s.session.Close(ctx)
}

// ExecuteTurn executes a turn while tracking metrics and actively enforcing bounds.
func (s *MeteredSession) ExecuteTurn(ctx context.Context, input TurnInput) (res TurnResult, err error) {
	start, startErr := s.meter.RecordOperationStart(ctx)
	if startErr != nil {
		return TurnResult{
			TurnID:       input.TurnID,
			PausedReason: PauseReasonBudgetExceeded,
		}, startErr
	}

	// Actively enforce MaxDurationPerOp via context timeout
	opCtx := ctx
	var cancelOp context.CancelFunc
	if s.meter.limits.MaxDurationPerOp > 0 {
		opCtx, cancelOp = context.WithTimeout(ctx, s.meter.limits.MaxDurationPerOp)
		defer cancelOp()
	}

	// Ensure RecordOperationEnd is called on EVERY terminal path
	defer func() {
		paused, _ := s.meter.RecordOperationEnd(start, res.Usage, res.ToolCalls, input.ToolResults)
		if paused {
			res.PausedReason = PauseReasonBudgetExceeded
		}
	}()

	res, err = s.session.ExecuteTurn(opCtx, input)
	if err != nil {
		if opCtx.Err() != nil && ctx.Err() == nil {
			err = errs.New(errs.CategoryProbeTimeout, "operation exceeded MaxDurationPerOp %v", s.meter.limits.MaxDurationPerOp)
		}
		return res, err
	}

	return res, nil
}

// StreamTurn performs a streaming turn wrapped with silent metering.
func (s *MeteredSession) StreamTurn(ctx context.Context, input TurnInput) (EventStream, error) {
	start, err := s.meter.RecordOperationStart(ctx)
	if err != nil {
		return nil, err
	}

	// Actively enforce MaxDurationPerOp via context timeout
	opCtx := ctx
	var cancelOp context.CancelFunc
	if s.meter.limits.MaxDurationPerOp > 0 {
		opCtx, cancelOp = context.WithTimeout(ctx, s.meter.limits.MaxDurationPerOp)
	}

	rawStream, streamErr := s.session.StreamTurn(opCtx, input)
	if streamErr != nil {
		if cancelOp != nil {
			cancelOp()
		}
		s.meter.RecordOperationEnd(start, TokenUsage{}, nil, input.ToolResults)
		return nil, streamErr
	}

	outStream := NewChannelEventStream(32)
	go func() {
		defer rawStream.Close()
		if cancelOp != nil {
			defer cancelOp()
		}

		var cumulativeTurnUsage TokenUsage
		var emittedToolCalls []ToolCall

		// Guarantee RecordOperationEnd on ALL exit paths (EOF, cancellation, stream error, consumer early close)
		defer func() {
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
		}()

		for {
			ev, recvErr := rawStream.Recv()
			if recvErr != nil {
				if recvErr == io.EOF {
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

// MeteredDriver wraps any SessionDriver with silent metering and persists
// meters across session resumptions and checkpoints.
type MeteredDriver struct {
	driver SessionDriver
	limits MeterLimits
	meters map[string]*SilentMeter
	mu     sync.RWMutex
}

// NewMeteredDriver wraps a SessionDriver with outer budget limits.
func NewMeteredDriver(driver SessionDriver, limits MeterLimits) *MeteredDriver {
	return &MeteredDriver{
		driver: driver,
		limits: limits,
		meters: make(map[string]*SilentMeter),
	}
}

// ID returns driver ID.
func (d *MeteredDriver) ID() string { return d.driver.ID() }

// Capabilities returns driver capabilities.
func (d *MeteredDriver) Capabilities() DriverCapabilities { return d.driver.Capabilities() }

// RestoreCheckpoint rehydrates a session meter from a durable checkpoint snapshot.
func (d *MeteredDriver) RestoreCheckpoint(snap MeterSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	meter := NewSilentMeter(snap.SessionID, d.limits)
	meter.RestoreFromSnapshot(snap)
	d.meters[snap.SessionID] = meter
}

// GetMeter returns the meter for a given session ID, if present.
func (d *MeteredDriver) GetMeter(sessionID string) *SilentMeter {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.meters[sessionID]
}

// StartSession creates a metered session.
func (d *MeteredDriver) StartSession(ctx context.Context, cfg SessionConfig) (Session, error) {
	session, err := d.driver.StartSession(ctx, cfg)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	meter, exists := d.meters[cfg.SessionID]
	if !exists {
		meter = NewSilentMeter(cfg.SessionID, d.limits)
		d.meters[cfg.SessionID] = meter
	}
	d.mu.Unlock()

	return NewMeteredSessionWithMeter(session, meter), nil
}

// ResumeSession resumes a metered session, preserving cumulative meter state.
func (d *MeteredDriver) ResumeSession(ctx context.Context, sessionID string, cfg SessionConfig) (Session, error) {
	session, err := d.driver.ResumeSession(ctx, sessionID, cfg)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	meter, exists := d.meters[sessionID]
	if !exists {
		meter = NewSilentMeter(sessionID, d.limits)
		d.meters[sessionID] = meter
	}
	d.mu.Unlock()

	return NewMeteredSessionWithMeter(session, meter), nil
}

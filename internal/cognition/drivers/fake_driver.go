package drivers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// FakeDriver is an in-memory controllable driver for unit testing, contract testing,
// and failure injection.
type FakeDriver struct {
	id           string
	capabilities DriverCapabilities
	mu           sync.RWMutex
	sessions     map[string]*fakeSession
	turnHandlers map[string]func(ctx context.Context, input TurnInput) (TurnResult, error)
	delay        time.Duration
}

// FakeDriverOptions configures a FakeDriver.
type FakeDriverOptions struct {
	Capabilities *DriverCapabilities
	Delay        time.Duration
}

// NewFakeDriver creates a new FakeDriver.
func NewFakeDriver(id string, opts ...FakeDriverOptions) *FakeDriver {
	caps := DriverCapabilities{
		Kind:                  protocol.ChannelLocalDaemonSocket,
		SessionMode:           protocol.SessionPersistentState,
		ContextControl:        protocol.ContextControlAppendOnly,
		PrefixCache:           protocol.PrefixCacheSessionKV,
		SupportsStreaming:     true,
		SupportsTools:         true,
		NativeWorktreeAccess:  false,
		MaxConcurrentRequests: 2,
	}

	var delay time.Duration
	if len(opts) > 0 {
		if opts[0].Capabilities != nil {
			caps = *opts[0].Capabilities
		}
		delay = opts[0].Delay
	}

	return &FakeDriver{
		id:           id,
		capabilities: caps,
		sessions:     make(map[string]*fakeSession),
		turnHandlers: make(map[string]func(ctx context.Context, input TurnInput) (TurnResult, error)),
		delay:        delay,
	}
}

// ID returns the driver identifier.
func (d *FakeDriver) ID() string { return d.id }

// Capabilities returns the driver capabilities.
func (d *FakeDriver) Capabilities() DriverCapabilities { return d.capabilities }

// SetDelay configures an artificial turn delay for simulating latency/cancellation.
func (d *FakeDriver) SetDelay(delay time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.delay = delay
}

// SetTurnHandler registers a scripted turn handler for custom responses.
func (d *FakeDriver) SetTurnHandler(turnID string, handler func(ctx context.Context, input TurnInput) (TurnResult, error)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.turnHandlers[turnID] = handler
}

// StartSession creates a new session.
func (d *FakeDriver) StartSession(ctx context.Context, cfg SessionConfig) (Session, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if len(cfg.Tools) > 0 && !d.capabilities.SupportsTools {
		return nil, errs.New(errs.CategoryUnsupported, "fake driver %q does not support tools", d.id)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.sessions[cfg.SessionID]; exists {
		return nil, errs.New(errs.CategoryConflict, "session %q already exists", cfg.SessionID)
	}

	if cfg.Mediator != nil {
		if scoped, ok := cfg.Mediator.(*ScopedToolMediator); ok {
			cfg.Mediator = scoped.ForSession(cfg.SessionID, cfg.Tools)
		} else {
			cfg.Mediator.SetDeclaredTools(cfg.Tools)
		}
	}

	s := &fakeSession{
		driver:    d,
		config:    cfg.DeepCopy(),
		status:    SessionStatusActive,
		turnCount: 0,
	}
	d.sessions[cfg.SessionID] = s
	return s, nil
}

// ResumeSession resumes an existing session.
func (d *FakeDriver) ResumeSession(ctx context.Context, sessionID string, cfg SessionConfig) (Session, error) {
	if sessionID == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "sessionID cannot be empty")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	s, exists := d.sessions[sessionID]
	if !exists {
		if cfg.SessionID == "" {
			cfg.SessionID = sessionID
		}
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		if cfg.Mediator != nil {
			if scoped, ok := cfg.Mediator.(*ScopedToolMediator); ok {
				cfg.Mediator = scoped.ForSession(cfg.SessionID, cfg.Tools)
			} else {
				cfg.Mediator.SetDeclaredTools(cfg.Tools)
			}
		}
		s = &fakeSession{
			driver:    d,
			config:    cfg.DeepCopy(),
			status:    SessionStatusActive,
			turnCount: 0,
		}
		d.sessions[sessionID] = s
	} else if s.status == SessionStatusClosed {
		s.status = SessionStatusActive
	}

	return s, nil
}

type fakeSession struct {
	driver    *FakeDriver
	config    SessionConfig
	status    SessionStatus
	turnCount int
	mu        sync.Mutex
}

func (s *fakeSession) ID() string { return s.config.SessionID }

func (s *fakeSession) DriverID() string { return s.driver.ID() }

// Config returns an immutable deep copy of session configuration.
func (s *fakeSession) Config() SessionConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config.DeepCopy()
}

func (s *fakeSession) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *fakeSession) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = SessionStatusClosed
	return nil
}

func (s *fakeSession) ExecuteTurn(ctx context.Context, input TurnInput) (TurnResult, error) {
	if err := ctx.Err(); err != nil {
		return TurnResult{}, err
	}

	s.mu.Lock()
	if s.status == SessionStatusClosed {
		s.mu.Unlock()
		return TurnResult{}, errs.New(errs.CategoryInvalidTransition, "session is closed")
	}
	s.turnCount++
	delay := s.driver.delay
	s.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			return TurnResult{}, ctx.Err()
		case <-time.After(delay):
		}
	}

	s.driver.mu.RLock()
	handler, hasHandler := s.driver.turnHandlers[input.TurnID]
	s.driver.mu.RUnlock()

	if hasHandler {
		return handler(ctx, input)
	}

	// Default synthetic turn response
	content := fmt.Sprintf("Fake response to: %s", input.Prompt)
	toolCalls := make([]ToolCall, 0)

	// If prompt asks to call a tool, generate one
	if strings.Contains(input.Prompt, "CALL_TOOL:") {
		parts := strings.Split(input.Prompt, "CALL_TOOL:")
		toolName := strings.TrimSpace(parts[1])
		tc := ToolCall{
			ID:        fmt.Sprintf("call-%d", s.turnCount),
			Name:      toolName,
			Arguments: []byte(`{"arg":"test"}`),
		}
		toolCalls = append(toolCalls, tc)

		if s.config.Mediator != nil {
			_, _ = s.config.Mediator.ExecuteTool(ctx, tc)
		}
	}

	return TurnResult{
		TurnID:    input.TurnID,
		Content:   content,
		ToolCalls: toolCalls,
		Usage: TokenUsage{
			InputTokens:  int64(len(input.Prompt)),
			CachedTokens: 0,
			OutputTokens: int64(len(content)),
		},
		Duration: delay,
	}, nil
}

func (s *fakeSession) StreamTurn(ctx context.Context, input TurnInput) (EventStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if !s.driver.capabilities.SupportsStreaming {
		return nil, errs.New(errs.CategoryUnsupported, "fake driver %q does not support streaming", s.driver.id)
	}

	s.mu.Lock()
	if s.status == SessionStatusClosed {
		s.mu.Unlock()
		return nil, errs.New(errs.CategoryInvalidTransition, "session is closed")
	}
	delay := s.driver.delay
	s.mu.Unlock()

	outStream := NewChannelEventStream(8)
	go func() {
		if delay > 0 {
			select {
			case <-ctx.Done():
				outStream.CloseWithError(ctx.Err())
				return
			case <-time.After(delay):
			}
		}

		words := strings.Fields(fmt.Sprintf("Fake stream response to: %s", input.Prompt))
		for _, w := range words {
			if err := ctx.Err(); err != nil {
				outStream.CloseWithError(err)
				return
			}
			outStream.Send(DriverEvent{
				Kind:      EventContentDelta,
				SessionID: s.ID(),
				TurnID:    input.TurnID,
				Delta:     w + " ",
				Timestamp: time.Now(),
			})
		}

		// Emit usage event
		outStream.Send(DriverEvent{
			Kind:      EventTurnCompleted,
			SessionID: s.ID(),
			TurnID:    input.TurnID,
			Usage: &TokenUsage{
				InputTokens:  int64(len(input.Prompt)),
				OutputTokens: int64(len(words) * 5),
			},
			Timestamp: time.Now(),
		})

		outStream.CloseWithError(nil)
	}()

	return outStream, nil
}

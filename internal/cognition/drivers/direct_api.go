package drivers

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// DirectAPIClient represents an underlying direct HTTP or local runtime API client.
type DirectAPIClient interface {
	Complete(ctx context.Context, req DirectAPIRequest) (DirectAPIResponse, error)
	Stream(ctx context.Context, req DirectAPIRequest) (EventStream, error)
}

// DirectAPIRequest is the payload sent to the direct API client.
type DirectAPIRequest struct {
	ModelID      string           `json:"model_id"`
	SystemPrompt string           `json:"system_prompt,omitempty"`
	Prompt       string           `json:"prompt,omitempty"`
	Messages     []DirectMessage  `json:"messages,omitempty"`
	Tools        []ToolDefinition `json:"tools,omitempty"`
	Stream       bool             `json:"stream,omitempty"`
}

// DirectMessage represents a single message in the direct API conversation history.
type DirectMessage struct {
	Role       string      `json:"role"`
	Content    string      `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
}

// DirectAPIResponse is the response returned by a direct API completion.
type DirectAPIResponse struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     TokenUsage `json:"usage"`
}

// DirectAPIDriver implements SessionDriver for direct HTTP APIs and local runtimes.
type DirectAPIDriver struct {
	id           string
	client       DirectAPIClient
	capabilities DriverCapabilities
	mu           sync.RWMutex
	sessions     map[string]*directAPISession
}

// DirectAPIOptions configures DirectAPIDriver.
type DirectAPIOptions struct {
	Capabilities *DriverCapabilities
}

// NewDirectAPIDriver creates a new DirectAPIDriver, failing fast on invalid client or capabilities.
func NewDirectAPIDriver(id string, client DirectAPIClient, opts ...DirectAPIOptions) (*DirectAPIDriver, error) {
	if client == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "direct_api: client cannot be nil")
	}

	caps := DriverCapabilities{
		Kind:                  protocol.ChannelDirectHTTPAPI,
		SessionMode:           protocol.SessionStatelessPerCall,
		ContextControl:        protocol.ContextControlExactStateless,
		PrefixCache:           protocol.PrefixCacheExplicit,
		SupportsStreaming:     true,
		SupportsTools:         true,
		NativeWorktreeAccess:  false,
		MaxConcurrentRequests: 4,
	}

	if len(opts) > 0 && opts[0].Capabilities != nil {
		caps = *opts[0].Capabilities
	}

	if err := caps.Validate(); err != nil {
		return nil, err
	}

	return &DirectAPIDriver{
		id:           id,
		client:       client,
		capabilities: caps,
		sessions:     make(map[string]*directAPISession),
	}, nil
}

// MustNewDirectAPIDriver creates a DirectAPIDriver or panics on invalid configuration.
func MustNewDirectAPIDriver(id string, client DirectAPIClient, opts ...DirectAPIOptions) *DirectAPIDriver {
	d, err := NewDirectAPIDriver(id, client, opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// ID returns driver ID.
func (d *DirectAPIDriver) ID() string { return d.id }

// Capabilities returns driver capabilities.
func (d *DirectAPIDriver) Capabilities() DriverCapabilities { return d.capabilities }

// StartSession creates a new direct API session.
func (d *DirectAPIDriver) StartSession(ctx context.Context, cfg SessionConfig) (Session, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if len(cfg.Tools) > 0 && !d.capabilities.SupportsTools {
		return nil, errs.New(errs.CategoryUnsupported, "direct api driver %q does not support tools", d.id)
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

	s := &directAPISession{
		driver:   d,
		config:   cfg.DeepCopy(),
		status:   SessionStatusActive,
		messages: make([]DirectMessage, 0),
	}
	d.sessions[cfg.SessionID] = s
	return s, nil
}

// ResumeSession resumes an existing session without wiping its message history.
func (d *DirectAPIDriver) ResumeSession(ctx context.Context, sessionID string, cfg SessionConfig) (Session, error) {
	if sessionID == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "sessionID cannot be empty")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	s, exists := d.sessions[sessionID]
	if !exists {
		// Restore session with provided config
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
		s = &directAPISession{
			driver:   d,
			config:   cfg.DeepCopy(),
			status:   SessionStatusActive,
			messages: make([]DirectMessage, 0),
		}
		d.sessions[sessionID] = s
	} else if s.status == SessionStatusClosed {
		// Reopen closed session while preserving existing messages
		s.status = SessionStatusActive
	}

	return s, nil
}

type directAPISession struct {
	driver   *DirectAPIDriver
	config   SessionConfig
	status   SessionStatus
	messages []DirectMessage
	mu       sync.Mutex
}

func (s *directAPISession) ID() string { return s.config.SessionID }

func (s *directAPISession) DriverID() string { return s.driver.ID() }

// Config returns an immutable deep copy of session configuration.
func (s *directAPISession) Config() SessionConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config.DeepCopy()
}

func (s *directAPISession) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *directAPISession) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = SessionStatusClosed
	return nil
}

func (s *directAPISession) ExecuteTurn(ctx context.Context, input TurnInput) (TurnResult, error) {
	if err := ctx.Err(); err != nil {
		return TurnResult{}, err
	}

	s.mu.Lock()
	if s.status == SessionStatusClosed {
		s.mu.Unlock()
		return TurnResult{}, errs.New(errs.CategoryInvalidTransition, "session is closed")
	}

	// Append incoming prompt / tool results
	if input.Prompt != "" {
		s.messages = append(s.messages, DirectMessage{
			Role:    "user",
			Content: input.Prompt,
		})
	}
	for _, tr := range input.ToolResults {
		cp := tr
		s.messages = append(s.messages, DirectMessage{
			Role:       "tool",
			Content:    tr.Content,
			ToolResult: &cp,
		})
	}

	req := DirectAPIRequest{
		ModelID:      s.config.ModelID,
		SystemPrompt: s.config.SystemPrompt,
		Prompt:       input.Prompt,
		Messages:     append([]DirectMessage(nil), s.messages...),
		Tools:        s.config.Tools,
		Stream:       false,
	}
	s.mu.Unlock()

	start := time.Now()
	resp, err := s.driver.client.Complete(ctx, req)
	duration := time.Since(start)
	if err != nil {
		return TurnResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Record assistant response
	s.messages = append(s.messages, DirectMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	})

	// If tools are returned and a mediator is registered, execute them
	if len(resp.ToolCalls) > 0 && s.config.Mediator != nil {
		for _, tc := range resp.ToolCalls {
			tr, medErr := s.config.Mediator.ExecuteTool(ctx, tc)
			if medErr != nil {
				tr = ToolResult{
					ToolCallID: tc.ID,
					Name:       tc.Name,
					Content:    medErr.Error(),
					IsError:    true,
				}
			}
			s.messages = append(s.messages, DirectMessage{
				Role:       "tool",
				Content:    tr.Content,
				ToolResult: &tr,
			})
		}
	}

	return TurnResult{
		TurnID:    input.TurnID,
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
		Usage:     resp.Usage,
		Duration:  duration,
	}, nil
}

func (s *directAPISession) StreamTurn(ctx context.Context, input TurnInput) (EventStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if !s.driver.capabilities.SupportsStreaming {
		return nil, errs.New(errs.CategoryUnsupported, "direct api driver %q does not support streaming", s.driver.id)
	}

	s.mu.Lock()
	if s.status == SessionStatusClosed {
		s.mu.Unlock()
		return nil, errs.New(errs.CategoryInvalidTransition, "session is closed")
	}

	if input.Prompt != "" {
		s.messages = append(s.messages, DirectMessage{
			Role:    "user",
			Content: input.Prompt,
		})
	}
	for _, tr := range input.ToolResults {
		cp := tr
		s.messages = append(s.messages, DirectMessage{
			Role:       "tool",
			Content:    tr.Content,
			ToolResult: &cp,
		})
	}

	req := DirectAPIRequest{
		ModelID:      s.config.ModelID,
		SystemPrompt: s.config.SystemPrompt,
		Prompt:       input.Prompt,
		Messages:     append([]DirectMessage(nil), s.messages...),
		Tools:        s.config.Tools,
		Stream:       true,
	}
	s.mu.Unlock()

	streamCtx, cancelStream := context.WithCancel(ctx)
	rawStream, err := s.driver.client.Stream(streamCtx, req)
	if err != nil {
		cancelStream()
		return nil, err
	}

	outStream := NewChannelEventStream(16)
	outStream.SetOnClose(func() {
		cancelStream()
		_ = rawStream.Close()
	})

	go func() {
		defer func() {
			cancelStream()
			_ = rawStream.Close()
		}()
		var fullContent string
		var toolCalls []ToolCall

		for {
			ev, recvErr := rawStream.Recv()
			if recvErr != nil {
				if recvErr == io.EOF {
					// Update history
					s.mu.Lock()
					s.messages = append(s.messages, DirectMessage{
						Role:      "assistant",
						Content:   fullContent,
						ToolCalls: toolCalls,
					})
					s.mu.Unlock()
					outStream.CloseWithError(nil)
					return
				}
				outStream.CloseWithError(recvErr)
				return
			}

			if ev.SessionID == "" {
				ev.SessionID = s.ID()
			}
			if ev.TurnID == "" {
				ev.TurnID = input.TurnID
			}
			if ev.Timestamp.IsZero() {
				ev.Timestamp = time.Now()
			}

			if ev.Delta != "" {
				fullContent += ev.Delta
			}
			if ev.ToolCall != nil {
				toolCalls = append(toolCalls, *ev.ToolCall)
			}

			if !outStream.Send(ev) {
				return
			}
		}
	}()

	return outStream, nil
}

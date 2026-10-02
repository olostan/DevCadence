package drivers

import (
	"context"
	"io"
	"sync"
)

// SessionDriver normalizes cognition interaction across heterogeneous endpoints:
// direct HTTP APIs, local runtimes, and authenticated CLIs.
type SessionDriver interface {
	// ID returns the stable identifier of the driver.
	ID() string
	// Capabilities returns the operational and context features supported by this driver.
	Capabilities() DriverCapabilities
	// StartSession initializes and starts a new session with the given configuration.
	StartSession(ctx context.Context, cfg SessionConfig) (Session, error)
	// ResumeSession resumes an existing session by ID.
	ResumeSession(ctx context.Context, sessionID string, cfg SessionConfig) (Session, error)
}

// Session represents an active or resumable cognitive session.
type Session interface {
	// ID returns the unique session identifier.
	ID() string
	// DriverID returns the ID of the driver owning this session.
	DriverID() string
	// Config returns the immutable configuration used to start or resume this session.
	Config() SessionConfig
	// Status returns the current lifecycle status of the session.
	Status() SessionStatus
	// ExecuteTurn performs a unary, synchronous turn with the model.
	ExecuteTurn(ctx context.Context, input TurnInput) (TurnResult, error)
	// StreamTurn performs a streaming turn, returning an EventStream for incremental consumption.
	StreamTurn(ctx context.Context, input TurnInput) (EventStream, error)
	// Close terminates the session and frees underlying resources.
	Close(ctx context.Context) error
}

// EventStream provides sequential consumption of DriverEvents from a streaming turn.
type EventStream interface {
	// Recv blocks until the next event is ready, returning io.EOF when stream terminates cleanly.
	Recv() (DriverEvent, error)
	// Close halts the stream early and cleans up resources.
	Close() error
}

// ChannelEventStream is a thread-safe, strictly FIFO implementation of EventStream.
// It guarantees that all buffered events are delivered before any terminal error or EOF,
// and prevents any send-on-closed panic under concurrent Close/CloseWithError calls.
type ChannelEventStream struct {
	mu      sync.Mutex
	cond    *sync.Cond
	queue   []DriverEvent
	termErr error
	closed  bool
}

// NewChannelEventStream creates an event stream with the specified buffer capacity.
func NewChannelEventStream(buffer int) *ChannelEventStream {
	if buffer < 1 {
		buffer = 1
	}
	s := &ChannelEventStream{
		queue: make([]DriverEvent, 0, buffer),
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Send emits an event to the stream in strictly FIFO order.
// Returns false if the stream has already been closed.
func (s *ChannelEventStream) Send(event DriverEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.queue = append(s.queue, event)
	s.cond.Signal()
	return true
}

// CloseWithError closes the event stream reporting an error (or nil for clean EOF).
// Any already-buffered events remain receivable by Recv() before the error or EOF is reported.
func (s *ChannelEventStream) CloseWithError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.termErr = err
	s.cond.Broadcast()
}

// Recv receives the next event from the stream in strictly FIFO order.
func (s *ChannelEventStream) Recv() (DriverEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for len(s.queue) == 0 && !s.closed {
		s.cond.Wait()
	}

	if len(s.queue) > 0 {
		ev := s.queue[0]
		s.queue = s.queue[1:]
		return ev, nil
	}

	if s.termErr != nil {
		return DriverEvent{}, s.termErr
	}
	return DriverEvent{}, io.EOF
}

// Close closes the stream early without error.
func (s *ChannelEventStream) Close() error {
	s.CloseWithError(nil)
	return nil
}

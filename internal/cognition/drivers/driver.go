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

// ChannelEventStream is an in-memory channel-backed implementation of EventStream.
type ChannelEventStream struct {
	events chan DriverEvent
	errCh  chan error
	done   chan struct{}
	once   sync.Once
}

// NewChannelEventStream creates an event stream with the specified buffer capacity.
func NewChannelEventStream(buffer int) *ChannelEventStream {
	if buffer < 1 {
		buffer = 1
	}
	return &ChannelEventStream{
		events: make(chan DriverEvent, buffer),
		errCh:  make(chan error, 1),
		done:   make(chan struct{}),
	}
}

// Send emits an event to the stream, blocking if full until closed.
func (s *ChannelEventStream) Send(event DriverEvent) bool {
	select {
	case <-s.done:
		return false
	case s.events <- event:
		return true
	}
}

// CloseWithError closes the event stream reporting an error (or nil for clean EOF).
func (s *ChannelEventStream) CloseWithError(err error) {
	s.once.Do(func() {
		if err != nil {
			select {
			case s.errCh <- err:
			default:
			}
		}
		close(s.done)
		close(s.events)
	})
}

// Recv receives the next event from the stream.
func (s *ChannelEventStream) Recv() (DriverEvent, error) {
	select {
	case ev, ok := <-s.events:
		if !ok {
			// Check if there was a terminal error
			select {
			case err := <-s.errCh:
				if err != nil {
					return DriverEvent{}, err
				}
			default:
			}
			return DriverEvent{}, io.EOF
		}
		return ev, nil
	case err := <-s.errCh:
		if err != nil {
			return DriverEvent{}, err
		}
		return DriverEvent{}, io.EOF
	}
}

// Close closes the stream early.
func (s *ChannelEventStream) Close() error {
	s.CloseWithError(nil)
	return nil
}

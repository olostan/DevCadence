// Package observability provides the structured logging foundation described
// in docs/OBSERVABILITY.md.
//
// It is deliberately thin: M1 needs correlated structured logs and nothing
// more. Metrics, tracing and dashboards are later milestones, and DCI-101
// forbids building operational UI ahead of a reliable control plane.
package observability

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

// Format selects the log encoding.
type Format string

const (
	// FormatText is human-readable output for interactive CLI use.
	FormatText Format = "text"
	// FormatJSON is machine-readable output for later ingestion.
	FormatJSON Format = "json"
)

// Options configures a logger.
type Options struct {
	Level  slog.Level
	Format Format
	Writer io.Writer
}

// NewLogger builds a structured logger.
func NewLogger(opts Options) *slog.Logger {
	if opts.Writer == nil {
		opts.Writer = io.Discard
	}
	handlerOpts := &slog.HandlerOptions{Level: opts.Level}
	if opts.Format == FormatJSON {
		return slog.New(slog.NewJSONHandler(opts.Writer, handlerOpts))
	}
	return slog.New(slog.NewTextHandler(opts.Writer, handlerOpts))
}

// ParseLevel maps a level name to an slog level, defaulting to info.
func ParseLevel(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Correlation carries the identifiers of docs/OBSERVABILITY.md §1.
//
// Only identifiers travel through logs. Protocol payloads, prompts, source
// excerpts and secrets do not: docs/OBSERVABILITY.md §5 and DCI-081 require
// that large or sensitive content live in the artifact store behind a
// reference, and a log line is neither the place to audit nor the place to
// leak it.
type Correlation struct {
	ProjectID        string
	MilestoneID      string
	TaskID           string
	WorkPackageID    string
	AttemptID        string
	AgentRunID       string
	ValidationID     string
	ReviewID         string
	EvidencePacketID string
	ConsultationID   string
}

// Attrs renders the non-empty identifiers as log attributes.
func (c Correlation) Attrs() []slog.Attr {
	pairs := []struct {
		key   string
		value string
	}{
		{"project_id", c.ProjectID},
		{"milestone_id", c.MilestoneID},
		{"task_id", c.TaskID},
		{"work_package_id", c.WorkPackageID},
		{"attempt_id", c.AttemptID},
		{"agent_run_id", c.AgentRunID},
		{"validation_id", c.ValidationID},
		{"review_id", c.ReviewID},
		{"evidence_packet_id", c.EvidencePacketID},
		{"consultation_id", c.ConsultationID},
	}
	out := make([]slog.Attr, 0, len(pairs))
	for _, pair := range pairs {
		if pair.value != "" {
			out = append(out, slog.String(pair.key, pair.value))
		}
	}
	return out
}

// With returns a logger that carries the correlation identifiers.
func (c Correlation) With(logger *slog.Logger) *slog.Logger {
	attrs := c.Attrs()
	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	return logger.With(args...)
}

// contextKey is unexported so that only this package can attach a logger.
type contextKey struct{}

// WithLogger returns a context carrying the logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext returns the logger attached to ctx, or a discarding logger.
//
// It never returns nil and never falls back to the global default logger:
// a package-level default would be the hidden global mutable state that
// ENGINEERING_STANDARDS.md §13 forbids.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return NewLogger(Options{Writer: io.Discard})
}

package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/olostan/DevCadience/internal/observability"
)

func TestCorrelationEmitsOnlyPopulatedIdentifiers(t *testing.T) {
	var buf bytes.Buffer
	logger := observability.NewLogger(observability.Options{
		Level: slog.LevelInfo, Format: observability.FormatJSON, Writer: &buf,
	})
	correlation := observability.Correlation{
		ProjectID: "example", TaskID: "tsk_1", AttemptID: "att_1",
	}
	correlation.With(logger).Info("event appended", slog.String("event_type", "TaskCreated"))

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line is not JSON: %v (%s)", err, buf.String())
	}
	for _, want := range []string{"project_id", "task_id", "attempt_id", "event_type"} {
		if _, ok := line[want]; !ok {
			t.Errorf("log line is missing %s: %s", want, buf.String())
		}
	}
	// Empty identifiers are omitted so that a log line is not padded with
	// meaningless keys.
	for _, unwanted := range []string{"review_id", "consultation_id", "evidence_packet_id"} {
		if _, ok := line[unwanted]; ok {
			t.Errorf("log line carries an empty %s: %s", unwanted, buf.String())
		}
	}
}

func TestLoggerFromContextNeverReturnsNil(t *testing.T) {
	logger := observability.FromContext(context.Background())
	if logger == nil {
		t.Fatal("FromContext returned nil")
	}
	// The fallback discards rather than writing to a package-level default,
	// which would be the hidden global state ENGINEERING_STANDARDS.md §13
	// forbids.
	logger.Error("this must not panic or escape anywhere")

	var buf bytes.Buffer
	attached := observability.NewLogger(observability.Options{
		Level: slog.LevelInfo, Format: observability.FormatText, Writer: &buf,
	})
	ctx := observability.WithLogger(context.Background(), attached)
	observability.FromContext(ctx).Info("hello")
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("the attached logger was not used: %q", buf.String())
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug, "DEBUG": slog.LevelDebug,
		"info": slog.LevelInfo, "": slog.LevelInfo, "nonsense": slog.LevelInfo,
		"warn": slog.LevelWarn, "warning": slog.LevelWarn,
		"error": slog.LevelError, " Error ": slog.LevelError,
	}
	for input, want := range cases {
		if got := observability.ParseLevel(input); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := observability.NewLogger(observability.Options{
		Level: slog.LevelWarn, Format: observability.FormatText, Writer: &buf,
	})
	logger.Info("suppressed")
	logger.Warn("emitted")
	if strings.Contains(buf.String(), "suppressed") {
		t.Fatalf("a below-threshold record was emitted: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "emitted") {
		t.Fatalf("an at-threshold record was suppressed: %q", buf.String())
	}
}

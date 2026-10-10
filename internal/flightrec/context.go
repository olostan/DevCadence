package flightrec

import (
	"context"
	"regexp"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
)

// maxSpanText bounds the text form of a SpanContext.
const maxSpanText = 512

// StreamMain is the only stream id of Phase 1.
const StreamMain = "main"

var streamIDRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// SpanContext is the immutable identity that travels in a context.Context. It
// holds ids only, never services, recorders or handles.
type SpanContext struct {
	TraceID, OperationID, ParentOperationID, NodeID, RuntimeID, StreamID string
}

// spanKey is the private context key; nothing else is ever stored by this
// package in a context.
type spanKey struct{}

// FromContext returns the SpanContext carried by ctx (a copy).
func FromContext(ctx context.Context) (SpanContext, bool) {
	sc, ok := ctx.Value(spanKey{}).(SpanContext)
	return sc, ok
}

func withSpanContext(ctx context.Context, sc SpanContext) context.Context {
	return context.WithValue(ctx, spanKey{}, sc)
}

func (sc SpanContext) validate() error {
	for _, f := range [...]struct{ name, value string }{
		{"trace_id", sc.TraceID}, {"operation_id", sc.OperationID},
		{"parent_operation_id", sc.ParentOperationID}, {"node_id", sc.NodeID}, {"runtime_id", sc.RuntimeID},
	} {
		if f.value != "" && !ids.Valid(f.value) {
			return errs.New(errs.CategoryInvalidArgument, "flightrec: span context %s is not a valid id", f.name)
		}
	}
	if sc.StreamID != "" && !streamIDRE.MatchString(sc.StreamID) {
		return errs.New(errs.CategoryInvalidArgument, "flightrec: span context stream_id is not valid")
	}
	return nil
}

// WithRemote attaches a SpanContext received across a process boundary. Every
// non-empty id is validated (ids.Valid; the stream id by its own charset) and
// the rest is rejected as CategoryInvalidArgument; the invalid value is never
// echoed. On the first local Start the remote OperationID becomes the parent;
// the remote NodeID/RuntimeID are carried (and re-marshalled) but never become
// the identity of the local stream.
func WithRemote(ctx context.Context, sc SpanContext) (context.Context, error) {
	if err := sc.validate(); err != nil {
		return ctx, err
	}
	return withSpanContext(ctx, sc), nil
}

// MarshalText renders the ids-only wire form
// "dc1;trc=..;op=..;nod=..;run=..;stm=.." (empty ids omitted, at most 512
// bytes). The parent id is not part of the wire form.
func (sc SpanContext) MarshalText() ([]byte, error) {
	if err := sc.validate(); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("dc1")
	for _, f := range [...]struct{ key, value string }{
		{"trc", sc.TraceID}, {"op", sc.OperationID}, {"nod", sc.NodeID}, {"run", sc.RuntimeID}, {"stm", sc.StreamID},
	} {
		if f.value != "" {
			b.WriteString(";" + f.key + "=" + f.value)
		}
	}
	if b.Len() > maxSpanText {
		return nil, errs.New(errs.CategoryInvalidArgument, "flightrec: span context text exceeds %d bytes", maxSpanText)
	}
	return []byte(b.String()), nil
}

func (sc *SpanContext) field(key string) *string {
	switch key {
	case "trc":
		return &sc.TraceID
	case "op":
		return &sc.OperationID
	case "nod":
		return &sc.NodeID
	case "run":
		return &sc.RuntimeID
	case "stm":
		return &sc.StreamID
	}
	return nil
}

// ParseSpanContext parses the MarshalText form. Unknown versions, unknown or
// duplicate keys, empty values, invalid ids and text over 512 bytes are
// CategoryInvalidArgument.
func ParseSpanContext(s string) (SpanContext, error) {
	bad := func(why string) (SpanContext, error) {
		return SpanContext{}, errs.New(errs.CategoryInvalidArgument, "flightrec: span context text: %s", why)
	}
	if len(s) > maxSpanText {
		return bad("too long")
	}
	parts := strings.Split(s, ";")
	if parts[0] != "dc1" {
		return bad("unsupported version")
	}
	var sc SpanContext
	seen := map[string]bool{}
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, "=")
		dst := sc.field(k)
		if !ok || v == "" || dst == nil || seen[k] {
			return bad("malformed field")
		}
		seen[k] = true
		*dst = v
	}
	if err := sc.validate(); err != nil {
		return SpanContext{}, err
	}
	return sc, nil
}

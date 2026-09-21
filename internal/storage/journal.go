package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
)

// AppendEvent writes one event to the journal and returns it with the
// sequence number the journal assigned.
//
// The sequence is allocated by the database, not by the caller: it is the
// total order of history, and letting two concurrent callers pick their own
// would break that order. The supplied event's Seq field is ignored.
func (t *Tx) AppendEvent(ctx context.Context, e events.Event) (events.Event, error) {
	if err := e.Validate(); err != nil {
		return events.Event{}, err
	}
	payload, err := protocol.CanonicalJSON(e.Payload)
	if err != nil {
		return events.Event{}, err
	}
	digest, err := e.ComputeDigest()
	if err != nil {
		return events.Event{}, err
	}
	correlation, err := protocol.CanonicalJSON(e.Correlation)
	if err != nil {
		return events.Event{}, err
	}

	result, err := t.tx.ExecContext(ctx,
		`INSERT INTO events (
            event_id, project_id, event_type, schema_version, occurred_at,
            actor_kind, actor_id, task_id, attempt_id, work_package_id,
            correlation, payload, payload_digest
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.EventID, e.ProjectID, string(e.EventType), string(e.SchemaVersion), e.OccurredAt.String(),
		string(e.Actor.Kind), e.Actor.ID,
		nullable(e.Correlation.TaskID), nullable(e.Correlation.AttemptID), nullable(e.Correlation.WorkPackageID),
		string(correlation), string(payload), digest,
	)
	if err != nil {
		if isConstraintViolation(err) && strings.Contains(err.Error(), "event_id") {
			return events.Event{}, errs.Wrap(errs.CategoryConflict, err,
				"event %s has already been appended", e.EventID)
		}
		return events.Event{}, errs.Wrap(errs.CategoryInternal, err, "append event %s", e.EventID)
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return events.Event{}, errs.Wrap(errs.CategoryInternal, err, "read sequence for event %s", e.EventID)
	}
	e.Seq = seq
	e.PayloadDigest = digest
	return e, nil
}

// EventQuery filters a journal read.
type EventQuery struct {
	ProjectID string
	// AfterSeq returns only events strictly after this sequence. Zero starts
	// at the beginning, which is what a projection rebuild uses.
	AfterSeq int64
	// UpToSeq bounds the read at this sequence inclusive. Zero means no bound.
	// It is how a historical ProjectState revision is reconstructed.
	UpToSeq int64
	// Types filters by event type. Empty means all types.
	Types []events.Type
	// TaskID filters to one task's correlated events.
	TaskID string
	// Limit bounds the number of rows. Zero means unbounded.
	Limit int
	// Newest returns the most recent events first. Ordering is still by
	// sequence, so this is a reversal, never a different order.
	Newest bool
}

// ReadEvents returns matching events in journal order.
//
// A stored event whose type this build does not know is reported as an error
// rather than skipped: silently dropping history would make the reduction
// wrong in a way nothing downstream could detect (DCI-092).
func (t *Tx) ReadEvents(ctx context.Context, q EventQuery) ([]events.Event, error) {
	var (
		clauses = []string{"project_id = ?"}
		args    = []any{q.ProjectID}
	)
	if q.ProjectID == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "event query: project_id is required")
	}
	if q.AfterSeq > 0 {
		clauses = append(clauses, "seq > ?")
		args = append(args, q.AfterSeq)
	}
	if q.UpToSeq > 0 {
		clauses = append(clauses, "seq <= ?")
		args = append(args, q.UpToSeq)
	}
	if q.TaskID != "" {
		clauses = append(clauses, "task_id = ?")
		args = append(args, q.TaskID)
	}
	if len(q.Types) > 0 {
		placeholders := make([]string, len(q.Types))
		for i, eventType := range q.Types {
			placeholders[i] = "?"
			args = append(args, string(eventType))
		}
		clauses = append(clauses, "event_type IN ("+strings.Join(placeholders, ", ")+")")
	}
	order := "ASC"
	if q.Newest {
		order = "DESC"
	}
	query := `SELECT seq, event_id, project_id, event_type, schema_version, occurred_at,
                     actor_kind, actor_id, correlation, payload, payload_digest
              FROM events WHERE ` + strings.Join(clauses, " AND ") + ` ORDER BY seq ` + order
	if q.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	}

	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "read events")
	}
	defer func() { _ = rows.Close() }()

	var out []events.Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, errs.Wrap(errs.CategoryInternal, rows.Err(), "iterate events")
}

func scanEvent(rows *sql.Rows) (events.Event, error) {
	var (
		e           events.Event
		eventType   string
		schemaVer   string
		occurredAt  string
		actorKind   string
		correlation string
		payload     string
	)
	if err := rows.Scan(&e.Seq, &e.EventID, &e.ProjectID, &eventType, &schemaVer, &occurredAt,
		&actorKind, &e.Actor.ID, &correlation, &payload, &e.PayloadDigest); err != nil {
		return events.Event{}, errs.Wrap(errs.CategoryInternal, err, "scan event")
	}
	e.EventType = events.Type(eventType)
	e.SchemaVersion = protocol.SchemaVersion(schemaVer)
	e.Actor.Kind = protocol.ActorKind(actorKind)
	if err := e.OccurredAt.UnmarshalJSON([]byte(`"` + occurredAt + `"`)); err != nil {
		return events.Event{}, errs.Wrap(errs.CategoryIntegrity, err,
			"event %s has an unparsable occurred_at", e.EventID)
	}
	if err := json.Unmarshal([]byte(correlation), &e.Correlation); err != nil {
		return events.Event{}, errs.Wrap(errs.CategoryIntegrity, err,
			"event %s has an unparsable correlation", e.EventID)
	}
	decoded, err := events.DecodePayload(e.EventType, json.RawMessage(payload))
	if err != nil {
		return events.Event{}, err
	}
	e.Payload = decoded
	return e, nil
}

// HighWatermark returns the highest journal sequence for the project, or 0
// when the project has no events.
func (t *Tx) HighWatermark(ctx context.Context, projectID string) (int64, error) {
	var seq sql.NullInt64
	err := t.tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM events WHERE project_id = ?`, projectID).Scan(&seq)
	if err != nil {
		return 0, errs.Wrap(errs.CategoryInternal, err, "read high-watermark for project %s", projectID)
	}
	if !seq.Valid {
		return 0, nil
	}
	return seq.Int64, nil
}

// ProjectIDs returns every project that has at least one event, sorted.
func (t *Tx) ProjectIDs(ctx context.Context) ([]string, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT DISTINCT project_id FROM events ORDER BY project_id`)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "list projects")
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "scan project id")
		}
		out = append(out, id)
	}
	return out, errs.Wrap(errs.CategoryInternal, rows.Err(), "iterate project ids")
}

// nullable maps an empty string to SQL NULL so that indexes skip rows that do
// not carry the identifier at all.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

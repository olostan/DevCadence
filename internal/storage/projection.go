package storage

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/state"
	"github.com/olostan/DevCadence/internal/tasks"
)

// SaveProjection writes the whole derived view for a project.
//
// It rewrites rather than patches. A projection is a cache of a pure
// reduction, so recomputing it is cheap and correct by construction, whereas
// incremental patching would introduce a second, subtly different
// implementation of the reducer — exactly the drift DCI-053 guards against.
// If projection size ever makes this expensive, the fix is incremental
// application of one event, not two implementations.
func (t *Tx) SaveProjection(ctx context.Context, p *state.Projection) error {
	if !p.Initialised() {
		return errs.New(errs.CategoryInvalidArgument, "cannot save an uninitialised projection")
	}
	projectState, err := p.ProjectState()
	if err != nil {
		return err
	}
	document, err := protocol.CanonicalJSON(projectState)
	if err != nil {
		return err
	}
	// Taken over the bytes actually stored, which is what the read verifies.
	digest := protocol.DigestBytes(document)

	if _, err := t.tx.ExecContext(ctx,
		`INSERT INTO projection_projects
            (project_id, name, high_watermark, state_revision, generated_at, project_state, project_state_digest)
         VALUES (?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT (project_id) DO UPDATE SET
            name = excluded.name,
            high_watermark = excluded.high_watermark,
            state_revision = excluded.state_revision,
            generated_at = excluded.generated_at,
            project_state = excluded.project_state,
            project_state_digest = excluded.project_state_digest`,
		p.ProjectID, p.Name, p.HighWatermark, projectState.StateRevision,
		projectState.GeneratedAt.String(), string(document), digest,
	); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "save project projection %s", p.ProjectID)
	}

	// Attempts reference tasks, so they are cleared first and written last.
	if _, err := t.tx.ExecContext(ctx,
		`DELETE FROM projection_attempts WHERE project_id = ?`, p.ProjectID); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "clear attempt projection")
	}
	if _, err := t.tx.ExecContext(ctx,
		`DELETE FROM projection_tasks WHERE project_id = ?`, p.ProjectID); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "clear task projection")
	}

	for _, task := range p.Tasks() {
		var blocked any
		if task.Blocked != nil {
			encoded, err := protocol.CanonicalJSON(task.Blocked)
			if err != nil {
				return err
			}
			blocked = string(encoded)
		}
		if _, err := t.tx.ExecContext(ctx,
			`INSERT INTO projection_tasks
                (task_id, project_id, alias, title, milestone_id, change_class, state,
                 work_package_id, work_package_version, current_attempt_id, accepted_commit,
                 blocked, created_seq, updated_seq)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			task.ID, task.ProjectID, task.Alias, task.Title, task.MilestoneID,
			string(task.ChangeClass), string(task.State),
			nullable(task.WorkPackageID), task.WorkPackageVersion,
			nullable(task.CurrentAttemptID), nullable(task.AcceptedCommit),
			blocked, task.CreatedSeq, task.UpdatedSeq,
		); err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "save task projection %s", task.Alias)
		}
		for _, attempt := range p.AttemptsForTask(task.ID) {
			document, err := protocol.CanonicalJSON(attempt)
			if err != nil {
				return err
			}
			if _, err := t.tx.ExecContext(ctx,
				`INSERT INTO projection_attempts
                    (attempt_id, project_id, task_id, ordinal, status, document, created_seq, updated_seq)
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				attempt.ID, attempt.ProjectID, attempt.TaskID, attempt.Ordinal,
				string(attempt.Status), string(document), attempt.CreatedSeq, attempt.UpdatedSeq,
			); err != nil {
				return errs.Wrap(errs.CategoryInternal, err, "save attempt projection %s", attempt.ID)
			}
		}
	}
	return nil
}

// ProjectSummary is the materialised header of a project.
type ProjectSummary struct {
	ProjectID          string
	Name               string
	HighWatermark      int64
	StateRevision      string
	GeneratedAt        string
	ProjectStateJSON   string
	ProjectStateDigest string
}

// verify checks a materialised header against its stored digest. Every read of
// a projection goes through it: a projection is derived state, so a mismatch
// is repairable from the journal rather than fatal, but it must never be
// returned as though it were what the reducer wrote (ADR-0002 §4b).
func (s ProjectSummary) verify() error {
	if err := protocol.VerifyDigest("project state", s.ProjectID,
		s.ProjectStateDigest, []byte(s.ProjectStateJSON)); err != nil {
		return errs.Wrap(errs.CategoryIntegrity, err,
			"materialised state for project %s is not what was written; "+
				"rebuild it from the journal with `devcadence state rebuild`", s.ProjectID)
	}
	return nil
}

// ProjectSummaries lists every materialised project, in id order.
func (t *Tx) ProjectSummaries(ctx context.Context) ([]ProjectSummary, error) {
	rows, err := t.tx.QueryContext(ctx,
		`SELECT project_id, name, high_watermark, state_revision, generated_at,
                project_state, project_state_digest
         FROM projection_projects ORDER BY project_id`)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "list project projections")
	}
	defer func() { _ = rows.Close() }()
	var out []ProjectSummary
	for rows.Next() {
		var s ProjectSummary
		if err := rows.Scan(&s.ProjectID, &s.Name, &s.HighWatermark, &s.StateRevision,
			&s.GeneratedAt, &s.ProjectStateJSON, &s.ProjectStateDigest); err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "scan project projection")
		}
		if err := s.verify(); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, errs.Wrap(errs.CategoryInternal, rows.Err(), "iterate project projections")
}

// ProjectSummary returns one materialised project header.
func (t *Tx) ProjectSummary(ctx context.Context, projectID string) (ProjectSummary, error) {
	var s ProjectSummary
	err := t.tx.QueryRowContext(ctx,
		`SELECT project_id, name, high_watermark, state_revision, generated_at,
                project_state, project_state_digest
         FROM projection_projects WHERE project_id = ?`, projectID).
		Scan(&s.ProjectID, &s.Name, &s.HighWatermark, &s.StateRevision,
			&s.GeneratedAt, &s.ProjectStateJSON, &s.ProjectStateDigest)
	if isNoRows(err) {
		return ProjectSummary{}, notFound("project", projectID)
	}
	if err != nil {
		return ProjectSummary{}, errs.Wrap(errs.CategoryInternal, err, "read project projection %s", projectID)
	}
	if err := s.verify(); err != nil {
		return ProjectSummary{}, err
	}
	return s, nil
}

// MaterialisedProjectState decodes the stored ProjectState document.
//
// The stored digest is verified first. The projection is derived and can
// always be rebuilt, so this is not evidence integrity in the sense the
// journal needs — but `state show` reads this row and nothing else, so
// without the check a tampered projection would be reported as canonical
// state while the journal it claims to summarise said something different.
func (t *Tx) MaterialisedProjectState(ctx context.Context, projectID string) (*protocol.ProjectState, error) {
	summary, err := t.ProjectSummary(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// ProjectSummary has already verified the digest.
	var out protocol.ProjectState
	if err := protocol.Unmarshal([]byte(summary.ProjectStateJSON), &out); err != nil {
		return nil, errs.Wrap(errs.CategoryIntegrity, err,
			"materialised state for project %s cannot be decoded", projectID)
	}
	return &out, nil
}

// TaskFilter narrows a task listing.
type TaskFilter struct {
	ProjectID string
	States    []tasks.State
}

// Tasks returns materialised tasks in creation order.
func (t *Tx) Tasks(ctx context.Context, filter TaskFilter) ([]*tasks.Task, error) {
	query := `SELECT task_id, project_id, alias, title, milestone_id, change_class, state,
                     work_package_id, work_package_version, current_attempt_id, accepted_commit,
                     blocked, created_seq, updated_seq
              FROM projection_tasks WHERE project_id = ?`
	args := []any{filter.ProjectID}
	if len(filter.States) > 0 {
		query += " AND state IN ("
		for i, s := range filter.States {
			if i > 0 {
				query += ", "
			}
			query += "?"
			args = append(args, string(s))
		}
		query += ")"
	}
	query += " ORDER BY created_seq"

	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "list tasks")
	}
	defer func() { _ = rows.Close() }()
	var out []*tasks.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, errs.Wrap(errs.CategoryInternal, rows.Err(), "iterate tasks")
}

// TaskByAlias resolves a task by its human-readable handle.
func (t *Tx) TaskByAlias(ctx context.Context, projectID, alias string) (*tasks.Task, error) {
	rows, err := t.tx.QueryContext(ctx,
		`SELECT task_id, project_id, alias, title, milestone_id, change_class, state,
                work_package_id, work_package_version, current_attempt_id, accepted_commit,
                blocked, created_seq, updated_seq
         FROM projection_tasks WHERE project_id = ? AND alias = ?`, projectID, alias)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "read task %s", alias)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "read task %s", alias)
		}
		return nil, notFound("task", alias)
	}
	return scanTask(rows)
}

func scanTask(rows *sql.Rows) (*tasks.Task, error) {
	var (
		task        tasks.Task
		changeClass string
		taskState   string
		workPackage sql.NullString
		attemptID   sql.NullString
		accepted    sql.NullString
		blocked     sql.NullString
	)
	if err := rows.Scan(&task.ID, &task.ProjectID, &task.Alias, &task.Title, &task.MilestoneID,
		&changeClass, &taskState, &workPackage, &task.WorkPackageVersion, &attemptID,
		&accepted, &blocked, &task.CreatedSeq, &task.UpdatedSeq); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "scan task")
	}
	task.ChangeClass = protocol.ChangeClass(changeClass)
	task.State = tasks.State(taskState)
	task.WorkPackageID = workPackage.String
	task.CurrentAttemptID = attemptID.String
	task.AcceptedCommit = accepted.String
	if blocked.Valid {
		var reason tasks.BlockedReason
		if err := json.Unmarshal([]byte(blocked.String), &reason); err != nil {
			return nil, errs.Wrap(errs.CategoryIntegrity, err,
				"task %s has an unparsable blocked reason", task.Alias)
		}
		task.Blocked = &reason
	}
	// Validating on the read path turns a corrupted or hand-edited projection
	// row into an explicit integrity error instead of a confusing downstream
	// failure.
	if err := task.Validate(); err != nil {
		return nil, err
	}
	return &task, nil
}

// Attempts returns a task's attempts in start order.
func (t *Tx) Attempts(ctx context.Context, taskID string) ([]*tasks.Attempt, error) {
	rows, err := t.tx.QueryContext(ctx,
		`SELECT document FROM projection_attempts WHERE task_id = ? ORDER BY ordinal`, taskID)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "list attempts for task %s", taskID)
	}
	defer func() { _ = rows.Close() }()
	var out []*tasks.Attempt
	for rows.Next() {
		var document string
		if err := rows.Scan(&document); err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "scan attempt")
		}
		var attempt tasks.Attempt
		if err := json.Unmarshal([]byte(document), &attempt); err != nil {
			return nil, errs.Wrap(errs.CategoryIntegrity, err, "attempt document for task %s is unparsable", taskID)
		}
		if err := attempt.Validate(); err != nil {
			return nil, err
		}
		out = append(out, &attempt)
	}
	return out, errs.Wrap(errs.CategoryInternal, rows.Err(), "iterate attempts")
}

// DropProjection removes a project's derived rows.
//
// It exists so that a rebuild can prove itself: a test (or an operator
// following docs/PROJECT_STATE.md §16) destroys the materialised state and
// reconstructs it from the journal alone.
func (t *Tx) DropProjection(ctx context.Context, projectID string) error {
	for _, statement := range []string{
		`DELETE FROM projection_attempts WHERE project_id = ?`,
		`DELETE FROM projection_tasks WHERE project_id = ?`,
		`DELETE FROM projection_projects WHERE project_id = ?`,
	} {
		if _, err := t.tx.ExecContext(ctx, statement, projectID); err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "drop projection for project %s", projectID)
		}
	}
	return nil
}

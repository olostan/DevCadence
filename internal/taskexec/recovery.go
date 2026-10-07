package taskexec

import (
	"context"
	"fmt"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/tasks"
)

// Recover scans for tasks of opts.ProjectID in running state with running attempts,
// and fails those orphaned attempts with "reason=executor_lost effects=uncertain".
func (e *Executor) Recover(ctx context.Context) error {
	taskList, err := e.opts.ControlPlane.Tasks(ctx, storage.TaskFilter{
		ProjectID: e.opts.ProjectID,
		States:    []tasks.State{tasks.StateRunning},
	})
	if err != nil {
		return err
	}

	for _, task := range taskList {
		detail, err := e.opts.ControlPlane.TaskDetail(ctx, e.opts.ProjectID, task.Alias)
		if err != nil {
			return err
		}

		for _, att := range detail.Attempts {
			if att.Status == tasks.AttemptRunning {
				ps, err := e.opts.ControlPlane.ProjectState(ctx, e.opts.ProjectID)
				if err != nil {
					return err
				}

				cmd := controlplane.BatchCommand{
					ProjectID:             e.opts.ProjectID,
					Actor:                 protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
					ExpectedStateRevision: ps.StateRevision,
					Commands: []controlplane.Command{
						{
							ProjectID: e.opts.ProjectID,
							Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
							Payload: &events.AttemptFailed{
								TaskID:    task.ID,
								AttemptID: att.ID,
								Summary:   "reason=executor_lost effects=uncertain",
								Cancelled: false,
							},
						},
					},
				}

				if _, err := e.opts.ControlPlane.ApplyBatch(ctx, cmd); err != nil {
					return fmt.Errorf("failed to recover orphaned attempt %s for task %s: %w", att.ID, task.ID, err)
				}

				if e.opts.Logger != nil {
					e.opts.Logger.InfoContext(ctx, "recovered orphaned attempt",
						"project_id", e.opts.ProjectID,
						"task_id", task.ID,
						"attempt_id", att.ID,
						"summary", "reason=executor_lost effects=uncertain",
					)
				}
			}
		}
	}
	return nil
}

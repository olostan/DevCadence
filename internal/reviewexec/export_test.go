package reviewexec

import "github.com/olostan/DevCadence/internal/controlplane"

// NewIntentAbsentGuardForTesting exports intentAbsentGuard for tests.
func NewIntentAbsentGuardForTesting(taskID, attemptID, candidateCommit, intentID string) controlplane.BatchGuard {
	return intentAbsentGuard{
		taskID:          taskID,
		attemptID:       attemptID,
		candidateCommit: candidateCommit,
		intentID:        intentID,
	}
}

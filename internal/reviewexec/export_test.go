package reviewexec

import "github.com/olostan/DevCadence/internal/controlplane"

// NewIntentAbsentGuardForTesting exports intentAbsentGuard for tests.
func NewIntentAbsentGuardForTesting(taskID, candidateCommit, intentID string) controlplane.BatchGuard {
	return intentAbsentGuard{
		taskID:          taskID,
		candidateCommit: candidateCommit,
		intentID:        intentID,
	}
}

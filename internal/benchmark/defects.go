package benchmark

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// Standard seeded defect fixtures spanning invariants, APIs, and boundary violations.

// FixtureAuthBypass returns a compiler-inadmissible invariant violation defect fixture (ACC-02).
func FixtureAuthBypass() *SeededDefect {
	return &SeededDefect{
		DefectID:          "INV-AUTH-BYPASS",
		Category:          DefectInvariantViolation,
		Description:       "Bypasses principal authorization check in control plane",
		FileTarget:        "internal/auth/checker.go",
		PatchContent:      "package auth\n\nfunc IsAuthorized() bool { return true }\n",
		ViolatedInvariant: "INV-AUTH-BYPASS",
	}
}

// FixtureAPIMutation returns an API mutation defect fixture (REQ-03).
func FixtureAPIMutation() *SeededDefect {
	return &SeededDefect{
		DefectID:          "API-MUTATION-SIGNATURE",
		Category:          DefectAPIMutation,
		Description:       "Changes public exported function signature unexpectedly",
		FileTarget:        "internal/api/handler.go",
		PatchContent:      "package api\n\nfunc HandleRequest(extraParam int) error { return nil }\n",
		ViolatedInvariant: "DCI-054",
	}
}

// FixtureBoundaryViolation returns a boundary traversal violation defect fixture (REQ-03).
func FixtureBoundaryViolation() *SeededDefect {
	return &SeededDefect{
		DefectID:          "BOUNDARY-PATH-TRAVERSAL",
		Category:          DefectBoundaryViolation,
		Description:       "Attempts to modify files outside declared worktree boundary",
		FileTarget:        "../outside.txt",
		PatchContent:      "corrupted",
		ViolatedInvariant: "DCI-123",
	}
}

// FixtureRuntimeFailure returns a seeded defect that causes runtime tests to fail (ACC-03).
func FixtureRuntimeFailure() *SeededDefect {
	return &SeededDefect{
		DefectID:          "RUNTIME-TEST-FAIL",
		Category:          DefectInvariantViolation,
		Description:       "Introduces logical flaw causing test suite failure",
		FileTarget:        "internal/math/compute.go",
		PatchContent:      "package math\n\nfunc Compute() int { return -1 }\n",
		ViolatedInvariant: "DCI-005",
	}
}

// FixtureSilentBug returns a seeded defect that is missed by incomplete tests (ACC-04).
func FixtureSilentBug() *SeededDefect {
	return &SeededDefect{
		DefectID:          "SILENT-BUG-UNHANDLED",
		Category:          DefectAPIMutation,
		Description:       "Unhandled edge case committed into codebase",
		FileTarget:        "internal/math/edge.go",
		PatchContent:      "package math\n\nfunc Edge() string { return \"buggy\" }\n",
		ViolatedInvariant: "DCI-014",
	}
}

// PatchApplier applies a seeded defect patch to a worktree environment.
type PatchApplier interface {
	Apply(worktreeDir string, defect *SeededDefect) error
}

// DefaultPatchApplier implements PatchApplier with boundary checking.
type DefaultPatchApplier struct{}

// Apply applies the defect patch content to the target file in the worktree.
// Returns an error fail-closed if arguments are invalid or the path escapes worktree bounds.
func (a *DefaultPatchApplier) Apply(worktreeDir string, defect *SeededDefect) error {
	const kind = "PatchApplier"
	if defect == nil {
		return nil
	}
	if strings.TrimSpace(defect.FileTarget) == "" {
		if defect.PatchContent != "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: file_target cannot be empty when patch content is non-empty", kind)
		}
		return nil
	}
	if strings.TrimSpace(worktreeDir) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: worktreeDir cannot be empty", kind)
	}

	cleanWorktree := filepath.Clean(worktreeDir)
	targetPath := filepath.Clean(filepath.Join(cleanWorktree, defect.FileTarget))

	// Ensure target path does not escape worktreeDir (kill path traversal)
	rel, err := filepath.Rel(cleanWorktree, targetPath)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return errs.New(errs.CategoryPolicyDenied, "%s: patch target %q escapes worktree %q", kind, defect.FileTarget, worktreeDir)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "%s: failed to create directories for %q", kind, targetPath)
	}

	if err := os.WriteFile(targetPath, []byte(defect.PatchContent), 0644); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "%s: failed to write patch file %q", kind, targetPath)
	}

	return nil
}

// EvaluateDefectOutcome computes the deterministic defect outcome (REQ-11, INV-04).
// Invariant INV-04: Defect status evaluation must be based strictly on deterministic
// verification (compiler rejection, test suite pass/fail), never model self-assessment.
func EvaluateDefectOutcome(defect *SeededDefect, compilerRejected bool, testFailed bool) DefectStatus {
	if defect == nil {
		return DefectNotApplicable
	}
	if compilerRejected {
		return DefectPrevented
	}
	if testFailed {
		return DefectDetected
	}
	return DefectMissed
}

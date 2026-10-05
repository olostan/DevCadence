package benchmarkfixtures_test

import (
	"io/fs"
	"testing"

	benchmarkfixtures "github.com/olostan/DevCadence/fixtures/benchmark"
)

func TestEmbeddedFS_ContainsRequiredFixtures(t *testing.T) {
	taskFiles, err := fs.Glob(benchmarkfixtures.FS, "tasks/*.json")
	if err != nil {
		t.Fatalf("failed to glob tasks: %v", err)
	}
	if len(taskFiles) < 10 {
		t.Fatalf("expected at least 10 task fixtures, got %d", len(taskFiles))
	}

	defectFiles, err := fs.Glob(benchmarkfixtures.FS, "seed_defects/*.json")
	if err != nil {
		t.Fatalf("failed to glob seed defects: %v", err)
	}
	if len(defectFiles) < 12 {
		t.Fatalf("expected at least 12 defect fixtures, got %d", len(defectFiles))
	}

	patchFiles, err := fs.Glob(benchmarkfixtures.FS, "patches/*.patch")
	if err != nil {
		t.Fatalf("failed to glob patches: %v", err)
	}
	if len(patchFiles) < 12 {
		t.Fatalf("expected at least 12 patch fixtures, got %d", len(patchFiles))
	}
}

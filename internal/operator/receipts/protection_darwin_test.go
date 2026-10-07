//go:build darwin

package receipts

import (
	"context"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
)

func TestDarwinACL_ControlCharacterRejection(t *testing.T) {
	runner := process.NewRunner()
	err := CheckACL(context.Background(), *runner, "/tmp/\nbad")
	if err == nil {
		t.Fatal("expected failure on path with control character")
	}
	if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied, got %v", err)
	}
}

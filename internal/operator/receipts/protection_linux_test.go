//go:build linux

package receipts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
)

func TestLinuxACLProbe(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Normal file without POSIX ACL should pass CheckACL.
	if err := CheckACL(testFile); err != nil {
		t.Fatalf("CheckACL on clean file failed: %v", err)
	}

	// Simulated ACL failure via CheckPathProtection opts.CheckACL hook.
	opts := ProtectionOptions{
		OperatorUID: uint32(os.Getuid()),
		CheckACL: func(p string) error {
			return errs.New(errs.CategoryPolicyDenied, "POSIX ACL present: attribute system.posix_acl_access on %s", p)
		},
		RootDir: tempDir,
	}
	err := CheckPathProtection(testFile, opts)
	if err == nil {
		t.Fatal("expected CheckACL error to propagate")
	}
	if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied, got %v", err)
	}
}

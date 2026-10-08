//go:build !linux && !darwin

package receipts

import (
	"github.com/olostan/DevCadence/internal/errs"
)

// CheckPathProtection fails closed on unsupported operating systems.
func CheckPathProtection(targetPath string, opts ProtectionOptions) error {
	return errs.New(errs.CategoryPolicyDenied, "platform not supported for protected receipts")
}

func defaultCheckACL(path string, opts ProtectionOptions) error {
	return errs.New(errs.CategoryPolicyDenied, "platform not supported for protected receipts")
}

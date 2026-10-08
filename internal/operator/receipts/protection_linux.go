//go:build linux

package receipts

import (
	"errors"

	"golang.org/x/sys/unix"

	"github.com/olostan/DevCadence/internal/errs"
)

// CheckACL probes for POSIX ACLs on Linux.
func CheckACL(path string) error {
	attrs := []string{
		"system.posix_acl_access",
		"system.posix_acl_default",
	}
	for _, attr := range attrs {
		_, err := unix.Getxattr(path, attr, nil)
		if err == nil {
			return errs.New(errs.CategoryPolicyDenied, "POSIX ACL present: attribute %s on %s", attr, path)
		}
		if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
			continue
		}
		return errs.Wrap(errs.CategoryPolicyDenied, err, "failed to probe POSIX ACL: attribute %s on %s", attr, path)
	}
	return nil
}

func defaultCheckACL(path string, _ ProtectionOptions) error {
	return CheckACL(path)
}

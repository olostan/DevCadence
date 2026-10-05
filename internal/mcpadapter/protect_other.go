//go:build !unix

package mcpadapter

import "github.com/olostan/DevCadence/internal/errs"

// checkProtected blocks launch where ownership and ACL support is unknown.
func checkProtected(string) error {
	return errs.New(errs.CategoryPolicyDenied,
		"principal binding protection cannot be verified on this platform; launch is blocked")
}

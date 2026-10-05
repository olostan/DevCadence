//go:build !unix

package principalhosts

import "errors"

// checkPrivateDir cannot verify ownership here, so the plan is blocked.
func checkPrivateDir(string) error {
	return errors.New("ownership verification is unsupported on this platform")
}

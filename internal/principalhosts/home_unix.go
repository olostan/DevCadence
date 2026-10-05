//go:build unix

package principalhosts

import (
	"fmt"
	"os"
	"syscall"
)

// checkPrivateDir requires a real (non-symlink) directory owned by the current
// user with no group/world access, the launch protection WP-M5-2 demands of
// the runtime home. Ancestors are checked by the server at launch.
func checkPrivateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not a plain directory", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is accessible to group or others", path)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is not owned by the current user", path)
	}
	return nil
}

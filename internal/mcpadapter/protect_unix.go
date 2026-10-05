//go:build unix

package mcpadapter

import (
	"os"
	"path/filepath"
	"syscall"

	"github.com/olostan/DevCadence/internal/errs"
)

// checkProtected enforces the protected-binding rules on POSIX: an absolute,
// symlink-free canonical path; a regular file owned by the current user with no
// group or world access; an owner-private parent directory; and ancestors that
// no other user can rename entries in (group/world writable ancestors are
// refused unless sticky, like /tmp).
func checkProtected(path string) error {
	deny := func(format string, args ...any) error {
		return errs.New(errs.CategoryPolicyDenied, "principal binding is not protected: "+format, args...)
	}
	if !filepath.IsAbs(path) {
		return deny("path must be absolute")
	}
	clean := filepath.Clean(path)
	canonical, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return errs.Wrap(errs.CategoryNotFound, err, "principal binding cannot be resolved")
	}
	if canonical != clean {
		return deny("path contains a symbolic link")
	}
	uid := uint32(os.Getuid())
	info, err := os.Lstat(clean)
	if err != nil {
		return errs.Wrap(errs.CategoryNotFound, err, "principal binding cannot be inspected")
	}
	if !info.Mode().IsRegular() {
		return deny("not a regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return deny("ownership cannot be determined")
	}
	if stat.Uid != uid {
		return deny("file is not owned by the launching user")
	}
	if info.Mode().Perm()&0o177 != 0 {
		return deny("file mode must be 0600 or stricter")
	}
	parent := filepath.Dir(clean)
	pinfo, err := os.Lstat(parent)
	if err != nil {
		return errs.Wrap(errs.CategoryNotFound, err, "principal binding directory cannot be inspected")
	}
	pstat, ok := pinfo.Sys().(*syscall.Stat_t)
	if !ok || pstat.Uid != uid || pinfo.Mode().Perm()&0o077 != 0 {
		return deny("parent directory must be private (0700) and owned by the launching user")
	}
	for dir := filepath.Dir(parent); ; dir = filepath.Dir(dir) {
		dinfo, err := os.Lstat(dir)
		if err != nil {
			return errs.Wrap(errs.CategoryNotFound, err, "principal binding ancestor cannot be inspected")
		}
		dstat, ok := dinfo.Sys().(*syscall.Stat_t)
		if !ok {
			return deny("ownership cannot be determined")
		}
		if dstat.Uid != uid && dstat.Uid != 0 {
			return deny("an ancestor directory is owned by another user")
		}
		if dinfo.Mode().Perm()&0o022 != 0 && dinfo.Mode()&os.ModeSticky == 0 {
			return deny("an ancestor directory is group or world writable")
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	return nil
}

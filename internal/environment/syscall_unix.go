//go:build unix

package environment

import (
	"io/fs"
	"syscall"

	"github.com/olostan/DevCadence/internal/errs"
)

// accessSyscall answers the access question the way the kernel would.
//
// access(2) is used rather than inspecting mode bits because the answer
// depends on the effective uid, supplementary groups and any ACL — exactly the
// things that make an accelerator device node usable by one user and not
// another. Reimplementing that check from st_mode would produce confident
// wrong answers on precisely the machines this matters on.
func accessSyscall(path string, mode AccessMode) error {
	var bits uint32
	if mode&AccessRead != 0 {
		bits |= 0x4 // R_OK
	}
	if mode&AccessWrite != 0 {
		bits |= 0x2 // W_OK
	}
	if bits == 0 {
		bits = 0x0 // F_OK: existence only
	}
	if err := syscall.Access(path, bits); err != nil {
		// syscall.Access returns a bare Errno; wrapping it in the fs error
		// vocabulary is what lets classify() treat "no such file" and
		// "permission denied" as the different findings they are.
		switch err {
		case syscall.ENOENT, syscall.ENOTDIR:
			return fs.ErrNotExist
		case syscall.EACCES, syscall.EPERM, syscall.EROFS:
			return fs.ErrPermission
		default:
			return errs.Wrap(errs.CategoryProbeFailed, err, "access %s", path)
		}
	}
	return nil
}

// diskUsageSyscall reports filesystem capacity.
//
// Available blocks rather than free blocks: the reserved portion of a
// filesystem is not space a model download could use, and reporting it would
// overstate what the machine can do.
func diskUsageSyscall(path string) (int64, int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		switch err {
		case syscall.ENOENT, syscall.ENOTDIR:
			return 0, 0, fs.ErrNotExist
		case syscall.EACCES, syscall.EPERM:
			return 0, 0, fs.ErrPermission
		default:
			return 0, 0, errs.Wrap(errs.CategoryProbeFailed, err, "statfs %s", path)
		}
	}
	blockSize := int64(stat.Bsize)
	if blockSize <= 0 {
		return 0, 0, errs.New(errs.CategoryProbeFailed, "statfs %s reported block size %d", path, blockSize)
	}
	//nolint:gosec // Blocks counts are kernel-reported and fit in int64 for any real filesystem.
	return int64(stat.Blocks) * blockSize, int64(stat.Bavail) * blockSize, nil
}

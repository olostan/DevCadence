//go:build unix

package journal

import (
	"os"
	"syscall"
)

// ownerUID reports the owning uid of fi. ok is false when the platform or the
// FileInfo does not expose it, which skips the ownership check.
func ownerUID(fi os.FileInfo) (uid int, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}

//go:build !unix

package journal

import "os"

// ownerUID is unavailable on this platform, which skips the ownership check.
func ownerUID(os.FileInfo) (int, bool) { return 0, false }

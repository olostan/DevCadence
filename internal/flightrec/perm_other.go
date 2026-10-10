//go:build !unix

package flightrec

import "os"

// ownerUID is unavailable off unix, which skips the ownership check.
func ownerUID(os.FileInfo) (int, bool) { return 0, false }

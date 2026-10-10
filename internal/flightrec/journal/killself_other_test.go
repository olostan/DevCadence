//go:build !unix

package journal

import "os"

// killSelf terminates the helper process without running defers.
func killSelf() { os.Exit(137) }

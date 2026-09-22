//go:build !unix

package environment

// accessSyscall reports that the question cannot be asked on this platform.
//
// A platform DevCadence has not integrated must still produce valid
// environment facts: an unanswerable probe is an explicit "unsupported"
// finding, never a crash and never a guess (DCI-104).
func accessSyscall(string, AccessMode) error { return ErrProbeUnsupported }

// diskUsageSyscall reports that the question cannot be asked on this platform.
func diskUsageSyscall(string) (int64, int64, error) { return 0, 0, ErrProbeUnsupported }

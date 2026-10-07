package receipts

import (
	"os"

	"github.com/olostan/DevCadence/internal/process"
)

// getEUID allows mocking os.Geteuid in unit tests.
var getEUID = os.Geteuid

// ProtectionOptions configures filesystem protection and ownership checks.
type ProtectionOptions struct {
	// OperatorUID is the required owner UID for the target file and its containing directory.
	OperatorUID uint32

	// TrustedOwnerUIDs lists all UIDs permitted to own ancestor directories.
	// Defaults to {0, OperatorUID} if empty.
	TrustedOwnerUIDs []uint32

	// CheckACL is an optional callback to verify absence of ACLs on a path.
	// When nil, platform-specific defaults are used.
	CheckACL func(path string) error

	// Runner is the process runner used by platforms requiring subprocess probes (e.g. Darwin).
	Runner *process.Runner

	// RootDir specifies the root of directory traversal. Defaults to "/".
	RootDir string
}

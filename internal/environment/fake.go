package environment

import (
	"context"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadience/internal/protocol"
)

// This file provides the deterministic doubles the test suite runs on.
//
// They live in the package rather than in a _test.go file on purpose: the
// cognition adapters and the CLI tests need to build the same fixture machines,
// and a fixture vocabulary shared across packages is worth more than the
// privacy of hiding it. ENGINEERING_STANDARDS.md §19 requires the hard logic to
// be testable with no LLM, no GPU and no network; these types are how.

// FakeSysProbe is a map-backed SysProbe describing a fictional machine.
//
// A path absent from Files and Dirs does not exist, which is the common case a
// discovery test needs: a blank machine is an empty FakeSysProbe.
type FakeSysProbe struct {
	// Files maps an absolute path to its content.
	Files map[string]string
	// Dirs maps an absolute directory path to its entry names. A directory
	// implied by a file path is *not* automatically present, so a test must
	// state which directories are enumerable — that difference matters when
	// discovery walks /sys/bus/pci/devices.
	Dirs map[string][]string
	// Unreadable marks paths that exist but cannot be read, for exercising the
	// permission-denied finding.
	Unreadable map[string]bool
	// Denied marks paths for which Access fails with a permission error.
	Denied map[string]bool
	// Disk maps a path to its reported total and available bytes.
	Disk map[string][2]int64
	// Unsupported marks paths for which the platform cannot answer.
	Unsupported map[string]bool
}

// ReadFile implements SysProbe.
func (f FakeSysProbe) ReadFile(p string) ([]byte, error) {
	if f.Unreadable[p] {
		return nil, fs.ErrPermission
	}
	content, ok := f.Files[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(content), nil
}

// ReadDir implements SysProbe.
func (f FakeSysProbe) ReadDir(p string) ([]string, error) {
	if f.Unreadable[p] {
		return nil, fs.ErrPermission
	}
	names, ok := f.Dirs[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out, nil
}

// Exists implements SysProbe.
func (f FakeSysProbe) Exists(p string) (bool, error) {
	if _, ok := f.Files[p]; ok {
		return true, nil
	}
	if _, ok := f.Dirs[p]; ok {
		return true, nil
	}
	if f.Denied[p] || f.Unreadable[p] {
		// A path marked denied or unreadable exists; that is the point of
		// marking it.
		return true, nil
	}
	// A path whose parent directory lists it exists even without content,
	// which is how device nodes are described.
	if names, ok := f.Dirs[path.Dir(p)]; ok {
		for _, name := range names {
			if name == path.Base(p) {
				return true, nil
			}
		}
	}
	return false, nil
}

// Access implements SysProbe.
func (f FakeSysProbe) Access(p string, _ AccessMode) error {
	if f.Unsupported[p] {
		return ErrProbeUnsupported
	}
	if f.Denied[p] {
		return fs.ErrPermission
	}
	exists, err := f.Exists(p)
	if err != nil {
		return err
	}
	if !exists {
		return fs.ErrNotExist
	}
	return nil
}

// DiskUsage implements SysProbe.
func (f FakeSysProbe) DiskUsage(p string) (int64, int64, error) {
	if f.Unsupported[p] {
		return 0, 0, ErrProbeUnsupported
	}
	usage, ok := f.Disk[p]
	if !ok {
		return 0, 0, fs.ErrNotExist
	}
	return usage[0], usage[1], nil
}

// FakeCommandProbe is a table-driven CommandProbe.
//
// Commands are keyed by the joined argv, so a test states exactly which
// invocation it is answering. An unmatched command reports absence, matching
// the common case of a machine where the tool is not installed.
type FakeCommandProbe struct {
	// Installed maps a bare executable name to its resolved path.
	Installed map[string]string
	// Outputs maps a joined argv ("ollama --version") to the outcome it
	// produces.
	Outputs map[string]ProbeOutcome
	// Calls records every invocation in order, so a test can assert that an
	// expensive probe was *not* run at a shallow depth.
	Calls []string
}

// Key builds the Outputs key for an executable and its arguments.
func Key(executable string, args ...string) string {
	return strings.TrimSpace(executable + " " + strings.Join(args, " "))
}

// Lookup implements CommandProbe.
func (f *FakeCommandProbe) Lookup(name string) (string, bool) {
	resolved, ok := f.Installed[name]
	return resolved, ok
}

// Run implements CommandProbe.
func (f *FakeCommandProbe) Run(ctx context.Context, cmd ProbeCommand) ProbeOutcome {
	key := Key(cmd.Executable, cmd.Args...)
	f.Calls = append(f.Calls, key)
	// Cancellation is honoured before consulting the table so that a
	// cancellation test does not depend on the table's contents.
	if err := ctx.Err(); err != nil {
		return ProbeOutcome{
			Name: cmd.Name, Status: protocol.FindingTimeout, ExitCode: -1,
			Detail: "probe cancelled before execution",
		}
	}
	outcome, ok := f.Outputs[key]
	if !ok {
		if _, installed := f.Installed[cmd.Executable]; !installed {
			return ProbeOutcome{
				Name: cmd.Name, Status: protocol.FindingAbsent, ExitCode: -1,
				Detail: "executable not found on the controlled PATH",
			}
		}
		return ProbeOutcome{
			Name: cmd.Name, Status: protocol.FindingError, ExitCode: -1,
			Detail: "fake probe has no answer for this invocation",
		}
	}
	outcome.Name = cmd.Name
	outcome.Command = append([]string{cmd.Executable}, cmd.Args...)
	if outcome.Duration == 0 {
		outcome.Duration = time.Millisecond
	}
	return outcome
}

// Observed builds a successful ProbeOutcome carrying stdout.
func Observed(stdout string) ProbeOutcome {
	return ProbeOutcome{Status: protocol.FindingObserved, ExitCode: 0, Stdout: stdout}
}

// Failed builds a ProbeOutcome for a command that ran and exited nonzero.
func Failed(exitCode int, stderr string) ProbeOutcome {
	return ProbeOutcome{Status: protocol.FindingError, ExitCode: exitCode, Stderr: stderr,
		Detail: "probe exited nonzero"}
}

// TimedOut builds a ProbeOutcome for a command that exceeded its bound.
func TimedOut() ProbeOutcome {
	return ProbeOutcome{Status: protocol.FindingTimeout, ExitCode: -1, Detail: "probe exceeded its time bound"}
}

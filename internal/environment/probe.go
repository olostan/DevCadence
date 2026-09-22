// Package environment discovers what the machine is, without assuming what it
// ought to be.
//
// Everything this package produces is a fact or an explicit statement that a
// fact could not be observed (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md
// §2). Assessment — which backends are plausible, which endpoints are usable —
// is a separate, pure function of those facts and lives in
// internal/environment's compatibility layer and internal/cognition.
//
// Two abstractions make that testable. A SysProbe reads operating-system state
// (files, directories, device nodes, filesystem capacity) and a CommandProbe
// runs bounded, non-mutating external commands through the M2 controlled
// runner. Discovery consults nothing else: no direct os/exec, no direct file
// reads, and no runtime.GOOS branch outside the constructor. That is what lets
// a Linux/AMD machine, a macOS/Apple Silicon machine and a machine with no
// accelerator at all be exercised as fixtures on whatever host runs the tests.
package environment

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// MaxProbeFileBytes bounds a single SysProbe file read.
//
// Pseudo-filesystem entries are small; a multi-megabyte /proc read means
// something is wrong, and an unbounded read of an attacker-influenced path
// would be a denial-of-service primitive.
const MaxProbeFileBytes = 1 << 20 // 1 MiB

// MaxProbeOutputBytes bounds captured output from one probe command.
const MaxProbeOutputBytes = 256 << 10 // 256 KiB

// DefaultProbeTimeout bounds one probe command.
//
// Version and health commands are expected to answer immediately. A generous
// but finite bound keeps a hung vendor utility from stalling discovery
// (DCI-033).
const DefaultProbeTimeout = 10 * time.Second

// AccessMode is the access being tested for.
type AccessMode int

const (
	// AccessRead tests readability.
	AccessRead AccessMode = 1 << iota
	// AccessWrite tests writability. For an accelerator device node this is
	// the mode that matters: compute submission needs write access, so a
	// read-only render node is a permissions problem rather than a usable
	// device.
	AccessWrite
)

// ErrProbeUnsupported reports that this build cannot answer a probe on this
// platform. It is a fact, not a failure.
var ErrProbeUnsupported = errs.New(errs.CategoryUnsupported, "probe is not supported on this platform")

// SysProbe reads operating-system state.
//
// Implementations must not mutate anything. Paths are absolute in the probed
// machine's namespace; a fixture implementation is free to interpret them
// against its own map.
type SysProbe interface {
	// ReadFile returns up to MaxProbeFileBytes of a file's content.
	ReadFile(path string) ([]byte, error)
	// ReadDir returns the sorted entry names of a directory.
	ReadDir(path string) ([]string, error)
	// Exists reports whether a path exists. A path that exists but cannot be
	// stat'ed for permission reasons reports fs.ErrPermission.
	Exists(path string) (bool, error)
	// Access reports whether the caller has the requested access. A nil error
	// means access is available; fs.ErrPermission means it is not;
	// ErrProbeUnsupported means the question could not be asked.
	Access(path string, mode AccessMode) error
	// DiskUsage reports total and available bytes for the filesystem holding
	// path.
	DiskUsage(path string) (total, available int64, err error)
}

// ProbeCommand describes one bounded, non-mutating external command.
//
// There is no shell string and no way to supply one: Executable and Args go
// straight to the controlled runner (DCI-033, docs/SECURITY.md §5). Nothing in
// this package builds a ProbeCommand from configuration or from model output —
// the command tables are typed Go values, so there is no path from a
// repository file or a provider response to an executed command.
type ProbeCommand struct {
	// Name is the stable probe identity recorded as provenance.
	Name string
	// Executable is a bare name resolved on the controlled PATH, or an
	// absolute path.
	Executable string
	Args       []string
	Timeout    time.Duration
	// MaxOutputBytes bounds captured stdout and stderr. Zero selects
	// MaxProbeOutputBytes.
	MaxOutputBytes int64
}

// ProbeOutcome is the result of one probe command.
//
// There is no error return alongside it. A missing binary, a permission
// failure, malformed output and a timeout are all facts about the environment
// (DCI-104); representing them as Go errors would invite a caller to abort
// discovery over an absent optional tool.
type ProbeOutcome struct {
	Name   string
	Status protocol.FindingStatus
	// Command is the resolved argv, for provenance.
	Command  []string
	ExitCode int
	// Stdout and Stderr are sanitised and length-bounded. They are untrusted
	// data (DCI-083): nothing in this package interprets them as instructions.
	Stdout   string
	Stderr   string
	Duration time.Duration
	Detail   string
}

// Succeeded reports whether the command ran and exited zero.
func (o ProbeOutcome) Succeeded() bool {
	return o.Status == protocol.FindingObserved && o.ExitCode == 0
}

// CommandProbe runs probe commands.
type CommandProbe interface {
	// Lookup resolves an executable without running it. This is the cheapest
	// depth of discovery: it establishes "installed", and deliberately
	// nothing more.
	Lookup(name string) (string, bool)
	// Run executes cmd to completion, to its timeout, or to cancellation.
	Run(ctx context.Context, cmd ProbeCommand) ProbeOutcome
}

// osSysProbe reads the real operating system.
//
// Root is empty in production. A non-empty Root rebases every path, which lets
// an integration test point discovery at a captured /proc and /sys tree
// without a mock. Absolute paths are joined under Root after cleaning, so a
// "..\/" segment in a path cannot escape it.
type osSysProbe struct{ root string }

// NewSysProbe returns a SysProbe reading the real operating system.
func NewSysProbe() SysProbe { return osSysProbe{} }

// NewRootedSysProbe returns a SysProbe reading a captured filesystem tree.
func NewRootedSysProbe(root string) SysProbe { return osSysProbe{root: root} }

func (p osSysProbe) resolve(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errs.New(errs.CategoryInvalidArgument, "environment: probe path %q must be absolute", path)
	}
	if p.root == "" {
		return filepath.Clean(path), nil
	}
	// Clean first so that a path containing ".." cannot escape the root.
	return filepath.Join(p.root, filepath.Clean(path)), nil
}

func (p osSysProbe) ReadFile(path string) ([]byte, error) {
	resolved, err := p.resolve(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	// A bounded stream read rather than os.ReadFile: several /proc and /sys
	// files report a zero size, which would defeat a size-based guard.
	return io.ReadAll(io.LimitReader(file, MaxProbeFileBytes))
}

func (p osSysProbe) ReadDir(path string) ([]string, error) {
	resolved, err := p.resolve(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func (p osSysProbe) Exists(path string) (bool, error) {
	resolved, err := p.resolve(path)
	if err != nil {
		return false, err
	}
	// Lstat rather than Stat: a dangling or looping symlink at a device path
	// is a fact about the path, and following it could take the probe
	// somewhere the caller did not name (docs/SECURITY.md §6).
	if _, err := os.Lstat(resolved); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (p osSysProbe) Access(path string, mode AccessMode) error {
	resolved, err := p.resolve(path)
	if err != nil {
		return err
	}
	return accessSyscall(resolved, mode)
}

func (p osSysProbe) DiskUsage(path string) (int64, int64, error) {
	resolved, err := p.resolve(path)
	if err != nil {
		return 0, 0, err
	}
	return diskUsageSyscall(resolved)
}

// commandProbe runs probe commands through the M2 controlled runner.
//
// Reusing the runner is not a stylistic preference. It is what gives every
// probe explicit argv with no shell interpolation, an explicit working
// directory, a controlled environment, a hard timeout and bounded captured
// output — the properties DCI-033 requires and that a scattered
// exec.Command would each have to re-establish.
type commandProbe struct {
	runner *process.Runner
	dir    string
	env    []string
}

// NewCommandProbe returns a CommandProbe backed by the controlled runner.
//
// dir is the working directory probe commands run in. It defaults to the
// system temporary directory, deliberately not the repository: a version or
// health command has no business being able to see project source, and several
// coding CLIs behave differently inside a project (docs/SECURITY.md §5).
func NewCommandProbe(dir string) (CommandProbe, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	if !filepath.IsAbs(dir) {
		return nil, errs.New(errs.CategoryInvalidArgument, "environment: probe dir %q must be absolute", dir)
	}
	return &commandProbe{runner: process.NewRunner(), dir: dir, env: process.BaseEnv()}, nil
}

func (p *commandProbe) Lookup(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		if !filepath.IsAbs(name) {
			return "", false
		}
		info, err := os.Stat(name)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return "", false
		}
		return name, true
	}
	// PATH comes from the controlled environment, not from os.Getenv at the
	// point of use, so that lookup and execution agree on which binary is
	// meant.
	var path string
	for _, entry := range p.env {
		if strings.HasPrefix(entry, "PATH=") {
			path = strings.TrimPrefix(entry, "PATH=")
			break
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return candidate, true
	}
	return "", false
}

func (p *commandProbe) Run(ctx context.Context, cmd ProbeCommand) ProbeOutcome {
	out := ProbeOutcome{Name: cmd.Name, ExitCode: -1}
	timeout := cmd.Timeout
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}
	maxBytes := cmd.MaxOutputBytes
	if maxBytes <= 0 {
		maxBytes = MaxProbeOutputBytes
	}
	result, err := p.runner.Run(ctx, process.Spec{
		Executable:     cmd.Executable,
		Args:           cmd.Args,
		Dir:            p.dir,
		Env:            p.env,
		Timeout:        timeout,
		MaxStdoutBytes: maxBytes,
		MaxStderrBytes: maxBytes,
	})
	out.Command = result.Command
	out.Duration = result.Duration()
	if err != nil {
		// The runner refuses before starting only for conditions that are
		// themselves facts: an executable it could not resolve is an absent
		// tool, not a defect.
		switch errs.CategoryOf(err) {
		case errs.CategoryNotFound:
			out.Status = protocol.FindingAbsent
			out.Detail = "executable not found on the controlled PATH"
		default:
			out.Status = protocol.FindingError
			out.Detail = sanitize(err.Error(), 512)
		}
		return out
	}
	out.Stdout = sanitize(string(result.Stdout), 8192)
	out.Stderr = sanitize(string(result.Stderr), 8192)
	switch result.Status {
	case process.StatusTimeout, process.StatusCancelled:
		out.Status = protocol.FindingTimeout
		out.Detail = "probe exceeded its time bound"
		return out
	}
	out.ExitCode = result.ExitCode
	if result.ExitCode != 0 {
		out.Status = protocol.FindingError
		out.Detail = "probe exited " + strconv.Itoa(result.ExitCode)
		return out
	}
	out.Status = protocol.FindingObserved
	return out
}

// classify maps a SysProbe error to the finding status that describes it.
//
// This is the single place absence, unreadability and genuine failure are
// distinguished, so no caller has to remember that a missing /sys entry is
// normal while an unreadable one is a permissions finding.
func classify(err error) protocol.FindingStatus {
	switch {
	case err == nil:
		return protocol.FindingObserved
	case errors.Is(err, fs.ErrNotExist):
		return protocol.FindingAbsent
	case errors.Is(err, fs.ErrPermission):
		return protocol.FindingPermissionDenied
	case errors.Is(err, ErrProbeUnsupported):
		return protocol.FindingUnsupported
	default:
		return protocol.FindingError
	}
}

// sanitize bounds and de-fangs text taken from an external tool.
//
// External output is untrusted data (DCI-083). Control characters are dropped
// so that a crafted version string cannot inject terminal escape sequences
// into an operator's console or forge structure in a log line, and the length
// bound keeps a hostile or broken tool from filling a durable record.
func sanitize(s string, max int) string {
	if max <= 0 {
		max = 256
	}
	var b strings.Builder
	b.Grow(min(len(s), max))
	for _, r := range s {
		if b.Len() >= max {
			break
		}
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(' ')
		case r == unicode.ReplacementChar:
			continue
		case unicode.IsControl(r):
			continue
		case !unicode.IsPrint(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

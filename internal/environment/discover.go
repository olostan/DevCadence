package environment

import (
	"context"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/olostan/DevCadience/internal/clock"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// Options configures a Discoverer.
//
// OS and Arch are inputs rather than reads of runtime.GOOS/GOARCH so that a
// test can discover a Linux machine while running on macOS. NewDefaultOptions
// is the single place the real host's identity enters this package.
type Options struct {
	OS   protocol.OSFamily
	Arch string
	Sys  SysProbe
	// Commands may be nil at DepthInventory only for a caller that wants a
	// pure filesystem view; discovery then records command probes as
	// unsupported rather than skipping them silently.
	Commands CommandProbe
	Clock    clock.Clock
	// Depth bounds how much work discovery performs. At DepthInventory no
	// external command runs at all: `environment inspect` must be cheap and
	// must never load a model as a side effect.
	Depth protocol.ProbeDepth
	// StoragePaths are the filesystem paths whose capacity matters. Empty
	// selects the home directory root, because that is where model weights
	// and DevCadience state land.
	StoragePaths []string
	// Software overrides the inventory table. Empty selects DefaultInventory.
	Software []SoftwareDescriptor
}

// NewDefaultOptions returns Options describing the machine this process is
// running on.
//
// This function is the only place in the package that consults runtime.GOOS,
// runtime.GOARCH or the real filesystem. Everything downstream is a function
// of Options, which is what makes the platform-specific logic testable.
func NewDefaultOptions(clk clock.Clock, depth protocol.ProbeDepth) (Options, error) {
	commands, err := NewCommandProbe("")
	if err != nil {
		return Options{}, err
	}
	family := protocol.OSUnknown
	switch runtime.GOOS {
	case "linux":
		family = protocol.OSLinux
	case "darwin":
		family = protocol.OSDarwin
	case "windows":
		family = protocol.OSWindows
	}
	paths := []string{"/"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, home)
	}
	return Options{
		OS:           family,
		Arch:         runtime.GOARCH,
		Sys:          NewSysProbe(),
		Commands:     commands,
		Clock:        clk,
		Depth:        depth,
		StoragePaths: paths,
	}, nil
}

// Discoverer collects environment facts.
type Discoverer struct {
	opts      Options
	inventory []SoftwareDescriptor
}

// New returns a Discoverer.
func New(opts Options) (*Discoverer, error) {
	if opts.Sys == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "environment: a SysProbe is required")
	}
	if opts.Clock == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "environment: a Clock is required")
	}
	if opts.Arch == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "environment: Arch is required")
	}
	if opts.OS == "" {
		opts.OS = protocol.OSUnknown
	}
	if !opts.OS.Valid() {
		return nil, errs.New(errs.CategoryInvalidArgument, "environment: unknown OS family %q", opts.OS)
	}
	if opts.Depth == "" {
		opts.Depth = protocol.DepthInventory
	}
	if !opts.Depth.Valid() {
		return nil, errs.New(errs.CategoryInvalidArgument, "environment: unknown probe depth %q", opts.Depth)
	}
	inventory := opts.Software
	if len(inventory) == 0 {
		inventory = DefaultInventory()
	}
	return &Discoverer{opts: opts, inventory: inventory}, nil
}

// collector accumulates facts and findings.
//
// Every discovery step takes the collector and records either a fact or a
// finding explaining why it could not. No step returns an error that aborts the
// others: DCI-104 requires that an absent vendor tool degrade its own component
// and leave memory, storage and Git discovery intact.
type collector struct {
	facts    protocol.EnvironmentFacts
	findings []protocol.DiscoveryFinding
}

func (c *collector) observe(component, source string) {
	c.findings = append(c.findings, protocol.DiscoveryFinding{
		Component: component, Status: protocol.FindingObserved, Source: source,
	})
}

func (c *collector) note(component string, status protocol.FindingStatus, source, detail string) {
	c.findings = append(c.findings, protocol.DiscoveryFinding{
		Component: component, Status: status, Source: source, Detail: sanitize(detail, 1024),
	})
}

// noteErr records the finding that classifies err.
func (c *collector) noteErr(component, source string, err error) protocol.FindingStatus {
	status := classify(err)
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	c.note(component, status, source, detail)
	return status
}

// Discover collects environment facts.
//
// The only error it returns is a configuration error. A machine that yields
// almost nothing still produces valid facts: that is the blank-machine case,
// and it is a supported state rather than a failure
// (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §1).
func (d *Discoverer) Discover(ctx context.Context) (protocol.EnvironmentFacts, error) {
	c := &collector{}
	c.facts.ObservedAt = protocol.NewTimestamp(d.opts.Clock.Now())
	c.facts.Host = protocol.HostFacts{Family: d.opts.OS, Arch: d.opts.Arch}
	c.facts.Virtualization = protocol.VirtualizationFacts{Container: protocol.ContainerUnknown}

	// Ordering is fixed and sequential. Discovery is cheap enough that
	// concurrency would buy nothing while costing deterministic output
	// ordering, which the machine fingerprint depends on.
	switch d.opts.OS {
	case protocol.OSLinux:
		d.discoverLinux(ctx, c)
	case protocol.OSDarwin:
		d.discoverDarwin(ctx, c)
	default:
		c.note("hardware", protocol.FindingUnsupported, string(d.opts.OS),
			"this build has no hardware discovery for this operating system; portable facts only")
	}

	d.discoverStorage(c)
	d.discoverSoftware(ctx, c)

	sortFacts(&c.facts)
	c.facts.Findings = append(c.facts.Findings, c.findings...)
	sort.SliceStable(c.facts.Findings, func(i, j int) bool {
		if c.facts.Findings[i].Component != c.facts.Findings[j].Component {
			return c.facts.Findings[i].Component < c.facts.Findings[j].Component
		}
		return c.facts.Findings[i].Status < c.facts.Findings[j].Status
	})
	if err := c.facts.Validate(); err != nil {
		// A validation failure here is a defect in this package, not a
		// property of the machine.
		return protocol.EnvironmentFacts{}, errs.Wrap(errs.CategoryInternal, err,
			"environment: discovery produced facts that violate the contract")
	}
	return c.facts, nil
}

// discoverStorage records capacity for the configured paths.
func (d *Discoverer) discoverStorage(c *collector) {
	paths := d.opts.StoragePaths
	if len(paths) == 0 {
		return
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		total, available, err := d.opts.Sys.DiskUsage(p)
		if err != nil {
			c.noteErr("hardware.storage", p, err)
			continue
		}
		c.facts.Storage = append(c.facts.Storage, protocol.StorageFacts{
			Path: p, TotalBytes: &total, AvailableBytes: &available,
		})
		c.observe("hardware.storage", p)
	}
}

// canRunCommands reports whether the configured depth and probe allow running
// external commands.
func (d *Discoverer) canRunCommands() bool {
	return d.opts.Commands != nil && d.opts.Depth.AtLeast(protocol.DepthHealth)
}

// run executes a probe command when depth permits.
//
// When it does not, the outcome is an explicit unsupported finding rather than
// a silent skip, so a shallow profile cannot be mistaken for one where the
// command failed.
func (d *Discoverer) run(ctx context.Context, cmd ProbeCommand) ProbeOutcome {
	if !d.canRunCommands() {
		return ProbeOutcome{
			Name: cmd.Name, Status: protocol.FindingUnsupported, ExitCode: -1,
			Detail: "probe depth " + string(d.opts.Depth) + " does not permit running external commands",
		}
	}
	return d.opts.Commands.Run(ctx, cmd)
}

// readLines reads a file and splits it into lines, recording a finding when it
// cannot be read.
func (d *Discoverer) readLines(c *collector, component, path string) ([]string, bool) {
	content, err := d.opts.Sys.ReadFile(path)
	if err != nil {
		c.noteErr(component, path, err)
		return nil, false
	}
	return strings.Split(string(content), "\n"), true
}

// readTrimmed reads a file and trims surrounding whitespace.
func (d *Discoverer) readTrimmed(path string) (string, bool) {
	content, err := d.opts.Sys.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(content)), true
}

// deviceNode records the presence and accessibility of one device path.
func (d *Discoverer) deviceNode(c *collector, path string) {
	exists, err := d.opts.Sys.Exists(path)
	if err != nil {
		status := c.noteErr("hardware.device_node", path, err)
		// A path that could not be stat'ed for permission reasons exists.
		present := status == protocol.FindingPermissionDenied
		c.facts.DeviceNodes = append(c.facts.DeviceNodes,
			protocol.DeviceNodeFacts{Path: path, Present: present})
		return
	}
	node := protocol.DeviceNodeFacts{Path: path, Present: exists}
	if exists {
		// Write access is the meaningful question for a compute device, and
		// the answer is recorded as a tri-state: nil means the platform could
		// not answer, which is different from "not writable".
		switch err := d.opts.Sys.Access(path, AccessWrite); {
		case err == nil:
			yes := true
			node.Writable = &yes
			node.Readable = &yes
		case classify(err) == protocol.FindingPermissionDenied:
			no := false
			node.Writable = &no
			readable := d.opts.Sys.Access(path, AccessRead) == nil
			node.Readable = &readable
			c.note("hardware.device_node", protocol.FindingPermissionDenied, path,
				"device node exists but is not writable by this user")
		default:
			c.noteErr("hardware.device_node", path, err)
		}
	}
	c.facts.DeviceNodes = append(c.facts.DeviceNodes, node)
}

// sortFacts imposes deterministic ordering on every collection.
//
// Discovery reads directories and PATH entries whose order is not guaranteed.
// Without stable sorting the same machine would produce different serialised
// facts on consecutive runs, and therefore a different fingerprint and
// different digests — which would make the profile useless for detecting real
// change (ENGINEERING_STANDARDS.md §17).
func sortFacts(f *protocol.EnvironmentFacts) {
	sort.Strings(f.CPU.Features)
	sort.Slice(f.Storage, func(i, j int) bool { return f.Storage[i].Path < f.Storage[j].Path })
	sort.Slice(f.Accelerators, func(i, j int) bool { return f.Accelerators[i].ID < f.Accelerators[j].ID })
	for i := range f.Accelerators {
		sort.Strings(f.Accelerators[i].Sources)
	}
	sort.Slice(f.DeviceNodes, func(i, j int) bool { return f.DeviceNodes[i].Path < f.DeviceNodes[j].Path })
	sort.Slice(f.Software, func(i, j int) bool { return f.Software[i].ID < f.Software[j].ID })
}

// parseKeyValue parses "KEY=value" and "KEY: value" lines into a map.
//
// Quoting is stripped because /etc/os-release quotes values. Only the first
// occurrence of a key wins, matching how these files are read elsewhere.
func parseKeyValue(lines []string, separator string) map[string]string {
	out := map[string]string{}
	for _, line := range lines {
		key, value, found := strings.Cut(line, separator)
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		if _, exists := out[key]; !exists {
			out[key] = sanitize(value, 256)
		}
	}
	return out
}

// parseBytesFromKB converts a "1234 kB" style value to bytes.
func parseBytesFromKB(value string) (int64, bool) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n * 1024, true
}

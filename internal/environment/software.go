package environment

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/olostan/DevCadience/internal/protocol"
)

// Software discovery answers "what is installed", and deliberately stops there.
//
// A binary on PATH is not a usable capability
// (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §7). Whether a runtime is
// serving, whether a model exists, whether a session is authenticated and
// whether inference is accelerated are all separate questions answered by
// internal/cognition against this inventory. Keeping them apart is what stops
// "ollama is installed" from being read as "local cognition is ready".
//
// The inventory is a typed table, not configuration. Nothing in a repository,
// a YAML file or a model response can add an entry, because an entry names an
// executable that will be run (§32 of the M3A specification, DCI-083).

// SoftwareDescriptor describes one piece of software discovery looks for.
type SoftwareDescriptor struct {
	// ID is the stable handle recorded in SoftwarePresence.
	ID       string
	Category protocol.SoftwareCategory
	// Executables are candidate names, tried in order. The first that resolves
	// wins, which is how a tool with several launcher names is handled.
	Executables []string
	// VersionArgs is the argv that makes the tool print its version. Empty
	// means this build knows no safe way to ask, and the version stays
	// unknown rather than being guessed from a path.
	VersionArgs []string
	// MinVersion is the compatibility floor, as a dotted version. Empty means
	// this build makes no compatibility statement, which yields
	// VersionUnknown — not VersionCompatible.
	MinVersion string
	// OSFamilies restricts where the tool is looked for. Empty means anywhere.
	OSFamilies []protocol.OSFamily
	// ApplicationPaths are filesystem locations to check for GUI applications
	// that are not normally on PATH, keyed by OS family.
	ApplicationPaths map[protocol.OSFamily][]string
}

// InventoryRevision versions the software table.
//
// The table is compatibility knowledge, not architecture: which CLIs exist and
// what their version floors are will change, and ADR-0011 §5 asks for that to
// live in versioned knowledge rather than scattered conditionals. A profile
// records the revision that produced it.
const InventoryRevision = "software-inventory/2026-09-21"

// DefaultInventory is the software this build knows how to look for.
//
// It is intentionally short. A universal software database is explicitly out of
// scope; the list covers the deterministic tooling DevCadience depends on, the
// local runtimes it can adapt, the coding CLIs it can route to, the accelerator
// utilities that enrich hardware facts, and the three first-class principal
// hosts of ADR-0011 §6.
func DefaultInventory() []SoftwareDescriptor {
	return []SoftwareDescriptor{
		// Deterministic engineering tooling. Its absence reduces deterministic
		// capability and has nothing to do with cognition.
		{
			ID: "git", Category: protocol.SoftwareEngineering,
			Executables: []string{"git"}, VersionArgs: []string{"--version"},
			MinVersion: "2.30",
		},
		{
			ID: "go", Category: protocol.SoftwareEngineering,
			Executables: []string{"go"}, VersionArgs: []string{"version"},
		},
		{
			ID: "docker", Category: protocol.SoftwareEngineering,
			Executables: []string{"docker"}, VersionArgs: []string{"--version"},
		},
		{
			ID: "podman", Category: protocol.SoftwareEngineering,
			Executables: []string{"podman"}, VersionArgs: []string{"--version"},
		},
		{
			ID: "python3", Category: protocol.SoftwareEngineering,
			Executables: []string{"python3"}, VersionArgs: []string{"--version"},
		},

		// Local inference runtimes. Presence only; readiness is an endpoint
		// question.
		{
			ID: "ollama", Category: protocol.SoftwareCognitionRuntime,
			Executables: []string{"ollama"}, VersionArgs: []string{"--version"},
		},
		{
			// mlx-lm installs console scripts; the deeper Python-level
			// detection belongs to the MLX adapter, which can tell an
			// importable package from a stale launcher.
			ID: "mlx-lm", Category: protocol.SoftwareCognitionRuntime,
			Executables: []string{"mlx_lm.generate"},
		},

		// Coding and agent CLIs. No version floor is asserted: these tools
		// release frequently and a stale floor would mislabel a working
		// install as incompatible.
		{
			ID: "codex-cli", Category: protocol.SoftwareCognitionCLI,
			Executables: []string{"codex"}, VersionArgs: []string{"--version"},
		},
		{
			ID: "claude-code", Category: protocol.SoftwareCognitionCLI,
			Executables: []string{"claude"}, VersionArgs: []string{"--version"},
		},
		{
			ID: "gemini-cli", Category: protocol.SoftwareCognitionCLI,
			Executables: []string{"gemini"}, VersionArgs: []string{"--version"},
		},

		// Accelerator userspace. Presence enriches hardware facts and is never
		// itself proof that inference is accelerated (DCI-106).
		{
			ID: "nvidia-smi", Category: protocol.SoftwareAcceleration,
			Executables: []string{"nvidia-smi"}, VersionArgs: []string{"--version"},
			OSFamilies: []protocol.OSFamily{protocol.OSLinux},
		},
		{
			ID: "rocminfo", Category: protocol.SoftwareAcceleration,
			Executables: []string{"rocminfo"},
			OSFamilies:  []protocol.OSFamily{protocol.OSLinux},
		},
		{
			ID: "vulkaninfo", Category: protocol.SoftwareAcceleration,
			Executables: []string{"vulkaninfo"},
			OSFamilies:  []protocol.OSFamily{protocol.OSLinux},
		},

		// Principal hosts. A host is a frontend, not a cognition endpoint; it
		// is inventoried here and interpreted by internal/principalhosts.
		{
			ID: "antigravity", Category: protocol.SoftwarePrincipalHost,
			Executables: []string{"antigravity"},
			ApplicationPaths: map[protocol.OSFamily][]string{
				protocol.OSDarwin: {"/Applications/Antigravity.app"},
				protocol.OSLinux:  {"/opt/antigravity", "/usr/share/antigravity"},
			},
		},
		{
			ID: "cursor", Category: protocol.SoftwarePrincipalHost,
			Executables: []string{"cursor"},
			ApplicationPaths: map[protocol.OSFamily][]string{
				protocol.OSDarwin: {"/Applications/Cursor.app"},
				protocol.OSLinux:  {"/opt/cursor", "/usr/share/cursor"},
			},
		},
		{
			ID: "vscode", Category: protocol.SoftwarePrincipalHost,
			Executables: []string{"code"}, VersionArgs: []string{"--version"},
			ApplicationPaths: map[protocol.OSFamily][]string{
				protocol.OSDarwin: {"/Applications/Visual Studio Code.app"},
				protocol.OSLinux:  {"/usr/share/code", "/opt/visual-studio-code"},
			},
		},
	}
}

// appliesTo reports whether the descriptor should be looked for on this OS.
func (s SoftwareDescriptor) appliesTo(family protocol.OSFamily) bool {
	if len(s.OSFamilies) == 0 {
		return true
	}
	for _, candidate := range s.OSFamilies {
		if candidate == family {
			return true
		}
	}
	return false
}

func (d *Discoverer) discoverSoftware(ctx context.Context, c *collector) {
	for _, descriptor := range d.inventory {
		if !descriptor.appliesTo(d.opts.OS) {
			c.note("software."+descriptor.ID, protocol.FindingUnsupported, "inventory",
				"not looked for on this operating system")
			continue
		}
		c.facts.Software = append(c.facts.Software, d.locateSoftware(ctx, c, descriptor))
	}
}

// locateSoftware resolves one descriptor into a presence record.
//
// Failure of one descriptor cannot affect another: each call is independent and
// records its own finding (DCI-104). A probe that times out leaves the tool
// marked installed with an unknown version, which is exactly what was observed.
func (d *Discoverer) locateSoftware(ctx context.Context, c *collector, descriptor SoftwareDescriptor) protocol.SoftwarePresence {
	component := "software." + descriptor.ID
	presence := protocol.SoftwarePresence{
		ID: descriptor.ID, Category: descriptor.Category, VersionStatus: protocol.VersionUnknown,
	}

	var executable string
	if d.opts.Commands != nil {
		for _, candidate := range descriptor.Executables {
			if resolved, ok := d.opts.Commands.Lookup(candidate); ok {
				executable = candidate
				presence.Installed = true
				presence.Path = resolved
				break
			}
		}
	}
	if !presence.Installed {
		// A GUI application is installed without being on PATH. Checking the
		// application bundle is what stops "code is not on PATH" from being
		// reported as "VS Code is not installed".
		for _, path := range descriptor.ApplicationPaths[d.opts.OS] {
			exists, err := d.opts.Sys.Exists(path)
			if err != nil {
				c.noteErr(component, path, err)
				continue
			}
			if exists {
				presence.Installed = true
				presence.Path = path
				presence.Detail = "found as an installed application; not on PATH"
				break
			}
		}
	}
	if !presence.Installed {
		c.note(component, protocol.FindingAbsent, "PATH", "")
		return presence
	}

	if len(descriptor.VersionArgs) == 0 || executable == "" {
		c.note(component, protocol.FindingUnsupported, presence.Path,
			"this build knows no safe command to read this tool's version")
		return presence
	}
	outcome := d.run(ctx, ProbeCommand{
		Name: descriptor.ID + "-version", Executable: executable, Args: descriptor.VersionArgs,
	})
	if !outcome.Succeeded() {
		c.note(component, outcome.Status, strings.Join(outcome.Command, " "),
			"installed, but the version probe did not answer: "+outcome.Detail)
		return presence
	}
	version := extractVersion(outcome.Stdout)
	if version == "" {
		version = extractVersion(outcome.Stderr)
	}
	if version == "" {
		c.note(component, protocol.FindingMalformed, strings.Join(outcome.Command, " "),
			"version output contained no recognisable version")
		return presence
	}
	presence.Version = version
	presence.VersionStatus = compareVersion(version, descriptor.MinVersion)
	c.observe(component, presence.Path)
	return presence
}

// versionPattern matches a dotted version anywhere in a tool's output.
//
// The bound on digit runs is deliberate: version text is untrusted input
// (DCI-083), and an unbounded numeric pattern applied to hostile output is a
// backtracking hazard as well as a way to smuggle a long string into a durable
// record.
var versionPattern = regexp.MustCompile(`\b(\d{1,5})\.(\d{1,5})(?:\.(\d{1,5}))?\b`)

// extractVersion pulls the first dotted version out of probe output.
func extractVersion(output string) string {
	match := versionPattern.FindString(output)
	return sanitize(match, 64)
}

// compareVersion judges an observed version against a floor.
//
// With no floor the answer is unknown, never compatible: this build has made no
// statement about the tool, and reporting compatibility would be an assertion
// without evidence.
func compareVersion(observed, minimum string) protocol.VersionStatus {
	if minimum == "" {
		return protocol.VersionUnknown
	}
	got, ok := parseVersion(observed)
	if !ok {
		return protocol.VersionUnknown
	}
	want, ok := parseVersion(minimum)
	if !ok {
		return protocol.VersionUnknown
	}
	for i := range got {
		if got[i] != want[i] {
			if got[i] > want[i] {
				return protocol.VersionCompatible
			}
			return protocol.VersionIncompatible
		}
	}
	return protocol.VersionCompatible
}

// parseVersion reads up to three dotted numeric components.
func parseVersion(value string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) == 0 || parts[0] == "" {
		return out, false
	}
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			if i == 0 {
				return out, false
			}
			break
		}
		out[i] = n
	}
	return out, true
}

package protocol

import "github.com/olostan/DevCadience/internal/errs"

// This file holds the durable representation of *observed machine facts*.
//
// The separation enforced here is the central rule of M3A
// (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §2, ADR-0011 §3):
//
//	facts  !=  assessment  !=  recommendation
//
// Everything in this file is a fact: something a probe observed, or an
// explicit statement that a probe could not observe it. Nothing here grades a
// machine, prefers a backend or recommends an installation. Assessment lives
// in AcceleratorCandidate and CognitionEndpoint; recommendation belongs to
// M3B and does not exist yet.
//
// Every "unknown" in these types is load-bearing. A blank machine, a machine
// whose vendor tooling is absent and a machine whose device nodes are
// unreadable must all produce valid facts (DCI-104), so absence is modelled
// explicitly rather than as a zero value that reads like a measurement.

// OSFamily is the operating-system family a probe observed.
//
// It is deliberately coarse. DevCadience branches on OS family only where the
// *location* of a fact differs (sysfs versus ioreg); capability decisions
// branch on observed evidence, never on the family name.
type OSFamily string

const (
	OSLinux   OSFamily = "linux"
	OSDarwin  OSFamily = "darwin"
	OSWindows OSFamily = "windows"
	// OSUnknown is a supported state: an unrecognised host still yields facts
	// for everything that could be observed portably.
	OSUnknown OSFamily = "unknown"
)

// Valid reports whether the family is defined by the schema.
func (f OSFamily) Valid() bool {
	switch f {
	case OSLinux, OSDarwin, OSWindows, OSUnknown:
		return true
	}
	return false
}

// FindingStatus classifies the outcome of one discovery probe.
//
// This type is why capability absence degrades instead of contaminating
// (DCI-104). Discovery returns facts plus a per-component finding list, so
// "nvidia-smi is not installed" is a recorded fact about one component rather
// than an error that aborts memory, storage and Git discovery with it.
type FindingStatus string

const (
	// FindingObserved means the probe ran and produced a usable fact.
	FindingObserved FindingStatus = "observed"
	// FindingAbsent means the source of the fact does not exist: no such
	// file, no such device, no such executable. This is a normal state.
	FindingAbsent FindingStatus = "absent"
	// FindingPermissionDenied means the source exists but could not be read.
	// It is distinct from absence: a GPU whose device node cannot be opened
	// is present-but-unusable, which is a different remediation than no GPU.
	FindingPermissionDenied FindingStatus = "permission_denied"
	// FindingMalformed means the source existed and was read but its content
	// did not parse. External output is untrusted data (DCI-083); malformed
	// output is a fact about the tool, not a defect in DevCadience.
	FindingMalformed FindingStatus = "malformed"
	// FindingTimeout means a probe exceeded its bound and was stopped.
	FindingTimeout FindingStatus = "timeout"
	// FindingUnsupported means this build knows the fact cannot be observed
	// on this platform by this probe.
	FindingUnsupported FindingStatus = "unsupported"
	// FindingError means the probe failed for a reason none of the above
	// describes.
	FindingError FindingStatus = "error"
)

// Valid reports whether the status is defined by the schema.
func (s FindingStatus) Valid() bool {
	switch s {
	case FindingObserved, FindingAbsent, FindingPermissionDenied, FindingMalformed,
		FindingTimeout, FindingUnsupported, FindingError:
		return true
	}
	return false
}

// DiscoveryFinding records what one probe established, including failure.
//
// Detail is bounded, sanitised text derived from external output. It is data,
// never an instruction, and never carries a credential (DCI-081, DCI-083).
type DiscoveryFinding struct {
	// Component is a stable identifier for what was probed, e.g.
	// "hardware.memory", "software.ollama", "accelerator.nvidia".
	Component string        `json:"component"`
	Status    FindingStatus `json:"status"`
	Detail    string        `json:"detail,omitempty"`
	// Source names where the fact came from: a file path, a probe command
	// name, or an API path. It is provenance, so a stale or surprising fact
	// can be re-checked at its origin (DCI-011).
	Source string `json:"source,omitempty"`
}

// Validate checks the finding is interpretable.
func (f DiscoveryFinding) Validate() error {
	const kind = "MachineCapabilityProfile"
	if err := requireNonEmpty(kind, "findings[].component", f.Component); err != nil {
		return err
	}
	if !f.Status.Valid() {
		return enumError(kind, "findings[].status", string(f.Status),
			"observed", "absent", "permission_denied", "malformed", "timeout", "unsupported", "error")
	}
	return nil
}

// HostFacts is the operating-system identity of the machine.
//
// No field is required beyond Family and Arch, because a machine may not
// publish a distribution identity at all.
type HostFacts struct {
	Family  OSFamily `json:"family"`
	Arch    string   `json:"arch"`
	Version string   `json:"version,omitempty"`
	// Distribution and DistributionVersion are Linux-style identity
	// (/etc/os-release ID and VERSION_ID).
	Distribution        string `json:"distribution,omitempty"`
	DistributionVersion string `json:"distribution_version,omitempty"`
	Kernel              string `json:"kernel,omitempty"`
}

// CPUFacts is what could be observed about the processor.
//
// Core counts are pointers because zero cores is not a possible observation:
// an absent count must be distinguishable from a measured one.
type CPUFacts struct {
	Model         string `json:"model,omitempty"`
	Vendor        string `json:"vendor,omitempty"`
	LogicalCores  *int   `json:"logical_cores,omitempty"`
	PhysicalCores *int   `json:"physical_cores,omitempty"`
	// Features lists instruction-set facts that matter to inference runtimes
	// (for example "avx2", "avx512f", "neon"). It is bounded and sorted.
	Features []string `json:"features,omitempty"`
}

// MemoryFacts is observed memory. All values are bytes.
//
// AvailableBytes is a runtime observation and ages immediately; it is here
// because a scheduling decision needs it, and it is explicitly not part of the
// machine fingerprint for that reason.
type MemoryFacts struct {
	TotalBytes     *int64 `json:"total_bytes,omitempty"`
	AvailableBytes *int64 `json:"available_bytes,omitempty"`
	SwapTotalBytes *int64 `json:"swap_total_bytes,omitempty"`
}

// StorageFacts is available capacity at one path DevCadience cares about.
type StorageFacts struct {
	Path           string `json:"path"`
	TotalBytes     *int64 `json:"total_bytes,omitempty"`
	AvailableBytes *int64 `json:"available_bytes,omitempty"`
}

// ContainerKind names an observed containerisation context.
type ContainerKind string

const (
	ContainerNone    ContainerKind = "none"
	ContainerDocker  ContainerKind = "docker"
	ContainerPodman  ContainerKind = "podman"
	ContainerLXC     ContainerKind = "lxc"
	ContainerOther   ContainerKind = "other"
	ContainerUnknown ContainerKind = "unknown"
)

// Valid reports whether the kind is defined by the schema.
func (k ContainerKind) Valid() bool {
	switch k {
	case ContainerNone, ContainerDocker, ContainerPodman, ContainerLXC, ContainerOther, ContainerUnknown:
		return true
	}
	return false
}

// VirtualizationFacts records execution-context facts that change what
// acceleration is plausible.
//
// A container without the GPU device nodes bind-mounted has no accelerator
// regardless of what the host has, and a translated process on Apple Silicon
// is not a native Apple Silicon path
// (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §4).
type VirtualizationFacts struct {
	Container ContainerKind `json:"container"`
	WSL       bool          `json:"wsl,omitempty"`
	// VirtualMachine is a tri-state: unknown virtualisation is common.
	VirtualMachine *bool `json:"virtual_machine,omitempty"`
	// NativeArchitecture is false when the process is observed to be running
	// translated (Rosetta). Nil means it was not established.
	NativeArchitecture *bool  `json:"native_architecture,omitempty"`
	Detail             string `json:"detail,omitempty"`
}

// AcceleratorVendor is the vendor of a discovered accelerator device.
type AcceleratorVendor string

const (
	VendorApple   AcceleratorVendor = "apple"
	VendorNVIDIA  AcceleratorVendor = "nvidia"
	VendorAMD     AcceleratorVendor = "amd"
	VendorIntel   AcceleratorVendor = "intel"
	VendorOther   AcceleratorVendor = "other"
	VendorUnknown AcceleratorVendor = "unknown"
)

// Valid reports whether the vendor is defined by the schema.
func (v AcceleratorVendor) Valid() bool {
	switch v {
	case VendorApple, VendorNVIDIA, VendorAMD, VendorIntel, VendorOther, VendorUnknown:
		return true
	}
	return false
}

// AcceleratorClass is how the device's memory relates to system memory.
type AcceleratorClass string

const (
	AcceleratorDiscrete   AcceleratorClass = "discrete"
	AcceleratorIntegrated AcceleratorClass = "integrated"
	// AcceleratorUnified is Apple Silicon's shared memory architecture,
	// where weights and KV cache compete with the OS and the build tools
	// (docs/MODEL_RUNTIME.md §10).
	AcceleratorUnified AcceleratorClass = "unified"
	AcceleratorUnknown AcceleratorClass = "unknown"
)

// Valid reports whether the class is defined by the schema.
func (c AcceleratorClass) Valid() bool {
	switch c {
	case AcceleratorDiscrete, AcceleratorIntegrated, AcceleratorUnified, AcceleratorUnknown:
		return true
	}
	return false
}

// AcceleratorDevice is one discovered device.
//
// Discovery must not require vendor tooling to exist: a device is found from
// OS facts (sysfs PCI class/vendor identifiers, ioreg) and only *enriched*
// when a vendor tool happens to be installed
// (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §2). A device record
// therefore says nothing about whether any runtime can use it.
type AcceleratorDevice struct {
	// ID is a deterministic handle for this device within the profile, e.g.
	// "pci:0000:03:00.0" or "apple:gpu".
	ID     string            `json:"id"`
	Vendor AcceleratorVendor `json:"vendor"`
	Class  AcceleratorClass  `json:"class"`
	Name   string            `json:"name,omitempty"`
	// VendorID and DeviceID are the raw PCI identifiers when observed. They
	// are the durable identity a compatibility table can key on without
	// depending on marketing names.
	VendorID string `json:"vendor_id,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	// DriverInUse is the kernel driver bound to the device when observable
	// ("amdgpu", "nvidia", "i915"). A device with no bound driver is present
	// but not usable.
	DriverInUse string `json:"driver_in_use,omitempty"`
	// Architecture is a vendor architecture identifier when observed, such as
	// an AMD LLVM target ("gfx902"). It is the key ROCm support actually
	// depends on, which is why vendor identity alone cannot decide it.
	Architecture string `json:"architecture,omitempty"`
	MemoryBytes  *int64 `json:"memory_bytes,omitempty"`
	// Sources lists where this device's facts came from, sorted.
	Sources []string `json:"sources,omitempty"`
}

// Validate checks the device is interpretable.
func (d AcceleratorDevice) Validate() error {
	const kind = "MachineCapabilityProfile"
	if err := requireNonEmpty(kind, "environment.accelerators[].id", d.ID); err != nil {
		return err
	}
	if !d.Vendor.Valid() {
		return enumError(kind, "environment.accelerators[].vendor", string(d.Vendor),
			"apple", "nvidia", "amd", "intel", "other", "unknown")
	}
	if !d.Class.Valid() {
		return enumError(kind, "environment.accelerators[].class", string(d.Class),
			"discrete", "integrated", "unified", "unknown")
	}
	return nil
}

// DeviceNodeFacts records the existence and accessibility of a device node.
//
// Both halves matter. On Linux an unreadable /dev/kfd or /dev/dri/renderD128
// is the difference between "this machine could accelerate inference" and
// "this user cannot", and it is a permissions fact a later milestone can
// remediate — not a missing GPU.
type DeviceNodeFacts struct {
	Path     string `json:"path"`
	Present  bool   `json:"present"`
	Readable *bool  `json:"readable,omitempty"`
	Writable *bool  `json:"writable,omitempty"`
}

// SoftwareCategory groups the software inventory.
type SoftwareCategory string

const (
	// SoftwareEngineering is deterministic engineering tooling: Git, build
	// tools, container tooling. Its absence affects deterministic capability,
	// not cognition.
	SoftwareEngineering SoftwareCategory = "engineering"
	// SoftwareCognitionRuntime is a local inference runtime.
	SoftwareCognitionRuntime SoftwareCategory = "cognition_runtime"
	// SoftwareCognitionCLI is an authenticated coding/agent CLI.
	SoftwareCognitionCLI SoftwareCategory = "cognition_cli"
	// SoftwarePrincipalHost is an editor/agent frontend that can host a
	// principal. It is deliberately a separate category: a host is not a
	// cognition endpoint (docs/PRINCIPAL_HOSTS.md).
	SoftwarePrincipalHost SoftwareCategory = "principal_host"
	// SoftwareAcceleration is accelerator userspace tooling (vendor
	// utilities, Vulkan loaders). Its presence is enrichment, never proof.
	SoftwareAcceleration SoftwareCategory = "acceleration"
)

// Valid reports whether the category is defined by the schema.
func (c SoftwareCategory) Valid() bool {
	switch c {
	case SoftwareEngineering, SoftwareCognitionRuntime, SoftwareCognitionCLI,
		SoftwarePrincipalHost, SoftwareAcceleration:
		return true
	}
	return false
}

// VersionStatus is this build's judgement of an observed version string.
type VersionStatus string

const (
	VersionCompatible   VersionStatus = "compatible"
	VersionIncompatible VersionStatus = "incompatible"
	// VersionUnknown is the correct answer whenever the version could not be
	// read or this build has no compatibility statement about it. It is the
	// default, not a fallback to optimism.
	VersionUnknown VersionStatus = "unknown"
)

// Valid reports whether the status is defined by the schema.
func (s VersionStatus) Valid() bool {
	switch s {
	case VersionCompatible, VersionIncompatible, VersionUnknown:
		return true
	}
	return false
}

// SoftwarePresence is one entry of the software inventory.
//
// Installed is not usable. A binary on PATH says nothing about whether a
// server is running, a model exists, or a session is authenticated
// (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §7); that judgement belongs
// to CognitionEndpoint, which is a separate type on purpose.
type SoftwarePresence struct {
	// ID is a stable handle, e.g. "git", "ollama", "codex-cli", "vscode".
	ID        string           `json:"id"`
	Category  SoftwareCategory `json:"category"`
	Installed bool             `json:"installed"`
	// Path is the resolved executable or application path when found.
	Path string `json:"path,omitempty"`
	// Version is sanitised, length-bounded text taken from the tool's own
	// output. It is untrusted data (DCI-083).
	Version       string        `json:"version,omitempty"`
	VersionStatus VersionStatus `json:"version_status,omitempty"`
	Detail        string        `json:"detail,omitempty"`
}

// Validate checks the entry is interpretable.
func (p SoftwarePresence) Validate() error {
	const kind = "MachineCapabilityProfile"
	if err := requireNonEmpty(kind, "environment.software[].id", p.ID); err != nil {
		return err
	}
	if !p.Category.Valid() {
		return enumError(kind, "environment.software[].category", string(p.Category),
			"engineering", "cognition_runtime", "cognition_cli", "principal_host", "acceleration")
	}
	if p.VersionStatus != "" && !p.VersionStatus.Valid() {
		return enumError(kind, "environment.software[].version_status", string(p.VersionStatus),
			"compatible", "incompatible", "unknown")
	}
	if p.Version != "" && !p.Installed {
		return errs.New(errs.CategoryInvalidArgument,
			"MachineCapabilityProfile: environment.software[%s] reports a version but is not installed", p.ID)
	}
	return nil
}

// EnvironmentFacts is everything discovery observed about the machine.
//
// It contains no assessment. Two machines with identical EnvironmentFacts must
// produce identical AcceleratorCandidates, which is what makes the assessment
// layer a pure, testable function of facts rather than of the host it runs on.
type EnvironmentFacts struct {
	ObservedAt     Timestamp           `json:"observed_at"`
	Host           HostFacts           `json:"host"`
	CPU            CPUFacts            `json:"cpu"`
	Memory         MemoryFacts         `json:"memory"`
	Storage        []StorageFacts      `json:"storage,omitempty"`
	Virtualization VirtualizationFacts `json:"virtualization"`
	Accelerators   []AcceleratorDevice `json:"accelerators,omitempty"`
	DeviceNodes    []DeviceNodeFacts   `json:"device_nodes,omitempty"`
	Software       []SoftwarePresence  `json:"software,omitempty"`
	// Findings is the per-component probe record, including every probe that
	// could not establish its fact (DCI-104).
	Findings []DiscoveryFinding `json:"findings,omitempty"`
}

// Validate checks the facts are interpretable.
func (f EnvironmentFacts) Validate() error {
	const kind = "MachineCapabilityProfile"
	if !f.Host.Family.Valid() {
		return enumError(kind, "environment.host.family", string(f.Host.Family),
			"linux", "darwin", "windows", "unknown")
	}
	if err := requireNonEmpty(kind, "environment.host.arch", f.Host.Arch); err != nil {
		return err
	}
	if f.CPU.LogicalCores != nil && *f.CPU.LogicalCores < 1 {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: environment.cpu.logical_cores must be >= 1 when present", kind)
	}
	if f.CPU.PhysicalCores != nil && *f.CPU.PhysicalCores < 1 {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: environment.cpu.physical_cores must be >= 1 when present", kind)
	}
	if !f.Virtualization.Container.Valid() {
		return enumError(kind, "environment.virtualization.container", string(f.Virtualization.Container),
			"none", "docker", "podman", "lxc", "other", "unknown")
	}
	for _, s := range f.Storage {
		if err := requireNonEmpty(kind, "environment.storage[].path", s.Path); err != nil {
			return err
		}
	}
	for _, d := range f.Accelerators {
		if err := d.Validate(); err != nil {
			return err
		}
	}
	for _, n := range f.DeviceNodes {
		if err := requireNonEmpty(kind, "environment.device_nodes[].path", n.Path); err != nil {
			return err
		}
	}
	for _, s := range f.Software {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	for _, finding := range f.Findings {
		if err := finding.Validate(); err != nil {
			return err
		}
	}
	return nil
}

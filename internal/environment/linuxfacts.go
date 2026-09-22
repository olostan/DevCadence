package environment

import (
	"context"
	"strconv"
	"strings"

	"github.com/olostan/DevCadence/internal/protocol"
)

// Linux discovery reads the kernel's own view of the machine.
//
// The important design rule is negative: accelerator discovery must not be
// "run nvidia-smi, and if it is missing conclude there is no NVIDIA GPU"
// (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §2). A GPU is found from
// /sys/bus/pci/devices — class code and vendor identifier — which is present on
// every Linux machine whether or not any vendor userspace was ever installed.
// Vendor tools only *enrich* what sysfs already established.

// PCI class codes, as sysfs reports them (three-byte class/subclass/prog-if).
const (
	pciClassVGA                   = "0x030000"
	pciClassDisplayOther          = "0x038000"
	pciClass3DController          = "0x030200"
	pciClassProcessingAccelerator = "0x120000"
)

// PCI vendor identifiers for the accelerator vendors DevCadence distinguishes.
var pciVendors = map[string]protocol.AcceleratorVendor{
	"0x10de": protocol.VendorNVIDIA,
	"0x1002": protocol.VendorAMD,
	"0x1022": protocol.VendorAMD,
	"0x8086": protocol.VendorIntel,
}

// linuxDeviceNodes are the device paths whose existence and writability decide
// whether a discovered device is usable by this user.
var linuxDeviceNodes = []string{
	"/dev/kfd",            // AMD ROCm compute
	"/dev/dri/card0",      // DRM primary
	"/dev/dri/renderD128", // DRM render node (Vulkan/OpenCL compute)
	"/dev/dri/renderD129",
	"/dev/nvidia0",
	"/dev/nvidiactl",
	"/dev/nvidia-uvm",
	"/dev/accel/accel0",
}

func (d *Discoverer) discoverLinux(ctx context.Context, c *collector) {
	d.linuxHost(c)
	d.linuxCPU(c)
	d.linuxMemory(c)
	d.linuxVirtualization(c)
	d.linuxAccelerators(ctx, c)
	for _, path := range linuxDeviceNodes {
		d.deviceNode(c, path)
	}
}

func (d *Discoverer) linuxHost(c *collector) {
	const component = "host.identity"
	if lines, ok := d.readLines(c, component, "/etc/os-release"); ok {
		values := parseKeyValue(lines, "=")
		c.facts.Host.Distribution = values["ID"]
		c.facts.Host.DistributionVersion = values["VERSION_ID"]
		c.facts.Host.Version = values["PRETTY_NAME"]
		c.observe(component, "/etc/os-release")
	}
	if kernel, ok := d.readTrimmed("/proc/sys/kernel/osrelease"); ok {
		c.facts.Host.Kernel = sanitize(kernel, 256)
		c.observe("host.kernel", "/proc/sys/kernel/osrelease")
	} else {
		c.note("host.kernel", protocol.FindingAbsent, "/proc/sys/kernel/osrelease", "")
	}
}

func (d *Discoverer) linuxCPU(c *collector) {
	const component = "hardware.cpu"
	lines, ok := d.readLines(c, component, "/proc/cpuinfo")
	if !ok {
		return
	}
	logical := 0
	physicalPairs := map[string]bool{}
	currentPhysical := ""
	var flags []string
	for _, line := range lines {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "processor":
			logical++
		case "model name", "Model":
			if c.facts.CPU.Model == "" {
				c.facts.CPU.Model = sanitize(value, 256)
			}
		case "vendor_id", "CPU implementer":
			if c.facts.CPU.Vendor == "" {
				c.facts.CPU.Vendor = sanitize(value, 256)
			}
		case "physical id":
			currentPhysical = value
		case "core id":
			// Distinct (socket, core) pairs give the physical core count on
			// x86. A machine that publishes neither leaves the count absent
			// rather than reporting the logical count as physical.
			physicalPairs[currentPhysical+"/"+value] = true
		case "flags", "Features":
			if flags == nil {
				flags = strings.Fields(value)
			}
		}
	}
	if logical > 0 {
		c.facts.CPU.LogicalCores = &logical
	}
	if n := len(physicalPairs); n > 0 {
		c.facts.CPU.PhysicalCores = &n
	}
	c.facts.CPU.Features = filterCPUFeatures(flags)
	c.observe(component, "/proc/cpuinfo")
}

// interestingCPUFeatures is the bounded set of instruction-set facts that
// materially affect CPU inference throughput.
//
// The full flag list runs to hundreds of entries; recording all of them would
// bloat every profile and make the fingerprint sensitive to microcode noise
// without telling anyone anything useful.
var interestingCPUFeatures = map[string]bool{
	"avx": true, "avx2": true, "avx512f": true, "avx512_bf16": true,
	"avx_vnni": true, "f16c": true, "fma": true, "amx_bf16": true, "amx_int8": true,
	"neon": true, "asimd": true, "asimdhp": true, "sve": true, "i8mm": true, "bf16": true,
}

func filterCPUFeatures(flags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, flag := range flags {
		lowered := strings.ToLower(flag)
		if interestingCPUFeatures[lowered] && !seen[lowered] {
			seen[lowered] = true
			out = append(out, lowered)
		}
	}
	return out
}

func (d *Discoverer) linuxMemory(c *collector) {
	const component = "hardware.memory"
	lines, ok := d.readLines(c, component, "/proc/meminfo")
	if !ok {
		return
	}
	values := parseKeyValue(lines, ":")
	if total, ok := parseBytesFromKB(values["MemTotal"]); ok {
		c.facts.Memory.TotalBytes = &total
	}
	if available, ok := parseBytesFromKB(values["MemAvailable"]); ok {
		c.facts.Memory.AvailableBytes = &available
	}
	if swap, ok := parseBytesFromKB(values["SwapTotal"]); ok {
		c.facts.Memory.SwapTotalBytes = &swap
	}
	if c.facts.Memory.TotalBytes == nil {
		c.note(component, protocol.FindingMalformed, "/proc/meminfo", "no parsable MemTotal")
		return
	}
	c.observe(component, "/proc/meminfo")
}

func (d *Discoverer) linuxVirtualization(c *collector) {
	const component = "host.virtualization"
	facts := protocol.VirtualizationFacts{Container: protocol.ContainerNone}
	native := true
	facts.NativeArchitecture = &native

	if exists, _ := d.opts.Sys.Exists("/.dockerenv"); exists {
		facts.Container = protocol.ContainerDocker
		facts.Detail = "/.dockerenv present"
	} else if exists, _ := d.opts.Sys.Exists("/run/.containerenv"); exists {
		facts.Container = protocol.ContainerPodman
		facts.Detail = "/run/.containerenv present"
	} else if content, ok := d.readTrimmed("/proc/1/cgroup"); ok {
		switch {
		case strings.Contains(content, "docker"):
			facts.Container = protocol.ContainerDocker
			facts.Detail = "docker path in /proc/1/cgroup"
		case strings.Contains(content, "lxc"):
			facts.Container = protocol.ContainerLXC
			facts.Detail = "lxc path in /proc/1/cgroup"
		case strings.Contains(content, "containerd") || strings.Contains(content, "kubepods"):
			facts.Container = protocol.ContainerOther
			facts.Detail = "container runtime path in /proc/1/cgroup"
		}
	}

	if kernel := strings.ToLower(c.facts.Host.Kernel); kernel != "" {
		// WSL publishes itself in the kernel release string. It matters
		// because GPU device nodes there are provided by a translation layer
		// rather than by a native driver.
		if strings.Contains(kernel, "microsoft") || strings.Contains(kernel, "wsl") {
			facts.WSL = true
		}
	}
	if content, ok := d.readTrimmed("/proc/cpuinfo"); ok && strings.Contains(content, "hypervisor") {
		vm := true
		facts.VirtualMachine = &vm
	}
	c.facts.Virtualization = facts
	c.observe(component, "/proc /sys")
}

// linuxAccelerators enumerates PCI devices and, only then, enriches them.
func (d *Discoverer) linuxAccelerators(ctx context.Context, c *collector) {
	const component = "hardware.accelerators"
	const pciRoot = "/sys/bus/pci/devices"
	entries, err := d.opts.Sys.ReadDir(pciRoot)
	if err != nil {
		c.noteErr(component, pciRoot, err)
		// A machine with no enumerable PCI bus may still have an accelerator
		// behind a platform driver; that is recorded as an absent fact, not as
		// proof of absence.
		return
	}
	found := 0
	for _, entry := range entries {
		base := pciRoot + "/" + entry
		class, ok := d.readTrimmed(base + "/class")
		if !ok {
			continue
		}
		if !isAcceleratorClass(class) {
			continue
		}
		vendorID, _ := d.readTrimmed(base + "/vendor")
		deviceID, _ := d.readTrimmed(base + "/device")
		vendor, known := pciVendors[strings.ToLower(vendorID)]
		if !known {
			vendor = protocol.VendorOther
		}
		device := protocol.AcceleratorDevice{
			ID:       "pci:" + entry,
			Vendor:   vendor,
			Class:    protocol.AcceleratorUnknown,
			VendorID: sanitize(vendorID, 32),
			DeviceID: sanitize(deviceID, 32),
			Sources:  []string{pciRoot},
		}
		device.DriverInUse = d.linuxDriverName(base)
		// VRAM as reported by amdgpu. Its absence is normal and is not a
		// finding worth recording per device.
		if vram, ok := d.readTrimmed(base + "/mem_info_vram_total"); ok {
			if n, err := strconv.ParseInt(strings.TrimSpace(vram), 10, 64); err == nil && n > 0 {
				device.MemoryBytes = &n
			}
		}
		device.Class = classifyLinuxDeviceClass(device)
		c.facts.Accelerators = append(c.facts.Accelerators, device)
		found++
	}
	if found == 0 {
		c.note(component, protocol.FindingAbsent, pciRoot,
			"no pci device reported an accelerator class")
		return
	}
	c.observe(component, pciRoot)
	d.linuxAMDArchitecture(c)
	d.linuxNVIDIAEnrichment(ctx, c)
}

// linuxDriverName resolves the bound kernel driver from the device's uevent.
//
// sysfs also exposes the driver as a symlink, but reading it would need a
// readlink the SysProbe deliberately does not offer — following a symlink takes
// a probe somewhere the caller did not name (docs/SECURITY.md §6). The uevent
// file states "DRIVER=amdgpu" as plain text and is read through the same
// bounded read as everything else.
//
// An empty result means no driver is bound. That is a meaningful fact rather
// than a gap: a GPU with no driver is present and unusable.
func (d *Discoverer) linuxDriverName(base string) string {
	content, ok := d.readTrimmed(base + "/uevent")
	if !ok {
		return ""
	}
	for _, line := range strings.Split(content, "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "DRIVER="); found {
			return sanitize(value, 128)
		}
	}
	return ""
}

// classifyLinuxDeviceClass distinguishes a discrete card from an integrated one.
//
// The distinction is load-bearing for ROCm: AMD's integrated APU graphics are
// where ROCm support is most often absent or unreliable, while discrete cards
// are where it is most often fine. Dedicated VRAM is the available signal; when
// there is none, the class stays unknown rather than guessing.
func classifyLinuxDeviceClass(device protocol.AcceleratorDevice) protocol.AcceleratorClass {
	switch {
	case device.Vendor == protocol.VendorIntel && device.MemoryBytes == nil:
		// Intel graphics on a CPU package have no dedicated VRAM. Intel
		// discrete cards report some, and fall through to the check below.
		return protocol.AcceleratorIntegrated
	case device.MemoryBytes != nil && *device.MemoryBytes > 0:
		return protocol.AcceleratorDiscrete
	default:
		return protocol.AcceleratorUnknown
	}
}

// linuxAMDArchitecture reads the AMD compute topology for the GPU architecture.
//
// /sys/class/kfd exists only when the amdkfd driver is loaded, and its
// gfx_target_version is the identifier ROCm support actually depends on. This is
// why "the vendor is AMD" cannot decide ROCm suitability: the answer lives in
// the architecture, and if the topology is absent the architecture is unknown.
func (d *Discoverer) linuxAMDArchitecture(c *collector) {
	const component = "hardware.accelerators.amd_topology"
	const root = "/sys/class/kfd/kfd/topology/nodes"
	nodes, err := d.opts.Sys.ReadDir(root)
	if err != nil {
		status := classify(err)
		if status == protocol.FindingAbsent {
			c.note(component, protocol.FindingAbsent, root,
				"amdkfd compute topology is absent, so the gpu architecture is unknown")
			return
		}
		c.noteErr(component, root, err)
		return
	}
	var architectures []string
	for _, node := range nodes {
		lines, ok := d.readLines(c, component, root+"/"+node+"/properties")
		if !ok {
			continue
		}
		values := parseKeyValue(lines, " ")
		version := values["gfx_target_version"]
		if version == "" {
			continue
		}
		if target := formatGFXTarget(version); target != "" {
			architectures = append(architectures, target)
		}
	}
	if len(architectures) == 0 {
		c.note(component, protocol.FindingMalformed, root, "no node published a gfx_target_version")
		return
	}
	// The topology does not map nodes to PCI addresses in a form this probe
	// reads, so the architecture is attached to the AMD devices found. A
	// machine with two different AMD GPUs would get the first architecture on
	// both, which is why the value carries its source.
	applied := false
	for i := range c.facts.Accelerators {
		if c.facts.Accelerators[i].Vendor != protocol.VendorAMD {
			continue
		}
		c.facts.Accelerators[i].Architecture = architectures[0]
		c.facts.Accelerators[i].Sources = append(c.facts.Accelerators[i].Sources, root)
		applied = true
	}
	if applied {
		c.observe(component, root)
	}
}

// linuxNVIDIAEnrichment adds a device name and memory size when the vendor
// utility happens to be installed.
//
// It is enrichment only. The device was already discovered from sysfs, so a
// machine without nvidia-smi still reports its NVIDIA GPU — it simply reports
// it without a marketing name, and records that the driver utility is absent,
// which is itself the fact that matters for CUDA usability.
func (d *Discoverer) linuxNVIDIAEnrichment(ctx context.Context, c *collector) {
	const component = "hardware.accelerators.nvidia_utility"
	hasNVIDIA := false
	for _, device := range c.facts.Accelerators {
		if device.Vendor == protocol.VendorNVIDIA {
			hasNVIDIA = true
			break
		}
	}
	if !hasNVIDIA {
		return
	}
	outcome := d.run(ctx, ProbeCommand{
		Name:       "nvidia-smi-query",
		Executable: "nvidia-smi",
		Args:       []string{"--query-gpu=name,memory.total", "--format=csv,noheader,nounits"},
	})
	if !outcome.Succeeded() {
		c.note(component, outcome.Status, "nvidia-smi",
			"nvidia hardware is present but the vendor utility did not answer: "+outcome.Detail)
		return
	}
	rows := strings.Split(outcome.Stdout, "\n")
	index := 0
	for i := range c.facts.Accelerators {
		if c.facts.Accelerators[i].Vendor != protocol.VendorNVIDIA {
			continue
		}
		if index >= len(rows) {
			break
		}
		name, memory, found := strings.Cut(rows[index], ",")
		index++
		c.facts.Accelerators[i].Name = sanitize(name, 256)
		c.facts.Accelerators[i].Sources = append(c.facts.Accelerators[i].Sources, "nvidia-smi")
		if !found {
			continue
		}
		if mib, err := strconv.ParseInt(strings.TrimSpace(memory), 10, 64); err == nil && mib > 0 {
			bytes := mib * 1024 * 1024
			c.facts.Accelerators[i].MemoryBytes = &bytes
			c.facts.Accelerators[i].Class = protocol.AcceleratorDiscrete
		}
	}
	c.observe(component, "nvidia-smi")
}

func isAcceleratorClass(class string) bool {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case pciClassVGA, pciClassDisplayOther, pciClass3DController, pciClassProcessingAccelerator:
		return true
	}
	return false
}

// formatGFXTarget renders amdkfd's numeric gfx_target_version as the gfx
// identifier ROCm documentation uses: 90200 becomes "gfx902".
func formatGFXTarget(raw string) string {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return ""
	}
	major := value / 10000
	minor := (value / 100) % 100
	step := value % 100
	// The step is hexadecimal in AMD's identifiers (gfx90a, gfx1100).
	return "gfx" + strconv.Itoa(major) + strconv.FormatInt(int64(minor), 16) + strconv.FormatInt(int64(step), 16)
}

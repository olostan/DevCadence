package environment

import (
	"context"
	"strconv"
	"strings"

	"github.com/olostan/DevCadience/internal/protocol"
)

// macOS discovery goes through sysctl and sw_vers rather than a pseudo
// filesystem, so almost every fact here needs a command probe. At
// DepthInventory nothing runs and the facts stay honestly empty — which is the
// right answer for a caller who asked for the cheapest possible inventory.
//
// Two macOS-specific facts matter more than the rest:
//
//   - sysctl.proc_translated tells us whether this process is running under
//     Rosetta. A translated process is not a native Apple Silicon path, and
//     treating the two as equivalent is exactly the mistake
//     docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §4 warns about.
//   - the GPU shares unified memory with everything else, so the device is
//     recorded with class "unified" and no dedicated memory size. Reporting
//     system memory as GPU memory would suggest headroom that does not exist
//     (docs/MODEL_RUNTIME.md §10).

// darwinSysctl is one sysctl key discovery reads.
type darwinSysctl struct {
	name string
	key  string
}

var darwinSysctls = []darwinSysctl{
	{"darwin-cpu-brand", "machdep.cpu.brand_string"},
	{"darwin-logical-cores", "hw.logicalcpu"},
	{"darwin-physical-cores", "hw.physicalcpu"},
	{"darwin-memsize", "hw.memsize"},
	{"darwin-translated", "sysctl.proc_translated"},
	{"darwin-vmm", "kern.hv_vmm_present"},
}

func (d *Discoverer) discoverDarwin(ctx context.Context, c *collector) {
	values := d.darwinSysctlValues(ctx, c)
	d.darwinHost(ctx, c)
	d.darwinCPU(c, values)
	d.darwinMemory(c, values)
	d.darwinVirtualization(c, values)
	d.darwinAccelerators(c, values)
}

// darwinSysctlValues reads each key as its own bounded probe.
//
// One key per invocation rather than a single batched call: a key that does not
// exist on this hardware makes sysctl exit nonzero, and batching would lose
// every other value along with it. sysctl.proc_translated, in particular, is
// absent on Intel Macs.
func (d *Discoverer) darwinSysctlValues(ctx context.Context, c *collector) map[string]string {
	out := map[string]string{}
	if !d.canRunCommands() {
		c.note("hardware.cpu", protocol.FindingUnsupported, "sysctl",
			"probe depth "+string(d.opts.Depth)+" does not permit running sysctl")
		return out
	}
	for _, entry := range darwinSysctls {
		outcome := d.run(ctx, ProbeCommand{
			Name: entry.name, Executable: "sysctl", Args: []string{"-n", entry.key},
		})
		if !outcome.Succeeded() || outcome.Stdout == "" {
			continue
		}
		out[entry.key] = outcome.Stdout
	}
	return out
}

func (d *Discoverer) darwinHost(ctx context.Context, c *collector) {
	const component = "host.identity"
	outcome := d.run(ctx, ProbeCommand{
		Name: "darwin-product-version", Executable: "sw_vers", Args: []string{"-productVersion"},
	})
	if outcome.Succeeded() && outcome.Stdout != "" {
		c.facts.Host.Version = sanitize(outcome.Stdout, 64)
		c.observe(component, "sw_vers")
	} else {
		c.note(component, outcome.Status, "sw_vers", outcome.Detail)
	}
	kernel := d.run(ctx, ProbeCommand{Name: "darwin-kernel", Executable: "uname", Args: []string{"-r"}})
	if kernel.Succeeded() {
		c.facts.Host.Kernel = sanitize(kernel.Stdout, 64)
		c.observe("host.kernel", "uname -r")
	} else {
		c.note("host.kernel", kernel.Status, "uname -r", kernel.Detail)
	}
}

func (d *Discoverer) darwinCPU(c *collector, values map[string]string) {
	const component = "hardware.cpu"
	brand := values["machdep.cpu.brand_string"]
	if brand != "" {
		c.facts.CPU.Model = sanitize(brand, 256)
	}
	if strings.HasPrefix(strings.ToLower(brand), "apple") {
		c.facts.CPU.Vendor = "apple"
	}
	if n, ok := parsePositiveInt(values["hw.logicalcpu"]); ok {
		c.facts.CPU.LogicalCores = &n
	}
	if n, ok := parsePositiveInt(values["hw.physicalcpu"]); ok {
		c.facts.CPU.PhysicalCores = &n
	}
	if strings.HasPrefix(d.opts.Arch, "arm") {
		// Every Apple Silicon core has NEON. This is an architectural
		// guarantee rather than an inference from a marketing name.
		c.facts.CPU.Features = append(c.facts.CPU.Features, "neon")
	}
	if c.facts.CPU.Model == "" {
		c.note(component, protocol.FindingAbsent, "sysctl machdep.cpu.brand_string", "")
		return
	}
	c.observe(component, "sysctl")
}

func (d *Discoverer) darwinMemory(c *collector, values map[string]string) {
	const component = "hardware.memory"
	if total, err := strconv.ParseInt(values["hw.memsize"], 10, 64); err == nil && total > 0 {
		c.facts.Memory.TotalBytes = &total
		c.observe(component, "sysctl hw.memsize")
	} else {
		c.note(component, protocol.FindingAbsent, "sysctl hw.memsize", "")
	}
	// Available memory on macOS requires interpreting vm_stat page counts, and
	// the interpretation changes between releases. An unverified number here
	// would be worse than none, since a scheduler would treat it as a
	// measurement: it is left absent with an explicit finding.
	c.note("hardware.memory.available", protocol.FindingUnsupported, "vm_stat",
		"available memory is not derived on this platform; only total memory is reported")
}

func (d *Discoverer) darwinVirtualization(c *collector, values map[string]string) {
	facts := protocol.VirtualizationFacts{Container: protocol.ContainerNone}
	if values["kern.hv_vmm_present"] == "1" {
		vm := true
		facts.VirtualMachine = &vm
	}
	// "0" means a native process; "1" means Rosetta. An absent key is an Intel
	// Mac, where the question does not apply and the process is native.
	translated := values["sysctl.proc_translated"]
	native := translated != "1"
	facts.NativeArchitecture = &native
	if !native {
		facts.Detail = "process is running translated under Rosetta"
		c.note("host.virtualization", protocol.FindingObserved, "sysctl sysctl.proc_translated",
			"process is translated; this is not a native apple silicon execution path")
	}
	c.facts.Virtualization = facts
}

// darwinAccelerators records the Apple GPU when the CPU is an Apple part.
//
// The GPU is not enumerated from a device tree here: on Apple Silicon it is an
// integral part of the SoC, and the CPU brand plus the architecture establish
// its presence more reliably than parsing system_profiler output. On an Intel
// Mac the question is left explicitly unanswered rather than guessed.
func (d *Discoverer) darwinAccelerators(c *collector, values map[string]string) {
	const component = "hardware.accelerators"
	brand := strings.ToLower(values["machdep.cpu.brand_string"])
	appleSilicon := strings.HasPrefix(brand, "apple") && strings.HasPrefix(d.opts.Arch, "arm")
	if !appleSilicon {
		c.note(component, protocol.FindingUnsupported, "sysctl",
			"this build does not enumerate discrete gpus on intel macs")
		return
	}
	name := sanitize(values["machdep.cpu.brand_string"], 240)
	c.facts.Accelerators = append(c.facts.Accelerators, protocol.AcceleratorDevice{
		ID:     "apple:gpu",
		Vendor: protocol.VendorApple,
		// Unified, and with no MemoryBytes: weights, KV cache, the OS and the
		// build tools all draw on the same pool, so a dedicated figure would
		// be a fiction.
		Class:   protocol.AcceleratorUnified,
		Name:    name + " GPU",
		Sources: []string{"sysctl"},
	})
	c.observe(component, "sysctl machdep.cpu.brand_string")
}

func parsePositiveInt(value string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

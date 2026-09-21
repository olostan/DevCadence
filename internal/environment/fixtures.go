package environment

import (
	"context"
	"strconv"

	"github.com/olostan/DevCadience/internal/clock"
	"github.com/olostan/DevCadience/internal/protocol"
)

// Fixture machines.
//
// These are the machines the M3A test matrix runs on. They are exported so that
// internal/cognition, internal/principalhosts and the CLI tests exercise the
// same environments rather than each inventing a plausible-looking one, and so
// that a new fixture added for one subsystem becomes available to all of them.
//
// Every fixture is a complete, self-consistent machine: the sysfs entries, the
// device nodes and the installed software agree with each other. That matters
// because the assessment layer reads all three, and a fixture where the PCI
// device says AMD while the device nodes say NVIDIA would test nothing real.

// Fixture bundles the two probes describing one machine.
type Fixture struct {
	OS       protocol.OSFamily
	Arch     string
	Sys      FakeSysProbe
	Commands *FakeCommandProbe
}

// DiscoveryOptions returns Options that discover this fixture machine.
//
// StoragePaths is "/" only: every fixture describes its root filesystem, and a
// home directory would make the facts depend on the machine running the test.
func (f Fixture) DiscoveryOptions(clk clock.Clock, depth protocol.ProbeDepth) Options {
	return Options{
		OS:           f.OS,
		Arch:         f.Arch,
		Sys:          f.Sys,
		Commands:     f.Commands,
		Clock:        clk,
		Depth:        depth,
		StoragePaths: []string{"/"},
	}
}

// Discover runs discovery against the fixture at the given depth.
func (f Fixture) Discover(ctx context.Context, clk clock.Clock, depth protocol.ProbeDepth) (protocol.EnvironmentFacts, error) {
	discoverer, err := New(f.DiscoveryOptions(clk, depth))
	if err != nil {
		return protocol.EnvironmentFacts{}, err
	}
	return discoverer.Discover(ctx)
}

// BlankLinux is a Linux machine where essentially nothing could be observed:
// no /proc, no /sys, no software. It is the hardest case for the rule that a
// blank machine must still produce valid facts.
func BlankLinux() Fixture {
	return Fixture{
		OS:       protocol.OSLinux,
		Arch:     "amd64",
		Sys:      FakeSysProbe{},
		Commands: &FakeCommandProbe{},
	}
}

// LinuxCPUOnly is an ordinary server with no accelerator at all, Git installed
// and no cognition software.
func LinuxCPUOnly() Fixture {
	sys := linuxBase(8, 33554432 /* KiB -> 32 GiB */)
	sys.Dirs["/sys/bus/pci/devices"] = []string{"0000:00:1f.2"}
	sys.Files["/sys/bus/pci/devices/0000:00:1f.2/class"] = "0x010601\n"
	commands := &FakeCommandProbe{
		Installed: map[string]string{"git": "/usr/bin/git"},
		Outputs: map[string]ProbeOutcome{
			Key("git", "--version"): Observed("git version 2.51.0"),
		},
	}
	return Fixture{OS: protocol.OSLinux, Arch: "amd64", Sys: sys, Commands: commands}
}

// LinuxAMDIntegrated is the thin-machine reference of ADR-0011: 32 GB, an
// integrated AMD GPU whose architecture ROCm does not support, a writable DRM
// render node and an installed Vulkan loader. The correct assessment is ROCm
// uncertain, Vulkan supported, CPU available.
func LinuxAMDIntegrated() Fixture {
	sys := linuxBase(16, 33554432)
	sys.Dirs["/sys/bus/pci/devices"] = []string{"0000:06:00.0"}
	sys.Files["/sys/bus/pci/devices/0000:06:00.0/class"] = "0x030000\n"
	sys.Files["/sys/bus/pci/devices/0000:06:00.0/vendor"] = "0x1002\n"
	sys.Files["/sys/bus/pci/devices/0000:06:00.0/device"] = "0x15d8\n"
	sys.Files["/sys/bus/pci/devices/0000:06:00.0/uevent"] = "DRIVER=amdgpu\nPCI_CLASS=30000\n"
	// gfx90c: Renoir/Cezanne integrated graphics. amdkfd is loaded, so the
	// architecture is observable — and it is not in the ROCm-supported set.
	sys.Dirs["/sys/class/kfd/kfd/topology/nodes"] = []string{"0"}
	sys.Files["/sys/class/kfd/kfd/topology/nodes/0/properties"] = "cpu_cores_count 16\ngfx_target_version 90012\n"
	sys.Dirs["/dev/dri"] = []string{"card0", "renderD128"}
	commands := &FakeCommandProbe{
		Installed: map[string]string{
			"git":        "/usr/bin/git",
			"vulkaninfo": "/usr/bin/vulkaninfo",
			"ollama":     "/usr/local/bin/ollama",
		},
		Outputs: map[string]ProbeOutcome{
			Key("git", "--version"):    Observed("git version 2.51.0"),
			Key("ollama", "--version"): Observed("ollama version is 0.12.3"),
		},
	}
	return Fixture{OS: protocol.OSLinux, Arch: "amd64", Sys: sys, Commands: commands}
}

// LinuxAMDDiscreteROCm is an AMD machine whose architecture is ROCm-supported
// and whose compute device node is writable.
func LinuxAMDDiscreteROCm() Fixture {
	fixture := LinuxAMDIntegrated()
	fixture.Sys.Files["/sys/class/kfd/kfd/topology/nodes/0/properties"] = "gfx_target_version 110000\n"
	fixture.Sys.Files["/sys/bus/pci/devices/0000:06:00.0/mem_info_vram_total"] = "25753026560\n"
	fixture.Sys.Dirs["/dev"] = []string{"kfd"}
	return fixture
}

// LinuxAMDRenderNodeDenied is LinuxAMDIntegrated with the render node present
// but not writable by this user — a permissions problem, not a missing GPU.
func LinuxAMDRenderNodeDenied() Fixture {
	fixture := LinuxAMDIntegrated()
	fixture.Sys.Denied = map[string]bool{"/dev/dri/renderD128": true}
	return fixture
}

// LinuxNVIDIANoDriver has NVIDIA hardware with no kernel driver bound and no
// vendor utility installed. Discovery must still find the GPU.
func LinuxNVIDIANoDriver() Fixture {
	sys := linuxBase(24, 67108864 /* 64 GiB */)
	sys.Dirs["/sys/bus/pci/devices"] = []string{"0000:01:00.0"}
	sys.Files["/sys/bus/pci/devices/0000:01:00.0/class"] = "0x030000\n"
	sys.Files["/sys/bus/pci/devices/0000:01:00.0/vendor"] = "0x10de\n"
	sys.Files["/sys/bus/pci/devices/0000:01:00.0/device"] = "0x2684\n"
	sys.Files["/sys/bus/pci/devices/0000:01:00.0/uevent"] = "PCI_CLASS=30000\n"
	commands := &FakeCommandProbe{
		Installed: map[string]string{"git": "/usr/bin/git"},
		Outputs:   map[string]ProbeOutcome{Key("git", "--version"): Observed("git version 2.51.0")},
	}
	return Fixture{OS: protocol.OSLinux, Arch: "amd64", Sys: sys, Commands: commands}
}

// LinuxNVIDIAReady has NVIDIA hardware, a bound driver, a writable control
// device, the vendor utility and Ollama.
func LinuxNVIDIAReady() Fixture {
	fixture := LinuxNVIDIANoDriver()
	fixture.Sys.Files["/sys/bus/pci/devices/0000:01:00.0/uevent"] = "DRIVER=nvidia\nPCI_CLASS=30000\n"
	fixture.Sys.Dirs["/dev"] = []string{"nvidia0", "nvidiactl", "nvidia-uvm"}
	fixture.Commands.Installed["nvidia-smi"] = "/usr/bin/nvidia-smi"
	fixture.Commands.Installed["ollama"] = "/usr/local/bin/ollama"
	fixture.Commands.Outputs[Key("nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits")] =
		Observed("NVIDIA GeForce RTX 4090, 24564")
	fixture.Commands.Outputs[Key("nvidia-smi", "--version")] = Observed("NVIDIA-SMI version : 570.86.15")
	fixture.Commands.Outputs[Key("ollama", "--version")] = Observed("ollama version is 0.12.3")
	return fixture
}

// LinuxMalformed is a machine whose kernel files are unreadable or garbage, and
// whose vendor utility emits nonsense. Every component must degrade on its own.
func LinuxMalformed() Fixture {
	sys := FakeSysProbe{
		Files: map[string]string{
			"/etc/os-release":                          "this file is not key=value at all\n",
			"/proc/meminfo":                            "MemTotal: not-a-number\nSwapTotal: also-not\n",
			"/proc/sys/kernel/osrelease":               "6.18.0-generic\n",
			"/sys/bus/pci/devices/0000:01:00.0/class":  "0x030000\n",
			"/sys/bus/pci/devices/0000:01:00.0/vendor": "0x10de\n",
			"/sys/bus/pci/devices/0000:01:00.0/uevent": "DRIVER=nvidia\n",
		},
		Dirs: map[string][]string{
			"/sys/bus/pci/devices": {"0000:01:00.0"},
			"/dev":                 {"nvidiactl"},
		},
		Unreadable: map[string]bool{"/proc/cpuinfo": true},
		Disk:       map[string][2]int64{"/": {500 << 30, 100 << 30}},
	}
	commands := &FakeCommandProbe{
		Installed: map[string]string{"nvidia-smi": "/usr/bin/nvidia-smi", "git": "/usr/bin/git"},
		Outputs: map[string]ProbeOutcome{
			Key("nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits"): Failed(9, "Unable to determine the device handle"),
			Key("git", "--version"): Observed("git: the future is not evenly distributed"),
		},
	}
	return Fixture{OS: protocol.OSLinux, Arch: "amd64", Sys: sys, Commands: commands}
}

// DarwinAppleSilicon is the strong-local reference profile: Apple Silicon,
// native execution, Ollama installed.
func DarwinAppleSilicon() Fixture {
	sys := FakeSysProbe{
		Files: map[string]string{},
		Dirs:  map[string][]string{},
		Disk:  map[string][2]int64{"/": {994 << 30, 400 << 30}},
	}
	commands := &FakeCommandProbe{
		Installed: map[string]string{
			"sysctl":  "/usr/sbin/sysctl",
			"sw_vers": "/usr/bin/sw_vers",
			"uname":   "/usr/bin/uname",
			"git":     "/usr/bin/git",
			"ollama":  "/usr/local/bin/ollama",
		},
		Outputs: map[string]ProbeOutcome{
			Key("sysctl", "-n", "machdep.cpu.brand_string"): Observed("Apple M3 Max"),
			Key("sysctl", "-n", "hw.logicalcpu"):            Observed("14"),
			Key("sysctl", "-n", "hw.physicalcpu"):           Observed("14"),
			Key("sysctl", "-n", "hw.memsize"):               Observed("51539607552"),
			Key("sysctl", "-n", "sysctl.proc_translated"):   Observed("0"),
			Key("sw_vers", "-productVersion"):               Observed("15.6"),
			Key("uname", "-r"):                              Observed("24.6.0"),
			Key("git", "--version"):                         Observed("git version 2.51.0"),
			Key("ollama", "--version"):                      Observed("ollama version is 0.12.3"),
		},
	}
	return Fixture{OS: protocol.OSDarwin, Arch: "arm64", Sys: sys, Commands: commands}
}

// DarwinTranslated is Apple Silicon hardware running this process under
// Rosetta. Metal must not be reported as a supported candidate.
func DarwinTranslated() Fixture {
	fixture := DarwinAppleSilicon()
	fixture.Commands.Outputs[Key("sysctl", "-n", "sysctl.proc_translated")] = Observed("1")
	return fixture
}

// UnknownOS is a machine on an operating system this build has not integrated.
func UnknownOS() Fixture {
	return Fixture{
		OS:       protocol.OSUnknown,
		Arch:     "riscv64",
		Sys:      FakeSysProbe{},
		Commands: &FakeCommandProbe{},
	}
}

// linuxBase builds the /proc files every Linux fixture shares.
func linuxBase(logicalCores int, memTotalKB int64) FakeSysProbe {
	cpuinfo := ""
	for i := 0; i < logicalCores; i++ {
		cpuinfo += "processor\t: " + strconv.Itoa(i) + "\n" +
			"vendor_id\t: AuthenticAMD\n" +
			"model name\t: AMD Ryzen 9 fixture\n" +
			"physical id\t: 0\n" +
			"core id\t: " + strconv.Itoa(i/2) + "\n" +
			"flags\t\t: fpu vme avx avx2 f16c fma\n\n"
	}
	return FakeSysProbe{
		Files: map[string]string{
			"/etc/os-release": "ID=debian\nVERSION_ID=\"13\"\nPRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\n",
			"/proc/cpuinfo":   cpuinfo,
			"/proc/meminfo": "MemTotal:       " + strconv.FormatInt(memTotalKB, 10) + " kB\n" +
				"MemAvailable:   " + strconv.FormatInt(memTotalKB/2, 10) + " kB\n" +
				"SwapTotal:             0 kB\n",
			"/proc/sys/kernel/osrelease": "6.18.0-generic\n",
		},
		Dirs: map[string][]string{},
		Disk: map[string][2]int64{"/": {500 << 30, 200 << 30}},
	}
}

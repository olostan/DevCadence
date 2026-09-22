package environment

import (
	"sort"
	"strconv"
	"strings"

	"github.com/olostan/DevCadience/internal/protocol"
)

// Backend-candidate assessment: the first place this package stops reporting
// facts and starts forming a judgement.
//
// The judgement is a pure function of EnvironmentFacts. It takes no clock, no
// probe and no host state, so the same facts always produce the same candidates
// and a fixture machine is assessed identically on any test runner.
//
// The ceiling on what this function may claim is low by design. It answers
// "could this backend plausibly work here, and what is missing?" — never "this
// backend works". The highest state it can produce is StateRuntimeAvailable:
// reaching StateVerified requires a real inference probe, and that happens in
// internal/cognition (DCI-106).

// KnowledgeRevision versions the backend-compatibility knowledge below.
//
// GPU and runtime support changes constantly, so the mapping from a device to a
// plausible backend is versioned knowledge rather than architecture (ADR-0011
// §5). Every candidate records the revision that produced it, so a later
// revision reaching a different conclusion about the same machine is
// explainable instead of mysterious.
const KnowledgeRevision = "backend-compatibility/2026-09-21"

// rocmSupportedArchitectures are the AMD GPU architectures this build is
// willing to call ROCm-supported.
//
// The list is short on purpose and errs towards "uncertain". An AMD device is
// not evidence of ROCm suitability: integrated Vega-class APU graphics
// (gfx90c, gfx902, gfx1035 and neighbours) are frequently unsupported or
// unreliable even when the packages install cleanly, while remaining perfectly
// good Vulkan inference devices (docs/MODEL_RUNTIME.md §11). Claiming ROCm for
// them would send a user to install a stack that then falls back to CPU.
var rocmSupportedArchitectures = map[string]bool{
	"gfx90a":  true, // CDNA 2 (MI200 series)
	"gfx942":  true, // CDNA 3 (MI300 series)
	"gfx1030": true, // RDNA 2 (RX 6800/6900)
	"gfx1100": true, // RDNA 3 (RX 7900)
	"gfx1101": true,
	"gfx1102": true,
}

// AssessBackends derives the accelerator candidates implied by the facts.
//
// The CPU candidate is always present and always last: CPU inference is slow
// but never unavailable, and a machine with no accelerator still has a valid
// (if unattractive) backend. Ordering is deterministic — accelerated candidates
// first, sorted by backend name — so serialised profiles are stable.
func AssessBackends(facts protocol.EnvironmentFacts) []protocol.AcceleratorCandidate {
	installed := installedSoftware(facts)
	var candidates []protocol.AcceleratorCandidate

	for _, device := range facts.Accelerators {
		switch device.Vendor {
		case protocol.VendorApple:
			candidates = append(candidates, appleCandidates(facts, device, installed)...)
		case protocol.VendorNVIDIA:
			candidates = append(candidates, nvidiaCandidate(facts, device, installed))
		case protocol.VendorAMD:
			candidates = append(candidates, amdCandidates(facts, device, installed)...)
		case protocol.VendorIntel, protocol.VendorOther, protocol.VendorUnknown:
			candidates = append(candidates, vulkanCandidate(facts, device, installed,
				"vendor "+string(device.Vendor)+" has no runtime-specific path in this build"))
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Backend != candidates[j].Backend {
			return candidates[i].Backend < candidates[j].Backend
		}
		return candidates[i].DeviceID < candidates[j].DeviceID
	})
	candidates = append(candidates, cpuCandidate())
	for i := range candidates {
		candidates[i].KnowledgeRevision = KnowledgeRevision
		sort.Strings(candidates[i].RequiredSoftware)
	}
	return candidates
}

// appleCandidates assesses the Metal path.
//
// Native execution is a hard requirement, not a preference. A translated
// process does not reach the GPU through Metal in the way a native one does, so
// a Rosetta process is reported as an unsupported Metal candidate rather than
// as a supported one that will mysteriously underperform.
func appleCandidates(facts protocol.EnvironmentFacts, device protocol.AcceleratorDevice, installed map[string]protocol.SoftwarePresence) []protocol.AcceleratorCandidate {
	candidate := protocol.AcceleratorCandidate{
		Backend:  protocol.BackendMetal,
		DeviceID: device.ID,
		Support:  protocol.SupportSupported,
		State:    protocol.StateCandidate,
		Reasons:  []string{"apple silicon gpu is present"},
	}
	if native := facts.Virtualization.NativeArchitecture; native != nil && !*native {
		candidate.Support = protocol.SupportUnsupported
		candidate.State = protocol.StateUnsupported
		candidate.Reasons = append(candidate.Reasons,
			"the process is running translated, which is not a native apple silicon execution path")
		return []protocol.AcceleratorCandidate{candidate}
	}
	candidate.Reasons = append(candidate.Reasons, "native architecture execution confirmed")

	// A runtime that can drive Metal must exist before the state may advance
	// past "candidate". Either of the two supported local runtimes qualifies.
	runtimes := presentRuntimes(installed)
	if len(runtimes) == 0 {
		candidate.RequiredSoftware = []string{"ollama", "mlx-lm"}
		candidate.Reasons = append(candidate.Reasons,
			"no local inference runtime is installed, so no runtime can drive metal yet")
		return []protocol.AcceleratorCandidate{candidate}
	}
	candidate.State = protocol.StateRuntimeAvailable
	candidate.Reasons = append(candidate.Reasons,
		"installed local runtime(s): "+strings.Join(runtimes, ", "))
	return []protocol.AcceleratorCandidate{candidate}
}

// nvidiaCandidate assesses the CUDA path.
//
// The four states this keeps apart are exactly the ones
// docs/MODEL_RUNTIME.md §11 asks for: hardware present, driver usable, runtime
// available, inference actually offloading. A full CUDA development toolkit is
// never required — the runtimes DevCadience adapts need a driver, not an SDK.
func nvidiaCandidate(facts protocol.EnvironmentFacts, device protocol.AcceleratorDevice, installed map[string]protocol.SoftwarePresence) protocol.AcceleratorCandidate {
	candidate := protocol.AcceleratorCandidate{
		Backend:  protocol.BackendCUDA,
		DeviceID: device.ID,
		Support:  protocol.SupportUncertain,
		State:    protocol.StateCandidate,
		Reasons:  []string{"nvidia device is present"},
	}
	driverBound := device.DriverInUse == "nvidia" || device.DriverInUse == "nvidia_drm"
	controlNode := deviceNodeUsable(facts, "/dev/nvidiactl")
	switch {
	case driverBound && controlNode:
		candidate.Support = protocol.SupportSupported
		candidate.Reasons = append(candidate.Reasons,
			"the proprietary nvidia driver is bound and /dev/nvidiactl is writable")
	case driverBound:
		candidate.Reasons = append(candidate.Reasons,
			"the nvidia driver is bound but its control device is not usable by this user")
		candidate.RequiredSoftware = append(candidate.RequiredSoftware, "nvidia-driver-device-access")
	default:
		candidate.Reasons = append(candidate.Reasons,
			"no nvidia kernel driver is bound to the device, so the hardware is present but unusable")
		candidate.RequiredSoftware = append(candidate.RequiredSoftware, "nvidia-driver")
		return candidate
	}
	if _, ok := installed["nvidia-smi"]; !ok {
		// Absent vendor utility does not downgrade support: the driver, not
		// the utility, is what inference needs. It is recorded because it
		// limits what can later be corroborated.
		candidate.Reasons = append(candidate.Reasons,
			"the nvidia-smi utility is absent, so vendor telemetry cannot corroborate offload")
	}
	runtimes := presentRuntimes(installed)
	if len(runtimes) == 0 {
		candidate.RequiredSoftware = append(candidate.RequiredSoftware, "ollama")
		candidate.Reasons = append(candidate.Reasons, "no local inference runtime is installed")
		return candidate
	}
	if candidate.Support == protocol.SupportSupported {
		candidate.State = protocol.StateRuntimeAvailable
		candidate.Reasons = append(candidate.Reasons,
			"installed local runtime(s): "+strings.Join(runtimes, ", "))
	}
	return candidate
}

// amdCandidates assesses ROCm and Vulkan separately.
//
// Both are returned when both are plausible, because they are genuinely
// different answers for the same device and the right one depends on facts this
// layer cannot settle. The AMD integrated case — ROCm uncertain, Vulkan viable,
// CPU fallback available — is a correct and expected result, not a gap.
func amdCandidates(facts protocol.EnvironmentFacts, device protocol.AcceleratorDevice, installed map[string]protocol.SoftwarePresence) []protocol.AcceleratorCandidate {
	rocm := protocol.AcceleratorCandidate{
		Backend:  protocol.BackendROCm,
		DeviceID: device.ID,
		Support:  protocol.SupportUnknown,
		State:    protocol.StateCandidate,
		Reasons:  []string{"amd device is present"},
	}
	switch {
	case device.Architecture == "":
		rocm.Support = protocol.SupportUnknown
		rocm.Reasons = append(rocm.Reasons,
			"the gpu architecture could not be observed, so rocm suitability is unknown; "+
				"vendor identity alone does not establish it")
		rocm.RequiredSoftware = append(rocm.RequiredSoftware, "amdkfd-compute-topology")
	case rocmSupportedArchitectures[device.Architecture]:
		rocm.Support = protocol.SupportSupported
		rocm.Reasons = append(rocm.Reasons,
			"architecture "+device.Architecture+" is in this build's rocm-supported set")
	default:
		// Not "unsupported": this build does not know, and an integrated
		// device that happens to work should not be told it cannot.
		rocm.Support = protocol.SupportUncertain
		rocm.Reasons = append(rocm.Reasons,
			"architecture "+device.Architecture+" is not in this build's rocm-supported set; "+
				"it may still work, and vulkan is the safer candidate")
	}
	if !deviceNodeUsable(facts, "/dev/kfd") {
		rocm.Reasons = append(rocm.Reasons,
			"/dev/kfd is missing or not writable by this user, so the rocm compute path is not usable as configured")
		rocm.RequiredSoftware = append(rocm.RequiredSoftware, "amdkfd-device-access")
		if rocm.Support == protocol.SupportSupported {
			rocm.Support = protocol.SupportUncertain
		}
	} else if rocm.Support == protocol.SupportSupported {
		if runtimes := presentRuntimes(installed); len(runtimes) > 0 {
			rocm.State = protocol.StateRuntimeAvailable
			rocm.Reasons = append(rocm.Reasons, "installed local runtime(s): "+strings.Join(runtimes, ", "))
		} else {
			rocm.RequiredSoftware = append(rocm.RequiredSoftware, "ollama")
			rocm.Reasons = append(rocm.Reasons, "no local inference runtime is installed")
		}
	}

	vulkan := vulkanCandidate(facts, device, installed, "amd devices are commonly usable through vulkan")
	return []protocol.AcceleratorCandidate{rocm, vulkan}
}

// vulkanCandidate assesses the portable Vulkan path.
//
// Vulkan availability is a device-and-loader question, not a package question:
// a Vulkan loader being installed proves nothing without a usable render node,
// and a usable render node proves nothing without a loader
// (docs/MODEL_RUNTIME.md §11).
func vulkanCandidate(facts protocol.EnvironmentFacts, device protocol.AcceleratorDevice, installed map[string]protocol.SoftwarePresence, why string) protocol.AcceleratorCandidate {
	candidate := protocol.AcceleratorCandidate{
		Backend:  protocol.BackendVulkan,
		DeviceID: device.ID,
		Support:  protocol.SupportUncertain,
		State:    protocol.StateCandidate,
		Reasons:  []string{why},
	}
	renderNode := deviceNodeUsable(facts, "/dev/dri/renderD128") || deviceNodeUsable(facts, "/dev/dri/renderD129")
	_, loader := installed["vulkaninfo"]
	switch {
	case renderNode && loader:
		candidate.Support = protocol.SupportSupported
		candidate.Reasons = append(candidate.Reasons,
			"a drm render node is writable and a vulkan loader utility is installed")
	case renderNode:
		candidate.Reasons = append(candidate.Reasons,
			"a drm render node is writable but no vulkan loader was found, so the runtime may not see the device")
		candidate.RequiredSoftware = append(candidate.RequiredSoftware, "vulkan-loader")
	case loader:
		candidate.Reasons = append(candidate.Reasons,
			"a vulkan loader is installed but no drm render node is writable by this user")
		candidate.RequiredSoftware = append(candidate.RequiredSoftware, "drm-render-node-access")
	default:
		candidate.Support = protocol.SupportUnknown
		candidate.Reasons = append(candidate.Reasons,
			"neither a writable drm render node nor a vulkan loader was found")
		candidate.RequiredSoftware = append(candidate.RequiredSoftware, "vulkan-loader", "drm-render-node-access")
	}
	if candidate.Support == protocol.SupportSupported {
		if runtimes := presentRuntimes(installed); len(runtimes) > 0 {
			candidate.State = protocol.StateRuntimeAvailable
			candidate.Reasons = append(candidate.Reasons, "installed local runtime(s): "+strings.Join(runtimes, ", "))
		} else {
			candidate.RequiredSoftware = append(candidate.RequiredSoftware, "ollama")
			candidate.Reasons = append(candidate.Reasons, "no local inference runtime is installed")
		}
	}
	return candidate
}

// cpuCandidate is the fallback every machine has.
//
// It is StateRuntimeAvailable rather than StateVerified: CPU execution is
// certain to be *available*, but whether a runtime is actually using it is
// still an observation about a specific endpoint, not a property of the
// machine.
func cpuCandidate() protocol.AcceleratorCandidate {
	return protocol.AcceleratorCandidate{
		Backend: protocol.BackendCPU,
		Support: protocol.SupportSupported,
		State:   protocol.StateRuntimeAvailable,
		Reasons: []string{"cpu execution is always available, though throughput may make it impractical"},
	}
}

// deviceNodeUsable reports whether a device node exists and is writable.
//
// Absent writability information (a platform that could not answer) counts as
// not usable. Assuming access on a machine that could not be asked would be the
// optimistic guess this whole layer exists to avoid.
func deviceNodeUsable(facts protocol.EnvironmentFacts, path string) bool {
	for _, node := range facts.DeviceNodes {
		if node.Path != path {
			continue
		}
		return node.Present && node.Writable != nil && *node.Writable
	}
	return false
}

// installedSoftware indexes the inventory by id, keeping only what is installed.
func installedSoftware(facts protocol.EnvironmentFacts) map[string]protocol.SoftwarePresence {
	out := make(map[string]protocol.SoftwarePresence, len(facts.Software))
	for _, entry := range facts.Software {
		if entry.Installed {
			out[entry.ID] = entry
		}
	}
	return out
}

// presentRuntimes lists installed local inference runtimes, sorted.
func presentRuntimes(installed map[string]protocol.SoftwarePresence) []string {
	var out []string
	for _, id := range []string{"mlx-lm", "ollama"} {
		if _, ok := installed[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// Fingerprint derives a stable identity for the machine from its facts.
//
// Only facts that change when the machine changes are included. Available
// memory, loaded models, filesystem free space and observation timestamps are
// excluded: a fingerprint that changed every minute could not distinguish "the
// same machine, observed again" from "the driver was upgraded and prior
// verification is stale", which is the one question it exists to answer
// (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §5).
func Fingerprint(facts protocol.EnvironmentFacts) (string, error) {
	type deviceIdentity struct {
		ID           string `json:"id"`
		Vendor       string `json:"vendor"`
		Class        string `json:"class"`
		VendorID     string `json:"vendor_id"`
		DeviceID     string `json:"device_id"`
		Driver       string `json:"driver"`
		Architecture string `json:"architecture"`
	}
	type softwareIdentity struct {
		ID        string `json:"id"`
		Installed bool   `json:"installed"`
		Version   string `json:"version"`
	}
	stable := struct {
		OS           string             `json:"os"`
		OSVersion    string             `json:"os_version"`
		Distribution string             `json:"distribution"`
		Kernel       string             `json:"kernel"`
		Arch         string             `json:"arch"`
		CPUModel     string             `json:"cpu_model"`
		LogicalCores string             `json:"logical_cores"`
		Features     []string           `json:"features"`
		TotalMemory  string             `json:"total_memory"`
		Container    string             `json:"container"`
		Native       string             `json:"native_architecture"`
		Devices      []deviceIdentity   `json:"devices"`
		Software     []softwareIdentity `json:"software"`
		Knowledge    string             `json:"knowledge_revision"`
		Inventory    string             `json:"inventory_revision"`
	}{
		OS:           string(facts.Host.Family),
		OSVersion:    facts.Host.Version,
		Distribution: facts.Host.Distribution + " " + facts.Host.DistributionVersion,
		Kernel:       facts.Host.Kernel,
		Arch:         facts.Host.Arch,
		CPUModel:     facts.CPU.Model,
		Features:     facts.CPU.Features,
		Container:    string(facts.Virtualization.Container),
		Knowledge:    KnowledgeRevision,
		Inventory:    InventoryRevision,
	}
	if facts.CPU.LogicalCores != nil {
		stable.LogicalCores = strconv.Itoa(*facts.CPU.LogicalCores)
	}
	if facts.Memory.TotalBytes != nil {
		stable.TotalMemory = strconv.FormatInt(*facts.Memory.TotalBytes, 10)
	}
	if facts.Virtualization.NativeArchitecture != nil {
		stable.Native = strconv.FormatBool(*facts.Virtualization.NativeArchitecture)
	}
	for _, device := range facts.Accelerators {
		stable.Devices = append(stable.Devices, deviceIdentity{
			ID: device.ID, Vendor: string(device.Vendor), Class: string(device.Class),
			VendorID: device.VendorID, DeviceID: device.DeviceID,
			Driver: device.DriverInUse, Architecture: device.Architecture,
		})
	}
	for _, entry := range facts.Software {
		// Uninstalled software is included so that installing something
		// changes the fingerprint; its version is empty either way.
		stable.Software = append(stable.Software, softwareIdentity{
			ID: entry.ID, Installed: entry.Installed, Version: entry.Version,
		})
	}
	sort.Slice(stable.Devices, func(i, j int) bool { return stable.Devices[i].ID < stable.Devices[j].ID })
	sort.Slice(stable.Software, func(i, j int) bool { return stable.Software[i].ID < stable.Software[j].ID })
	return protocol.Digest(stable)
}

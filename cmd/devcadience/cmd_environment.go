// Read-only inspection commands for M3A: environment facts, cognition
// endpoints, explicit probes and routing explanations.
//
// These are thin adapters in the same sense as every other command in this
// package: flag parsing and rendering only. Every judgement they print is made
// in internal/environment, internal/cognition or internal/principalhosts, so the
// same answers are available to the MCP layer and the future daemon without
// going through a terminal.
//
// All four commands are read-only. None installs anything, starts a service,
// downloads a model, writes configuration, authenticates, or mutates the machine
// in any way (DCI-108). `setup` and `doctor` — which do plan mutations — are M3B
// and deliberately absent.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/olostan/DevCadience/internal/clock"
	"github.com/olostan/DevCadience/internal/cognition"
	"github.com/olostan/DevCadience/internal/cognition/codingcli"
	"github.com/olostan/DevCadience/internal/cognition/mlx"
	"github.com/olostan/DevCadience/internal/cognition/ollama"
	"github.com/olostan/DevCadience/internal/environment"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/ids"
	"github.com/olostan/DevCadience/internal/principalhosts"
	"github.com/olostan/DevCadience/internal/protocol"
)

// parseDepth maps the -depth flag onto the probe-depth contract.
//
// The default is health everywhere except `cognition probe`, so an ordinary
// inspection never loads a model as a side effect: expensive verification is
// something a caller asks for explicitly (M3A performance rule).
func parseDepth(value string) (protocol.ProbeDepth, error) {
	depth := protocol.ProbeDepth(value)
	if !depth.Valid() {
		return "", errs.New(errs.CategoryInvalidArgument,
			"-depth %q is not one of inventory, health, inference", value)
	}
	return depth, nil
}

// discoverFacts runs environment discovery against the real machine.
func discoverFacts(ctx context.Context, depth protocol.ProbeDepth) (protocol.EnvironmentFacts, error) {
	opts, err := environment.NewDefaultOptions(clock.System(), depth)
	if err != nil {
		return protocol.EnvironmentFacts{}, err
	}
	discoverer, err := environment.New(opts)
	if err != nil {
		return protocol.EnvironmentFacts{}, err
	}
	return discoverer.Discover(ctx)
}

// buildProfile discovers the machine and its cognition endpoints.
//
// This function is the one place a provider is chosen. The core packages know
// only the Adapter contract, so adding or removing a runtime is an edit here
// (DCI-055).
func buildProfile(ctx context.Context, depth protocol.ProbeDepth) (protocol.MachineCapabilityProfile, error) {
	facts, err := discoverFacts(ctx, depth)
	if err != nil {
		return protocol.MachineCapabilityProfile{}, err
	}
	commands, err := environment.NewCommandProbe("")
	if err != nil {
		return protocol.MachineCapabilityProfile{}, err
	}
	transport, err := ollama.NewHTTPTransport("")
	if err != nil {
		return protocol.MachineCapabilityProfile{}, err
	}
	ollamaAdapter, err := ollama.New(ollama.Options{Transport: transport})
	if err != nil {
		return protocol.MachineCapabilityProfile{}, err
	}
	home, _ := os.UserHomeDir()
	mlxAdapter, err := mlx.New(mlx.Options{
		Commands: commands, Sys: environment.NewSysProbe(), HomeDir: home,
	})
	if err != nil {
		return protocol.MachineCapabilityProfile{}, err
	}
	cliAdapter, err := codingcli.New(codingcli.Options{Commands: commands})
	if err != nil {
		return protocol.MachineCapabilityProfile{}, err
	}
	// The remote-API adapter is not wired in: M3A ships the boundary and a
	// deterministic client, not a provider, and constructing one would need
	// credentials this command must not go looking for.
	service, err := cognition.NewService(cognition.Options{
		Adapters: []cognition.Adapter{ollamaAdapter, mlxAdapter, cliAdapter},
		Clock:    clock.System(),
		IDs:      ids.NewULIDSource(),
	})
	if err != nil {
		return protocol.MachineCapabilityProfile{}, err
	}
	return service.Profile(ctx, cognition.ProfileInput{
		Facts: facts, Depth: depth,
		Probe: cognition.ProbeRequest{RequireStructuredOutput: true},
	})
}

func runEnvironment(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadience environment <inspect>")
	}
	switch args[0] {
	case "inspect":
		return runEnvironmentInspect(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown environment subcommand %q", args[0])
	}
}

func runEnvironmentInspect(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("environment inspect", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the observed facts as JSON")
	depthFlag := fs.String("depth", string(protocol.DepthHealth),
		"probe depth: inventory (filesystem only) or health (also run version/health commands)")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	depth, err := parseDepth(*depthFlag)
	if err != nil {
		return err
	}
	// Inference depth is refused here on purpose: `environment inspect` must
	// stay cheap and must never load a model.
	if depth == protocol.DepthInference {
		return errs.New(errs.CategoryInvalidArgument,
			"environment inspect does not run inference; use `cognition probe` to verify an endpoint")
	}
	facts, err := discoverFacts(ctx, depth)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(e.stdout, facts)
	}
	renderFacts(e.stdout, facts)
	renderHosts(e.stdout, principalhosts.FromEnvironment(facts))
	renderCandidates(e.stdout, environment.AssessBackends(facts))
	return nil
}

func runCognition(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadience cognition <list|probe|route>")
	}
	switch args[0] {
	case "list":
		return runCognitionList(ctx, e, args[1:])
	case "probe":
		return runCognitionProbe(ctx, e, args[1:])
	case "route":
		return runCognitionRoute(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown cognition subcommand %q", args[0])
	}
}

func runCognitionList(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("cognition list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the machine capability profile as JSON")
	depthFlag := fs.String("depth", string(protocol.DepthHealth),
		"probe depth: inventory, health, or inference (runs a small synthetic inference on existing models)")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	depth, err := parseDepth(*depthFlag)
	if err != nil {
		return err
	}
	profile, err := buildProfile(ctx, depth)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(e.stdout, profile)
	}
	renderProfile(e.stdout, profile)
	return nil
}

func runCognitionProbe(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("cognition probe", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the probed endpoint as JSON")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errs.New(errs.CategoryInvalidArgument,
			"usage: devcadience cognition probe <endpoint-id>; run `cognition list` to see the ids")
	}
	wanted := fs.Arg(0)
	// Probing is inference depth by definition: this is the command that asks
	// for the expensive verification, using only models that already exist.
	profile, err := buildProfile(ctx, protocol.DepthInference)
	if err != nil {
		return err
	}
	for _, endpoint := range profile.Endpoints {
		if endpoint.ID != wanted {
			continue
		}
		if *asJSON {
			return writeJSON(e.stdout, endpoint)
		}
		renderEndpoint(e.stdout, endpoint, true)
		return nil
	}
	available := make([]string, 0, len(profile.Endpoints))
	for _, endpoint := range profile.Endpoints {
		available = append(available, endpoint.ID)
	}
	return errs.New(errs.CategoryNotFound,
		"no endpoint %q was discovered; available: %s", wanted, strings.Join(available, ", "))
}

func runCognitionRoute(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("cognition route", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the routing decisions as JSON")
	role := fs.String("role", "", "route one role only (scout, classifier, implementer, "+
		"correctness_reviewer, architecture_reviewer); empty routes all of them")
	depthFlag := fs.String("depth", string(protocol.DepthHealth), "probe depth used to discover endpoints")
	exposure := fs.String("source-exposure", string(protocol.ExposureFocusedSnippets),
		"maximum source exposure this project permits: local_only, semantic_evidence_only, "+
			"focused_snippets, selected_files, tool_mediated_worktree, unrestricted_authorized")
	maxCost := fs.String("max-cost", string(protocol.CostRemoteEconomy),
		"maximum cost class this project permits: local_compute, subscription_included, "+
			"remote_economy, remote_strong, frontier_expensive")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	depth, err := parseDepth(*depthFlag)
	if err != nil {
		return err
	}
	policy := cognition.Policy{
		MaxSourceExposure: protocol.SourceExposure(*exposure),
		MaxCostClass:      protocol.CostClass(*maxCost),
	}
	if !policy.MaxSourceExposure.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "-source-exposure %q is not a known class", *exposure)
	}
	if !policy.MaxCostClass.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "-max-cost %q is not a known class", *maxCost)
	}

	requirements := cognition.DefaultRequirements()
	if *role != "" {
		requirement, found := requirements[cognition.Role(*role)]
		if !found {
			return errs.New(errs.CategoryInvalidArgument, "-role %q is not a known role", *role)
		}
		requirements = map[cognition.Role]cognition.RoleRequirement{cognition.Role(*role): requirement}
	}

	profile, err := buildProfile(ctx, depth)
	if err != nil {
		return err
	}
	decisions := cognition.RouteAll(requirements, policy, profile.Endpoints)
	if *asJSON {
		return writeJSON(e.stdout, decisions)
	}
	fmt.Fprintf(e.stdout, "policy: source_exposure<=%s cost<=%s (probe depth %s)\n\n",
		policy.MaxSourceExposure, policy.MaxCostClass, depth)
	for _, decision := range decisions {
		fmt.Fprint(e.stdout, decision.Explain())
		fmt.Fprintln(e.stdout)
	}
	return nil
}

// Machine-readable output goes through the existing writeJSON in cmd_project.go,
// so every --json path in the CLI is formatted identically. The documents it
// emits are the protocol types themselves, which means a consumer validates
// against the published schema rather than parsing the human rendering.

func renderFacts(w io.Writer, facts protocol.EnvironmentFacts) {
	fmt.Fprintln(w, "Host")
	fmt.Fprintf(w, "  os            %s %s\n", facts.Host.Family, facts.Host.Arch)
	if facts.Host.Distribution != "" {
		fmt.Fprintf(w, "  distribution  %s %s\n", facts.Host.Distribution, facts.Host.DistributionVersion)
	}
	if facts.Host.Version != "" {
		fmt.Fprintf(w, "  version       %s\n", facts.Host.Version)
	}
	if facts.Host.Kernel != "" {
		fmt.Fprintf(w, "  kernel        %s\n", facts.Host.Kernel)
	}
	fmt.Fprintf(w, "  container     %s\n", facts.Virtualization.Container)
	if native := facts.Virtualization.NativeArchitecture; native != nil && !*native {
		fmt.Fprintln(w, "  architecture  translated (not a native execution path)")
	}

	fmt.Fprintln(w, "\nCPU")
	fmt.Fprintf(w, "  model         %s\n", orUnknown(facts.CPU.Model))
	fmt.Fprintf(w, "  cores         %s logical, %s physical\n",
		orUnknownInt(facts.CPU.LogicalCores), orUnknownInt(facts.CPU.PhysicalCores))
	if len(facts.CPU.Features) > 0 {
		fmt.Fprintf(w, "  features      %s\n", strings.Join(facts.CPU.Features, " "))
	}

	fmt.Fprintln(w, "\nMemory")
	fmt.Fprintf(w, "  total         %s\n", orUnknownBytes(facts.Memory.TotalBytes))
	fmt.Fprintf(w, "  available     %s\n", orUnknownBytes(facts.Memory.AvailableBytes))
	fmt.Fprintf(w, "  swap          %s\n", orUnknownBytes(facts.Memory.SwapTotalBytes))

	if len(facts.Storage) > 0 {
		fmt.Fprintln(w, "\nStorage")
		for _, entry := range facts.Storage {
			fmt.Fprintf(w, "  %-13s %s available of %s\n", entry.Path,
				orUnknownBytes(entry.AvailableBytes), orUnknownBytes(entry.TotalBytes))
		}
	}

	fmt.Fprintln(w, "\nAccelerators")
	if len(facts.Accelerators) == 0 {
		fmt.Fprintln(w, "  none detected")
	}
	for _, device := range facts.Accelerators {
		fmt.Fprintf(w, "  %s\n", device.ID)
		fmt.Fprintf(w, "    vendor      %s (%s)\n", device.Vendor, device.Class)
		if device.Name != "" {
			fmt.Fprintf(w, "    name        %s\n", device.Name)
		}
		if device.Architecture != "" {
			fmt.Fprintf(w, "    arch        %s\n", device.Architecture)
		}
		fmt.Fprintf(w, "    driver      %s\n", orUnknown(device.DriverInUse))
		if device.MemoryBytes != nil {
			fmt.Fprintf(w, "    memory      %s\n", orUnknownBytes(device.MemoryBytes))
		}
	}

	present := 0
	for _, node := range facts.DeviceNodes {
		if node.Present {
			present++
		}
	}
	if present > 0 {
		fmt.Fprintln(w, "\nDevice nodes")
		for _, node := range facts.DeviceNodes {
			if !node.Present {
				continue
			}
			fmt.Fprintf(w, "  %-22s writable=%s\n", node.Path, orUnknownBool(node.Writable))
		}
	}

	fmt.Fprintln(w, "\nSoftware")
	for _, entry := range facts.Software {
		status := "not installed"
		if entry.Installed {
			status = "installed"
			if entry.Version != "" {
				status += " " + entry.Version
			}
			if entry.VersionStatus == protocol.VersionIncompatible {
				status += " (below the supported floor)"
			}
		}
		fmt.Fprintf(w, "  %-14s %-12s %s\n", entry.ID, entry.Category, status)
	}

	// Findings that did not establish their fact are printed: an absent
	// observation the operator cannot see is indistinguishable from a bug.
	var unresolved []protocol.DiscoveryFinding
	for _, finding := range facts.Findings {
		if finding.Status != protocol.FindingObserved {
			unresolved = append(unresolved, finding)
		}
	}
	if len(unresolved) > 0 {
		fmt.Fprintln(w, "\nNot established")
		for _, finding := range unresolved {
			fmt.Fprintf(w, "  %-40s %s", finding.Component, finding.Status)
			if finding.Detail != "" {
				fmt.Fprintf(w, ": %s", finding.Detail)
			}
			fmt.Fprintln(w)
		}
	}
}

func renderHosts(w io.Writer, inventory principalhosts.Inventory) {
	fmt.Fprintln(w, "\nPrincipal hosts")
	if !inventory.Any() {
		fmt.Fprintln(w, "  none installed (a normal state; DevCadience does not require one)")
	}
	for _, host := range inventory.Hosts {
		if !host.Installed {
			continue
		}
		fmt.Fprintf(w, "  %-14s %s\n", host.ID, orUnknown(host.Version))
	}
}

func renderCandidates(w io.Writer, candidates []protocol.AcceleratorCandidate) {
	fmt.Fprintln(w, "\nBackend candidates")
	for _, candidate := range candidates {
		fmt.Fprintf(w, "  %-8s support=%-12s state=%s\n", candidate.Backend, candidate.Support, candidate.State)
		for _, reason := range candidate.Reasons {
			fmt.Fprintf(w, "      - %s\n", reason)
		}
		if len(candidate.RequiredSoftware) > 0 {
			fmt.Fprintf(w, "      missing: %s\n", strings.Join(candidate.RequiredSoftware, ", "))
		}
	}
	fmt.Fprintln(w, "\n  Assessment never reports a backend as verified; only an inference")
	fmt.Fprintln(w, "  probe can (`devcadience cognition probe <endpoint>`).")
}

func renderProfile(w io.Writer, profile protocol.MachineCapabilityProfile) {
	fmt.Fprintf(w, "machine      %s\n", profile.MachineFingerprint)
	fmt.Fprintf(w, "observed     %s (probe depth %s)\n", profile.ObservedAt, profile.ProbeDepth)
	fmt.Fprintf(w, "assessment   %s\n", profile.Assessment)
	if len(profile.Limitations) > 0 {
		fmt.Fprintln(w, "limitations")
		for _, limitation := range profile.Limitations {
			fmt.Fprintf(w, "  - %s\n", limitation)
		}
	}
	fmt.Fprintln(w, "\nCognition endpoints")
	if len(profile.Endpoints) == 0 {
		fmt.Fprintln(w, "  none discovered; deterministic control-plane capability is unaffected")
		return
	}
	for _, endpoint := range profile.Endpoints {
		renderEndpoint(w, endpoint, false)
	}
}

func renderEndpoint(w io.Writer, endpoint protocol.CognitionEndpoint, verbose bool) {
	fmt.Fprintf(w, "  %s\n", endpoint.ID)
	fmt.Fprintf(w, "    kind        %s (%s)\n", endpoint.Kind, endpoint.Locality)
	fmt.Fprintf(w, "    health      %s\n", endpoint.Health)
	fmt.Fprintf(w, "    auth        %s\n", endpoint.Auth)
	if endpoint.ModelID != "" {
		fmt.Fprintf(w, "    model       %s\n", endpoint.ModelID)
	}
	if endpoint.Version != "" {
		fmt.Fprintf(w, "    version     %s\n", endpoint.Version)
	}
	fmt.Fprintf(w, "    cost        %s\n", endpoint.CostClass)
	fmt.Fprintf(w, "    exposure    requires %s\n", endpoint.RequiredSourceExposure)
	fmt.Fprintf(w, "    structured  %s\n", endpoint.StructuredOutput)
	if endpoint.Acceleration != nil {
		fmt.Fprintf(w, "    accel       %s\n", cognition.Describe(endpoint.Acceleration))
		if verbose {
			for _, signal := range endpoint.Acceleration.Signals {
				fmt.Fprintf(w, "      %-12s %s offloaded=%t %s\n",
					signal.Trust, signal.Backend, signal.Offloaded, signal.Statement)
			}
		}
	}
	if len(endpoint.Capabilities) > 0 {
		fmt.Fprintln(w, "    capability")
		for _, capability := range endpoint.Capabilities {
			fmt.Fprintf(w, "      %-22s %s (%s)\n", capability.Dimension, capability.Grade, capability.Provenance)
		}
	} else {
		fmt.Fprintln(w, "    capability  ungraded (no measurement or declaration)")
	}
	if verbose && len(endpoint.Measurements) > 0 {
		fmt.Fprintln(w, "    measured")
		for _, measurement := range endpoint.Measurements {
			fmt.Fprintf(w, "      %-30s %.2f %s\n", measurement.Name, measurement.Value, measurement.Unit)
		}
	}
	findings := append([]protocol.DiscoveryFinding(nil), endpoint.Findings...)
	sort.Slice(findings, func(i, j int) bool { return findings[i].Component < findings[j].Component })
	for _, finding := range findings {
		if finding.Detail == "" {
			continue
		}
		fmt.Fprintf(w, "    note        %s\n", finding.Detail)
	}
}

func orUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func orUnknownInt(value *int) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *value)
}

func orUnknownBool(value *bool) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%t", *value)
}

// orUnknownBytes renders a byte count in binary units.
//
// Absence prints as "unknown" rather than "0 B": zero bytes of memory is not a
// possible observation, and printing it would make an unanswered probe look like
// a measurement.
func orUnknownBytes(value *int64) string {
	if value == nil {
		return "unknown"
	}
	bytes := float64(*value)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	index := 0
	for bytes >= 1024 && index < len(units)-1 {
		bytes /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%.0f %s", bytes, units[index])
	}
	return fmt.Sprintf("%.1f %s", bytes, units[index])
}

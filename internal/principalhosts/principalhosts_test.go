package principalhosts_test

import (
	"context"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/principalhosts"
	"github.com/olostan/DevCadence/internal/protocol"
)

func facts(t *testing.T, fixture environment.Fixture) protocol.EnvironmentFacts {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC), 0)
	observed, err := fixture.Discover(context.Background(), clk, protocol.DepthHealth)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	return observed
}

func hostByID(inventory principalhosts.Inventory, id principalhosts.HostID) principalhosts.Host {
	for _, host := range inventory.Hosts {
		if host.ID == id {
			return host
		}
	}
	return principalhosts.Host{}
}

// TestNoHostInstalledIsANormalState is the blank-machine case: DevCadence's
// control plane works without any principal host (DCI-107).
func TestNoHostInstalledIsANormalState(t *testing.T) {
	inventory := principalhosts.FromEnvironment(facts(t, environment.LinuxCPUOnly()))
	if inventory.Any() {
		t.Errorf("hosts were reported on a machine with none: %+v", inventory.Installed())
	}
	if len(inventory.Hosts) != len(principalhosts.SupportedHosts()) {
		t.Errorf("every supported host must be reported, installed or not: %+v", inventory.Hosts)
	}
	for _, host := range inventory.Hosts {
		if host.Detail == "" {
			t.Errorf("%s has no explanation of its absence", host.ID)
		}
		if host.VersionStatus != protocol.VersionUnknown {
			t.Errorf("%s version status = %q, want unknown", host.ID, host.VersionStatus)
		}
	}
}

// TestAnInstalledHostIsDetectedOnPathAndAsAnApplication covers both discovery
// routes, since a GUI editor is often installed without being on PATH.
func TestAnInstalledHostIsDetectedOnPathAndAsAnApplication(t *testing.T) {
	onPath := environment.LinuxCPUOnly()
	onPath.Commands.Installed["code"] = "/usr/bin/code"
	onPath.Commands.Outputs[environment.Key("code", "--version")] = environment.Observed("1.104.2\nabc123\nx64")
	inventory := principalhosts.FromEnvironment(facts(t, onPath))
	vscode := hostByID(inventory, principalhosts.HostVSCode)
	if !vscode.Installed || vscode.Version != "1.104.2" {
		t.Errorf("vscode = %+v", vscode)
	}
	if !inventory.Any() {
		t.Error("an installed host was not reported")
	}

	asApplication := environment.DarwinAppleSilicon()
	asApplication.Sys.Dirs["/Applications"] = []string{"Antigravity.app"}
	macInventory := principalhosts.FromEnvironment(facts(t, asApplication))
	antigravity := hostByID(macInventory, principalhosts.HostAntigravity)
	if !antigravity.Installed {
		t.Errorf("an application bundle not on PATH was missed: %+v", antigravity)
	}
	if antigravity.Path != "/Applications/Antigravity.app" {
		t.Errorf("path = %q", antigravity.Path)
	}
}

// TestAHostIsNotACognitionEndpoint is the conceptual separation stated as a
// test: the host inventory produces no endpoints and claims no model access.
func TestAHostIsNotACognitionEndpoint(t *testing.T) {
	fixture := environment.LinuxCPUOnly()
	fixture.Commands.Installed["code"] = "/usr/bin/code"
	fixture.Commands.Outputs[environment.Key("code", "--version")] = environment.Observed("1.104.2")
	observed := facts(t, fixture)

	inventory := principalhosts.FromEnvironment(observed)
	if !inventory.Any() {
		t.Fatal("the host was not detected")
	}
	// The software inventory must categorise it as a host, never as cognition.
	for _, entry := range observed.Software {
		if entry.ID == "vscode" && entry.Category != protocol.SoftwarePrincipalHost {
			t.Errorf("vscode category = %q, want principal_host", entry.Category)
		}
	}
	// Installing a host changes nothing about endpoints; that assertion lives
	// with the cognition adapters, which read only their own inventory entries.
	vscode := hostByID(inventory, principalhosts.HostVSCode)
	if vscode.VersionStatus != protocol.VersionUnknown {
		t.Errorf("a compatibility judgement was made before any integration exists: %q", vscode.VersionStatus)
	}
}

// TestOrderingIsDeterministic keeps rendered output stable.
func TestOrderingIsDeterministic(t *testing.T) {
	observed := facts(t, environment.LinuxCPUOnly())
	first := principalhosts.FromEnvironment(observed)
	second := principalhosts.FromEnvironment(observed)
	for i := range first.Hosts {
		if first.Hosts[i].ID != second.Hosts[i].ID {
			t.Fatal("host ordering is not stable")
		}
	}
	if first.Hosts[0].ID != principalhosts.HostAntigravity {
		t.Errorf("first host = %q, want the alphabetically first supported host", first.Hosts[0].ID)
	}
}

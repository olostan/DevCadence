// Package principalhosts reports which supported principal-host applications
// are installed.
//
// A principal host is a frontend a human drives — Antigravity, Cursor, Visual
// Studio Code — and it is emphatically *not* a cognition endpoint. Antigravity
// is a host that happens to reach a Gemini model; conflating the two would make
// "the user has Antigravity installed" look like "a Gemini endpoint is
// available", which is false in both directions: the host may be installed with
// no usable session, and the model may be reachable with no host at all
// (docs/PRINCIPAL_HOSTS.md, DCI-107).
//
// This package therefore produces facts only. It does not configure a host, does
// not install an integration and does not write to any host's settings — that is
// M3B and M4A. No-host-installed is a normal state.
package principalhosts

import (
	"sort"

	"github.com/olostan/DevCadience/internal/protocol"
)

// HostID identifies a supported principal host.
//
// The set is bounded by ADR-0011 §6 to the three first-class hosts. Other
// editors are future integration candidates, and listing one here before its
// integration exists would advertise support DevCadience does not have.
type HostID string

const (
	HostAntigravity HostID = "antigravity"
	HostCursor      HostID = "cursor"
	HostVSCode      HostID = "vscode"
)

// SupportedHosts are the hosts this build recognises, in stable order.
func SupportedHosts() []HostID { return []HostID{HostAntigravity, HostCursor, HostVSCode} }

// Host is what could be observed about one principal host.
//
// IntegrationConfigured is deliberately absent. Whether DevCadience's semantic
// integration is installed in a host is an M4A question, and a field reserved
// for it here would be read as "not configured" when the truth is "nothing has
// ever tried".
type Host struct {
	ID        HostID
	Installed bool
	// Path is the resolved executable or application path.
	Path string
	// Version is sanitised text the host reported, when it reports one.
	Version string
	// VersionStatus is this build's compatibility judgement, which is unknown
	// for every host until an integration exists to be compatible with.
	VersionStatus protocol.VersionStatus
	// Detail explains how the host was found, or why it was not.
	Detail string
}

// Inventory is the observed host situation.
type Inventory struct {
	Hosts []Host
}

// Installed returns the installed hosts, in stable order.
func (i Inventory) Installed() []Host {
	var out []Host
	for _, host := range i.Hosts {
		if host.Installed {
			out = append(out, host)
		}
	}
	return out
}

// Any reports whether any supported host is installed.
//
// False is a normal blank-machine state, not an error: DevCadience's control
// plane and its CLI work without a host, and M3B asks the user which one they
// want rather than assuming one exists.
func (i Inventory) Any() bool { return len(i.Installed()) > 0 }

// FromEnvironment reads the host inventory out of already-discovered facts.
//
// It runs no probe of its own. The environment layer already resolves each
// host's executable and application bundle; re-probing here would duplicate that
// work and risk the two disagreeing.
func FromEnvironment(facts protocol.EnvironmentFacts) Inventory {
	byID := map[string]protocol.SoftwarePresence{}
	for _, entry := range facts.Software {
		if entry.Category == protocol.SoftwarePrincipalHost {
			byID[entry.ID] = entry
		}
	}
	inventory := Inventory{}
	for _, id := range SupportedHosts() {
		entry, found := byID[string(id)]
		host := Host{ID: id, VersionStatus: protocol.VersionUnknown}
		switch {
		case !found:
			host.Detail = "this build did not look for this host on this operating system"
		case !entry.Installed:
			host.Detail = "not installed"
		default:
			host.Installed = true
			host.Path = entry.Path
			host.Version = entry.Version
			host.VersionStatus = entry.VersionStatus
			host.Detail = entry.Detail
			if host.Detail == "" {
				host.Detail = "installed; DevCadience integration is not configured by this milestone"
			}
		}
		inventory.Hosts = append(inventory.Hosts, host)
	}
	sort.Slice(inventory.Hosts, func(i, j int) bool { return inventory.Hosts[i].ID < inventory.Hosts[j].ID })
	return inventory
}

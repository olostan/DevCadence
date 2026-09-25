package cognition

import (
	"sort"

	"github.com/olostan/DevCadence/internal/protocol"
)

// Project reduces a machine profile to the compact ProjectState projection.
//
// Three questions decided this shape, and the answers are worth stating because
// the obvious alternative — copying the profile into ProjectState — is wrong in
// all three:
//
// *Is machine capability global or project-specific?* The machine is global: the
// same hardware and the same installed runtimes serve every project on the host.
// What is project-specific is policy — which endpoints a project's privacy and
// cost rules permit — and policy is routing input, not state. So the projection
// reports what exists, and routing decides what a given project may use.
//
// *What belongs in ProjectState?* Only what a project-level reader needs:
// whether cognition can happen at all, through which endpoints, at what cost and
// exposure, and how stale the answer is. Capability grades, probe signals,
// measurements, CPU features and device nodes stay in the profile, reachable
// through ProfileRef (DCI-010, DCI-011).
//
// *How do we keep stale machine facts from masquerading as durable project
// truth?* By making staleness checkable rather than implicit. ObservedAt says
// when, MachineFingerprint says which machine, and a reader comparing the
// fingerprint against a fresh discovery learns immediately whether the
// projection still describes the machine in front of it
// (docs/PROJECT_STATE.md §13).
//
// The projection is pure: the same profile always yields the same bytes.
func Project(profile protocol.MachineCapabilityProfile, profileRef string) protocol.CognitionCapabilities {
	observedAt := profile.ObservedAt
	fingerprint := profile.MachineFingerprint
	out := protocol.CognitionCapabilities{
		Assessment:         profile.Assessment,
		ObservedAt:         &observedAt,
		MachineFingerprint: &fingerprint,
		Limitations:        append([]string(nil), profile.Limitations...),
	}
	if profileRef != "" {
		ref := profileRef
		out.ProfileRef = &ref
	}
	for _, endpoint := range profile.Endpoints {
		summary := protocol.CognitionEndpointSummary{
			ID:                     endpoint.ID,
			Kind:                   endpoint.Kind,
			Locality:               endpoint.Locality,
			Health:                 endpoint.Health,
			Auth:                   endpoint.Auth,
			CostClass:              endpoint.CostClass,
			RequiredSourceExposure: endpoint.RequiredSourceExposure,
			CredentialRef:          endpoint.CredentialRef,
		}
		if endpoint.Acceleration != nil && endpoint.Locality == protocol.LocalityLocal {
			backend := endpoint.Acceleration.Backend
			summary.AccelerationBackend = &backend
			// Only a verified non-CPU backend sets the flag. Recording the
			// backend alongside it is what lets a reader distinguish
			// "considered and not verified" from "never considered".
			summary.AccelerationVerified = endpoint.AccelerationVerified()
		}
		out.Endpoints = append(out.Endpoints, summary)
	}
	sort.Slice(out.Endpoints, func(i, j int) bool { return out.Endpoints[i].ID < out.Endpoints[j].ID })
	sort.Strings(out.Limitations)
	return out
}

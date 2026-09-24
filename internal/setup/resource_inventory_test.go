package setup

import (
	"context"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/credentials"
	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
)

func validInventoryFacts() protocol.EnvironmentFacts {
	cores := 8
	return protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSLinux,
			Arch:   "amd64",
		},
		CPU: protocol.CPUFacts{
			LogicalCores: &cores,
		},
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(16 * 1024 * 1024 * 1024),
		},
		Virtualization: protocol.VirtualizationFacts{
			Container: protocol.ContainerNone,
		},
	}
}

// TestBuildResourceInventoryNilCredentialManagerDegradesGracefully proves a
// Doctor with no CredentialManager (the default) produces an empty
// Credentials section rather than erroring — the same nil-safe pattern
// discoverEndpoints already uses for a nil CognitionService (WP-M3B-5).
func TestBuildResourceInventoryNilCredentialManagerDegradesGracefully(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	doc, err := NewDoctor(DoctorOptions{
		Clock:   clk,
		IDs:     seq,
		HomeDir: t.TempDir(),
		CredentialRefs: []protocol.CredentialRef{
			{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-1", Kind: protocol.CredRefEnvVar, Locator: "SOME_KEY"},
		},
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	facts := validInventoryFacts()
	fp, err := environment.Fingerprint(facts)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	inv, err := doc.BuildResourceInventory(ctx, facts, fp, nil, nil)
	if err != nil {
		t.Fatalf("BuildResourceInventory: %v", err)
	}
	if err := inv.Validate(); err != nil {
		t.Fatalf("inventory failed validation: %v", err)
	}
	if len(inv.Credentials) != 0 {
		t.Fatalf("expected empty credentials with nil CredentialManager, got %d", len(inv.Credentials))
	}
	if inv.Hardware.OSFamily != protocol.OSLinux || inv.Hardware.Arch != "amd64" || inv.Hardware.LogicalCores != 8 {
		t.Errorf("hardware summary not projected correctly: %+v", inv.Hardware)
	}
	if inv.MachineFingerprint != fp {
		t.Errorf("machine_fingerprint = %q, want %q", inv.MachineFingerprint, fp)
	}
}

// TestBuildResourceInventoryChecksConfiguredCredentials proves a
// configured CredentialManager's real CheckCredential result appears
// verbatim in the inventory.
func TestBuildResourceInventoryChecksConfiguredCredentials(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	env := credentials.NewMapEnvReader(map[string]string{"SOME_KEY": "present-value"})
	mgr, err := credentials.NewManager(credentials.Options{Clock: clk, Env: env})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ref := protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-1", Kind: protocol.CredRefEnvVar, Locator: "SOME_KEY"}
	doc, err := NewDoctor(DoctorOptions{
		Clock:             clk,
		IDs:               seq,
		HomeDir:           t.TempDir(),
		CredentialManager: mgr,
		CredentialRefs:    []protocol.CredentialRef{ref},
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	facts := validInventoryFacts()
	fp, err := environment.Fingerprint(facts)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	inv, err := doc.BuildResourceInventory(ctx, facts, fp, nil, nil)
	if err != nil {
		t.Fatalf("BuildResourceInventory: %v", err)
	}
	if err := inv.Validate(); err != nil {
		t.Fatalf("inventory failed validation: %v", err)
	}
	if len(inv.Credentials) != 1 {
		t.Fatalf("expected 1 credential entry, got %d", len(inv.Credentials))
	}
	entry := inv.Credentials[0]
	if entry.Ref.RefID != ref.RefID {
		t.Errorf("ref not preserved: got %+v", entry.Ref)
	}
	// Presence maps to Indeterminate, not Authenticated (WP-M3B-4's own
	// invariant) — confirming the real Manager.CheckCredential path ran,
	// not a stub.
	if entry.Evidence.Status != protocol.AuthStatusIndeterminate {
		t.Errorf("status = %q, want indeterminate", entry.Evidence.Status)
	}
	if entry.Evidence.ProbeKind != protocol.AuthProbeEnvPresence {
		t.Errorf("probe_kind = %q, want env_presence", entry.Evidence.ProbeKind)
	}
}

// TestBuildResourceInventoryMalformedRefFailsClosed proves a structurally
// malformed configured CredentialRef fails BuildResourceInventory outright
// (a real configuration bug in DoctorOptions.CredentialRefs, distinct from
// "the credential isn't there", which CheckCredential already reports as a
// valid Unavailable/Unauthenticated AuthEvidence rather than a Go error) —
// fail closed rather than fabricating a plausible-looking evidence entry
// for a reference that was never actually checkable (AGENTS.md §16, DCI-104).
func TestBuildResourceInventoryMalformedRefFailsClosed(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	mgr, err := credentials.NewManager(credentials.Options{Clock: clk})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Malformed: locator is required and this leaves it empty, which
	// fails CredentialRef.Validate() and so CheckCredential returns an
	// error rather than a valid AuthEvidence.
	badRef := protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-bad", Kind: protocol.CredRefEnvVar, Locator: ""}

	doc, err := NewDoctor(DoctorOptions{
		Clock:             clk,
		IDs:               seq,
		HomeDir:           t.TempDir(),
		CredentialManager: mgr,
		CredentialRefs:    []protocol.CredentialRef{badRef},
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	facts := validInventoryFacts()
	fp, err := environment.Fingerprint(facts)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	if _, err := doc.BuildResourceInventory(ctx, facts, fp, nil, nil); err == nil {
		t.Fatal("expected BuildResourceInventory to fail closed on a malformed configured CredentialRef")
	}
}

// TestBuildResourceInventoryProjectsPolicy proves the active policy is
// projected into PolicySummary.
func TestBuildResourceInventoryProjectsPolicy(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	doc, err := NewDoctor(DoctorOptions{
		Clock:   clk,
		IDs:     seq,
		HomeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	facts := validInventoryFacts()
	fp, err := environment.Fingerprint(facts)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	inv, err := doc.BuildResourceInventory(ctx, facts, fp, nil, nil)
	if err != nil {
		t.Fatalf("BuildResourceInventory: %v", err)
	}
	if inv.Policy == nil {
		t.Fatal("expected a default policy to be projected (NewDoctor defaults Policy when nil)")
	}
}

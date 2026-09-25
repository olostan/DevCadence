package setup

import (
	"context"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
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

	inv, err := doc.BuildResourceInventory(ctx, facts, fp, nil, nil, nil, nil)
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

	inv, err := doc.BuildResourceInventory(ctx, facts, fp, nil, nil, nil, nil)
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

// TestNewDoctorMalformedRefFailsClosed proves a structurally malformed
// configured CredentialRef fails NewDoctor outright (a real configuration
// bug in DoctorOptions.CredentialRefs, distinct from "the credential isn't
// there", which CheckCredential already reports as a valid
// Unavailable/Unauthenticated AuthEvidence rather than a Go error) — fail
// closed as early as construction, rather than fabricating a
// plausible-looking evidence entry for a reference that was never actually
// checkable (AGENTS.md §16, DCI-104). NewDoctor validates every configured
// CredentialRef structurally, since round-5's referential-validity fix
// (independent-review follow-up on WP-M3B-5) needs a clean RefID index to
// check EndpointCredentialRefs bindings against.
func TestNewDoctorMalformedRefFailsClosed(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	mgr, err := credentials.NewManager(credentials.Options{Clock: clk})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Malformed: locator is required and this leaves it empty, which
	// fails CredentialRef.Validate().
	badRef := protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-bad", Kind: protocol.CredRefEnvVar, Locator: ""}

	_, err = NewDoctor(DoctorOptions{
		Clock:             clk,
		IDs:               seq,
		HomeDir:           t.TempDir(),
		CredentialManager: mgr,
		CredentialRefs:    []protocol.CredentialRef{badRef},
	})
	if err == nil {
		t.Fatal("expected NewDoctor to fail closed on a malformed configured CredentialRef")
	}
}

// TestNewDoctorRejectsEndpointCredentialRefBindingToUnknownRefID is the
// independent-review follow-up on WP-M3B-5, round-5 finding 1: an
// EndpointCredentialRefs value that names no actually-configured
// CredentialRef (a typo, or a stale binding after a CredentialRefs entry
// was removed) must fail NewDoctor at construction time, not silently
// produce an unverifiable endpoint_authenticated condition downstream.
func TestNewDoctorRejectsEndpointCredentialRefBindingToUnknownRefID(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	_, err := NewDoctor(DoctorOptions{
		Clock:   clk,
		IDs:     seq,
		HomeDir: t.TempDir(),
		CredentialRefs: []protocol.CredentialRef{
			{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-real", Kind: protocol.CredRefCLISession, Locator: "claude"},
		},
		EndpointCredentialRefs: map[string]string{
			"cli:claude": "cred-typo",
		},
	})
	if err == nil {
		t.Fatal("expected NewDoctor to reject an EndpointCredentialRefs binding to a RefID absent from CredentialRefs")
	}
}

// TestNewDoctorRejectsDuplicateCredentialRefID is the independent-review
// follow-up on WP-M3B-5, round-6 finding: CredentialRef.RefID is meant to
// be a unique identity ("CredentialRef.RefID -> exactly one configured
// CredentialRef"), not a multimap key. Two configured CredentialRefs
// sharing a RefID — whether identical or different content — must be
// rejected at NewDoctor's configuration boundary, both with and without a
// CredentialManager configured (the no-manager path is exactly where
// ResourceInventory's own duplicate-credential-entry check never runs,
// since ObserveCredentials returns no entries without a manager).
func TestNewDoctorRejectsDuplicateCredentialRefID(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	cases := []struct {
		name string
		refs []protocol.CredentialRef
	}{
		{"duplicate RefID, identical content", []protocol.CredentialRef{
			{SchemaVersion: protocol.SchemaVersion1, RefID: "shared", Kind: protocol.CredRefCLISession, Locator: "claude"},
			{SchemaVersion: protocol.SchemaVersion1, RefID: "shared", Kind: protocol.CredRefCLISession, Locator: "claude"},
		}},
		{"duplicate RefID, different content", []protocol.CredentialRef{
			{SchemaVersion: protocol.SchemaVersion1, RefID: "shared", Kind: protocol.CredRefCLISession, Locator: "claude"},
			{SchemaVersion: protocol.SchemaVersion1, RefID: "shared", Kind: protocol.CredRefEnvVar, Locator: "OPENAI_API_KEY"},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/no CredentialManager", func(t *testing.T) {
			_, err := NewDoctor(DoctorOptions{
				Clock:          clk,
				IDs:            seq,
				HomeDir:        t.TempDir(),
				CredentialRefs: tc.refs,
				// No CredentialManager: proves this is caught at Doctor
				// configuration time, not accidentally by
				// ResourceInventory's later duplicate-credential-entry
				// check, which never runs without a manager.
			})
			if err == nil {
				t.Fatal("expected NewDoctor to reject duplicate configured CredentialRef.RefID values")
			}
		})

		t.Run(tc.name+"/with CredentialManager", func(t *testing.T) {
			mgr, mgrErr := credentials.NewManager(credentials.Options{Clock: clk})
			if mgrErr != nil {
				t.Fatalf("credentials.NewManager: %v", mgrErr)
			}
			_, err := NewDoctor(DoctorOptions{
				Clock:             clk,
				IDs:               seq,
				HomeDir:           t.TempDir(),
				CredentialManager: mgr,
				CredentialRefs:    tc.refs,
			})
			if err == nil {
				t.Fatal("expected NewDoctor to reject duplicate configured CredentialRef.RefID values")
			}
		})
	}
}

// TestNewCredentialsEndpointAuthChecker_RejectsDuplicateCredentialRefID
// covers the Executor-side half of the round-6 finding: the checker must
// not silently build a "last write wins" index over an ambiguous
// configured set either.
func TestNewCredentialsEndpointAuthChecker_RejectsDuplicateCredentialRefID(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	mgr, err := credentials.NewManager(credentials.Options{Clock: clk})
	if err != nil {
		t.Fatalf("credentials.NewManager: %v", err)
	}
	refs := []protocol.CredentialRef{
		{SchemaVersion: protocol.SchemaVersion1, RefID: "shared", Kind: protocol.CredRefCLISession, Locator: "claude"},
		{SchemaVersion: protocol.SchemaVersion1, RefID: "shared", Kind: protocol.CredRefEnvVar, Locator: "OPENAI_API_KEY"},
	}
	if _, err := NewCredentialsEndpointAuthChecker(mgr, refs); err == nil {
		t.Fatal("expected NewCredentialsEndpointAuthChecker to reject duplicate configured CredentialRef.RefID values")
	}
}

// TestDoctorDiscoverEndpoints_IgnoresUnrecognizedAdapterCredentialRef is
// the independent-review follow-up on WP-M3B-5, round-5 finding 1: an
// adapter-declared CognitionEndpoint.CredentialRef that does not resolve
// against the configured CredentialRefs index must be treated as
// diagnostic-only (left out of the summary), never carried through as a
// machine-verifiable binding Planner could turn into an unverifiable
// endpoint_authenticated condition.
func TestDoctorDiscoverEndpoints_IgnoresUnrecognizedAdapterCredentialRef(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)
	seq := ids.NewSequential()
	tmpHome := t.TempDir()

	stub := &stubCognitionAdapter{
		endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "remote:some-provider",
				Kind:                   protocol.EndpointRemoteAPI,
				Locality:               protocol.LocalityRemote,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostRemoteEconomy,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				StructuredOutput:       protocol.FeatureDeclared,
				ToolUse:                protocol.FeatureDeclared,
				ObservedAt:             protocol.NewTimestamp(now),
				// Declared by the adapter itself, but never configured in
				// DoctorOptions.CredentialRefs below.
				CredentialRef: "unconfigured-ref",
			},
		},
	}
	service, err := cognition.NewService(cognition.Options{
		Adapters: []cognition.Adapter{stub},
		Clock:    clk,
		IDs:      seq,
	})
	if err != nil {
		t.Fatalf("cognition.NewService: %v", err)
	}

	doc, err := NewDoctor(DoctorOptions{
		Clock:            clk,
		IDs:              seq,
		HomeDir:          tmpHome,
		CognitionService: service,
		CredentialRefs: []protocol.CredentialRef{
			{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-real", Kind: protocol.CredRefCLISession, Locator: "claude"},
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

	summaries, _, _, _, err := doc.discoverEndpoints(ctx, facts, fp)
	if err != nil {
		t.Fatalf("discoverEndpoints: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 endpoint summary, got %d", len(summaries))
	}
	if got := summaries[0].CredentialRef; got != "" {
		t.Fatalf("CredentialRef = %q, want empty (an unrecognized adapter-declared ref must not be carried through as machine-verifiable)", got)
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

	inv, err := doc.BuildResourceInventory(ctx, facts, fp, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildResourceInventory: %v", err)
	}
	if inv.Policy == nil {
		t.Fatal("expected a default policy to be projected (NewDoctor defaults Policy when nil)")
	}
}

package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
)

func TestCredentialRefValidation_AllKinds(t *testing.T) {
	cases := []struct {
		name string
		ref  protocol.CredentialRef
	}{
		{
			name: "valid env_var",
			ref: protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-env-anthropic",
				Kind:          protocol.CredRefEnvVar,
				Locator:       "ANTHROPIC_API_KEY",
			},
		},
		{
			name: "valid cli_session",
			ref: protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-cli-claude",
				Kind:          protocol.CredRefCLISession,
				Locator:       "claude-code:session-1",
			},
		},
		{
			name: "valid keychain_ref",
			ref: protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-keychain-default",
				Kind:          protocol.CredRefKeychainRef,
				Locator:       "devcadence/service/default",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.ref.Validate(); err != nil {
				t.Fatalf("expected valid reference, got error: %v", err)
			}
			// Round trip JSON
			data, err := json.Marshal(tc.ref)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			var decoded protocol.CredentialRef
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("json.Unmarshal failed: %v", err)
			}
			if decoded != tc.ref {
				t.Fatalf("round-trip mismatch: got %+v, want %+v", decoded, tc.ref)
			}
		})
	}
}

func TestCredentialRefValidation_UnknownKindRejected(t *testing.T) {
	ref := protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-invalid-kind",
		Kind:          protocol.CredentialRefKind("oauth_bearer_store"),
		Locator:       "TOKEN",
	}
	if err := ref.Validate(); err == nil {
		t.Fatal("expected error for unknown CredentialRefKind, got nil")
	} else if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %q, want invalid_argument", errs.CategoryOf(err))
	}
}

func TestCredentialRefValidation_MalformedOrEmpty(t *testing.T) {
	cases := []struct {
		name string
		ref  protocol.CredentialRef
	}{
		{
			name: "empty ref_id",
			ref: protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "",
				Kind:          protocol.CredRefEnvVar,
				Locator:       "ANTHROPIC_API_KEY",
			},
		},
		{
			name: "ref_id with whitespace",
			ref: protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "bad ref id",
				Kind:          protocol.CredRefEnvVar,
				Locator:       "ANTHROPIC_API_KEY",
			},
		},
		{
			name: "empty locator",
			ref: protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-1",
				Kind:          protocol.CredRefEnvVar,
				Locator:       "",
			},
		},
		{
			name: "env_var lowercase locator",
			ref: protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-1",
				Kind:          protocol.CredRefEnvVar,
				Locator:       "anthropic_api_key",
			},
		},
		{
			name: "env_var with dash",
			ref: protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-1",
				Kind:          protocol.CredRefEnvVar,
				Locator:       "ANTHROPIC-API-KEY",
			},
		},
		{
			name: "cli_session invalid characters",
			ref: protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-1",
				Kind:          protocol.CredRefCLISession,
				Locator:       "claude/session@1",
			},
		},
		{
			name: "keychain_ref directory traversal",
			ref: protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-1",
				Kind:          protocol.CredRefKeychainRef,
				Locator:       "../../etc/shadow",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.ref.Validate(); err == nil {
				t.Fatalf("expected validation error for %s, got nil", tc.name)
			}
		})
	}
}

func TestCredentialRefValidation_SecretLookingLocatorsRejected(t *testing.T) {
	secrets := []string{
		"sk-ant-api03-abcdefghijklmnop",
		"sk_live_abcdef123456",
		"ghp_1234567890abcdefghijklmnopqrst",
		"pat_1234567890abcdefghijklmnopqrst",
		"bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		"AKIAIOSFODNN7EXAMPLE",
		"token=abcdef123456",
		"secret=topsecretvalue",
		"api_key=mykey123",
		strings.Repeat("a", 130),
	}

	for _, secret := range secrets {
		t.Run("secret_"+secret[:min(10, len(secret))], func(t *testing.T) {
			// Secret in locator
			refLocator := protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-valid-id",
				Kind:          protocol.CredRefCLISession,
				Locator:       secret,
			}
			if err := refLocator.Validate(); err == nil {
				t.Fatalf("expected secret-looking locator %q to be rejected, got nil", secret)
			}

			// Secret in ref_id
			refID := protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         secret,
				Kind:          protocol.CredRefCLISession,
				Locator:       "claude",
			}
			if err := refID.Validate(); err == nil {
				t.Fatalf("expected secret-looking ref_id %q to be rejected, got nil", secret)
			}
		})
	}
}

func TestAuthEvidenceValidation(t *testing.T) {
	ts := validTestTimestamp()

	valid := protocol.AuthEvidence{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-001",
		Kind:          protocol.CredRefCLISession,
		Status:        protocol.AuthStatusAuthenticated,
		ProbeKind:     protocol.AuthProbeCLIAuthCall,
		ObservedAt:    ts,
		ProbeTarget:   "claude",
		AdapterID:     "claude-auth-probe",
		Detail:        "session authenticated via CLI auth probe",
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid AuthEvidence, got %v", err)
	}

	// Version output alone cannot establish authenticated state
	versionAuth := valid
	versionAuth.ProbeKind = protocol.AuthProbeCLIVersionOnly
	versionAuth.Status = protocol.AuthStatusAuthenticated
	if err := versionAuth.Validate(); err == nil {
		t.Fatal("expected error: cli_version_only cannot establish authenticated status")
	}

	// Secret in detail is rejected
	secretDetail := valid
	secretDetail.Detail = "token=sk-ant-secret-value-leaked"
	if err := secretDetail.Validate(); err == nil {
		t.Fatal("expected secret in detail to be rejected")
	}

	// Secret in probe_target is rejected
	secretTarget := valid
	secretTarget.ProbeTarget = "sk-live-secret-key"
	if err := secretTarget.Validate(); err == nil {
		t.Fatal("expected secret in probe_target to be rejected")
	}
}

func TestSchemaParity(t *testing.T) {
	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("failed to compile schemas: %v", err)
	}

	// Test valid CredentialRef
	cred := protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-001",
		Kind:          protocol.CredRefEnvVar,
		Locator:       "ANTHROPIC_API_KEY",
	}
	if err := cred.Validate(); err != nil {
		t.Fatalf("Go Validate failed: %v", err)
	}
	credBytes, _ := json.Marshal(cred)
	if err := schemas.ValidateBytes(schema.NameCredentialRef, credBytes); err != nil {
		t.Fatalf("Schema validation failed on valid CredentialRef: %v", err)
	}

	// Test invalid CredentialRef (lowercase env_var) rejected by schema
	invalidCredBytes := []byte(`{"ref_id":"c1","kind":"env_var","locator":"lowercase_key"}`)
	if err := schemas.ValidateBytes(schema.NameCredentialRef, invalidCredBytes); err == nil {
		t.Fatal("Schema expected to reject lowercase env_var locator, but accepted")
	}

	// Test valid AuthEvidence
	ev := protocol.AuthEvidence{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-001",
		Kind:          protocol.CredRefCLISession,
		Status:        protocol.AuthStatusAuthenticated,
		ProbeKind:     protocol.AuthProbeCLIAuthCall,
		ObservedAt:    validTestTimestamp(),
		ProbeTarget:   "claude",
		AdapterID:     "claude-auth-adapter",
		Detail:        "session valid",
	}
	if err := ev.Validate(); err != nil {
		t.Fatalf("Go Validate failed on AuthEvidence: %v", err)
	}
	evBytes, _ := json.Marshal(ev)
	if err := schemas.ValidateBytes(schema.NameAuthEvidence, evBytes); err != nil {
		t.Fatalf("Schema validation failed on valid AuthEvidence: %v", err)
	}

	// Test invalid AuthEvidence: cli_version_only cannot be authenticated
	invalidEvBytes := []byte(`{
		"ref_id": "c1",
		"kind": "cli_session",
		"status": "authenticated",
		"probe_kind": "cli_version_only",
		"observed_at": "2026-09-24T10:00:00.000000Z"
	}`)
	if err := schemas.ValidateBytes(schema.NameAuthEvidence, invalidEvBytes); err == nil {
		t.Fatal("Schema expected to reject cli_version_only with authenticated status, but accepted")
	}
}

func TestAuthEvidenceKindProbeKindBinding(t *testing.T) {
	ts := validTestTimestamp()
	base := protocol.AuthEvidence{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-001",
		ObservedAt:    ts,
		Status:        protocol.AuthStatusIndeterminate,
	}

	cases := []struct {
		name  string
		kind  protocol.CredentialRefKind
		probe protocol.AuthProbeKind
		valid bool
	}{
		{"env_var + env_presence", protocol.CredRefEnvVar, protocol.AuthProbeEnvPresence, true},
		{"env_var + cli_auth_call mismatched", protocol.CredRefEnvVar, protocol.AuthProbeCLIAuthCall, false},
		{"keychain_ref + keychain_presence", protocol.CredRefKeychainRef, protocol.AuthProbeKeychainPresence, true},
		{"keychain_ref + env_presence mismatched", protocol.CredRefKeychainRef, protocol.AuthProbeEnvPresence, false},
		{"cli_session + cli_auth_call", protocol.CredRefCLISession, protocol.AuthProbeCLIAuthCall, true},
		{"cli_session + cli_version_only", protocol.CredRefCLISession, protocol.AuthProbeCLIVersionOnly, true},
		{"cli_session + env_presence mismatched", protocol.CredRefCLISession, protocol.AuthProbeEnvPresence, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := base
			ev.Kind = tc.kind
			ev.ProbeKind = tc.probe
			err := ev.Validate()
			if tc.valid && err != nil {
				t.Fatalf("expected valid kind/probe_kind pairing, got error: %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("expected kind/probe_kind mismatch to be rejected, got nil")
			}
		})
	}
}

func TestAuthEvidencePresenceProbesCannotEstablishAuthenticated(t *testing.T) {
	ts := validTestTimestamp()
	cases := []struct {
		name  string
		kind  protocol.CredentialRefKind
		probe protocol.AuthProbeKind
	}{
		{"env_presence", protocol.CredRefEnvVar, protocol.AuthProbeEnvPresence},
		{"keychain_presence", protocol.CredRefKeychainRef, protocol.AuthProbeKeychainPresence},
		{"cli_version_only", protocol.CredRefCLISession, protocol.AuthProbeCLIVersionOnly},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := protocol.AuthEvidence{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-001",
				Kind:          tc.kind,
				Status:        protocol.AuthStatusAuthenticated,
				ProbeKind:     tc.probe,
				ObservedAt:    ts,
			}
			if err := ev.Validate(); err == nil {
				t.Fatalf("expected %s probe kind to be rejected with status authenticated, got nil", tc.probe)
			}
		})
	}
}

func TestAuthEvidenceRefIDAndAdapterIDShareTheOpaqueIDContract(t *testing.T) {
	ts := validTestTimestamp()
	base := protocol.AuthEvidence{
		SchemaVersion: protocol.SchemaVersion1,
		Kind:          protocol.CredRefCLISession,
		Status:        protocol.AuthStatusIndeterminate,
		ProbeKind:     protocol.AuthProbeCLIVersionOnly,
		ObservedAt:    ts,
	}

	// ref_id must reject the same secret-shaped/oversized/malformed values
	// CredentialRef.RefID does — previously AuthEvidence.RefID was only
	// checked non-empty.
	badRefIDs := []string{
		"", "bad ref id", "sk-ant-api03-abcdefghijklmnop", strings.Repeat("a", 130),
	}
	for _, refID := range badRefIDs {
		ev := base
		ev.RefID = refID
		if refID != "" {
			ev.RefID = refID
		}
		if err := ev.Validate(); err == nil {
			t.Fatalf("expected ref_id %q to be rejected, got nil", refID)
		}
	}

	// adapter_id must reject secret-shaped content — previously it only
	// had a max-length check.
	secretAdapterID := base
	secretAdapterID.RefID = "cred-001"
	secretAdapterID.AdapterID = "sk-ant-api03-abcdefghijklmnop"
	if err := secretAdapterID.Validate(); err == nil {
		t.Fatal("expected secret-looking adapter_id to be rejected, got nil")
	}

	// A valid adapter_id (or none at all) still passes.
	valid := base
	valid.RefID = "cred-001"
	valid.AdapterID = "claude-version-probe"
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid adapter_id to pass, got %v", err)
	}
}

// TestSchemaSecretPatternParity is the table-driven Go/JSON-Schema parity
// test requested against the mismatches a review found: keychain ".."
// traversal, secret-shaped ref_id/locator/adapter_id/probe_target/detail,
// and the kind/probe_kind structural matrix. Each case is run through both
// Go validation and schema validation; the two must agree on accept/reject.
func TestSchemaSecretPatternParity(t *testing.T) {
	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("failed to compile schemas: %v", err)
	}

	type credCase struct {
		name string
		ref  protocol.CredentialRef
	}
	credCases := []credCase{
		{"valid env_var", protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefEnvVar, Locator: "ANTHROPIC_API_KEY"}},
		{"keychain traversal", protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefKeychainRef, Locator: "../../etc/shadow"}},
		{"keychain valid dotted locator", protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefKeychainRef, Locator: "com.devcadence.service"}},
		{"secret-shaped ref_id", protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "sk-ant-api03-abcdefghijklmnop", Kind: protocol.CredRefCLISession, Locator: "claude"}},
		{"secret-shaped locator", protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Locator: "sk-ant-api03-abcdefghijklmnop"}},
		{"token= keyword in locator", protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Locator: "token=abcdef123456"}},
		{"ref_id at 128-byte limit is accepted", protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: strings.Repeat("a", 128), Kind: protocol.CredRefCLISession, Locator: "claude"}},
		{"ref_id over 128-byte limit is rejected", protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: strings.Repeat("a", 129), Kind: protocol.CredRefCLISession, Locator: "claude"}},
		{"cli_session locator at 128-byte limit is accepted", protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Locator: strings.Repeat("a", 128)}},
		{"cli_session locator over 128-byte limit is rejected", protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Locator: strings.Repeat("a", 129)}},
	}
	for _, tc := range credCases {
		t.Run("CredentialRef/"+tc.name, func(t *testing.T) {
			goErr := tc.ref.Validate()
			data, err := json.Marshal(tc.ref)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			schemaErr := schemas.ValidateBytes(schema.NameCredentialRef, data)
			if (goErr == nil) != (schemaErr == nil) {
				t.Fatalf("parity mismatch: go accepted=%v (err=%v), schema accepted=%v (err=%v)",
					goErr == nil, goErr, schemaErr == nil, schemaErr)
			}
		})
	}

	ts := validTestTimestamp()
	type evCase struct {
		name string
		ev   protocol.AuthEvidence
	}
	evCases := []evCase{
		{"valid", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusAuthenticated, ProbeKind: protocol.AuthProbeCLIAuthCall, ObservedAt: ts}},
		{"secret-shaped adapter_id", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, AdapterID: "sk-ant-api03-abcdefghijklmnop"}},
		{"secret-shaped probe_target", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, ProbeTarget: "sk-ant-api03-abcdefghijklmnop"}},
		{"secret-shaped detail", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, Detail: "token=abcdef123456"}},
		{"mismatched kind/probe_kind", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefEnvVar, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIAuthCall, ObservedAt: ts}},
		{"presence probe claims authenticated", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefEnvVar, Status: protocol.AuthStatusAuthenticated, ProbeKind: protocol.AuthProbeEnvPresence, ObservedAt: ts}},
		// Boundary parity: independent-review follow-up on WP-M3B-4 found
		// that LooksLikeSecret's earlier unconditional ">128 bytes is a
		// secret" rule disagreed with probe_target's declared 256 and
		// detail's declared 512 — an ordinary 129-byte string was accepted
		// by the schema (maxLength 256/512) but rejected by Go. These
		// cases pin the boundary exactly at each field's own declared
		// limit, with no length-based secret heuristic in the way.
		{"probe_target 129 bytes ordinary text is accepted", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, ProbeTarget: strings.Repeat("a", 129)}},
		{"probe_target at 256-byte limit is accepted", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, ProbeTarget: strings.Repeat("a", 256)}},
		{"probe_target over 256-byte limit is rejected", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, ProbeTarget: strings.Repeat("a", 257)}},
		{"detail 129 bytes ordinary text is accepted", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, Detail: strings.Repeat("a", 129)}},
		{"detail at 512-byte limit is accepted", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, Detail: strings.Repeat("a", 512)}},
		{"detail over 512-byte limit is rejected", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, Detail: strings.Repeat("a", 513)}},
		{"detail secret-shaped and within length is still rejected", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, Detail: "sk-ant-" + strings.Repeat("a", 200)}},
		{"adapter_id at 128-byte limit is accepted", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, AdapterID: strings.Repeat("a", 128)}},
		{"adapter_id over 128-byte limit is rejected", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, AdapterID: strings.Repeat("a", 129)}},
		// Unicode boundary parity (independent-review follow-up on
		// WP-M3B-4, finding 2, third round): Go's len(string) counts UTF-8
		// bytes, but JSON Schema's maxLength counts Unicode characters/code
		// points. "é" is 2 UTF-8 bytes but 1 schema character, so these
		// cases would previously disagree at the boundary if Go used
		// len() instead of utf8.RuneCountInString.
		{"probe_target 256 multi-byte runes at the limit is accepted", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, ProbeTarget: strings.Repeat("é", 256)}},
		{"probe_target 257 multi-byte runes over the limit is rejected", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, ProbeTarget: strings.Repeat("é", 257)}},
		{"detail 512 multi-byte runes at the limit is accepted", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, Detail: strings.Repeat("é", 512)}},
		{"detail 513 multi-byte runes over the limit is rejected", protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-001", Kind: protocol.CredRefCLISession, Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: ts, Detail: strings.Repeat("é", 513)}},
	}
	for _, tc := range evCases {
		t.Run("AuthEvidence/"+tc.name, func(t *testing.T) {
			goErr := tc.ev.Validate()
			data, err := json.Marshal(tc.ev)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			schemaErr := schemas.ValidateBytes(schema.NameAuthEvidence, data)
			if (goErr == nil) != (schemaErr == nil) {
				t.Fatalf("parity mismatch: go accepted=%v (err=%v), schema accepted=%v (err=%v)",
					goErr == nil, goErr, schemaErr == nil, schemaErr)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

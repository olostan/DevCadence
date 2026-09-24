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
				RefID:   "cred-env-anthropic",
				Kind:    protocol.CredRefEnvVar,
				Locator: "ANTHROPIC_API_KEY",
			},
		},
		{
			name: "valid cli_session",
			ref: protocol.CredentialRef{
				RefID:   "cred-cli-claude",
				Kind:    protocol.CredRefCLISession,
				Locator: "claude-code:session-1",
			},
		},
		{
			name: "valid keychain_ref",
			ref: protocol.CredentialRef{
				RefID:   "cred-keychain-default",
				Kind:    protocol.CredRefKeychainRef,
				Locator: "devcadence/service/default",
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
		RefID:   "cred-invalid-kind",
		Kind:    protocol.CredentialRefKind("oauth_bearer_store"),
		Locator: "TOKEN",
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
				RefID:   "",
				Kind:    protocol.CredRefEnvVar,
				Locator: "ANTHROPIC_API_KEY",
			},
		},
		{
			name: "ref_id with whitespace",
			ref: protocol.CredentialRef{
				RefID:   "bad ref id",
				Kind:    protocol.CredRefEnvVar,
				Locator: "ANTHROPIC_API_KEY",
			},
		},
		{
			name: "empty locator",
			ref: protocol.CredentialRef{
				RefID:   "cred-1",
				Kind:    protocol.CredRefEnvVar,
				Locator: "",
			},
		},
		{
			name: "env_var lowercase locator",
			ref: protocol.CredentialRef{
				RefID:   "cred-1",
				Kind:    protocol.CredRefEnvVar,
				Locator: "anthropic_api_key",
			},
		},
		{
			name: "env_var with dash",
			ref: protocol.CredentialRef{
				RefID:   "cred-1",
				Kind:    protocol.CredRefEnvVar,
				Locator: "ANTHROPIC-API-KEY",
			},
		},
		{
			name: "cli_session invalid characters",
			ref: protocol.CredentialRef{
				RefID:   "cred-1",
				Kind:    protocol.CredRefCLISession,
				Locator: "claude/session@1",
			},
		},
		{
			name: "keychain_ref directory traversal",
			ref: protocol.CredentialRef{
				RefID:   "cred-1",
				Kind:    protocol.CredRefKeychainRef,
				Locator: "../../etc/shadow",
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
				RefID:   "cred-valid-id",
				Kind:    protocol.CredRefCLISession,
				Locator: secret,
			}
			if err := refLocator.Validate(); err == nil {
				t.Fatalf("expected secret-looking locator %q to be rejected, got nil", secret)
			}

			// Secret in ref_id
			refID := protocol.CredentialRef{
				RefID:   secret,
				Kind:    protocol.CredRefCLISession,
				Locator: "claude",
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
		RefID:       "cred-001",
		Kind:        protocol.CredRefCLISession,
		Status:      protocol.AuthStatusAuthenticated,
		ProbeKind:   protocol.AuthProbeCLIAuthCall,
		ObservedAt:  ts,
		ProbeTarget: "claude",
		AdapterID:   "claude-auth-probe",
		Detail:      "session authenticated via CLI auth probe",
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
		RefID:   "cred-001",
		Kind:    protocol.CredRefEnvVar,
		Locator: "ANTHROPIC_API_KEY",
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
		RefID:       "cred-001",
		Kind:        protocol.CredRefCLISession,
		Status:      protocol.AuthStatusAuthenticated,
		ProbeKind:   protocol.AuthProbeCLIAuthCall,
		ObservedAt:  validTestTimestamp(),
		ProbeTarget: "claude",
		AdapterID:   "claude-auth-adapter",
		Detail:      "session valid",
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

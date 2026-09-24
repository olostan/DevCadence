package protocol

import (
	"regexp"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// CredentialRefKind specifies the storage/resolution mechanism for an opaque credential reference.
type CredentialRefKind string

const (
	CredRefEnvVar      CredentialRefKind = "env_var"
	CredRefCLISession  CredentialRefKind = "cli_session"
	CredRefKeychainRef CredentialRefKind = "keychain_ref"
)

// Valid reports whether the credential reference kind is known.
func (k CredentialRefKind) Valid() bool {
	switch k {
	case CredRefEnvVar, CredRefCLISession, CredRefKeychainRef:
		return true
	}
	return false
}

// CredentialRef is an opaque reference to an authorization mechanism.
// It is provider-neutral and contains NO secret material.
//
// Invariant: CredentialRef != CognitionEndpoint != AccessChannel != Session != Account != EconomicRegime != CognitionPortfolio.
type CredentialRef struct {
	RefID   string            `json:"ref_id"`
	Kind    CredentialRefKind `json:"kind"`
	Locator string            `json:"locator"`
}

var (
	envVarIdentifierRegex  = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	cliSessionHandleRegex  = regexp.MustCompile(`^[a-zA-Z0-9_\-:]+$`)
	keychainLocatorRegex   = regexp.MustCompile(`^[a-zA-Z0-9_\-\./:]+$`)
	credentialRefIDRegex   = regexp.MustCompile(`^[a-zA-Z0-9_\-:]+$`)
)

// Validate checks that the CredentialRef is well-formed, provider-neutral,
// and free of secret-looking material.
func (c CredentialRef) Validate() error {
	const kind = "CredentialRef"
	if c.RefID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: ref_id is required", kind)
	}
	if len(c.RefID) > 128 {
		return errs.New(errs.CategoryInvalidArgument, "%s: ref_id exceeds maximum length of 128", kind)
	}
	if !credentialRefIDRegex.MatchString(c.RefID) {
		return errs.New(errs.CategoryInvalidArgument, "%s: ref_id must be alphanumeric with dashes/underscores/colons, got %q", kind, c.RefID)
	}
	if LooksLikeSecret(c.RefID) {
		return errs.New(errs.CategoryInvalidArgument, "%s: ref_id looks like a secret value; it must be an opaque reference", kind)
	}

	if !c.Kind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid kind %q", kind, string(c.Kind))
	}

	if c.Locator == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: locator is required", kind)
	}
	if LooksLikeSecret(c.Locator) {
		return errs.New(errs.CategoryInvalidArgument, "%s: locator looks like a secret value; raw credentials must never be used as locators", kind)
	}

	switch c.Kind {
	case CredRefEnvVar:
		if len(c.Locator) > 128 {
			return errs.New(errs.CategoryInvalidArgument, "%s: env_var locator exceeds maximum length of 128", kind)
		}
		if !envVarIdentifierRegex.MatchString(c.Locator) {
			return errs.New(errs.CategoryInvalidArgument, "%s: env_var locator must be valid uppercase env identifier, got %q", kind, c.Locator)
		}
	case CredRefCLISession:
		if len(c.Locator) > 128 {
			return errs.New(errs.CategoryInvalidArgument, "%s: cli_session locator exceeds maximum length of 128", kind)
		}
		if !cliSessionHandleRegex.MatchString(c.Locator) {
			return errs.New(errs.CategoryInvalidArgument, "%s: cli_session locator must be alphanumeric handle, got %q", kind, c.Locator)
		}
	case CredRefKeychainRef:
		if len(c.Locator) > 256 {
			return errs.New(errs.CategoryInvalidArgument, "%s: keychain_ref locator exceeds maximum length of 256", kind)
		}
		if !keychainLocatorRegex.MatchString(c.Locator) {
			return errs.New(errs.CategoryInvalidArgument, "%s: keychain_ref locator contains invalid characters, got %q", kind, c.Locator)
		}
		if strings.Contains(c.Locator, "..") {
			return errs.New(errs.CategoryInvalidArgument, "%s: keychain_ref locator must not contain directory traversal '..'", kind)
		}
	}
	return nil
}

// RecordKind implements Record.
func (c CredentialRef) RecordKind() string { return "CredentialRef" }

// RecordID implements Record.
func (c CredentialRef) RecordID() string { return c.RefID }

// SchemaVer implements Record.
func (c CredentialRef) SchemaVer() string { return "1.0" }

// AuthEvidenceStatus represents the authoritative outcome of an authentication probe.
type AuthEvidenceStatus string

const (
	AuthStatusAuthenticated   AuthEvidenceStatus = "authenticated"
	AuthStatusUnauthenticated AuthEvidenceStatus = "unauthenticated"
	AuthStatusUnavailable     AuthEvidenceStatus = "unavailable"
	AuthStatusIndeterminate   AuthEvidenceStatus = "indeterminate"
)

// Valid reports whether the status is valid.
func (s AuthEvidenceStatus) Valid() bool {
	switch s {
	case AuthStatusAuthenticated, AuthStatusUnauthenticated, AuthStatusUnavailable, AuthStatusIndeterminate:
		return true
	}
	return false
}

// AuthProbeKind identifies the mechanism used to obtain authentication evidence.
type AuthProbeKind string

const (
	AuthProbeEnvPresence      AuthProbeKind = "env_presence"
	AuthProbeCLIAuthCall      AuthProbeKind = "cli_auth_call"
	AuthProbeCLIVersionOnly   AuthProbeKind = "cli_version_only"
	AuthProbeKeychainPresence AuthProbeKind = "keychain_presence"
)

// Valid reports whether the probe kind is valid.
func (k AuthProbeKind) Valid() bool {
	switch k {
	case AuthProbeEnvPresence, AuthProbeCLIAuthCall, AuthProbeCLIVersionOnly, AuthProbeKeychainPresence:
		return true
	}
	return false
}

// AuthEvidence is a structured, bounded durable record of authentication readiness.
// It NEVER contains raw output, tokens, or secret-bearing data (ADR-0014 §6, ADR-0014 §7).
type AuthEvidence struct {
	RefID       string             `json:"ref_id"`
	Kind        CredentialRefKind  `json:"kind"`
	Status      AuthEvidenceStatus `json:"status"`
	ProbeKind   AuthProbeKind      `json:"probe_kind"`
	ObservedAt  Timestamp          `json:"observed_at"`
	ProbeTarget string             `json:"probe_target,omitempty"`
	AdapterID   string             `json:"adapter_id,omitempty"`
	Detail      string             `json:"detail,omitempty"`
}

// Validate checks that the AuthEvidence record is well-formed and safe.
func (e AuthEvidence) Validate() error {
	const kind = "AuthEvidence"
	if e.RefID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: ref_id is required", kind)
	}
	if !e.Kind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid kind %q", kind, string(e.Kind))
	}
	if !e.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid status %q", kind, string(e.Status))
	}
	if !e.ProbeKind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid probe_kind %q", kind, string(e.ProbeKind))
	}
	if e.ObservedAt.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: observed_at timestamp is required", kind)
	}

	// ADR-0014 §6: Version output alone proves software installation, NEVER authentication.
	if e.ProbeKind == AuthProbeCLIVersionOnly && e.Status == AuthStatusAuthenticated {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: cli_version_only probe kind cannot establish authenticated status", kind)
	}

	if len(e.ProbeTarget) > 256 {
		return errs.New(errs.CategoryInvalidArgument, "%s: probe_target exceeds maximum length of 256", kind)
	}
	if LooksLikeSecret(e.ProbeTarget) {
		return errs.New(errs.CategoryInvalidArgument, "%s: probe_target looks like a secret value", kind)
	}

	if len(e.AdapterID) > 128 {
		return errs.New(errs.CategoryInvalidArgument, "%s: adapter_id exceeds maximum length of 128", kind)
	}

	if len(e.Detail) > 512 {
		return errs.New(errs.CategoryInvalidArgument, "%s: detail exceeds maximum length of 512", kind)
	}
	if LooksLikeSecret(e.Detail) {
		return errs.New(errs.CategoryInvalidArgument, "%s: detail contains secret-looking material", kind)
	}

	return nil
}

// RecordKind implements Record.
func (e AuthEvidence) RecordKind() string { return "AuthEvidence" }

// RecordID implements Record.
func (e AuthEvidence) RecordID() string { return e.RefID }

// SchemaVer implements Record.
func (e AuthEvidence) SchemaVer() string { return "1.0" }

// LooksLikeSecret is a conservative guard against raw credential material reaching
// durable records, configuration, or process boundaries (DCI-081, ADR-0014 §6).
//
// It rejects common secret token prefixes, high-entropy/length heuristics,
// and key-value secret assignments. It does not replace OS secrets storage,
// but prevents accidental pasting of raw secrets into opaque handle locators.
func LooksLikeSecret(value string) bool {
	if value == "" {
		return false
	}
	if len(value) > 128 {
		return true
	}
	lowered := strings.ToLower(value)
	for _, prefix := range []string{
		"sk-", "sk_", "pat_", "ghp_", "gho_", "ghu_", "ghs_", "ghr_",
		"github_pat_", "bearer ", "xoxb-", "xoxp-", "glpat-", "npm_", "aiza",
	} {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	// AWS access key ID prefixes (AKIA..., ASIA...)
	if strings.HasPrefix(value, "AKIA") || strings.HasPrefix(value, "ASIA") {
		return true
	}
	// Secret assignment / keyword patterns
	if strings.Contains(lowered, "secret=") ||
		strings.Contains(lowered, "token=") ||
		strings.Contains(lowered, "password=") ||
		strings.Contains(lowered, "api_key=") ||
		strings.Contains(lowered, "apikey=") {
		return true
	}
	return false
}

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
	SchemaVersion SchemaVersion     `json:"schema_version"`
	RefID         string            `json:"ref_id"`
	Kind          CredentialRefKind `json:"kind"`
	Locator       string            `json:"locator"`
}

var (
	envVarIdentifierRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	cliSessionHandleRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-:]+$`)
	// keychainLocatorRegex requires every "." to be immediately followed by
	// an allowed non-dot character, which makes ".." structurally
	// unmatchable — the same guarantee the old regex plus a separate
	// strings.Contains(locator, "..") check provided, but as a single
	// source of truth this file's JSON Schema twin
	// (schemas/credential-ref.schema.json) can mirror exactly, rather than
	// two independently-maintained traversal defenses that can drift.
	keychainLocatorRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-/:]+(\.[a-zA-Z0-9_\-/:]+)*$`)
	// opaqueIDRegex bounds every opaque identifier this package hands out
	// or accepts (CredentialRef.RefID, AuthEvidence.RefID, AuthEvidence.AdapterID)
	// — one shared shape so an identifier's validity never depends on
	// which field it happens to be assigned to.
	opaqueIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-:]+$`)
)

// validateOpaqueID applies the one bounded-identifier contract every
// opaque ID field in this file shares: non-empty (when required), bounded
// length, a restricted character set, and never secret-looking. Reusing
// this in one place is what makes CredentialRef.RefID, AuthEvidence.RefID,
// and AuthEvidence.AdapterID actually agree with each other rather than
// three independently-drifting ad hoc checks.
func validateOpaqueID(kind, field, value string, maxLen int, required bool) error {
	if value == "" {
		if required {
			return errs.New(errs.CategoryInvalidArgument, "%s: %s is required", kind, field)
		}
		return nil
	}
	if len(value) > maxLen {
		return errs.New(errs.CategoryInvalidArgument, "%s: %s exceeds maximum length of %d", kind, field, maxLen)
	}
	if !opaqueIDRegex.MatchString(value) {
		return errs.New(errs.CategoryInvalidArgument, "%s: %s must be alphanumeric with dashes/underscores/colons, got %q", kind, field, value)
	}
	if LooksLikeSecret(value) {
		return errs.New(errs.CategoryInvalidArgument, "%s: %s looks like a secret value; it must be an opaque reference", kind, field)
	}
	return nil
}

// Validate checks that the CredentialRef is well-formed, provider-neutral,
// and free of secret-looking material.
func (c CredentialRef) Validate() error {
	const kind = "CredentialRef"
	if err := c.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := validateOpaqueID(kind, "ref_id", c.RefID, 128, true); err != nil {
		return err
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
		// 128, not the 256 an earlier revision allowed: LooksLikeSecret
		// already rejects any value over 128 chars unconditionally (above),
		// so a 256 ceiling here was unreachable dead room that only made
		// this kind's declared limit disagree with its JSON Schema twin.
		if len(c.Locator) > 128 {
			return errs.New(errs.CategoryInvalidArgument, "%s: keychain_ref locator exceeds maximum length of 128", kind)
		}
		if !keychainLocatorRegex.MatchString(c.Locator) {
			return errs.New(errs.CategoryInvalidArgument, "%s: keychain_ref locator contains invalid characters or directory traversal, got %q", kind, c.Locator)
		}
	}
	return nil
}

// RecordKind implements Record.
func (c CredentialRef) RecordKind() string { return "CredentialRef" }

// RecordID implements Record.
func (c CredentialRef) RecordID() string { return c.RefID }

// SchemaVer implements Record.
func (c CredentialRef) SchemaVer() SchemaVersion { return c.SchemaVersion }

// AuthEvidenceStatus represents the authoritative outcome of an authentication probe.
type AuthEvidenceStatus string

const (
	// AuthStatusAuthenticated is reserved for an authoritative signal that
	// actually exercised the credential against the provider (e.g. a CLI's
	// own auth-status command succeeding) — never for mere presence of a
	// value somewhere (see AuthProbeEnvPresence/AuthProbeKeychainPresence's
	// doc comments and AuthEvidence.Validate's structural enforcement).
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
	// AuthProbeEnvPresence and AuthProbeKeychainPresence are presence-only
	// checks: they prove a value exists somewhere, never that it is valid,
	// accepted by the provider, unexpired, or bound to a usable session —
	// exactly the same "installed is not authenticated" distinction
	// AuthProbeCLIVersionOnly already draws for CLI version output.
	// AuthEvidence.Validate() structurally forbids AuthStatusAuthenticated
	// for either of these, the same way it already does for
	// AuthProbeCLIVersionOnly.
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
	SchemaVersion SchemaVersion      `json:"schema_version"`
	RefID         string             `json:"ref_id"`
	Kind          CredentialRefKind  `json:"kind"`
	Status        AuthEvidenceStatus `json:"status"`
	ProbeKind     AuthProbeKind      `json:"probe_kind"`
	ObservedAt    Timestamp          `json:"observed_at"`
	ProbeTarget   string             `json:"probe_target,omitempty"`
	AdapterID     string             `json:"adapter_id,omitempty"`
	Detail        string             `json:"detail,omitempty"`
}

// Validate checks that the AuthEvidence record is well-formed and safe.
func (e AuthEvidence) Validate() error {
	const kind = "AuthEvidence"
	if err := e.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := validateOpaqueID(kind, "ref_id", e.RefID, 128, true); err != nil {
		return err
	}
	if err := validateOpaqueID(kind, "adapter_id", e.AdapterID, 128, false); err != nil {
		return err
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

	// Structural binding between Kind and ProbeKind: an env_var reference
	// can only ever be evidenced by an env-presence probe, and so on — a
	// record claiming to describe a keychain_ref's presence via a
	// cli_auth_call probe (or vice versa) is internally inconsistent and
	// must be rejected here rather than trusted by a later reader.
	switch e.Kind {
	case CredRefEnvVar:
		if e.ProbeKind != AuthProbeEnvPresence {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: kind env_var requires probe_kind env_presence, got %q", kind, e.ProbeKind)
		}
	case CredRefKeychainRef:
		if e.ProbeKind != AuthProbeKeychainPresence {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: kind keychain_ref requires probe_kind keychain_presence, got %q", kind, e.ProbeKind)
		}
	case CredRefCLISession:
		if e.ProbeKind != AuthProbeCLIAuthCall && e.ProbeKind != AuthProbeCLIVersionOnly {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: kind cli_session requires probe_kind cli_auth_call or cli_version_only, got %q", kind, e.ProbeKind)
		}
	}

	// ADR-0014 §6: presence/version-only evidence NEVER proves authenticated
	// status — only an authoritative probe that actually exercised the
	// credential against its provider (cli_auth_call) can.
	if e.Status == AuthStatusAuthenticated {
		switch e.ProbeKind {
		case AuthProbeCLIVersionOnly, AuthProbeEnvPresence, AuthProbeKeychainPresence:
			return errs.New(errs.CategoryInvalidArgument,
				"%s: probe_kind %q cannot establish authenticated status (presence/version alone is not proof of authentication)", kind, e.ProbeKind)
		}
	}

	if len(e.ProbeTarget) > 256 {
		return errs.New(errs.CategoryInvalidArgument, "%s: probe_target exceeds maximum length of 256", kind)
	}
	if LooksLikeSecret(e.ProbeTarget) {
		return errs.New(errs.CategoryInvalidArgument, "%s: probe_target looks like a secret value", kind)
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
func (e AuthEvidence) SchemaVer() SchemaVersion { return e.SchemaVersion }

// LooksLikeSecret is a conservative guard against raw credential material reaching
// durable records, configuration, or process boundaries (DCI-081, ADR-0014 §6).
//
// It rejects common secret token prefixes, high-entropy/length heuristics,
// and key-value secret assignments. It does not replace OS secrets storage,
// but prevents accidental pasting of raw secrets into opaque handle locators.
//
// This exact set of prefixes/keywords is mirrored as JSON Schema "not"
// clauses in schemas/credential-ref.schema.json and
// schemas/auth-evidence.schema.json ($defs/noSecretLike) — keep the two in
// sync when changing either (ENGINEERING_STANDARDS.md §5, and see
// TestSchemaSecretPatternParity for the regression that checks it).
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

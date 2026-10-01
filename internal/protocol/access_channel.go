package protocol

import "github.com/olostan/DevCadence/internal/errs"

// ChannelKind distinguishes the operational pathway to model cognition (ADR-0018 §2).
type ChannelKind string

const (
	ChannelDirectHTTPAPI     ChannelKind = "direct_http_api"
	ChannelCLISubprocess     ChannelKind = "cli_subprocess"
	ChannelLocalDaemonSocket ChannelKind = "local_daemon_socket"
	ChannelRemoteAgentProxy  ChannelKind = "remote_agent_proxy"
)

// Valid reports whether the channel kind is defined by the schema.
func (k ChannelKind) Valid() bool {
	switch k {
	case ChannelDirectHTTPAPI, ChannelCLISubprocess, ChannelLocalDaemonSocket, ChannelRemoteAgentProxy:
		return true
	}
	return false
}

// SessionMode describes how session state is persisted across requests.
type SessionMode string

const (
	SessionStatelessPerCall SessionMode = "stateless_per_call"
	SessionPersistentState  SessionMode = "persistent_session"
	SessionResumableHandle  SessionMode = "resumable_session"
)

// Valid reports whether the session mode is defined by the schema.
func (s SessionMode) Valid() bool {
	switch s {
	case SessionStatelessPerCall, SessionPersistentState, SessionResumableHandle:
		return true
	}
	return false
}

// ContextControl describes the adapter's prompt controllability (ADR-0019 §1).
type ContextControl string

const (
	ContextControlExactStateless ContextControl = "exact_stateless"
	ContextControlAppendOnly     ContextControl = "append_only"
	ContextControlOpaqueSession  ContextControl = "opaque_session"
)

// Valid reports whether the context control capability is known.
func (c ContextControl) Valid() bool {
	switch c {
	case ContextControlExactStateless, ContextControlAppendOnly, ContextControlOpaqueSession:
		return true
	}
	return false
}

// PrefixCache describes observable KV/prefix caching capabilities (ADR-0019 §1).
type PrefixCache string

const (
	PrefixCacheExplicit  PrefixCache = "explicit"
	PrefixCacheImplicit  PrefixCache = "implicit"
	PrefixCacheSessionKV PrefixCache = "session_kv"
	PrefixCacheNone      PrefixCache = "none"
)

// Valid reports whether the prefix cache capability is known.
func (p PrefixCache) Valid() bool {
	switch p {
	case PrefixCacheExplicit, PrefixCacheImplicit, PrefixCacheSessionKV, PrefixCacheNone:
		return true
	}
	return false
}

// AccessChannel represents a concrete access path to an endpoint.
// It decouples endpoint and model identity from how cognition is reached (ADR-0018 §2).
type AccessChannel struct {
	SchemaVersion         SchemaVersion  `json:"schema_version"`
	ChannelID             string         `json:"channel_id"`
	EndpointID            string         `json:"endpoint_id"`
	Kind                  ChannelKind    `json:"kind"`
	SessionMode           SessionMode    `json:"session_mode"`
	ContextControl        ContextControl `json:"context_control"`
	PrefixCache           PrefixCache    `json:"prefix_cache"`
	SupportsStreaming     bool           `json:"supports_streaming"`
	SupportsTools         bool           `json:"supports_tools"`
	NativeWorktreeAccess  bool           `json:"native_worktree_access"`
	CredentialRefID       *string        `json:"credential_ref_id,omitempty"`
	MaxConcurrentRequests int            `json:"max_concurrent_requests"`
}

// RecordKind implements Record.
func (a *AccessChannel) RecordKind() string { return "AccessChannel" }

// RecordID implements Record.
func (a *AccessChannel) RecordID() string { return a.ChannelID }

// SchemaVer implements Record.
func (a *AccessChannel) SchemaVer() SchemaVersion { return a.SchemaVersion }

// Validate enforces schema constraints for AccessChannel.
func (a *AccessChannel) Validate() error {
	const kind = "AccessChannel"
	if err := a.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "channel_id", a.ChannelID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "endpoint_id", a.EndpointID); err != nil {
		return err
	}
	if !a.Kind.Valid() {
		return enumError(kind, "kind", string(a.Kind),
			string(ChannelDirectHTTPAPI), string(ChannelCLISubprocess), string(ChannelLocalDaemonSocket), string(ChannelRemoteAgentProxy))
	}
	if !a.SessionMode.Valid() {
		return enumError(kind, "session_mode", string(a.SessionMode),
			string(SessionStatelessPerCall), string(SessionPersistentState), string(SessionResumableHandle))
	}
	if !a.ContextControl.Valid() {
		return enumError(kind, "context_control", string(a.ContextControl),
			string(ContextControlExactStateless), string(ContextControlAppendOnly), string(ContextControlOpaqueSession))
	}
	if !a.PrefixCache.Valid() {
		return enumError(kind, "prefix_cache", string(a.PrefixCache),
			string(PrefixCacheExplicit), string(PrefixCacheImplicit), string(PrefixCacheSessionKV), string(PrefixCacheNone))
	}
	if a.MaxConcurrentRequests < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_concurrent_requests must be >= 1, got %d", kind, a.MaxConcurrentRequests)
	}
	if a.CredentialRefID != nil && *a.CredentialRefID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: credential_ref_id cannot be empty string when provided", kind)
	}
	return nil
}

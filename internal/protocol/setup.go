package protocol

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// Authority specifies the minimum approval level required to execute an action.
type Authority string

const (
	AuthorityReadOnly               Authority = "read_only"
	AuthorityUserConfirmation       Authority = "user_confirmation"
	AuthorityPrivilegedConfirmation Authority = "privileged_confirmation"
	AuthorityHighImpactManual       Authority = "high_impact_manual"
)

func (a Authority) Valid() bool {
	switch a {
	case AuthorityReadOnly, AuthorityUserConfirmation, AuthorityPrivilegedConfirmation, AuthorityHighImpactManual:
		return true
	}
	return false
}

func (a Authority) rank() int {
	switch a {
	case AuthorityReadOnly:
		return 0
	case AuthorityUserConfirmation:
		return 1
	case AuthorityPrivilegedConfirmation:
		return 2
	case AuthorityHighImpactManual:
		return 3
	default:
		return -1
	}
}

// Rank returns the numeric precedence rank for this authority.
func (a Authority) Rank() int {
	return a.rank()
}

// AtLeast reports whether a meets or exceeds want.
func (a Authority) AtLeast(want Authority) bool {
	return a.rank() >= want.rank()
}

// EffectCategory flags the specific operational domain a SetupAction alters.
type EffectCategory string

const (
	EffectFilesystemWrite        EffectCategory = "filesystem_write"
	EffectNetworkAccess          EffectCategory = "network_access"
	EffectPackageDownload        EffectCategory = "package_download"
	EffectModelDownload          EffectCategory = "model_download"
	EffectServiceModification    EffectCategory = "service_modification"
	EffectAuthentication         EffectCategory = "authentication"
	EffectCredentialAccess       EffectCategory = "credential_access"
	EffectPrivilegeElevation     EffectCategory = "privilege_elevation"
	EffectDevicePermissionChange EffectCategory = "device_permission_change"
	EffectRepositoryMutation     EffectCategory = "repository_mutation"
)

func (e EffectCategory) Valid() bool {
	switch e {
	case EffectFilesystemWrite, EffectNetworkAccess, EffectPackageDownload, EffectModelDownload,
		EffectServiceModification, EffectAuthentication, EffectCredentialAccess, EffectPrivilegeElevation,
		EffectDevicePermissionChange, EffectRepositoryMutation:
		return true
	}
	return false
}

// SetupTarget identifies a bounded setup scope.
type SetupTarget string

const (
	TargetAll       SetupTarget = "all"
	TargetHardware  SetupTarget = "hardware"
	TargetInference SetupTarget = "inference"
	TargetCognition SetupTarget = "cognition"
	TargetAuth      SetupTarget = "auth"
)

func (t SetupTarget) Valid() bool {
	switch t {
	case TargetAll, TargetHardware, TargetInference, TargetCognition, TargetAuth:
		return true
	}
	return false
}

// ManagedDirectoryLocation restricts filesystem directory creation to executor-derived locations under $DEVCADENCE_HOME.
type ManagedDirectoryLocation string

const (
	LocationState          ManagedDirectoryLocation = "state"
	LocationArtifactsSetup ManagedDirectoryLocation = "artifacts_setup"
	LocationTmp            ManagedDirectoryLocation = "tmp"
)

func (l ManagedDirectoryLocation) Valid() bool {
	switch l {
	case LocationState, LocationArtifactsSetup, LocationTmp:
		return true
	}
	return false
}

// CacheTarget restricts cache eviction to known, managed operational objects.
type CacheTarget string

const (
	CacheTargetMachineProfile CacheTarget = "machine_profile"
	CacheTargetEndpointProbes CacheTarget = "endpoint_probes"
)

func (c CacheTarget) Valid() bool {
	switch c {
	case CacheTargetMachineProfile, CacheTargetEndpointProbes:
		return true
	}
	return false
}

// ManagedConfigKey restricts configuration mutation to allowlisted non-secret settings.
type ManagedConfigKey string

const (
	ConfigKeyDefaultProfile        ManagedConfigKey = "default_profile"
	ConfigKeyMaxConcurrentSessions ManagedConfigKey = "max_concurrent_sessions"
	ConfigKeyLogVerbosity          ManagedConfigKey = "log_verbosity"
)

func (k ManagedConfigKey) Valid() bool {
	switch k {
	case ConfigKeyDefaultProfile, ConfigKeyMaxConcurrentSessions, ConfigKeyLogVerbosity:
		return true
	}
	return false
}

func (k ManagedConfigKey) ValidateValue(val string) error {
	if val == "" {
		return errs.New(errs.CategoryInvalidArgument, "config value for %s cannot be empty", string(k))
	}
	switch k {
	case ConfigKeyDefaultProfile:
		p := DeploymentProfile(val)
		if !p.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "invalid default_profile %q", val)
		}
	case ConfigKeyMaxConcurrentSessions:
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			return errs.New(errs.CategoryInvalidArgument, "max_concurrent_sessions must be a positive integer, got %q", val)
		}
	case ConfigKeyLogVerbosity:
		switch val {
		case "debug", "info", "warn", "error":
			return nil
		default:
			return errs.New(errs.CategoryInvalidArgument, "invalid log_verbosity %q", val)
		}
	}
	return nil
}

// DiagnosticCheckName identifies a code-owned, non-shell diagnostic probe.
type DiagnosticCheckName string

const (
	CheckGitAvailable      DiagnosticCheckName = "git_available"
	CheckOllamaResponding  DiagnosticCheckName = "ollama_responding"
	CheckMLXImportable     DiagnosticCheckName = "mlx_importable"
	CheckStateRootWritable DiagnosticCheckName = "state_root_writable"
)

func (d DiagnosticCheckName) Valid() bool {
	switch d {
	case CheckGitAvailable, CheckOllamaResponding, CheckMLXImportable, CheckStateRootWritable:
		return true
	}
	return false
}

// OperationKind identifies a supported closed operation.
type OperationKind string

const (
	// OpKindEnsureLocalModel is a runtime-agnostic "acquire this immutable
	// model for this local runtime" operation. Runtime is an opaque,
	// adapter-scoped identifier (e.g. "ollama", "mlx") — every local model
	// runtime is a peer behind this one operation kind; no runtime is the
	// default or the core-domain dependency (INVARIANTS.md DCI-055,
	// docs/MODEL_RUNTIME.md). Runtime-specific mechanics (pull command,
	// identity/revision scheme, presence verification) live entirely in
	// the executor-layer adapter for that Runtime value, never here.
	OpKindEnsureLocalModel   OperationKind = "ensure_local_model"
	OpKindCreateDirectory    OperationKind = "create_directory"
	OpKindWriteManagedConfig OperationKind = "write_managed_config"
	OpKindRemoveStaleCache   OperationKind = "remove_stale_cache"
	OpKindRunDiagnosticCheck OperationKind = "run_diagnostic_check"
)

func (k OperationKind) Valid() bool {
	switch k {
	case OpKindEnsureLocalModel, OpKindCreateDirectory, OpKindWriteManagedConfig, OpKindRemoveStaleCache, OpKindRunDiagnosticCheck:
		return true
	}
	return false
}

type TypedOperation struct {
	Kind               OperationKind             `json:"kind"`
	EnsureLocalModel   *EnsureLocalModelParams   `json:"ensure_local_model,omitempty"`
	CreateDirectory    *CreateDirectoryParams    `json:"create_directory,omitempty"`
	WriteManagedConfig *WriteManagedConfigParams `json:"write_managed_config,omitempty"`
	RemoveStaleCache   *RemoveStaleCacheParams   `json:"remove_stale_cache,omitempty"`
	RunDiagnosticCheck *RunDiagnosticCheckParams `json:"run_diagnostic_check,omitempty"`
}

// EnsureLocalModelParams identifies an immutable model to acquire for a
// named local runtime. Fields are deliberately runtime-agnostic:
//   - ModelRef is the runtime-scoped model identity (an Ollama tag, an
//     MLX/Hugging Face repo id, or whatever the next runtime adapter uses)
//     — opaque to everything except that runtime's adapter.
//   - ResolvedRevision is the immutable pin within that identity (Ollama's
//     manifest digest, an HF commit SHA, etc.) — also adapter-interpreted,
//     which is why it is not constrained to sha256-hex here the way other
//     protocol digests are: the whole point of a runtime-agnostic type is
//     that this package does not get to assume every runtime's immutable
//     pin looks like Ollama's.
//   - ExpectedSizeBytes/AllowedSource are bounded supply-chain metadata
//     ADR-0014 §7 requires, and each runtime adapter enforces them against
//     what it actually fetches (e.g. OllamaAdapter checks the pulled
//     manifest's exact digest/size against the live registry; MLXAdapter
//     checks the downloaded snapshot's measured size and rejects any
//     AllowedSource other than "huggingface.co").
//   - LicenseReference is approval/provenance metadata only — it records
//     what license the plan was approved under, for audit purposes. No
//     current adapter verifies it against the runtime's own fetched
//     metadata (neither Ollama's registry API nor Hugging Face's model API
//     response is treated as an authoritative license source here), so it
//     must not be read as a runtime-enforced field the way
//     ExpectedSizeBytes/AllowedSource are.
type EnsureLocalModelParams struct {
	Runtime           string `json:"runtime"`
	ModelRef          string `json:"model_ref"`
	ResolvedRevision  string `json:"resolved_revision"`
	ExpectedSizeBytes int64  `json:"expected_size_bytes"`
	AllowedSource     string `json:"allowed_source"`
	LicenseReference  string `json:"license_reference"`
}

type CreateDirectoryParams struct {
	Location    ManagedDirectoryLocation `json:"location"`
	FileModeOct string                   `json:"file_mode_oct"`
}

type WriteManagedConfigParams struct {
	Key   ManagedConfigKey `json:"key"`
	Value string           `json:"value"`
}

type RemoveStaleCacheParams struct {
	Target CacheTarget `json:"target"`
}

type RunDiagnosticCheckParams struct {
	CheckName DiagnosticCheckName `json:"check_name"`
	TargetID  string              `json:"target_id,omitempty"`
}

var hexSha256Regex = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func (o TypedOperation) Validate() error {
	const kind = "TypedOperation"
	if !o.Kind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid kind %q", kind, string(o.Kind))
	}
	count := 0
	if o.EnsureLocalModel != nil {
		count++
	}
	if o.CreateDirectory != nil {
		count++
	}
	if o.WriteManagedConfig != nil {
		count++
	}
	if o.RemoveStaleCache != nil {
		count++
	}
	if o.RunDiagnosticCheck != nil {
		count++
	}
	if count != 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: exactly one payload must be set, got %d", kind, count)
	}

	switch o.Kind {
	case OpKindEnsureLocalModel:
		p := o.EnsureLocalModel
		if p == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: ensure_local_model payload is required for kind %q", kind, o.Kind)
		}
		if p.Runtime == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: runtime is required", kind)
		}
		if p.ModelRef == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: model_ref is required", kind)
		}
		if p.ResolvedRevision == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: resolved_revision is required", kind)
		}
		if p.ExpectedSizeBytes <= 0 {
			return errs.New(errs.CategoryInvalidArgument, "%s: expected_size_bytes must be positive, got %d", kind, p.ExpectedSizeBytes)
		}
		if p.AllowedSource == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: allowed_source is required", kind)
		}
		if p.LicenseReference == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: license_reference is required", kind)
		}
	case OpKindCreateDirectory:
		p := o.CreateDirectory
		if p == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: create_directory payload is required for kind %q", kind, o.Kind)
		}
		if !p.Location.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid location %q", kind, p.Location)
		}
		if p.FileModeOct != "0700" {
			return errs.New(errs.CategoryInvalidArgument, "%s: file_mode_oct must be \"0700\", got %q", kind, p.FileModeOct)
		}
	case OpKindWriteManagedConfig:
		p := o.WriteManagedConfig
		if p == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: write_managed_config payload is required for kind %q", kind, o.Kind)
		}
		if !p.Key.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid key %q", kind, p.Key)
		}
		if err := p.Key.ValidateValue(p.Value); err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "%s", kind)
		}
	case OpKindRemoveStaleCache:
		p := o.RemoveStaleCache
		if p == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: remove_stale_cache payload is required for kind %q", kind, o.Kind)
		}
		if !p.Target.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid target %q", kind, p.Target)
		}
	case OpKindRunDiagnosticCheck:
		p := o.RunDiagnosticCheck
		if p == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: run_diagnostic_check payload is required for kind %q", kind, o.Kind)
		}
		if !p.CheckName.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid check_name %q", kind, p.CheckName)
		}
	}
	return nil
}

// IntrinsicPolicy defines the intrinsic minimum authority and required effect categories for an operation.
func IntrinsicPolicy(op TypedOperation) ([]EffectCategory, Authority) {
	switch op.Kind {
	case OpKindEnsureLocalModel:
		return []EffectCategory{EffectNetworkAccess, EffectModelDownload, EffectFilesystemWrite}, AuthorityUserConfirmation
	case OpKindCreateDirectory:
		return []EffectCategory{EffectFilesystemWrite}, AuthorityUserConfirmation
	case OpKindWriteManagedConfig:
		return []EffectCategory{EffectFilesystemWrite}, AuthorityUserConfirmation
	case OpKindRemoveStaleCache:
		return []EffectCategory{EffectFilesystemWrite}, AuthorityUserConfirmation
	case OpKindRunDiagnosticCheck:
		return []EffectCategory{EffectNetworkAccess}, AuthorityReadOnly
	default:
		return nil, AuthorityHighImpactManual
	}
}

// ConditionKind identifies a closed condition.
type ConditionKind string

const (
	CondKindCommandAvailable   ConditionKind = "command_available"
	CondKindExecutableVerified ConditionKind = "executable_verified"
	CondKindManagedDirExists   ConditionKind = "managed_dir_exists"
	CondKindPortListening      ConditionKind = "port_listening"
	CondKindEndpointHealthy    ConditionKind = "endpoint_healthy"
	// CondKindModelPresent is the runtime-agnostic counterpart to
	// OpKindEnsureLocalModel: "this runtime has this immutable model
	// revision available." Presence/revision semantics are entirely
	// adapter-interpreted (see ModelPresentOperand and
	// EnsureLocalModelParams's doc comment) — this package never assumes
	// every runtime represents model identity the way Ollama's manifest
	// digest does.
	CondKindModelPresent ConditionKind = "model_present"
)

func (k ConditionKind) Valid() bool {
	switch k {
	case CondKindCommandAvailable, CondKindExecutableVerified, CondKindManagedDirExists, CondKindPortListening, CondKindEndpointHealthy, CondKindModelPresent:
		return true
	}
	return false
}

type Condition struct {
	Kind               ConditionKind              `json:"kind"`
	CommandAvailable   *CommandAvailableOperand   `json:"command_available,omitempty"`
	ExecutableVerified *ExecutableVerifiedOperand `json:"executable_verified,omitempty"`
	ManagedDirExists   *ManagedDirOperand         `json:"managed_dir_exists,omitempty"`
	PortListening      *PortOperand               `json:"port_listening,omitempty"`
	EndpointHealthy    *EndpointOperand           `json:"endpoint_healthy,omitempty"`
	ModelPresent       *ModelPresentOperand       `json:"model_present,omitempty"`
}

type CommandAvailableOperand struct {
	CommandName string `json:"command_name"`
}

type ExecutableVerifiedOperand struct {
	CanonicalPath   string `json:"canonical_path"`
	ExpectedVersion string `json:"expected_version"`
	ExpectedDigest  string `json:"expected_digest,omitempty"`
	// VersionArgs is the argv that makes CanonicalPath print its version,
	// checked against ExpectedVersion. Empty means ["--version"] — the
	// common case, but not universal: e.g. the current Hugging Face Hub
	// CLI exposes a "version" subcommand rather than a "--version" flag
	// (https://huggingface.co/docs/huggingface_hub/main/package_reference/cli).
	// This mirrors internal/environment.SoftwareDescriptor.VersionArgs,
	// which the same per-tool variation already required at discovery
	// time — this field lets a setup plan's own executable_verified
	// precondition agree with however that tool was actually discovered,
	// instead of hardcoding one flag convention for every tool.
	VersionArgs []string `json:"version_args,omitempty"`
}

type ManagedDirOperand struct {
	Location    ManagedDirectoryLocation `json:"location"`
	FileModeOct string                   `json:"file_mode_oct"`
}

type PortOperand struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type EndpointOperand struct {
	EndpointID string `json:"endpoint_id"`
}

// ModelPresentOperand mirrors EnsureLocalModelParams's identity and size
// fields — see that type's doc comment for why Runtime/ModelRef/
// ResolvedRevision are opaque, adapter-interpreted strings rather than a
// fixed sha256-hex digest. ExpectedSizeBytes matters here specifically
// because this condition is also what Executor.Recover uses to decide
// whether an interrupted ensure_local_model action actually succeeded: for
// a runtime whose ResolvedRevision alone does not cryptographically prove
// content completeness (e.g. MLX's snapshot-directory existence, unlike
// Ollama's content-addressed manifest digest), the adapter needs the
// approved size to distinguish a complete download from a partial or
// corrupted one during recovery. 0 means "not checked" for adapters that
// don't need it (e.g. Ollama, whose digest already proves completeness).
type ModelPresentOperand struct {
	Runtime           string `json:"runtime"`
	ModelRef          string `json:"model_ref"`
	ResolvedRevision  string `json:"resolved_revision"`
	ExpectedSizeBytes int64  `json:"expected_size_bytes,omitempty"`
}

func (c Condition) Validate() error {
	const kind = "Condition"
	if !c.Kind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid kind %q", kind, string(c.Kind))
	}
	count := 0
	if c.CommandAvailable != nil {
		count++
	}
	if c.ExecutableVerified != nil {
		count++
	}
	if c.ManagedDirExists != nil {
		count++
	}
	if c.PortListening != nil {
		count++
	}
	if c.EndpointHealthy != nil {
		count++
	}
	if c.ModelPresent != nil {
		count++
	}
	if count != 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: exactly one operand must be set, got %d", kind, count)
	}

	switch c.Kind {
	case CondKindCommandAvailable:
		if c.CommandAvailable == nil || c.CommandAvailable.CommandName == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: command_name is required", kind)
		}
	case CondKindExecutableVerified:
		if c.ExecutableVerified == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: executable_verified operand is required", kind)
		}
		if !strings.HasPrefix(c.ExecutableVerified.CanonicalPath, "/") {
			return errs.New(errs.CategoryInvalidArgument, "%s: canonical_path must be absolute, got %q", kind, c.ExecutableVerified.CanonicalPath)
		}
		if c.ExecutableVerified.ExpectedVersion == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: expected_version is required", kind)
		}
		if c.ExecutableVerified.ExpectedDigest != "" && !hexSha256Regex.MatchString(c.ExecutableVerified.ExpectedDigest) {
			return errs.New(errs.CategoryInvalidArgument, "%s: expected_digest must be sha256 hex, got %q", kind, c.ExecutableVerified.ExpectedDigest)
		}
	case CondKindManagedDirExists:
		if c.ManagedDirExists == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: managed_dir_exists operand is required", kind)
		}
		if !c.ManagedDirExists.Location.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid location %q", kind, c.ManagedDirExists.Location)
		}
		if c.ManagedDirExists.FileModeOct != "0700" {
			return errs.New(errs.CategoryInvalidArgument, "%s: file_mode_oct must be \"0700\", got %q", kind, c.ManagedDirExists.FileModeOct)
		}
	case CondKindPortListening:
		if c.PortListening == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: port_listening operand is required", kind)
		}
		switch c.PortListening.Host {
		case "localhost", "127.0.0.1", "::1":
			// Loopback probe only (ADR-0014)
		default:
			return errs.New(errs.CategoryInvalidArgument, "%s: port_listening host must be loopback only (localhost, 127.0.0.1, ::1), got %q", kind, c.PortListening.Host)
		}
		if c.PortListening.Port <= 0 || c.PortListening.Port > 65535 {
			return errs.New(errs.CategoryInvalidArgument, "%s: port must be 1-65535, got %d", kind, c.PortListening.Port)
		}
	case CondKindEndpointHealthy:
		if c.EndpointHealthy == nil || c.EndpointHealthy.EndpointID == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: endpoint_id is required", kind)
		}
	case CondKindModelPresent:
		if c.ModelPresent == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: model_present operand is required", kind)
		}
		if c.ModelPresent.Runtime == "" || c.ModelPresent.ModelRef == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: runtime and model_ref are required", kind)
		}
		if c.ModelPresent.ResolvedRevision == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: resolved_revision is required", kind)
		}
		if c.ModelPresent.ExpectedSizeBytes < 0 {
			return errs.New(errs.CategoryInvalidArgument, "%s: expected_size_bytes must not be negative", kind)
		}
	}
	return nil
}

// MutationKind categorises expected mutations.
type MutationKind string

const (
	MutationDirectoryCreated MutationKind = "directory_created"
	MutationConfigKeySet     MutationKind = "config_key_set"
	MutationModelPulled      MutationKind = "model_pulled"
	MutationCacheRemoved     MutationKind = "cache_removed"
)

func (k MutationKind) Valid() bool {
	switch k {
	case MutationDirectoryCreated, MutationConfigKeySet, MutationModelPulled, MutationCacheRemoved:
		return true
	}
	return false
}

type ExpectedMutation struct {
	Kind   MutationKind `json:"kind"`
	Target string       `json:"target"`
	Detail string       `json:"detail"`
}

func (m ExpectedMutation) Validate() error {
	const kind = "ExpectedMutation"
	if !m.Kind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid kind %q", kind, string(m.Kind))
	}
	if m.Target == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: target is required", kind)
	}
	if m.Detail == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: detail is required", kind)
	}
	return nil
}

type ManualGuide struct {
	Summary           string      `json:"summary"`
	Steps             []string    `json:"steps"`
	VerificationCheck []Condition `json:"verification_check"`
}

func (g ManualGuide) Validate() error {
	const kind = "ManualGuide"
	if g.Summary == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: summary is required", kind)
	}
	if len(g.Steps) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: at least one step is required", kind)
	}
	for i, step := range g.Steps {
		if step == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: step %d cannot be empty", kind, i)
		}
	}
	if len(g.VerificationCheck) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: at least one verification_check condition is required", kind)
	}
	for _, c := range g.VerificationCheck {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type SetupAction struct {
	ActionID           string             `json:"action_id"`
	RecipeID           string             `json:"recipe_id"`
	RecipeVersion      string             `json:"recipe_version"`
	Title              string             `json:"title"`
	Description        string             `json:"description"`
	Authority          Authority          `json:"authority"`
	Effects            []EffectCategory   `json:"effects"`
	Preconditions      []Condition        `json:"preconditions"`
	Postconditions     []Condition        `json:"postconditions"`
	ExpectedMutations  []ExpectedMutation `json:"expected_mutations"`
	IdempotencyKey     string             `json:"idempotency_key"`
	DependsOn          []string           `json:"depends_on,omitempty"`
	Operation          *TypedOperation    `json:"operation,omitempty"`
	ManualInstructions *ManualGuide       `json:"manual_instructions,omitempty"`
}

func (a SetupAction) Validate() error {
	const kind = "SetupAction"
	if a.ActionID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: action_id is required", kind)
	}
	if a.RecipeID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: recipe_id is required", kind)
	}
	if a.RecipeVersion == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: recipe_version is required", kind)
	}
	if a.Title == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: title is required", kind)
	}
	if a.Description == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: description is required", kind)
	}
	if !a.Authority.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid authority %q", kind, string(a.Authority))
	}
	for _, e := range a.Effects {
		if !e.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid effect %q", kind, string(e))
		}
	}
	for _, c := range a.Preconditions {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	for _, c := range a.Postconditions {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	for _, m := range a.ExpectedMutations {
		if err := m.Validate(); err != nil {
			return err
		}
	}
	if a.IdempotencyKey == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: idempotency_key is required", kind)
	}

	// Mutual exclusion:
	// AuthorityHighImpactManual must have ManualInstructions and Operation == nil.
	// Executable actions must have Operation != nil and ManualInstructions == nil.
	if a.Authority == AuthorityHighImpactManual {
		if a.Operation != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: high_impact_manual action must have operation == nil", kind)
		}
		if a.ManualInstructions == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: high_impact_manual action requires manual_instructions", kind)
		}
		if err := a.ManualInstructions.Validate(); err != nil {
			return err
		}
	} else {
		if a.Operation == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: executable action requires non-nil operation", kind)
		}
		if a.ManualInstructions != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: executable action must not have manual_instructions", kind)
		}
		if err := a.Operation.Validate(); err != nil {
			return err
		}

		// Structural identity binding: an ensure_local_model action's
		// approved model identity — and approved size — must be exactly
		// what its own model_present postcondition asks for. Without this,
		// a plan could approve pulling model A (or a given size) in the
		// operation while declaring success against model B's (or an
		// unbounded) postcondition — the postcondition would then either
		// never hold (masking the real failure behind a generic
		// "postcondition failed" error) or, worse, a future adapter could
		// satisfy it by coincidence. ExpectedSizeBytes is included in the
		// match (not just identity) because model_present's ExpectedSizeBytes
		// is also what Executor.Recover relies on to tell a complete
		// download from a partial/corrupted one during crash recovery for a
		// runtime whose revision alone doesn't cryptographically prove
		// completeness (see ModelPresentOperand's doc comment) — a plan
		// approving one size but asking recovery to accept any size would
		// silently weaken that recovery check. Requiring at least one
		// matching model_present postcondition makes the binding structural
		// rather than dependent on the adapter's own internal checks (which
		// do still independently verify the pulled model, per-adapter).
		if a.Operation.Kind == OpKindEnsureLocalModel && a.Operation.EnsureLocalModel != nil {
			op := a.Operation.EnsureLocalModel
			found := false
			for _, c := range a.Postconditions {
				if c.Kind != CondKindModelPresent || c.ModelPresent == nil {
					continue
				}
				if c.ModelPresent.Runtime == op.Runtime && c.ModelPresent.ModelRef == op.ModelRef &&
					c.ModelPresent.ResolvedRevision == op.ResolvedRevision && c.ModelPresent.ExpectedSizeBytes == op.ExpectedSizeBytes {
					found = true
					break
				}
			}
			if !found {
				return errs.New(errs.CategoryInvalidArgument,
					"%s: ensure_local_model action requires a model_present postcondition with the identical identity and expected_size_bytes (runtime=%q, model_ref=%q, resolved_revision=%q, expected_size_bytes=%d)",
					kind, op.Runtime, op.ModelRef, op.ResolvedRevision, op.ExpectedSizeBytes)
			}
		}

		// Intrinsic policy check:
		intrinsicEffects, minAuth := IntrinsicPolicy(*a.Operation)
		if !a.Authority.AtLeast(minAuth) {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: declared authority %q is weaker than intrinsic policy %q for operation %q",
				kind, a.Authority, minAuth, a.Operation.Kind)
		}
		for _, reqEffect := range intrinsicEffects {
			if !slices.Contains(a.Effects, reqEffect) {
				return errs.New(errs.CategoryInvalidArgument,
					"%s: missing required intrinsic effect %q for operation %q",
					kind, reqEffect, a.Operation.Kind)
			}
		}
	}

	return nil
}

type SetupPlan struct {
	SchemaVersion      SchemaVersion    `json:"schema_version"`
	PlanID             string           `json:"plan_id"`
	PlanDigest         string           `json:"plan_digest"`
	RecipeSetVersion   string           `json:"recipe_set_version"`
	MachineFingerprint string           `json:"machine_fingerprint"`
	CreatedAt          Timestamp        `json:"created_at"`
	Target             SetupTarget      `json:"target"`
	Actions            []SetupAction    `json:"actions"`
	RequiredAuthority  Authority        `json:"required_authority"`
	TotalEffects       []EffectCategory `json:"total_effects"`
}

type setupPlanDigestView struct {
	SchemaVersion      SchemaVersion    `json:"schema_version"`
	PlanID             string           `json:"plan_id"`
	RecipeSetVersion   string           `json:"recipe_set_version"`
	MachineFingerprint string           `json:"machine_fingerprint"`
	CreatedAt          Timestamp        `json:"created_at"`
	Target             SetupTarget      `json:"target"`
	Actions            []SetupAction    `json:"actions"`
	RequiredAuthority  Authority        `json:"required_authority"`
	TotalEffects       []EffectCategory `json:"total_effects"`
}

func (p *SetupPlan) RecordKind() string       { return "SetupPlan" }
func (p *SetupPlan) RecordID() string         { return p.PlanID }
func (p *SetupPlan) SchemaVer() SchemaVersion { return p.SchemaVersion }

func (p *SetupPlan) Validate() error {
	const kind = "SetupPlan"
	if err := p.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "plan_id", p.PlanID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "recipe_set_version", p.RecipeSetVersion); err != nil {
		return err
	}
	if !hexSha256Regex.MatchString(p.MachineFingerprint) {
		return errs.New(errs.CategoryInvalidArgument, "%s: machine_fingerprint must be sha256 hex, got %q", kind, p.MachineFingerprint)
	}
	if p.CreatedAt.Time().IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: created_at is required", kind)
	}
	if !p.Target.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid target %q", kind, string(p.Target))
	}
	if !p.RequiredAuthority.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid required_authority %q", kind, string(p.RequiredAuthority))
	}
	for _, eff := range p.TotalEffects {
		if !eff.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid effect %q in total_effects", kind, string(eff))
		}
	}

	// Verify actions and aggregate authority/effects
	seenActionIDs := make(map[string]bool, len(p.Actions))
	seenIdempotencyKeys := make(map[string]bool, len(p.Actions))
	actionIndex := make(map[string]int, len(p.Actions))

	maxAuthority := AuthorityReadOnly
	effectsSet := make(map[EffectCategory]bool)

	for i, act := range p.Actions {
		if err := act.Validate(); err != nil {
			return err
		}
		if seenActionIDs[act.ActionID] {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate action_id %q", kind, act.ActionID)
		}
		seenActionIDs[act.ActionID] = true
		actionIndex[act.ActionID] = i

		if seenIdempotencyKeys[act.IdempotencyKey] {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate idempotency_key %q", kind, act.IdempotencyKey)
		}
		seenIdempotencyKeys[act.IdempotencyKey] = true

		if act.Authority.Rank() > maxAuthority.Rank() {
			maxAuthority = act.Authority
		}
		for _, e := range act.Effects {
			effectsSet[e] = true
		}
	}

	// Dependency graph validation:
	// 1. Every dependency must exist.
	// 2. No dependency cycles (must depend on strictly earlier actions in ordered list).
	for _, act := range p.Actions {
		currIdx := actionIndex[act.ActionID]
		for _, depID := range act.DependsOn {
			depIdx, exists := actionIndex[depID]
			if !exists {
				return errs.New(errs.CategoryInvalidArgument, "%s: action %q has missing dependency %q", kind, act.ActionID, depID)
			}
			if depIdx >= currIdx {
				return errs.New(errs.CategoryInvalidArgument, "%s: action %q has forward dependency or cycle on %q", kind, act.ActionID, depID)
			}
		}
	}

	if len(p.Actions) > 0 && p.RequiredAuthority != maxAuthority {
		return errs.New(errs.CategoryInvalidArgument, "%s: required_authority %q does not match maximum action authority %q", kind, p.RequiredAuthority, maxAuthority)
	}

	// Verify TotalEffects matches aggregate effects
	for _, e := range p.TotalEffects {
		if !effectsSet[e] {
			return errs.New(errs.CategoryInvalidArgument, "%s: total_effects contains undeclared effect %q", kind, e)
		}
	}
	if len(p.TotalEffects) != len(effectsSet) {
		return errs.New(errs.CategoryInvalidArgument, "%s: total_effects count %d does not match aggregate effects count %d", kind, len(p.TotalEffects), len(effectsSet))
	}

	// Verify PlanDigest is non-empty, hex sha256, and matches computed digest
	if err := requireNonEmpty(kind, "plan_digest", p.PlanDigest); err != nil {
		return err
	}
	if !hexSha256Regex.MatchString(p.PlanDigest) {
		return errs.New(errs.CategoryInvalidArgument, "%s: plan_digest must be sha256 hex, got %q", kind, p.PlanDigest)
	}
	expectedDigest, err := ComputePlanDigest(p)
	if err != nil {
		return err
	}
	if p.PlanDigest != expectedDigest {
		return errs.New(errs.CategoryInvalidArgument, "%s: plan_digest mismatch: got %q, expected %q", kind, p.PlanDigest, expectedDigest)
	}

	return nil
}

// ComputePlanDigest calculates the SHA-256 digest of the canonical JSON encoding
// of the complete saved SetupPlan with only the plan_digest field omitted (ADR-0014).
func ComputePlanDigest(p *SetupPlan) (string, error) {
	view := setupPlanDigestView{
		SchemaVersion:      p.SchemaVersion,
		PlanID:             p.PlanID,
		RecipeSetVersion:   p.RecipeSetVersion,
		MachineFingerprint: p.MachineFingerprint,
		CreatedAt:          p.CreatedAt,
		Target:             p.Target,
		Actions:            p.Actions,
		RequiredAuthority:  p.RequiredAuthority,
		TotalEffects:       p.TotalEffects,
	}
	canonical, err := CanonicalJSON(&view)
	if err != nil {
		return "", err
	}
	return DigestBytes(canonical), nil
}

// ComputePlanDigest calculates the SHA-256 digest on the receiver.
func (p *SetupPlan) ComputePlanDigest() (string, error) {
	return ComputePlanDigest(p)
}

// CredentialRefKind specifies the storage/resolution mechanism for an opaque credential reference.
type CredentialRefKind string

const (
	CredRefEnvVar      CredentialRefKind = "env_var"
	CredRefCLISession  CredentialRefKind = "cli_session"
	CredRefKeychainRef CredentialRefKind = "keychain_ref"
)

func (k CredentialRefKind) Valid() bool {
	switch k {
	case CredRefEnvVar, CredRefCLISession, CredRefKeychainRef:
		return true
	}
	return false
}

type CredentialRef struct {
	RefID    string            `json:"ref_id"`
	Kind     CredentialRefKind `json:"kind"`
	Provider string            `json:"provider"`
	Locator  string            `json:"locator"`
}

var envVarIdentifierRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
var cliSessionHandleRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-:]+$`)

func (c CredentialRef) Validate() error {
	const kind = "CredentialRef"
	if c.RefID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: ref_id is required", kind)
	}
	if !c.Kind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid kind %q", kind, string(c.Kind))
	}
	if c.Provider == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: provider is required", kind)
	}
	if c.Locator == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: locator is required", kind)
	}
	switch c.Kind {
	case CredRefEnvVar:
		if !envVarIdentifierRegex.MatchString(c.Locator) {
			return errs.New(errs.CategoryInvalidArgument, "%s: env_var locator must be valid uppercase env identifier, got %q", kind, c.Locator)
		}
	case CredRefCLISession:
		if !cliSessionHandleRegex.MatchString(c.Locator) {
			return errs.New(errs.CategoryInvalidArgument, "%s: cli_session locator must be alphanumeric handle, got %q", kind, c.Locator)
		}
	case CredRefKeychainRef:
		if len(c.Locator) > 256 {
			return errs.New(errs.CategoryInvalidArgument, "%s: keychain_ref locator too long", kind)
		}
	}
	return nil
}

// ActionStatus tracks individual action lifecycle.
type ActionStatus string

const (
	ActionStatusPlanned          ActionStatus = "planned"
	ActionStatusAwaitingApproval ActionStatus = "awaiting_approval"
	ActionStatusRunning          ActionStatus = "running"
	ActionStatusSucceeded        ActionStatus = "succeeded"
	ActionStatusFailed           ActionStatus = "failed"
	ActionStatusSkipped          ActionStatus = "skipped"
	ActionStatusManualRequired   ActionStatus = "manual_required"
	ActionStatusBlocked          ActionStatus = "blocked"
	ActionStatusInterrupted      ActionStatus = "interrupted"
)

func (s ActionStatus) Valid() bool {
	switch s {
	case ActionStatusPlanned, ActionStatusAwaitingApproval, ActionStatusRunning, ActionStatusSucceeded,
		ActionStatusFailed, ActionStatusSkipped, ActionStatusManualRequired, ActionStatusBlocked, ActionStatusInterrupted:
		return true
	}
	return false
}

// ExecutionStatus tracks whole execution lifecycle.
type ExecutionStatus string

const (
	ExecutionStatusPlanned     ExecutionStatus = "planned"
	ExecutionStatusRunning     ExecutionStatus = "running"
	ExecutionStatusSucceeded   ExecutionStatus = "succeeded"
	ExecutionStatusFailed      ExecutionStatus = "failed"
	ExecutionStatusBlocked     ExecutionStatus = "blocked"
	ExecutionStatusInterrupted ExecutionStatus = "interrupted"
)

func (s ExecutionStatus) Valid() bool {
	switch s {
	case ExecutionStatusPlanned, ExecutionStatusRunning, ExecutionStatusSucceeded, ExecutionStatusFailed,
		ExecutionStatusBlocked, ExecutionStatusInterrupted:
		return true
	}
	return false
}

type ActionResult struct {
	ActionID      string       `json:"action_id"`
	Status        ActionStatus `json:"status"`
	StartedAt     *Timestamp   `json:"started_at,omitempty"`
	FinishedAt    *Timestamp   `json:"finished_at,omitempty"`
	ExitCode      *int         `json:"exit_code,omitempty"`
	OutputSummary string       `json:"output_summary,omitempty"`
	ArtifactRef   *ArtifactRef `json:"artifact_ref,omitempty"`
	FailureDetail string       `json:"failure_detail,omitempty"`
}

func (r ActionResult) Validate() error {
	const kind = "ActionResult"
	if r.ActionID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: action_id is required", kind)
	}
	if !r.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid status %q", kind, string(r.Status))
	}
	if r.ArtifactRef != nil {
		if err := r.ArtifactRef.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type SetupExecutionReport struct {
	SchemaVersion      SchemaVersion   `json:"schema_version"`
	ExecutionID        string          `json:"execution_id"`
	PlanID             string          `json:"plan_id"`
	PlanDigest         string          `json:"plan_digest"`
	MachineFingerprint string          `json:"machine_fingerprint"`
	StartedAt          Timestamp       `json:"started_at"`
	CompletedAt        *Timestamp      `json:"completed_at,omitempty"`
	Status             ExecutionStatus `json:"status"`
	Results            []ActionResult  `json:"results"`
}

func (r *SetupExecutionReport) RecordKind() string       { return "SetupExecutionReport" }
func (r *SetupExecutionReport) RecordID() string         { return r.ExecutionID }
func (r *SetupExecutionReport) SchemaVer() SchemaVersion { return r.SchemaVersion }

func (r *SetupExecutionReport) Validate() error {
	const kind = "SetupExecutionReport"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "execution_id", r.ExecutionID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "plan_id", r.PlanID); err != nil {
		return err
	}
	if !hexSha256Regex.MatchString(r.PlanDigest) {
		return errs.New(errs.CategoryInvalidArgument, "%s: plan_digest must be sha256 hex, got %q", kind, r.PlanDigest)
	}
	if !hexSha256Regex.MatchString(r.MachineFingerprint) {
		return errs.New(errs.CategoryInvalidArgument, "%s: machine_fingerprint must be sha256 hex, got %q", kind, r.MachineFingerprint)
	}
	if r.StartedAt.Time().IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: started_at is required", kind)
	}
	if !r.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid status %q", kind, string(r.Status))
	}
	seen := make(map[string]bool, len(r.Results))
	for _, res := range r.Results {
		if err := res.Validate(); err != nil {
			return err
		}
		if seen[res.ActionID] {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate action_id %q in results", kind, res.ActionID)
		}
		seen[res.ActionID] = true
	}
	return nil
}

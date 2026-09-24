# Engineering Work Package: WP-M3B-4 — Credential-reference abstraction

- **Milestone:** M3B — Guided bootstrap and onboarding
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3b-4--credential-reference-abstraction](../WORK_PACKAGES.md#wp-m3b-4--credential-reference-abstraction)
- **Base commit:** `9b8c809615bc2f5c961f2cacf14e0de4110b0ad4` (`feat/m3b-guided-bootstrap`, post-merge of PR #12 roadmap rebaseline; includes accepted WP-M3B-1, WP-M3B-2, and WP-M3B-3 checkpoints)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 (accepted at `6833219`, amended §13), WP-M3B-2 (accepted at `bb01bc9`), WP-M3B-3 (accepted at `cc799cd`).
- **Status:** Implementation complete; independent review found 6 substantive blockers (presence-vs-authenticated conflation, unenforced secret guard, AuthEvidence validation gaps, Go/schema parity mismatches, an incoherent Record integration, and over-confident CLI failure semantics) plus bookkeeping issues — all fixed; see §13 below.

---

## 0. What already exists (read before implementing anything)

Following the disciplined pattern of WP-M3B-1 through WP-M3B-3, this WP's scope is evaluated against the current codebase before writing implementation code. The credential and authentication domain touches several layers:

### Existing code assessment

1. **`internal/protocol/setup.go` (lines 981–1037):**
   - An early draft of `CredentialRefKind` (`env_var`, `cli_session`, `keychain_ref`) and `CredentialRef` was introduced in WP-M3B-1.
   - However, that early draft contained a `Provider string` field on `CredentialRef`.
   - **Crucial architectural correction:** ADR-0014 §6, ADR-0018 §1, and the WP-M3B-4 scope card establish that `CredentialRef != CognitionEndpoint != AccessChannel != Session != Account != EconomicRegime != CognitionPortfolio`. A `CredentialRef` identifies *how authorization is referenced* (e.g., an env var name or keychain locator), not *who provides inference* or *which account owns it*. Encoding provider names into the core `CredentialRef` type couples the credential locator to a provider identity, violating provider neutrality (DCI-054, DCI-055). This WP removes `Provider` from the core `CredentialRef` struct and keeps locator validation strictly provider-neutral.
   - Existing validation: `envVarIdentifierRegex` (`^[A-Z_][A-Z0-9_]*$`), `cliSessionHandleRegex` (`^[a-zA-Z0-9_\-:]+$`), and a 256-character length limit on keychain locators. These must be preserved and strengthened.

2. **`internal/cognition/service.go` and `internal/cognition/remoteapi/remoteapi.go`:**
   - Both files have independent unexported copies of `looksLikeSecret(value string) bool`.
   - The function checks `len(value) > 128`, prefixes (`sk-`, `sk_`, `pat_`, `ghp_`, `github_pat_`, `bearer `, `aws_`), and substrings (`secret=`, `token=`).
   - `looksLikeSecret` is called during declaration validation and description conversion.
   - **Gap:** It is duplicated, package-private, and does not cover newer token prefixes (`xoxb-`, `xoxp-`, `glpat-`, `npm_`, `aiza`, `gho_`, `ghu_`, `ghs_`, `ghr_`). Furthermore, applying a naive `aws_` prefix check across lowercase strings would reject valid environment variable names like `AWS_ACCESS_KEY_ID` or `AWS_SECRET_ACCESS_KEY`. This WP consolidates and strengthens this logic into a canonical, reusable `protocol.LooksLikeSecret` function that distinguishes secret tokens/assignments from valid uppercase environment variable names.

3. **`internal/cognition/codingcli/codingcli.go`:**
   - Discovers installed coding CLIs (`codex`, `claude`, `gemini`) from `EnvironmentFacts.Software`.
   - Correctly marks endpoints as `health: installed`, `auth: unknown`, and `cost_class: unknown` upon discovery, declaring that presence on PATH says nothing about authentication or billing.
   - Probe execution currently runs an inference prompt (`SyntheticProbePrompt`) to test if the CLI answers, and checks if output mentions sign-in to downgrade auth to `unauthenticated`.
   - **Gap:** There is no dedicated, non-inference authenticated-session prober for CLI sessions in M3B. Probing auth via inference burns user quota and model resources. A bounded, non-inference/read-only auth probe contract is needed for CLI sessions.

4. **`internal/process/process.go`:**
   - `process.Spec` accepts `Executable`, `Args`, `Dir`, `Env`, `Timeout`, etc.
   - `process.Result` captures `Stdout`, `Stderr`, `Env`.
   - Comment in `process.go:89-91`: *"Callers are responsible for not putting secret values into Env in the first place (DCI-081); this package does not attempt to guess which entries are sensitive."*
   - **Gap:** No abstraction currently validates that a constructed `process.Spec` does not inadvertently contain raw secrets in `Args` or `Env`. This WP provides explicit validation preventing secrets from being passed to subprocesses.

5. **Schemas:**
   - `schemas/machine-capability-profile.schema.json` references `credential_ref` as an opaque string.
   - No dedicated JSON Schema currently exists for `CredentialRef` or `AuthEvidence`. This WP provides schemas and ensures Go/JSON Schema validation parity.

---

## 1. Objective and rationale

Build the **Credential-Reference Abstraction** (`internal/credentials` and `internal/protocol`) as a foundational, reusable security primitive for M3B (readiness and `ResourceInventory`), M3C (access channels and sessions), M3D (cognition portfolio synthesis), and ADR-0017 (external research evidence acquisition).

### Rationale

1. **No Secret Custody:** DevCadence is a control plane, not a secrets vault. It must never hold custody of raw API keys, passwords, or session tokens. Storing raw secrets in configuration, state files, ledgers, or logs is a critical vulnerability (ADR-0014 §6, docs/SECURITY.md §7, DCI-081).
2. **Orthogonality of Concerns:** Authorization reference is distinct from endpoint capability, economic regime, and model routing:
   ```text
   CredentialRef
   != CognitionEndpoint
   != AccessChannel
   != Session
   != Account
   != EconomicRegime
   != CognitionPortfolio
   ```
   A `CredentialRef` only specifies *how to locate an authorization source*.
3. **Presence-Only Environment Resolution:** For environment variables, DevCadence needs to know if authorization is available (`present: true`), without ever reading, storing, logging, or hashing the secret value.
4. **Trustworthy Auth Evidence:** An executable existing on PATH (`claude --version`) proves *installation only*, never *authentication*. Establishing an authenticated session requires bounded, trustworthy evidence without leaking tokens or consuming model inference quota.

---

## 2. Relevant invariants and ADRs

- **ADR-0014 §6 (Opaque Credential References and Secret Isolation):** Configuration references credentials via opaque locators. DevCadence holds no secret custody. Environment lookup is presence-only. CLI version output never establishes authentication. Secrets are strictly forbidden from process args and env.
- **ADR-0014 §7 (Output Artifacts & CommandRunner):** Output capture is bounded and stripped of ANSI. Authentication operations capture zero raw output artifacts.
- **ADR-0013 §1 & §3 (Facts vs Assessment vs Evidence, Probe Depth):** Shallow probes do not invoke inference. Probing one endpoint does not fan out.
- **ADR-0018 §1 & §6 (Adaptive Cognition, Planner Authority Boundary):** Credentials, access channels, accounts, and economic regimes are orthogonal. Portfolio planners receive only non-secret references and cannot create credentials.
- **docs/SECURITY.md §5 & §7:** Principle of least authority; credentials represented by opaque references; credentials redacted from logs and never committed.
- **CONTRIBUTING.md §Security-sensitive contributions:** Threat-model review required against `docs/SECURITY.md`.
- **Invariants:**
  - `DCI-033`: Deterministic process execution, no arbitrary shell.
  - `DCI-054`: Cognition roles separate from providers.
  - `DCI-055`: Provider-neutral interfaces; no vendor favoritism in core types.
  - `DCI-081`: Credentials and accounts opaque; secret-shaped values refused at boundary.
  - `DCI-083`: Untrusted inputs treated as data, never authority; output sanitized.
  - `DCI-104`: Missing tools/credentials degrade gracefully rather than aborting unrelated capabilities.
  - `DCI-106`: Empirical verification required; no fabricated readiness claims.
  - `DCI-108`: Operator consent required; immutable plans.

---

## 3. Threat model & security review

Per `CONTRIBUTING.md` and `docs/SECURITY.md`, the following 12 threat vectors are systematically evaluated and mitigated:

| Threat Vector | Description | Mitigation Strategy |
|---|---|---|
| **1. Malicious config with raw API keys** | Operator or attacker places a raw API key (e.g., `sk-ant-...`) in configuration where a locator is expected. | `CredentialRef.Validate()` runs `LooksLikeSecret()` on `Locator` and `RefID`. Any secret-looking string fails closed with `errs.CategoryInvalidArgument`. |
| **2. Token leakage via structured logging / `%v`** | Logging a `CredentialRef` or `AuthEvidence` struct prints secrets. | Neither struct contains a secret field. `Locator` for `env_var` is the variable name, not value. `AuthEvidence` contains only status, timestamp, and sanitized detail. |
| **3. Provider CLI echoing credentials** | A CLI auth status command outputs `Logged in with token: sk-...` or echoes headers. | All CLI probe stdout/stderr is treated as untrusted and potentially secret-bearing. Raw output is NEVER retained, NEVER stored as an artifact, and NEVER copied into `AuthEvidence.Detail` or error strings. |
| **4. Hostile process output** | A malicious or hijacked binary outputs gigabytes of garbage or simulated tokens. | Subprocess execution uses `process.Runner` with strict timeout and output bounds (`MaxStdoutBytes`). Output is discarded immediately after parsing against fixed regexes. |
| **5. Environment-variable leakage** | Inspecting environment variables exposes values, lengths, prefixes, or hashes. | `EnvReader` uses presence-only resolution (`os.LookupEnv` checking non-empty). The value is never assigned to a struct, never hashed, never measured for length or entropy, and discarded immediately. |
| **6. Secret-looking locator values** | Locators containing embedded secrets (e.g., `token=xyz` or long tokens). | Bounded locator lengths (128 for env/cli, 256 for keychain) and strict character whitelists. `LooksLikeSecret()` rejects suspicious prefixes/patterns. |
| **7. Reference confusion (path / account / session)** | Conflating an account ID, an access channel, or a file path with a credential locator. | Strict kind discrimination (`CredentialRefKind` enum). Distinct validation rules per kind (`env_var`, `cli_session`, `keychain_ref`). |
| **8. Arbitrary CLI execution via auth probe** | An attacker supplies a crafted CLI locator (e.g. `rm -rf /` or `curl \| sh`) that gets executed as a probe. | Probers map locators to hardcoded, typed command definitions or registered adapters. No arbitrary command lines are accepted from user input or configuration. |
| **9. TOCTOU between auth probe and session use** | Auth probe reports authenticated, but session expires or is revoked before execution. | `AuthEvidence` records `ObservedAt` timestamp. It is evidence of past observation, not an eternal guarantee. Downstream consumers verify freshness. |
| **10. Provider / account confusion** | Assuming an Anthropic credential works for OpenAI or treating different accounts as identical. | `CredentialRef` is solely a locator. Downstream consumers (`CognitionEndpoint`, `AccessChannel`) bind locators to provider/account contexts. |
| **11. Keychain locator traversal / injection** | Malicious keychain locator attempting directory traversal (e.g. `../../etc/shadow`) or null-byte injection. | Keychain locators must match `^[a-zA-Z0-9_\-\./:]+$` with explicit rejection of `..` segments and null bytes. Length capped at 256 bytes. |
| **12. Error wrapping with raw values** | Error returns formatting the secret value (e.g. `fmt.Errorf("failed auth with %s", secret)`). | Secret values are never in scope in Go variables. Errors only reference non-secret `RefID`, `Locator`, or exit codes. |

---

## 4. Trust boundaries & secret-flow analysis

```mermaid
flowchart TD
    subgraph External["External System Boundaries"]
        OS_ENV["OS Environment Variables"]
        OS_KEYCHAIN["OS Credential Manager / Keychain"]
        CLI_DAEMON["Vendor CLI External Auth Session"]
    end

    subgraph Boundary["DevCadence Isolation Boundary"]
        EnvReader["Presence-Only EnvReader<br/>(returns bool only)"]
        KeychainChecker["Keychain Locator Checker<br/>(checks presence, no secret custody)"]
        CLIProber["CLI Auth Prober<br/>(executes fixed argv, parses status)"]
    end

    subgraph Durable["Safe Durable Records (No Secrets)"]
        CredRef["protocol.CredentialRef<br/>{RefID, Kind, Locator}"]
        AuthEv["protocol.AuthEvidence<br/>{RefID, Kind, Status, ObservedAt}"]
        SpecValidator["Process Spec Guard<br/>(verifies NO secrets in Args/Env)"]
    end

    OS_ENV -->|"os.LookupEnv (presence only)"| EnvReader
    OS_KEYCHAIN -->|"presence query"| KeychainChecker
    CLI_DAEMON -->|"untrusted output (discarded)"| CLIProber

    EnvReader -->|"present: bool"| AuthEv
    KeychainChecker -->|"present: bool"| AuthEv
    CLIProber -->|"status enum"| AuthEv

    CredRef --> SpecValidator
    AuthEv --> SpecValidator
```

### Strict Secret-Flow Rules

1. **Environment Variables:**
   - Lookup method: `os.LookupEnv(name)` (injected via `EnvReader`).
   - Recorded information: Boolean `present = ok && val != ""`.
   - Forbidden information: `val`, `len(val)`, `val[:4]`, `sha256(val)`, `entropy(val)`.
   - Sentinel tests verify that sentinel secret values never appear in strings, logs, errors, or memory dumps.

2. **CLI Sessions:**
   - Vendor CLIs manage their own credentials in user-space sessions (e.g., `~/.claude`, macOS keychain).
   - DevCadence probes CLI authentication using bounded, non-inference, non-mutating commands where supported.
   - Stdout/stderr from CLI probes is treated as untrusted and potentially secret-bearing: parsed strictly for known status tokens, then immediately discarded.
   - Zero raw output artifacts are captured or filed for authentication operations.

3. **Keychain References:**
   - DevCadence holds opaque service/account references (`keychain_ref`).
   - Presence check queries the platform credential store (behind a Go interface).
   - DevCadence does not persist or take custody of the decrypted secret material during bootstrap/readiness.

4. **Process Boundary:**
   - `process.Spec.Args` and `process.Spec.Env` must NEVER contain raw secrets.
   - CLI tools execute using their existing external authenticated sessions.
   - A process spec validator enforces that no arg or env string satisfies `LooksLikeSecret()`.

---

## 5. Proposed interfaces and types

### Protocol Types (`internal/protocol/credentials.go`)

```go
package protocol

// CredentialRefKind specifies the storage/resolution mechanism for an opaque credential reference.
type CredentialRefKind string

const (
	CredRefEnvVar      CredentialRefKind = "env_var"
	CredRefCLISession  CredentialRefKind = "cli_session"
	CredRefKeychainRef CredentialRefKind = "keychain_ref"
)

func (k CredentialRefKind) Valid() bool

// CredentialRef is an opaque reference to an authorization mechanism.
// It is provider-neutral and contains NO secret material.
type CredentialRef struct {
	RefID   string            `json:"ref_id"`
	Kind    CredentialRefKind `json:"kind"`
	Locator string            `json:"locator"`
}

func (c CredentialRef) Validate() error

// AuthEvidenceStatus represents the authoritative outcome of an authentication probe.
type AuthEvidenceStatus string

const (
	AuthStatusAuthenticated   AuthEvidenceStatus = "authenticated"
	AuthStatusUnauthenticated AuthEvidenceStatus = "unauthenticated"
	AuthStatusUnavailable     AuthEvidenceStatus = "unavailable"
	AuthStatusIndeterminate   AuthEvidenceStatus = "indeterminate"
)

func (s AuthEvidenceStatus) Valid() bool

// AuthProbeKind identifies the mechanism used to obtain authentication evidence.
type AuthProbeKind string

const (
	AuthProbeEnvPresence      AuthProbeKind = "env_presence"
	AuthProbeCLIAuthCall      AuthProbeKind = "cli_auth_call"
	AuthProbeCLIVersionOnly   AuthProbeKind = "cli_version_only"
	AuthProbeKeychainPresence AuthProbeKind = "keychain_presence"
)

func (k AuthProbeKind) Valid() bool

// AuthEvidence is a structured, bounded durable record of authentication readiness.
// It NEVER contains raw output, tokens, or secret-bearing data.
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

func (e AuthEvidence) Validate() error

// LooksLikeSecret performs conservative checks to detect raw secret material.
func LooksLikeSecret(value string) bool
```

### Domain Interfaces (`internal/credentials/`)

```go
package credentials

import (
	"context"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// EnvReader abstracts environment variable lookup for presence-only inspection.
type EnvReader interface {
	IsPresent(name string) bool
}

// CLISessionAuthAdapter probes authentication for a specific CLI tool.
type CLISessionAuthAdapter interface {
	AdapterID() string
	Handles(locator string) bool
	ProbeAuth(ctx context.Context, runner process.Runner, locator string) protocol.AuthEvidence
}

// KeychainChecker abstracts platform keychain presence checks.
type KeychainChecker interface {
	CheckPresence(ctx context.Context, locator string) (bool, error)
}

// Manager coordinates credential reference resolution and authentication evidence generation.
type Manager struct { ... }

func NewManager(opts Options) *Manager
func (m *Manager) CheckCredential(ctx context.Context, ref protocol.CredentialRef) (protocol.AuthEvidence, error)
func (m *Manager) ValidateProcessSpec(spec process.Spec) error
```

---

## 6. Authentication evidence semantics

### 1. Distinct Readiness States

1. **`authenticated`**: Authoritative proof that an active, authorized session or credential exists (e.g., non-empty environment variable confirmed present, or CLI auth probe succeeded with zero exit code and positive auth signal).
2. **`unauthenticated`**: Authoritative proof that authorization is absent or rejected (e.g., environment variable is unset/empty, or CLI explicitly returned "not logged in" or exit code 1 on auth check).
3. **`unavailable`**: The mechanism cannot be evaluated because underlying tooling is absent (e.g., CLI binary not found on PATH or in canonical location, or keychain service unreachable).
4. **`indeterminate`**: The tool exists and was executed, but cannot definitively prove authentication without running inference (or returned ambiguous output/timeout). **Crucial rule:** If a CLI does not support a non-inference auth check, DevCadence reports `indeterminate`, NOT `authenticated`.

### 2. The Version-Output Rule

ADR-0014 §6 dictates:
```text
claude --version
codex --version
gemini --version
```
**MUST NOT** establish an authenticated session.
A version probe produces `AuthProbeCLIVersionOnly`. If the binary runs and returns a version string, it proves `command_available` and `installed` only. In `AuthEvidence`, `ProbeKind: cli_version_only` results in `status: indeterminate` (or `unavailable` if missing), **never** `status: authenticated`.

---

## 7. Implementation strategy

1. **Protocol Layer (`internal/protocol/credentials.go`):**
   - Define `CredentialRefKind`, `CredentialRef`, `AuthEvidenceStatus`, `AuthProbeKind`, `AuthEvidence`.
   - Update `CredentialRef` to remove `Provider`.
   - Implement `protocol.LooksLikeSecret(val string) bool` with comprehensive prefix and shape checks.
   - Implement `Validate()` for both structs with strict regular expressions and bounds.
   - Refactor `internal/cognition/service.go` and `internal/cognition/remoteapi/remoteapi.go` to use `protocol.LooksLikeSecret()`.

2. **JSON Schemas (`schemas/credential-ref.schema.json`, `schemas/auth-evidence.schema.json`):**
   - Create Draft 2020-12 schemas for both types.
   - Register in `internal/schema/schema.go`.
   - Verify bidirectional Go/schema parity in tests.

3. **Credentials Service (`internal/credentials/`):**
   - `env.go`: `EnvReader` interface, `OsEnvReader` implementation (presence-only, zero value retention).
   - `cli.go`: `CLISessionAuthAdapter` registry, `DefaultCLIAuthAdapters()`, version probe vs auth probe semantics, strict stdout/stderr scrubbing.
   - `keychain.go`: `KeychainChecker` interface with cross-platform stub/adapter (no CGO, compiles cleanly on Windows/Linux/Darwin).
   - `process_guard.go`: `ValidateProcessSpecNoSecrets(spec process.Spec) error`.
   - `manager.go`: `Manager` orchestrating resolution into `protocol.AuthEvidence`.

4. **Cross-Platform Compatibility:**
   - No platform-specific syscalls in core files.
   - Safe stubbing for keychain operations on non-supported platforms.
   - All packages compile under `GOOS=windows GOARCH=amd64`.

---

## 8. Acceptance criteria

1. **Closed Provider-Neutral Model:** `CredentialRef` supports `env_var`, `cli_session`, and `keychain_ref` without hardcoding provider names in the core type.
2. **Secret Rejection at Boundary:** Any locator or ref ID containing secret patterns or excessive length is rejected by `Validate()` with `errs.CategoryInvalidArgument`.
3. **Presence-Only Env Lookup:** Looking up an environment variable reports existence without exposing, retaining, hashing, or serializing the value or its length.
4. **CLI Version Command Isolation:** Running `--version` on a CLI establishes installation evidence only; it is structurally and programmatically rejected as evidence of authentication.
5. **Clean Process Boundary:** No secret material can be passed into `process.Spec.Args` or `process.Spec.Env`.
6. **Hostile Output Immunity:** CLI output containing simulated API keys or tokens is never captured into `AuthEvidence`, error messages, artifacts, or logs.
7. **Cross-Platform Verification:** Code compiles and tests pass across macOS, Linux, and Windows targets.

---

## 9. Verification & test suite

The test suite in `internal/protocol/credentials_test.go` and `internal/credentials/credentials_test.go` covers all 16 required scenarios:

1. **Valid CredentialRef round trips:** JSON marshaling/unmarshaling and validation for `env_var`, `cli_session`, and `keychain_ref`.
2. **Unknown credential kind rejected:** Rejection of arbitrary kind strings.
3. **Malformed/empty references rejected:** Rejection of empty `ref_id`, empty `locator`, lowercase/special-character env names, and invalid handles.
4. **Obvious raw secret values rejected:** Locators containing `sk-`, `ghp_`, `bearer `, `token=`, `secret=`, or length > 128 bytes are refused.
5. **Env-var presence-only lookup:** Verifies `IsPresent` returns true for set variables without returning the value, length, or hash.
6. **Sentinel secret leakage prevention:** A sentinel secret (`sk-ant-sentinel-vault-token-xyz-12345`) placed in an environment variable never appears in any struct, string format (`%v`, `%+v`), log, error, or JSON output.
7. **CLI version output cannot authenticate:** Probing a CLI that only returns version output yields `status: indeterminate`, never `authenticated`.
8. **Authenticated-session probe success:** A CLI probe returning a valid auth status produces `status: authenticated`.
9. **Authenticated-session probe unauthenticated failure:** A CLI probe indicating not logged in produces `status: unauthenticated`.
10. **Authenticated-session probe unavailable/indeterminate behavior:** Missing executable produces `status: unavailable`; timeout or unparseable output produces `status: indeterminate`.
11. **Hostile auth-probe output isolation:** A simulated CLI returning `Token: sk-live-hostile-secret-key-do-not-leak` does NOT leak that token into `AuthEvidence`, errors, or artifacts.
12. **Process spec secret injection prevention:** `ValidateProcessSpecNoSecrets` flags and rejects any spec whose `Args` or `Env` contains secret values.
13. **Serialization leak checks:** Serialized ledger events, doctor reports, and configs created alongside credential operations contain zero sentinel substrings.
14. **Go/Schema parity:** Valid and invalid `CredentialRef` and `AuthEvidence` fixtures verified against Draft 2020-12 schemas.
15. **Provider-neutral adapter contract:** Adding a custom `CLISessionAuthAdapter` without touching protocol types.
16. **Cross-Platform compilation:** Verified with `GOOS=windows GOARCH=amd64 go build ./...`.

---

## 10. Non-goals

The following are strictly out of scope for WP-M3B-4:
- M3C `AccessChannel` implementation;
- `EconomicRegime` and `BudgetPool` quota management;
- `CognitionPortfolio` synthesis and portfolio activation;
- AI Portfolio Planner;
- Workflow topology and multi-step orchestration;
- Role-to-provider routing and capability scoring;
- Rich setup interactive TUI;
- Actual secret retrieval/decryption from macOS Keychain (presence checking only).

---

## 11. Escalation conditions

Stop and escalate if:
1. A downstream requirement demands passing raw credentials through DevCadence process arguments or environment variables.
2. A provider CLI cannot be probed without triggering paid model inference or mutating local user state.
3. An existing protocol schema requires breaking changes that invalidate accepted M1/M2/M3A fixtures without an approved ADR.

---

## 12. Implementation and deterministic verification summary

### Implemented deliverables

1. **Protocol Types (`internal/protocol/credentials.go`):**
   - `CredentialRefKind`: `env_var`, `cli_session`, `keychain_ref`.
   - `CredentialRef`: Provider-neutral struct `{RefID, Kind, Locator}`. `Provider` was decoupled from the core type in accordance with the invariant `CredentialRef != CognitionEndpoint != AccessChannel != Account`.
   - `AuthEvidenceStatus`: `authenticated`, `unauthenticated`, `unavailable`, `indeterminate`.
   - `AuthProbeKind`: `env_presence`, `cli_auth_call`, `cli_version_only`, `keychain_presence`.
   - `AuthEvidence`: Structured, bounded durable record of authentication readiness `{RefID, Kind, Status, ProbeKind, ObservedAt, ProbeTarget, AdapterID, Detail}`.
   - `LooksLikeSecret`: Canonical, hardened secret detector preventing token leakage into locators, IDs, details, and environment/args.
   - Rule enforcement: `AuthProbeCLIVersionOnly` with `AuthStatusAuthenticated` is structurally rejected by `Validate()`.

2. **JSON Schemas (`schemas/credential-ref.schema.json`, `schemas/auth-evidence.schema.json`):**
   - Published Draft 2020-12 schemas for both types.
   - Registered in `internal/schema/schema.go` as `NameCredentialRef` and `NameAuthEvidence` with `RecordKindToSchema` mappings.
   - Verified schema compilation and Go/schema bidirectional parity in `internal/protocol/credentials_test.go`.

3. **Domain Implementation (`internal/credentials/`):**
   - `env.go`: `EnvReader` interface, `OsEnvReader` (presence-only, zero value retention), and `MapEnvReader` for testing.
   - `cli.go`: `CLISessionAuthAdapter` interface, `VersionOnlyAdapter` (version proves installation only, never authentication), `BoundedCLIAuthAdapter` (bounded non-inference auth calls, zero raw stdout/stderr retained in durable records), and `StaticCLIAuthAdapter`.
   - `keychain.go`: `KeychainChecker` interface, `MapKeychainChecker`, and cross-platform safe fallback `UnsupportedKeychainChecker` (no CGO, zero platform-specific syscalls).
   - `process_guard.go`: `ValidateProcessSpecNoSecrets(process.Spec)` preventing secrets from being passed to subprocess `Args` or `Env`.
   - `manager.go`: `Manager` coordinating safe presence/auth checks into schema-valid `AuthEvidence`.

4. **Integration with Existing Codebase:**
   - Replaced duplicate unexported `looksLikeSecret` in `internal/cognition/service.go` and `internal/cognition/remoteapi/remoteapi.go` with delegation to `protocol.LooksLikeSecret`.
   - Updated `docs/PROTOCOLS.md` (§20) and `docs/ARCHITECTURE.md` (§6.7D) with component boundaries and invariants.

### Deterministic verification results

All commands executed with zero errors and zero diagnostics:

| Command | Result |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -count=1 ./...` | PASS (all 29 tested packages) |
| `go test -race ./internal/credentials/... ./internal/protocol/... ./internal/setup/...` | PASS (no data races) |
| `GOOS=windows GOARCH=amd64 go build ./...` | PASS (clean cross-compilation) |
| `GOOS=linux GOARCH=amd64 go build ./...` | PASS (clean cross-compilation) |
| `git diff --check` | PASS |

## 13. Independent review disposition: 6 findings, all fixed

An independent review ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5811641514), owner, 2026-09-24) found the overall direction good — EWP-before-code, provider-neutral `CredentialRef` with no M3C/M3D absorption, `cli_version_only` correctly barred from `authenticated`, centralized secret-shape logic, no raw probe output in durable evidence, meaningful tests — but 6 substantive blockers, plus bookkeeping issues, all fixed in this revision:

1. **Presence-only evidence was promoted to `authenticated`.** `Manager.CheckCredential` mapped non-empty `env_var`/keychain-item presence directly to `AuthStatusAuthenticated`, overstating what was actually proven — presence proves nothing about validity, provider acceptance, expiry, or session usability. Fixed: presence now maps to `AuthStatusIndeterminate` (absence still maps to `AuthStatusUnauthenticated` — a real, if weak, negative signal, unlike promoting mere presence to a positive one). `AuthEvidence.Validate()` now structurally forbids `AuthStatusAuthenticated` for `env_presence`/`keychain_presence` probe kinds (the same rule already applied to `cli_version_only`), so this can never regress silently through a different code path. Related bug also fixed: `UnsupportedKeychainChecker.CheckPresence` returned `(false, nil)`, which `Manager` read as "item absent" (`unauthenticated`) rather than "check not completed" (`unavailable`) — it now returns an error, routing through `Manager`'s existing error branch to `AuthStatusUnavailable`. New tests: `TestUnsupportedKeychainBackendIsUnavailableNotAbsent`; `TestSentinelSecretNeverLeaksFromEnvResolution`/`TestKeychainPresenceResolution` updated to assert `indeterminate` for presence.
2. **The process-spec secret guard was an unenforced opt-in helper.** `credentials.ValidateProcessSpecNoSecrets` existed but neither CLI adapter called it before `runner.Run`. Fixed: both `VersionOnlyAdapter.ProbeAuth` and `BoundedCLIAuthAdapter.ProbeAuth` now route through a shared `runGuarded` helper that calls the guard before the runner ever sees the spec — a real execution-time chokepoint for every WP4 CLI probe, not a helper a caller could forget. New test: `TestCLIAdaptersEnforceProcessSpecSecretGuard` (both adapter types, a secret-looking arg, asserts the fake runner was never actually invoked with it).
3. **`AuthEvidence.Validate()` had secret/structural validation gaps.** `RefID` was only checked non-empty (no length/pattern/secret check `CredentialRef.RefID` already had); `AdapterID` had only a max-length check, no secret-shape check; there was no structural binding between `Kind` and `ProbeKind`. Fixed: a shared `validateOpaqueID` helper now backs `CredentialRef.RefID`, `AuthEvidence.RefID`, and `AuthEvidence.AdapterID` uniformly; `AuthEvidence.Validate()` now enforces the valid `Kind`↔`ProbeKind` matrix (`env_var`→`env_presence`, `keychain_ref`→`keychain_presence`, `cli_session`→`cli_auth_call`|`cli_version_only`). New tests: `TestAuthEvidenceKindProbeKindBinding`, `TestAuthEvidenceRefIDAndAdapterIDShareTheOpaqueIDContract`.
4. **JSON Schema / Go validation parity was false.** Concrete mismatches: schema accepted keychain `..` traversal Go rejected; schema didn't encode `LooksLikeSecret`'s prefix/keyword checks at all; keychain's schema max (256) disagreed with Go's effective max (128, since `LooksLikeSecret` rejects any value over 128 unconditionally); `AuthEvidence.ref_id` had no schema pattern/length match to Go's (now-fixed) check. Fixed: unified the keychain locator regex (`^[a-zA-Z0-9_\-/:]+(\.[a-zA-Z0-9_\-/:]+)*$`, which structurally cannot match `..`) between Go and schema; lowered Go's keychain max to 128 to match the schema (and reality); added a `$defs/noSecretLike` JSON Schema fragment mirroring every `LooksLikeSecret` prefix/keyword check as `not`/`anyOf`/`pattern` clauses, applied via `allOf`+`$ref` to every secret-checkable field in both schemas (`ref_id`, `locator`, `probe_target`, `adapter_id`, `detail`); added the `Kind`↔`ProbeKind` matrix to `auth-evidence.schema.json`'s `allOf`. New test: `TestSchemaSecretPatternParity` — table-driven, each case run through both Go `Validate()` and `schemas.ValidateBytes`, asserting they agree on accept/reject (keychain traversal, secret-shaped ref_id/locator/adapter_id/probe_target/detail, kind/probe_kind mismatches, presence-claims-authenticated).
5. **The "durable Record" integration was incoherent.** `CredentialRef`/`AuthEvidence` were registered in `schema.RecordKindToSchema` and had `SchemaVer()`/`RecordKind()`/`RecordID()` methods, but `SchemaVer()` returned plain `string` (not `protocol.SchemaVersion`, so neither type actually satisfied `protocol.Record`), neither JSON shape had a `schema_version` field, and `protocol.NewRecord` had no cases for either — a half-record state generic persistence/decoding could not handle. Fixed by completing the Record contract rather than downgrading to value objects (the EWP explicitly calls `AuthEvidence` a durable record, §6/§7): both types gained a `SchemaVersion SchemaVersion` field (validated in `Validate()`), `SchemaVer()` now returns `protocol.SchemaVersion`, `protocol.NewRecord` gained both cases, both JSON schemas gained a required `schema_version` const `"1.0"` property, and new fixtures (`fixtures/protocol/credential-ref.valid.json`, `auth-evidence.valid.json`) are wired into `tests/schema_fixtures_test.go`'s generic round-trip suite (`decodeInto[protocol.CredentialRef]`/`decodeInto[protocol.AuthEvidence]`) alongside every other durable record. Separately found and fixed while wiring this: `internal/schema.AllNames()` was missing both `NameCredentialRef`/`NameAuthEvidence` entirely, so `TestEverySchemaCompiles` and fixture auto-discovery had never actually exercised either schema since they were added — a real latent gap, not just a naming inconsistency.
6. **Generic CLI failure semantics were over-confident.** `BoundedCLIAuthAdapter` mapped *every* unrecognized nonzero exit code to `AuthStatusUnauthenticated`, but a nonzero exit can just as easily mean executable failure, an incompatible CLI version, local misconfiguration, a provider outage, or a permission failure — none of which is evidence of being unauthenticated. Fixed: only a nonzero exit whose output matches a provider-specific `UnauthenticatedMsgs` entry (unchanged, already correct) reports `unauthenticated`; an unrecognized nonzero exit now reports `indeterminate`. New test: `TestBoundedCLIAuthAdapterUnrecognizedFailureIsIndeterminate`.

**Bookkeeping, also fixed:** the EWP status-line Markdown typo (`-**Status:**`); `HANDOFF.md`'s "Expected remote HEAD" pointed at a SHA that does not resolve to an actual commit — corrected to the real checkpoint parent as part of this same repair revision, not a dedicated SHA-only commit.

**Verification (this revision):** `go build ./...`, `go vet ./...`, `gofmt -l internal/protocol/*.go internal/credentials/*.go`, `go test -count=1 ./...` (all packages, including the new fixture round-trip and parity tests), `go test -race ./internal/credentials/... ./internal/protocol/...`, `GOOS=windows GOARCH=amd64 go build ./...` — all clean.

**Disposition:** all 6 findings plus bookkeeping fixed; awaiting a follow-up review round before WP-M3B-4 can be marked `accepted`.


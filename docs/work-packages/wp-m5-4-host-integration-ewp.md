# WP-M5-4 — Principal Host Integration and Verified Source Boundary

## Identity

- Work Package ID: WP-M5-4 (M5 host integration slice)
- Revision: 2 (consolidated runtime binding, exclusive mutation and bounded rollback repairs)
- Task ID: task-m5-4-host-integration
- Base commit: `bd6c424e292815460033b2570dce4d682ef5cb73`
- Project state revision: not supplied; capture canonical revision before execution.
- Contract digest: not frozen; Markdown is the authoritative draft contract.
- Target implementation endpoint/profile: bounded Go implementer with host-adapter and security-test competence.
- Status: DRAFT — both independent lenses approved the preparation contract; NOT_READY for implementation.
- Dependencies: WP-M5-1 protocol/authority/CAS; WP-M5-2 reduced Principal facade/MCP adapter; follow-on accepted runtime window; WP-M5-3 discovery semantics.
- Window decisions remain proposed and unfrozen until independent review and Principal adjudication.

## Objective

Deliver a bounded host integration service consuming M3 observed host detection,
with native Antigravity reference and Cursor portability recipes, VS Code guidance,
approved configuration planning and empirical verification. Preserve a shared
semantic Principal facade in `internal/principal`; `internal/mcpadapter` owns MCP
transport under WP-M5-2. Readiness must distinguish connectivity, instruction
availability, and enforced source isolation. A separate principal workspace alone
must never establish a strict information firewall.

## Context Manifest

- role: host-adapter implementer; independent authority and test reviewers.
- read-authority envelope: this complete contract; `AGENTS.md`; exact clauses below;
  `docs/PRINCIPAL_HOSTS.md` §§2–8; `docs/ANTIGRAVITY_INTEGRATION.md` §§4–5,9–11,17–18;
  `docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md` §11 and M5 subsection;
  M3 `internal/environment`, `internal/principalhosts`, `internal/setup` interfaces;
  WP-M5-1 through WP-M5-3 interface contracts, progressively resolved.
- semantic write/scope envelope: host adapter/service, integration assets, scoped CLI
  setup exposure, host tests and owning host guidance as specified below.
- risk tags: configuration mutation, host drift, process launch, filesystem authority,
  external-tool trust, prompt injection, acceptance authority.
- exact normative clauses: DCI-080 least privilege; DCI-081 secret redaction;
  DCI-082 permission-enforced source firewall; DCI-083 untrusted tool outputs;
  DCI-104 optional-host degradation; DCI-105 observed discovery; DCI-107 replaceable
  hosts; DCI-108 visible approved setup mutation; AGENTS §§6–9,14–15.
- initial evidence handles: F-01–F-04 and upstream U-01–U-10 below.
- deferred references: concrete M3 setup action schema and WP-M5-2 construction
  signatures; resolve only to bind this adapter, without redesigning their semantics.
- assumptions: no authenticated GUI host is available in authoring environment;
  detection is evidence, not proof of current connection or isolation.
- re-resolution triggers: dependency interface changes; new host/OS behavior;
  unsupported config keys; unavailable isolation probe path; required setup schema change.

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/principalhosts/**`: consume and extend M3 detection with the service below.
- `integrations/antigravity/**`, `integrations/cursor/**`: native scoped recipes/assets.
- `integrations/vscode/**`: intended guidance/sample configuration only in this slice.
- `cmd/devcadence/**`: thin existing setup command exposure and associated tests only.
- `docs/PRINCIPAL_HOSTS.md`, `docs/ANTIGRAVITY_INTEGRATION.md`: minimal contract sync.
- `docs/work-packages/wp-m5-4-host-integration-ewp.md`: reviewed contract/evidence.
- Existing boundary tests may add assertions covering this declared dependency edge.

### Explicitly forbidden semantic changes

- No host-specific keys in core project, task, Work Package or decision schemas.
- No duplicate semantic operation handlers, CAS logic, accepted-authority ledger or
  Discovery state machine; WP-M5-1/WP-M5-2/WP-M5-3 own those contracts respectively.
- No automatic host installation, login, credential scraping, system settings or
  privileged security-policy modification; return visible operator guidance.
- No writes to target source or accepted project state through the host adapter.
- No claiming rules/skills, tool approval or source-free workspace enforce isolation.
- No expansion of server transport capabilities beyond WP-M5-2.

### LOCAL_DISCRETION

- Private helpers and test fixture organization in authorized domains.
- Native JSON formatting preserving unrelated configuration semantically.
- Integration asset filenames where required discovery paths/extensions stay exact.

## Requirements

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | Consume M3 observed host facts; detect read-only; report absent, incompatible, ambiguous and unknown states separately. No host is mandatory. | DCI-104,105,107 |
| REQ-02 | MUST | Provide the bounded PrincipalHost interface below, with host-specific configuration confined to adapters. | PRINCIPAL_HOSTS §§2,6 |
| REQ-03 | MUST | Produce an immutable digest-bound plan, explicit paths/actions, preimage hashes and operator-visible effects before any mutation. | DCI-108 |
| REQ-04 | MUST | Apply only exact approved bytes to DevCadence-owned paths/entries within a proven exclusive operator-owned mutation channel with per-write no-follow/preimage checks; preserve unrelated entries and offer reviewed manual application when M3 setup cannot represent keys. | DCI-080,108 |
| REQ-05 | MUST | Render Antigravity and Cursor native no-argument stdio recipes plus appropriate persistent principal instructions. | ANTIGRAVITY_INTEGRATION §§4–5,9; U-01–U-05 |
| REQ-06 | MUST | Reuse WP-M5-2 MCP connection and shared `internal/principal` operations; basic smoke invokes configured project state without requiring target source; it does not prove production task/review execution. | PRINCIPAL_HOSTS §8 |
| REQ-07 | MUST | Strict readiness requires fresh complete host/version/OS-bound negative access evidence for native files, terminal, escape routes and other tools. Unknown isolation blocks strict-ready. | DCI-082 |
| REQ-08 | MUST | Assisted readiness is separately labeled and may succeed with source access/unknown isolation; never relabel it strict or hide degraded evidence. | DCI-104; ANTIGRAVITY_INTEGRATION §11 |
| REQ-09 | MUST | Host tool approval authorizes host invocation only; canonical accepted authority, policy and CAS remain server-side and independent of host rules. | DCI-080; WP-M5-1 |
| REQ-10 | MUST | Detect configuration, executable, host-version and permission drift before reusing verification; redact secrets and never expose arbitrary source in probe output. | DCI-081; ANTIGRAVITY_INTEGRATION §18 |
| REQ-11 | MUST | Provide accurate VS Code guidance and scope all readiness claims to tested host/version/OS/session; do not infer first-class empirical readiness from samples. | DCI-107; U-06–U-08 |
| REQ-12 | MUST | Capture honest deterministic and human smoke evidence; unavailable interactive checks remain unavailable and prohibit a complete integration-ready claim. | AGENTS §8 |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | Detection, planning and verification do not mutate host configuration or accepted state. | REQ-01,03,06 |
| INV-02 | Unapproved plans, initially stale preimages and unavailable writer exclusion produce zero automated mutations; drift after an earlier write stops further writes and yields explicit partial recovery. | REQ-03,04 |
| INV-03 | Files outside owned artifacts/entries never change; partial failure is visible; only bounded in-process rollback with captured bytes/metadata may restore exact afterimages. Crash recovery never infers restoration. | REQ-04,10 |
| INV-04 | `strict_ready` implies complete fresh connection, instructions and denied source-access evidence for every route in the verified session. | REQ-06,07,10,12 |
| INV-05 | Host approval, prompts and SDK transport never bypass accepted domain authority or CAS. | REQ-09 |
| INV-06 | Absent or unverified host integration cannot disable independent deterministic/worker capabilities. | REQ-01,08,11 |

Pre-state is observed host/config evidence. Planning creates no accepted state.
Successful approved application changes only owned config/assets and produces an
application receipt; it is not a ready transition. Verification yields a fresh
report. Restart retains receipts as evidence but recomputes readiness; a receipt,
previous report, connected icon or file existence cannot alone restore ready.

## Verified facts about the current code

These are bounded repository-identity facts, not uninspected API claims.

| ID | Claim | Evidence (file:line / command / artifact) | Verified by |
| --- | --- | --- | --- |
| F-01 | M3 host detection package exists at the supplied base. | `git ls-tree bd6c424e292815460033b2570dce4d682ef5cb73 internal/principalhosts/principalhosts.go`; blob `fa3d92b554f774e263ac7009f1345abaabfe2dd6` | Research scout |
| F-02 | Environment discovery exists at supplied base. | same command for `internal/environment/discover.go`; blob `fb71b6103aa83d98096bd80b8e844eb4aebbe0e8` | Research scout |
| F-03 | Setup planner exists at supplied base; representability of host-specific actions is not established. | same command for `internal/setup/planner.go`; blob `7096c6b784a9d7bc66d9dcaa06dd81050009b54c` | Research scout |
| F-04 | Host scope and approved setup boundary are explicit in owning docs. | `docs/PRINCIPAL_HOSTS.md` §§1,6–8; onboarding §11; INVARIANTS DCI-082,104–108 | Research scout |

Step 0 re-verifies base and concrete M3 APIs. Bind existing types through an adapter;
do not invent fields on an existing M3 type. A false fact escalates. Manual exact
file/entry application is the specified fallback to inadequate setup action kinds.

## Interface / algorithm contract

Proposed interface belongs entirely to `internal/principalhosts`; these are host
integration records, not core protocol records. All returned slices/bytes are copies.

```go
// Existing M3 observation is adapted into this bounded view without new discovery.
type HostObservation struct {
    HostID, Executable, Version, OS string
    ObservedAt time.Time
    State string // absent | detected | incompatible | unknown | ambiguous
    EvidenceRefs []string
}
type IntegrationRequest struct {
    HostID, ProjectID, PrincipalWorkspace, MCPExecutable string
    RuntimeHome, PrincipalBindingPath string // absolute; binding optional, default below
    Mode string // strict | assisted
}
type PlannedFile struct {
    Path, BeforeSHA256, AfterSHA256 string // absent preimage uses literal "absent"
    Content []byte
    OwnedEntry string // config entry "devcadence"; empty for dedicated owned asset
}
type IntegrationPlan struct {
    Digest string
    Request IntegrationRequest
    Observation HostObservation
    Files []PlannedFile
    ManualSteps []string
    ApprovalRequired bool // true for every plan with mutation/manual configuration
    AutomaticApplyEligible bool // requires enforceable exclusive mutation channel evidence
}
type ApprovedPlan struct { PlanDigest, ApprovalRef string }
type ApplyReceipt struct {
    PlanDigest, ApprovalRef string
    AppliedPaths, FailedPaths []string
    Manual bool
    EvidenceRefs []string
}
type ProbeCheck struct {
    Route, Result, EvidenceRef string // result: pass | fail | unavailable
    ObservedAt time.Time
}
type VerificationReport struct {
    HostID, HostVersion, OS, SessionRef, PlanDigest string
    State string // strict_ready | assisted_ready | unverified | blocked
    Checks []ProbeCheck
    Reasons []string
}
type PrincipalHost interface {
    Detect(context.Context) ([]HostObservation, error)
    Plan(context.Context, HostObservation, IntegrationRequest) (IntegrationPlan, error)
    ApplyApproved(context.Context, IntegrationPlan, ApprovedPlan) (ApplyReceipt, error)
    Verify(context.Context, IntegrationPlan, string /* sessionRef */) (VerificationReport, error)
}
```

Detect wraps M3, sorts by HostID/executable, and never probes authentication secrets.
Plan validates nonempty project ID and canonical absolute executable/workspace/RuntimeHome
paths plus optional canonical absolute PrincipalBindingPath. RuntimeHome and binding
(default RuntimeHome/config/principal-binding.json) must be disjoint from target source,
owned/private and satisfy WP-M5-2 binding/parent ownership, no-symlink and permission
requirements. Missing HOME or unsupported ownership/ACL verification blocks launch.
The service rejects source-containing/overlapping runtime or binding paths and refuses
workspace/source overlap in strict mode, and rejects an ambiguous selected host.
Project/source identity comes from the shared facade, never a host-selected path.
Canonical plan digest is SHA-256 of versioned JSON (format `principal-host-plan/v1`)
covering request, observation identity, sorted files including content/preimages,
and ordered manual steps; exclude digest itself. Plans contain no credentials.

Apply checks approval reference through existing approved setup authority; a nonempty
string alone is insufficient. Bind both digest and exact action scope. Automated apply
is permitted ONLY in an exclusive operator-owned mutation channel that establishes
and enforces exclusion of all host, user and other process writers for the entire
operation including rollback. An advisory lock, stdio tool approval, same-user private
mode or operator statement does not prove this exclusion. If the adapter cannot enforce
it, AutomaticApplyEligible is false and ApplyApproved returns manual-required with
zero writes. The default shared host configuration path is therefore manual unless
an independently enforced exclusive channel is demonstrated for the installed OS.

Under verified exclusion, re-read every initial preimage/executable/version before
mutation, then resolve every destination immediately before EACH write using rooted
no-follow directory/file handles and recheck identity/preimage. Never follow a parent
or destination symlink; rooted resolution must prevent substitutions/escapes. Parse
JSON preserving entries except owned devcadence. Replacing an unequal owned entry
must be explicit in the approved plan. Temporary-sibling atomic rename is permitted
only inside this enforced channel and validated root, and is not a compare-and-swap
claim or protection against external writers. Any observed writer/exclusion breach
stops the operation before the next write; no blind retry or acceptance occurs.

Before mutation, capture in memory each old file's BeforeContent, existence, POSIX mode
and ownership IDs (or supported equivalent verified ACL/owner metadata), plus rooted
identity. Total captured BeforeContent is limited to 1 MiB across the plan; exceeding
this bound or unavailable metadata makes the plan manual-required, zero automated
writes. Recovery data are private process memory, not exported model evidence. New
files record an absent preimage; existing files must have restorable metadata.
If a later write fails while the process/channel remains alive, bounded rollback may
restore old bytes/metadata or remove a newly created file ONLY when current bytes and
rooted identity still match the planned afterimage and exclusive channel remains
valid. Re-resolve no-follow roots and compare before EACH restoration. If bytes/path/
ownership differ or exclusion is lost, leave that path unchanged and report conflict.
No multi-file atomicity, durable backup or crash-atomic restoration is promised.
Crash/restart loses in-memory backups: perform fresh read-only observation, report
unknown/partial configuration, and produce manual recovery/replanning instructions;
never resume writes or infer that rollback occurred. Receipts identify partial paths,
without containing old config secrets. All successful applications still require Verify.

If M3 cannot express actions, verify approval, or enforce the exclusive writer channel, Plan returns reviewable exact files
and manual steps. ApplyApproved reports manual-required without writing; the operator
applies approved configuration through vendor-supported UI/files. Verify checks
actual bytes, not an operator assertion. No new setup schema is silently introduced. AutomaticApplyEligible is computed by
trusted adapter enforcement, never caller-provided approval; this package may not
build a new OS sandbox/exclusive-channel mechanism as LOCAL_DISCRETION. If no
accepted enforced channel exists at step 0, ship approved manual application only.

Antigravity workspace config: `.agents/mcp_config.json`, root `mcpServers`, entry:

```json
{"mcpServers":{"devcadence":{"command":"/absolute/path/devcadence-mcp","env":{"DEVCADENCE_PROJECT_ID":"project-id","DEVCADENCE_HOME":"/absolute/private/devcadence-home"}}}}
```

Cursor workspace config: `.cursor/mcp.json`, root `mcpServers`, entry:

```json
{"mcpServers":{"devcadence":{"type":"stdio","command":"/absolute/path/devcadence-mcp","env":{"DEVCADENCE_PROJECT_ID":"project-id","DEVCADENCE_HOME":"/absolute/private/devcadence-home"}}}}
```

No args are required. Both recipes require absolute DEVCADENCE_HOME. If the operator
selects a nondefault binding path, add DEVCADENCE_PRINCIPAL_BINDING with the validated
absolute PrincipalBindingPath; otherwise WP-M5-2 uses HOME/config/principal-binding.json.
Neither value confers same-OS-user writer exclusion or accepted authority.
Project ID is configuration, not authority. Neither recipe
requires source cwd; do not assume Cursor supports a `cwd` key. Antigravity optional
cwd is a principal/runtime directory. Install scoped Antigravity rule with YAML
`trigger: always_on` in `.agents/rules/*.md` and skill with description frontmatter
in `.agents/skills/devcadence-principal/SKILL.md`. Cursor rule is `.cursor/rules/*.mdc`
with `alwaysApply: true`; plain `.md` is not a rule. Rules explain semantic-first
work, progressive evidence, proposed versus accepted decisions and escalation;
they must not claim to enforce authorization or source isolation.

VS Code guidance gives portable `.mcp.json`/`mcpServers` and native
`.vscode/mcp.json`/`servers` examples with explicit stdio type, project env and
absolute executable and the same required DEVCADENCE_HOME/optional absolute
DEVCADENCE_PRINCIPAL_BINDING contract. Explain Agent Host forwarding and supported OS caveats.
Portable VS Code recipe (native .vscode/mcp.json changes only the root to servers):

```json
{"mcpServers":{"devcadence":{"type":"stdio","command":"/absolute/path/devcadence-mcp","env":{"DEVCADENCE_PROJECT_ID":"project-id","DEVCADENCE_HOME":"/absolute/private/devcadence-home"}}}}
```

Optional nondefault binding is rendered in all three recipes as the exact additional
env pair `"DEVCADENCE_PRINCIPAL_BINDING":"/absolute/private/principal-binding.json"`
only when IntegrationRequest.PrincipalBindingPath is nonempty and verified.
Do not promise native UI configuration automation or verified host version floors.

Verify captures host/version/OS/session, config/asset digests, executable identity
and permission evidence identity. Basic connectivity uses WP-M5-2 stdio client and shared
facade `project_state` with configured project binding, no state mutation. WP-M5-2 supplies a reduced facade: absent production task/review
ports must deny without effects. Basic smoke/host-ready here never means full-task or
M5 runtime readiness; successful delegate/validate/review needs the separate accepted
follow-on runtime window and its end-to-end evidence. Native
host instructions/tool discovery need actual session evidence. Strict checks use
a harmless externally prepared sentinel outside principal workspace under the
protected source boundary, exposing only success/denial metadata, never source text.
Test absolute-path native read, search/index route, native write, terminal read/write,
unsandboxed/alternate-shell escape, and other enabled file/shell/MCP routes. The
fixture writer is isolated test infrastructure, not principal authorization.
An allowed access, missing route inventory or unavailable probe blocks strict.
Every enabled route must have a fresh denial test; excluded routes require evidence
that the installed host disables them. MCP bridge/daemon trusted repository access
is explicitly outside principal native tools and governed by WP-M5-1/WP-M5-2.

Reports are valid only for the observed session and exact identities. Before any
reuse, refresh config, version, executable and policy identities; a new session,
identity drift, changed route inventory or unavailable policy introspection requires
reverification. A timeout/cancel never means pass. Assisted may be ready only when
connection and instructions pass, and its report records isolation failures/unknowns.
The service reports no persisted accepted-ready flag; callers store reports only as
evidence and cannot upgrade their state. Launch/login/install remains manual guidance.

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Host selection/mode | Explicit operator selection and observed M3 facts | Presence of Antigravity, model/vendor preference |
| Configuration mutation | Existing approved setup authority bound to plan digest/actions; otherwise manual | Detected executable, proposed plan, tool approval |
| Project semantic access | Shared Principal facade with WP-M5-1 policy/project binding | Host env/rule/stdio caller identity alone |
| Acceptance/decision freezing | WP-M5-1 canonical authorized operation with CAS | Connected host, rule, MCP approval, proposed decision |
| Strict isolation result | Fresh complete native negative tests plus host/OS permissions evidence | Empty workspace, deny prompt, connection success |
| Automated apply | Enforced exclusive operator-owned mutation channel, approved plan and rooted per-write checks | Advisory lock, private mode, host permission or assumed absence of writers |
| Rollback | Live captured bytes/metadata, validated root, exact afterimage and continuing exclusion | Blind backup overwrite, restart with lost backup, inferred durable restore |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| Host observation | Absent normal state | Report unknown, no apply | Re-detect | Block selected host |
| Project/executable/workspace/HOME/binding | Invalid argument, no plan | Resolve through facade/operator | Revalidate identities | Block overlapping/escaping paths |
| Approval | Manual-required, zero writes | No apply | Renew exact plan approval | Unauthorized, zero writes |
| Config/preimage | Absent valid only if planned | No overwrite | Conflict, replan | Parse error/ownership conflict |
| Session/instructions/connection | Unverified | Unverified | Re-probe | Block integration-ready |
| Policy/route inventory | Strict blocked | Strict blocked | Re-probe | Strict blocked; assisted disclosed |
| Probe denial evidence | Unavailable, never pass | Unavailable | Re-probe | Fail strict; preserve result |
| Writer exclusion/rollback capture | Manual-required, zero automatic writes | Manual-required | Re-establish before apply | Stop writes; partial conflict if begun |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| Approval/preimage validation fails | No writes | Replan/approved manual path | Reason and digest |
| Config invalid/ownership ambiguous | Original bytes intact | Operator correction | Redacted parse/conflict evidence |
| First atomic write fails | Original file intact | Retry new reviewed plan | Path/OS error |
| Later file fails, live exclusion intact | Partial application never ready | Bounded in-process bytes/metadata rollback | Receipt and hashes |
| Exclusion unavailable/breached or between-write external edit | No first write if unavailable; no next write after breach | Manual-required/conflict; do not clobber edit | Channel/path/preimage evidence |
| Between-write symlink substitution | Substituted target never accessed or written | Rooted no-follow rejection; stop | Path identity denial |
| Crash/restart loses captured backups | No resumed writes or inferred restoration | Fresh observation/manual recovery | Explicit unknown/partial state |
| External edit before rollback | External bytes preserved | No restore after exclusion loss; manual recovery | Current/expected digest |
| MCP child fails/times out | No accepted state change | WP-M5-2 bounded cleanup; unverified | Exit/status metadata |
| Instructions undiscoverable | No integration-ready | Correct recipe/session | Native discovery result |
| Any source access succeeds | Strict blocked | Correct actual permissions or assisted | Route and outcome, no contents |
| Probe/policy introspection unavailable | Strict blocked | Supported manual evidence/re-probe or assisted | Explicit unavailable route |
| Host/config/policy drift | Previous report unusable | Reverify | Changed identity refs |

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| Observed host selection | M3 facts adapted to HostObservation | Proposed bounded view | Step-0 bind; false base fact escalates |
| Planned native changes | IntegrationPlan/PlannedFile digest/preimages | Specified | Review representation before implementation |
| Setup approval/exclusion | Existing authority plus ApprovedPlan digest and enforced exclusive writer channel | Conditional | Manual application, no fabricated grant/exclusion |
| Config result/recovery | ApplyReceipt; process-private BeforeContent/existence/mode/ownership, total ≤1 MiB | Specified, live only/nontransactional | Missing exclusion/metadata/bound: manual-required |
| Semantic connection | WP-M5-2 client/shared internal/principal facade | Dependency-owned | Block if published dependency unavailable |
| Strict/assisted readiness | VerificationReport + ProbeCheck | Specified | Unknown is blocked strict, never guessed |
| Accepted domain authority | WP-M5-1 policy/CAS | Dependency-owned | No host-local substitute |

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | No supported host | Detect/plan options | Absent reported; skip and independent worker capabilities remain usable | REQ-01; INV-06 |
| ACC-02 | Multiple/unknown versions | Choose/plan without explicit resolved host | No silent host selection/apply | REQ-01,02 |
| ACC-03 | Antigravity/Cursor selected | Render native plan | Required absolute HOME, optional binding/default, exact paths/type/frontmatter/env; no source cwd; unrelated JSON preserved | REQ-03,05 |
| ACC-04 | No approval, wrong digest, stale preimage | Apply | Zero bytes changed | REQ-03,04; INV-02 |
| ACC-05 | M3 lacks host action representation | Apply reviewed plan | Manual-required; actual post-apply bytes verified | REQ-04 |
| ACC-06 | Live mid-apply failure with captured bytes/metadata; then exclusion loss/external edit | Recover | Restore unchanged exact afterimages only while exclusion holds; preserve external edit and report conflict | REQ-04,10; INV-03 |
| ACC-07 | Source-free workspace/configured MCP | Smoke | Shared facade project state; wrong binding rejected, no source or mutation required; absent runtime denies task/review without effects, no full-task claim | REQ-06,09 |
| ACC-08 | Native file/terminal/other-tool source access allowed | Verify strict | Strict blocked despite connected MCP/always-on rule | REQ-07; INV-04 |
| ACC-09 | Primary shell denied, unsandboxed/alternate shell succeeds | Verify strict | Strict blocked; escape route recorded | REQ-07 |
| ACC-10 | Complete denied route inventory and fresh native session evidence | Verify strict | strict_ready only for exact host/version/OS/session/policy | REQ-07,10,12 |
| ACC-11 | Policy or host introspection unavailable | Verify strict then assisted | Strict blocked; assisted_ready only if connection/instructions pass | REQ-08,12 |
| ACC-12 | Host approval allowed but domain authorization/CAS denied | Invoke facade mutation through MCP | Denied/conflict according to WP-M5-1; no accepted record | REQ-09; INV-05 |
| ACC-13 | Config/permission/executable/session changes | Reuse report | Reverification required; no strict-ready cached shortcut | REQ-10 |
| ACC-14 | VS Code sample and unsupported Windows MCP sandbox | Review guidance | Correct roots/current portable option; no unsupported sandbox claim | REQ-11 |
| ACC-15 | GUI absent or instruction probe timeout | Record validation | Unavailable checks explicit; no empirical integration-ready claim | REQ-12 |
| ACC-16 | Host/shared-user config with only advisory lock/tool approval | Apply | Manual-required, zero writes; no writer exclusion inferred | REQ-04; INV-02 |
| ACC-17 | First file applied under test channel, external edit between writes | Write second file | Recheck detects exclusion/preimage breach; second file/edit unchanged, partial report | REQ-04,10; INV-03 |
| ACC-18 | Destination/parent substituted by symlink between writes | Continue apply/rollback | Rooted no-follow rejects; outside target unchanged | REQ-04; INV-03 |
| ACC-19 | Crash after first file, restart without BeforeContent | Observe/recover | No write resume/automatic restore; fresh partial observation/manual instructions | REQ-04,10; INV-03 |
| ACC-20 | Missing/relative/source-overlapping HOME or binding, capture >1 MiB/unknown owner | Plan/apply | Invalid runtime paths blocked; unsafe/oversize capture manual-required, zero writes | REQ-04,05 |

## Validation

- Run focused host-adapter/configuration tests with `go test -race ./internal/principalhosts/...`.
- Run affected CLI tests, shared-boundary checks and repository `make verify` on the immutable candidate.
- Validate rendered JSON by parsing; assert unrelated entry preservation and exact native discovery paths/frontmatter.
- Execute ACC-08–ACC-11 using controlled sentinel fixtures and hostile route simulations; mocks must exercise plan/verification code, not preassign ready flags.
- Capture separate native human smoke packs for Antigravity and Cursor with exact installed version, OS, session, config/permission identities, route inventory, connection/instruction/negative-test outcomes.
- Human smoke was unavailable during authoring; no host connection or strict-ready result is claimed here. Candidate may pass deterministic checks while host verification remains blocked.
- mutation testing: mutation review sufficient: adversarial catalog below; reviewers actively demonstrate each listed mutant fails an assertion.

### Mutation Catalog (Mandatory)

| Mutant (Plausible Bug / Omission / Boundary Inversion) | Expected Test Failure (Scenario / Invariant Check) |
| --- | --- |
| Treat missing host as global setup failure | ACC-01 independent-capability assertion |
| Automatically prefer/install Antigravity | ACC-02 no mutation/selection assertion |
| Omit plan approval/preimage digest binding | ACC-04 byte invariance assertion |
| Rewrite whole config, lose unrelated MCP entry | ACC-03 parsed unrelated-entry equality |
| Apply unsupported M3 host action by invented schema | ACC-05 zero automatic writes assertion |
| Restore backup over intervening external edit | ACC-06 external digest preservation |
| Assume advisory lock or host approval excludes same-user writers | ACC-16 zero-writes assertion |
| Check preimages only once before multi-file writes | ACC-17 next-path equality/partial-state assertion |
| Follow swapped parent/destination symlink | ACC-18 outside-target unchanged assertion |
| Resume/restore on restart without captured BeforeContent | ACC-19 no recovery writes assertion |
| Omit required HOME/binding path validation or 1 MiB/metadata bound | ACC-03/ACC-20 rejection and zero-writes assertions |
| Copy semantic implementation into adapter | Boundary test plus ACC-07 facade spy |
| Use empty workspace/connected status as firewall proof | ACC-08 strict state assertion |
| Skip alternate/unsandboxed terminal negative test | ACC-09 complete-route assertion |
| Treat timeout/unavailable route as denial | ACC-11/ACC-15 blocked strict assertion |
| Omit host/session/policy binding or reuse stale ready report | ACC-10/ACC-13 identity assertion |
| Use tool approval/env project ID as accepted authority | ACC-12 no accepted-record assertion |
| Mark assisted as strict or fail assisted solely for source access | ACC-11 explicit-mode assertion |
| Install plain Cursor .md or invalid Antigravity trigger | ACC-03 native manifest assertion |
| Claim Windows VS Code MCP sandbox supported | ACC-14 guidance assertion |

### Required Independent Review Lenses (Dual-Lens Review Pack)

1. **Contract & Authority Reviewer:** complete contract, immutable diff, M3/dependency interface facts, authority separation, source-route coverage, rollback safety, secret redaction and package boundaries.
2. **Test Adequacy & Mutation Reviewer:** ACC-01–ACC-20 assertions, real verification path exercised, hostile route/mock fidelity, each catalog mutant, honest native-host unavailable evidence.

- evidence to capture: base/head/diff identity; commands and exit statuses; asset digests;
  approval/action references; pre/post/rollback digests; native version/OS/session;
  each route outcome; unavailable coverage; independent findings/dispositions.

## Escalation triggers

Stop implementation and return to the Principal when an undeclared schema, authority,
protocol, persistence or cross-layer change is needed; mandatory sources conflict;
WP-M5-1/WP-M5-2 contracts cannot support binding; approved setup authority cannot be
verified and operator manual flow is unavailable; a config ownership/path is ambiguous;
strict source routes cannot be enumerated; or a failure/missing-input case is unspecified.
Unavailable strict enforcement blocks strict-ready, not all assisted/control-plane work.

## Design / rationale

Preferred: native scoped config/rules plus a shared semantic facade and empirical
readiness report. Alternative: plugin-only distribution offers convenient packaging
but introduces separate host/version discovery and plugin-loading dependencies.
A manual reviewed recipe remains viable without widening M3 setup schema. Automated
installer/config mutation is outside this package until its authority is represented.
Separate workspaces reduce ambient context but are not an OS security boundary.
Host tool approvals control invocation; server-side accepted authority controls effects.

Official sources verified 2026-10-05 (host version floors remain empirical):

- U-01 https://antigravity.google/docs/mcp — workspace/global config, command/env/args/cwd, mcpServers.
- U-02 https://antigravity.google/docs/skills — workspace .agents/skills and required skill description.
- U-03 https://antigravity.google/docs/rules — valid trigger frontmatter; plain AGENTS.md; discovery paths.
- U-04 https://antigravity.google/docs/permissions — Deny > Ask > Allow, MCP permissions, OS differences.
- U-05 https://cursor.com/docs/mcp and https://cursor.com/docs/rules — native config, optional args, stdio type table, .mdc alwaysApply, approvals; no published precise protocol-revision matrix verified.
- U-06 https://code.visualstudio.com/docs/agent-customization/mcp-servers — portable/native roots, Agent Host forwarding, trust, optional MCP process sandbox.
- U-07 https://code.visualstudio.com/docs/agents/reference/mcp-configuration — stdio fields and macOS/Linux-only MCP sandbox.
- U-08 https://code.visualstudio.com/docs/agents/run/security — approvals versus OS isolation; native agent terminal and MCP process boundaries differ.
- U-09 https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio — newline JSON-RPC, stderr logs, stdout protocol only; no prescribed CLI args.
- U-10 https://github.com/modelcontextprotocol/go-sdk — no-argument command example and version compatibility; v1.7+ supports 2026-07-28 and earlier revisions. WP-M5-2 owns selected SDK/version support, not this package.

Antigravity terminal sandbox documentation at https://antigravity.google/docs/sandbox
proves agent shell isolation under supported configured modes, not isolation of every
MCP subprocess. Do not infer that equivalence. Latest MCP revision is 2026-07-28;
its transport metadata model differs from older initialize-based sessions. Host
compatibility must be tested against WP-M5-2's declared protocol set.

## Implementation Readiness Report

```text
requirements represented: 12/12 in proposed bounded contract
mandatory clauses resolved: 8/8 named DCI clauses; preparation draft independently reviewed; execution-time admission still required
state transitions specified: 4/4 (observed, planned/applied, verified, drift invalidation)
failure cases specified: 13/13
authority decisions specified: 7/7
missing/unknown input semantics: 8/8
acceptance scenarios mapped: 20/20
unresolved architecture choices: 0 within this proposed contract
integration prerequisites: dependency contracts and M3 binding require step-0 verification
native human smoke: unavailable; no host empirical readiness claimed
independent preparation review: APPROVE_DRAFT (both lenses; window review record)
independent implementation readiness review: outstanding after dependency binding and required live evidence
readiness: NOT_READY — DRAFT; dependency binding and implementation readiness evidence outstanding
```

### Consistency sweep after every material revision

- [x] Search/remove superseded contract names, mode semantics and path claims.
- [x] Recompute requirement/state/failure/acceptance counts and map all mutants.
- [x] Confirm facade/transport ownership matches WP-M5-1 through WP-M5-3.
- [x] Verify Status and readiness remain consistent with actual independent review.
- [x] Re-resolve changed draft paths/risks/clauses and verify repaired draft blockers independently; execution-time dependency binding remains a separate prerequisite.

### Weaker-implementer check

- [ ] Yes — independently verified executable contract.
- [x] No — the preparation contract has independent draft approval, but dependency binding, required live evidence and implementation readiness verification remain outstanding; do not delegate implementation yet.


## Owning-contract alignment

| Owning contract | This host draft's role | Conflict / implementation synchronization |
| --- | --- | --- |
| [MCP_API bootstrap executable contract](../MCP_API.md#bootstrap-executable-contract) and [WP-M5-2](wp-m5-2-semantic-mcp-ewp.md) | Render the same no-argument stdio executable and its project/action binding. | No additional MCP tools or wire schema. WP-M5-2 owns registration, DTO serialization and transport limits; host-specific configuration is confined to the adapter. |
| [MCP_API §2A](../MCP_API.md#2a-discovery-tool-set) and [WP-M5-3](wp-m5-3-discovery-ewp.md) | Expose the same nine discovery tools through the facade. | No competing discovery persistence contract. Host observation/setup types are Go adapter types, not principal request DTOs. |
| [PRINCIPAL_HOSTS](../PRINCIPAL_HOSTS.md) | Adds proposed concrete launch recipes, source-boundary verification and safe mutation behavior. | Exact host instructions and owning contract must synchronize during implementation; installed-host evidence remains outstanding. |


## Implementation record

Implemented from base `100a3cc` (WP-M5-1, -2 and the WP-M5-3 read-side subset merged) in the working tree, **without commit**. **Gate exception:** the repository owner explicitly authorized implementation while the header status stays DRAFT/NOT_READY; no independent Contract/Authority or Test Adequacy review and no separate-reviewer mutant observation has run. This record does not change the contract above.

**Dependency binding (Step 0, from repository facts).** WP-M5-2 is merged: `cmd/devcadence-mcp` is a no-argument stdio server (`DEVCADENCE_PROJECT_ID`, absolute `DEVCADENCE_HOME`, optional `DEVCADENCE_PRINCIPAL_BINDING`) with 11 tools; the MCP SDK is confined to `internal/mcpadapter` and `cmd/devcadence-mcp`, and the facade is `internal/principal/facade`. WP-M5-3 is merged read-side only: no discovery tool is registered, so no discovery smoke or tool expectation is implemented (**blocked on WP-M5-3 writes**). M3 `internal/setup` has operation kinds `create_directory`, `write_managed_config`, `remove_stale_cache`, `run_diagnostic_check` and `ensure_local_model` only: host configuration cannot be represented, and no enforced exclusive operator-owned mutation channel exists. Per the EWP this selects the specified fallback: reviewed exact files plus manual application. M3's `principalhosts.FromEnvironment` is adapted by `Detect`; no field was added to an M3 type.

**Delivered.**

- `internal/principalhosts/integration.go` (types, `Detect`, `Plan`, `ApplyApproved`, `Service`, `PrincipalHost`), `verify.go` (`Verify`, `Reuse`, `Prober`), `smoke.go` (`SmokeProjectState`), `home_*.go`, `assets/` (embedded rule and skill text).
- Antigravity plan: `.agents/mcp_config.json` (no `type`), `.agents/rules/devcadence-principal.md` (`trigger: always_on`), `.agents/skills/devcadence-principal/SKILL.md` (description frontmatter). Cursor plan: `.cursor/mcp.json` (`type: stdio`, no `cwd`), `.cursor/rules/devcadence-principal.mdc` (`alwaysApply: true`). Unrelated JSON is preserved semantically (values re-serialized with indentation, keys sorted); an unequal existing `devcadence` entry is replaced only with an explicit manual step; non-object or malformed JSON, symlinks and non-regular files are refused.
- Golden fixtures `fixtures/principalhosts/*`; VS Code guidance and samples in `integrations/vscode/`; boundary test `TestPrincipalHostsStayOutsideTheSemanticLayers`; owning docs synchronized.
- `SmokeProjectState` speaks the identical wire contract to the real binary over newline-delimited stdio without the SDK, from an empty temporary directory with only the three DevCadence env variables, and reports only pass/fail metadata.

**Requirement and acceptance coverage.**

| Item | Where proved |
| --- | --- |
| REQ-01, ACC-01, ACC-02 | `TestDetectReportsEveryStateSeparately`, `TestPlanRequiresAnExplicitDetectedHost` |
| REQ-02 | `PrincipalHost` interface, compile-time assertion; boundary test |
| REQ-03, ACC-03 | digest in `Plan`; `TestRenderedNativePlansMatchGoldenFixtures`, `TestNativeRecipeSemantics`, `TestExistingConfigIsMergedNotRewritten` |
| REQ-04, ACC-04, ACC-05, ACC-16 | `TestApplyNeverWritesAndRefusesUnauthorizedOrStalePlans` (zero writes always; manual) |
| REQ-05 | same golden/semantic tests |
| REQ-06, ACC-07 | `TestSmokeAgainstTheRealServerBinary` (project state read; wrong project and missing binding rejected; absent-runtime denial is WP-M5-2's `TestA8_*`) |
| REQ-07, ACC-08..ACC-10 | `TestStrictReadyOnlyWithCompleteDenialEvidence`, `TestStrictIsBlockedByAnyAccessUnknownOrEscape` |
| REQ-08, ACC-11 | same, assisted labeled with recorded shortfalls |
| REQ-09, ACC-12 | no host approval path reaches the server; denial/CAS is covered by WP-M5-1/2 tests (`TestA2_*`, `TestA10_AcceptIsHardDisabled`); not re-tested here |
| REQ-10, ACC-13 | `TestReuseRequiresUnchangedIdentities`, `TestConnectionInstructionsAndDriftGateReadiness` |
| REQ-11, ACC-14 | `TestVSCodeGuidance` |
| REQ-12, ACC-15 | `Unverified` for unavailable/timeout/cancel; no live host smoke claimed |
| ACC-20 | `TestInvalidRuntimePathsAreBlocked` (capture bound is moot: nothing is captured) |

**Not delivered (explicit).** ACC-06, ACC-17, ACC-18 and ACC-19, and the 1 MiB capture, rooted no-follow per-write handles and in-process rollback: they only apply to automated apply, which is not built because no enforced exclusive channel exists (`AutomaticApplyEligible` is always false). The CLI setup exposure under `cmd/devcadence` was not added: no REQ/ACC needs it. No real `Prober` ships; Antigravity and Cursor native smoke packs are **manual evidence required** (below). Discovery tool smoke is blocked on WP-M5-3 writes.

**Manual evidence required (operator steps, per host).** On an installed Antigravity or Cursor: (1) record the exact host version and OS; (2) review and hand-apply the planned files byte-for-byte; (3) restart/reload the host and open a new session, record the session reference; (4) confirm the `devcadence` server connects and the always-on rule is discovered; (5) place a harmless sentinel outside the principal workspace and, for every enabled route (absolute-path read, search/index, write, terminal read and write, unsandboxed or alternate shell, any other file/shell/MCP tool), attempt access and record only allowed or denied; (6) record the host permission policy identity and which routes the host disables; (7) supply these as a `Prober` and run `Verify`. Until then every report is `unverified` or `blocked`, and strict-ready is not claimed.

**Ambiguities and interpretations.** (1) Source roots needed for the overlap rules are not in `IntegrationRequest`; they are a service option the shared facade should supply, and strict mode refuses to plan without them. (2) Runtime-home protection checks only the home directory (plain, `0700`, owned); ancestors and the binding file are enforced by the server at launch and surface as a failed smoke. (3) `ApprovalVerifier` is an interface because M3 has no host-plan approval authority to bind; with none, apply returns manual-required. (4) `VerificationReport` gained `Identities`, needed to bind and compare drift identities. (5) Route names in `RequiredRoutes` are this slice's labels, not a host vocabulary. (6) The smoke offers protocol `2025-06-18` in `initialize`; the server negotiated successfully, but the 2026-07-28 revision's metadata model was not separately exercised. (7) Instruction and tool discovery cannot be checked without a live session and are prober responsibilities.

**Mutation observation.** Not observed by a separate reviewer; tests are written to fail the listed mutants but that is unverified.

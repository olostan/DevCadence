# WP-M5-R4 — Protected operator evidence ingress and receipts

## Identity

- Revision: 3 (owner review of r2 head 106dafd repaired; verdicts pending re-verification); task: task-m5-r4-operator-ingress; window: [2026-10-G](window-2026-10-g-overview.md).
- Base: `71bdaec6d6d81c1b6e52d8b30f0f8485f928925a` (`main` `cebb4f0` plus the PR #83 reconcile merge, 2026-10-05).
- Contract digest: reviewed immutable Git blob. No fictitious runtime state revision; record the actual accepted dependency commits at execution.
- Endpoint: competent Go implementer with POSIX filesystem/ownership and `crypto/ed25519` skill; complete admission of this contract is mandatory.
- Status: **DRAFT / NOT_READY / NOT FROZEN. No implementation authority.** Part A is freezable first; Part B and any production enrolment are BLOCKED on OWNER INPUT-1.
- Dependencies: WP-M5-1 `ApplyBatch`, `BatchGuard`, `BatchReadView` (merged). No dependency on R1–R3. Consumers: WP-M5-3 write side, WP-M5-4 `ApprovalVerifier`, [WP-M5-5](wp-m5-5-empirical-campaign-ewp.md) `operatorAuthority` (wired by [M5-R3](wp-m5-r3-empirical-verifier-composition-ewp.md)), [M5-R2](wp-m5-r2-independent-review-acceptance-ewp.md) acceptance policy and [M5-R1](wp-m5-r1-native-task-executor-ewp.md) execution policy activation.
- Parts (separately freezable): **A** pure receipt verifier, trust-anchor protection check, one-time consumption (no owner input needed to freeze). **B** operator issuer ceremony and key custody (needs OWNER INPUT-1).

## Objective

Make human/owner authority an enforceable property rather than a label. Define one receipt: a detached Ed25519 signature, made by a key the model-reachable processes cannot use, over an exact statement bound to project, purpose, subject, artifact digest and expiry; and one verifier that fails closed unless the trust anchors it relies on are structurally outside the verifier process's own OS identity. The same primitive unblocks three consumers that are currently denied: human-confirming discovery writes (WP-M5-3: `NEEDS_HUMAN`), host-plan approval (WP-M5-4: `ApprovalVerifier` nil, apply is manual-only), and campaign authorization (WP-M5-5: production `denyAuthority`, `OPERATOR_AUTHORITY_UNAVAILABLE`). It also supplies policy-activation authority to M5-R2 (acceptance policy) and M5-R1 (execution policy: which endpoints, source exposure and spend are granted).

## Context Manifest

Role: security-contract implementer / independent authority reviewer. Read envelope: `internal/mcpadapter/{binding,launch}*.go` (protected-file checks to reuse, not rewrite), `internal/controlplane/{batch,service,records}.go`, `internal/protocol` canonical JSON/digest and record registry, `internal/principalhosts/integration.go` (`ApprovalVerifier`), `internal/benchmark/empirical/types.go` (`operatorAuthority`), WP-M5-3 `HumanReceiptVerifier` text. Write scope: new `internal/operator/receipts/` (+ tests), one additive record kind `ReceiptConsumption` with schema and fixtures, optional `cmd/devcadence-operator/` (Part B only), owning docs. No change to `INVARIANTS.md` or the invariant catalog.

Exact normative clauses (admit exact text at the readiness gate): AGENTS §§2–9, 12–15, 17; SECURITY §§1, 3, 7, 12A, 14–17; ADR-0024 §§3–5; DCI-005, 012, 015, 032, 033, 080–084, 090, 123–124, 134, 160 and the WP-M5-3 authority matrix. Risks: authority forgery, trust-anchor substitution, replay, confused-deputy rendering, TOCTOU between verification and effect, clock skew, key custody. Re-resolution triggers: the owner chooses a mechanism other than a separate OS identity, a new crypto dependency, a change to the record registry or `BatchGuard`, or a consumer needing a purpose not in the closed vocabulary.

## Scope envelope

Authorized: `internal/operator/receipts` (types, canonical encode, verify, anchor store, ownership check, consumption guard, host-plan adapter); additive record kind `ReceiptConsumption` registered like existing kinds, with `schemas/` file and fixtures; Part B only: `cmd/devcadence-operator` and a `Signer` interface with the one backend named by OWNER INPUT-1; synchronization of SECURITY §17 bootstrap posture, PRINCIPAL_HOSTS, MCP_API and the schema README.

Forbidden: any MCP tool or facade method that mints, imports, lists or revokes receipts or anchors; reading credentials or provider sessions; network access; a new third-party dependency (Ed25519 comes from the standard library; hardware-key signature formats need a separate dependency decision and amendment); treating a same-identity file, CLI flag, environment variable, `AllowedActions` value or `PolicyRef` as authority; a verifier fake constructible by production composition; changing existing record, event or wire schemas.

LOCAL_DISCRETION: file and helper layout, test fixtures, error wording that is safe and fixed, the `fs` abstraction used to test ownership checks. Everything under Interfaces, Algorithms and Failure semantics is MUST.

## Requirements and invariants

| ID | MUST requirement | Invariant |
| --- | --- | --- |
| R1 | A receipt verifies only if its Ed25519 signature checks against an enrolled, unexpired, unrevoked anchor whose public key is allowed for that purpose | I1: a request field, flag or file that merely says "human" is never authority |
| R2 | The trust-anchor and revocation files are loaded only from a path whose every component is owned by a uid in the explicit `TrustedOwnerUIDs` allow-list that is also different from the verifier's effective UID, not group/world-writable, not a symlink and free of ACL entries; the operator directory and its two files must be owned by the single configured `OperatorUID`. Composition supplies `TrustedOwnerUIDs = {0, OperatorUID}` (system ancestors such as `/`, `/etc`, `/Library` are normally root-owned; with OPTION A and root as operator, `OperatorUID = 0`). The verifier refuses to run as UID 0 | I2: an identity that can reach the model's tools cannot edit the thing that decides who is trusted; "owner is not me" alone is insufficient (any third account could own the directory) |
| R3 | A receipt binds `project_id`, `purpose`, subject `{kind,id,version}`, `subject_digest`, optional `input_digest`, the rendered statement digest and a bounded validity window; any mismatch with the consumer's recomputed values fails | I3: a receipt cannot be reused for another project, purpose, artifact or revision |
| R4 | A `once` receipt is consumed in the same transaction as the effect it authorizes; replay fails; a rolled-back effect leaves it unconsumed | I4: one human act authorizes at most one effect, and no effect commits without its consumption |
| R5 | A `grant` receipt is accepted only until expiry or revocation and is re-verified at every use. "Use" is defined once for the window: every `Delegate`, every `Review` start, every acceptance `Decide`, every campaign run admission; the policy file bytes and the **anchor set** are pinned for the process, while the receipt files and **`revoked.json`** are re-read and strictly decoded on every `Verify` (see Algorithm: protection check). Revocation therefore takes effect at the next use without relaunch; replacing `anchors.json` takes a relaunch (enrolment ceremony) | I5: standing authority is bounded and revocable; trust roots are immutable for the process |
| R6 | Verification returns a trusted in-process `Verified` value; wire/request data never constructs it; the zero value is invalid and every consumer and `ConsumeOnce` refuse it via `Verified.IsValid()` | I6: no accepting JSON or test double can reach production authority, and a zero-value struct cannot pass as one |
| R7 | The issuer renders the human-visible text from the exact artifact bytes by a typed renderer, never from request-supplied text | I7: the human signs what will be applied |
| R8 | Missing, unreadable, malformed, expired, unknown-anchor, unprotected-anchor, unreadable-revocation or ambiguous inputs deny with `NEEDS_HUMAN` and zero effects | I8: absence of authority is never defaulted |
| R10 | `Statement.HumanActorID` is signer-controlled text and is **never** trusted by itself: `Verify` returns `Verified` only when `Statement.HumanActorID == anchor.HumanActorID` of the selected anchor, and the identity that consumers (WP-M5-3 adapter) report is taken from the anchor | I10: a signer cannot assert another human's identity under its own key |
| R9 | Receipt creation, enrolment and revocation have no model-reachable surface | I9: the principal cannot mint, enrol or revoke |

## Verified facts and representability gaps

| Fact / gap | Evidence at pinned base | Implication |
| --- | --- | --- |
| Production operator authority is a deny seam | `internal/benchmark/empirical/types.go` `operatorAuthority`; `admission.go` `denyAuthority` | consumer R3 wires this; R4 supplies the verified value |
| `ApprovalVerifier` exists with no implementation; automatic apply never eligible | `internal/principalhosts/integration.go`; WP-M5-4 record | R4 unblocks approval only; WP-M5-4 still needs an exclusive writer channel for automatic apply, so manual apply remains |
| Discovery writes refuse `NEEDS_HUMAN` | `facade.Service.DiscoveryWrite`; WP-M5-3 record | the consumer interface `HumanReceiptVerifier` is specified in WP-M5-3 §Dependency surface; its mutation code is not written |
| Launch binding checks ownership but rejects only unrelated users | WP-M5-2 "Launch and trusted caller binding"; `internal/mcpadapter` | this protects against other users, not a same-UID agent; R2 rule below is stricter and new |
| Records are stored via `Command.Records` and read via `BatchReadView.Record` | `internal/controlplane/{service,batch}.go` | consumption is a record id `receipt_id`, version 1 |
| Storage idempotence of a duplicate `(kind,id,version)` insert is not established by this EWP | not verified | the explicit `BatchGuard` existence check below is mandatory and does not rely on a storage error; step 0 verifies behaviour and records it |
| No Ed25519/receipt code, no anchor store, no `devcadence-operator` binary | repository search | all new |
| `WP-M5-3` `ReceiptSubject{Kind,ID,Version}`, `HumanReceiptVerifier.Verify(ctx, caller, receiptRef, purpose, inputDigest, subject)` and `principalhosts.ApprovalVerifier.VerifyApproval(ctx, ref, planDigest, paths)` are the consumer contracts | `wp-m5-3-discovery-ewp.md` §Dependency surface; `internal/principalhosts/integration.go` | R4 supplies thin adapters to both (Part A, below); `receiptRef`/`ref` is a `ReceiptID` |
| `empirical.CampaignAuthorization` has `AuthorizedBy` (a label) and no receipt field | `internal/benchmark/empirical/types.go` | the receipt is found by subject, never named inside the signed bytes (see Lookup) |

Step 0 re-verifies each row; a false row escalates.

## Interfaces and wire representation

Family `operator-receipt` version `1.0`, strict decoding: unknown fields, duplicate JSON keys, trailing content, non-canonical numbers refuse. Digests are `sha256:` over protocol canonical JSON bytes.

~~~go
type Purpose string // closed
const (
    PurposeDiscoveryDecision   Purpose = "discovery.product_decision"
    PurposeDiscoveryRequirement Purpose = "discovery.requirement_confirm"
    PurposeDiscoveryLedger     Purpose = "discovery.ledger_resolution"
    PurposeDiscoveryReflection Purpose = "discovery.reflection"
    PurposeDiscoveryRisk       Purpose = "discovery.accepted_risk"
    PurposeHostPlanApply       Purpose = "host.plan_apply"
    PurposeAcceptancePolicy    Purpose = "acceptance.policy_activate"
    PurposeExecutionPolicy     Purpose = "execution.policy_activate"
    PurposeCampaignAuthorize   Purpose = "empirical.campaign_authorize"
)
type Use string // "once" | "grant"; fixed per purpose, never chosen by the receipt
type Subject struct { Kind, ID string; Version int }
type Statement struct {
    Version        string  // "1.0"
    ReceiptID      string  // "rcpt_" + 26 base32 chars, issuer generated, unique
    Purpose        Purpose
    Use            Use     // must equal the fixed table value
    ProjectID      string
    Subject        Subject
    SubjectDigest  string  // digest of the exact canonical artifact bytes
    InputDigest    string  // "" unless the purpose table requires it
    Text           string  // <= 4096 bytes, issuer-rendered, printable UTF-8, no control characters
    TextDigest     string  // sha256 of Text bytes
    HumanActorID   string  // signed copy; MUST equal the selected anchor's human_actor_id (R10); the consumer-visible identity is the anchor's
    AnchorID       string
    IssuedAt, NotAfter string // RFC3339 UTC
}
type Receipt struct { Statement Statement; Signature string } // base64 Ed25519 over "devcadence-receipt/1\n" + canonical Statement
type Request struct {
    ReceiptID string          // OPTIONAL. "" = discover by subject (see Lookup); non-empty = exactly that file
    ProjectID string
    Purpose Purpose
    Subject Subject
    SubjectDigest, InputDigest string // recomputed by the consumer, never copied from the receipt
}
type Verified struct { /* unexported fields incl. a package-private validity token */ } // constructed only by Verifier implementations in this package
func (Verified) Statement() Statement
func (Verified) IsValid() bool                 // false for the zero value or any value not produced by a Verifier
type Verifier interface { Verify(context.Context, Request) (Verified, error) }
type Consumption struct { // protocol record kind "ReceiptConsumption", schema 1.0, version 1, id = ReceiptID
    SchemaVersion, ReceiptID, ProjectID, Purpose, SubjectDigest, InputDigest, AnchorID string
    EffectKind, EffectID string // e.g. "ProductDecision"/id, "HostPlan"/digest; set by the consumer
    ConsumedAt string
}
// ConsumeOnce refuses (error, no guard) when !v.IsValid() or when v's purpose Use is "grant".
func ConsumeOnce(v Verified, effectKind, effectID string) (controlplane.BatchGuard, controlplane.RecordToStore, error)
// AuditGrantUse is the grant counterpart: no replay guard; returns the record "<receipt_id>:<effect_id>" for audit. Refuses !IsValid or Use "once".
func AuditGrantUse(v Verified, effectKind, effectID string) (controlplane.RecordToStore, error)
type FileOptions struct {
    OperatorDir string        // default per platform; DEVCADENCE_OPERATOR_DIR override is protection-checked too
    ReceiptsDir string        // default DEVCADENCE_HOME/receipts; need NOT be protected (receipts are signed)
    OperatorUID uint32        // owner required for OperatorDir, anchors.json and revoked.json (the operator account; 0 allowed when root is the operator)
    TrustedOwnerUIDs []uint32 // required non-empty and MUST contain OperatorUID; expected composition {0, OperatorUID} so root-owned ancestors pass; root (0) is never the verifier euid; no entry may equal the verifier euid
    Clock clock.Clock         // required
}
func NewFileVerifier(opts FileOptions) (Verifier, error) // refuses when R2 protection fails, TrustedOwnerUIDs is empty or lacks OperatorUID, or OperatorUID/any entry equals the euid
~~~

**Receipt location and lookup (single rule, replaces any per-consumer receipt path).** Every receipt is a file `DEVCADENCE_HOME/receipts/<ReceiptID>.json` (64 KiB cap). Consumers never read receipt files themselves, and policy loaders do **not** read `config/*.receipt.json`. With `Request.ReceiptID` set, exactly that file is verified. With it empty, `Verify` enumerates `receipts/*.json` (sorted by name, at most 256 files, files over the cap or failing strict decode are skipped), keeps those whose `Purpose`, `ProjectID`, `Subject` and `SubjectDigest` equal the request, fully verifies each, and returns the verifying one with the latest `IssuedAt` (ties: lexicographically greatest `ReceiptID`); none verifying returns `receipt-missing` (or the most specific failure of the best candidate). A `once` consumer is given the `ReceiptID` by the human/host (the `receiptRef`/`ref` argument of the consumer ports); `grant` consumers (policies, campaigns) discover by subject, so no receipt reference is ever embedded in the signed artifact (a receipt id is issuer-generated and signed over the digest of those same bytes, so it cannot live inside them).

**Subject table (R3 binding, exact).** Digests are `sha256:` over protocol canonical JSON of the stated value; policies carry no floating point (spend is integer micro-USD) so digests do not depend on float formatting; step 0 verifies `protocol.Digest` number canonicalization.

| Purpose | `Subject.Kind` | `Subject.ID` | `Subject.Version` | `SubjectDigest` over | `InputDigest` |
| --- | --- | --- | --- | --- | --- |
| `discovery.product_decision` | `ProductDecision` | decision id | decision record version | canonical `DecisionRecord` | discovery `InputDigest` the decision answers (WP-M5-3) |
| `discovery.requirement_confirm` | `Requirement` | requirement id | record version | canonical requirement record | as above |
| `discovery.ledger_resolution` | `AmbiguityResolution` | ambiguity id | ledger version | canonical resolution | none |
| `discovery.reflection` | `ProblemModelReflection` | problem-model id | problem-model revision | canonical `ReflectionInput` minus `HumanReceiptRef` | `ReflectionInput.InputDigest` |
| `discovery.accepted_risk` | `AcceptedRisk` | risk id | record version | canonical risk record | discovery `InputDigest` |
| `host.plan_apply` | `HostPlan` | plan digest | 1 | canonical `HostPlanScope{plan_digest, paths}` (adapter below) | none |
| `acceptance.policy_activate` | `AcceptancePolicy` | `PolicyID` | `Revision` | canonical policy document | none |
| `execution.policy_activate` | `ExecutionPolicy` | `PolicyID` | `Revision` | canonical policy document | none |
| `empirical.campaign_authorize` | `CampaignAuthorization` | plan digest | 1 | the `authorizationDigest` argument of `VerifyAuthorization` | none |

WP-M5-3's write side MUST adopt the discovery rows verbatim (it owns the record versions); a mismatch is a re-resolution trigger, not a local choice.

**Consumer adapters (Part A, in `internal/operator/receipts`).** `HostPlanApprovals{Verifier}` implements `principalhosts.ApprovalVerifier`: `VerifyApproval(ctx, ref, planDigest, paths)` canonicalizes `paths` (each must be absolute, NUL-free, equal to its `filepath.Clean` form and free of `..`; sorted ascending by bytes; a duplicate or non-canonical entry refuses), builds `HostPlanScope{PlanDigest, Paths}`, and calls `Verify` with `Purpose: host.plan_apply`, `ReceiptID: ref`, `Subject{HostPlan, planDigest, 1}`; success requires `Verified.IsValid()`; the receipt is then consumed by the apply path through `ConsumeOnce` in the effect's transaction (today manual apply never consumes, so the adapter alone grants nothing). `HumanReceipts{Verifier}` implements WP-M5-3 `HumanReceiptVerifier`: maps `(receiptRef, purpose, inputDigest, subject)` to a `Request` with `ReceiptID: receiptRef` and returns `HumanReceipt{HumanActorID: Statement.HumanActorID, SourceRef: receiptRef, Purpose, InputDigest, Subject}`.

Fixed purpose table (use, maximum validity, `input_digest` required): `discovery.*` once, 1 h, required for decision/requirement/reflection/risk and not for ledger; `host.plan_apply` once, 1 h, no; `acceptance.policy_activate` and `execution.policy_activate` grant, 30 days, no; `empirical.campaign_authorize` grant, 7 days, no. A receipt whose `NotAfter - IssuedAt` exceeds the maximum, whose `Use` differs from the table, or whose `IssuedAt` is more than 5 minutes in the future refuses.

Trust-anchor files (read-only to the verifier). Directory `OperatorDir` contains `anchors.json` `{version:"1.0", anchors:[{anchor_id, human_actor_id, public_key (base64 Ed25519), not_before, not_after, purposes:[Purpose]}]}` (pinned per process) and `revoked.json` `{version:"1.0", receipt_ids:[], anchor_ids:[]}` (re-read at every `Verify`; enrolment creates it with empty lists). Default directory is a platform constant (`/etc/devcadence-operator` on Linux, `/Library/Application Support/DevCadence/operator` on macOS); `DEVCADENCE_OPERATOR_DIR` may name another path, but R2 protection is checked on whatever path is used, so the override cannot weaken the property. Windows and other platforms: `NewFileVerifier` refuses.

### Algorithm: protection check (R2)

Two files, two lifetimes. **`anchors.json` is immutable for the process**: its bytes are read once at construction, their sha256 is pinned and the decoded anchor set is held in memory; replacement (enrolment, key rotation, anchor revocation by expiry edit) takes effect only in a relaunched process. **`revoked.json` is dynamic**: it is protection-checked, read and strict-decoded fresh on every `Verify`; its content may change without relaunch. The directory and both files share the one chain check below.

1. `euid := os.Geteuid()`; if `euid == 0` refuse (a root verifier has no separation to rely on).
2. Resolve the absolute path without following symlinks; walk every component from `/` to each of the three targets (`OperatorDir`, `anchors.json`, `revoked.json`) with `Lstat`.
3. For each component: not a symlink; owner uid is in `TrustedOwnerUIDs` **and** `!= euid`; `mode & 0022 == 0` (no group/world write; a sticky directory does not excuse this); regular file or directory as expected; a file has no write bit for anyone but its owner. **Operator ownership:** `OperatorDir`, `anchors.json` and `revoked.json` MUST be owned by exactly `OperatorUID`; ancestors above `OperatorDir` may be owned by any uid in `TrustedOwnerUIDs` (the mixed chain `/`=0, `/Library`=0, ... `operator`=OperatorUID is the expected macOS shape, `/etc`=0 ... `devcadence-operator`=OperatorUID the Linux shape). macOS note: user-created directories can be group-writable for `admin`/`staff`, so the `0022` rule is checked, not assumed; `/etc`, `/var` and `/tmp` are symlinks on macOS, so a path through them refuses and the operator names the real path (the default `/Library/Application Support/DevCadence/operator` has no symlink component).
4. **ACL handling (concrete, per supported OS; no cgo, no new dependency).** *Linux:* for every component call the standard-library `syscall.Getxattr` for `system.posix_acl_access` and, for directories, `system.posix_acl_default`; `ENODATA` means absent (accept); `ENOTSUP`/`EOPNOTSUPP` means the filesystem cannot hold POSIX ACLs (accept); any success (an ACL exists) or any other error refuses. *macOS:* for every component run `/bin/ls -ld -- <path>` through `process.Runner` (fixed absolute executable, argv only, minimal environment, 5 s timeout) and parse the first whitespace-delimited field: it must match `^[-dl][-rwxsStT]{9}[@ ]?$`; a trailing `+` (ACL present) refuses, and any output not matching the pattern refuses. Other OS: `NewFileVerifier` refuses (as for Windows). Tests inject an `fs.ACLPresent(path) (bool, error)` seam; the OS adapters are exercised by the manual operator step. An unknown ownership/ACL capability is therefore never a vague refusal: each OS has the exact probe above and anything outside it refuses.
5. The chain check runs at construction **and at every `Verify`** (all three targets). Additionally: at every `Verify` the sha256 of the `anchors.json` bytes is re-read and compared with the pinned value; a changed `anchors.json` refuses (`anchors-changed`) until relaunch. `revoked.json` is **not** digest-pinned; it is strictly decoded (`{version:"1.0", receipt_ids, anchor_ids}`, unknown fields, duplicates or trailing content refuse; 64 KiB cap) on every `Verify`; a missing, unreadable, malformed or unprotected `revoked.json` refuses (`revocation-unavailable`, or `anchor-unprotected` for a failed chain check): fail closed, never "no revocations".

### Algorithm: Verify

1. Load nothing from the request except `ReceiptID` and the binding fields. With a non-empty `ReceiptID` resolve `DEVCADENCE_HOME/receipts/<ReceiptID>.json` only after the id matches `^rcpt_[a-z2-7]{26}$`; with an empty one apply the Lookup rule above. Size cap 64 KiB. Strict decode.
2. Re-run the R2 protection check on all three targets; compare the `anchors.json` digest with the pinned value; read and strict-decode `revoked.json` fresh (step 5 of the protection check). The anchor set used is the in-memory set pinned at construction.
3. Reject if the receipt id is in `revoked.json.receipt_ids` or its anchor id is in `revoked.json.anchor_ids`; the anchor must exist, be inside `not_before..not_after`, and list `Purpose`.
4. Check the fixed table (use, validity, input requirement); that `ProjectID`, `Purpose`, `Subject`, `SubjectDigest`, `InputDigest` equal the **consumer's** values; `TextDigest == sha256(Text)`; and **`Statement.HumanActorID == anchor.HumanActorID`** of the anchor named by `Statement.AnchorID` (mismatch: `receipt-invalid`). `Verified.Statement().HumanActorID` is therefore always the anchor's enrolled identity.
5. Verify the signature with the anchor key. Compare `now` (injected clock) with `[IssuedAt-5m, NotAfter]`.
6. Return `Verified`. Any failure returns a typed sentinel mapped by consumers to `NEEDS_HUMAN` with a fixed evidence ref (`receipt-missing`, `receipt-invalid`, `receipt-expired`, `receipt-revoked`, `anchor-unprotected`, `anchors-changed`, `revocation-unavailable`, `receipt-binding-mismatch`, `receipt-replayed`) and never echoes receipt text, key material or paths.

### Algorithm: consumption and replay

`ConsumeOnce` returns (a) a **Precondition** `BatchGuard` that fails with `receipt-replayed` when `view.Record(ctx,"ReceiptConsumption",id,1)` exists (existence is checked explicitly, never inferred from a storage error) and (b) the `RecordToStore` to attach to the member that carries the effect. The consumer re-runs `Verify` immediately before building the batch and the batch's expected prefix is the usual WP-M5-1 compare, so two concurrent uses serialize and exactly one wins. Grants are not consumed; each use re-verifies and, for audit, the consumer may attach the record returned by `AuditGrantUse` (id `<receipt_id>:<effect_id>`).

### Algorithm: issuer ceremony (Part B)

`devcadence-operator issue --request <file>` runs as the operator identity from OWNER INPUT-1. It reads an immutable `ApprovalRequest` (project, purpose, subject, exact canonical artifact bytes) produced by the server side on `NEEDS_HUMAN`. The `ApprovalRequest` JSON format is **specified at Part B freeze** (it is blocked on OWNER INPUT-1 and nothing in Part A depends on it; Part A's fixtures use hand-built `Statement`s); it is noted here so it is not mistaken for closed; recomputes `SubjectDigest`; renders `Text` with the purpose's typed renderer (below); displays text, digest, project and validity to the controlling TTY; requires the human to type `approve <first 8 hex of subject digest>` read from `/dev/tty` (anti-accident only, not the isolation mechanism); signs through `Signer`; writes the receipt to a path the human names or to stdout. Renderers print every authority-bearing field: decision/requirement/ledger text and ids; host plan file paths with content digests and the manual/automatic mode; the entire `AcceptancePolicy` or `ExecutionPolicy` (endpoints, source exposure, domains, caps); for campaigns the plan digest, tiers, run count, every cap, allowed endpoints, source classes, network domains, credential reference names (never secrets), metered and unknown-quota grants, the verification-profile digest and the distinct executables its checks run. Unknown artifact kind or a field the renderer cannot display refuses.

## OWNER INPUT-1 — protected-ingress mechanism on a single-user machine

Decision owner: repository owner. **Safe default: deny.** Until answered, no anchor exists, `Verify` returns `receipt-invalid`/`anchor-unprotected`, and everything in "Remains blocked" stays blocked. The verifier-side rule R2 holds for every option below; options differ in who may *use* the signing key.

| Criterion | A. Dedicated OS account (or root) owns key, anchors and issuer; run via `su`/`sudo -u` with password each time | B. Same-UID TTY or Touch ID/keychain prompt | C. A plus hardware security-key touch for each signature | D. Key on a second device; signature carried back |
| --- | --- | --- | --- | --- |
| Independent of a same-UID agent that has a shell | yes, if sudo/su requires a password for every use | **no**: the agent can drive a pty or substitute the anchor | yes, plus physical presence | yes if the device is not agent-reachable |
| Anchor substitution resistance | yes (R2) | no | yes (R2) | requires R2 location anyway |
| Setup burden on one machine | one extra account and one directory; `sudo` configured with `timestamp_timeout=0` | none | key purchase, SSH-sk or WebAuthn tooling; macOS system OpenSSH lacks sk support | second device workflow |
| New dependency | none | none, plus cgo for keychain | SK signature verification needs a reviewed dependency (not in `go.mod`) | none |
| Testability here | high (ownership abstraction) | low | medium (needs hardware) | low |
| Residual risk | cached sudo credential lets an agent drive the issuer through a pty | defeats the purpose | tooling drift | operator error |

**Recommendation: A for the first slice**, with C as a later hardening that changes only the `Signer` and verifier key format. B is rejected as insufficient and is listed so the reason is on record. Required answers: (1) option, (2) the operator account name or `root`, (3) OS (macOS/Linux) and path, (4) acceptance that Windows is unsupported, (5) whether cached `sudo` credentials are disabled. **Remains blocked until answered:** Part B, any enrolment, human-confirming discovery writes, host-plan approval, campaign authorization, acceptance-policy and execution-policy activation (so any live task, review or campaign). Part A, the verifier and consumption guard, and all offline tests proceed.

## Authority matrix

| Effect | Authority | Forbidden substitute |
| --- | --- | --- |
| Confirm human intent / approve plan / authorize campaign / activate policy | Verified receipt, bound as above | `authority: human` field, `AllowedActions`, `PolicyRef`, host tool approval, same-UID JSON, CLI flag |
| Enrol, rotate or revoke an anchor | Operator identity writing the protected directory out of band | any MCP/facade/CLI path of the verifier process |
| Consume a receipt | `ApplyBatch` member in the effect's transaction | post-hoc marker, in-memory set |

## Missing / unknown / stale and failure semantics

| Input | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| Anchor dir / `anchors.json` | `NEEDS_HUMAN`, zero effects | unknown owner/ACL probe result: refuse | changed digest: `anchors-changed`, refuse until relaunch | refuse |
| `revoked.json` | `revocation-unavailable`, refuse | unreadable/unprotected: refuse | edited content takes effect at the next `Verify` (no relaunch) | strict-decode failure: refuse |
| Receipt | `receipt-missing` | unknown purpose/anchor: refuse | expired/revoked: refuse | signature/digest/binding mismatch: refuse |
| Clock | injected clock required; unavailable denies | n/a | skew over 5 minutes denies | n/a |

| Failure boundary | Required postcondition | Evidence |
| --- | --- | --- |
| Verification fails before batch | zero records/events | no consumption record |
| Batch rolls back (stale prefix, guard, crash) | receipt unconsumed and reusable until expiry | state unchanged |
| Ambiguous commit response | consult state/record before any retry; a repeat is `receipt-replayed` if the first committed | consumption record |
| `anchors.json` replaced mid-run | next `Verify` refuses (`anchors-changed`) until relaunch | digest comparison |
| `revoked.json` edited mid-run | next `Verify` sees the new content; a newly listed receipt/anchor refuses | fresh strict decode |

## Traceability and acceptance scenarios

| ID | Setup → action → expected | Requirement → invariant → representation |
| --- | --- | --- |
| A1 | forged/modified statement or wrong key → verify → refuse | R1 → I1 → signature over canonical statement |
| A2 | anchor directory owned by the process UID, or process is root → construct → refuse | R2 → I2 → protection check |
| A3 | writable parent, symlink component, group-writable file → verify → refuse | R2 → I2 → component walk |
| A4 | receipt for other project/purpose/subject/digest/input digest → verify → binding mismatch | R3 → I3 → Request equality |
| A5 | expired, future-dated, over-long validity, wrong use → verify → refuse | R3/R5 → I3/I5 → purpose table |
| A6 | same `once` receipt used twice → second effect → refused, no second event | R4 → I4 → `ConsumeOnce` |
| A7 | batch rolls back after verification → retry → receipt still valid | R4 → I4 → same transaction |
| A8 | two concurrent uses → exactly one commits | R4 → I4 → expected-prefix serialization |
| A9 | grant receipt id or anchor id added to `revoked.json` while the process runs (no relaunch) → next use → refuse; removed again → verifies again | R5 → I5 → fresh revocation read |
| A10 | test/fake or wire data → obtain `Verified` → impossible (unexported fields; package boundary test) | R6 → I6 |
| A11 | issuer given a request whose text differs from the artifact → refuses; text rendered only from bytes | R7 → I7 → renderer (Part B) |
| A12 | every missing/unknown input above → `NEEDS_HUMAN`, zero effects, no secret in message | R8 → I8 |
| A13 | MCP tool list and facade grants contain no receipt/anchor tool | R9 → I9 → existing allowlist test extended |
| A14 | host-plan adapter: valid receipt bound to plan digest and exact path list → `ApprovalVerifier` true; changed, reordered-then-uncanonical, duplicate or relative path list → false | R1/R3 → I1/I3 |
| A15 | zero-value `Verified{}` or one decoded from JSON → `ConsumeOnce`, `AuditGrantUse` and every adapter refuse; `ConsumeOnce` of a `grant` Verified refuses | R6 → I6 → `IsValid` |
| A16 | empty `ReceiptID`: two valid receipts for one subject → latest `IssuedAt` wins; one revoked → the other is used; none → `receipt-missing`; a receipt whose subject digest differs is never selected | R3/R5 → I3/I5 → Lookup |
| A17 | anchor directory owned by a uid not in `TrustedOwnerUIDs` (even if not the euid) → refuse; empty allow-list or allow-list without `OperatorUID` → construct refuses | R2 → I2 |
| A18 | `anchors.json` bytes changed after construction → refuse `anchors-changed` until a new verifier is constructed; `revoked.json` malformed, missing, unreadable, group-writable or symlinked → every `Verify` refuses (fail closed) | R2/R5 → I2/I5 → split lifetimes |
| A19 | receipt signed by a valid anchor whose `Statement.HumanActorID` differs from that anchor's enrolled `human_actor_id` → refuse `receipt-invalid`; equal → `Verified.Statement().HumanActorID` equals the anchor's; the `HumanReceipts` adapter reports the anchor's identity | R10 → I10 |
| A20 | mixed chain: `/`=0, `/Library`=0, `.../operator`=OperatorUID(501), files=501, euid 502 → accepted; `.../operator` owned by 0 while OperatorUID=501 → refuse (final owner mismatch); an ancestor owned by 777 ∉ allow-list → refuse; an ancestor owned by the euid → refuse; OperatorUID=0 (root operator) with root chain → accepted; euid 0 → refuse | R2 → I2 → ownership model |
| A21 | ACL per OS through the injected seam: Linux ACL xattr present on any component → refuse, `ENODATA`/`ENOTSUP` → accept, other errno → refuse; macOS `ls -ld` field with `+` → refuse, non-matching output → refuse; unsupported OS → construct refuses | R2 → I2 → ACL probe |

A live A2 against a real separate account is a manual operator acceptance step after OWNER INPUT-1; unit tests use the `fs` abstraction and never claim it.

## Validation and Mutation Catalog

`go test -count=1 -race ./internal/operator/... ./internal/controlplane/... ./tests/...`; `make schemas`; `make docs-check`; `make verify`. Record versions, receipt fixtures, ownership-abstraction cases and exit statuses.

Test seams (explicit): ownership/mode/`Lstat` come from an `fs` interface in `FileOptions`-internal construction (unexported field set only by `_test.go` helpers in package `receipts`; production `NewFileVerifier` always uses the OS implementation); the clock is the required `Clock`; concurrent-use (A8) runs two goroutines through `ApplyBatch` on one SQLite file. The "no exported fake" claims (A10/A15) are enforced by a package-boundary test under `tests/` that parses `go list -json` for `internal/operator/receipts` and asserts no exported identifier other than the declared API can produce a `Verified` and that no non-test importer constructs `FileOptions` with an `fs` override.

| Mutant | Expected failure |
| --- | --- |
| Skip signature check or accept empty signature | A1 |
| Allow anchors owned by the process UID, allow UID 0, follow symlinks | A2/A3 |
| Ignore one binding field (project, purpose, subject digest, input digest) | A4 |
| Drop expiry/max-validity/use-table check | A5 |
| Existence check removed or reliant on storage error | A6 |
| Consume before the effect commits, outside the transaction | A7 |
| Process-local replay set instead of record | A8 |
| Revocation ignored | A9 |
| Exported constructor for `Verified` | A10 |
| Render text from the request | A11 |
| Echo receipt text or paths in an error | A12 |
| Register a receipt-minting tool | A13 |
| Plan path list not in the signed subject | A14 |
| `IsValid` ignored, or `ConsumeOnce` accepts a grant | A15 |
| Lookup picks the first/any file without full verification or ignores subject digest | A16 |
| Ownership check only compares to euid | A17 |
| Revocation file digest-pinned (revocation needs relaunch) or cached, or missing file treated as no revocations | A9/A18 |
| `anchors.json` replacement accepted in a running process | A18 |
| `HumanActorID` taken from the statement without the anchor equality check | A19 |
| Whole chain required to be OperatorUID-owned (rejects root ancestors), or any `TrustedOwnerUIDs` entry accepted for the operator dir | A20 |
| ACL probe skipped, or unknown probe result treated as no ACL | A21 |

Independent lenses: Contract/Authority; Test Adequacy/Mutation. The issuer author does not verify the verifier.

## Rationale, escalation and readiness

Selected: detached signature plus protected-location trust anchors plus transactional consumption record, versus alternatives of an in-process secret/HMAC token or a marker file. An in-process secret is readable by anything with the same identity and cannot prove human presence; a marker file is forgeable by the same identity. The cost is an operator setup step, which is the point of the boundary. Rejected variant: storing anchors in the same-UID runtime home; a hardware key does not help if the agent can enrol its own key there.

Escalate on: owner chooses B or a mechanism without a location the process identity cannot write; a requirement for Windows; a consumer needing standing authority beyond the two grants; a storage layer that cannot enforce the explicit existence guard in the same transaction; any request to let the principal view receipts' private material.

Implementation Readiness Report:

~~~text
author tally after repair round 2 (r3) (a self-count, not evidence; independent re-verification PENDING):
requirements represented: 10 (R1-R10) with R2/R5/R6 tightened in r2 and R2/R5/R10 in r3
acceptance scenarios mapped: 21 (A1-A21); mutation rows 22
unresolved architecture choices: 1 (OWNER INPUT-1, Part B only); 1 deferred format (`ApprovalRequest`, Part B)
readiness: Part A NOT_READY pending independent re-verification, current-base gate and step-0 fact checks; Part B BLOCKED
~~~

Weaker-implementer check for Part A: author expectation only, to be tested by the independent Implementability reviewer; Part B: no, until OWNER INPUT-1.

## Changelog

- r1: initial draft for window 2026-10-G.
- r3: owner review of head 106dafd: `anchors.json` bytes pinned per process vs `revoked.json` re-read fresh at every `Verify` (fail closed), resolving the contradiction with "changed digests refuse until relaunch" (items 12); `HumanActorID == anchor.HumanActorID` required (R10, 13); explicit `OperatorUID` + `TrustedOwnerUIDs = {0, OperatorUID}` composition, mixed-chain acceptance cases and concrete Linux/macOS ACL probes (14); "every Evaluate" aligned to `Decide`; new refs `anchors-changed`, `revocation-unavailable`; `receipts` imports `controlplane` (stated in the overview dependency paragraph).
- r2: repair round 1: trusted-owner allow-list and macOS notes; optional-`ReceiptID` subject lookup and one receipt location; exact subject table per purpose; host-plan path canonicalization and consumer adapters; `Verified.IsValid`, `AuditGrantUse`, grant `ConsumeOnce` refusal; per-use re-verification definition; explicit test seams; `ApprovalRequest` format noted as Part B; honest readiness tally.

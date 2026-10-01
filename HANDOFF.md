# Handoff — docs/cognitive-invocation-compiler-review-ledger

Last updated: 2026-09-30 by architecture session

PR #17 Base: `5324be77f82947084dc71c2ecc0caff9b60a1bb1` (main after merge of PR #16)
Active PR: https://github.com/olostan/DevCadence/pull/17

## Purpose

PR #17 is an architecture/documentation change, not runtime implementation.

It materializes two related decisions:

1. **Cognitive Invocation Compiler**
   - deterministic mandatory-rule applicability/admission;
   - optional lexical/graph/dense retrieval;
   - mandatory clauses cannot be similarity-ranked away;
   - bounded ContextPack + endpoint-specific PromptProjection;
   - model-visible process complexity stays small even if control-plane policy grows.

2. **Durable Review Ledger**
   - preserves existing ReviewCampaign / FindingDisposition / ClosureDecision compatibility;
   - normalizes raw ReviewResult observations into stable ReviewFinding identity;
   - authors emit FindingResolution (`fix_attempted | challenge`);
   - independent ResolutionVerification closes/invalidates attempted resolutions;
   - focused verification replaces repeated unrestricted review loops.

ADR-0020 is the owning architectural decision.

## Current state

- PR #16 is merged and accepted.
- PR #17 is based on current main; its early API-authored history was squashed before review began, and post-review repair commits were preserved without rewriting reviewer anchors.
- Runtime/compiler/review-ledger code is intentionally **not** implemented in this PR.
- WP-M3C-2 owns session drivers + the Cognitive Invocation Compiler.
- WP-M3C-5 separately owns minimal durable ReviewFinding/FindingResolution/ResolutionVerification primitives.
- M4 owns empirical retrieval/prompt-renderer/effective-load evaluation, including seeded mis-mapping ground truth.
- M7 owns rich multi-review orchestration, lenses, falsification and aggregation.

## Key invariants added

- DCI-131 — control-plane complexity does not imply prompt complexity.
- DCI-132 — mandatory applicability is never similarity-ranked away.
- DCI-133 — operative obligations are resident; rationale is retrievable.
- DCI-134 — attempted resolution is not independent verification.
- DCI-135 — review conversation is not canonical review state.

## Review focus

Challenge especially:

1. deterministic applicability vs optional retrieval boundary;
2. whether path/domain/risk/action + explicit dependency mappings can fail closed without becoming unmaintainable;
3. backward compatibility with existing review schemas;
4. whether M3C-2 scope is still bounded;
5. PromptProjection abstraction and model-format neutrality;
6. whether dense embeddings should remain optional until M4;
7. whether the M4 metrics can falsify the proposal;
8. duplicated/contradictory normative ownership;
9. small/local-model cognitive-load assumptions;
10. opportunities to simplify.

## Verification / limitations

- Documentation-only diff; no runtime behavior claimed.
- Existing review schemas are preserved as compatibility surfaces.
- Old duplicated local-agent token target guidance was removed; M4 remains calibration owner.
- Context followups CF-6, CF-7, CF-8 and CF-10 are closed by this architecture.
- External long-context/prompt-format research is cited as motivation only, not normative authority.


## PR #17 review processing

Two independent reviews converged on the same issues. The branch now addresses them without rewriting reviewed history:
- fail-safe mandatory admission adds always/capability-default/mapped classes plus reverse coverage;
- provisional pre-M4 ContextProfiles use real hard limits/reserves/uncertainty rather than an arbitrary fixed percentage;
- dense retrieval is removed from M3C's required baseline and left for M4 experiments;
- review-ledger primitives move out of WP-M3C-2 into WP-M3C-5;
- ReviewFinding uses canonical severity/materiality plus optional confidence;
- existing review schema citations/status/ADR ownership are reconciled;
- challenge verification is identity-blinded by default;
- prompt renderers must safely contain adversarial delimiter text;
- Contract Completeness Review has an explicit Principal owner as an effective-now manual step.


## Remaining closure condition

The author-side repair packet is complete. Per DCI-134 and ADR-0020's own proposed process, PR #17 becomes green only after an independent focused verifier confirms the accepted findings on the repaired candidate (or the reviewers' conditional approval is explicitly treated as satisfied by objective evidence). No additional broad review is required absent a repair regression, changed contract, or materially new evidence.

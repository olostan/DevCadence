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
- PR #17 branch was created from PR #16 head, then merged with current main without rebase or force-push.
- Runtime/compiler/review-ledger code is intentionally **not** implemented in this PR.
- M3C-2 owns the compiler + compact review-state foundation.
- M4 owns empirical retrieval/prompt-renderer/effective-load evaluation.
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

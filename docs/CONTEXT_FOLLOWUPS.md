# Context-Discipline Follow-Up Register

Open implementation/evidence items from the independent review of PR #14 (context admission and adaptive working sets) that are still unresolved. This is a work register, not normative design: ADR-0019 owns context layers, ADR-0020 owns Cognitive Invocation Compiler/review-ledger authority boundaries, and PROTOCOLS owns wire semantics. Close an item by linking the PR or ADR that resolves it, then delete the row.

Rationale for the register: DCI-018/019 are only as good as their tooling. Until M3C, several rules rely on manual discipline, and unrecorded review findings are how that discipline decays.

| ID | Item | Why it matters | Target | Closes when |
| --- | --- | --- | --- | --- |
| CF-1 | Repo docs grew net **+26 KB** (1,147 to 1,173 KB) in PR #14. Rules restated across AGENTS §2, docs/README, WORK_PACKAGES, PROTOCOLS §10B, ADR-0019, LOCAL_AGENTS, PRINCIPAL_ENGINEER. | The design's own DCI-018 argument applies to the docs. | Next docs PR | One owner per rule; other docs link instead of restating. |
| CF-2 | Add an informational per-PR report of net Markdown and `prompts/` size change. | The only cheap deterministic guard against regrowth that fits "budget packs, not files". | Before M3C | Report runs in CI; decide later whether it gates. |
| CF-3 | Archive `docs/work-packages/wp-m3b-*` (321 KB, completed). | Historical evidence that should sit outside default reading sets, not be deleted. | Next docs PR | Files moved, references updated, audit evidence preserved. |
| CF-4 | Manual manifests are "required now" with no checker. State which rules are advisory until M3C and who verifies a hand-written manifest. | An unverified manual manifest can silently omit a MUST, the failure DCI-019 forbids. | Before M3C | AGENTS/WORK_PACKAGES name the verifier and the advisory set. |
| CF-5 | No existing EWP fits a 20-30k window (25-65 KB each). Produce one worked atomic-contract split from a real M3B EWP. | Tests the "complete bounded contract fits a small endpoint" claim before machinery is built. | Before M3C | Example contract reviewed; measured size recorded. |
| CF-9 | Doc-review protocol requires propagating changes through direct and reverse references; no tool exists. First step: script listing changed headings and inbound links. | Otherwise review depends on manual graph work. | Before M3C | Script exists and is used by the doc-review checklist. |
| CF-11 | Render-check the Mermaid flowchart added to ADR-0019 with a real parser. | PR #14 could only inspect it manually. | Next docs PR | Parser run recorded. |
| CF-12 | Run the cheapest M4 baseline pair early: full-corpus versus manual manifest, on one implementation and one docs-review task. | Tests the hypothesis before more machinery is built. | Before M3C resolver work | Result recorded, with the missing endpoint classes disclosed. |


Resolved by PR #17 architecture:
- **CF-6:** WP-M3C-2 now has an explicit planning-size/scope split: 2A session execution substrate (medium) and 2B Cognitive Invocation Compiler/context mediation (large), each independently acceptable under the WP-M3C-2 umbrella; M4 compiler/context experiments require 2B, so there is no silent resolver/compiler slip past M4, and either slice must be split again if its EWP grows beyond the stated boundary.
- **CF-7:** WP-M3C-2 now names applicability/dependency mapping freshness as owned deterministic compiler work; unknown mapping fails closed.
- **CF-8:** endpoint working-set sizes are no longer duplicated as a worker rule; M4 owns empirical ContextProfile calibration.
- **CF-10:** review convergence no longer relies on an open-ended queued finding budget; stable findings are normalized before a bounded repair packet and closure uses thresholded structured state.

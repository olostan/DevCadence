# Context-Discipline Follow-Up Register

Open items from the independent review of PR #14 (context admission and adaptive working sets) that were **not** fixed in that PR. This is a work register, not normative design: ADR-0019 and PROTOCOLS §10B own the decisions. Close an item by linking the PR or ADR that resolves it, then delete the row.

Rationale for the register: DCI-018/019 are only as good as their tooling. Until M3C, several rules rely on manual discipline, and unrecorded review findings are how that discipline decays.

| ID | Item | Why it matters | Target | Closes when |
| --- | --- | --- | --- | --- |
| CF-1 | Repo docs grew net **+26 KB** (1,147 to 1,173 KB) in PR #14. Rules restated across AGENTS §2, docs/README, WORK_PACKAGES, PROTOCOLS §10B, ADR-0019, LOCAL_AGENTS, PRINCIPAL_ENGINEER. | The design's own DCI-018 argument applies to the docs. | Next docs PR | One owner per rule; other docs link instead of restating. |
| CF-2 | Add an informational per-PR report of net Markdown and `prompts/` size change. | The only cheap deterministic guard against regrowth that fits "budget packs, not files". | Before M3C | Report runs in CI; decide later whether it gates. |
| CF-3 | Archive `docs/work-packages/wp-m3b-*` (321 KB, completed). | Historical evidence that should sit outside default reading sets, not be deleted. | Next docs PR | Files moved, references updated, audit evidence preserved. |
| CF-4 | Manual manifests are "required now" with no checker. State which rules are advisory until M3C and who verifies a hand-written manifest. | An unverified manual manifest can silently omit a MUST, the failure DCI-019 forbids. | Before M3C | AGENTS/WORK_PACKAGES name the verifier and the advisory set. |
| CF-5 | No existing EWP fits a 20-30k window (25-65 KB each). Produce one worked atomic-contract split from a real M3B EWP. | Tests the "complete bounded contract fits a small endpoint" claim before machinery is built. | Before M3C | Example contract reviewed; measured size recorded. |
| CF-6 | M3C scope grew (schemas, resolver, mappings, projection freshness, lint, leases, restarts, telemetry) with no cost estimate. Consider M3C-a (drivers/budgets) and M3C-b (context resolver), or state that the resolver may slip without blocking M4. | Milestone risk. | M3C planning | IMPLEMENTATION_PLAN records the split or slip rule. |
| CF-7 | Path/domain/risk to clause mapping tables: no named owner and no staleness detection beyond projection freshness. | Unmapped means blocked, so drift stalls work or hides a MUST. | M3C design | Owner and drift check specified. |
| CF-8 | The provisional 8-12k table appears in ADR-0019 and again in LOCAL_AGENTS. | Will be read as a rule. | Next docs PR | Single copy, labeled hypothesis. |
| CF-9 | Doc-review protocol requires propagating changes through direct and reverse references; no tool exists. First step: script listing changed headings and inbound links. | Otherwise review depends on manual graph work. | Before M3C | Script exists and is used by the doc-review checklist. |
| CF-10 | Finding budget is now "prioritize 5, queue the rest". State the interaction with ADR-0010 bounded convergence so a reviewer cannot queue unbounded findings. | Keeps repair rounds bounded. | Next docs PR | REVIEW_AND_CONVERGENCE states the cap or escalation. |
| CF-11 | Render-check the Mermaid flowchart added to ADR-0019 with a real parser. | PR #14 could only inspect it manually. | Next docs PR | Parser run recorded. |
| CF-12 | Run the cheapest M4 baseline pair early: full-corpus versus manual manifest, on one implementation and one docs-review task. | Tests the hypothesis before more machinery is built. | Before M3C resolver work | Result recorded, with the missing endpoint classes disclosed. |

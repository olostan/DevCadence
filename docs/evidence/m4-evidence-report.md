# Milestone M4 Empirical Evidence Report & Gate Evaluation

## Executive Summary

- **Evaluated At:** 2026-10-05T08:18:23Z
- **Telemetry Report Digest:** `sha256:f816651558ef9f9183e78cbe8f1a66183ad9a9ff35d83fb07877493b9c2a5097`
- **Gate Decision:** **GO**
- **Summary:** All Milestone M4 gate criteria passed. Empirical evidence supports progressing to Milestone M5.

> [!IMPORTANT]
> **GATE DECISION: GO**
> All empirical criteria for Milestone M4 (Defect Catch Rate, Resource Efficiency, Delegation Floor) have passed.
> The Cognitive Invocation Compiler and 4-layer context architecture demonstrate empirical superiority over monolithic history.

### Epistemic Accounting Disclosure

> [!CAUTION]
> **Provisional Token Accounting:** Certain telemetry runs were collected from endpoints without calibrated native token counters.
> Token counts for these runs represent heuristic approximations and are explicitly marked as provisional per DCI-005.

## Gate Criteria Evaluation

| Criterion | Status | Threshold | Observed | Details |
|---|---|---|---|---|
| `defect_catch_rate_frontier_api` | ✅ PASS | >= 80.0% | 83.3% | Strategy 4 catch rate in tier frontier_api: 83.3% (threshold >= 80.0%) |
| `resource_efficiency_frontier_api` | ✅ PASS | <= 1.00x | 0.15x | Resource ratio in tier frontier_api: 0.15x versus baseline Strategy 1 (threshold <= 1.00x) |
| `peak_resident_context_frontier_api` | ✅ PASS | 1.00 | 0.30 | Peak resident context ratio in tier frontier_api: 0.30x (threshold <= 1.00x) |
| `defect_catch_rate_local_small` | ✅ PASS | >= 80.0% | 83.3% | Strategy 4 catch rate in tier local_small: 83.3% (threshold >= 80.0%) |
| `resource_efficiency_local_small` | ✅ PASS | <= 1.00x | 0.15x | Resource ratio in tier local_small: 0.15x versus baseline Strategy 1 (threshold <= 1.00x) |
| `peak_resident_context_local_small` | ✅ PASS | 1.00 | 0.30 | Peak resident context ratio in tier local_small: 0.30x (threshold <= 1.00x) |
| `defect_catch_rate_subscription_cli` | ✅ PASS | >= 80.0% | 83.3% | Strategy 4 catch rate in tier subscription_cli: 83.3% (threshold >= 80.0%) |
| `resource_efficiency_subscription_cli` | ✅ PASS | <= 1.00x | 0.15x | Resource ratio in tier subscription_cli: 0.15x versus baseline Strategy 1 (threshold <= 1.00x) |
| `peak_resident_context_subscription_cli` | ✅ PASS | 1.00 | 0.30 | Peak resident context ratio in tier subscription_cli: 0.30x (threshold <= 1.00x) |
| `delegation_floor_task-arch-01:frontier_api` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-arch-01:frontier_api |
| `delegation_floor_task-arch-01:local_small` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-arch-01:local_small |
| `delegation_floor_task-arch-01:subscription_cli` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-arch-01:subscription_cli |
| `delegation_floor_task-arch-02:frontier_api` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-arch-02:frontier_api |
| `delegation_floor_task-arch-02:local_small` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-arch-02:local_small |
| `delegation_floor_task-arch-02:subscription_cli` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-arch-02:subscription_cli |
| `delegation_floor_task-impl-01:frontier_api` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-01:frontier_api |
| `delegation_floor_task-impl-01:local_small` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-01:local_small |
| `delegation_floor_task-impl-01:subscription_cli` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-01:subscription_cli |
| `delegation_floor_task-impl-02:frontier_api` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-02:frontier_api |
| `delegation_floor_task-impl-02:local_small` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-02:local_small |
| `delegation_floor_task-impl-02:subscription_cli` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-02:subscription_cli |
| `delegation_floor_task-impl-03:frontier_api` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-03:frontier_api |
| `delegation_floor_task-impl-03:local_small` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-03:local_small |
| `delegation_floor_task-impl-03:subscription_cli` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-03:subscription_cli |
| `delegation_floor_task-impl-04:frontier_api` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-04:frontier_api |
| `delegation_floor_task-impl-04:local_small` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-04:local_small |
| `delegation_floor_task-impl-04:subscription_cli` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-impl-04:subscription_cli |
| `delegation_floor_task-nav-01:frontier_api` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-nav-01:frontier_api |
| `delegation_floor_task-nav-01:local_small` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-nav-01:local_small |
| `delegation_floor_task-nav-01:subscription_cli` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-nav-01:subscription_cli |
| `delegation_floor_task-nav-02:frontier_api` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-nav-02:frontier_api |
| `delegation_floor_task-nav-02:local_small` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-nav-02:local_small |
| `delegation_floor_task-nav-02:subscription_cli` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-nav-02:subscription_cli |
| `delegation_floor_task-rev-01:frontier_api` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-rev-01:frontier_api |
| `delegation_floor_task-rev-01:local_small` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-rev-01:local_small |
| `delegation_floor_task-rev-01:subscription_cli` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-rev-01:subscription_cli |
| `delegation_floor_task-rev-02:frontier_api` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-rev-02:frontier_api |
| `delegation_floor_task-rev-02:local_small` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-rev-02:local_small |
| `delegation_floor_task-rev-02:subscription_cli` | ✅ PASS | 0 falsifications | Satisfied | Delegation floor hypothesis satisfied for task-rev-02:subscription_cli |

## Aggregated Telemetry Breakdown

# Empirical Benchmark Telemetry Report

- Generated: 2026-10-05T08:18:17Z
- Total Snapshots: 264

## Aggregated Performance by Strategy & Capability

| Strategy | Capability | Runs | Accepted | Acceptance Rate | Defect Catch Rate | Cache Hit Ratio | Avg Peak Tokens | Avg Input Tokens | Resource / Accepted | Uncertain? |
|---|---|---|---|---|---|---|---|---|---|---|
| compacted | frontier_api | 22 | 10 | 45.5% | 83.3% | 77.8% | 409 | 409 | 900.0 tokens | No |
| compacted | local_small | 22 | 10 | 45.5% | 83.3% | 77.8% | 409 | 409 | 900.0 tokens | No |
| compacted | subscription_cli | 22 | 10 | 45.5% | 83.3% | 77.8% | 409 | 409 | 900.0 tokens | No |
| full_history | frontier_api | 22 | 10 | 45.5% | 83.3% | 8.3% | 1091 | 1091 | 2400.0 tokens | No |
| full_history | local_small | 22 | 10 | 45.5% | 83.3% | 8.3% | 1091 | 1091 | 2400.0 tokens | No |
| full_history | subscription_cli | 22 | 10 | 45.5% | 83.3% | 8.3% | 1091 | 1091 | 2400.0 tokens | No |
| hybrid_4layer | frontier_api | 22 | 20 | 90.9% | 83.3% | 77.8% | 327 | 327 | 360.0 tokens | **YES (provisional)** |
| hybrid_4layer | local_small | 22 | 20 | 90.9% | 83.3% | 77.8% | 327 | 327 | 360.0 tokens | **YES (provisional)** |
| hybrid_4layer | subscription_cli | 22 | 20 | 90.9% | 83.3% | 77.8% | 327 | 327 | 360.0 tokens | **YES (provisional)** |
| snippet_pool | frontier_api | 22 | 10 | 45.5% | 83.3% | 77.8% | 409 | 409 | 900.0 tokens | No |
| snippet_pool | local_small | 22 | 10 | 45.5% | 83.3% | 77.8% | 409 | 409 | 900.0 tokens | No |
| snippet_pool | subscription_cli | 22 | 10 | 45.5% | 83.3% | 77.8% | 409 | 409 | 900.0 tokens | No |

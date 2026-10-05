---
name: devcadence-principal
description: Work as the DevCadence Principal through the devcadence MCP tools - semantic project state, progressive evidence, proposals versus accepted decisions, and escalation.
---

# DevCadence Principal

1. Call `project_state` first and note its `state_revision`.
2. Ask for evidence progressively (`request_evidence`, `investigate`) and only for the current question.
3. Use `create_work_package`, `record_decision` and `reject` to propose or record; these do not accept anything. Acceptance is decided server-side.
4. On a typed refusal or stale-state error, re-read `project_state` and escalate to the human when asked to.

This skill does not enforce authorization or source isolation.

# Principal Host Integrations

## Scope

DevCadence is driven through a **PrincipalHost** integration, not by one mandatory editor or model vendor.

The initial supported host scope is deliberately narrow:

1. **Antigravity** — reference/bootstrap host.
2. **Cursor** — first-class AI IDE host.
3. **Visual Studio Code** — first-class general-purpose editor/agent host.

Additional hosts are future integration candidates, not bootstrap commitments.

## 1. Host versus cognition endpoint

A principal host is the user-facing environment in which the principal operates.

A cognition endpoint is the model/runtime/CLI/API that provides reasoning.

These concepts may overlap but must not be conflated.

A product may also expose a machine-invocable agent CLI or SDK. That interface is modeled separately as a cognition **session driver/access channel**. Integrating an editor as a host must not make DevCadence's scheduler depend on that editor to reach unrelated Codex/Claude/Gemini/local endpoints, and a headless agent CLI remains usable even when its corresponding GUI host is not selected.

Examples:

```text
Antigravity host
  -> Gemini cognition
  -> DevCadence semantic MCP

Cursor host
  -> configured model/provider
  -> DevCadence semantic MCP

VS Code host
  -> configured agent/model
  -> DevCadence semantic MCP
```

Codex CLI, Claude Code, local Ollama, remote APIs and similar tools are normally cognition endpoints or worker/consultant integrations, not principal-host requirements.

## 2. PrincipalHost abstraction

Conceptual capabilities:

```yaml
id: cursor

capabilities:
  semantic_tool_transport:
    mcp: true
  persistent_instructions: true
  tool_permissions: true
  interactive_ui: true

integration:
  detectable: true
  configurable: true
  verifiable: true
```

A host adapter should eventually support operations conceptually equivalent to:

```text
detect()
version()
compatibility()
integrationStatus()

planInstall()
planIntegration()
applyApprovedPlan()
verifyIntegration()

launchOrOpen()
```

Host-specific filesystem/config formats belong in adapters/recipes, not the core domain model.

## 3. Blank-machine behavior

No supported principal host installed is a valid setup state.

The onboarding flow should:

1. report that no supported host was detected;
2. present Antigravity, Cursor and VS Code neutrally;
3. explain relevant compatibility/installation facts;
4. allow the user to select one;
5. create an installation/configuration plan;
6. request approval for mutating actions;
7. guide or automate installation where safe;
8. verify the installed version;
9. configure the DevCadence integration when available;
10. smoke-test the semantic connection.

The user may choose **Skip for now**.

DevCadence remains usable for deterministic/local worker capabilities that do not require a principal host.

## 4. Existing-host preference

When one or more supported hosts are already installed and compatible, setup should normally prefer reuse over installing another IDE.

Example:

```text
Detected:
  VS Code       installed, compatible
  Cursor        not installed
  Antigravity   not installed

Recommended:
  Configure existing VS Code

Alternatives:
  Install Antigravity
  Install Cursor
```

If multiple supported hosts exist, the user chooses. DevCadence should not silently replace the operator's preferred environment.

## 5. Initial host sequence

### Antigravity

Reference integration because it is the current development/bootstrap environment.

The first semantic MCP/principal-host path may be proven here.

### Cursor

First portability target. It should exercise the same semantic principal contract rather than introducing Cursor-specific domain semantics.

### Visual Studio Code

First-class supported host because of its broad developer ecosystem. It should consume the same host-neutral semantic interface.

All three are in the initial host support set. Implementation may be staged, but architecture and documentation must not claim Antigravity is required.

## 6. Host integration boundary

Principal hosts receive:

- compact ProjectState;
- semantic MCP operations;
- principal instructions/skills/rules;
- targeted evidence on request.

They should not require:

- direct write access to accepted source;
- generic unrestricted shell as the normal interface;
- raw database access;
- provider-specific core records.

Strict information-firewall deployments may keep the target repository outside the principal host workspace entirely.

## 7. Installation and configuration safety

Host installation/configuration follows the setup authority rules in [ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md](ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md).

In particular:

- detection is read-only;
- installation is explicit;
- host login/authentication uses supported vendor mechanisms;
- plugin/MCP configuration is planned and auditable;
- DevCadence does not scrape or copy unrelated host credentials;
- rollback/removal should be possible for DevCadence-owned configuration.

## 8. Verification

A host is not considered ready merely because its executable/application exists.

Verification should establish, where applicable:

- supported/compatible version;
- DevCadence integration installed/configured;
- semantic MCP endpoint launches/connects;
- expected principal instructions are available;
- a harmless semantic smoke operation succeeds;
- permissions match the configured security posture.

No repository source is required for the basic integration smoke test.

## 9. Future host integrations

Architecture should remain open to future hosts, but these are explicitly outside the initial support commitment.

Potential future directions include:

- Zed;
- Windsurf;
- other MCP-capable editors;
- standalone terminal principal surfaces;
- ACP-compatible/external-agent hosts;
- future IDEs with equivalent semantic tool/instruction support.

This section is a compatibility direction, not a promise to implement every listed host.

## 10. Product principle

The onboarding experience should communicate:

> DevCadence adapts to the development environment you already use. Antigravity, Cursor and VS Code are the initial first-class hosts; none is a mandatory architectural dependency.

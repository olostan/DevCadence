# Development Setup

## Status

The control-plane core (M1), the repository/worktree/process substrate (M2) and
environment intelligence with cognition routing (M3A) are implemented and merged.

Sections below describing hardware, runtime, coding-CLI and principal-host
**discovery** are therefore implemented behaviour, observable through
`devcadence environment inspect` and `devcadence cognition list`.

Sections describing guided **setup, remediation, installation and credential
references** remain intended M3B behaviour. M3B requires safe plain/JSON/basic-
terminal operation; the richer adaptive setup/explanation UX belongs to M3D
after portfolio semantics exist. Principal-host **integration** belongs to M5. No model integration is required
to build or test the control plane, and none of the implemented commands mutates
the machine.

## 1. Target environment

Supported bootstrap starts from an ordinary macOS or Linux machine and assumes optional AI software may be absent.

Reference profiles include:
- Apple Silicon with 48 GB unified memory for strong local-model use;
- modest 32 GB-class Linux systems where only small local models may be practical;
- machines with no local LLM at all, using policy-authorized remote cognition.

Core prerequisites remain basic engineering tooling such as Git and the supported Go toolchain.

Ollama, MLX-LM, Antigravity, Cursor, VS Code, consultant CLIs and provider credentials are optional capabilities discovered/configured by guided setup rather than unconditional prerequisites.

## 2. Clone

```bash
git clone https://github.com/olostan/DevCadence.git
cd DevCadence
```

## 3. Core tools

Recommended macOS tools:

```bash
brew install git go sqlite
```

Exact minimum Go version will be defined when the module is created.

Optional quality tools can be installed later through project scripts rather than requiring global setup.

## 4. Optional local model option A: Ollama

Install through the supported Ollama distribution/package for your platform. On Homebrew-based macOS environments:

```bash
brew install ollama
```

Start/verify according to the installed Ollama package.

Model selection is configuration. DevCadence should not hard-code one model name in architecture.

The bootstrap profile should target a strong coding model that fits with sufficient context headroom on the machine.

## 5. Optional local model option B: MLX-LM

MLX-LM requires Apple Silicon and Python.

A recommended isolated environment can use `uv`:

```bash
brew install uv
uv venv .venv-mlx
source .venv-mlx/bin/activate
uv pip install mlx-lm
```

DevCadence should communicate with MLX-LM through a controlled adapter/process boundary so the Go core does not absorb Python dependency semantics.

## 6. Keep machine headroom

Do not size local models based only on weight memory.

Leave memory for:
- macOS;
- IDE/Antigravity;
- DevCadence daemon;
- Git/worktrees;
- compiler/test processes;
- KV/context cache;
- reviewer model swapping.

The initial scheduler should prefer sequential strong-model use to unsafe memory saturation.

## 7. Go bootstrap

The module is `github.com/olostan/DevCadence`. Its `go` directive is 1.25.0,
which the SQLite driver requires; a newer toolchain is fetched automatically
by recent Go releases.

```bash
make verify        # go vet ./... && go test ./... && schema validation
make build         # bin/devcadence
```

The build needs no C toolchain: the SQLite driver is pure Go
([ADR-0002](adr/0002-control-plane-persistence.md)).

Binaries:
- `devcadence` — CLI/daemon (implemented in M1);
- `devcadence-mcp` — stdio semantic MCP adapter (M5, not yet present).

## 7A. Guided bootstrap (M3)

The intended normal onboarding path is not manual runtime installation.

### Implemented in M3A: read-only inspection

These commands exist today. All are read-only and none mutates the machine.

```bash
devcadence environment inspect                   # observed hardware/software facts
devcadence environment inspect --depth inventory # filesystem only; runs no command
devcadence environment inspect --json
devcadence cognition list                        # endpoints, health, assessment
devcadence cognition list --depth inventory      # filesystem only; runs no command
devcadence cognition probe <endpoint-id>         # verify one endpoint explicitly
devcadence cognition route --role implementer    # explainable routing decision
devcadence cognition route --source-exposure local_only --max-cost local_compute
```

`environment inspect`, `cognition list` and `cognition route` all refuse
`--depth inference`: an ordinary environment query must never load a model as a
side effect, and an inventory command must never fan inference out across every
endpoint it finds — several of those endpoints are authenticated coding CLIs that
bill the operator's subscription.

Inference is authorised one endpoint at a time, through
`cognition probe <endpoint-id>`. That command discovers and health-checks the
whole machine cheaply, then invokes exactly the endpoint named and no other. It
uses only models that already exist locally and never downloads one.

A discovered coding CLI is reported with `cost_class: unknown`. Finding `claude`
or `codex` on PATH says nothing about whether a subscription, a metered API key or
an enterprise account is being billed, and DevCadence will not read credentials
to guess. Routing treats `unknown` as dearer than every known class, so an
operator who wants a CLI preferred on cost declares its class explicitly.

### Intended in M3B: guided mutation

The remaining commands plan and apply changes, and do not exist yet.

Conceptual commands:

```bash
devcadence doctor
devcadence setup
devcadence setup --dry-run
devcadence setup verify
```

The setup engine starts by discovering the machine and existing software.

It then:
1. assesses accelerator/runtime candidates;
2. discovers supported cognition CLIs/providers and authentication readiness;
3. discovers supported principal hosts;
4. produces deterministic ResourceInventory/readiness evidence;
5. presents a structured remediation/install plan for concrete missing facts;
6. requests approval for mutating or privileged actions;
7. verifies actual inference acceleration/endpoint health;
8. performs lightweight capability benchmarks where useful.

M3B does not choose an "optimal" provider/model/role organization. That
multidimensional recommendation problem is intentionally deferred to M3D over
the M3C substrate.

No local model, principal host or consultant subscription is assumed.

See [ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md](ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md).

### M3B terminal contract

M3B should remain usable from local terminals, SSH and automation:
- plain text status and explicit confirmations where approval is required;
- `--json` for machine-readable output;
- basic-terminal/SSH fallbacks;
- no control sequences in non-interactive mode.

A richer Huh/Bubble Tea/Lip Gloss experience is deliberately deferred to M3D,
where it can explain real portfolio alternatives, budgets, privacy exposure,
fallbacks and workflow consequences instead of being built around incomplete
semantics.

## 7B. Cognition substrate and adaptive portfolio (M3C/M3D)

M3B gets the machine safely to a factual, usable state.

**M3C** defines how cognition resources are represented and invoked:
access/session capabilities, economic/budget state, policy, versioned portfolio
protocols, session drivers, and deterministic validation/activation.

**M3D** decides how to organize those resources: AI-assisted portfolio
alternatives, adaptive task topology, resource-change proposals, rollback, and
the richer explain/setup experience.

The intended user experience may still look like one simple setup flow:

```text
devcadence setup
  -> M3B: discover and safely remediate concrete resources
  -> M3C: represent sessions/economics/policy and validate candidate portfolios
  -> M3D: synthesize explainable alternatives with an eligible endpoint
  -> deterministically validate before activation
  -> present a diff, implications and rollback path
```

Adding a subscription, API authorization, GPU or local model later should
support incremental recommendation rather than reinstalling DevCadence. No user
is required to own local AI hardware or any particular commercial subscription.

## 8. Local data directories

Do not store runtime state inside target project source trees by default.

Proposed layout:

```text
~/.devcadence/
  config/
  state/
  artifacts/
  worktrees/
  models/
  logs/
```

Per-project configuration references repository paths and policies.

Settled by [ADR-0002](adr/0002-control-plane-persistence.md) §9: the
control-plane database is `$DEVCADENCE_HOME/state/control-plane.db`, with
`DEVCADENCE_HOME` defaulting to `~/.devcadence`. The path must be absolute.
`devcadence -db <path>` overrides it, which is what the test suite and
throwaway experiments use. A full XDG layout was not adopted: the macOS-first
target and the single `DEVCADENCE_HOME` indirection cover the need with one
variable.

## 9. Target repository registration

Future CLI shape:

```bash
devcadence project add \
  --id hearthmind \
  --repo ~/src/HearthMind
```

This syntax is illustrative until implemented.

## 10. Validation setup

Each target project should define safe validation profiles through DevCadence configuration rather than allowing a model to invent arbitrary shell commands every run.

Example conceptual config:

```yaml
validation:
  fast:
    - ["go", "test", "./internal/..."]
    - ["go", "vet", "./..."]
  full:
    - ["go", "test", "./..."]
    - ["staticcheck", "./..."]
```

Commands are argv arrays by default.

## 11. Principal host integration

The initial first-class principal hosts are:
- Antigravity (reference integration);
- Cursor;
- Visual Studio Code.

No host is assumed to already be installed.

Guided setup should prefer a compatible host the user already has. If none is installed, it should present the supported choices, help the user install/configure the selected host, and allow setup to be deferred.

See [PRINCIPAL_HOSTS.md](PRINCIPAL_HOSTS.md).

### Antigravity reference integration

The intended bootstrap topology is:

```mermaid
flowchart LR
    AG["Antigravity"]
    MCP["devcadence-mcp<br/>stdio"]
    D["DevCadence daemon"]
    Local["Ollama / MLX-LM"]
    Repo["Target repo"]

    AG <--> MCP
    MCP <--> D
    D <--> Local
    D <--> Repo
```

Antigravity should receive:
- the DevCadence principal Skill/Rules;
- semantic MCP tools;
- project design artifacts when useful.

The target source repository should not need to be Antigravity’s directly writable workspace in the strict information-firewall configuration.

The concrete configuration, strict principal-workspace topology, plugin packaging, permissions strategy, smoke test, and current Antigravity file locations are specified in [ANTIGRAVITY_INTEGRATION.md](ANTIGRAVITY_INTEGRATION.md).

A versioned plugin skeleton is already maintained under `integrations/antigravity/devcadence/`. It becomes directly usable once the `devcadence-mcp` binary is implemented and available on PATH.

## 12. External cognition and consultants

Consultants are optional.

Setup should detect usable existing cognition endpoints/authenticated CLIs before asking the user to create new credentials or subscriptions.

Examples may include:
- Codex CLI;
- Claude Code;
- Gemini/provider tooling;
- future supported consultant integrations;
- API-backed providers.

No particular consultant provider is required. Missing consultant capability should be reported as reduced cognitive diversity, not setup failure.

## 13. Development workflow

During M1–M3A, run:

```bash
go test ./...
go vet ./...
```

The suite requires no GPU, no local model runtime, no Python, no coding CLI, no
provider credentials and no network. Environment and cognition behaviour is
exercised against deterministic fixture machines
(`internal/environment/fixtures.go`), so a Linux/AMD machine and an Apple Silicon
machine are both covered whatever host runs the tests.

Additional linters/static analyzers should be introduced with version pinning/CI.

## 14. Documentation

GitHub renders Mermaid fenced blocks directly in Markdown, so no documentation build step is required for the repository's core diagrams.

Keep a textual explanation around diagrams so the docs remain usable if rendering fails and remain accessible.

## 15. First end-to-end bootstrap

The first real setup demo should begin from a blank or intentionally stripped environment:

1. run environment discovery/doctor;
2. choose or confirm a deployment profile;
3. configure any desired local runtime and verify the actual acceleration backend, or explicitly choose no-local-model operation;
4. discover/configure at least one implementation cognition endpoint;
5. discover an existing principal host or select/install one of Antigravity, Cursor or VS Code;
6. start DevCadence daemon;
7. register a small target repository;
8. start `devcadence-mcp`;
9. verify the principal-host semantic connection;
10. ask principal to inspect ProjectState;
11. issue a targeted investigation;
12. author a Work Package;
13. delegate to an implementation worker;
14. validate/review;
15. inspect ChangeReport;
16. accept or reject.

The same core flow should be demonstrable under strong-local and hybrid/no-local-model configurations.

This demo is a milestone test, not merely onboarding documentation.

## 16. Troubleshooting principles

If local inference fails:
- verify runtime health;
- verify model/context configuration;
- inspect memory pressure;
- use structured runtime diagnostics;
- do not silently route private code to an external provider unless policy allows.

If MCP fails:
- test daemon application service independently;
- test MCP adapter separately;
- keep semantic logic out of transport layer.

If a task fails:
- preserve Attempt/artifacts;
- do not “fix” the database state manually unless recovery tooling explicitly supports it.

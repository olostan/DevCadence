# ADR-0015: Declarative monorepo modules, scoped worktree execution, and reproducible state reduction

- **Status:** Accepted
- **Date:** 2026-09-22
- **Related:** ADR-0004 (canonical task state machine), ADR-0005 (deterministic project state identity), ADR-0007 (repository and worktree safety model), ADR-0012 (mandatory brownfield adoption baseline), DCI-010, DCI-030, DCI-033, DCI-052, DCI-053
- **Documents:** docs/ARCHITECTURE.md, docs/PROJECT_ADOPTION.md, docs/PROJECT_STATE.md, docs/PROTOCOLS.md, docs/SECURITY.md

## Context

Many real-world software systems exist as monorepos housing multiple heterogeneous components (e.g. a Go backend, a TypeScript/React web frontend, and a Flutter or native mobile app) within a single Git repository.

A flat, single-module assumption fails in such repositories:
1. **Working Directory & Tooling Isolation:** Validation checks (e.g., `go test ./...`, `npm test`, `flutter analyze`) cannot execute at the repository root when package manifests (`go.mod`, `package.json`, `pubspec.yaml`) reside in distinct subdirectories.
2. **Context & Token Bloat:** Giving local execution agents unrestricted repository-wide views forces them to ingest irrelevant files from other tech stacks, rapidly exhausting constrained context budgets.
3. **Reproducibility vs. Ambient Filesystem Discovery:** Discovering modules on-the-fly during `ProjectState` reduction violates DCI-052 and DCI-053; state must be reproducible from journaled events and canonical configuration alone.
4. **Manifests vs. Authoritative Boundaries:** Manifest files (`package.json`) exist in nested directories (`packages/foo`, `submodules/bar`); equating every manifest to an independent buildable module creates false component boundaries.
5. **Path Traversal & Symlink Escapes:** Sub-path configurations must strictly validate directory containment within the Git worktree root to prevent sandbox escapes.

## Decision

### 1. Separation of Discovered Manifests from Authoritative Modules

- **Discovered Manifests:** Scanners observe package manifests (`go.mod`, `package.json`, `pubspec.yaml`, `Cargo.toml`, `pyproject.toml`) and record `ObservedManifest` evidence records. Discovery does not mutate canonical state or unilaterally define module boundaries.
- **Authoritative Catalog:** The operator or adoption proposal declares the authoritative module catalog in `.devcadence/project.yaml`.
- **Reproducible Journaling:** An approved module configuration is journaled as `ModuleCatalogRecorded` carrying the canonical configuration digest and explicit module definitions:
  ```go
  type ModuleDefinition struct {
      ID           string            `json:"id"`
      Path         string            `json:"path"` // Repository-relative, canonicalized
      ManifestPath string            `json:"manifest_path,omitempty"`
      Language     string            `json:"language"`
      Dependencies []string          `json:"dependencies,omitempty"` // Explicit module IDs
      Properties   map[string]string `json:"properties,omitempty"`
  }

  type ModuleCatalogRecorded struct {
      ConfigDigest string             `json:"config_digest"`
      RootModuleID string             `json:"root_module_id,omitempty"`
      Modules      []ModuleDefinition `json:"modules"`
  }
  ```
- During event reduction, `ProjectState` folds `ModuleCatalogRecorded` directly into the projection without consulting the ambient filesystem.

### 2. Explicit Scoping and Path Containment

- **No Session-Global "Active Module":** Tool invocations and validation checks carry an explicit `Scope` consisting of `ProjectID`, `WorktreeID`, and an optional `ModuleID`.
- **Path Resolution Rules:**
  - `Module.Path` and `ManifestPath` are repository-relative paths, cleaned of `.` and `..`.
  - Resolution enforces containment within the worktree root: symlinks are evaluated with `filepath.EvalSymlinks`, and paths resolving outside the worktree boundary are rejected with `CategorySecurityViolation`.
  - Setting `process.Spec.Dir` is scoped to `filepath.Join(worktreeRoot, module.Path)`. Setting a working directory is recognized as path scoping, not a security sandbox.

### 3. Cross-Module Impact & Search Boundaries

- By default, search and reconnaissance tools (`grep_search`, `find_files`) scope results to the target module directory to protect context.
- Callers may explicitly request repository-root scope or include named module dependencies. Agents may not claim complete impact coverage from module-local results alone.

## Consequences

- **Positive:** Polyglot monorepos are first-class citizens; commands execute in their native manifest roots; context windows are protected from cross-stack noise; module state is fully deterministic and auditable.
- **Negative:** Project adoption requires confirmation of module boundaries if multiple manifests are discovered.

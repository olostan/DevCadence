# DevCadence Go Engineering Practices

- **Status:** Proposed engineering conventions for review and adoption
- **Date:** 2026-10-09
- **Relationship:** Supplements [ENGINEERING_STANDARDS.md](../ENGINEERING_STANDARDS.md); accepted ADRs, invariants and security policy take precedence. Motivated by idiomatic Go guidance and the DevCadence flight-recorder design; not a claim of official Google-internal policy.

## API and dependency design

1. **Accept interfaces, return concrete implementations** is a useful default, **not an absolute law**. Constructors normally return a concrete pointer, allowing users to discover functionality. Functions consume narrow interfaces when multiple implementations or test substitution are actually valuable; do not create an interface for every concrete type by default.
2. **Define consumer-owned interfaces where used**, usually in the package consuming the behavior. A provider can expose concrete types and methods without depending on its consumers. Shared interfaces belong in a stable lower-level package only when genuinely shared as a first-class contract (for example a defined runtime/adapter boundary). Do not duplicate an existing canonical protocol interface merely for style.
3. Keep interfaces **small and behavioral** (often one to three methods). Name methods for the capability they express. Compose interfaces rather than create kitchen-sink `Manager` interfaces.
4. Prefer **explicit constructor/options injection** for I/O, clocks, IDs, configuration, tracing, policy and external effects. Existing `taskexec.Options` and `mcpadapter.Config` are precedents. Do not introduce a DI framework or global mutable singleton simply for convenience.
5. Use **`context.Context` for cancellation/deadlines and immutable request-scoped identifiers**, not a general dependency-injection container, configuration bag or mutable operation object. Use private typed keys, preserve cancellation semantics, and keep operation handles outside context. Propagate a derived context to child work; detached tasks require explicit task-owned lifetime. Never serialize a Go context to remote workers: transmit minimal typed trace/correlation identifiers.
6. Prefer explicit ownership and lifecycle (`Close`, cancellation, bounded goroutines). Constructors should validate required dependencies. Do not silently substitute insecure/no-op providers in security-sensitive flows.
7. Return errors with actionable context and stable categories where existing contracts require them; use `errors.Is/As` or typed errors for machine decisions. Do not convert unknown effect outcomes into success or safe retry.
8. Make interfaces mockable at **real boundaries**; use deterministic fakes for clock/IDs/transport and integration tests for persistence, Git and process behavior. Avoid abstractions invented solely to mock pure functions.

## Idiomatic implementation

- Favor simple structs/functions and explicit control flow over inheritance-like frameworks, reflection-heavy registries or elaborate generic abstractions.
- Document exported APIs and invariants; keep package imports acyclic and boundaries aligned with durable responsibilities.
- Keep secrets, raw source and mutable infrastructure objects out of contexts and broad logs; favor minimal typed metadata and artifact references.
- Use `gofmt`, `go vet`, race tests and repository health checks; preserve coverage and evidence gates.
- When a practical tradeoff conflicts with these preferences, explain it in the PR/EWP instead of introducing speculative layers.

## Tracing example

```go
// Consumer package owns this narrow dependency contract.
type OperationRecorder interface {
    Start(ctx context.Context, name string, meta StartMetadata) (context.Context, Operation, error)
}

// Concrete implementations are built at the composition root; only trace IDs
// are placed in the returned context. Operation is a local handle, not context baggage.
type Executor struct { recorder OperationRecorder }

func (e *Executor) Delegate(ctx context.Context) (err error) {
    ctx, op, err := e.recorder.Start(ctx, "task.delegate", StartMetadata{})
    if err != nil { return err }
    // Schematic only: the final API must distinguish panics from success.\n    defer func() { err = errors.Join(err, op.End(err)) }()
    return e.run(ctx)
}
```

This is schematic, not a committed API; END error/durability and error precedence require explicit EWP semantics. **Do not use this defer verbatim for crash reporting:** if `e.run` panics, `err` may still be nil and a defer can misleadingly emit `COMPLETED`. Implementations MUST recognize active panic unwinding and must not record a successful END. A safe option is to leave START unmatched and let offline export mark the operation unresolved; if panic capture is intentionally used, it must not silently swallow or turn the panic into success. Use `errors.Join` (Go 1.20+) or an equivalent typed combination if both the operation and terminal journal append fail, preserving the primary error; never hide a failed critical END append. Refer to accepted ADR-0026.

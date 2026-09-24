package setup

import (
	"context"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// LocalModelRuntimeAdapter is the sole extension point through which a local
// model-serving runtime (Ollama, MLX-LM, or any future one) participates in
// ensure_local_model operations and model_present conditions. Every runtime
// implements the same two methods with the same authority: this package
// must never special-case one runtime's name in applyOperation or
// EvaluateCondition (see INVARIANTS.md DCI-055 and docs/MODEL_RUNTIME.md) —
// all runtime-specific behavior lives behind this interface instead.
type LocalModelRuntimeAdapter interface {
	// Runtime returns the exact string this adapter answers for (e.g.
	// "ollama", "mlx"), matched against EnsureLocalModelParams.Runtime /
	// ModelPresentOperand.Runtime by ModelRuntimeRegistry.For.
	Runtime() string

	// EnsureModel performs the mutating pull/download for params and
	// reports whether it mutated the system, a human detail, the
	// subprocess result (nil if none ran), and an artifact reference for
	// captured output (nil if none captured or captureOutput is false).
	// verifiedExecutablePath, when the adapter's pull path runs a specific
	// binary, must be used verbatim (never re-resolved from PATH) — same
	// PATH-substitution defense as every other operation applier
	// (ADR-0014 §1).
	EnsureModel(ctx context.Context, deps applierDeps, params *protocol.EnsureLocalModelParams, captureOutput bool, verifiedExecutablePath string) (mutated bool, detail string, procResult *process.Result, artifact *protocol.ArtifactRef, err error)

	// ModelPresent evaluates a model_present condition against the live
	// system: a read-only check, never a mutation.
	ModelPresent(ctx context.Context, deps EvaluatorDeps, op *protocol.ModelPresentOperand) (bool, string, error)
}

// ModelRuntimeRegistry dispatches ensure_local_model operations and
// model_present conditions to the adapter named by their Runtime field.
// There is no default or preferred runtime: an unregistered Runtime value
// is always an error, for every runtime equally, including "ollama".
type ModelRuntimeRegistry struct {
	adapters map[string]LocalModelRuntimeAdapter
}

// NewModelRuntimeRegistry builds a registry from adapters, keyed by each
// adapter's own Runtime(). A later adapter with a Runtime() already seen
// overwrites the earlier one silently — callers are expected to pass each
// runtime at most once.
func NewModelRuntimeRegistry(adapters ...LocalModelRuntimeAdapter) *ModelRuntimeRegistry {
	reg := &ModelRuntimeRegistry{adapters: make(map[string]LocalModelRuntimeAdapter, len(adapters))}
	for _, a := range adapters {
		reg.adapters[a.Runtime()] = a
	}
	return reg
}

// For looks up the adapter registered for runtime.
func (r *ModelRuntimeRegistry) For(runtime string) (LocalModelRuntimeAdapter, bool) {
	if r == nil {
		return nil, false
	}
	a, ok := r.adapters[runtime]
	return a, ok
}

// DefaultModelRuntimeAdapters returns the production adapter set: Ollama
// and MLX as equal peers, neither privileged over the other.
func DefaultModelRuntimeAdapters() []LocalModelRuntimeAdapter {
	return []LocalModelRuntimeAdapter{OllamaAdapter{}, MLXAdapter{}}
}

// unsupportedRuntimeError reports a Runtime value with no registered
// adapter, used identically by both the operation and condition dispatch
// paths so neither one silently favors a particular runtime name.
func unsupportedRuntimeError(op string, runtime string) error {
	return errs.New(errs.CategoryInvalidArgument, "%s: unsupported model runtime %q (no adapter registered)", op, runtime)
}

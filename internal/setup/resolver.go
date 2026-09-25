package setup

import (
	"context"
	"sync"

	"github.com/olostan/DevCadence/internal/errs"
)

// ModelResolver resolves a runtime model reference into an immutable supply-chain record
// prior to plan generation.
type ModelResolver interface {
	ResolveModel(ctx context.Context, runtime, modelRef string) (ResolvedModel, error)
}

// verifyResolvedIdentity fails closed unless resolved's own Runtime/ModelRef
// exactly match what was requested. ModelResolver is a caller-pluggable
// interface, and the fail-closed pre-resolution contract cannot depend on
// every implementation being bug-free: a resolver that returns a
// perfectly valid, immutable record for a different runtime or model would
// otherwise let the approved plan silently target the wrong artifact
// (independent-review follow-up on WP-M3B-6, FIX_NOW 2).
func verifyResolvedIdentity(resolved ResolvedModel, wantRuntime, wantModelRef string) error {
	if resolved.Runtime != wantRuntime {
		return errs.New(errs.CategoryInvalidArgument,
			"setup planner: model resolver returned runtime %q for requested runtime %q; refusing to plan a mismatched identity", resolved.Runtime, wantRuntime)
	}
	if resolved.ModelRef != wantModelRef {
		return errs.New(errs.CategoryInvalidArgument,
			"setup planner: model resolver returned model_ref %q for requested model_ref %q; refusing to plan a mismatched identity", resolved.ModelRef, wantModelRef)
	}
	return nil
}

// ResolvedModel contains verified supply-chain metadata for an install-class model operation.
type ResolvedModel struct {
	Runtime           string `json:"runtime"`
	ModelRef          string `json:"model_ref"`
	ResolvedRevision  string `json:"resolved_revision"`
	ExpectedSizeBytes int64  `json:"expected_size_bytes"`
	AllowedSource     string `json:"allowed_source"`
	LicenseReference  string `json:"license_reference"`
}

// Validate checks that all required supply-chain fields are present and immutable.
func (m ResolvedModel) Validate() error {
	const kind = "ResolvedModel"
	if m.Runtime == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: runtime is required", kind)
	}
	if m.ModelRef == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: model_ref is required", kind)
	}
	if m.ResolvedRevision == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: resolved_revision is required", kind)
	}
	// Revision-immutability format is runtime-specific: each runtime pins
	// an immutable snapshot with a different identifier shape (a Hugging
	// Face commit hash for MLX, a content-addressed manifest digest for
	// Ollama), so this cannot be a single shared check. A resolver result
	// for a runtime this package does not yet know how to verify fails
	// closed rather than being silently accepted as "non-empty is enough"
	// (independent-review follow-up on WP-M3B-6, FIX_NOW 1).
	switch m.Runtime {
	case "mlx":
		if !isImmutableHFRevision(m.ResolvedRevision) {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: mlx revision %q is not an immutable 40-character commit hash", kind, m.ResolvedRevision)
		}
	case "ollama":
		if !isImmutableOllamaRevision(m.ResolvedRevision) {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: ollama revision %q is not an immutable sha256 manifest digest", kind, m.ResolvedRevision)
		}
	default:
		return errs.New(errs.CategoryInvalidArgument,
			"%s: runtime %q has no known immutable-revision verification rule; refusing to treat %q as pinned", kind, m.Runtime, m.ResolvedRevision)
	}
	if m.ExpectedSizeBytes <= 0 {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: expected_size_bytes must be positive, got %d", kind, m.ExpectedSizeBytes)
	}
	if m.AllowedSource == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: allowed_source is required", kind)
	}
	if m.LicenseReference == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: license_reference is required", kind)
	}
	return nil
}

// CatalogModelResolver resolves models against an in-memory catalog of verified models.
type CatalogModelResolver struct {
	mu      sync.RWMutex
	entries map[string]ResolvedModel
}

func catalogKey(runtime, modelRef string) string {
	return runtime + ":" + modelRef
}

// NewCatalogModelResolver creates a CatalogModelResolver pre-populated with standard default models.
func NewCatalogModelResolver() *CatalogModelResolver {
	c := &CatalogModelResolver{
		entries: make(map[string]ResolvedModel),
	}
	// Register default Ollama model
	_ = c.Register(ResolvedModel{
		Runtime:           "ollama",
		ModelRef:          DefaultOllamaModelTag,
		ResolvedRevision:  DefaultOllamaDigest,
		ExpectedSizeBytes: DefaultOllamaSizeBytes,
		AllowedSource:     DefaultOllamaRegistryHost,
		LicenseReference:  DefaultOllamaLicense,
	})
	// Register default MLX model
	_ = c.Register(ResolvedModel{
		Runtime:           "mlx",
		ModelRef:          DefaultMLXModelRef,
		ResolvedRevision:  DefaultMLXRevision,
		ExpectedSizeBytes: DefaultMLXSizeBytes,
		AllowedSource:     DefaultMLXSource,
		LicenseReference:  DefaultMLXLicense,
	})
	return c
}

// Register adds or updates a verified model in the catalog.
func (c *CatalogModelResolver) Register(m ResolvedModel) error {
	if err := m.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[catalogKey(m.Runtime, m.ModelRef)] = m
	return nil
}

// ResolveModel looks up the model in the catalog. If not found or if the context is cancelled,
// it returns an error.
func (c *CatalogModelResolver) ResolveModel(ctx context.Context, runtime, modelRef string) (ResolvedModel, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return ResolvedModel{}, errs.Wrap(errs.CategoryInternal, err, "model resolution cancelled")
		}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	key := catalogKey(runtime, modelRef)
	m, ok := c.entries[key]
	if !ok {
		return ResolvedModel{}, errs.New(errs.CategoryNotFound,
			"model resolver: no verified supply-chain record found for runtime %q model %q", runtime, modelRef)
	}
	return m, nil
}

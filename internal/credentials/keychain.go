package credentials

import (
	"context"

	"github.com/olostan/DevCadence/internal/errs"
)

// KeychainChecker checks whether an opaque keychain locator exists in the platform credential store.
// It never decrypts, reads, or persists secret material during readiness checks.
type KeychainChecker interface {
	CheckPresence(ctx context.Context, locator string) (bool, error)
}

// MapKeychainChecker is an in-memory KeychainChecker for testing.
type MapKeychainChecker struct {
	items map[string]bool
}

// NewMapKeychainChecker creates an in-memory KeychainChecker with the specified presence states.
func NewMapKeychainChecker(items map[string]bool) *MapKeychainChecker {
	copied := make(map[string]bool, len(items))
	for k, v := range items {
		copied[k] = v
	}
	return &MapKeychainChecker{items: copied}
}

// CheckPresence reports whether locator is recorded as present.
func (m *MapKeychainChecker) CheckPresence(_ context.Context, locator string) (bool, error) {
	if m == nil || m.items == nil {
		return false, nil
	}
	return m.items[locator], nil
}

// UnsupportedKeychainChecker is a cross-platform fallback for a platform
// with no keychain backend implemented.
type UnsupportedKeychainChecker struct{}

// CheckPresence always returns an error: an unsupported backend means the
// presence question could not be asked at all, which is evidence the
// check is inconclusive (Manager maps this to AuthStatusUnavailable) — it
// must never be read as "the item is absent" (AuthStatusUnauthenticated),
// which would claim to have checked something this checker never actually
// looked at (DCI-104's "clean degradation" is failing closed to
// unavailable, not silently reporting a negative result).
func (UnsupportedKeychainChecker) CheckPresence(_ context.Context, _ string) (bool, error) {
	return false, errs.New(errs.CategoryUnsupported, "credentials: no keychain backend is available on this platform")
}

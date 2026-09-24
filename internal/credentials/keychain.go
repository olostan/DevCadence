package credentials

import "context"

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

// UnsupportedKeychainChecker is a cross-platform fallback that safely reports unavailable.
type UnsupportedKeychainChecker struct{}

// CheckPresence reports false with nil error (clean degradation, DCI-104).
func (UnsupportedKeychainChecker) CheckPresence(_ context.Context, _ string) (bool, error) {
	return false, nil
}

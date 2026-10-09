package sessionclients

import "net/http"

// ValidateLoopbackURL reports whether rawURL is an http loopback address
// without credentials (the only shape this package will ever dial).
func ValidateLoopbackURL(rawURL string) error { return validateLoopbackURL(rawURL) }

// NewLoopbackHTTPClient returns the strictly loopback-only, proxy-free,
// redirect-refusing HTTP client used by this package, for callers that must
// probe the same runtime (for example the self-host preflight check).
func NewLoopbackHTTPClient() *http.Client { return newLoopbackHTTPClient() }

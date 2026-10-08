package sessionclients

import (
	"github.com/olostan/DevCadence/internal/clock"
)

// DefaultLoopbackBaseURL is Ollama's documented default listen address.
const DefaultLoopbackBaseURL = "http://127.0.0.1:11434"

// MaxRequestBytes bounds request payloads (1 MiB).
const MaxRequestBytes = 1 << 20 // 1 MiB

// MaxResponseBytes bounds response payloads (4 MiB).
const MaxResponseBytes = 4 << 20 // 4 MiB

// Options configures Composition.
type Options struct {
	// LoopbackBaseURLs maps endpoint id -> loopback URL (e.g. http://127.0.0.1:<port> or http://[::1]:<port>).
	LoopbackBaseURLs map[string]string
	// Clock supplies wall or deterministic time. If nil, clock.System() is used.
	Clock clock.Clock
}

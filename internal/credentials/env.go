package credentials

import "os"

// EnvReader abstracts environment variable lookup for presence-only inspection.
// It MUST NOT expose or retain the variable's value, length, prefix, suffix,
// hash, or entropy (ADR-0014 §6).
type EnvReader interface {
	IsPresent(name string) bool
}

// OsEnvReader implements EnvReader using os.LookupEnv.
type OsEnvReader struct{}

// IsPresent reports whether name exists and contains non-empty content.
// The value is never saved, retained, serialized, or logged.
func (OsEnvReader) IsPresent(name string) bool {
	val, ok := os.LookupEnv(name)
	return ok && val != ""
}

// MapEnvReader is an in-memory EnvReader for testing.
type MapEnvReader struct {
	vars map[string]string
}

// NewMapEnvReader creates an in-memory EnvReader with the given values.
func NewMapEnvReader(vars map[string]string) *MapEnvReader {
	copied := make(map[string]string, len(vars))
	for k, v := range vars {
		copied[k] = v
	}
	return &MapEnvReader{vars: copied}
}

// IsPresent reports whether name exists and is non-empty in the map.
func (m *MapEnvReader) IsPresent(name string) bool {
	if m == nil || m.vars == nil {
		return false
	}
	val, ok := m.vars[name]
	return ok && val != ""
}

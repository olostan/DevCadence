// Package devcadience is the module root. It exists solely to publish the
// canonical JSON Schema files under schemas/ to Go packages that need them,
// so that the schemas have exactly one source of truth in the repository
// (ENGINEERING_STANDARDS.md §23: do not mirror generated documentation).
package devcadience

import "embed"

// SchemaFS exposes the normative JSON Schema documents described in
// schemas/README.md. Readers must treat these files as normative at
// integration boundaries (ENGINEERING_STANDARDS.md §5).
//
//go:embed schemas/*.json
var SchemaFS embed.FS

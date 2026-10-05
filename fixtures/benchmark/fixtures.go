package benchmarkfixtures

import "embed"

// FS embeds canonical benchmark task fixtures, defect patches, and seed defect definitions.
//
//go:embed tasks/*.json patches/*.patch seed_defects/*.json
var FS embed.FS

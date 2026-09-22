package protocol

import (
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// ModuleDefinition represents an authoritative, buildable component or package
// boundary within a monorepo (ADR-0015).
type ModuleDefinition struct {
	ID           string            `json:"id"`
	Path         string            `json:"path"` // Repository-relative, canonicalized
	ManifestPath string            `json:"manifest_path,omitempty"`
	Language     string            `json:"language"`
	Dependencies []string          `json:"dependencies,omitempty"` // Explicit module IDs
	Properties   map[string]string `json:"properties,omitempty"`
}

// Validate ensures the module definition is well-formed and contained.
func (m ModuleDefinition) Validate() error {
	if m.ID == "" {
		return errs.New(errs.CategoryInvalidArgument, "module: id is required")
	}
	if m.Path == "" {
		return errs.New(errs.CategoryInvalidArgument, "module %q: path is required", m.ID)
	}
	// Path must be repository-relative and not escape via ..
	cleaned := cleanRelativePath(m.Path)
	if cleaned == "" || strings.HasPrefix(cleaned, "..") || strings.HasPrefix(cleaned, "/") {
		return errs.New(errs.CategoryInvalidArgument, "module %q: path %q must be relative and contained within repository", m.ID, m.Path)
	}
	if m.ManifestPath != "" {
		cleanedManifest := cleanRelativePath(m.ManifestPath)
		if cleanedManifest == "" || strings.HasPrefix(cleanedManifest, "..") || strings.HasPrefix(cleanedManifest, "/") {
			return errs.New(errs.CategoryInvalidArgument, "module %q: manifest_path %q must be relative and contained within repository", m.ID, m.ManifestPath)
		}
	}
	return nil
}

// cleanRelativePath normalizes path separators and cleans redundant segments.
func cleanRelativePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	return p
}

// Package mcpadapter is the thin Model Context Protocol adapter of the
// semantic Principal facade (WP-M5-2). It strictly decodes tool arguments,
// binds the trusted caller from the local launch binding, dispatches to the
// shared facade and normalises the response. It owns no semantics: the same
// authorised call yields the same result through Go and through MCP (I1).
package mcpadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
)

// BindingVersion is the only binding schema this build reads.
const BindingVersion = "1.0"

// bindingFile is the protected launch binding. It carries no credentials and
// no registration paths; the launch composition owns those.
type bindingFile struct {
	BindingVersion   string   `json:"binding_version"`
	ProjectID        string   `json:"project_id"`
	PrincipalID      string   `json:"principal_id"`
	AllowedActions   []string `json:"allowed_actions"`
	PolicyRef        string   `json:"policy_ref"`
	MaxEvidenceBytes int      `json:"max_evidence_bytes"`
	MaxSnippetLines  int      `json:"max_snippet_lines"`
	SourceDepth      string   `json:"source_depth"`
}

// Binding is a loaded, validated launch binding. It is immutable for the life
// of the process: Digest pins the exact bytes it was read from.
type Binding struct {
	Caller principal.CallerContext
	Digest string
	Path   string
}

// ParseBinding strictly decodes and validates binding bytes. Unknown fields and
// unknown grants are rejected; an empty grant set is allowed.
func ParseBinding(document []byte) (principal.CallerContext, error) {
	var f bindingFile
	if err := principal.DecodeStrict(document, &f); err != nil {
		return principal.CallerContext{}, errs.Wrap(errs.CategoryInvalidArgument, err, "binding is not valid")
	}
	if f.BindingVersion != BindingVersion {
		return principal.CallerContext{}, errs.New(errs.CategorySchemaVersionUnsupported,
			"binding_version %q is not supported; this build reads %q", f.BindingVersion, BindingVersion)
	}
	if f.AllowedActions == nil {
		return principal.CallerContext{}, errs.New(errs.CategoryInvalidArgument,
			"allowed_actions is required; use an empty array for no grants")
	}
	seen := map[string]bool{}
	for _, action := range f.AllowedActions {
		if !facade.KnownAction(action) {
			return principal.CallerContext{}, errs.New(errs.CategoryInvalidArgument,
				"allowed_actions entry %q is not a canonical tool name", action)
		}
		if seen[action] {
			return principal.CallerContext{}, errs.New(errs.CategoryInvalidArgument,
				"allowed_actions entry %q is repeated", action)
		}
		seen[action] = true
	}
	if f.PolicyRef == "" {
		return principal.CallerContext{}, errs.New(errs.CategoryInvalidArgument, "policy_ref is required")
	}
	switch f.SourceDepth {
	case facade.DepthSummary, facade.DepthSymbol, facade.DepthSnippet:
	default:
		return principal.CallerContext{}, errs.New(errs.CategoryInvalidArgument,
			"source_depth must be summary, symbol or snippet")
	}
	if f.MaxEvidenceBytes < 1 || f.MaxEvidenceBytes > facade.MaxEvidenceBytes {
		return principal.CallerContext{}, errs.New(errs.CategoryInvalidArgument,
			"max_evidence_bytes must be 1 to %d", facade.MaxEvidenceBytes)
	}
	if f.MaxSnippetLines < 1 || f.MaxSnippetLines > facade.MaxSnippetLines {
		return principal.CallerContext{}, errs.New(errs.CategoryInvalidArgument,
			"max_snippet_lines must be 1 to %d", facade.MaxSnippetLines)
	}
	caller := principal.CallerContext{
		PrincipalID: f.PrincipalID, ProjectID: f.ProjectID, AllowedActions: append([]string{}, f.AllowedActions...),
		PolicyRef: f.PolicyRef, MaxEvidenceBytes: f.MaxEvidenceBytes, MaxSnippetLines: f.MaxSnippetLines,
		SourceDepth: f.SourceDepth,
	}
	if err := caller.Validate(); err != nil {
		return principal.CallerContext{}, err
	}
	return caller, nil
}

func digestOf(document []byte) string {
	sum := sha256.Sum256(document)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// LoadBinding reads the protected binding file at an absolute path. The file
// and its parent directories must pass the ownership and permission checks of
// checkProtected (POSIX). This protects against unrelated users, not against
// agent processes running as the same OS user.
func LoadBinding(path string) (*Binding, error) {
	if err := checkProtected(path); err != nil {
		return nil, err
	}
	document, err := os.ReadFile(path)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "read principal binding")
	}
	caller, err := ParseBinding(document)
	if err != nil {
		return nil, err
	}
	return &Binding{Caller: caller, Digest: digestOf(document), Path: path}, nil
}

// BindingPolicy is the facade's PolicyResolver for one launch. It pins the
// binding and its policy identity for the process lifetime: a changed file
// never silently grants anything, and drift denies every later action until
// the operator relaunches the process.
type BindingPolicy struct {
	binding *Binding
}

// NewBindingPolicy pins a loaded binding.
func NewBindingPolicy(b *Binding) *BindingPolicy { return &BindingPolicy{binding: b} }

// Check implements facade.PolicyResolver.
func (p *BindingPolicy) Check(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, tool string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.check(caller, meta, tool)
}

func sameCaller(a, b principal.CallerContext) bool {
	if a.PrincipalID != b.PrincipalID || a.ProjectID != b.ProjectID || a.PolicyRef != b.PolicyRef ||
		a.MaxEvidenceBytes != b.MaxEvidenceBytes || a.MaxSnippetLines != b.MaxSnippetLines ||
		a.SourceDepth != b.SourceDepth || len(a.AllowedActions) != len(b.AllowedActions) {
		return false
	}
	for i := range a.AllowedActions {
		if a.AllowedActions[i] != b.AllowedActions[i] {
			return false
		}
	}
	return true
}

func (p *BindingPolicy) check(caller principal.CallerContext, meta principal.CallMeta, tool string) error {
	if !sameCaller(caller, p.binding.Caller) {
		return errs.New(errs.CategoryPolicyDenied, "caller is not the launch-bound caller")
	}
	if meta.ProjectID != p.binding.Caller.ProjectID || !p.binding.Caller.Allows(tool) {
		return errs.New(errs.CategoryPolicyDenied, "action is not granted by the launch binding")
	}
	current, err := os.ReadFile(p.binding.Path)
	if err != nil || digestOf(current) != p.binding.Digest {
		return errs.New(errs.CategoryPolicyDenied,
			"the launch binding changed after launch; relaunch the process to apply it")
	}
	return nil
}

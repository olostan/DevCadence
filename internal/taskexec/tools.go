package taskexec

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/tools"
)

const (
	maxWriteBytes        = 256 * 1024
	maxFilesPerAttempt   = 64
	writeFileDescription = "Write the FULL content of a file at a relative path inside the declared write scope. Use apply_patch instead to change part of an existing file."
)

// writeGuard holds the checks and the per-attempt file budget shared by every
// file-mutating worker tool (write_file, apply_patch).
type writeGuard struct {
	scope      *tools.Scope
	writeScope []string
	mu         sync.Mutex
	files      map[string]bool
}

func newWriteGuard(scope *tools.Scope, writeScope []string) *writeGuard {
	return &writeGuard{scope: scope, writeScope: writeScope, files: make(map[string]bool)}
}

// resolve validates a model-supplied relative path for mutation and returns the
// cleaned relative path and its resolved absolute path. It refuses absolute
// paths, traversal, .git, paths outside the write scope, containment escapes,
// and any path whose resolution traverses a symlink (including symlinked parent
// directories, even ones that stay inside the worktree, since those would
// bypass the lexical write-scope check).
func (g *writeGuard) resolve(tool, rel string) (clean, resolved string, err error) {
	if filepath.IsAbs(rel) {
		return "", "", errs.New(errs.CategoryPolicyDenied, "%s: relative path required, got absolute %q", tool, rel)
	}
	clean = filepath.Clean(rel)
	if clean == "." || clean == "" || strings.HasPrefix(clean, "..") {
		return "", "", errs.New(errs.CategoryPolicyDenied, "%s: invalid path %q", tool, rel)
	}
	if isGitPath(clean) {
		return "", "", errs.New(errs.CategoryPolicyDenied, "%s: modifying .git directory is forbidden", tool)
	}
	if !compiler.IsPathAuthorized(clean, g.writeScope) {
		return "", "", errs.New(errs.CategoryPolicyDenied, "%s: path %q is outside declared write scope", tool, clean)
	}
	resolved, err = g.scope.ResolvePath(clean)
	if err != nil {
		return "", "", errs.Wrap(errs.CategoryPolicyDenied, err, "%s: path containment violation", tool)
	}
	if err := refuseSymlinkedPath(g.scope, clean, resolved); err != nil {
		return "", "", errs.Wrap(errs.CategoryPolicyDenied, err, "%s: refusing to use symlinked path %q", tool, clean)
	}
	if fi, lerr := os.Lstat(resolved); lerr == nil && (fi.Mode()&os.ModeSymlink != 0) {
		return "", "", errs.New(errs.CategoryPolicyDenied, "%s: refusing to write through symlink %q", tool, clean)
	}
	return clean, resolved, nil
}

// reserve counts clean against the per-attempt file limit.
func (g *writeGuard) reserve(tool, clean string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.files[clean] && len(g.files) >= maxFilesPerAttempt {
		return errs.New(errs.CategoryPolicyDenied, "%s: attempt exceeded maximum of %d files modified", tool, maxFilesPerAttempt)
	}
	g.files[clean] = true
	return nil
}

// available reports whether clean can be written without exceeding the file
// budget, without consuming a slot.
func (g *writeGuard) available(tool, clean string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.files[clean] && len(g.files) >= maxFilesPerAttempt {
		return errs.New(errs.CategoryPolicyDenied, "%s: attempt exceeded maximum of %d files modified", tool, maxFilesPerAttempt)
	}
	return nil
}

func isGitPath(clean string) bool {
	return clean == ".git" || strings.HasPrefix(clean, ".git/") || strings.Contains(clean, "/.git/") || strings.HasSuffix(clean, "/.git")
}

// refuseSymlinkedPath fails when any existing component of the worktree-relative
// path is a symlink (checked component by component, so it also covers
// not-yet-existing targets below a symlinked directory).
func refuseSymlinkedPath(scope *tools.Scope, clean, resolved string) error {
	root, err := filepath.EvalSymlinks(scope.WorktreePath)
	if err != nil {
		return err
	}
	cur := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return errs.New(errs.CategoryPolicyDenied, "component %q is a symlink", part)
		}
	}
	return nil
}

func setupWorkerTools(mediator *drivers.ScopedToolMediator, scope *tools.Scope, runner *process.Runner, writeScope []string) []drivers.ToolDefinition {
	return setupWorkerToolsWith(mediator, scope, runner, writeScope, workerToolConfig{})
}

// workerToolConfig carries the optional run_command policy. The zero value is
// strict mode: run_command is neither declared nor executable.
type workerToolConfig struct {
	Mode        ExecutionMode
	Audit       *commandAudit
	Logger      *slog.Logger
	ScratchHome string // per-attempt HOME for run_command (never the host HOME)
}

func setupWorkerToolsWith(mediator *drivers.ScopedToolMediator, scope *tools.Scope, runner *process.Runner, writeScope []string, cfg workerToolConfig) []drivers.ToolDefinition {
	guard := newWriteGuard(scope, writeScope)

	// 1. read_file
	readFileDef := drivers.ToolDefinition{
		Name:           "read_file",
		Description:    "Read file contents within the worktree.",
		PathParameters: []string{"path"},
	}
	mediator.RegisterToolDefinition(readFileDef)
	mediator.RegisterHandler("read_file", func(ctx context.Context, args json.RawMessage) (string, error) {
		var p struct {
			Path            string `json:"path"`
			StartLine       int    `json:"start_line"`
			EndLine         int    `json:"end_line"`
			ShowLineNumbers bool   `json:"show_line_numbers"`
			MaxBytes        int64  `json:"max_bytes"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", errs.Wrap(errs.CategoryInvalidArgument, err, "invalid read_file arguments")
		}
		res, err := tools.ReadFile(tools.ReadFileOptions{
			Scope:           *scope,
			Path:            p.Path,
			StartLine:       p.StartLine,
			EndLine:         p.EndLine,
			ShowLineNumbers: p.ShowLineNumbers,
			MaxBytes:        p.MaxBytes,
		})
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(res)
		if err != nil {
			return "", err
		}
		return string(b), nil
	})

	// 2. grep
	grepDef := drivers.ToolDefinition{
		Name:           "grep",
		Description:    "Search repository contents within the worktree.",
		PathParameters: []string{"path"},
	}
	mediator.RegisterToolDefinition(grepDef)
	mediator.RegisterHandler("grep", func(ctx context.Context, args json.RawMessage) (string, error) {
		var p struct {
			Query      string         `json:"query"`
			Path       string         `json:"path"`
			Mode       tools.GrepMode `json:"mode"`
			MaxResults int            `json:"max_results"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", errs.Wrap(errs.CategoryInvalidArgument, err, "invalid grep arguments")
		}
		res, err := tools.GrepSearch(ctx, tools.GrepOptions{
			Runner:     runner,
			Scope:      *scope,
			Query:      p.Query,
			Path:       p.Path,
			Mode:       p.Mode,
			MaxResults: p.MaxResults,
		})
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(res)
		if err != nil {
			return "", err
		}
		return string(b), nil
	})

	// 3. symbols
	symbolsDef := drivers.ToolDefinition{
		Name:           "symbols",
		Description:    "Search syntactic Go symbols within the worktree.",
		PathParameters: []string{"path"},
	}
	mediator.RegisterToolDefinition(symbolsDef)
	mediator.RegisterHandler("symbols", func(ctx context.Context, args json.RawMessage) (string, error) {
		var p struct {
			Query      string `json:"query"`
			Path       string `json:"path"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", errs.Wrap(errs.CategoryInvalidArgument, err, "invalid symbols arguments")
		}
		res, err := tools.FindSymbol(ctx, tools.FindSymbolOptions{
			Scope:  *scope,
			Symbol: p.Query,
			Path:   p.Path,
		})
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(res)
		if err != nil {
			return "", err
		}
		return string(b), nil
	})

	// 4. write_file
	writeFileDef := drivers.ToolDefinition{
		Name:           "write_file",
		Description:    writeFileDescription,
		MutatesFiles:   true,
		PathParameters: []string{"path"},
	}
	mediator.RegisterToolDefinition(writeFileDef)
	mediator.RegisterHandler("write_file", func(ctx context.Context, args json.RawMessage) (string, error) {
		if !utf8.Valid(args) {
			return "", errs.New(errs.CategoryInvalidArgument, "write_file: content/arguments are not valid UTF-8")
		}
		var p struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", errs.Wrap(errs.CategoryInvalidArgument, err, "invalid write_file arguments")
		}

		clean, resolved, err := guard.resolve("write_file", p.Path)
		if err != nil {
			return "", err
		}

		contentBytes := []byte(p.Content)
		if len(contentBytes) > maxWriteBytes {
			return "", errs.New(errs.CategoryPolicyDenied, "write_file: file %q size %d exceeds 256 KiB cap", clean, len(contentBytes))
		}
		if !utf8.Valid(contentBytes) {
			return "", errs.New(errs.CategoryInvalidArgument, "write_file: content for %q is not valid UTF-8", clean)
		}

		if err := guard.reserve("write_file", clean); err != nil {
			return "", err
		}

		if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
			return "", errs.Wrap(errs.CategoryInternal, err, "write_file: failed creating parent directory")
		}

		if err := os.WriteFile(resolved, contentBytes, 0644); err != nil {
			return "", errs.Wrap(errs.CategoryInternal, err, "write_file: failed writing file")
		}

		return "ok", nil
	})

	toolDefs := []drivers.ToolDefinition{readFileDef, grepDef, symbolsDef, writeFileDef}
	toolDefs = append(toolDefs, registerApplyPatch(mediator, guard))
	if def, ok := registerRunCommand(mediator, scope, runner, cfg); ok {
		toolDefs = append(toolDefs, def)
	}
	mediator.SetDeclaredTools(toolDefs)
	return toolDefs
}

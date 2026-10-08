package taskexec

import (
	"context"
	"encoding/json"
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

func setupWorkerTools(mediator *drivers.ScopedToolMediator, scope *tools.Scope, runner *process.Runner, writeScope []string) []drivers.ToolDefinition {
	var writtenMu sync.Mutex
	writtenFiles := make(map[string]bool)

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
		Description:    "Write full file content to a path within declared write scope in the worktree.",
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

		if filepath.IsAbs(p.Path) {
			return "", errs.New(errs.CategoryPolicyDenied, "write_file: relative path required, got absolute %q", p.Path)
		}
		clean := filepath.Clean(p.Path)
		if clean == "." || clean == "" || strings.HasPrefix(clean, "..") {
			return "", errs.New(errs.CategoryPolicyDenied, "write_file: invalid path %q", p.Path)
		}
		if clean == ".git" || strings.HasPrefix(clean, ".git/") || strings.Contains(clean, "/.git/") || strings.HasSuffix(clean, "/.git") {
			return "", errs.New(errs.CategoryPolicyDenied, "write_file: modifying .git directory is forbidden")
		}

		if !compiler.IsPathAuthorized(clean, writeScope) {
			return "", errs.New(errs.CategoryPolicyDenied, "write_file: path %q is outside declared write scope", clean)
		}

		resolved, err := scope.ResolvePath(clean)
		if err != nil {
			return "", errs.Wrap(errs.CategoryPolicyDenied, err, "write_file: path containment violation")
		}

		if fi, err := os.Lstat(resolved); err == nil && (fi.Mode()&os.ModeSymlink != 0) {
			return "", errs.New(errs.CategoryPolicyDenied, "write_file: refusing to write through symlink %q", clean)
		}

		contentBytes := []byte(p.Content)
		if len(contentBytes) > 256*1024 {
			return "", errs.New(errs.CategoryPolicyDenied, "write_file: file %q size %d exceeds 256 KiB cap", clean, len(contentBytes))
		}
		if !utf8.Valid(contentBytes) {
			return "", errs.New(errs.CategoryInvalidArgument, "write_file: content for %q is not valid UTF-8", clean)
		}

		writtenMu.Lock()
		if !writtenFiles[clean] && len(writtenFiles) >= 64 {
			writtenMu.Unlock()
			return "", errs.New(errs.CategoryPolicyDenied, "write_file: attempt exceeded maximum of 64 files modified")
		}
		writtenFiles[clean] = true
		writtenMu.Unlock()

		if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
			return "", errs.Wrap(errs.CategoryInternal, err, "write_file: failed creating parent directory")
		}

		if err := os.WriteFile(resolved, contentBytes, 0644); err != nil {
			return "", errs.Wrap(errs.CategoryInternal, err, "write_file: failed writing file")
		}

		return "ok", nil
	})

	toolDefs := []drivers.ToolDefinition{readFileDef, grepDef, symbolsDef, writeFileDef}
	mediator.SetDeclaredTools(toolDefs)
	return toolDefs
}

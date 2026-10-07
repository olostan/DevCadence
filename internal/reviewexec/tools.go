package reviewexec

import (
	"context"
	"encoding/json"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/tools"
)

func setupReviewerTools(mediator *drivers.ScopedToolMediator, scope *tools.Scope, runner *process.Runner) []drivers.ToolDefinition {
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

	return []drivers.ToolDefinition{readFileDef, grepDef, symbolsDef}
}

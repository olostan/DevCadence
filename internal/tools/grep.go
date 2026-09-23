package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
)

// Default limits for grep operations.
const (
	DefaultGrepMaxResults = 20
	DefaultGrepTimeout    = 15 * time.Second
	DefaultGrepMaxOutput  = 4 << 20 // 4 MiB
)

// GrepMode represents the output mode of grep_search (ADR-0016).
type GrepMode string

const (
	GrepModeMatches   GrepMode = "matches"
	GrepModeFilesOnly GrepMode = "files_only"
	GrepModeCount     GrepMode = "count"
)

// GrepOptions configures bounded repository search.
type GrepOptions struct {
	Runner     *process.Runner
	Artifacts  *artifacts.Store
	Scope      Scope
	Query      string
	Path       string   // Relative to scope (file or directory)
	IsRegex    bool
	Mode       GrepMode // "matches" (default), "files_only", "count"
	MaxResults int      // Default 20
	Timeout    time.Duration
}

// GrepMatch represents a single matching line.
type GrepMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// GrepResult holds bounded search matches or counts.
type GrepResult struct {
	Query           string      `json:"query"`
	Mode            GrepMode    `json:"mode"`
	Matches         []GrepMatch `json:"matches,omitempty"`
	Files           []string    `json:"files,omitempty"`
	Count           int         `json:"count,omitempty"`
	ReturnedCount   int         `json:"returned_count"`
	TotalCount      int         `json:"total_count"`
	TotalCountExact bool        `json:"total_count_exact"`
	HasMore         bool        `json:"has_more"`
	ContentRef      string      `json:"content_ref,omitempty"`
}

// GrepSearch executes a bounded search across the scoped worktree using ripgrep,
// git grep, or pure Go fallback.
func GrepSearch(ctx context.Context, opts GrepOptions) (GrepResult, error) {
	if opts.Query == "" {
		return GrepResult{}, errs.New(errs.CategoryInvalidArgument, "grep_search: query is required")
	}

	mode := opts.Mode
	if mode == "" {
		mode = GrepModeMatches
	}

	if opts.IsRegex {
		if _, err := regexp.Compile(opts.Query); err != nil {
			return GrepResult{}, errs.Wrap(errs.CategoryInvalidArgument, err, "grep_search: invalid regex %q", opts.Query)
		}
	}

	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = DefaultGrepMaxResults
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultGrepTimeout
	}

	targetDir, err := opts.Scope.ResolvePath(opts.Path)
	if err != nil {
		return GrepResult{}, err
	}

	resolvedWorktree, err := filepath.EvalSymlinks(opts.Scope.WorktreePath)
	if err != nil {
		return GrepResult{}, errs.Wrap(errs.CategoryInvalidArgument, err, "grep_search: resolve worktree %q", opts.Scope.WorktreePath)
	}

	relTarget, err := filepath.Rel(resolvedWorktree, targetDir)
	if err != nil {
		return GrepResult{}, errs.Wrap(errs.CategoryInternal, err, "grep_search: rel target")
	}
	if relTarget == "." {
		relTarget = ""
	}

	runner := opts.Runner
	if runner == nil {
		runner = process.NewRunner()
	}

	projectID := opts.Scope.ProjectID
	if projectID == "" {
		projectID = "default"
	}

	// 1. Try ripgrep if available
	if hasRipgrep() {
		res, err := runRipgrep(ctx, runner, resolvedWorktree, relTarget, opts, timeout)
		if err == nil {
			return formatGrepResult(ctx, res, mode, opts.Query, maxResults, opts.Artifacts, projectID)
		}
	}

	// 2. Try git grep if in git repository
	if isGitRepo(resolvedWorktree) {
		res, err := runGitGrep(ctx, runner, resolvedWorktree, relTarget, opts, timeout)
		if err == nil {
			return formatGrepResult(ctx, res, mode, opts.Query, maxResults, opts.Artifacts, projectID)
		}
	}

	// 3. Fallback to pure Go search
	raw, truncated, err := fallbackGoGrep(ctx, resolvedWorktree, targetDir, opts)
	if err != nil {
		return GrepResult{}, err
	}

	res := rawGrepOutput{
		stdout:    raw,
		truncated: truncated,
	}
	return formatGrepResult(ctx, res, mode, opts.Query, maxResults, opts.Artifacts, projectID)
}

type rawGrepOutput struct {
	stdout    []byte
	truncated bool
}

func hasRipgrep() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}

func isGitRepo(dir string) bool {
	gitDir := filepath.Join(dir, ".git")
	_, err := os.Stat(gitDir)
	return err == nil
}

func runRipgrep(ctx context.Context, runner *process.Runner, worktreeRoot, relTarget string, opts GrepOptions, timeout time.Duration) (rawGrepOutput, error) {
	args := []string{"--color=never"}
	if !opts.IsRegex {
		args = append(args, "-F")
	}

	switch opts.Mode {
	case GrepModeFilesOnly:
		args = append(args, "-l")
	case GrepModeCount:
		args = append(args, "-c")
	default:
		args = append(args, "--no-heading", "--line-number")
	}

	args = append(args, "-e", opts.Query)
	if relTarget != "" {
		args = append(args, "--", relTarget)
	}

	spec := process.Spec{
		Executable:     "rg",
		Args:           args,
		Dir:            worktreeRoot,
		Env:            process.BaseEnv(),
		Timeout:        timeout,
		MaxStdoutBytes: DefaultGrepMaxOutput,
	}

	res, err := runner.Run(ctx, spec)
	if err != nil {
		return rawGrepOutput{}, err
	}
	if res.Status != process.StatusCompleted {
		return rawGrepOutput{}, errs.New(errs.CategoryInternal, "grep_search: ripgrep terminated with %v", res.Status)
	}
	// Exit code 0 means matches found, 1 means no matches found. Both are valid.
	if res.ExitCode != 0 && res.ExitCode != 1 {
		return rawGrepOutput{}, errs.New(errs.CategoryInternal, "grep_search: ripgrep exited with %d: %s", res.ExitCode, string(res.Stderr))
	}

	return rawGrepOutput{
		stdout:    res.Stdout,
		truncated: res.StdoutTruncated,
	}, nil
}

func runGitGrep(ctx context.Context, runner *process.Runner, worktreeRoot, relTarget string, opts GrepOptions, timeout time.Duration) (rawGrepOutput, error) {
	args := []string{"grep", "--untracked", "--no-color"}
	if !opts.IsRegex {
		args = append(args, "-F")
	}

	switch opts.Mode {
	case GrepModeFilesOnly:
		args = append(args, "-l")
	case GrepModeCount:
		args = append(args, "-c")
	default:
		args = append(args, "-n")
	}

	args = append(args, "-e", opts.Query)
	if relTarget != "" {
		args = append(args, "--", relTarget)
	}

	spec := process.Spec{
		Executable:     "git",
		Args:           args,
		Dir:            worktreeRoot,
		Env:            process.BaseEnv(),
		Timeout:        timeout,
		MaxStdoutBytes: DefaultGrepMaxOutput,
	}

	res, err := runner.Run(ctx, spec)
	if err != nil {
		return rawGrepOutput{}, err
	}
	if res.Status != process.StatusCompleted {
		return rawGrepOutput{}, errs.New(errs.CategoryInternal, "grep_search: git grep terminated with %v", res.Status)
	}
	// Exit code 0 means matches, 1 means no matches
	if res.ExitCode != 0 && res.ExitCode != 1 {
		return rawGrepOutput{}, errs.New(errs.CategoryInternal, "grep_search: git grep exited with %d: %s", res.ExitCode, string(res.Stderr))
	}

	return rawGrepOutput{
		stdout:    res.Stdout,
		truncated: res.StdoutTruncated,
	}, nil
}

func fallbackGoGrep(ctx context.Context, worktreeRoot, targetPath string, opts GrepOptions) ([]byte, bool, error) {
	var re *regexp.Regexp
	var err error
	if opts.IsRegex {
		re, err = regexp.Compile(opts.Query)
		if err != nil {
			return nil, false, err
		}
	}

	var buf bytes.Buffer
	truncated := false

	stat, err := os.Stat(targetPath)
	if err != nil {
		return nil, false, errs.Wrap(errs.CategoryNotFound, err, "grep_search: target %q", targetPath)
	}

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(worktreeRoot, path)
		if err != nil {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		fileMatches := 0
		scanner := bufio.NewScanner(file)
		lineNum := 0

		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			matched := false
			if re != nil {
				matched = re.MatchString(line)
			} else {
				matched = strings.Contains(line, opts.Query)
			}

			if matched {
				fileMatches++
				if opts.Mode == GrepModeFilesOnly {
					buf.WriteString(rel + "\n")
					break
				}
				if opts.Mode == "" || opts.Mode == GrepModeMatches {
					buf.WriteString(fmt.Sprintf("%s:%d:%s\n", rel, lineNum, line))
					if buf.Len() >= DefaultGrepMaxOutput {
						truncated = true
						return io.EOF // Stop early
					}
				}
			}
		}

		if opts.Mode == GrepModeCount && fileMatches > 0 {
			buf.WriteString(fmt.Sprintf("%s:%d\n", rel, fileMatches))
		}

		return nil
	}

	if stat.IsDir() {
		err = filepath.WalkDir(targetPath, walkFn)
	} else {
		err = walkFn(targetPath, fs.FileInfoToDirEntry(stat), nil)
	}

	if err != nil && err != io.EOF {
		return nil, false, errs.Wrap(errs.CategoryInternal, err, "grep_search: walk %q", targetPath)
	}

	return buf.Bytes(), truncated, nil
}

func formatGrepResult(ctx context.Context, raw rawGrepOutput, mode GrepMode, query string, maxResults int, store *artifacts.Store, projectID string) (GrepResult, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw.stdout))
	var lines []string
	for scanner.Scan() {
		t := scanner.Text()
		if t != "" {
			lines = append(lines, t)
		}
	}

	switch mode {
	case GrepModeFilesOnly:
		total := len(lines)
		hasMore := total > maxResults || raw.truncated
		returned := lines
		if len(returned) > maxResults {
			returned = returned[:maxResults]
		}

		var contentRef string
		if hasMore && store != nil {
			var full bytes.Buffer
			for _, f := range lines {
				full.WriteString(f + "\n")
			}
			putRes, err := store.PutBytes(ctx, projectID, "evidence", "text/plain", full.Bytes(), 0)
			if err == nil {
				contentRef = putRes.Ref.Locator
			}
		}

		return GrepResult{
			Query:           query,
			Mode:            GrepModeFilesOnly,
			Files:           returned,
			ReturnedCount:   len(returned),
			TotalCount:      total,
			TotalCountExact: !raw.truncated,
			HasMore:         hasMore,
			ContentRef:      contentRef,
		}, nil

	case GrepModeCount:
		totalCount := 0
		for _, l := range lines {
			parts := strings.SplitN(l, ":", 2)
			if len(parts) == 2 {
				n, _ := strconv.Atoi(parts[1])
				totalCount += n
			}
		}

		return GrepResult{
			Query:           query,
			Mode:            GrepModeCount,
			Count:           totalCount,
			ReturnedCount:   1,
			TotalCount:      totalCount,
			TotalCountExact: !raw.truncated,
			HasMore:         raw.truncated,
		}, nil

	default: // GrepModeMatches
		var allMatches []GrepMatch
		for _, l := range lines {
			parts := strings.SplitN(l, ":", 3)
			if len(parts) == 3 {
				lineNum, _ := strconv.Atoi(parts[1])
				allMatches = append(allMatches, GrepMatch{
					Path:    parts[0],
					Line:    lineNum,
					Content: parts[2],
				})
			}
		}

		total := len(allMatches)
		hasMore := total > maxResults || raw.truncated
		returned := allMatches
		if len(returned) > maxResults {
			returned = returned[:maxResults]
		}

		var contentRef string
		if hasMore && store != nil {
			var full bytes.Buffer
			for _, m := range allMatches {
				full.WriteString(fmt.Sprintf("%s:%d:%s\n", m.Path, m.Line, m.Content))
			}
			putRes, err := store.PutBytes(ctx, projectID, "evidence", "text/plain", full.Bytes(), 0)
			if err == nil {
				contentRef = putRes.Ref.Locator
			}
		}

		return GrepResult{
			Query:           query,
			Mode:            GrepModeMatches,
			Matches:         returned,
			ReturnedCount:   len(returned),
			TotalCount:      total,
			TotalCountExact: !raw.truncated,
			HasMore:         hasMore,
			ContentRef:      contentRef,
		}, nil
	}
}

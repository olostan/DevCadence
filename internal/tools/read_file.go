package tools

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// DefaultReadMaxBytes bounds a single read operation to protect context.
const DefaultReadMaxBytes = 64 * 1024 // 64 KiB

// ReadFileOptions configures a bounded file read.
type ReadFileOptions struct {
	Scope           Scope
	Path            string
	StartLine       int  // 1-indexed, inclusive
	EndLine         int  // 1-indexed, inclusive; 0 means to end of file
	ShowLineNumbers bool // Default false to optimize tokens (ADR-0016)
	MaxBytes        int64
}

// ReadFileResult represents the bounded file content.
type ReadFileResult struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	TotalLines int    `json:"total_lines"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Truncated  bool   `json:"truncated"`
}

// ReadFile reads a file within scope, bounded by line range and byte caps.
func ReadFile(opts ReadFileOptions) (ReadFileResult, error) {
	if opts.Path == "" {
		return ReadFileResult{}, errs.New(errs.CategoryInvalidArgument, "read_file: path is required")
	}
	absPath, err := opts.Scope.ResolvePath(opts.Path)
	if err != nil {
		return ReadFileResult{}, err
	}

	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultReadMaxBytes
	}

	file, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ReadFileResult{}, errs.Wrap(errs.CategoryNotFound, err, "read_file: %q does not exist", opts.Path)
		}
		return ReadFileResult{}, errs.Wrap(errs.CategoryInternal, err, "read_file: open %q", opts.Path)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	start := opts.StartLine
	if start <= 0 {
		start = 1
	}
	end := opts.EndLine

	var sb strings.Builder
	var currentBytes int64
	truncated := false
	actualEnd := start - 1
	lineIdx := 0

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			lineIdx++
			trimmed := strings.TrimRight(line, "\r\n")

			if lineIdx >= start && (end <= 0 || lineIdx <= end) {
				if !truncated {
					var formatted string
					if opts.ShowLineNumbers {
						formatted = fmt.Sprintf("%d: %s\n", lineIdx, trimmed)
					} else {
						formatted = trimmed + "\n"
					}

					lineBytes := int64(len(formatted))
					if currentBytes+lineBytes > maxBytes {
						truncated = true
					} else {
						sb.WriteString(formatted)
						currentBytes += lineBytes
						actualEnd = lineIdx
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return ReadFileResult{}, errs.Wrap(errs.CategoryInternal, err, "read_file: read %q", opts.Path)
		}
	}

	totalLines := lineIdx
	if actualEnd < start && totalLines > 0 {
		actualEnd = start - 1
	}

	return ReadFileResult{
		Path:       opts.Path,
		Content:    sb.String(),
		TotalLines: totalLines,
		StartLine:  start,
		EndLine:    actualEnd,
		Truncated:  truncated,
	}, nil
}

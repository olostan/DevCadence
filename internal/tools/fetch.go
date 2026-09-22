package tools

import (
	"bufio"
	"io"
	"os"
	"strings"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
)

// Default limits for artifact pagination.
const (
	DefaultPageLimit      = 20
	DefaultFetchMaxBytes  = 64 * 1024 // 64 KiB response cap (Finding 9)
)

// FetchContentOptions configures universal artifact pagination (ADR-0016).
type FetchContentOptions struct {
	Artifacts  *artifacts.Store
	ProjectID  string // Required caller authorization scope (Finding 7)
	Scope      Scope  // Optional scope containing ProjectID
	ContentRef string
	Offset     int    // 1-indexed for lines, 0-indexed for bytes
	Limit      int    // Number of lines/bytes to fetch
	Unit       string // "lines" (default) or "bytes"
	MaxBytes   int64  // Response byte cap
}

// PagedContentResult represents a deterministic paginated view of an artifact.
type PagedContentResult struct {
	ContentRef      string `json:"content_ref"`
	Offset          int    `json:"offset"`
	ReturnedCount   int    `json:"returned_count"`
	TotalCount      int    `json:"total_count"`
	TotalCountExact bool   `json:"total_count_exact"`
	HasMore         bool   `json:"has_more"`
	NextOffset      int    `json:"next_offset,omitempty"`
	Content         string `json:"content"`
	Truncated       bool   `json:"truncated,omitempty"`
}

// FetchContent retrieves a slice of content from an immutable stored artifact.
func FetchContent(opts FetchContentOptions) (PagedContentResult, error) {
	if opts.Artifacts == nil {
		return PagedContentResult{}, errs.New(errs.CategoryInvalidArgument, "fetch_content: artifact store is required")
	}
	if opts.ContentRef == "" {
		return PagedContentResult{}, errs.New(errs.CategoryInvalidArgument, "fetch_content: content_ref is required")
	}

	projectID := opts.ProjectID
	if projectID == "" && opts.Scope.ProjectID != "" {
		projectID = opts.Scope.ProjectID
	}
	if projectID == "" {
		return PagedContentResult{}, errs.New(errs.CategoryInvalidArgument, "fetch_content: project_id authorization scope is required")
	}

	// Finding 7: Validate that caller's ProjectID matches locator's ProjectID
	parts := strings.SplitN(opts.ContentRef, ":", 3)
	if len(parts) == 3 && parts[0] == "artifact" {
		locatorProject := parts[1]
		if locatorProject != projectID {
			return PagedContentResult{}, errs.New(errs.CategoryPolicyDenied,
				"fetch_content: caller project %q is not authorized to access artifact of project %q",
				projectID, locatorProject)
		}
	}

	rc, err := opts.Artifacts.OpenLocator(opts.ContentRef)
	if err != nil {
		return PagedContentResult{}, errs.Wrap(errs.CategoryNotFound, err, "fetch_content: artifact %q", opts.ContentRef)
	}
	defer rc.Close()

	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultPageLimit
	}

	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultFetchMaxBytes
	}

	unit := strings.ToLower(opts.Unit)
	if unit == "bytes" {
		return fetchBytesFromReader(opts.ContentRef, rc, opts.Offset, limit, maxBytes)
	}
	return fetchLinesFromReader(opts.ContentRef, rc, opts.Offset, limit, maxBytes)
}

func fetchLinesFromReader(ref string, rc io.Reader, offset, limit int, maxBytes int64) (PagedContentResult, error) {
	start := offset
	if start <= 0 {
		start = 1
	}
	end := start + limit - 1

	reader := bufio.NewReader(rc)
	var selected []string
	var currentBytes int64
	truncated := false
	lineIdx := 0

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			lineIdx++
			// Trim trailing newline for line representation
			trimmed := strings.TrimRight(line, "\r\n")

			if lineIdx >= start && lineIdx <= end {
				lineBytes := int64(len(trimmed) + 1)
				if currentBytes+lineBytes > maxBytes && len(selected) > 0 {
					truncated = true
					// Response cap reached
				} else {
					selected = append(selected, trimmed)
					currentBytes += lineBytes
				}
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return PagedContentResult{}, errs.Wrap(errs.CategoryInternal, err, "fetch_content: read lines %q", ref)
		}
	}

	total := lineIdx
	returnedCount := len(selected)
	actualEnd := start + returnedCount - 1
	hasMore := actualEnd < total
	nextOffset := 0
	if hasMore {
		nextOffset = actualEnd + 1
	}

	return PagedContentResult{
		ContentRef:      ref,
		Offset:          start,
		ReturnedCount:   returnedCount,
		TotalCount:      total,
		TotalCountExact: true,
		HasMore:         hasMore,
		NextOffset:      nextOffset,
		Content:         strings.Join(selected, "\n"),
		Truncated:       truncated,
	}, nil
}

func fetchBytesFromReader(ref string, rc io.Reader, offset, limit int, maxBytes int64) (PagedContentResult, error) {
	start := offset
	if start < 0 {
		start = 0
	}

	// Check if rc is an *os.File for O(1) seek and size lookup
	var total int64
	if f, ok := rc.(*os.File); ok {
		if fi, err := f.Stat(); err == nil {
			total = fi.Size()
		}
		if _, err := f.Seek(int64(start), io.SeekStart); err != nil {
			return PagedContentResult{}, errs.Wrap(errs.CategoryInternal, err, "fetch_content: seek %q", ref)
		}
	} else {
		// Discard up to start bytes
		if start > 0 {
			if _, err := io.CopyN(io.Discard, rc, int64(start)); err != nil && err != io.EOF {
				return PagedContentResult{}, errs.Wrap(errs.CategoryInternal, err, "fetch_content: discard offset")
			}
		}
	}

	toRead := int64(limit)
	truncated := false
	if toRead > maxBytes {
		toRead = maxBytes
		truncated = true
	}

	buf := make([]byte, toRead)
	n, err := io.ReadFull(rc, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return PagedContentResult{}, errs.Wrap(errs.CategoryInternal, err, "fetch_content: read bytes %q", ref)
	}
	buf = buf[:n]

	// If total not yet known from stat, drain remaining to get exact total
	if total == 0 {
		remaining, err := io.Copy(io.Discard, rc)
		if err != nil {
			return PagedContentResult{}, errs.Wrap(errs.CategoryInternal, err, "fetch_content: drain total")
		}
		total = int64(start) + int64(n) + remaining
	}

	hasMore := int64(start)+int64(n) < total
	nextOffset := 0
	if hasMore {
		nextOffset = start + n
	}

	return PagedContentResult{
		ContentRef:      ref,
		Offset:          start,
		ReturnedCount:   n,
		TotalCount:      int(total),
		TotalCountExact: true,
		HasMore:         hasMore,
		NextOffset:      nextOffset,
		Content:         string(buf),
		Truncated:       truncated,
	}, nil
}

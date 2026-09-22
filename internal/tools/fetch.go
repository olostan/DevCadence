package tools

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
)

// DefaultPageLimit is the default page size when limit is unspecified.
const DefaultPageLimit = 20

// FetchContentOptions configures universal artifact pagination (ADR-0016).
type FetchContentOptions struct {
	Artifacts  *artifacts.Store
	ContentRef string
	Offset     int    // 1-indexed for lines, 0-indexed for bytes
	Limit      int    // Number of lines/bytes to fetch
	Unit       string // "lines" (default) or "bytes"
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
}

// FetchContent retrieves a slice of content from an immutable stored artifact.
func FetchContent(opts FetchContentOptions) (PagedContentResult, error) {
	if opts.Artifacts == nil {
		return PagedContentResult{}, errs.New(errs.CategoryInvalidArgument, "fetch_content: artifact store is required")
	}
	if opts.ContentRef == "" {
		return PagedContentResult{}, errs.New(errs.CategoryInvalidArgument, "fetch_content: content_ref is required")
	}

	data, err := opts.Artifacts.Get(opts.ContentRef)
	if err != nil {
		return PagedContentResult{}, errs.Wrap(errs.CategoryNotFound, err, "fetch_content: artifact %q", opts.ContentRef)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultPageLimit
	}

	unit := strings.ToLower(opts.Unit)
	if unit == "bytes" {
		return fetchBytes(opts.ContentRef, data, opts.Offset, limit)
	}
	return fetchLines(opts.ContentRef, data, opts.Offset, limit)
}

func fetchLines(ref string, data []byte, offset, limit int) (PagedContentResult, error) {
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	total := len(lines)
	start := offset
	if start <= 0 {
		start = 1
	}

	if start > total {
		return PagedContentResult{
			ContentRef:      ref,
			Offset:          start,
			ReturnedCount:   0,
			TotalCount:      total,
			TotalCountExact: true,
			HasMore:         false,
			Content:         "",
		}, nil
	}

	end := start + limit - 1
	if end > total {
		end = total
	}

	selected := lines[start-1 : end]
	hasMore := end < total
	nextOffset := 0
	if hasMore {
		nextOffset = end + 1
	}

	return PagedContentResult{
		ContentRef:      ref,
		Offset:          start,
		ReturnedCount:   len(selected),
		TotalCount:      total,
		TotalCountExact: true,
		HasMore:         hasMore,
		NextOffset:      nextOffset,
		Content:         strings.Join(selected, "\n"),
	}, nil
}

func fetchBytes(ref string, data []byte, offset, limit int) (PagedContentResult, error) {
	total := len(data)
	start := offset
	if start < 0 {
		start = 0
	}
	if start >= total {
		return PagedContentResult{
			ContentRef:      ref,
			Offset:          start,
			ReturnedCount:   0,
			TotalCount:      total,
			TotalCountExact: true,
			HasMore:         false,
			Content:         "",
		}, nil
	}

	end := start + limit
	if end > total {
		end = total
	}

	selected := data[start:end]
	hasMore := end < total
	nextOffset := 0
	if hasMore {
		nextOffset = end
	}

	return PagedContentResult{
		ContentRef:      ref,
		Offset:          start,
		ReturnedCount:   len(selected),
		TotalCount:      total,
		TotalCountExact: true,
		HasMore:         hasMore,
		NextOffset:      nextOffset,
		Content:         string(selected),
	}, nil
}

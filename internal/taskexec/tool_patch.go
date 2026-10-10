package taskexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
)

// Stable apply_patch error codes. Every error message has the form
// "apply_patch: <code>: <detail>" so models and tests can key on the code.
const (
	patchCodeStale      = "stale_context"
	patchCodeAmbiguous  = "ambiguous_context"
	patchCodeEmptyOld   = "empty_old_text"
	patchCodeOverlap    = "overlapping_edits"
	patchCodeNoEdits    = "no_edits"
	patchCodeTooMany    = "too_many_edits"
	patchCodeNotRegular = "not_regular_file"
	patchCodeBinary     = "binary_or_non_utf8"
	patchCodeTooLarge   = "too_large"
	patchCodeNotFound   = "file_not_found"
)

const (
	maxPatchEdits      = 64
	maxPatchDiffBytes  = 8 * 1024
	applyPatchToolName = "apply_patch"
)

const applyPatchDescription = "Edit an EXISTING file by exact text replacement. Give the relative path and a list of edits; " +
	"each edit has old_text (exact text to find, copied from read_file, may span many lines, must occur exactly once in the file) " +
	"and new_text (its replacement). Include enough surrounding lines in old_text to make it unique. " +
	"All edits are applied together or not at all. Errors: stale_context (old_text not found), ambiguous_context (found more than once), " +
	"overlapping_edits, empty_old_text. Use write_file to create a new file."

const applyPatchSchema = `{"type":"object","properties":{` +
	`"path":{"type":"string","description":"relative path of an existing file"},` +
	`"edits":{"type":"array","description":"edits applied atomically against the original file content","items":{"type":"object","properties":{` +
	`"old_text":{"type":"string","description":"exact existing text, must match exactly once"},` +
	`"new_text":{"type":"string","description":"replacement text (may be empty to delete)"}},` +
	`"required":["old_text","new_text"]}}},"required":["path","edits"]}`

type patchEdit struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

// patchResult is the JSON returned to the model on success.
type patchResult struct {
	Path          string `json:"path"`
	HunksApplied  int    `json:"hunks_applied"`
	BytesBefore   int    `json:"bytes_before"`
	BytesAfter    int    `json:"bytes_after"`
	Diff          string `json:"diff"`
	DiffTruncated bool   `json:"diff_truncated,omitempty"`
}

func patchErr(cat errs.Category, code, format string, a ...any) error {
	return errs.New(cat, "%s: %s: %s", applyPatchToolName, code, fmt.Sprintf(format, a...))
}

func registerApplyPatch(mediator *drivers.ScopedToolMediator, guard *writeGuard) drivers.ToolDefinition {
	def := drivers.ToolDefinition{
		Name:           applyPatchToolName,
		Description:    applyPatchDescription,
		Parameters:     json.RawMessage(applyPatchSchema),
		MutatesFiles:   true,
		PathParameters: []string{"path"},
	}
	mediator.RegisterToolDefinition(def)
	mediator.RegisterHandler(applyPatchToolName, func(ctx context.Context, args json.RawMessage) (string, error) {
		if !utf8.Valid(args) {
			return "", patchErr(errs.CategoryInvalidArgument, patchCodeBinary, "arguments are not valid UTF-8")
		}
		var p struct {
			Path  string      `json:"path"`
			Edits []patchEdit `json:"edits"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", errs.Wrap(errs.CategoryInvalidArgument, err, "invalid apply_patch arguments")
		}
		res, err := applyPatch(guard, p.Path, p.Edits)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(res)
		if err != nil {
			return "", err
		}
		return string(b), nil
	})
	return def
}

type patchSpan struct {
	idx        int
	start, end int
}

func applyPatch(guard *writeGuard, rel string, edits []patchEdit) (*patchResult, error) {
	if len(edits) == 0 {
		return nil, patchErr(errs.CategoryInvalidArgument, patchCodeNoEdits, "edits must contain at least one edit")
	}
	if len(edits) > maxPatchEdits {
		return nil, patchErr(errs.CategoryInvalidArgument, patchCodeTooMany, "%d edits exceed the maximum of %d", len(edits), maxPatchEdits)
	}
	clean, resolved, err := guard.resolve(applyPatchToolName, rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Lstat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, patchErr(errs.CategoryNotFound, patchCodeNotFound, "%q does not exist; use write_file to create it", clean)
		}
		return nil, errs.Wrap(errs.CategoryInternal, err, "apply_patch: stat %q", clean)
	}
	if !fi.Mode().IsRegular() {
		return nil, patchErr(errs.CategoryPolicyDenied, patchCodeNotRegular, "%q is not a regular file", clean)
	}
	if fi.Size() > maxWriteBytes {
		return nil, patchErr(errs.CategoryPolicyDenied, patchCodeTooLarge, "file %q size %d exceeds 256 KiB cap", clean, fi.Size())
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "apply_patch: read %q", clean)
	}
	if !utf8.Valid(raw) || strings.IndexByte(string(raw), 0) >= 0 {
		return nil, patchErr(errs.CategoryInvalidArgument, patchCodeBinary, "%q is binary or not valid UTF-8", clean)
	}
	orig := string(raw)

	spans := make([]patchSpan, 0, len(edits))
	for i, e := range edits {
		if e.OldText == "" {
			return nil, patchErr(errs.CategoryInvalidArgument, patchCodeEmptyOld, "edit %d has empty old_text", i)
		}
		if strings.IndexByte(e.NewText, 0) >= 0 {
			return nil, patchErr(errs.CategoryInvalidArgument, patchCodeBinary, "edit %d new_text contains a NUL byte", i)
		}
		first, count := -1, 0
		for off := 0; ; {
			j := strings.Index(orig[off:], e.OldText)
			if j < 0 {
				break
			}
			if first < 0 {
				first = off + j
			}
			count++
			off += j + 1
		}
		switch {
		case count == 0:
			return nil, patchErr(errs.CategoryInvalidArgument, patchCodeStale, "edit %d old_text not found in %q; re-read the file and copy the text exactly", i, clean)
		case count > 1:
			return nil, patchErr(errs.CategoryInvalidArgument, patchCodeAmbiguous, "edit %d old_text matches %d places in %q; add surrounding lines to make it unique", i, count, clean)
		}
		spans = append(spans, patchSpan{idx: i, start: first, end: first + len(e.OldText)})
	}
	sort.SliceStable(spans, func(a, b int) bool { return spans[a].start < spans[b].start })
	for k := 1; k < len(spans); k++ {
		if spans[k].start < spans[k-1].end {
			return nil, patchErr(errs.CategoryInvalidArgument, patchCodeOverlap, "edits %d and %d overlap in %q", spans[k-1].idx, spans[k].idx, clean)
		}
	}

	var out strings.Builder
	last := 0
	for _, sp := range spans {
		out.WriteString(orig[last:sp.start])
		out.WriteString(edits[sp.idx].NewText)
		last = sp.end
	}
	out.WriteString(orig[last:])
	result := out.String()
	if len(result) > maxWriteBytes {
		return nil, patchErr(errs.CategoryPolicyDenied, patchCodeTooLarge, "result for %q would be %d bytes, exceeding the 256 KiB cap", clean, len(result))
	}

	if err := guard.reserve(applyPatchToolName, clean); err != nil {
		return nil, err
	}
	if err := atomicReplace(resolved, []byte(result), fi.Mode().Perm()); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "apply_patch: failed writing %q", clean)
	}

	diff, truncated := buildPatchDiff(orig, spans, edits)
	return &patchResult{
		Path: clean, HunksApplied: len(spans), BytesBefore: len(orig), BytesAfter: len(result),
		Diff: diff, DiffTruncated: truncated,
	}, nil
}

// atomicReplace writes data to a temp file in the same directory and renames it
// over path, preserving perm. On any failure the temp file is removed.
func atomicReplace(path string, data []byte, perm os.FileMode) (retErr error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".apply_patch-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if retErr != nil {
			_ = os.Remove(name)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// buildPatchDiff renders a compact unified-style listing of the hunks (no
// shared context lines), bounded to maxPatchDiffBytes.
func buildPatchDiff(orig string, spans []patchSpan, edits []patchEdit) (string, bool) {
	var b strings.Builder
	for _, sp := range spans {
		line := 1 + strings.Count(orig[:sp.start], "\n")
		oldText := orig[sp.start:sp.end]
		newText := edits[sp.idx].NewText
		fmt.Fprintf(&b, "@@ line %d (edit %d) @@\n", line, sp.idx)
		writePrefixed(&b, "-", oldText)
		writePrefixed(&b, "+", newText)
		if b.Len() > maxPatchDiffBytes {
			break
		}
	}
	s := b.String()
	if len(s) > maxPatchDiffBytes {
		s = strings.ToValidUTF8(s[:maxPatchDiffBytes], "") + "\n... diff truncated ...\n"
		return s, true
	}
	return s, false
}

func writePrefixed(b *strings.Builder, prefix, text string) {
	if text == "" {
		return
	}
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		b.WriteString(prefix)
		b.WriteString(l)
		b.WriteByte('\n')
	}
}

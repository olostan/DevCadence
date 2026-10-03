package tests

import (
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

var (
	markdownLinkPattern = regexp.MustCompile("!?\\[[^\\]]*\\]\\(([^)]+)\\)")
	headingPattern      = regexp.MustCompile("^(#{1,6})[ \\t]+(.+?)[ \\t]*#*[ \\t]*$")
	dciPattern          = regexp.MustCompile("DCI-[0-9]{3}")
	adrPattern          = regexp.MustCompile("ADR-([0-9]{4})")
	fencePattern        = regexp.MustCompile("(?s)" + string(rune(96)) + string(rune(96)) + string(rune(96)) + ".*?" + string(rune(96)) + string(rune(96)) + string(rune(96)) + "|~~~.*?~~~")
)

func TestRepositoryMarkdownLinksAndAnchors(t *testing.T) {
	root := repositoryRoot(t)
	files := markdownFiles(t, root)

	for _, source := range files {
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
		text := fencePattern.ReplaceAllString(string(raw), "")

		for _, match := range markdownLinkPattern.FindAllStringSubmatch(text, -1) {
			destination := strings.TrimSpace(match[1])
			if destination == "" {
				continue
			}
			if i := strings.IndexAny(destination, " \\t"); i >= 0 {
				destination = destination[:i]
			}
			destination = strings.Trim(destination, "<>")
			if isExternalMarkdownDestination(destination) {
				continue
			}

			pathPart, fragment := splitMarkdownDestination(destination)
			target := source
			if pathPart != "" {
				decoded, err := url.PathUnescape(pathPart)
				if err != nil {
					t.Errorf("%s: invalid URL-escaped link %q: %v", rel(root, source), destination, err)
					continue
				}
				if strings.HasPrefix(decoded, "/") {
					target = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(decoded, "/")))
				} else {
					target = filepath.Clean(filepath.Join(filepath.Dir(source), filepath.FromSlash(decoded)))
				}
			}

			info, err := os.Stat(target)
			if err != nil {
				t.Errorf("%s: broken link %q -> %s", rel(root, source), destination, rel(root, target))
				continue
			}
			if fragment == "" || info.IsDir() || !strings.EqualFold(filepath.Ext(target), ".md") {
				continue
			}

			anchors, err := markdownAnchors(target)
			if err != nil {
				t.Errorf("%s: read linked markdown %s: %v", rel(root, source), rel(root, target), err)
				continue
			}
			decodedFragment, err := url.PathUnescape(fragment)
			if err != nil {
				t.Errorf("%s: invalid anchor %q: %v", rel(root, source), fragment, err)
				continue
			}
			if _, ok := anchors[strings.ToLower(decodedFragment)]; !ok {
				t.Errorf("%s: broken anchor %q in %s", rel(root, source), fragment, rel(root, target))
			}
		}
	}
}

func TestDocumentationDCIReferencesResolve(t *testing.T) {
	root := repositoryRoot(t)
	invariants, err := os.ReadFile(filepath.Join(root, "INVARIANTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]struct{}{}
	for _, id := range dciPattern.FindAllString(string(invariants), -1) {
		defined[id] = struct{}{}
	}

	for _, path := range markdownFiles(t, root) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := fencePattern.ReplaceAllString(string(raw), "")
		for _, loc := range dciPattern.FindAllStringIndex(text, -1) {
			id := text[loc[0]:loc[1]]
			if _, ok := defined[id]; ok {
				continue
			}
			if looksLikeRangeEndpoint(text, loc[0], loc[1]) {
				continue
			}
			t.Errorf("%s: references undefined %s", rel(root, path), id)
		}
	}
}

func TestDocumentationADRReferencesResolve(t *testing.T) {
	root := repositoryRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "docs", "adr"))
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() || len(entry.Name()) < 4 {
			continue
		}
		prefix := entry.Name()[:4]
		if matched, _ := regexp.MatchString("^[0-9]{4}$", prefix); matched {
			defined[prefix] = struct{}{}
		}
	}

	for _, path := range markdownFiles(t, root) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := fencePattern.ReplaceAllString(string(raw), "")
		for _, match := range adrPattern.FindAllStringSubmatch(text, -1) {
			if _, ok := defined[match[1]]; !ok {
				t.Errorf("%s: references undefined ADR-%s", rel(root, path), match[1])
			}
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func markdownFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", ".devcadence", "artifacts", "worktrees":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func isExternalMarkdownDestination(destination string) bool {
	lower := strings.ToLower(destination)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "data:")
}

func splitMarkdownDestination(destination string) (string, string) {
	if i := strings.IndexByte(destination, '#'); i >= 0 {
		return destination[:i], destination[i+1:]
	}
	return destination, ""
}

func markdownAnchors(path string) (map[string]struct{}, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := fencePattern.ReplaceAllString(string(raw), "")
	anchors := map[string]struct{}{}
	seen := map[string]int{}
	for _, line := range strings.Split(text, "\n") {
		match := headingPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if match == nil {
			continue
		}
		base := githubHeadingSlug(match[2])
		if base == "" {
			continue
		}
		slug := base
		if count := seen[base]; count > 0 {
			slug = base + "-" + strconv.Itoa(count)
		}
		seen[base]++
		anchors[slug] = struct{}{}
	}
	return anchors, nil
}

func githubHeadingSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, string(rune(96)), "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "_", "")
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == ' ':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), "-")
}

func looksLikeRangeEndpoint(text string, start, end int) bool {
	left := text[maxInt(0, start-4):start]
	right := text[end:minInt(len(text), end+4)]
	return strings.Contains(left, "..") || strings.Contains(right, "..") ||
		strings.Contains(left, "–") || strings.Contains(right, "–") ||
		strings.Contains(left, "—") || strings.Contains(right, "—")
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

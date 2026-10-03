package tests

import (
	"bytes"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
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
	dciDefinition       = regexp.MustCompile("(?m)^#{2,6}[ \\t]+(DCI-[0-9]{3})\\b")
	htmlTagPattern      = regexp.MustCompile("<[^>]+>")
	inlineLinkPattern   = regexp.MustCompile("\\[([^\\]]*)\\]\\([^)]*\\)")
	refDefPattern       = regexp.MustCompile("(?m)^[ ]{0,3}\\[[^\\]]+\\]:[ \\t]*(\\S+)")
	adrPattern          = regexp.MustCompile("ADR-([0-9]{4})")
)

func TestRepositoryMarkdownLinksAndAnchors(t *testing.T) {
	root := repositoryRoot(t)
	files := markdownFiles(t, root)

	for _, source := range files {
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
		text := stripFences(string(raw))

		destinations := []string{}
		for _, match := range markdownLinkPattern.FindAllStringSubmatch(text, -1) {
			destinations = append(destinations, match[1])
		}
		for _, match := range refDefPattern.FindAllStringSubmatch(text, -1) {
			destinations = append(destinations, match[1])
		}
		for _, raw := range destinations {
			destination := strings.TrimSpace(raw)
			if destination == "" {
				continue
			}
			if strings.HasPrefix(destination, "<") {
				if end := strings.IndexByte(destination, '>'); end > 0 {
					destination = destination[1:end]
				}
			} else if i := strings.IndexAny(destination, " \t"); i >= 0 {
				destination = destination[:i]
			}
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
	for _, match := range dciDefinition.FindAllStringSubmatch(string(invariants), -1) {
		defined[match[1]] = struct{}{}
	}
	if len(defined) == 0 {
		t.Fatal("INVARIANTS.md defines no DCI headings; definition pattern out of date")
	}

	for _, path := range markdownFiles(t, root) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := stripFences(string(raw))
		for _, loc := range dciPattern.FindAllStringIndex(text, -1) {
			id := text[loc[0]:loc[1]]
			if _, ok := defined[id]; ok {
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
		text := stripFences(string(raw))
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

// markdownFiles lists the repository's tracked Markdown files so untracked
// local content (virtualenvs, runtime state) cannot make the result differ
// between machines. A staged-index snapshot has no Git metadata, and every file
// in it is tracked by construction, so it falls back to a plain walk.
func markdownFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z", "--", "*.md")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if out, err := cmd.Output(); err == nil && len(out) > 0 {
		var files []string
		for _, name := range strings.Split(string(out), "\x00") {
			if name != "" {
				files = append(files, filepath.Join(root, filepath.FromSlash(name)))
			}
		}
		sort.Strings(files)
		return files
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		t.Fatalf("git ls-files failed in a Git checkout: %s", stderr.String())
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root {
			switch d.Name() {
			case ".git", ".devcadence":
				return filepath.SkipDir
			}
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
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

// stripFences removes fenced code blocks line by line, honouring CommonMark's
// rule that a closing fence uses the same character and is at least as long as
// the opening fence.
func stripFences(text string) string {
	var out []string
	var fenceChar byte
	fenceLen := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(strings.TrimRight(line, "\r"), " ")
		indent := len(line) - len(strings.TrimLeft(line, " "))
		run := 0
		if indent <= 3 && len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			for run < len(trimmed) && trimmed[run] == trimmed[0] {
				run++
			}
		}
		switch {
		case fenceLen == 0 && run >= 3:
			fenceChar, fenceLen = trimmed[0], run
			out = append(out, "")
		case fenceLen > 0 && run >= fenceLen && trimmed[0] == fenceChar && strings.TrimSpace(trimmed[run:]) == "":
			fenceLen = 0
			out = append(out, "")
		case fenceLen > 0:
			out = append(out, "")
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
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
	text := stripFences(string(raw))
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

// githubHeadingSlug approximates GitHub's heading anchors: inline links and
// HTML reduce to their text, emphasis/code markers vanish, punctuation other
// than hyphen and underscore is dropped, and each space becomes a hyphen.
func githubHeadingSlug(s string) string {
	s = inlineLinkPattern.ReplaceAllString(s, "$1")
	s = htmlTagPattern.ReplaceAllString(s, "")
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("`", "", "*", "").Replace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune('-')
		}
	}
	return b.String()
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

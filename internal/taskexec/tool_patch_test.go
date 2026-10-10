package taskexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/tools"
)

// toolEnv is a canonical temp worktree with the worker tools registered.
type toolEnv struct {
	t        *testing.T
	root     string
	outside  string
	mediator *drivers.ScopedToolMediator
	audit    *commandAudit
	defs     []drivers.ToolDefinition
}

func newToolEnv(t *testing.T, writeScope []string, mode ExecutionMode) *toolEnv {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "wt")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{filepath.Join(root, "src", "sub"), filepath.Join(root, ".git"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scope := &tools.Scope{ProjectID: "p", WorktreePath: root}
	med := drivers.NewScopedToolMediator(scope)
	audit := newCommandAudit(mode)
	defs := setupWorkerToolsWith(med, scope, process.NewRunner(), writeScope, workerToolConfig{Mode: mode, Audit: audit})
	return &toolEnv{t: t, root: root, outside: outside, mediator: med, audit: audit, defs: defs}
}

func (e *toolEnv) write(rel, content string, mode os.FileMode) string {
	e.t.Helper()
	p := filepath.Join(e.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		e.t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *toolEnv) read(rel string) string {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.root, rel))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

// call runs a tool through the mediator and returns its content and whether it failed.
func (e *toolEnv) call(name string, args any) (string, bool) {
	e.t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		e.t.Fatal(err)
	}
	res, _ := e.mediator.ExecuteTool(context.Background(), drivers.ToolCall{ID: "c", Name: name, Arguments: raw})
	return res.Content, res.IsError
}

func (e *toolEnv) patch(path string, edits ...[2]string) (string, bool) {
	list := make([]map[string]string, 0, len(edits))
	for _, ed := range edits {
		list = append(list, map[string]string{"old_text": ed[0], "new_text": ed[1]})
	}
	return e.call("apply_patch", map[string]any{"path": path, "edits": list})
}

func (e *toolEnv) toolNames() []string {
	var n []string
	for _, d := range e.defs {
		n = append(n, d.Name)
	}
	return n
}

func TestApplyPatch_Declared(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionStrict)
	var def drivers.ToolDefinition
	for _, d := range e.defs {
		if d.Name == "apply_patch" {
			def = d
		}
		if d.Name == "run_command" {
			t.Fatal("run_command declared in strict mode")
		}
	}
	if !def.MutatesFiles || len(def.PathParameters) != 1 || def.PathParameters[0] != "path" || !json.Valid(def.Parameters) {
		t.Fatalf("apply_patch def = %+v", def)
	}
}

func TestApplyPatch_Success(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionStrict)
	orig := "package a\n\nfunc F() int {\n\tx := 1\n\ty := 2\n\treturn x + y\n}\n\nfunc G() {}\n\n// tail without newline"
	p := e.write("src/a.go", orig, 0o750)

	// Edits are given out of file order and one spans several lines.
	out, isErr := e.patch("src/a.go",
		[2]string{"func G() {}", "func G() { println(\"g\") }"},
		[2]string{"\tx := 1\n\ty := 2\n\treturn x + y\n", "\tz := 3\n\treturn z\n"},
		[2]string{"// tail without newline", "// tail"},
	)
	if isErr {
		t.Fatalf("patch failed: %s", out)
	}
	want := "package a\n\nfunc F() int {\n\tz := 3\n\treturn z\n}\n\nfunc G() { println(\"g\") }\n\n// tail"
	if got := e.read("src/a.go"); got != want {
		t.Fatalf("content:\n%q\nwant\n%q", got, want)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o750 {
		t.Errorf("mode = %v, want 0750", fi.Mode().Perm())
	}
	var res patchResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Path != "src/a.go" || res.HunksApplied != 3 || res.BytesBefore != len(orig) || res.BytesAfter != len(want) {
		t.Errorf("result = %+v", res)
	}
	for _, frag := range []string{"@@ line 4 (edit 1) @@", "-\tx := 1", "+\tz := 3", "+func G() { println(\"g\") }"} {
		if !strings.Contains(res.Diff, frag) {
			t.Errorf("diff missing %q:\n%s", frag, res.Diff)
		}
	}
	assertNoTemp(t, filepath.Dir(p))
}

func TestApplyPatch_DiffIsBounded(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionStrict)
	e.write("src/big.txt", "start\n", 0o644)
	big := strings.Repeat("0123456789abcdef\n", 4000) // 68000 bytes
	out, isErr := e.patch("src/big.txt", [2]string{"start\n", big})
	if isErr {
		t.Fatal(out)
	}
	var res patchResult
	_ = json.Unmarshal([]byte(out), &res)
	if !res.DiffTruncated || len(res.Diff) > maxPatchDiffBytes+64 {
		t.Fatalf("truncated=%v diff len=%d", res.DiffTruncated, len(res.Diff))
	}
}

func TestApplyPatch_Rejections(t *testing.T) {
	const orig = "one\ntwo\nthree\ntwo\nabcd\n"
	e := newToolEnv(t, []string{"src/*"}, ExecutionStrict)
	e.write("src/a.txt", orig, 0o644)
	e.write("src/bin.dat", "ab\x00cd", 0o644)
	e.write("src/bad.txt", "ab\xffcd", 0o644)
	e.write("src/huge.txt", strings.Repeat("a", maxWriteBytes+1), 0o644)
	e.write("src/near.txt", strings.Repeat("a", maxWriteBytes-1)+"\n", 0o644)
	e.write("docs/out.txt", "x", 0o644)
	e.write(".git/config", "x", 0o644)
	if err := os.WriteFile(filepath.Join(e.outside, "secret.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Symlink(filepath.Join(e.root, "src", "a.txt"), filepath.Join(e.root, "src", "link.txt")))
	must(os.Symlink(e.outside, filepath.Join(e.root, "src", "escape")))
	must(os.Symlink(filepath.Join(e.root, "src", "sub"), filepath.Join(e.root, "src", "inner")))
	must(os.MkdirAll(filepath.Join(e.root, "src", "adir"), 0o755))

	tests := []struct {
		name  string
		path  string
		edits [][2]string
		want  string
	}{
		{"stale names index", "src/a.txt", [][2]string{{"one", "1"}, {"missing", "x"}}, "stale_context: edit 1"},
		{"ambiguous names index and count", "src/a.txt", [][2]string{{"two", "2"}}, "ambiguous_context: edit 0 old_text matches 2 places"},
		{"overlapping", "src/a.txt", [][2]string{{"abc", "x"}, {"bcd", "y"}}, "overlapping_edits: edits 0 and 1"},
		{"self-overlapping occurrences are ambiguous", "src/a.txt", [][2]string{{"one\ntwo\nthree\ntwo\nabcd\nX", "x"}}, "stale_context"},
		{"empty old_text", "src/a.txt", [][2]string{{"", "x"}}, "empty_old_text: edit 0"},
		{"no edits", "src/a.txt", nil, "no_edits"},
		{"traversal", "../outside/secret.txt", [][2]string{{"s", "t"}}, "escapes"},
		{"embedded traversal", "src/../../outside/secret.txt", [][2]string{{"s", "t"}}, "escapes"},
		{"absolute", filepath.Join(e.outside, "secret.txt"), [][2]string{{"s", "t"}}, "relative path required"},
		{"outside write scope", "docs/out.txt", [][2]string{{"x", "y"}}, "outside declared write scope"},
		{".git", ".git/config", [][2]string{{"x", "y"}}, ".git"},
		{"symlink file", "src/link.txt", [][2]string{{"one", "1"}}, "symlink"},
		{"symlinked parent escaping worktree", "src/escape/secret.txt", [][2]string{{"s", "t"}}, "escapes"},
		{"symlinked parent inside worktree", "src/inner/x.txt", [][2]string{{"s", "t"}}, "symlink"},
		{"directory is not regular", "src/adir", [][2]string{{"s", "t"}}, "not_regular_file"},
		{"missing file", "src/nope.txt", [][2]string{{"s", "t"}}, "file_not_found"},
		{"binary NUL", "src/bin.dat", [][2]string{{"ab", "x"}}, "binary_or_non_utf8"},
		{"invalid UTF-8", "src/bad.txt", [][2]string{{"ab", "x"}}, "binary_or_non_utf8"},
		{"oversize file", "src/huge.txt", [][2]string{{"a", "b"}}, "too_large"},
		{"oversize result", "src/near.txt", [][2]string{{"\n", strings.Repeat("b", 10)}}, "too_large"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, isErr := e.patch(tc.path, tc.edits...)
			if !isErr || !strings.Contains(out, tc.want) {
				t.Fatalf("isErr=%v out=%q, want error containing %q", isErr, out, tc.want)
			}
		})
	}
	if got := e.read("src/a.txt"); got != orig {
		t.Errorf("a.txt modified by failed patches: %q", got)
	}
	if got := readOutside(t, e); got != "s" {
		t.Errorf("outside file modified: %q", got)
	}
	assertNoTemp(t, filepath.Join(e.root, "src"))
}

func readOutside(t *testing.T, e *toolEnv) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.outside, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplyPatch_AtomicNoPartialApplication(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionStrict)
	const orig = "alpha\nbeta\ngamma\n"
	p := e.write("src/a.txt", orig, 0o644)
	before := dirNames(t, filepath.Dir(p))

	out, isErr := e.patch("src/a.txt", [2]string{"alpha", "A"}, [2]string{"nonexistent", "B"})
	if !isErr || !strings.Contains(out, "stale_context: edit 1") {
		t.Fatalf("out=%q isErr=%v", out, isErr)
	}
	if got := e.read("src/a.txt"); got != orig {
		t.Fatalf("file changed after failed patch: %q", got)
	}
	if after := dirNames(t, filepath.Dir(p)); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Fatalf("directory entries changed: %v -> %v", before, after)
	}
}

func TestApplyPatch_FileCountSharedWithWriteFile(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionStrict)
	for i := 0; i < maxFilesPerAttempt-1; i++ {
		if out, isErr := e.call("write_file", map[string]string{"path": fmt.Sprintf("src/w%d.txt", i), "content": "x"}); isErr {
			t.Fatal(out)
		}
	}
	e.write("src/a.txt", "a", 0o644)
	e.write("src/b.txt", "b", 0o644)
	if out, isErr := e.patch("src/a.txt", [2]string{"a", "A"}); isErr { // 64th distinct file
		t.Fatal(out)
	}
	if out, isErr := e.patch("src/a.txt", [2]string{"A", "AA"}); isErr { // same file again is free
		t.Fatal(out)
	}
	out, isErr := e.patch("src/b.txt", [2]string{"b", "B"})
	if !isErr || !strings.Contains(out, "maximum of 64 files") {
		t.Fatalf("65th file: isErr=%v out=%q", isErr, out)
	}
	if e.read("src/b.txt") != "b" {
		t.Fatal("65th file was modified")
	}
}

func TestWriteFile_RefusesSymlinkedParent(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionStrict)
	if err := os.Symlink(filepath.Join(e.root, "src", "sub"), filepath.Join(e.root, "src", "inner")); err != nil {
		t.Fatal(err)
	}
	out, isErr := e.call("write_file", map[string]string{"path": "src/inner/x.txt", "content": "x"})
	if !isErr || !strings.Contains(out, "symlink") {
		t.Fatalf("isErr=%v out=%q", isErr, out)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, en := range ents {
		n = append(n, en.Name())
	}
	return n
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	for _, n := range dirNames(t, dir) {
		if strings.HasSuffix(n, ".tmp") {
			t.Errorf("leftover temp file %s in %s", n, dir)
		}
	}
}

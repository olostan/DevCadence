package flightrec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/flightrec/journal"
)

// firstTimeDeny denies MkdirAll of every matching path the first time only, so
// path resolution fails while the later sidecar directory check succeeds.
func firstTimeDeny(match func(string) bool) func(string) bool {
	seen := map[string]bool{}
	return func(p string) bool {
		if match(p) && !seen[p] {
			seen[p] = true
			return true
		}
		return false
	}
}

// orderRecorder records the directories the sidecar tried and fails creation
// in the first failFirst of them; later ones use the real implementation.
type orderRecorder struct {
	dirs      []string
	failFirst int
}

func (o *orderRecorder) create(dir, preferred, pattern string) (*os.File, error) {
	o.dirs = append(o.dirs, dir)
	if len(o.dirs) <= o.failFirst {
		return nil, os.ErrPermission
	}
	return createSidecarFile(dir, preferred, pattern)
}

type sidecarEnv struct {
	home, tmp, tmpTrace string
}

func newSidecarEnv(t *testing.T) sidecarEnv {
	t.Helper()
	e := sidecarEnv{home: t.TempDir(), tmp: t.TempDir()}
	e.tmpTrace = filepath.Join(e.tmp, "devcadence-trace-"+strconv.Itoa(os.Getuid()))
	return e
}

func (e sidecarEnv) input(deny func(string) bool) BootstrapInput {
	return BootstrapInput{
		Resolve: ResolveInput{
			Getenv: noEnv, UserHomeDir: func() (string, error) { return e.home, nil }, TempDir: func() string { return e.tmp },
			UID: os.Getuid, FS: scriptedFS{PathFS: OSPathFS(), deny: deny},
		},
		Clock: newClockForTest(), IDs: newIDs(), Now: time.Now, WriterVersion: "test-1",
	}
}

func (e sidecarEnv) failResolve() func(string) bool {
	return firstTimeDeny(func(p string) bool { return strings.HasSuffix(p, "/traces") || p == e.tmpTrace })
}

func readSidecar(t *testing.T, path string) map[string]any {
	t.Helper()
	b := ioFile(t, path)
	if len(b) > MaxSidecarBytes {
		t.Fatalf("sidecar %d bytes", len(b))
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return doc
}

// B4: the directory order, each step failing in turn.
func TestBootstrapSidecarDirectoryOrder(t *testing.T) {
	e := newSidecarEnv(t)
	homeDir := filepath.Join(e.home, ".devcadence")
	cases := []struct {
		name      string
		mod       func(*BootstrapInput)
		deny      func() func(string) bool
		failFirst int
		wantDirs  []string
	}{
		{name: "no root: first directory (home) succeeds", deny: e.failResolve, wantDirs: []string{homeDir, e.tmpTrace, e.tmp}},
		{name: "no root: home fails", deny: e.failResolve, failFirst: 1, wantDirs: []string{homeDir, e.tmpTrace, e.tmp}},
		{name: "no root: home and temp trace dir fail", deny: e.failResolve, failFirst: 2, wantDirs: []string{homeDir, e.tmpTrace, e.tmp}},
		{name: "no root: every directory fails", deny: e.failResolve, failFirst: 99, wantDirs: []string{homeDir, e.tmpTrace, e.tmp}},
		{name: "unknown uid skips the temp trace dir", deny: e.failResolve, failFirst: 1,
			mod:      func(in *BootstrapInput) { in.Resolve.UID = func() int { return -1 } },
			wantDirs: []string{homeDir, e.tmp}},
		{name: "home lookup error", deny: e.failResolve, failFirst: 0,
			mod:      func(in *BootstrapInput) { in.Resolve.UserHomeDir = noHome },
			wantDirs: []string{e.tmpTrace, e.tmp}},
		{name: "empty home", deny: e.failResolve, failFirst: 99,
			mod:      func(in *BootstrapInput) { in.Resolve.UserHomeDir = func() (string, error) { return "", nil } },
			wantDirs: []string{e.tmpTrace, e.tmp}},
		{name: "home directory unusable", failFirst: 0,
			deny: func() func(string) bool {
				once := e.failResolve()
				return func(p string) bool { return strings.HasPrefix(p, e.home) || once(p) }
			},
			wantDirs: []string{e.tmpTrace, e.tmp}},
		{name: "relative temp dir skips both temp steps", failFirst: 99,
			deny: func() func(string) bool {
				return firstTimeDeny(func(p string) bool { return strings.HasSuffix(p, "/traces") })
			},
			mod:      func(in *BootstrapInput) { in.Resolve.TempDir = func() string { return "relative-tmp" } },
			wantDirs: []string{homeDir}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.RemoveAll(homeDir) // each case starts from empty sidecar directories
			_ = os.RemoveAll(e.tmpTrace)
			old, _ := filepath.Glob(filepath.Join(e.tmp, "devcadence-bootstrap-failure-*.json"))
			for _, f := range old {
				_ = os.Remove(f)
			}
			in := e.input(tc.deny())
			if tc.mod != nil {
				tc.mod(&in)
			}
			o := &orderRecorder{failFirst: tc.failFirst}
			in.sidecarCreate = o.create
			rec, st := Bootstrap(context.Background(), in)
			if st.Mode != ModeDegradedNoop || st.Reason != ReasonNoUsablePath || rec.Status().SidecarPath != st.SidecarPath {
				t.Fatalf("%+v", st)
			}
			if tc.failFirst >= len(tc.wantDirs) {
				if st.SidecarError != SidecarErrNoWritableDir || st.SidecarPath != "" || !slices.Equal(o.dirs, tc.wantDirs) {
					t.Fatalf("dirs %v status %+v", o.dirs, st)
				}
				return
			}
			want := tc.wantDirs[tc.failFirst]
			if !slices.Equal(o.dirs, tc.wantDirs[:tc.failFirst+1]) || st.SidecarError != "" || filepath.Dir(st.SidecarPath) != want {
				t.Fatalf("dirs %v status %+v", o.dirs, st)
			}
			name := filepath.Base(st.SidecarPath)
			if want == e.tmp {
				if ok, _ := filepath.Match("devcadence-bootstrap-failure-*.json", name); !ok {
					t.Fatalf("name %s", name)
				}
			} else if name != SidecarName {
				t.Fatalf("name %s", name)
			}
		})
	}
}

// B4: all candidates fail -> the sidecar lands in the temp directory with
// sanitized content only and mode 0600; the no-op recorder still reports
// degraded.
func TestBootstrapSidecarContent(t *testing.T) {
	e := newSidecarEnv(t)
	in := e.input(func(p string) bool {
		return strings.HasPrefix(p, e.home) || p == e.tmpTrace || strings.Contains(p, "VALUE")
	})
	in.Resolve.ExplicitDir = "/work/API_KEY=VALUE1/ghp_VALUE2abcdef"
	in.Resolve.Getenv = func(k string) string {
		if k == envTraceDir {
			return "/work/password: VALUE3"
		}
		return ""
	}
	in.WriterVersion = "v1 token=VALUE5"
	rec, st := Bootstrap(context.Background(), in)
	if st.Mode != ModeDegradedNoop || st.Reason != ReasonNoUsablePath || rec.Status().Mode != ModeDegradedNoop {
		t.Fatalf("%+v", st)
	}
	if st.SidecarError != "" || filepath.Dir(st.SidecarPath) != e.tmp {
		t.Fatalf("%+v", st)
	}
	if fi, err := os.Stat(st.SidecarPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	raw := string(ioFile(t, st.SidecarPath))
	if strings.Contains(raw, "VALUE") {
		t.Fatalf("sidecar leaked: %s", raw)
	}
	doc := readSidecar(t, st.SidecarPath)
	if doc["schema_version"] != float64(SidecarSchemaVersion) || doc["reason"] != ReasonNoUsablePath {
		t.Fatalf("%v", doc)
	}
	if wa, _ := doc["written_at"].(string); !strings.HasPrefix(wa, "2023-11-14T22:13:20") {
		t.Fatalf("written_at %v", doc["written_at"])
	}
	if doc["writer_version"] != "[REDACTED]" {
		t.Fatalf("writer_version %v", doc["writer_version"])
	}
	if _, ok := doc["node_id"]; ok {
		t.Fatalf("no node id was known: %v", doc)
	}
	atts, _ := doc["attempts"].([]any)
	if len(atts) != len(st.Attempts) || len(atts) == 0 {
		t.Fatalf("attempts %v", doc["attempts"])
	}
	first := atts[0].(map[string]any)
	if first["source"] != SourceExplicitFlag || first["error_code"] != ErrCodeMkdirFailed || first["path"] != "[REDACTED]" {
		t.Fatalf("attempt %v", first)
	}
	if _, ok := doc["stats"].(map[string]any); !ok {
		t.Fatalf("stats %v", doc["stats"])
	}
	checkNoopInert(t, rec)
}

// B4: a journal failure after a root was resolved writes the sidecar into that
// root and reports the node-id source; the journal itself is not required.
func TestBootstrapSidecarInRootWhenOnlyJournalFailed(t *testing.T) {
	t.Run("journal open failure", func(t *testing.T) {
		in, root := bootInput(t)
		in.Limits = journal.Limits{MaxRecordBytes: 100}
		in.WriterVersion = "test-1"
		_, st := Bootstrap(context.Background(), in)
		if st.Reason != ReasonJournalOpenFailed || st.SidecarPath != filepath.Join(root, SidecarName) || st.SidecarError != "" {
			t.Fatalf("%+v", st)
		}
		doc := readSidecar(t, st.SidecarPath)
		if doc["reason"] != ReasonJournalOpenFailed || doc["node_id_source"] != "file" || doc["writer_version"] != "test-1" {
			t.Fatalf("%v", doc)
		}
		if id, _ := doc["node_id"].(string); !strings.HasPrefix(id, "nod_") {
			t.Fatalf("node_id %v", doc["node_id"])
		}
	})
	t.Run("STREAM_STARTED failure with an ephemeral node id", func(t *testing.T) {
		in, root := bootInput(t)
		if err := os.MkdirAll(filepath.Join(root, "node-id"), 0o700); err != nil {
			t.Fatal(err)
		}
		in.wrapSink = func(s Sink) Sink { return failingSink{s} }
		_, st := Bootstrap(context.Background(), in)
		if st.Reason != ReasonStreamStartFailure || st.SidecarPath != filepath.Join(root, SidecarName) {
			t.Fatalf("%+v", st)
		}
		if doc := readSidecar(t, st.SidecarPath); doc["node_id_source"] != "ephemeral" {
			t.Fatalf("%v", doc)
		}
	})
	t.Run("invalid ids", func(t *testing.T) {
		in, root := bootInput(t)
		in.IDs = testIDs{Sequential: newIDs(), bad: map[string]bool{"run": true}}
		_, st := Bootstrap(context.Background(), in)
		if st.Reason != ReasonInvalidID || st.SidecarPath != filepath.Join(root, SidecarName) {
			t.Fatalf("%+v", st)
		}
	})
}

// B4: the preferred name is never overwritten (O_EXCL); a collision falls back
// to a random bootstrap-failure-*.json name in the same directory.
func TestBootstrapSidecarCollision(t *testing.T) {
	in, root := bootInput(t)
	in.Limits = journal.Limits{MaxRecordBytes: 100}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	pre := filepath.Join(root, SidecarName)
	if err := os.WriteFile(pre, []byte("earlier failure"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, st := Bootstrap(context.Background(), in)
	if st.SidecarError != "" || st.SidecarPath == pre || filepath.Dir(st.SidecarPath) != root {
		t.Fatalf("%+v", st)
	}
	if ok, _ := filepath.Match(filepath.Join(root, "bootstrap-failure-*.json"), st.SidecarPath); !ok {
		t.Fatalf("name %s", st.SidecarPath)
	}
	if string(ioFile(t, pre)) != "earlier failure" {
		t.Fatal("earlier sidecar overwritten")
	}
	if fi, err := os.Stat(st.SidecarPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	// The exporter glob finds both.
	if m, _ := filepath.Glob(filepath.Join(root, "bootstrap-failure*.json")); len(m) != 2 {
		t.Fatalf("glob %v", m)
	}
}

// B4: a failed write removes the partial file and moves on to the next
// directory; a creation error in the real implementation does the same.
func TestBootstrapSidecarWriteFailureFallsThrough(t *testing.T) {
	e := newSidecarEnv(t)
	in := e.input(e.failResolve())
	var made []string
	in.sidecarCreate = func(dir, preferred, pattern string) (*os.File, error) {
		f, err := createSidecarFile(dir, preferred, pattern)
		if err != nil {
			return nil, err
		}
		made = append(made, f.Name())
		if len(made) == 1 {
			_ = f.Close() // the write of the first file fails
		}
		return f, nil
	}
	_, st := Bootstrap(context.Background(), in)
	if len(made) != 2 || st.SidecarPath != made[1] || st.SidecarError != "" {
		t.Fatalf("made %v status %+v", made, st)
	}
	if _, err := os.Lstat(made[0]); !os.IsNotExist(err) {
		t.Fatalf("partial sidecar not removed: %v", err)
	}

	// Real implementation: TempDir is a regular file, so every directory fails.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	in2 := e.input(firstTimeDeny(func(p string) bool { return strings.HasSuffix(p, "/traces") }))
	in2.Resolve.UserHomeDir = noHome
	in2.Resolve.TempDir = func() string { return file }
	_, st2 := Bootstrap(context.Background(), in2)
	if st2.Mode != ModeDegradedNoop || st2.SidecarError != SidecarErrNoWritableDir || st2.SidecarPath != "" {
		t.Fatalf("%+v", st2)
	}
}

// B4: the document is bounded and sanitized whatever the attempts contain.
func TestBuildSidecarBounds(t *testing.T) {
	var attempts []Attempt
	for i := 0; i < 1000; i++ {
		attempts = append(attempts, Attempt{Source: "s" + strconv.Itoa(i), Path: "/p/" + strings.Repeat("x", 400) + "token=VALUE", ErrCode: "bad code with spaces"})
	}
	b := buildSidecar(DefaultSanitizer(), t0, "reason with spaces", strings.Repeat("v", 5000), "", "", attempts, Stats{Dropped: 3})
	if len(b) > MaxSidecarBytes {
		t.Fatalf("%d bytes", len(b))
	}
	var doc struct {
		Reason            string `json:"reason"`
		WriterVersion     string `json:"writer_version"`
		Attempts          []sidecarAttempt
		AttemptsTruncated bool `json:"attempts_truncated"`
		Stats             Stats
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Attempts) != maxSidecarAttempts || !doc.AttemptsTruncated || doc.Reason != "[INVALID]" || doc.Stats.Dropped != 3 ||
		doc.Attempts[0].Path != "[INVALID]" || doc.Attempts[0].ErrCode != "[INVALID]" || strings.Contains(string(b), "VALUE") {
		t.Fatalf("%+v", doc)
	}
	if !strings.Contains(doc.WriterVersion, "truncated") {
		t.Fatalf("writer version %q", doc.WriterVersion)
	}
	// An empty attempts list is an empty array, not null.
	if b := buildSidecar(DefaultSanitizer(), t0, ReasonInvalidID, "", "", "", nil, Stats{}); !strings.Contains(string(b), `"attempts":[]`) {
		t.Fatalf("%s", b)
	}
}

// B4: Bootstrap still never panics when sidecar writing panics too.
func TestBootstrapSidecarPanicIsContained(t *testing.T) {
	e := newSidecarEnv(t)
	in := e.input(nil)
	in.Resolve.FS = panicFS{OSPathFS()}
	in.lastResortCreate = func(string, string, string) (*os.File, error) { panic("last resort exploded") }
	rec, st := Bootstrap(context.Background(), in)
	if st.Mode != ModeDegradedNoop || st.Reason != ReasonBootstrapPanic || st.SidecarError != SidecarErrPanic || st.SidecarPath != "" {
		t.Fatalf("%+v", st)
	}
	checkNoopInert(t, rec)
}

// B4: a panic after the root is known (journal already open) still writes the
// sidecar into the root and closes the writer.
func TestBootstrapSidecarAfterLatePanic(t *testing.T) {
	in, root := bootInput(t)
	in.wrapSink = func(s Sink) Sink { return panicSink{s} }
	_, st := Bootstrap(context.Background(), in)
	if st.Reason != ReasonBootstrapPanic || st.SidecarPath != filepath.Join(root, SidecarName) {
		t.Fatalf("%+v", st)
	}
	if doc := readSidecar(t, st.SidecarPath); doc["reason"] != ReasonBootstrapPanic {
		t.Fatalf("%v", doc)
	}
}

// N5: the node-id file is never opened unless it is a small regular file.
func TestNodeIDFileBounded(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, path string){
		"oversized regular file": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(strings.Repeat("nod_", 1<<16)), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"directory is never read or replaced": func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"valid symlink is not followed": func(t *testing.T, path string) {
			target := filepath.Join(filepath.Dir(path), "target")
			if err := os.WriteFile(target, []byte(nodeID()+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			setup(t, filepath.Join(dir, "node-id"))
			id, eph := nodeIDFile(dir, newIDs())
			if !eph || !strings.HasPrefix(id, "nod_") {
				t.Fatalf("%q %v", id, eph)
			}
		})
	}
}

// N5: Lstat failures other than not-exist and a failed create both yield an
// ephemeral id and never write.
func TestNodeIDFileUnusableRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, root := range map[string]string{"root is a file (ENOTDIR)": file, "root is missing (create fails)": filepath.Join(dir, "missing")} {
		id, eph := nodeIDFile(root, newIDs())
		if !eph || !strings.HasPrefix(id, "nod_") {
			t.Fatalf("%s: %q %v", name, id, eph)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Fatalf("root created: %v", err)
	}
}

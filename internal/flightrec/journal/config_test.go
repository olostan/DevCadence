package journal

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
)

func TestLimitsNormalize(t *testing.T) {
	def, err := (Limits{}).Normalize()
	if err != nil || def.MaxRecordBytes != DefaultMaxRecordBytes || def.MaxSegmentBytes != DefaultMaxSegmentBytes {
		t.Fatalf("%+v %v", def, err)
	}
	if minSegmentBytes(DefaultMaxRecordBytes) != 1065028 || minSegmentBytes(4096) != 20548 {
		t.Fatalf("minimum segment sizes %d %d", minSegmentBytes(DefaultMaxRecordBytes), minSegmentBytes(4096))
	}
	if _, err := (Limits{MaxRecordBytes: 4096, MaxSegmentBytes: 20548}).Normalize(); err != nil {
		t.Fatalf("exact minimum must be accepted: %v", err)
	}
	bad := []Limits{
		{MaxRecordBytes: 4096, MaxSegmentBytes: 20547},
		{MaxRecordBytes: 4095},
		{MaxRecordBytes: HardMaxRecordBytes + 1, MaxSegmentBytes: 1 << 30},
		{MaxSegmentBytes: 1 << 20},
	}
	for _, l := range bad {
		if _, err := l.Normalize(); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Fatalf("%+v: %v", l, err)
		}
	}
	if _, err := (Limits{MaxRecordBytes: HardMaxRecordBytes, MaxSegmentBytes: 1 << 30}).Normalize(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidation(t *testing.T) {
	good := newEnv(t)
	cases := map[string]func(c *Config){
		"bad limits":      func(c *Config) { c.Limits = Limits{MaxRecordBytes: 1} },
		"no root":         func(c *Config) { c.Root = "" },
		"no dir":          func(c *Config) { c.Dir = "" },
		"no clock":        func(c *Config) { c.Clock = nil },
		"no ids":          func(c *Config) { c.IDs = nil },
		"no mono":         func(c *Config) { c.Mono = nil },
		"negative queue":  func(c *Config) { c.QueueSize = -1 },
		"negative batch":  func(c *Config) { c.MaxBatch = -1 },
		"negative flush":  func(c *Config) { c.FlushEvery = -time.Second },
		"empty node":      func(c *Config) { c.NodeID = "" },
		"long runtime":    func(c *Config) { c.RuntimeID = strings.Repeat("r", 129) },
		"invalid stream":  func(c *Config) { c.StreamID = "\xff" },
		"long version":    func(c *Config) { c.WriterVersion = strings.Repeat("v", 257) },
		"invalid version": func(c *Config) { c.WriterVersion = "\xff" },
		"dir outside":     func(c *Config) { c.Dir = filepath.Join(filepath.Dir(c.Root), "elsewhere") },
		"dir is parent":   func(c *Config) { c.Dir = filepath.Dir(c.Root) },
		"dir not related": func(c *Config) { c.Dir = "relative/dir" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := good.cfg
			mutate(&cfg)
			if _, err := Open(ctxBG(), cfg); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
				t.Fatalf("%v", err)
			}
		})
	}
	// Dir equal to Root is allowed (the chain has a single element).
	env := newEnv(t)
	env.cfg.Dir = env.root
	w := env.open(t)
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
}

// A-20: directory safety.
func TestSymlinkedDirIsRejected(t *testing.T) {
	env := newEnv(t)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(env.dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, env.dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctxBG(), env.cfg); errs.CategoryOf(err) != errs.CategoryInvalidArgument || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("%v", err)
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Fatal("nothing may be written through the symlink")
	}
}

func TestDirThatIsAFileFailsToCreate(t *testing.T) {
	env := newEnv(t)
	if err := os.MkdirAll(filepath.Dir(env.dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctxBG(), env.cfg); err == nil || !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("%v", err)
	}
	// A regular file where a parent directory should be: Lstat reports ENOTDIR.
	env2 := newEnv(t)
	if err := os.MkdirAll(env2.root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env2.root, "nodes"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctxBG(), env2.cfg); err == nil || !strings.Contains(err.Error(), "lstat") {
		t.Fatalf("%v", err)
	}
}

func TestOwnerCheckThroughSeam(t *testing.T) {
	env := newEnv(t)
	env.cfg.selfUID = func() int { return 4242 }
	env.cfg.ownerUID = func(os.FileInfo) (int, bool) { return 4243, true }
	if _, err := Open(ctxBG(), env.cfg); errs.CategoryOf(err) != errs.CategoryInvalidArgument || !strings.Contains(err.Error(), "owned by uid 4243") {
		t.Fatalf("%v", err)
	}
	// Equal owner passes; an unknown owner skips the check.
	env.cfg.ownerUID = func(os.FileInfo) (int, bool) { return 4242, true }
	w := env.open(t)
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	env2 := newEnv(t)
	env2.cfg.selfUID = func() int { return 4242 }
	env2.cfg.ownerUID = func(os.FileInfo) (int, bool) { return 1, false }
	w2 := env2.open(t)
	if err := w2.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerUIDOfFileInfo(t *testing.T) {
	fi, err := os.Lstat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	uid, ok := ownerUID(fi)
	if !ok || uid != os.Getuid() {
		t.Fatalf("ownerUID = %d, %v", uid, ok)
	}
	if _, ok := ownerUID(fakeInfo{}); ok {
		t.Fatal("a FileInfo without syscall data has no owner")
	}
}

type fakeInfo struct{ os.FileInfo }

func (fakeInfo) Sys() any { return nil }

func TestSyncDirCoversOnlyNewDirectories(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "traces")
	node := filepath.Join(root, "nodes", "nod_t")
	dir := filepath.Join(node, "run_t", "main")
	env := newEnv(t)
	env.cfg.Root, env.cfg.Dir = root, dir
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	synced := map[string]bool{}
	for _, e := range ffs.logCopy() {
		if p, ok := strings.CutPrefix(e, "syncdir:"); ok {
			synced[p] = true
		}
	}
	for _, d := range []string{root, filepath.Join(root, "nodes"), node, filepath.Join(node, "run_t"), dir, base} {
		if !synced[d] {
			t.Fatalf("%s not synced; synced=%v", d, synced)
		}
	}
	for p := range synced {
		if !strings.HasPrefix(p, base) {
			t.Fatalf("synced above the root's parent: %s", p)
		}
	}
	if synced[filepath.Dir(base)] {
		t.Fatal("the parent of Root's parent must not be synced")
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}

	// A second run under the existing root syncs only its own new directories and their parents.
	env2 := newEnv(t)
	env2.cfg.Root = root
	env2.cfg.Dir = filepath.Join(node, "run_2", "main")
	ffs2 := newFaultFS(nil)
	env2.cfg.FS = ffs2
	w2 := env2.open(t)
	for _, e := range ffs2.logCopy() {
		if p, ok := strings.CutPrefix(e, "syncdir:"); ok && (p == root || p == filepath.Join(root, "nodes")) {
			t.Fatalf("pre-existing directory synced: %s", p)
		}
	}
	if err := w2.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
}

func TestLockPreCreatedAsDirectory(t *testing.T) {
	env := newEnv(t)
	if err := os.MkdirAll(filepath.Join(env.dir, "LOCK"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctxBG(), env.cfg); err == nil || !strings.Contains(err.Error(), "open LOCK") {
		t.Fatalf("%v", err)
	}
}

func TestDirectoryPreparationFaults(t *testing.T) {
	notExist := fs.ErrNotExist
	cases := []struct {
		name string
		rule func(op, name string, n int) *fault
		want string
	}{
		{"lstat chain", failNth("lstat", 1, errBoom), "lstat"},
		{"mkdirall", failNth("mkdirall", 1, errBoom), "mkdir"},
		{"lstat dir", func(op, _ string, n int) *fault {
			// 5 chain lookups (root, nodes, nod, run, main), then the check of Dir.
			if op == "lstat" && n == 6 {
				return &fault{err: errBoom}
			}
			return nil
		}, "lstat"},
		{"syncdir created", failNth("syncdir", 1, errBoom), "sync"},
		{"syncdir parent", failNth("syncdir", 2, errBoom), "sync parent"},
		{"lstat not-exist is fine", failNth("lstat", 99, notExist), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newEnv(t)
			env.cfg.FS = newFaultFS(c.rule)
			w, err := Open(ctxBG(), env.cfg)
			if c.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				_ = w.Close(ctxBG())
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) || !errors.Is(err, errBoom) {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestOSFSErrorPaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope", "x")
	o := OSFS()
	if _, err := o.OpenFile(missing, os.O_RDONLY, 0); err == nil {
		t.Fatal("OpenFile")
	}
	if _, err := o.Open(missing); err == nil {
		t.Fatal("Open")
	}
	if err := o.SyncDir(missing); err == nil {
		t.Fatal("SyncDir")
	}
	if _, err := o.ReadDir(missing); err == nil {
		t.Fatal("ReadDir")
	}
	if _, err := o.Lstat(missing); err == nil {
		t.Fatal("Lstat")
	}
}

func TestListSegmentsIgnoresOtherNames(t *testing.T) {
	got := listSegments([]string{"LOCK", "segment-000002.pbj", "segment-1.pbj", "segment-000001.pbj", "x-segment-000003.pbj", "segment-000010.pbj.tmp"})
	if len(got) != 2 || got[0].index != 1 || got[1].index != 2 {
		t.Fatalf("%+v", got)
	}
}

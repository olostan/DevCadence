package flightrec

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeInfo struct{ mode os.FileMode }

func (fakeInfo) Name() string        { return "x" }
func (fakeInfo) Size() int64         { return 0 }
func (f fakeInfo) Mode() os.FileMode { return f.mode }
func (fakeInfo) ModTime() time.Time  { return time.Time{} }
func (f fakeInfo) IsDir() bool       { return f.mode.IsDir() }
func (fakeInfo) Sys() any            { return nil }

// nodeState scripts what the fake PathFS reports for one path.
type nodeState struct {
	mkdirErr, lstatErr, chmodErr error
	mode                         os.FileMode // zero means a plain 0700 directory
	owner                        int
	ownerKnown                   bool
}

type fakePathFS struct {
	nodes  map[string]nodeState
	chmods []string
	mkdirs []string
}

func (f *fakePathFS) MkdirAll(p string, _ os.FileMode) error {
	f.mkdirs = append(f.mkdirs, p)
	return f.nodes[p].mkdirErr
}

func (f *fakePathFS) Lstat(p string) (os.FileInfo, error) {
	n := f.nodes[p]
	if n.lstatErr != nil {
		return nil, n.lstatErr
	}
	mode := n.mode
	if mode == 0 {
		mode = os.ModeDir | 0o700
	}
	return fakeInfo{mode: mode}, nil
}

func (f *fakePathFS) Chmod(p string, _ os.FileMode) error {
	f.chmods = append(f.chmods, p)
	return f.nodes[p].chmodErr
}

func (f *fakePathFS) OwnerUID(os.FileInfo) (int, bool) { return -1, false }

// ownedFS reports per-path owners through a stable fake info keyed by path.
type ownedFS struct {
	*fakePathFS
	last string
}

func (o *ownedFS) Lstat(p string) (os.FileInfo, error) {
	o.last = p
	return o.fakePathFS.Lstat(p)
}

func (o *ownedFS) OwnerUID(os.FileInfo) (int, bool) {
	n := o.nodes[o.last]
	return n.owner, n.ownerKnown
}

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func resolveInput(env map[string]string, fsys PathFS) ResolveInput {
	return ResolveInput{
		Getenv:      envOf(env),
		UserHomeDir: func() (string, error) { return "/home/u", nil },
		TempDir:     func() string { return "/tmp" },
		UID:         func() int { return 1000 },
		FS:          fsys,
	}
}

func TestResolveOrderAndAttempts(t *testing.T) {
	const (
		explicit = "/flag/dir"
		traceDir = "/env/trace"
		homePath = "/dh/traces"
		xdgPath  = "/xdg/devcadence/traces"
		userPath = "/home/u/.devcadence/traces"
		tmpPath  = "/tmp/devcadence-trace-1000"
	)
	all := map[string]string{"DEVCADENCE_TRACE_DIR": traceDir, "DEVCADENCE_HOME": "/dh", "XDG_STATE_HOME": "/xdg"}
	fail := nodeState{mkdirErr: errors.New("denied")}
	cases := []struct {
		name     string
		explicit string
		env      map[string]string
		nodes    map[string]nodeState
		root     string
		source   string
		fallback bool
		attempts []Attempt
	}{
		{"explicit wins", explicit, all, nil, explicit, SourceExplicitFlag, false, nil},
		{"trace dir", "", all, nil, traceDir, SourceEnvTraceDir, false, []Attempt{{SourceExplicitFlag, "", ErrCodeUnset}}},
		{"home", "", map[string]string{"DEVCADENCE_HOME": "/dh", "XDG_STATE_HOME": "/xdg"}, nil, homePath, SourceEnvHome, false,
			[]Attempt{{SourceExplicitFlag, "", ErrCodeUnset}, {SourceEnvTraceDir, "", ErrCodeUnset}}},
		{"xdg", "", map[string]string{"XDG_STATE_HOME": "/xdg"}, nil, xdgPath, SourceXDGState, false,
			[]Attempt{{SourceExplicitFlag, "", ErrCodeUnset}, {SourceEnvTraceDir, "", ErrCodeUnset}, {SourceEnvHome, "", ErrCodeUnset}}},
		{"user home", "", nil, nil, userPath, SourceUserHome, false,
			[]Attempt{{SourceExplicitFlag, "", ErrCodeUnset}, {SourceEnvTraceDir, "", ErrCodeUnset}, {SourceEnvHome, "", ErrCodeUnset}, {SourceXDGState, "", ErrCodeUnset}}},
		{"temp after failures", explicit, all, map[string]nodeState{explicit: fail, traceDir: fail, homePath: fail, xdgPath: fail, userPath: fail},
			tmpPath, SourceTemp, true, []Attempt{
				{SourceExplicitFlag, explicit, ErrCodeMkdirFailed}, {SourceEnvTraceDir, traceDir, ErrCodeMkdirFailed},
				{SourceEnvHome, homePath, ErrCodeMkdirFailed}, {SourceXDGState, xdgPath, ErrCodeMkdirFailed}, {SourceUserHome, userPath, ErrCodeMkdirFailed}}},
		{"set failure then lower unset", explicit, nil, map[string]nodeState{explicit: fail}, userPath, SourceUserHome, true, []Attempt{
			{SourceExplicitFlag, explicit, ErrCodeMkdirFailed}, {SourceEnvTraceDir, "", ErrCodeUnset}, {SourceEnvHome, "", ErrCodeUnset}, {SourceXDGState, "", ErrCodeUnset}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := resolveInput(c.env, &fakePathFS{nodes: c.nodes})
			in.ExplicitDir = c.explicit
			res, err := Resolve(in)
			if err != nil {
				t.Fatal(err)
			}
			if res.Root != c.root || res.Source != c.source || res.Fallback != c.fallback || !reflect.DeepEqual(res.Attempts, c.attempts) {
				t.Fatalf("got %+v", res)
			}
		})
	}
}

func TestResolveErrCodes(t *testing.T) {
	const p = "/flag/dir"
	cases := []struct {
		name string
		path string
		node nodeState
		uid  int
		want string
	}{
		{"not absolute", "relative/dir", nodeState{}, 1000, ErrCodeNotAbsolute},
		{"mkdir failed", p, nodeState{mkdirErr: errors.New("x")}, 1000, ErrCodeMkdirFailed},
		{"lstat failed", p, nodeState{lstatErr: errors.New("x")}, 1000, ErrCodeMkdirFailed},
		{"symlink", p, nodeState{mode: os.ModeSymlink | 0o777}, 1000, ErrCodeSymlink},
		{"not a dir", p, nodeState{mode: 0o600}, 1000, ErrCodeNotDir},
		{"wrong owner", p, nodeState{owner: 0, ownerKnown: true}, 1000, ErrCodeNotOwner},
		{"owner unknown skips check", p, nodeState{owner: 0, ownerKnown: false}, 1000, ""},
		{"negative uid skips check", p, nodeState{owner: 0, ownerKnown: true}, -1, ""},
		{"right owner", p, nodeState{owner: 1000, ownerKnown: true}, 1000, ""},
		{"perm fixed", p, nodeState{mode: os.ModeDir | 0o755}, 1000, ""},
		{"perm fix fails", p, nodeState{mode: os.ModeDir | 0o755, chmodErr: errors.New("x")}, 1000, ErrCodePermFixedFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := &ownedFS{fakePathFS: &fakePathFS{nodes: map[string]nodeState{c.path: c.node}}}
			if got := usableCode(fsys, c.uid, c.path); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
			if c.name == "perm fixed" && !reflect.DeepEqual(fsys.chmods, []string{p}) {
				t.Fatalf("chmods %v", fsys.chmods)
			}
			if c.want == "" && c.node.mode&0o777 == 0 && len(fsys.chmods) != 0 {
				t.Fatalf("unexpected chmod %v", fsys.chmods)
			}
		})
	}
}

func TestResolveUnknownUIDAndTotalFailure(t *testing.T) {
	fail := nodeState{mkdirErr: errors.New("denied")}
	in := resolveInput(map[string]string{"DEVCADENCE_TRACE_DIR": "/t", "DEVCADENCE_HOME": "relative", "XDG_STATE_HOME": "/x"},
		&fakePathFS{nodes: map[string]nodeState{"/flag": fail, "/t": fail, "/x/devcadence/traces": fail, "/home/u/.devcadence/traces": fail}})
	in.ExplicitDir = "/flag"
	in.UID = func() int { return -1 }
	res, err := Resolve(in)
	if err == nil {
		t.Fatal("expected failure")
	}
	want := []Attempt{
		{SourceExplicitFlag, "/flag", ErrCodeMkdirFailed}, {SourceEnvTraceDir, "/t", ErrCodeMkdirFailed},
		{SourceEnvHome, "relative/traces", ErrCodeNotAbsolute}, {SourceXDGState, "/x/devcadence/traces", ErrCodeMkdirFailed},
		{SourceUserHome, "/home/u/.devcadence/traces", ErrCodeMkdirFailed}, {SourceTemp, "", ErrCodeUIDUnknown},
	}
	if !reflect.DeepEqual(res.Attempts, want) || !res.Fallback || res.Root != "" {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveHomeDirErrors(t *testing.T) {
	for name, fn := range map[string]func() (string, error){
		"error": func() (string, error) { return "", errors.New("no HOME") },
		"empty": func() (string, error) { return "", nil },
	} {
		t.Run(name, func(t *testing.T) {
			in := resolveInput(nil, &fakePathFS{})
			in.UserHomeDir = fn
			res, err := Resolve(in)
			if err != nil || res.Source != SourceTemp || res.Fallback {
				t.Fatalf("got %+v %v", res, err)
			}
			if res.Attempts[4] != (Attempt{SourceUserHome, "", ErrCodeUnset}) {
				t.Fatalf("attempts %+v", res.Attempts)
			}
		})
	}
}

// TestResolveOSDefaults drives the production defaults (real filesystem under
// t.TempDir, explicit dir wins so no host state is consulted).
func TestResolveOSDefaults(t *testing.T) {
	base := t.TempDir()
	loose := filepath.Join(base, "loose")
	if err := os.Mkdir(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Resolve(ResolveInput{ExplicitDir: loose})
	if err != nil || res.Root != loose || res.Source != SourceExplicitFlag {
		t.Fatalf("got %+v %v", res, err)
	}
	fi, err := os.Stat(loose)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
	// A symlink to a real directory is rejected (real Lstat).
	link := filepath.Join(base, "link")
	if err := os.Symlink(loose, link); err != nil {
		t.Fatal(err)
	}
	res, err = Resolve(ResolveInput{ExplicitDir: link, Getenv: func(string) string { return "" }, TempDir: func() string { return base }, UserHomeDir: func() (string, error) { return "", errors.New("no home") }})
	if err != nil || res.Source != SourceTemp || res.Attempts[0].ErrCode != ErrCodeSymlink || !res.Fallback {
		t.Fatalf("got %+v %v", res, err)
	}
	// A new directory is created 0700.
	fresh := filepath.Join(base, "a", "b")
	if res, err = Resolve(ResolveInput{ExplicitDir: fresh}); err != nil || res.Root != fresh {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestOwnerUID(t *testing.T) {
	if _, ok := ownerUID(fakeInfo{mode: 0o600}); ok {
		t.Fatal("fake info must not expose an owner")
	}
	fi, err := os.Lstat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if uid, ok := ownerUID(fi); !ok || uid != os.Getuid() {
		t.Fatalf("uid %d ok %v", uid, ok)
	}
	if _, ok := OSPathFS().OwnerUID(fi); !ok {
		t.Fatal("OSPathFS must expose the owner")
	}
}

var _ = fs.ErrNotExist

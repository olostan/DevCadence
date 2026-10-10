package flightrec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/flightrec/journal"
)

// hermeticEnv points every home/temp environment variable at fresh temporary
// directories: the last-resort sidecar uses plain os.UserHomeDir/os.TempDir.
func hermeticEnv(t *testing.T) (home, tmp string) {
	t.Helper()
	home, tmp = t.TempDir(), t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE"} {
		t.Setenv(k, home)
	}
	for _, k := range []string{"TMPDIR", "TEMP", "TMP"} {
		t.Setenv(k, tmp)
	}
	return home, tmp
}

type panicClock struct{}

func (panicClock) Now() time.Time { panic("clock exploded") }

type panicIDs struct{}

func (panicIDs) New(string) string { panic("ids exploded") }

// allPanicFS panics on every PathFS method.
type allPanicFS struct{}

func (allPanicFS) MkdirAll(string, os.FileMode) error      { panic("fs exploded") }
func (allPanicFS) Lstat(string) (os.FileInfo, error)       { panic("fs exploded") }
func (allPanicFS) Chmod(string, os.FileMode) error         { panic("fs exploded") }
func (allPanicFS) OwnerUID(os.FileInfo) (int, bool)        { panic("fs exploded") }
func panicCreate(string, string, string) (*os.File, error) { panic("create exploded") }

// R2-B3: a panicking injected Clock cannot escape the deferred recovery; the
// sidecar is still written (zero written_at) in the resolved root.
func TestBootstrapPanickingClock(t *testing.T) {
	in, root := bootInput(t)
	in.Clock = panicClock{}
	rec, st := Bootstrap(context.Background(), in)
	if st.Mode != ModeDegradedNoop || st.Reason != ReasonBootstrapPanic || st.SidecarPath != filepath.Join(root, SidecarName) {
		t.Fatalf("%+v", st)
	}
	if doc := readSidecar(t, st.SidecarPath); doc["written_at"] != "0001-01-01T00:00:00Z" {
		t.Fatalf("written_at %v", doc["written_at"])
	}
	checkNoopInert(t, rec)
}

// R2-B3: a panicking IDs source is contained; the root is known so the sidecar
// lands there.
func TestBootstrapPanickingIDs(t *testing.T) {
	in, root := bootInput(t)
	in.IDs = panicIDs{}
	_, st := Bootstrap(context.Background(), in)
	if st.Mode != ModeDegradedNoop || st.Reason != ReasonBootstrapPanic || st.SidecarPath != filepath.Join(root, SidecarName) {
		t.Fatalf("%+v", st)
	}
}

// R2-B3 / R2-NB(c): when the injected PathFS panics during the sidecar's own
// directory checks, the last-resort attempt (plain os calls only) still writes
// the evidence: first <HOME>/.devcadence, else <TMPDIR>.
func TestBootstrapLastResortSidecar(t *testing.T) {
	check := func(t *testing.T, st Status, dir, pattern string) {
		t.Helper()
		if st.Mode != ModeDegradedNoop || st.Reason != ReasonBootstrapPanic || st.SidecarError != "" || filepath.Dir(st.SidecarPath) != dir {
			t.Fatalf("%+v want dir %s", st, dir)
		}
		if ok, _ := filepath.Match(filepath.Join(dir, pattern), st.SidecarPath); !ok {
			t.Fatalf("name %s", st.SidecarPath)
		}
		if fi, err := os.Stat(st.SidecarPath); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v %v", fi, err)
		}
		if doc := readSidecar(t, st.SidecarPath); doc["reason"] != ReasonBootstrapPanic {
			t.Fatalf("%v", doc)
		}
	}
	setup := func(t *testing.T) (BootstrapInput, string, string) {
		home, tmp := hermeticEnv(t)
		in, _ := bootInput(t)
		in.Resolve.FS = allPanicFS{}
		return in, home, tmp
	}
	t.Run("home", func(t *testing.T) {
		in, home, _ := setup(t)
		rec, st := Bootstrap(context.Background(), in)
		check(t, st, filepath.Join(home, ".devcadence"), "bootstrap-failure-*.json")
		checkNoopInert(t, rec)
	})
	t.Run("no home falls back to the temp dir", func(t *testing.T) {
		in, _, tmp := setup(t)
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		_, st := Bootstrap(context.Background(), in)
		check(t, st, tmp, "devcadence-bootstrap-failure-*.json")
	})
	t.Run("unusable home falls back to the temp dir", func(t *testing.T) {
		in, home, tmp := setup(t)
		if err := os.WriteFile(filepath.Join(home, ".devcadence"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_, st := Bootstrap(context.Background(), in)
		check(t, st, tmp, "devcadence-bootstrap-failure-*.json")
	})
	t.Run("panic in the last resort keeps sidecar_panic", func(t *testing.T) {
		in, _, _ := setup(t)
		in.lastResortCreate = panicCreate
		rec, st := Bootstrap(context.Background(), in)
		if st.Reason != ReasonBootstrapPanic || st.SidecarError != SidecarErrPanic || st.SidecarPath != "" {
			t.Fatalf("%+v", st)
		}
		checkNoopInert(t, rec)
	})
	t.Run("last resort that cannot write keeps sidecar_panic", func(t *testing.T) {
		in, _, _ := setup(t)
		in.lastResortCreate = func(string, string, string) (*os.File, error) { return nil, errors.New("read-only") }
		_, st := Bootstrap(context.Background(), in)
		if st.SidecarError != SidecarErrPanic || st.SidecarPath != "" {
			t.Fatalf("%+v", st)
		}
	})
}

// R2-B3: every injected dependency nil or panicking at once, and a nil
// context, never panic and always return a usable recorder.
func TestBootstrapNeverPanicsAnyDependency(t *testing.T) {
	hermeticEnv(t)
	t.Run("nil deps and no usable path", func(t *testing.T) {
		rec, st := Bootstrap(context.Background(), BootstrapInput{Resolve: ResolveInput{
			Getenv: noEnv, UserHomeDir: noHome, TempDir: func() string { return "relative" }, UID: os.Getuid,
		}})
		if st.Mode != ModeDegradedNoop || st.Reason != ReasonNoUsablePath || st.SidecarError != SidecarErrNoWritableDir {
			t.Fatalf("%+v", st)
		}
		checkNoopInert(t, rec)
	})
	t.Run("everything panics", func(t *testing.T) {
		rec, st := Bootstrap(context.Background(), BootstrapInput{
			Resolve: ResolveInput{
				Getenv:      func(string) string { panic("getenv") },
				UserHomeDir: func() (string, error) { panic("home") }, TempDir: func() string { panic("tmp") },
				UID: func() int { panic("uid") }, FS: allPanicFS{},
			},
			Clock: panicClock{}, IDs: panicIDs{}, Now: func() time.Time { panic("now") },
			sidecarCreate: panicCreate, lastResortCreate: panicCreate,
		})
		if st.Mode != ModeDegradedNoop || st.Reason != ReasonBootstrapPanic || st.SidecarError != SidecarErrPanic {
			t.Fatalf("%+v", st)
		}
		checkNoopInert(t, rec)
	})
	t.Run("nil context", func(t *testing.T) {
		in, _ := bootInput(t)
		var nilCtx context.Context
		rec, st := Bootstrap(nilCtx, in)
		if rec == nil || (st.Mode != ModeDegradedNoop && st.Mode != ModeJournal) {
			t.Fatalf("%+v", st)
		}
		if st.Mode == ModeJournal {
			closeRec(t, rec)
		}
	})
}

// R2-NB(d): a directory already holding more than maxSidecarFiles sidecar
// files gets no new random-named sidecar; the fixed name is still created
// when free, and counting never fails creation in an empty directory.
func TestCreateSidecarFileCap(t *testing.T) {
	fill := func(t *testing.T, dir, prefix string, n int) {
		for i := 0; i < n; i++ {
			if err := os.WriteFile(filepath.Join(dir, prefix+strings.Repeat("x", i+1)+".json"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Run("at the cap a new file is created", func(t *testing.T) {
		dir := t.TempDir()
		fill(t, dir, "bootstrap-failure-", maxSidecarFiles)
		f, err := createSidecarFile(dir, SidecarName, "bootstrap-failure-*.json")
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		// The fixed name was free, so it was used; now 33 exist and a
		// random-named file is refused.
		if _, err := createSidecarFile(dir, SidecarName, "bootstrap-failure-*.json"); !errors.Is(err, errSidecarDirFull) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("temp directory pattern", func(t *testing.T) {
		dir := t.TempDir()
		fill(t, dir, "devcadence-bootstrap-failure-", maxSidecarFiles+1)
		fill(t, dir, "unrelated-", 40)
		if _, err := createSidecarFile(dir, "", "devcadence-bootstrap-failure-*.json"); !errors.Is(err, errSidecarDirFull) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("unreadable directory counts as empty", func(t *testing.T) {
		if n := countSidecars(filepath.Join(t.TempDir(), "missing"), "bootstrap-failure-*.json"); n != 0 {
			t.Fatalf("n %d", n)
		}
	})
}

// R2-NB(d): a full root is skipped and the next directory of the order wins.
func TestBootstrapSidecarSkipsFullDirectory(t *testing.T) {
	in, root := bootInput(t)
	in.Limits = journal.Limits{MaxRecordBytes: 100} // journal open fails after the root resolved
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= maxSidecarFiles; i++ {
		name := SidecarName
		if i > 0 {
			name = "bootstrap-failure-" + strings.Repeat("y", i) + ".json"
		}
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home, _ := hermeticEnv(t)
	in.Resolve.UserHomeDir = func() (string, error) { return home, nil }
	_, st := Bootstrap(context.Background(), in)
	if st.Reason != ReasonJournalOpenFailed || st.SidecarError != "" || filepath.Dir(st.SidecarPath) != filepath.Join(home, ".devcadence") {
		t.Fatalf("%+v", st)
	}
}

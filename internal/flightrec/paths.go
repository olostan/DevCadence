package flightrec

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/olostan/DevCadence/internal/errs"
)

// Environment variable NAMES read by path resolution (values are never
// recorded; only the source name and the resulting path are).
const (
	envTraceDir = "DEVCADENCE_TRACE_DIR"
	envHome     = "DEVCADENCE_HOME"
	envXDGState = "XDG_STATE_HOME"
)

// Stable Attempt.ErrCode values.
const (
	ErrCodeNotAbsolute     = "not_absolute"
	ErrCodeMkdirFailed     = "mkdir_failed"
	ErrCodeSymlink         = "symlink"
	ErrCodeNotOwner        = "not_owner"
	ErrCodeNotDir          = "not_dir"
	ErrCodePermFixedFailed = "perm_fixed_failed"
	ErrCodeUnset           = "unset"
	ErrCodeUIDUnknown      = "uid_unknown"
)

// Candidate source names, in resolution order.
const (
	SourceExplicitFlag = "explicit_flag"
	SourceEnvTraceDir  = "env_trace_dir"
	SourceEnvHome      = "env_home"
	SourceXDGState     = "xdg_state"
	SourceUserHome     = "user_home"
	SourceTemp         = "temp"
)

// PathFS is the filesystem seam of path resolution.
type PathFS interface {
	MkdirAll(path string, perm os.FileMode) error
	Lstat(path string) (os.FileInfo, error)
	Chmod(path string, mode os.FileMode) error
	// OwnerUID reports the owning uid; ok is false where the platform does not
	// expose it, which skips the ownership check.
	OwnerUID(fi os.FileInfo) (uid int, ok bool)
}

type osPathFS struct{}

// OSPathFS returns the operating-system implementation of PathFS.
func OSPathFS() PathFS { return osPathFS{} }

func (osPathFS) MkdirAll(p string, perm os.FileMode) error { return os.MkdirAll(p, perm) }
func (osPathFS) Lstat(p string) (os.FileInfo, error)       { return os.Lstat(p) }
func (osPathFS) Chmod(p string, m os.FileMode) error       { return os.Chmod(p, m) }
func (osPathFS) OwnerUID(fi os.FileInfo) (int, bool)       { return ownerUID(fi) }

// ResolveInput carries everything Resolve reads. Nil fields take the OS
// defaults (os.Getenv, os.UserHomeDir, os.TempDir, os.Getuid, OSPathFS).
type ResolveInput struct {
	ExplicitDir string // the --trace-dir style flag value
	Getenv      func(string) string
	UserHomeDir func() (string, error)
	TempDir     func() string
	UID         func() int
	FS          PathFS
}

// Resolution is the chosen trace root and how it was chosen.
type Resolution struct {
	Root, Source string
	// Attempts lists every candidate that was not used (set but failed, or
	// unset), in order, with a stable ErrCode.
	Attempts []Attempt
	// Fallback is true when a candidate that WAS set failed before the winner.
	Fallback bool
}

func (in ResolveInput) withDefaults() ResolveInput {
	if in.Getenv == nil {
		in.Getenv = os.Getenv
	}
	if in.UserHomeDir == nil {
		in.UserHomeDir = os.UserHomeDir
	}
	if in.TempDir == nil {
		in.TempDir = os.TempDir
	}
	if in.UID == nil {
		in.UID = os.Getuid
	}
	if in.FS == nil {
		in.FS = OSPathFS()
	}
	return in
}

// usableCode checks one candidate directory and returns "" when it is usable:
// absolute, creatable (0700), a real directory (not a symlink) owned by uid
// and mode 0700 (fixed by Chmod). The checks are Lstat-then-use (TOCTOU) in
// possibly shared directories. Only the leaf directory p is inspected: an
// ancestor that is a symlink or owned by someone else is not detected. The
// risk is mitigated by 0700, the owner check, O_EXCL files and flock, and
// accepted as documented.
func usableCode(fsys PathFS, uid int, p string) string {
	if !filepath.IsAbs(p) {
		return ErrCodeNotAbsolute
	}
	if err := fsys.MkdirAll(p, 0o700); err != nil {
		return ErrCodeMkdirFailed
	}
	fi, err := fsys.Lstat(p)
	switch {
	case err != nil:
		return ErrCodeMkdirFailed
	case fi.Mode()&os.ModeSymlink != 0:
		return ErrCodeSymlink
	case !fi.IsDir():
		return ErrCodeNotDir
	}
	if owner, ok := fsys.OwnerUID(fi); ok && uid >= 0 && owner != uid {
		return ErrCodeNotOwner
	}
	if fi.Mode().Perm() != 0o700 {
		if err := fsys.Chmod(p, 0o700); err != nil {
			return ErrCodePermFixedFailed
		}
	}
	return ""
}

// Resolve picks the trace root. Candidates, first usable wins:
//
//  1. explicit_flag  ExplicitDir
//  2. env_trace_dir  $DEVCADENCE_TRACE_DIR
//  3. env_home       $DEVCADENCE_HOME/traces
//  4. xdg_state      $XDG_STATE_HOME/devcadence/traces
//  5. user_home      <UserHomeDir>/.devcadence/traces
//  6. temp           <TempDir>/devcadence-trace-<uid>
//
// Every candidate that is not used adds an Attempt (ErrCode one of
// not_absolute, mkdir_failed, symlink, not_owner, not_dir, perm_fixed_failed,
// unset, uid_unknown). A negative UID() skips the temp candidate with
// uid_unknown. Resolve returns an error (CategoryInvalidArgument) only when
// every candidate failed; the Resolution still carries the Attempts. Resolve
// reads only the documented variable names and records the source name, never
// environment values beyond the resulting path.
func Resolve(in ResolveInput) (Resolution, error) {
	in = in.withDefaults()
	uid := in.UID()
	type candidate struct{ source, path, preset string }
	cands := []candidate{{SourceExplicitFlag, in.ExplicitDir, ""}}
	cands = append(cands, candidate{SourceEnvTraceDir, in.Getenv(envTraceDir), ""})
	home := in.Getenv(envHome)
	if home != "" {
		home = filepath.Join(home, "traces")
	}
	cands = append(cands, candidate{SourceEnvHome, home, ""})
	xdg := in.Getenv(envXDGState)
	if xdg != "" {
		xdg = filepath.Join(xdg, "devcadence", "traces")
	}
	cands = append(cands, candidate{SourceXDGState, xdg, ""})
	userHome := ""
	if h, err := in.UserHomeDir(); err == nil && h != "" {
		userHome = filepath.Join(h, ".devcadence", "traces")
	}
	cands = append(cands, candidate{SourceUserHome, userHome, ""})
	tmp := candidate{SourceTemp, "", ""}
	if uid < 0 {
		tmp.preset = ErrCodeUIDUnknown
	} else {
		tmp.path = filepath.Join(in.TempDir(), "devcadence-trace-"+strconv.Itoa(uid))
	}
	cands = append(cands, tmp)

	var res Resolution
	for _, c := range cands {
		code := c.preset
		switch {
		case code != "":
		case c.path == "":
			code = ErrCodeUnset
		default:
			code = usableCode(in.FS, uid, c.path)
		}
		if code == "" {
			res.Root, res.Source = c.path, c.source
			return res, nil
		}
		if code != ErrCodeUnset {
			res.Fallback = true
		}
		res.Attempts = append(res.Attempts, Attempt{Source: c.source, Path: c.path, ErrCode: code})
	}
	return res, errs.New(errs.CategoryInvalidArgument, "flightrec: no usable trace directory among %d candidates", len(cands))
}

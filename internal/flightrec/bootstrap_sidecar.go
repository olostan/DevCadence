package flightrec

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Last-resort bootstrap-failure sidecar. When Bootstrap cannot produce a
// journal (no usable trace directory, id failure, journal open failure,
// STREAM_STARTED failure, panic) nothing else reaches disk, so an early
// startup failure would be invisible to the offline exporter. The sidecar is a
// single small JSON document written with plain os calls (no journal), to the
// first writable directory of a fixed order:
//
//  1. the resolved trace root (only when a root was resolved, i.e. only the
//     journal failed): bootstrap-failure.json, or bootstrap-failure-<random>.json
//     when that name already exists;
//  2. <UserHomeDir>/.devcadence (created 0700; symlink/ownership rules of
//     the trace roots): same file names;
//  3. <TempDir>/devcadence-trace-<uid> (same rules; skipped when uid < 0): same
//     file names;
//  4. <TempDir> itself, as devcadence-bootstrap-failure-<random>.json.
//
// Files are created O_EXCL with mode 0600. The content holds only sanitized
// fields: schema version, write time (injected clock), stable reason code,
// writer version, node id and its source when known, the failed path attempts
// (source, sanitized path and error code only; never error text), and the
// recorder counters. It never contains environment values or file contents and
// is bounded by MaxSidecarBytes. The Phase 2 exporter scans
// <root>/bootstrap-failure*.json for the resolved trace roots and the
// directories above.
const (
	// SidecarName is the preferred sidecar file name inside a directory.
	SidecarName = "bootstrap-failure.json"
	// SidecarSchemaVersion is the schema_version of the sidecar document.
	SidecarSchemaVersion = 1
	// MaxSidecarBytes bounds the sidecar document.
	MaxSidecarBytes = 64 << 10

	// SidecarErrNoWritableDir is Status.SidecarError when no directory of the
	// order was writable.
	SidecarErrNoWritableDir = "no_writable_dir"
	// SidecarErrPanic is Status.SidecarError when writing the sidecar panicked.
	SidecarErrPanic = "sidecar_panic"

	// maxSidecarAttempts bounds the attempts list, which keeps the document
	// far below MaxSidecarBytes (each attempt is at most ~0.4 KiB).
	maxSidecarAttempts = 32

	nodeIDSourceFile      = "file"
	nodeIDSourceEphemeral = "ephemeral"
)

type sidecarAttempt struct {
	Source  string `json:"source"`
	Path    string `json:"path"`
	ErrCode string `json:"error_code"`
}

type sidecarDoc struct {
	SchemaVersion     int              `json:"schema_version"`
	WrittenAt         string           `json:"written_at"`
	Reason            string           `json:"reason"`
	WriterVersion     string           `json:"writer_version"`
	NodeID            string           `json:"node_id,omitempty"`
	NodeIDSource      string           `json:"node_id_source,omitempty"`
	Attempts          []sidecarAttempt `json:"attempts"`
	AttemptsTruncated bool             `json:"attempts_truncated,omitempty"`
	Stats             Stats            `json:"stats"`
}

// sidecarCreate creates the sidecar file in dir: preferred (when non-empty)
// first, O_EXCL, falling back to a random name from pattern when it exists.
type sidecarCreate func(dir, preferred, pattern string) (*os.File, error)

// maxSidecarFiles bounds accumulation: a new random-named sidecar is not
// created in a directory that already holds more than this many sidecar files.
const maxSidecarFiles = 32

var errSidecarDirFull = errors.New("flightrec: too many bootstrap-failure files")

// countSidecars counts the entries of dir matching the sidecar glob derived
// from pattern ("bootstrap-failure-*.json" counts "bootstrap-failure*.json").
// It reads the directory with plain os calls: createSidecarFile is itself the
// replaceable creation seam, so tests stay deterministic through it.
func countSidecars(dir, pattern string) int {
	glob := strings.Replace(pattern, "-*.json", "*.json", 1)
	ents, _ := os.ReadDir(dir) // an unreadable directory counts as empty; creation then decides
	n := 0
	for _, e := range ents {
		if ok, _ := filepath.Match(glob, e.Name()); ok {
			n++
		}
	}
	return n
}

func createSidecarFile(dir, preferred, pattern string) (*os.File, error) {
	if preferred != "" {
		f, err := os.OpenFile(filepath.Join(dir, preferred), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
	}
	if countSidecars(dir, pattern) > maxSidecarFiles {
		return nil, errSidecarDirFull
	}
	return os.CreateTemp(dir, pattern) // O_EXCL, 0600, random suffix
}

// writeSidecarFile creates a sidecar in dir through create and writes body; ""
// means the directory was not usable (a partial file is removed).
func writeSidecarFile(create sidecarCreate, dir, preferred, pattern string, body []byte) string {
	f, err := create(dir, preferred, pattern)
	if err != nil {
		return ""
	}
	_, werr := f.Write(body)
	serr := f.Sync()
	cerr := f.Close()
	if errors.Join(werr, serr, cerr) != nil {
		_ = os.Remove(f.Name())
		return ""
	}
	return f.Name()
}

// lastResortSidecar is the final attempt when the injected filesystem or
// functions panicked: it uses ONLY plain os calls (os.UserHomeDir, os.TempDir,
// os.MkdirAll, and create, which is O_EXCL, 0600 with a random suffix) and no
// ownership checks, in its own recover. Order: <UserHomeDir>/.devcadence, then
// <TempDir>. Names are always random (bootstrap-failure-<random>.json or
// devcadence-bootstrap-failure-<random>.json) so an existing file or symlink is
// never followed or overwritten.
func lastResortSidecar(create sidecarCreate, body []byte) (path string) {
	defer func() {
		if recover() != nil {
			path = ""
		}
	}()
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		d := filepath.Join(h, ".devcadence")
		if os.MkdirAll(d, 0o700) == nil {
			if p := writeSidecarFile(create, d, "", "bootstrap-failure-*.json", body); p != "" {
				return p
			}
		}
	}
	return writeSidecarFile(create, os.TempDir(), "", "devcadence-bootstrap-failure-*.json", body)
}

// buildSidecar renders the sanitized document.
func buildSidecar(san *Sanitizer, now time.Time, reason, writerVersion, nodeID, nodeSource string, attempts []Attempt, stats Stats) []byte {
	var t tally
	doc := sidecarDoc{
		SchemaVersion: SidecarSchemaVersion, WrittenAt: now.UTC().Format(time.RFC3339Nano),
		Reason: san.Scalar(ScalarID, reason), WriterVersion: san.text(&t, writerVersion),
		NodeIDSource: nodeSource, Attempts: []sidecarAttempt{}, Stats: stats,
	}
	if nodeID != "" {
		doc.NodeID = san.Scalar(ScalarID, nodeID)
	}
	for i, a := range attempts {
		if i >= maxSidecarAttempts {
			doc.AttemptsTruncated = true
			break
		}
		doc.Attempts = append(doc.Attempts, sidecarAttempt{
			Source: san.Scalar(ScalarID, a.Source), Path: san.Scalar(ScalarLocator, a.Path), ErrCode: san.Scalar(ScalarID, a.ErrCode),
		})
	}
	b, _ := json.Marshal(doc) // plain strings and numbers always marshal
	return b
}

// writeSidecar writes body to the first writable directory of the documented
// order and returns the file path, or a stable error code. It never panics.
func writeSidecar(in ResolveInput, create sidecarCreate, journalRoot string, body []byte) (path, code string) {
	defer func() {
		if recover() != nil {
			path, code = "", SidecarErrPanic
		}
	}()
	in = in.withDefaults()
	try := func(dir, preferred, pattern string) string {
		return writeSidecarFile(create, dir, preferred, pattern, body)
	}
	const dirPattern = "bootstrap-failure-*.json"
	if journalRoot != "" {
		if p := try(journalRoot, SidecarName, dirPattern); p != "" {
			return p, ""
		}
	}
	uid := in.UID()
	if h, err := in.UserHomeDir(); err == nil && h != "" {
		d := filepath.Join(h, ".devcadence")
		if usableCode(in.FS, uid, d) == "" {
			if p := try(d, SidecarName, dirPattern); p != "" {
				return p, ""
			}
		}
	}
	tmp := in.TempDir()
	if uid >= 0 {
		d := filepath.Join(tmp, "devcadence-trace-"+strconv.Itoa(uid))
		if usableCode(in.FS, uid, d) == "" {
			if p := try(d, SidecarName, dirPattern); p != "" {
				return p, ""
			}
		}
	}
	if filepath.IsAbs(tmp) {
		if p := try(tmp, "", "devcadence-bootstrap-failure-*.json"); p != "" {
			return p, ""
		}
	}
	return "", SidecarErrNoWritableDir
}

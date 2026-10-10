package journal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
	"github.com/olostan/DevCadence/internal/ids"
)

// Durability is the class of an append.
type Durability uint8

// Durability classes; they map 1:1 to wire.Durability.
const (
	// DurabilityCritical appends are written and fsynced before AppendCritical returns.
	DurabilityCritical Durability = Durability(wire.DurabilityCritical)
	// DurabilityDiagnostic appends are queued, never block and never fail the caller.
	DurabilityDiagnostic Durability = Durability(wire.DurabilityDiagnostic)
)

const (
	defaultQueueSize = 1024
	defaultMaxBatch  = 64
	maxDetailBytes   = 1024
	maxAttempts      = 8
	maxAttemptPath   = 256
	maxAttemptField  = 64
	maxHealthField   = 128
	truncSuffixSlack = 40
)

// Config configures Open.
type Config struct {
	// Root is the trace root. Open creates and syncs directories only at or
	// below Root.
	Root string
	// Dir is the stream directory (<root>/nodes/<node>/<run>/<stream>); it must
	// be under Root and is created with mode 0700.
	Dir                         string
	NodeID, RuntimeID, StreamID string
	Limits                      Limits
	// QueueSize is the diagnostic queue capacity (default 1024).
	QueueSize int
	// MaxBatch is the number of diagnostic records written per mutex hold (default 64).
	MaxBatch int
	// FlushEvery is the diagnostic fsync cadence; 0 disables the background
	// ticker (tests call Flush).
	FlushEvery time.Duration
	// Clock and IDs supply wall stamps and event ids; Mono returns monotonic
	// nanoseconds since runtime start. All three are required.
	Clock clock.Clock
	IDs   ids.Source
	Mono  func() int64
	// FS is the fault seam; nil means the operating-system filesystem.
	FS            FS
	WriterVersion string

	// Unexported test seams.
	ownerUID   func(os.FileInfo) (uid int, ok bool)
	selfUID    func() int
	tickC      <-chan time.Time
	afterBatch func()
}

// Stats is a snapshot of writer counters.
type Stats struct {
	// Appended counts non-health records written; Critical is the critical subset.
	Appended, Critical uint64
	// Dropped is the cumulative number of diagnostic records dropped.
	Dropped      uint64
	SegmentIndex uint32
	LastSequence uint64
	// Broken is true while the active segment is poisoned (the next append
	// attempts a fresh segment).
	Broken    bool
	LastError string
}

type pending struct {
	segStart  wire.HealthKind
	segCode   string
	segOffset int64
	writerErr bool
	errDetail string
}

// Writer is a single-writer, append-only journal for one stream.
//
// Durability classes: AppendCritical returns nil only after the complete frame
// was written and File.Sync succeeded (the fsync also makes every earlier
// diagnostic record of that segment durable). AppendDiagnostic is a bounded,
// non-blocking, drop-on-full queue; a diagnostic record that is queued or
// written but not followed by a later successful Critical append, Flush, tick,
// rotation or Close may be lost on a crash without trace. A START without END
// on disk means unresolved, not failed.
//
// flock is advisory and unreliable on NFS and some network filesystems, where
// single-writer enforcement is best effort.
type Writer struct {
	cfg  Config
	lim  Limits
	fs   FS
	lock *os.File

	mu        sync.Mutex
	active    File
	size      int
	nextSeq   uint64
	segIndex  uint32
	unsynced  bool
	shut      bool
	broken    bool
	lastErr   string
	pend      pending
	reported  uint64
	appended  uint64
	critical  uint64
	cmu       sync.RWMutex
	closed    bool
	queue     chan *wire.JournalRecord
	wake      chan struct{}
	done      chan struct{}
	exited    chan struct{}
	dropped   atomic.Uint64
	maxBatch  int
	afterHook func()
}

func ioErr(op string, err error) error {
	return errs.Wrap(errs.CategoryInternal, err, "journal: %s", op)
}

func invalid(format string, args ...any) error {
	return errs.New(errs.CategoryInvalidArgument, "journal: "+format, args...)
}

// normalize validates cfg and fills defaults.
func (c Config) normalize() (Config, error) {
	var err error
	if c.Limits, err = c.Limits.Normalize(); err != nil {
		return c, err
	}
	switch {
	case c.Root == "" || c.Dir == "":
		return c, invalid("Root and Dir are required")
	case c.Clock == nil || c.IDs == nil || c.Mono == nil:
		return c, invalid("Clock, IDs and Mono are required")
	case c.QueueSize < 0 || c.MaxBatch < 0 || c.FlushEvery < 0:
		return c, invalid("QueueSize, MaxBatch and FlushEvery must not be negative")
	}
	for _, id := range []string{c.NodeID, c.RuntimeID, c.StreamID} {
		if id == "" || len(id) > maxIDBytes || !utf8.ValidString(id) {
			return c, invalid("NodeID, RuntimeID and StreamID must be non-empty UTF-8 of at most %d bytes", maxIDBytes)
		}
	}
	if len(c.WriterVersion) > maxVersionByte || !utf8.ValidString(c.WriterVersion) {
		return c, invalid("WriterVersion must be UTF-8 of at most %d bytes", maxVersionByte)
	}
	rel, err := filepath.Rel(c.Root, c.Dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return c, invalid("Dir %q is not under Root %q", c.Dir, c.Root)
	}
	if c.QueueSize == 0 {
		c.QueueSize = defaultQueueSize
	}
	if c.MaxBatch == 0 {
		c.MaxBatch = defaultMaxBatch
	}
	if c.FS == nil {
		c.FS = OSFS()
	}
	if c.ownerUID == nil {
		c.ownerUID = ownerUID
	}
	if c.selfUID == nil {
		c.selfUID = os.Getuid
	}
	return c, nil
}

// newDirs lists, outermost first, the directories between Root and Dir that do
// not exist yet.
func newDirs(fsys FS, root, dir string) ([]string, error) {
	rel, _ := filepath.Rel(root, dir)
	chain := []string{root}
	if rel != "." {
		cur := root
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			cur = filepath.Join(cur, part)
			chain = append(chain, cur)
		}
	}
	var created []string
	for _, d := range chain {
		_, err := fsys.Lstat(d)
		if errors.Is(err, fs.ErrNotExist) {
			created = append(created, d)
		} else if err != nil {
			return nil, ioErr("lstat "+d, err)
		}
	}
	return created, nil
}

// prepareDir creates Dir, verifies it is a real directory owned by this user,
// and syncs every directory this call created (and its parent).
func prepareDir(cfg Config) error {
	created, err := newDirs(cfg.FS, cfg.Root, cfg.Dir)
	if err != nil {
		return err
	}
	if err := cfg.FS.MkdirAll(cfg.Dir, 0o700); err != nil {
		return ioErr("mkdir "+cfg.Dir, err)
	}
	fi, err := cfg.FS.Lstat(cfg.Dir)
	if err != nil {
		return ioErr("lstat "+cfg.Dir, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return invalid("%s is not a real directory", cfg.Dir)
	}
	if uid, ok := cfg.ownerUID(fi); ok && uid != cfg.selfUID() {
		return invalid("%s is owned by uid %d, not %d", cfg.Dir, uid, cfg.selfUID())
	}
	for _, d := range created {
		if err := cfg.FS.SyncDir(d); err != nil {
			return ioErr("sync "+d, err)
		}
		if err := cfg.FS.SyncDir(filepath.Dir(d)); err != nil {
			return ioErr("sync parent of "+d, err)
		}
	}
	return nil
}

// Open creates the stream directory, takes the single-writer lock, recovers
// the sequence from any existing segments (never modifying them) and starts a
// new segment whose first record is a JournalHealth record.
func Open(ctx context.Context, cfg Config) (w *Writer, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if cfg, err = cfg.normalize(); err != nil {
		return nil, err
	}
	if err = prepareDir(cfg); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(cfg.Dir, "LOCK"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, ioErr("open LOCK", err)
	}
	defer func() {
		if err != nil {
			_ = unlockFile(lock)
			_ = lock.Close()
		}
	}()
	if err = lockFile(lock); err != nil {
		return nil, err
	}
	w = &Writer{
		cfg: cfg, lim: cfg.Limits, fs: cfg.FS, lock: lock, nextSeq: 1, maxBatch: cfg.MaxBatch,
		queue: make(chan *wire.JournalRecord, cfg.QueueSize), wake: make(chan struct{}, 1),
		done: make(chan struct{}), exited: make(chan struct{}), afterHook: cfg.afterBatch,
	}
	segs, err := segmentNames(cfg.FS, cfg.Dir)
	if err != nil {
		return nil, ioErr("list segments", err)
	}
	w.pend.segStart = wire.HealthStreamStarted
	if len(segs) > 0 {
		rep, serr := ScanStream(cfg.FS, cfg.Dir, ScanOptions{Limits: cfg.Limits}, nil)
		if serr != nil {
			err = ioErr("scan existing segments", serr)
			return nil, err
		}
		w.segIndex = segs[len(segs)-1].index
		w.nextSeq = rep.LastSequence + 1
		w.pend.segStart = wire.HealthRecoveryReopen
		w.pend.segCode, w.pend.segOffset = reopenSummary(rep)
	}
	w.mu.Lock()
	err = w.openNextLocked()
	if err == nil {
		err = w.syncLocked()
	}
	w.mu.Unlock()
	if err != nil {
		return nil, err
	}
	go w.run()
	return w, nil
}

// reopenSummary renders the warning codes of a recovery scan and the offset of
// the first damage.
func reopenSummary(rep StreamReport) (code string, offset int64) {
	var codes []string
	seen := map[FindingCode]bool{}
	first := true
	for _, f := range rep.Findings {
		if f.Severity != SeverityWarning {
			continue
		}
		if first {
			offset, first = f.Offset, false
		}
		if !seen[f.Code] {
			seen[f.Code] = true
			codes = append(codes, string(f.Code))
		}
	}
	if len(codes) == 0 {
		return "none", 0
	}
	return strings.Join(codes, ","), offset
}

func (w *Writer) stamp() *wire.Stamp {
	return &wire.Stamp{WallUnixNanos: w.cfg.Clock.Now().UnixNano(), MonoNanos: w.cfg.Mono()}
}

func (w *Writer) pendingCount() int {
	n := 0
	if w.pend.segStart != 0 {
		n++
	}
	if w.pend.writerErr {
		n++
	}
	if w.dropped.Load() > w.reported {
		n++
	}
	return n
}

// poisonLocked abandons the active segment after an I/O failure. The segment
// is never appended to again; its leftover bytes are never touched.
func (w *Writer) poisonLocked(err error) {
	if w.active != nil {
		_ = w.active.Close()
		w.active = nil
	}
	w.broken = true
	w.lastErr = err.Error()
	w.pend.writerErr = true
	w.pend.errDetail = err.Error()
}

func writeAll(f File, b []byte) error {
	n, err := f.Write(b)
	if err == nil && n < len(b) {
		err = io.ErrShortWrite
	}
	return err
}

// newSegmentLocked creates the next segment. The index is consumed on every
// attempt, successful or not, so a retry never collides on O_EXCL.
func (w *Writer) newSegmentLocked() error {
	w.segIndex++
	if w.segIndex > maxSegmentIdx {
		w.poisonLocked(ErrSegmentLimit)
		return ErrSegmentLimit
	}
	st := w.stamp()
	meta, _ := (&wire.SegmentHeader{
		SchemaVersion: wire.SchemaVersion, NodeID: w.cfg.NodeID, RuntimeID: w.cfg.RuntimeID, StreamID: w.cfg.StreamID,
		SegmentIndex: w.segIndex, FirstSequence: w.nextSeq, CreatedAt: st, WriterPID: uint32(os.Getpid()),
		WriterVersion: w.cfg.WriterVersion, MaxRecordBytes: uint32(w.lim.MaxRecordBytes),
		MonoOriginWallUnixNanos: st.WallUnixNanos - st.MonoNanos,
	}).Marshal() // cannot fail: identifiers and version were validated as UTF-8
	hdr := encodeHeader(meta)
	f, err := w.fs.OpenFile(filepath.Join(w.cfg.Dir, segmentName(w.segIndex)), os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		err = writeAll(f, hdr)
	}
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = w.fs.SyncDir(w.cfg.Dir)
	}
	if err != nil {
		if f != nil {
			_ = f.Close()
		}
		w.poisonLocked(err)
		return ioErr("create segment", err)
	}
	w.active, w.size, w.unsynced, w.broken = f, len(hdr), false, false
	return nil
}

// openNextLocked starts a segment and writes its pending health records
// (segment-start health first).
func (w *Writer) openNextLocked() error {
	if err := w.newSegmentLocked(); err != nil {
		return err
	}
	return w.flushPendingHealthLocked()
}

// ensureActiveLocked recovers from a poisoned writer with a fresh segment.
func (w *Writer) ensureActiveLocked() error {
	if w.shut {
		return ErrClosed
	}
	if w.active != nil {
		return nil
	}
	if w.pend.segStart == 0 {
		w.pend.segStart = wire.HealthSegmentRotated
	}
	return w.openNextLocked()
}

// rotateLocked syncs and closes the active segment and opens the next one.
func (w *Writer) rotateLocked() error {
	if err := w.syncLocked(); err != nil {
		return err
	}
	_ = w.active.Close() // already synced; a close failure loses nothing
	w.active = nil
	w.pend.segStart = wire.HealthSegmentRotated
	return w.openNextLocked()
}

// syncLocked fsyncs the active segment when it holds unsynced data.
func (w *Writer) syncLocked() error {
	if w.active == nil || !w.unsynced {
		return nil
	}
	if err := w.active.Sync(); err != nil {
		w.poisonLocked(err)
		return ioErr("fsync", err)
	}
	w.unsynced = false
	return nil
}

// writeFrameLocked performs the single Write of one encoded frame. It never
// rotates and never appends.
func (w *Writer) writeFrameLocked(payload []byte) error {
	frame := encodeFrame(payload)
	if err := writeAll(w.active, frame); err != nil {
		w.poisonLocked(err)
		return ioErr("write record", err)
	}
	w.size += len(frame)
	w.nextSeq++
	w.unsynced = true
	return nil
}

func (w *Writer) envelope(durability Durability) wire.JournalRecord {
	return wire.JournalRecord{
		SchemaVersion: wire.SchemaVersion, EventID: w.cfg.IDs.New("evt"), NodeID: w.cfg.NodeID, RuntimeID: w.cfg.RuntimeID,
		StreamSequence: w.nextSeq, Durability: wire.Durability(durability), StreamID: w.cfg.StreamID,
	}
}

// writeHealthLocked is the only producer of health frames.
func (w *Writer) writeHealthLocked(h *wire.JournalHealth) error {
	h.At = w.stamp()
	rec := w.envelope(DurabilityDiagnostic)
	rec.Type = wire.RecordTypeJournalHealth
	rec.Health = capHealth(h)
	payload, _ := rec.Marshal() // cannot fail: capHealth leaves only valid UTF-8
	return w.writeFrameLocked(payload)
}

// flushPendingHealthLocked writes the pending segment-start, WRITER_ERROR and
// DROPPED_DIAGNOSTICS records (in that order) into the active segment.
func (w *Writer) flushPendingHealthLocked() error {
	if w.pend.segStart != 0 {
		h := &wire.JournalHealth{Kind: w.pend.segStart, DetailCode: w.pend.segCode, Offset: w.pend.segOffset}
		if err := w.writeHealthLocked(h); err != nil {
			return err
		}
		w.pend.segStart, w.pend.segCode, w.pend.segOffset = 0, "", 0
	}
	if w.pend.writerErr {
		h := &wire.JournalHealth{Kind: wire.HealthWriterError, DetailCode: "writer_error", Detail: w.pend.errDetail}
		if err := w.writeHealthLocked(h); err != nil {
			return err
		}
		w.pend.writerErr, w.pend.errDetail = false, ""
	}
	if delta := w.dropped.Load() - w.reported; delta > 0 {
		h := &wire.JournalHealth{Kind: wire.HealthDroppedDiagnostics, DetailCode: "dropped_diagnostics", DroppedCount: delta}
		if err := w.writeHealthLocked(h); err != nil {
			return err
		}
		w.reported += delta
	}
	return nil
}

// flushHealthLocked persists pending health, opening a fresh segment when the
// writer is poisoned.
func (w *Writer) flushHealthLocked() error {
	if w.shut {
		return ErrClosed
	}
	if w.pendingCount() == 0 {
		return nil
	}
	if err := w.ensureActiveLocked(); err != nil {
		return err
	}
	return w.flushPendingHealthLocked()
}

func truncUTF8(s string, limit int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= limit {
		return s
	}
	cut := limit
	for !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func truncDetail(s string, limit int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= limit {
		return s
	}
	cut := len(truncUTF8(s, limit-truncSuffixSlack))
	return fmt.Sprintf("%s...[truncated %d bytes]", s[:cut], len(s)-cut)
}

// capHealth returns a copy of h that marshals to at most MaxHealthBytes:
// detail <= 1024 bytes, <= 8 attempts, bounded path/source/code fields, and
// attempts emptied when the record would still be too large. It never fails.
func capHealth(in *wire.JournalHealth) *wire.JournalHealth {
	h := *in
	h.Unknown = nil
	h.DetailCode = truncUTF8(h.DetailCode, maxHealthField)
	h.SelectedSource = truncUTF8(h.SelectedSource, maxHealthField)
	h.Attempts = nil
	extra := len(in.Attempts) - maxAttempts
	for i, a := range in.Attempts {
		if i == maxAttempts {
			break
		}
		h.Attempts = append(h.Attempts, wire.PathAttempt{
			Source: truncUTF8(a.Source, maxAttemptField), Path: truncUTF8(a.Path, maxAttemptPath),
			ErrorCode: truncUTF8(a.ErrorCode, maxAttemptField),
		})
	}
	detail := in.Detail
	if extra > 0 {
		detail = fmt.Sprintf("%s [+%d attempts dropped]", truncDetail(detail, maxDetailBytes-truncSuffixSlack), extra)
	}
	h.Detail = truncDetail(detail, maxDetailBytes)
	if b, _ := h.Marshal(); len(b) > MaxHealthBytes {
		h.Attempts = nil
	}
	return &h
}

// encodeLocked fills the envelope and marshals body as the next record.
func (w *Writer) encodeLocked(body *wire.JournalRecord, class Durability) ([]byte, error) {
	rec := *body
	rec.Type = rec.BodyType()
	if rec.Type == wire.RecordTypeUnspecified {
		return nil, invalid("record has no body")
	}
	env := w.envelope(class)
	rec.SchemaVersion, rec.EventID, rec.NodeID, rec.RuntimeID = env.SchemaVersion, env.EventID, env.NodeID, env.RuntimeID
	rec.StreamSequence, rec.Durability, rec.StreamID = env.StreamSequence, env.Durability, env.StreamID
	if rec.Health != nil {
		rec.Health = capHealth(rec.Health)
	}
	payload, err := rec.Marshal()
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "journal: marshal record")
	}
	if len(payload) > w.lim.MaxRecordBytes {
		return nil, ErrRecordTooLarge
	}
	return payload, nil
}

// appendLocked appends one non-health record (see the package contract for the
// rotation and re-fill rules).
func (w *Writer) appendLocked(body *wire.JournalRecord, class Durability) error {
	if w.shut {
		return ErrClosed
	}
	if err := w.ensureActiveLocked(); err != nil {
		return err
	}
	payload, err := w.encodeLocked(body, class)
	if err != nil {
		return err
	}
	est := frameOverhead + len(payload) + 10 // the sequence varint may grow after re-fill
	reserve := w.pendingCount() * (frameOverhead + MaxHealthBytes)
	if w.size+reserve+est > w.lim.MaxSegmentBytes {
		err = w.rotateLocked()
	} else {
		err = w.flushPendingHealthLocked()
	}
	if err != nil {
		return err
	}
	if payload, err = w.encodeLocked(body, class); err != nil { // re-fill: health consumed sequences
		return err
	}
	if err = w.writeFrameLocked(payload); err != nil {
		return err
	}
	w.appended++
	if class == DurabilityCritical {
		w.critical++
	}
	return nil
}

// AppendCritical appends body (a record with exactly one body member set; the
// writer fills the envelope) and returns nil only after the frame was written
// and fsynced. ctx is consulted only before any lock is taken: a started
// write or fsync is never aborted. The writer does not modify body.
func (w *Writer) AppendCritical(ctx context.Context, body *wire.JournalRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if body == nil {
		return ErrNilBody
	}
	w.cmu.RLock()
	closed := w.closed
	w.cmu.RUnlock()
	if closed {
		return ErrClosed
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.appendLocked(body, DurabilityCritical); err != nil {
		return err
	}
	return w.syncLocked()
}

// AppendDiagnostic queues body and never blocks or returns an I/O error. It
// returns false when the record was dropped (queue full or writer closed); the
// drop is counted and later reported as DROPPED_DIAGNOSTICS. The caller must
// not mutate body's members after the call.
func (w *Writer) AppendDiagnostic(body *wire.JournalRecord) (accepted bool) {
	w.cmu.RLock()
	defer w.cmu.RUnlock()
	if w.closed || body == nil { // a nil body is an invalid argument: counted as dropped
		w.dropped.Add(1)
		return false
	}
	cp := *body
	select {
	case w.queue <- &cp:
	default:
		w.dropped.Add(1)
		return false
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return true
}

// drainLocked writes up to limit queued diagnostic records and returns how
// many it took from the queue. A record that cannot be appended is dropped.
func (w *Writer) drainLocked(limit int) int {
	for i := 0; i < limit; i++ {
		select {
		case body := <-w.queue:
			if err := w.appendLocked(body, DurabilityDiagnostic); err != nil {
				w.dropped.Add(1)
			}
		default:
			return i
		}
	}
	return limit
}

func (w *Writer) run() {
	defer close(w.exited)
	tickC := w.cfg.tickC
	if tickC == nil && w.cfg.FlushEvery > 0 {
		t := time.NewTicker(w.cfg.FlushEvery)
		defer t.Stop()
		tickC = t.C
	}
	for {
		select {
		case <-w.done:
			return
		case <-w.wake:
			w.drainAll()
		case <-tickC:
			w.tick()
		}
	}
}

// drainAll empties the queue in batches, releasing the mutex between batches so
// a critical append waits for at most one batch.
func (w *Writer) drainAll() {
	for {
		w.mu.Lock()
		n := w.drainLocked(w.maxBatch)
		w.mu.Unlock()
		if w.afterHook != nil {
			w.afterHook()
		}
		if n < w.maxBatch {
			return
		}
	}
}

// tick is the periodic diagnostic fsync; a failure poisons the segment and is
// reported by the next append.
func (w *Writer) tick() {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.syncLocked()
}

// Flush writes the diagnostic records queued at call time, then pending health,
// and fsyncs. ctx is consulted only before locking.
func (w *Writer) Flush(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.cmu.RLock()
	closed := w.closed
	w.cmu.RUnlock()
	if closed {
		return ErrClosed
	}
	// One mutex hold per batch of what is queued now; a drain that finds the
	// queue already emptied (by the background goroutine) returns immediately.
	for batches := (len(w.queue) + w.maxBatch - 1) / w.maxBatch; batches > 0; batches-- {
		w.mu.Lock()
		w.drainLocked(w.maxBatch)
		w.mu.Unlock()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.shut { // Close completed between the closed check and this lock
		return ErrClosed
	}
	if err := w.flushHealthLocked(); err != nil {
		return err
	}
	return w.syncLocked()
}

// Stats returns a snapshot of the counters.
func (w *Writer) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Stats{
		Appended: w.appended, Critical: w.critical, Dropped: w.dropped.Load(), SegmentIndex: w.segIndex,
		LastSequence: w.nextSeq - 1, Broken: w.broken, LastError: w.lastErr,
	}
}

// Close drains the queue, writes pending health, fsyncs, closes the segment
// and releases the lock. It is idempotent: the first caller owns teardown and
// later or concurrent calls return nil immediately. If ctx expires first, the
// wait for the drain goroutine is abandoned, queued records are counted as
// dropped, the lock is still released, and ctx.Err() is part of the result.
func (w *Writer) Close(ctx context.Context) error {
	w.cmu.Lock()
	if w.closed {
		w.cmu.Unlock()
		return nil
	}
	w.closed = true
	close(w.done)
	w.cmu.Unlock()

	select {
	case <-w.exited:
	case <-ctx.Done():
	}
	cerr := ctx.Err()

	w.mu.Lock()
	defer w.mu.Unlock()
	if cerr == nil {
		w.drainLocked(cap(w.queue))
	} else {
		w.discardQueueLocked()
	}
	ferr := w.flushHealthLocked()
	serr := w.syncLocked()
	if w.active != nil {
		_ = w.active.Close()
		w.active = nil
	}
	w.shut = true
	_ = unlockFile(w.lock)
	return errors.Join(cerr, ferr, serr, w.lock.Close())
}

func (w *Writer) discardQueueLocked() {
	for {
		select {
		case <-w.queue:
			w.dropped.Add(1)
		default:
			return
		}
	}
}

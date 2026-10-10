package journal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/olostan/DevCadence/internal/flightrec/wire"
)

// FindingCode names one kind of damage or anomaly found by the scanner.
type FindingCode string

// Finding codes. UnknownRecordType and LiveTail are informational; every other
// code is a warning.
const (
	TornTail          FindingCode = "TORN_TAIL"
	BadHeader         FindingCode = "BAD_HEADER"
	BadSync           FindingCode = "BAD_SYNC"
	BadLength         FindingCode = "BAD_LENGTH"
	BadCRC            FindingCode = "BAD_CRC"
	DecodeError       FindingCode = "DECODE_ERROR"
	SeqGap            FindingCode = "SEQ_GAP"
	SeqRegression     FindingCode = "SEQ_REGRESSION"
	SegmentMissing    FindingCode = "SEGMENT_MISSING"
	SegmentOrder      FindingCode = "SEGMENT_ORDER"
	ResyncAbandoned   FindingCode = "RESYNC_ABANDONED"
	UnknownRecordType FindingCode = "UNKNOWN_RECORD_TYPE"
	LiveTail          FindingCode = "LIVE_TAIL"
)

// Severity classifies a finding. Only warnings make a report unclean.
type Severity uint8

// Severity values.
const (
	SeverityInfo Severity = iota + 1
	SeverityWarning
)

// Severity returns the fixed severity of the code.
func (c FindingCode) Severity() Severity {
	if c == UnknownRecordType || c == LiveTail {
		return SeverityInfo
	}
	return SeverityWarning
}

// Finding is one typed observation about damage. Data damage is never an error
// return of the scanner.
type Finding struct {
	Code         FindingCode
	Severity     Severity
	Segment      uint32
	Offset       int64
	SkippedBytes int64
	Expected     uint64
	Got          uint64
	Detail       string
}

// ScanOptions tunes the scanner.
type ScanOptions struct {
	// Limits supplies MaxRecordBytes (the zero value means the default).
	Limits Limits
	// MaxResyncBytes bounds the search for the next valid frame per damage
	// region (default 16 MiB).
	MaxResyncBytes int64
	// AssumeLiveTail downgrades a torn tail on the final segment to LIVE_TAIL
	// when a writer currently holds the stream lock (ScanStream only).
	AssumeLiveTail bool
	// PrevLastSequence seeds the segment-boundary check of ScanSegment: the last
	// sequence of the previous segment (0 = this is the first segment).
	PrevLastSequence uint64

	skipBoundary bool
	// hasPrev marks that a previous segment with a valid header exists even
	// when PrevLastSequence is 0 (a header-only first segment).
	hasPrev bool
}

const (
	defaultMaxResync = 16 << 20
	scanChunk        = 64 << 10
	// schemaVersionTag is the first byte of every valid record payload
	// (field 1, varint).
	schemaVersionTag = 0x08
	// resyncBudgetFactor scales MaxResyncBytes into the per-damage-region
	// budget of candidate payload bytes that may be read and checksummed.
	resyncBudgetFactor = 4
)

// Scanned is one record delivered to the visitor.
type Scanned struct {
	Record  *wire.JournalRecord
	Segment uint32
	Offset  int64
}

// SegmentReport summarizes one scanned segment.
type SegmentReport struct {
	Index         uint32
	Size          int64
	HeaderOK      bool
	FirstSequence uint64 // first_sequence from the header
	LastSequence  uint64 // last sequence delivered (0 when none)
	Records       uint64
	Findings      []Finding
	// CleanEOF is true when the segment ended exactly at a frame boundary with
	// no warning in it.
	CleanEOF bool
}

// StreamReport summarizes a whole stream directory.
type StreamReport struct {
	Segments []SegmentReport
	// Findings holds every finding ordered by (Segment, Offset); stream-level
	// findings carry Offset 0.
	Findings []Finding
	Records  uint64
	// FirstSequence is the first_sequence of the first segment with a valid header.
	FirstSequence uint64
	// LastSequence is the maximum over segments of the last delivered sequence
	// (first_sequence-1 for a header-only segment); a regressing segment never
	// lowers it.
	LastSequence uint64
	// Clean is true iff there is no warning finding.
	Clean bool
}

func hasWarning(list []Finding) bool {
	for _, f := range list {
		if f.Severity == SeverityWarning {
			return true
		}
	}
	return false
}

type segScan struct {
	r         io.ReaderAt
	size      int64
	idx       uint32
	maxRec    int64
	maxResync int64
	hdr       wire.SegmentHeader
	last      uint64
	rep       SegmentReport
	visit     func(Scanned) error

	cBuf        []byte // read-through chunk cache used by the resync search
	cOff        int64
	spent       int64 // candidate payload bytes verified in the current damage region
	budgetBlown bool
}

func (s *segScan) add(code FindingCode, off, skipped int64, expected, got uint64, detail string) {
	s.rep.Findings = append(s.rep.Findings, Finding{
		Code: code, Severity: code.Severity(), Segment: s.idx, Offset: off, SkippedBytes: skipped,
		Expected: expected, Got: got, Detail: detail,
	})
}

func (s *segScan) readAt(off int64, n int) ([]byte, error) {
	if n <= frameOverhead+1 && off >= s.cOff && off+int64(n) <= s.cOff+int64(len(s.cBuf)) {
		return s.cBuf[off-s.cOff : off-s.cOff+int64(n)], nil // read-only view of the chunk cache
	}
	buf := make([]byte, n)
	got, err := s.r.ReadAt(buf, off)
	if got == n {
		return buf, nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return nil, err
}

// parseHeader returns the offset where records start. A non-empty detail
// means the header is damaged (the segment is then not scanned further); err
// is reserved for I/O errors.
func (s *segScan) parseHeader() (end int64, detail string, err error) {
	if s.size < headerFixed {
		return 0, "file shorter than the fixed header", nil
	}
	fixed, err := s.readAt(0, headerFixed)
	if err != nil {
		return 0, "", err
	}
	metaLen := int64(binary.LittleEndian.Uint32(fixed[12:]))
	switch {
	case !bytes.Equal(fixed[:8], segmentMagic[:]):
		return 0, "bad magic", nil
	case binary.LittleEndian.Uint16(fixed[8:]) != FormatVersion:
		return 0, "unsupported format_version", nil
	case binary.LittleEndian.Uint16(fixed[10:]) != 0:
		return 0, "unknown reserved flag bits", nil
	case metaLen < 1 || metaLen > MaxHeaderMeta:
		return 0, "meta_len out of range", nil
	case s.size < headerFixed+metaLen+4:
		return 0, "file shorter than the header", nil
	}
	rest, err := s.readAt(headerFixed, int(metaLen)+4)
	if err != nil {
		return 0, "", err
	}
	meta := rest[:metaLen]
	if checksum(fixed, meta) != binary.LittleEndian.Uint32(rest[metaLen:]) {
		return 0, "header checksum mismatch", nil
	}
	if err := s.hdr.Unmarshal(meta); err != nil {
		return 0, "header meta does not decode: " + err.Error(), nil
	}
	switch {
	case s.hdr.SchemaVersion < 1:
		return 0, "header schema_version < 1", nil
	case s.hdr.SegmentIndex != s.idx:
		return 0, fmt.Sprintf("header segment_index %d does not match file index %d", s.hdr.SegmentIndex, s.idx), nil
	case s.hdr.FirstSequence < 1:
		return 0, "header first_sequence < 1", nil
	}
	return headerFixed + metaLen + 4, "", nil
}

// frameAt validates the frame at off. code is empty for a valid frame; torn is
// set when the frame is merely incomplete (length beyond EOF).
func (s *segScan) frameAt(off int64, probe bool) (rec *wire.JournalRecord, n int64, code FindingCode, torn bool, err error) {
	if s.size-off < frameOverhead {
		return nil, 0, BadLength, true, nil
	}
	head, err := s.readAt(off, frameOverhead)
	if err != nil {
		return nil, 0, "", false, err
	}
	if !bytes.Equal(head[:4], frameSync[:]) {
		return nil, 0, BadSync, false, nil
	}
	length := int64(binary.LittleEndian.Uint32(head[4:]))
	switch {
	case length == 0 || length > s.maxRec:
		return nil, 0, BadLength, false, nil
	case off+frameOverhead+length > s.size:
		return nil, 0, BadLength, true, nil
	}
	if probe {
		// Resync candidates are filtered cheaply before the payload is read:
		// every valid record starts with the schema_version field (tag 0x08),
		// and the verified-bytes budget bounds the work per damage region.
		first, err := s.readAt(off+frameOverhead, 1)
		if err != nil {
			return nil, 0, "", false, err
		}
		if first[0] != schemaVersionTag {
			return nil, 0, DecodeError, false, nil
		}
		if s.spent+length > resyncBudgetFactor*s.maxResync {
			s.budgetBlown = true
			return nil, 0, DecodeError, false, nil
		}
		s.spent += length
	}
	payload, err := s.readAt(off+frameOverhead, int(length))
	if err != nil {
		return nil, 0, "", false, err
	}
	if checksum(head[4:8], payload) != binary.LittleEndian.Uint32(head[8:]) {
		return nil, 0, BadCRC, false, nil
	}
	rec = &wire.JournalRecord{}
	if rec.Unmarshal(payload) != nil || rec.Validate() != nil {
		return nil, 0, DecodeError, false, nil
	}
	return rec, frameOverhead + length, "", false, nil
}

// findSync returns the first offset in [from, to] where the frame sync marker
// starts, or -1.
func (s *segScan) findSync(from, to int64) (int64, error) {
	hi := min(to+int64(len(frameSync)), s.size)
	for from+int64(len(frameSync)) <= hi {
		if from < s.cOff || from+int64(len(frameSync)) > s.cOff+int64(len(s.cBuf)) {
			n := min(int64(scanChunk), hi-from)
			buf := make([]byte, n)
			if got, err := s.r.ReadAt(buf, from); int64(got) != n {
				if err == nil {
					err = io.ErrUnexpectedEOF
				}
				return -1, err
			}
			s.cBuf, s.cOff = buf, from
		}
		end := min(int64(len(s.cBuf)), hi-s.cOff)
		if i := bytes.Index(s.cBuf[from-s.cOff:end], frameSync[:]); i >= 0 {
			return from + int64(i), nil
		}
		from = s.cOff + end - int64(len(frameSync)) + 1
	}
	return -1, nil
}

// candidate reports whether a fully valid frame of this stream starts at off.
func (s *segScan) candidate(off int64) (bool, error) {
	rec, _, code, _, err := s.frameAt(off, true)
	if err != nil || code != "" {
		return false, err
	}
	ok := rec.NodeID == s.hdr.NodeID && rec.RuntimeID == s.hdr.RuntimeID && rec.StreamID == s.hdr.StreamID &&
		rec.StreamSequence >= s.last
	return ok, nil
}

// resync searches for the next valid frame after the damage at off. found is
// the frame offset (or -1); abandoned reports that MaxResyncBytes was
// exhausted before EOF.
func (s *segScan) resync(off int64) (found int64, abandoned bool, err error) {
	hi := min(off+s.maxResync, s.size-1)
	s.spent, s.budgetBlown = 0, false
	for q := off + 1; ; {
		p, err := s.findSync(q, hi)
		if err != nil || p < 0 {
			return -1, off+s.maxResync < s.size-1, err
		}
		ok, err := s.candidate(p)
		if s.budgetBlown {
			return -1, true, nil
		}
		if err != nil || ok {
			return p, false, err
		}
		q = p + 1
	}
}

func (s *segScan) deliver(rec *wire.JournalRecord, off int64) error {
	switch seq := rec.StreamSequence; {
	case seq == s.last+1:
		s.last = seq
	case seq > s.last+1:
		s.add(SeqGap, off, 0, s.last+1, seq, "")
		s.last = seq
	default:
		s.add(SeqRegression, off, 0, s.last+1, seq, "")
	}
	if rec.BodyType() == wire.RecordTypeUnspecified {
		s.add(UnknownRecordType, off, 0, 0, uint64(rec.Type), "")
	}
	s.rep.Records++
	s.rep.LastSequence = s.last
	return s.visit(Scanned{Record: rec, Segment: s.idx, Offset: off})
}

// boundary checks the first_sequence of the segment against the previous one.
func (s *segScan) boundary(prev uint64) {
	first := s.hdr.FirstSequence
	switch {
	case first == prev+1:
	case first <= prev:
		s.add(SegmentOrder, 0, 0, prev+1, first, "segment boundary")
	default:
		s.add(SeqGap, 0, 0, prev+1, first, "segment boundary")
	}
}

// ScanSegment scans one segment held in r (size bytes). It never mutates
// anything. Valid records after a damaged region are still delivered to visit
// (which may be nil). The error return is reserved for I/O errors on r and for
// errors returned by visit; data damage is reported as Findings.
func ScanSegment(r io.ReaderAt, size int64, segmentIndex uint32, opt ScanOptions, visit func(Scanned) error) (SegmentReport, error) {
	s := &segScan{r: r, size: size, idx: segmentIndex, maxRec: int64(opt.Limits.MaxRecordBytes), maxResync: opt.MaxResyncBytes, visit: visit}
	s.rep = SegmentReport{Index: segmentIndex, Size: size}
	if s.maxRec <= 0 {
		s.maxRec = DefaultMaxRecordBytes
	}
	if s.maxResync <= 0 {
		s.maxResync = defaultMaxResync
	}
	if s.visit == nil {
		s.visit = func(Scanned) error { return nil }
	}
	o, detail, err := s.parseHeader()
	if err != nil {
		return s.rep, err
	}
	if detail != "" {
		s.add(BadHeader, 0, size, 0, 0, detail)
		return s.rep, nil
	}
	s.rep.HeaderOK = true
	s.rep.FirstSequence = s.hdr.FirstSequence
	s.last = s.hdr.FirstSequence - 1
	if !opt.skipBoundary && (opt.PrevLastSequence > 0 || opt.hasPrev) {
		s.boundary(opt.PrevLastSequence)
	}
	for o < size {
		rec, n, code, torn, err := s.frameAt(o, false)
		if err != nil {
			return s.rep, err
		}
		if code == "" {
			if err := s.deliver(rec, o); err != nil {
				return s.rep, err
			}
			o += n
			continue
		}
		p, abandoned, err := s.resync(o)
		switch {
		case err != nil:
			return s.rep, err
		case p >= 0:
			s.add(code, o, p-o, 0, 0, "")
			o = p
		case abandoned:
			detail := ""
			if s.budgetBlown {
				detail = "candidate verification budget exhausted"
			}
			s.add(ResyncAbandoned, o, s.maxResync, 0, 0, detail)
			return s.rep, nil
		case torn:
			s.add(TornTail, o, size-o, 0, 0, "")
			return s.rep, nil
		default:
			s.add(code, o, size-o, 0, 0, "")
			return s.rep, nil
		}
	}
	s.rep.CleanEOF = !hasWarning(s.rep.Findings)
	return s.rep, nil
}

func scanFile(fsys FS, path string, idx uint32, opt ScanOptions, visit func(Scanned) error) (SegmentReport, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return SegmentReport{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return SegmentReport{}, err
	}
	return ScanSegment(f, fi.Size(), idx, opt, visit)
}

func segmentNames(fsys FS, dir string) ([]segEntry, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return listSegments(names), nil
}

// ScanStream scans every segment of a stream directory in index order, reading
// through fsys (FS.ReadDir/FS.Open). Segment-index holes yield SEGMENT_MISSING;
// a torn tail on the final segment is downgraded to LIVE_TAIL (info) iff
// opt.AssumeLiveTail and a writer currently holds the stream lock.
func ScanStream(fsys FS, dir string, opt ScanOptions, visit func(Scanned) error) (StreamReport, error) {
	segs, err := segmentNames(fsys, dir)
	if err != nil {
		return StreamReport{}, err
	}
	var rep StreamReport
	var stream []Finding
	expected := uint32(1)
	var prevEff uint64 // effective last sequence of the previous segment with a valid header
	havePrev := false
	for _, e := range segs {
		so := opt
		so.PrevLastSequence, so.hasPrev = prevEff, havePrev
		so.skipBoundary = false
		if e.index > expected {
			stream = append(stream, Finding{
				Code: SegmentMissing, Severity: SeverityWarning, Segment: expected,
				Expected: uint64(expected), Got: uint64(e.index),
				Detail: fmt.Sprintf("segment indices %d..%d missing", expected, e.index-1),
			})
			so.skipBoundary = true
		}
		sr, err := scanFile(fsys, filepath.Join(dir, e.name), e.index, so, visit)
		if err != nil {
			return rep, err
		}
		rep.Segments = append(rep.Segments, sr)
		rep.Records += sr.Records
		if sr.HeaderOK && rep.FirstSequence == 0 {
			rep.FirstSequence = sr.FirstSequence
		}
		if sr.HeaderOK {
			// A header-only segment has consumed sequences up to first_sequence-1.
			eff := sr.FirstSequence - 1
			if sr.Records > 0 {
				eff = sr.LastSequence
			}
			prevEff, havePrev = eff, true
			rep.LastSequence = max(rep.LastSequence, eff) // never lowered by a regressing segment
		}
		expected = e.index + 1
	}
	if opt.AssumeLiveTail && len(rep.Segments) > 0 {
		if err := downgradeLiveTail(dir, &rep.Segments[len(rep.Segments)-1]); err != nil {
			return rep, err
		}
	}
	rep.Findings = stream
	for _, sr := range rep.Segments {
		rep.Findings = append(rep.Findings, sr.Findings...)
	}
	sort.SliceStable(rep.Findings, func(i, j int) bool {
		a, b := rep.Findings[i], rep.Findings[j]
		return a.Segment < b.Segment || (a.Segment == b.Segment && a.Offset < b.Offset)
	})
	rep.Clean = !hasWarning(rep.Findings)
	return rep, nil
}

// downgradeLiveTail turns a TORN_TAIL finding of the final segment into the
// informational LIVE_TAIL when a writer is active.
func downgradeLiveTail(dir string, sr *SegmentReport) error {
	tail := -1
	for i, f := range sr.Findings {
		if f.Code == TornTail {
			tail = i
		}
	}
	if tail < 0 {
		return nil
	}
	active, err := WriterActive(dir)
	if err != nil || !active {
		return err
	}
	sr.Findings[tail].Code = LiveTail
	sr.Findings[tail].Severity = SeverityInfo
	return nil
}

// WriterActive reports whether a writer currently holds the stream lock. It
// tries a non-blocking flock on dir/LOCK and releases it again. A missing LOCK
// file means no writer.
func WriterActive(dir string) (bool, error) {
	f, err := os.OpenFile(filepath.Join(dir, "LOCK"), os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	return lockHeld(f)
}

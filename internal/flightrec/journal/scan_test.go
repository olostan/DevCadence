package journal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/olostan/DevCadence/internal/flightrec/wire"
)

func frameLen() int { return len(mkFrame(mkRecord(1))) }

func baseSeg() *segBuilder { return newSeg(1, 1).rec(1).rec(2).rec(3).rec(4) }

func wantCodes(t *testing.T, rep SegmentReport, want ...FindingCode) {
	t.Helper()
	got := codesOf(rep.Findings)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings %v, want %v (%+v)", got, want, rep.Findings)
	}
}

func TestScanClean(t *testing.T) {
	b := baseSeg()
	rep, seqs := scanBytes(t, b.bytes(), 1, ScanOptions{})
	wantCodes(t, rep)
	if !rep.HeaderOK || !rep.CleanEOF || rep.FirstSequence != 1 || rep.LastSequence != 4 || rep.Records != 4 || rep.Size != int64(len(b.buf)) {
		t.Fatalf("%+v", rep)
	}
	if !reflect.DeepEqual(seqs, []uint64{1, 2, 3, 4}) {
		t.Fatal(seqs)
	}
	// Header only is a clean, empty segment; a nil visitor is accepted.
	hdr := newSeg(1, 1).bytes()
	rep2, err := ScanSegment(bytes.NewReader(hdr), int64(len(hdr)), 1, ScanOptions{}, nil)
	if err != nil || !rep2.CleanEOF || rep2.Records != 0 {
		t.Fatalf("%+v %v", rep2, err)
	}
}

func TestScanHeaderDamage(t *testing.T) {
	good := newSeg(1, 1).rec(1).bytes()
	metaLen := int(binary.LittleEndian.Uint32(good[12:]))
	withMeta := func(meta []byte) []byte { return append(encodeHeader(meta), mkFrame(mkRecord(1))...) }
	hdrOf := func(h wire.SegmentHeader) []byte {
		meta, err := h.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		return withMeta(meta)
	}
	mut := func(f func(b []byte) []byte) []byte { return f(append([]byte(nil), good...)) }
	cases := map[string][]byte{
		"torn before fixed header": good[:10],
		"bad magic":                mut(func(b []byte) []byte { b[0] ^= 1; return b }),
		"unsupported version":      mut(func(b []byte) []byte { b[8] = 2; return b }),
		"reserved flags":           mut(func(b []byte) []byte { b[10] = 1; return b }),
		"meta_len zero":            mut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[12:], 0); return b }),
		"meta_len too large":       mut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[12:], 5000); return b }),
		"torn inside header":       good[:16+metaLen+3],
		"header crc":               mut(func(b []byte) []byte { b[16+metaLen] ^= 1; return b }),
		"meta undecodable":         withMeta([]byte{0x12, 0x01, 0xff}),
		"schema_version zero":      hdrOf(wire.SegmentHeader{SegmentIndex: 1, FirstSequence: 1}),
		"index mismatch":           hdrOf(wire.SegmentHeader{SchemaVersion: 1, SegmentIndex: 2, FirstSequence: 1}),
		"first_sequence zero":      hdrOf(wire.SegmentHeader{SchemaVersion: 1, SegmentIndex: 1}),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			rep, seqs := scanBytes(t, data, 1, ScanOptions{})
			wantCodes(t, rep, BadHeader)
			f := rep.Findings[0]
			if rep.HeaderOK || rep.CleanEOF || len(seqs) != 0 || f.Offset != 0 || f.SkippedBytes != int64(len(data)) || f.Detail == "" || f.Severity != SeverityWarning {
				t.Fatalf("%+v %+v", rep, f)
			}
		})
	}
}

func TestScanDamageMatrix(t *testing.T) {
	L := int64(frameLen())
	b := baseSeg()
	offs := b.offs
	data := b.bytes()
	patchLen := func(frame int, n uint32) []byte {
		d := append([]byte(nil), data...)
		binary.LittleEndian.PutUint32(d[offs[frame]+4:], n)
		return d
	}
	payloadLen := uint32(L - 12)

	type want struct {
		codes  []FindingCode
		seqs   []uint64
		offset int64
		skip   int64
	}
	cases := []struct {
		name string
		data []byte
		opt  ScanOptions
		want want
	}{
		{"torn payload tail", data[:len(data)-5], ScanOptions{}, want{[]FindingCode{TornTail}, []uint64{1, 2, 3}, int64(offs[3]), L - 5}},
		{"torn inside frame header", data[:offs[3]+7], ScanOptions{}, want{[]FindingCode{TornTail}, []uint64{1, 2, 3}, int64(offs[3]), 7}},
		{"length beyond EOF with a later valid frame", patchLen(1, payloadLen+4000), ScanOptions{},
			want{[]FindingCode{BadLength, SeqGap}, []uint64{1, 3, 4}, int64(offs[1]), L}},
		{"zero tail", append(append([]byte(nil), data...), make([]byte, 100)...), ScanOptions{},
			want{[]FindingCode{BadSync}, []uint64{1, 2, 3, 4}, int64(len(data)), 100}},
		{"short zero tail is torn", append(append([]byte(nil), data...), make([]byte, 5)...), ScanOptions{},
			want{[]FindingCode{TornTail}, []uint64{1, 2, 3, 4}, int64(len(data)), 5}},
		{"flipped payload byte", func() []byte { d := append([]byte(nil), data...); d[offs[1]+12+3] ^= 0xff; return d }(), ScanOptions{},
			want{[]FindingCode{BadCRC, SeqGap}, []uint64{1, 3, 4}, int64(offs[1]), L}},
		{"bad sync", func() []byte { d := append([]byte(nil), data...); d[offs[1]] = 'X'; return d }(), ScanOptions{},
			want{[]FindingCode{BadSync, SeqGap}, []uint64{1, 3, 4}, int64(offs[1]), L}},
		{"oversize length", patchLen(1, 5000), ScanOptions{Limits: Limits{MaxRecordBytes: 4096}},
			want{[]FindingCode{BadLength, SeqGap}, []uint64{1, 3, 4}, int64(offs[1]), L}},
		{"zero length", patchLen(1, 0), ScanOptions{},
			want{[]FindingCode{BadLength, SeqGap}, []uint64{1, 3, 4}, int64(offs[1]), L}},
		{"corrupt length is caught by the CRC", patchLen(1, payloadLen-3), ScanOptions{},
			want{[]FindingCode{BadCRC, SeqGap}, []uint64{1, 3, 4}, int64(offs[1]), L}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep, seqs := scanBytes(t, c.data, 1, c.opt)
			wantCodes(t, rep, c.want.codes...)
			f := rep.Findings[0]
			if f.Offset != c.want.offset || f.SkippedBytes != c.want.skip || f.Severity != SeverityWarning || f.Segment != 1 {
				t.Fatalf("%+v", f)
			}
			if !reflect.DeepEqual(seqs, c.want.seqs) || rep.CleanEOF {
				t.Fatalf("seqs %v cleanEOF=%v", seqs, rep.CleanEOF)
			}
		})
	}
}

func TestScanSequenceChecks(t *testing.T) {
	rep, seqs := scanBytes(t, newSeg(1, 1).rec(1).rec(3).bytes(), 1, ScanOptions{})
	wantCodes(t, rep, SeqGap)
	if f := rep.Findings[0]; f.Expected != 2 || f.Got != 3 || !reflect.DeepEqual(seqs, []uint64{1, 3}) {
		t.Fatalf("%+v %v", f, seqs)
	}
	rep, seqs = scanBytes(t, newSeg(1, 1).rec(1).rec(2).rec(2).rec(3).bytes(), 1, ScanOptions{})
	wantCodes(t, rep, SeqRegression)
	if f := rep.Findings[0]; f.Expected != 3 || f.Got != 2 || !reflect.DeepEqual(seqs, []uint64{1, 2, 2, 3}) || rep.LastSequence != 3 {
		t.Fatalf("%+v %v", f, seqs)
	}
	// A first record that disagrees with first_sequence is reported once.
	rep, _ = scanBytes(t, newSeg(1, 5).rec(7).bytes(), 1, ScanOptions{})
	wantCodes(t, rep, SeqGap)
}

func TestScanUnknownRecordTypeAndDecodeErrors(t *testing.T) {
	future := mkRecord(2)
	future.Observation = nil
	future.Type = 9
	future.Unknown = []byte{0x72, 0x01, 'x'} // field 14, length 1
	b := newSeg(1, 1).rec(1).frame(mkFrame(future)).rec(3)
	rep, seqs := scanBytes(t, b.bytes(), 1, ScanOptions{})
	wantCodes(t, rep, UnknownRecordType)
	if f := rep.Findings[0]; f.Severity != SeverityInfo || f.Got != 9 || !rep.CleanEOF || !reflect.DeepEqual(seqs, []uint64{1, 2, 3}) || hasWarning(rep.Findings) {
		t.Fatalf("%+v %v", f, seqs)
	}

	garbage := encodeFrame([]byte{0x0a, 0x05, 'a'}) // valid CRC, undecodable payload
	zeroSeq := mkRecord(0)
	invalid := mkFrame(zeroSeq) // decodes, fails Validate
	for name, frame := range map[string][]byte{"undecodable": garbage, "invalid": invalid} {
		t.Run(name, func(t *testing.T) {
			b := newSeg(1, 1).rec(1).frame(frame).rec(2)
			rep, seqs := scanBytes(t, b.bytes(), 1, ScanOptions{})
			wantCodes(t, rep, DecodeError)
			if f := rep.Findings[0]; f.SkippedBytes != int64(len(frame)) || !reflect.DeepEqual(seqs, []uint64{1, 2}) {
				t.Fatalf("%+v %v", f, seqs)
			}
		})
	}
}

func TestScanResyncBounds(t *testing.T) {
	garbage := bytes.Repeat([]byte{0xAB}, 100)
	b := newSeg(1, 1).rec(1)
	damageAt := int64(len(b.buf))
	b.raw(garbage).rec(2)
	data := b.bytes()

	rep, seqs := scanBytes(t, data, 1, ScanOptions{MaxResyncBytes: 8})
	wantCodes(t, rep, ResyncAbandoned)
	if f := rep.Findings[0]; f.Offset != damageAt || f.SkippedBytes != 8 || !reflect.DeepEqual(seqs, []uint64{1}) || rep.CleanEOF {
		t.Fatalf("%+v %v", f, seqs)
	}
	rep, seqs = scanBytes(t, data, 1, ScanOptions{MaxResyncBytes: 200})
	wantCodes(t, rep, BadSync)
	if f := rep.Findings[0]; f.SkippedBytes != 100 || !reflect.DeepEqual(seqs, []uint64{1, 2}) {
		t.Fatalf("%+v %v", f, seqs)
	}

	// False sync markers are skipped; a marker too close to EOF to be a frame is rejected.
	fake := append(append([]byte("FRM1"), bytes.Repeat([]byte{0x01}, 20)...), []byte("FRM1ab")...)
	b2 := newSeg(1, 1).rec(1).raw(fake).rec(2)
	rep, seqs = scanBytes(t, b2.bytes(), 1, ScanOptions{})
	wantCodes(t, rep, BadLength) // "FRM1" + 0x01010101 is an oversize length
	if !reflect.DeepEqual(seqs, []uint64{1, 2}) {
		t.Fatal(seqs)
	}
	b3 := newSeg(1, 1).rec(1).raw(bytes.Repeat([]byte{0xCD}, 20)).raw([]byte("FRM1abc"))
	rep, seqs = scanBytes(t, b3.bytes(), 1, ScanOptions{})
	wantCodes(t, rep, BadSync)
	if f := rep.Findings[0]; f.SkippedBytes != 27 || !reflect.DeepEqual(seqs, []uint64{1}) {
		t.Fatalf("%+v %v", f, seqs)
	}

	// Search across the internal read-chunk boundary, including a marker that straddles it.
	for _, gap := range []int{70000, scanChunk - 1, scanChunk, scanChunk + 1} {
		b := newSeg(1, 1).rec(1).raw(make([]byte, gap)).rec(2)
		rep, seqs := scanBytes(t, b.bytes(), 1, ScanOptions{})
		wantCodes(t, rep, BadSync)
		if f := rep.Findings[0]; f.SkippedBytes != int64(gap) || !reflect.DeepEqual(seqs, []uint64{1, 2}) {
			t.Fatalf("gap %d: %+v %v", gap, f, seqs)
		}
	}
}

// A-19: a valid frame embedded in a damaged record's payload.
func TestResyncCandidateChecks(t *testing.T) {
	embed := func(e *wire.JournalRecord) ([]byte, int) {
		inner := mkFrame(e)
		payload := append([]byte("junk-before-"), inner...)
		d := encodeFrame(payload)
		d[8] ^= 0xFF // damage the CRC: the whole record is damaged
		return d, 12 + len("junk-before-")
	}
	foreign := mkRecord(5)
	foreign.StreamID = "other"
	old := mkRecord(1) // sequence below the running last (2)
	fits := mkRecord(5)

	for name, c := range map[string]struct {
		e     *wire.JournalRecord
		codes []FindingCode
		seqs  []uint64
	}{
		"foreign stream id rejected":  {foreign, []FindingCode{BadCRC, SeqGap}, []uint64{1, 2, 6}},
		"older sequence rejected":     {old, []FindingCode{BadCRC, SeqGap}, []uint64{1, 2, 6}},
		"matching ids and seq accept": {fits, []FindingCode{BadCRC, SeqGap}, []uint64{1, 2, 5, 6}},
	} {
		t.Run(name, func(t *testing.T) {
			dmg, embedAt := embed(c.e)
			b := newSeg(1, 1).rec(1).rec(2)
			damageAt := int64(len(b.buf))
			b.frame(dmg).rec(6)
			rep, seqs := scanBytes(t, b.bytes(), 1, ScanOptions{})
			wantCodes(t, rep, c.codes...)
			skipped := int64(len(dmg))
			if name == "matching ids and seq accept" {
				skipped = int64(embedAt)
			}
			if f := rep.Findings[0]; f.Offset != damageAt || f.SkippedBytes != skipped || !reflect.DeepEqual(seqs, c.seqs) {
				t.Fatalf("%+v %v", f, seqs)
			}
		})
	}
}

func TestScanSegmentBoundary(t *testing.T) {
	cases := []struct {
		name  string
		first uint64
		prev  uint64
		want  []FindingCode
		exp   uint64
		got   uint64
	}{
		{"contiguous", 11, 10, nil, 0, 0},
		{"first segment unchecked", 11, 0, nil, 0, 0},
		{"gap", 15, 10, []FindingCode{SeqGap}, 11, 15},
		{"order", 8, 10, []FindingCode{SegmentOrder}, 11, 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := newSeg(2, c.first).rec(c.first).bytes()
			rep, _ := scanBytes(t, data, 2, ScanOptions{PrevLastSequence: c.prev})
			wantCodes(t, rep, c.want...)
			if len(c.want) > 0 {
				if f := rep.Findings[0]; f.Expected != c.exp || f.Got != c.got || f.Offset != 0 || f.Detail != "segment boundary" {
					t.Fatalf("%+v", f)
				}
			}
		})
	}
}

func TestFindingSeverityAndCleanTruthTable(t *testing.T) {
	info := map[FindingCode]bool{UnknownRecordType: true, LiveTail: true}
	all := []FindingCode{TornTail, BadHeader, BadSync, BadLength, BadCRC, DecodeError, SeqGap, SeqRegression,
		SegmentMissing, SegmentOrder, ResyncAbandoned, UnknownRecordType, LiveTail}
	for _, c := range all {
		want := SeverityWarning
		if info[c] {
			want = SeverityInfo
		}
		if c.Severity() != want {
			t.Fatalf("%s severity %d", c, c.Severity())
		}
		if got := hasWarning([]Finding{{Code: c, Severity: c.Severity()}}); got == info[c] {
			t.Fatalf("%s: hasWarning=%v", c, got)
		}
	}
	if hasWarning(nil) {
		t.Fatal("empty report has no warning")
	}
}

// ---- stream scans over real files ----

func TestScanStreamHolesAndOrdering(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s")
	// Segment 2 is missing; segment 3's first_sequence has a hole of its own, which must
	// not produce a second (boundary) finding. An unrelated file is ignored.
	writeSegFile(t, dir, 1, newSeg(1, 1).rec(1).rec(2).bytes())
	writeSegFile(t, dir, 3, newSeg(3, 9).rec(9).bytes())
	if err := os.WriteFile(filepath.Join(dir, "LOCK"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	rep, recs := scanDir(t, dir, ScanOptions{})
	if got := codesOf(rep.Findings); !reflect.DeepEqual(got, []FindingCode{SegmentMissing}) || rep.Clean {
		t.Fatalf("%v", rep.Findings)
	}
	if f := rep.Findings[0]; f.Segment != 2 || f.Expected != 2 || f.Got != 3 || f.Offset != 0 {
		t.Fatalf("%+v", f)
	}
	if rep.Records != 3 || rep.FirstSequence != 1 || rep.LastSequence != 9 || len(rep.Segments) != 2 || len(recs) != 3 {
		t.Fatalf("%+v", rep)
	}

	// Segment 1 missing at the start.
	dir2 := filepath.Join(t.TempDir(), "s")
	writeSegFile(t, dir2, 3, newSeg(3, 1).rec(1).bytes())
	rep, _ = scanDir(t, dir2, ScanOptions{})
	if f := rep.Findings[0]; f.Code != SegmentMissing || f.Segment != 1 || f.Got != 3 || len(rep.Findings) != 1 {
		t.Fatalf("%v", rep.Findings)
	}

	// A damaged header in the middle: the next segment's boundary check reports the gap.
	dir3 := filepath.Join(t.TempDir(), "s")
	writeSegFile(t, dir3, 1, newSeg(1, 1).rec(1).rec(2).bytes())
	writeSegFile(t, dir3, 2, []byte("garbage"))
	writeSegFile(t, dir3, 3, newSeg(3, 6).rec(6).bytes())
	rep, _ = scanDir(t, dir3, ScanOptions{})
	got := codesOf(rep.Findings)
	if !reflect.DeepEqual(got, []FindingCode{BadHeader, SeqGap}) || rep.Findings[1].Segment != 3 || rep.Findings[1].Expected != 3 || rep.Findings[1].Got != 6 {
		t.Fatalf("%v", rep.Findings)
	}

	// Segment order anomaly across a boundary, ordered by (Segment, Offset).
	dir4 := filepath.Join(t.TempDir(), "s")
	writeSegFile(t, dir4, 1, newSeg(1, 1).rec(1).rec(2).rec(3).bytes())
	writeSegFile(t, dir4, 2, newSeg(2, 2).rec(2).rec(3).bytes())
	rep, _ = scanDir(t, dir4, ScanOptions{})
	if got := codesOf(rep.Findings); !reflect.DeepEqual(got, []FindingCode{SegmentOrder}) {
		t.Fatalf("%v", rep.Findings)
	}
}

func TestScanStreamLiveTail(t *testing.T) {
	torn := func() []byte { d := newSeg(1, 1).rec(1).rec(2).bytes(); return d[:len(d)-4] }

	// No writer: the torn tail stays a warning even with AssumeLiveTail.
	dir := filepath.Join(t.TempDir(), "s")
	writeSegFile(t, dir, 1, torn())
	rep, _ := scanDir(t, dir, ScanOptions{AssumeLiveTail: true})
	if rep.Clean || rep.Findings[0].Code != TornTail {
		t.Fatalf("%v", rep.Findings)
	}
	// Without AssumeLiveTail the lock is not consulted at all.
	rep, _ = scanDir(t, dir, ScanOptions{})
	if rep.Clean || rep.Findings[0].Code != TornTail {
		t.Fatalf("%v", rep.Findings)
	}

	// A writer holds the lock: the final segment's torn tail is a live tail (info).
	lock, err := os.OpenFile(filepath.Join(dir, "LOCK"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		t.Fatal(err)
	}
	rep, _ = scanDir(t, dir, ScanOptions{AssumeLiveTail: true})
	if !rep.Clean || rep.Findings[0].Code != LiveTail || rep.Findings[0].Severity != SeverityInfo {
		t.Fatalf("%v", rep.Findings)
	}

	// A torn tail in a non-final segment stays a warning.
	writeSegFile(t, dir, 2, newSeg(2, 2).rec(2).bytes())
	rep, _ = scanDir(t, dir, ScanOptions{AssumeLiveTail: true})
	if rep.Clean || rep.Findings[0].Code != TornTail || rep.Findings[0].Segment != 1 {
		t.Fatalf("%v", rep.Findings)
	}

	// No torn tail: nothing to downgrade, the lock is not consulted.
	dir2 := filepath.Join(t.TempDir(), "s")
	writeSegFile(t, dir2, 1, newSeg(1, 1).rec(1).bytes())
	rep, _ = scanDir(t, dir2, ScanOptions{AssumeLiveTail: true})
	if !rep.Clean || len(rep.Findings) != 0 {
		t.Fatalf("%v", rep.Findings)
	}

	// An unusable LOCK is an error, not a finding.
	dir3 := filepath.Join(t.TempDir(), "s")
	writeSegFile(t, dir3, 1, torn())
	if err := os.Mkdir(filepath.Join(dir3, "LOCK"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanStream(OSFS(), dir3, ScanOptions{AssumeLiveTail: true}, nil); err == nil {
		t.Fatal("expected an error from WriterActive")
	}
	if active, err := WriterActive(dir3); err == nil || active {
		t.Fatalf("WriterActive on a directory LOCK: %v %v", active, err)
	}
	if active, err := WriterActive(filepath.Join(t.TempDir(), "none")); err != nil || active {
		t.Fatalf("WriterActive without LOCK: %v %v", active, err)
	}
}

// flaky is an io.ReaderAt failing (or truncating) the nth read.
type flaky struct {
	r     io.ReaderAt
	n     int
	calls int
	short bool
}

func (f *flaky) ReadAt(p []byte, off int64) (int, error) {
	f.calls++
	if f.calls == f.n {
		if f.short {
			return len(p) - 1, nil
		}
		return 0, errors.New("disk on fire")
	}
	return f.r.ReadAt(p, off)
}

func TestScanIOErrors(t *testing.T) {
	// A rich segment reaching every read site: header, frames, payloads, resync search
	// and candidate validation.
	b := newSeg(1, 1).rec(1).raw(bytes.Repeat([]byte{0xEE}, 40)).raw([]byte("FRM1")).raw(bytes.Repeat([]byte{0x01}, 30)).rec(2)
	data := b.bytes()
	for _, short := range []bool{false, true} {
		reads := 0
		for n := 1; ; n++ {
			f := &flaky{r: bytes.NewReader(data), n: n, short: short}
			_, err := ScanSegment(f, int64(len(data)), 1, ScanOptions{}, nil)
			if err == nil {
				reads = n - 1
				break
			}
			if short && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("short read %d: %v", n, err)
			}
		}
		if reads < 6 {
			t.Fatalf("only %d read sites exercised", reads)
		}
	}
	// A visitor error aborts the scan and is returned as is.
	boom := errors.New("visitor")
	_, err := ScanSegment(bytes.NewReader(data), int64(len(data)), 1, ScanOptions{}, func(Scanned) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("visitor error: %v", err)
	}
}

func TestScanStreamFSErrors(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s")
	writeSegFile(t, dir, 1, newSeg(1, 1).rec(1).bytes())
	for _, op := range []string{"readdir", "open", "stat"} {
		_, err := ScanStream(newFaultFS(failNth(op, 1, errBoom)), dir, ScanOptions{}, nil)
		if !errors.Is(err, errBoom) {
			t.Fatalf("%s: %v", op, err)
		}
	}
	// A visitor error from ScanStream.
	boom := errors.New("visitor")
	if _, err := ScanStream(OSFS(), dir, ScanOptions{}, func(Scanned) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("%v", err)
	}
}

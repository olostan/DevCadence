package journal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"regexp"
	"sort"
	"strconv"

	"github.com/olostan/DevCadence/internal/errs"
)

// Format constants (format_version 1). All integers are little-endian and the
// checksum is CRC32C (Castagnoli).
const (
	// FormatVersion is the segment framing version written by this package.
	FormatVersion = 1
	// MaxHealthBytes caps every marshalled JournalHealth record.
	MaxHealthBytes = 4096
	// DefaultMaxRecordBytes is the default Limits.MaxRecordBytes (1 MiB).
	DefaultMaxRecordBytes = 1 << 20
	// HardMaxRecordBytes is the largest permitted Limits.MaxRecordBytes (8 MiB).
	HardMaxRecordBytes = 8 << 20
	// DefaultMaxSegmentBytes is the default Limits.MaxSegmentBytes (64 MiB).
	DefaultMaxSegmentBytes = 64 << 20
	// MaxHeaderMeta is the largest permitted segment-header metadata block.
	MaxHeaderMeta = 4096

	frameOverhead  = 12
	headerFixed    = 16
	maxSegmentIdx  = 999999
	maxIDBytes     = 128
	maxVersionByte = 256
)

var (
	segmentMagic = [8]byte{0x89, 'D', 'C', 'J', 'R', 'N', 'L', '\n'}
	frameSync    = [4]byte{'F', 'R', 'M', '1'}
	crcTable     = crc32.MakeTable(crc32.Castagnoli)
)

// Errors returned by the journal. Compare with errors.Is.
var (
	// ErrClosed reports an append to a closed writer.
	ErrClosed = errors.New("journal: writer closed")
	// ErrRecordTooLarge reports a record whose marshalled size exceeds
	// Limits.MaxRecordBytes. It does not poison the writer.
	ErrRecordTooLarge = errors.New("journal: record exceeds MaxRecordBytes")
	// ErrSegmentLimit reports that the segment index space (999999) is exhausted.
	ErrSegmentLimit = errors.New("journal: segment index limit reached")
	// ErrLockUnsupported reports a platform without flock support.
	ErrLockUnsupported = errors.New("journal: single-writer lock unsupported on this platform")
)

// Limits bound record and segment sizes. Zero fields take their defaults.
type Limits struct {
	// MaxRecordBytes bounds one marshalled JournalRecord (default 1 MiB, hard cap 8 MiB).
	MaxRecordBytes int
	// MaxSegmentBytes bounds one segment file (default 64 MiB).
	MaxSegmentBytes int
}

// minSegmentBytes is the smallest legal MaxSegmentBytes for a MaxRecordBytes:
// a worst-case header (meta_len 4096), at most three health frames and one
// maximal record frame.
func minSegmentBytes(maxRecord int) int {
	return (headerFixed + MaxHeaderMeta + 4) + 3*(frameOverhead+MaxHealthBytes) + (frameOverhead + maxRecord)
}

// Normalize applies defaults and validates the limits, returning
// errs.CategoryInvalidArgument on violation.
func (l Limits) Normalize() (Limits, error) {
	if l.MaxRecordBytes == 0 {
		l.MaxRecordBytes = DefaultMaxRecordBytes
	}
	if l.MaxSegmentBytes == 0 {
		l.MaxSegmentBytes = DefaultMaxSegmentBytes
	}
	switch {
	case l.MaxRecordBytes < MaxHealthBytes || l.MaxRecordBytes > HardMaxRecordBytes:
		return l, errs.New(errs.CategoryInvalidArgument, "journal: MaxRecordBytes %d outside [%d, %d]", l.MaxRecordBytes, MaxHealthBytes, HardMaxRecordBytes)
	case l.MaxSegmentBytes < minSegmentBytes(l.MaxRecordBytes):
		return l, errs.New(errs.CategoryInvalidArgument, "journal: MaxSegmentBytes %d below the minimum %d for MaxRecordBytes %d", l.MaxSegmentBytes, minSegmentBytes(l.MaxRecordBytes), l.MaxRecordBytes)
	}
	return l, nil
}

func checksum(parts ...[]byte) uint32 {
	var sum uint32
	for _, p := range parts {
		sum = crc32.Update(sum, crcTable, p)
	}
	return sum
}

// encodeHeader frames the marshalled SegmentHeader meta block.
func encodeHeader(meta []byte) []byte {
	b := make([]byte, headerFixed+len(meta)+4)
	copy(b, segmentMagic[:])
	binary.LittleEndian.PutUint16(b[8:], FormatVersion)
	// flags (b[10:12]) stay zero.
	binary.LittleEndian.PutUint32(b[12:], uint32(len(meta)))
	copy(b[headerFixed:], meta)
	binary.LittleEndian.PutUint32(b[headerFixed+len(meta):], checksum(b[:headerFixed+len(meta)]))
	return b
}

// encodeFrame frames one marshalled record: sync, len, crc(len||payload), payload.
func encodeFrame(payload []byte) []byte {
	b := make([]byte, frameOverhead+len(payload))
	copy(b, frameSync[:])
	binary.LittleEndian.PutUint32(b[4:], uint32(len(payload)))
	copy(b[frameOverhead:], payload)
	binary.LittleEndian.PutUint32(b[8:], checksum(b[4:8], payload))
	return b
}

func segmentName(index uint32) string { return fmt.Sprintf("segment-%06d.pbj", index) }

var segmentNameRe = regexp.MustCompile(`^segment-([0-9]{6})\.pbj$`)

type segEntry struct {
	index uint32
	name  string
}

// listSegments returns the segment files among names, ordered by index.
func listSegments(names []string) []segEntry {
	var out []segEntry
	for _, n := range names {
		if m := segmentNameRe.FindStringSubmatch(n); m != nil {
			idx, _ := strconv.ParseUint(m[1], 10, 32)
			out = append(out, segEntry{index: uint32(idx), name: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].index < out[j].index })
	return out
}

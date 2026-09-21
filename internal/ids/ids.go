// Package ids generates the opaque stable identifiers used by durable
// DevCadience records.
//
// docs/PROTOCOLS.md §2 requires opaque stable string IDs (ULID/UUID shaped)
// with human-readable aliases layered on top. This package implements ULIDs
// because they sort lexicographically by creation time, which keeps listings
// of durable records stable without a secondary sort key, and it keeps the
// generator behind an interface so tests can inject deterministic IDs
// (ENGINEERING_STANDARDS.md §17).
package ids

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"
)

// crockford is the Crockford base32 alphabet used by ULID. It excludes I, L,
// O and U so that identifiers are unambiguous when read or transcribed.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Length is the encoded length of a ULID: 10 timestamp characters followed by
// 16 randomness characters.
const Length = 26

// Source produces identifiers. The prefix names the record kind (for example
// "evt" or "att") and is joined to the ULID with an underscore, matching the
// `evt_...`/`att_...` shapes used throughout docs/PROJECT_STATE.md.
type Source interface {
	New(prefix string) string
}

// ULIDSource generates real ULIDs from an injected clock reading and a
// cryptographically random suffix.
type ULIDSource struct {
	mu   sync.Mutex
	last [10]byte
	rand func([]byte) (int, error)
}

// NewULIDSource returns a Source backed by crypto/rand.
func NewULIDSource() *ULIDSource {
	return &ULIDSource{rand: rand.Read}
}

// New returns a prefixed ULID for the supplied creation instant.
//
// The caller passes the instant explicitly rather than letting this package
// read a clock, so that identifier time and record time cannot disagree.
func (s *ULIDSource) NewAt(prefix string, at time.Time) string {
	var buf [Length]byte
	encodeTime(buf[:10], uint64(at.UTC().UnixMilli()))

	entropy := make([]byte, 10)
	if _, err := s.rand(entropy); err != nil {
		// crypto/rand failure is not an ordinary runtime condition; a process
		// that cannot generate identifiers cannot produce durable records at
		// all, so failing loudly is preferable to emitting colliding IDs.
		panic(fmt.Sprintf("ids: entropy source failed: %v", err))
	}
	encodeBytes(buf[10:], entropy)
	return join(prefix, string(buf[:]))
}

// New returns a prefixed ULID stamped with the current wall-clock time.
//
// Prefer NewAt in control-plane code so that the identifier carries the same
// instant as the record it names.
func (s *ULIDSource) New(prefix string) string {
	return s.NewAt(prefix, time.Now())
}

// Sequential is a deterministic Source for tests. It emits
// `<prefix>_<pad>NNNNNNNNNN` style identifiers that remain 26 characters long
// so that they exercise the same column widths as real ULIDs.
//
// Sequential is safe for concurrent use, but concurrent callers must not
// depend on which counter value they receive.
type Sequential struct {
	mu       sync.Mutex
	counters map[string]uint64
}

// NewSequential returns a deterministic identifier source.
func NewSequential() *Sequential {
	return &Sequential{counters: make(map[string]uint64)}
}

// New returns the next deterministic identifier for the prefix. Counters are
// kept per prefix so that a test reading `tsk_...0000000001` knows it is the
// first task regardless of how many events were created alongside it.
func (s *Sequential) New(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[prefix]++
	n := s.counters[prefix]
	body := fmt.Sprintf("%026d", n)
	return join(prefix, body)
}

// NewAt ignores the instant; deterministic identifiers must not depend on time.
func (s *Sequential) NewAt(prefix string, _ time.Time) string { return s.New(prefix) }

// Reset returns the source to its initial state.
func (s *Sequential) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters = make(map[string]uint64)
}

func join(prefix, body string) string {
	if prefix == "" {
		return body
	}
	return prefix + "_" + body
}

// encodeTime writes a 48-bit millisecond timestamp as 10 Crockford base32
// characters, most significant character first.
func encodeTime(dst []byte, ms uint64) {
	for i := 9; i >= 0; i-- {
		dst[i] = crockford[ms&0x1f]
		ms >>= 5
	}
}

// encodeBytes writes 10 bytes of entropy as 16 Crockford base32 characters.
func encodeBytes(dst []byte, src []byte) {
	var v uint64
	var bits uint
	di := len(dst)
	for i := len(src) - 1; i >= 0; i-- {
		v |= uint64(src[i]) << bits
		bits += 8
		for bits >= 5 {
			di--
			dst[di] = crockford[v&0x1f]
			v >>= 5
			bits -= 5
		}
	}
	for di > 0 {
		di--
		dst[di] = crockford[v&0x1f]
		v >>= 5
	}
}

// Valid reports whether id looks like an identifier produced by this package:
// an optional lowercase prefix, an underscore, and a 26-character body drawn
// from the Crockford alphabet.
//
// It is a shape check for stored data, not an authenticity check.
func Valid(id string) bool {
	body := id
	if i := strings.LastIndex(id, "_"); i >= 0 {
		if i == 0 || i == len(id)-1 {
			return false
		}
		body = id[i+1:]
	}
	if len(body) != Length {
		return false
	}
	for i := 0; i < len(body); i++ {
		if !strings.ContainsRune(crockford, rune(body[i])) {
			return false
		}
	}
	return true
}

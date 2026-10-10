package flightrec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
	"github.com/olostan/DevCadence/internal/protocol"
)

// Sanitizer defaults and fixed limits (WP-TRACE-1 §8.4).
const (
	// DefaultMaxStringBytes is the default cap on one JSON string value.
	DefaultMaxStringBytes = 256
	// DefaultMaxDepth is the default maximum JSON nesting depth.
	DefaultMaxDepth = 6
	// DefaultMaxEntries is the default cap on entries per JSON object or array.
	DefaultMaxEntries = 64
	// DefaultMaxJSONBytes is the default cap on a sanitized JSON document.
	DefaultMaxJSONBytes = 16384
	// MaxMarshalBytes caps the marshalled input before the walk.
	MaxMarshalBytes = 1 << 20

	maxScalarBytes  = 128
	maxLocatorBytes = 256
	maxKeyBytes     = 64

	redacted     = "[REDACTED]"
	invalidValue = "[INVALID]"
)

const unserializableDoc = `{"_unserializable":true}`

var (
	jwtRE      = regexp.MustCompile(`eyJ[\w-]{10,}\.[\w-]{10,}\.[\w-]{5,}`)
	pemRE      = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	userinfoRE = regexp.MustCompile(`://[^/\s:@]+:[^/\s@]+@`)
	authRE     = regexp.MustCompile(`(?i)\bauthorization:[^\n]*`)
	envRE      = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{2,}=\S+`)
	digestRE   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	tokenRE    = regexp.MustCompile(`\S+`)
)

var denyKeys = []string{
	"password", "passwd", "secret", "token", "apikey", "authorization",
	"credential", "privatekey", "cookie", "bearer", "sessionid",
}

// rawKey reports whether a normalized key names raw content that must travel
// only as an ArtifactRef.
func rawKey(n string) bool {
	switch n {
	case "prompt", "completion", "source", "contents", "content", "body", "diff",
		"stdout", "stderr", "env", "environ", "environment", "configcontents", "rawconfig":
		return true
	}
	return false
}

// ScalarKind selects the validator applied by Sanitizer.Scalar.
type ScalarKind string

// Scalar kinds.
const (
	// ScalarID is an identifier-like field (128 bytes).
	ScalarID ScalarKind = "id"
	// ScalarLocator is a locator or path (256 bytes, value patterns apply).
	ScalarLocator ScalarKind = "locator"
	// ScalarMediaType is a media type (128 bytes, '+' allowed).
	ScalarMediaType ScalarKind = "media_type"
)

// SanitizerConfig may only ADD denylist entries and patterns or TIGHTEN the
// bounds (zero keeps the default; larger than the default is rejected).
type SanitizerConfig struct {
	ExtraDenyKeys  []string
	ExtraPatterns  []*regexp.Regexp
	MaxStringBytes int
	MaxDepth       int
	MaxEntries     int
	MaxJSONBytes   int
}

type rule struct {
	re   *regexp.Regexp
	repl string
}

// Sanitizer redacts and bounds everything the recorder persists. It is
// immutable and safe for concurrent use.
type Sanitizer struct {
	maxString, maxDepth, maxEntries, maxJSON int
	deny                                     []string
	rules                                    []rule
}

// tally accumulates the Sanitization counters of one record.
type tally struct {
	red, trunc uint32
	replaced   bool
}

func (t tally) wire() wire.Sanitization {
	return wire.Sanitization{Redactions: t.red, Truncations: t.trunc, PayloadReplaced: t.replaced}
}

func (t tally) ptr() *wire.Sanitization {
	if t == (tally{}) {
		return nil
	}
	w := t.wire()
	return &w
}

// NewSanitizer validates cfg and returns a Sanitizer.
func NewSanitizer(cfg SanitizerConfig) (*Sanitizer, error) {
	for _, b := range []struct {
		name string
		v    *int
		def  int
	}{
		{"MaxStringBytes", &cfg.MaxStringBytes, DefaultMaxStringBytes},
		{"MaxDepth", &cfg.MaxDepth, DefaultMaxDepth},
		{"MaxEntries", &cfg.MaxEntries, DefaultMaxEntries},
		{"MaxJSONBytes", &cfg.MaxJSONBytes, DefaultMaxJSONBytes},
	} {
		if *b.v == 0 {
			*b.v = b.def
		} else if *b.v < 0 || *b.v > b.def {
			return nil, errs.New(errs.CategoryInvalidArgument, "flightrec: sanitizer %s must be in [1, %d]", b.name, b.def)
		}
	}
	s := &Sanitizer{
		maxString: cfg.MaxStringBytes, maxDepth: cfg.MaxDepth, maxEntries: cfg.MaxEntries, maxJSON: cfg.MaxJSONBytes,
		deny: append([]string(nil), denyKeys...),
		rules: []rule{
			{jwtRE, redacted}, {pemRE, redacted}, {userinfoRE, "://" + redacted + "@"},
			{authRE, redacted}, {envRE, redacted},
		},
	}
	for _, k := range cfg.ExtraDenyKeys {
		n := normalizeKey(k)
		if n == "" {
			return nil, errs.New(errs.CategoryInvalidArgument, "flightrec: sanitizer deny key must not be empty")
		}
		s.deny = append(s.deny, n)
	}
	for _, re := range cfg.ExtraPatterns {
		if re == nil {
			return nil, errs.New(errs.CategoryInvalidArgument, "flightrec: sanitizer pattern must not be nil")
		}
		s.rules = append(s.rules, rule{re, redacted})
	}
	return s, nil
}

// DefaultSanitizer returns a Sanitizer with the normative defaults.
func DefaultSanitizer() *Sanitizer {
	s, _ := NewSanitizer(SanitizerConfig{}) // the zero config is always valid
	return s
}

func normalizeKey(k string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', '.', ' ':
			return -1
		}
		return r
	}, strings.ToLower(k))
}

func tokenAllowed(n string) bool {
	return strings.HasSuffix(n, "tokens") || n == "tokencount" || n == "tokenlimit" || n == "tokenusage"
}

// keyRule reports the replacement value a key forces, if any.
func (s *Sanitizer) keyRule(k string) (string, bool) {
	n := normalizeKey(k)
	if rawKey(n) {
		return "[OMITTED:raw]", true
	}
	for _, d := range s.deny {
		if strings.Contains(n, d) && !(d == "token" && tokenAllowed(n)) {
			return redacted, true
		}
	}
	return "", false
}

// redact replaces secret-looking content in str (token-level where possible).
func (s *Sanitizer) redact(t *tally, str string) string {
	if protocol.LooksLikeSecret(str) {
		t.red++
		return redacted
	}
	for _, r := range s.rules {
		if m := r.re.FindAllStringIndex(str, -1); len(m) > 0 {
			t.red += uint32(len(m))
			str = r.re.ReplaceAllString(str, r.repl)
		}
	}
	return tokenRE.ReplaceAllStringFunc(str, func(tok string) string {
		if digestRE.MatchString(tok) {
			return tok // content digests are exempt
		}
		if protocol.LooksLikeSecret(tok) {
			t.red++
			return redacted
		}
		return tok
	})
}

func truncUTF8(str string, n int) string {
	for n > 0 && !utf8.RuneStart(str[n]) {
		n--
	}
	return str[:n]
}

func lengthBucket(n int) string {
	switch {
	case n <= 256:
		return "256b"
	case n <= 1024:
		return "1k"
	case n <= 4096:
		return "4k"
	case n <= 16384:
		return "16k"
	}
	return "over16k"
}

// text sanitizes free text: multiline omission, secret redaction, truncation
// at a UTF-8 boundary. The truncation suffix is appended after the cap.
func (s *Sanitizer) text(t *tally, str string) string {
	if strings.Contains(strings.TrimSuffix(str, "\n"), "\n") {
		t.trunc++
		return "[OMITTED:multiline " + lengthBucket(len(str)) + "]"
	}
	str = s.redact(t, str)
	if len(str) > s.maxString {
		kept := truncUTF8(str, s.maxString)
		t.trunc++
		return fmt.Sprintf("%s...[truncated %d bytes]", kept, len(str)-len(kept))
	}
	return str
}

func scalarLimits(k ScalarKind) (int, string) {
	switch k {
	case ScalarLocator:
		return maxLocatorBytes, ""
	case ScalarMediaType:
		return maxScalarBytes, "+"
	}
	return maxScalarBytes, ""
}

// validScalar implements ^[A-Za-z0-9._:/@=-]{0,limit}$ plus extra characters.
func validScalar(v string, limit int, extra string) bool {
	if len(v) > limit {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			strings.IndexByte("._:/@=-", c) >= 0 || strings.IndexByte(extra, c) >= 0
		if !ok {
			return false
		}
	}
	return true
}

func (s *Sanitizer) scalar(t *tally, kind ScalarKind, v string) string {
	limit, extra := scalarLimits(kind)
	if !validScalar(v, limit, extra) {
		t.red++
		return invalidValue
	}
	var scratch tally
	if s.redact(&scratch, v) != v {
		t.red++
		return redacted
	}
	return v
}

// Scalar sanitizes one scalar string field: values failing the validator
// become "[INVALID]", values matching a secret pattern "[REDACTED]".
func (s *Sanitizer) Scalar(kind ScalarKind, v string) string {
	var t tally
	return s.scalar(&t, kind, v)
}

func (s *Sanitizer) key(t *tally, k string, ordinal int) string {
	var scratch tally
	if !validScalar(k, maxKeyBytes, "") || s.redact(&scratch, k) != k {
		t.red++
		return fmt.Sprintf("[INVALID_KEY_%d]", ordinal)
	}
	return k
}

func (s *Sanitizer) walk(t *tally, v any, depth int) any {
	if depth > s.maxDepth {
		t.trunc++
		return "[DEPTH_LIMIT]"
	}
	switch x := v.(type) {
	case string:
		return s.text(t, x)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any)
		for i, k := range keys {
			if i >= s.maxEntries {
				out["_truncated"] = json.Number(strconv.Itoa(len(keys) - i))
				t.trunc++
				break
			}
			ek := s.key(t, k, i)
			if repl, ok := s.keyRule(k); ok {
				t.red++
				out[ek] = repl
				continue
			}
			out[ek] = s.walk(t, x[k], depth+1)
		}
		return out
	case []any:
		n := min(len(x), s.maxEntries)
		out := make([]any, 0, n+1)
		for _, e := range x[:n] {
			out = append(out, s.walk(t, e, depth+1))
		}
		if len(x) > n {
			t.trunc++
			out = append(out, map[string]any{"_truncated": json.Number(strconv.Itoa(len(x) - n))})
		}
		return out
	}
	return v // nil, bool, json.Number
}

func truncatedDoc(n int) []byte {
	return fmt.Appendf(nil, `{"_truncated":true,"_original_bytes":%d}`, n)
}

func rawLen(v any) int {
	switch x := v.(type) {
	case string:
		return len(x)
	case []byte:
		return len(x)
	}
	return 0
}

// json is the never-failing pipeline behind JSON.
func (s *Sanitizer) json(v any) (out []byte, t tally) {
	if v == nil {
		return nil, tally{}
	}
	defer func() {
		if recover() != nil {
			out, t = []byte(unserializableDoc), tally{replaced: true}
		}
	}()
	if n := rawLen(v); n > MaxMarshalBytes {
		return truncatedDoc(n), tally{replaced: true}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(unserializableDoc), tally{replaced: true}
	}
	if len(raw) > MaxMarshalBytes {
		return truncatedDoc(len(raw)), tally{replaced: true}
	}
	var tree any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&tree) // json.Marshal output always decodes
	out, _ = json.Marshal(s.walk(&t, tree, 0))
	if len(out) > s.maxJSON {
		return truncatedDoc(len(out)), tally{replaced: true}
	}
	return out, t
}

func isProtoAny(v any) bool {
	if v == nil {
		return false
	}
	switch v.(type) {
	case wire.Any, *wire.Any:
		return true
	}
	_, ok := reflect.TypeOf(v).MethodByName("ProtoReflect")
	return ok
}

// checkedJSON is JSON with the tally kept for composition into a record.
func (s *Sanitizer) checkedJSON(v any) ([]byte, tally, error) {
	if isProtoAny(v) {
		return nil, tally{}, errs.New(errs.CategoryInvalidArgument, "flightrec: Any/protobuf payloads are not accepted in Phase 1")
	}
	out, t := s.json(v)
	return out, t, nil
}

// JSON sanitizes v into deterministic JSON bytes (sorted keys) and reports the
// counters. A nil v yields empty bytes. Unserializable or oversized payloads
// are replaced, never an error. The only error is a protobuf message / Any
// payload, which Phase 1 does not accept (CategoryInvalidArgument).
func (s *Sanitizer) JSON(v any) ([]byte, wire.Sanitization, error) {
	out, t, err := s.checkedJSON(v)
	return out, t.wire(), err
}

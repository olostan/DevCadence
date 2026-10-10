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
	"unicode"
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

// credWords are the credential-bearing name fragments of the free-text
// assignment rules. They are matched anywhere inside a name (prefix and suffix
// tolerant, so db_password, GITHUB_TOKEN and aws_secret_access_key all match;
// \b cannot be used because '_' is a word character).
const credWords = `password|passwd|pwd|passphrase|secret|token|api[_-]?key|private[_-]?key|access[_-]?key|secret[_-]?key|` +
	`authorization|cookie|credential|bearer|jwt|session[_-]?id|ssh[_-]?key|connection[_-]?string|encryption[_-]?key|` +
	`signing[_-]?key|hmac|mnemonic|signature|userpass|userpwd|dbpass|dbpw|account[_-]?key|license[_-]?key|key[_-]?data|client[_-]?key`

// assignValue is the value of an assignment: a quoted string (which may hold
// spaces) or the next whitespace-delimited word.
const assignValue = `(?:"[^"]*"|'[^']*'|\S+)`

// quoteSlot is the optional quote between a name and its operator. The text is
// unescaped first (see foldText) so the backslash tolerance is defense in depth.
const quoteSlot = `(?:\\*["'])?\]?`

// assignOp is an assignment operator: ":", "=", "=>" (PHP, Ruby, Lua), ":="
// and "?=" (Make, Go).
const assignOp = `(?:=>|::=|:=|\?=|[:=])`

var (
	jwtRE    = regexp.MustCompile(`eyJ[\w-]{10,}\.[\w-]{10,}\.[\w-]{5,}`)
	pemRE    = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY[A-Z ]*-----[^\n]*`)
	sshKeyRE = regexp.MustCompile(`\bssh-(?:rsa|dss|ed25519)\s+AAAA\S*`)
	// userinfoRE: the userinfo ends at the LAST '@' before the first '/' (a '@'
	// inside the password must not leak its tail). A '/' inside an unescaped
	// password ends the match early (documented limit).
	userinfoRE = regexp.MustCompile(`//[^/\s]*@`)
	// dsnRE covers scheme-less "user:pass@host" (a host with a dot, or MySQL's
	// "@tcp(" form); "image:1.2@sha256:..." digests do not match.
	dsnRE = regexp.MustCompile(`[\w.%+-]+:[^\s@/:'"]+@(?:[\w-]+\.[\w.-]+|\w+\()`)
	// webhookRE: Slack and Discord webhook URLs carry the secret in the path.
	webhookRE = regexp.MustCompile(`(?i)hooks\.slack\.com/(?:services|workflows|triggers)/[\w/-]+|discord(?:app)?\.com/api/webhooks/\d+/[\w-]+`)
	// dockerLoginRE: "docker login ... -p SECRET" (a bare -p is a port flag in
	// other commands, so only the login form is a credential).
	dockerLoginRE = regexp.MustCompile(`((?:docker|podman|buildah)\s+login\b[^\n]*?\s-p\s*)\S+`)
	// queryKeyRE: "key" is a credential only in query position (?key=, &key=).
	queryKeyRE = regexp.MustCompile(`(?i)([?&;#])key=\S+`)
	// authRE and cookieRE redact to the end of the line: the value may hold a
	// scheme and several words or ';'-separated pairs.
	authRE   = regexp.MustCompile(`(?i)authorization` + quoteSlot + `\s*` + assignOp + `[^\n]*`)
	cookieRE = regexp.MustCompile(`(?i)cookie` + quoteSlot + `\s*` + assignOp + `[^\n]*`)
	schemeRE = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+\S{6,}`)
	// assignRE: <name containing a credential word> [quote] <operator> <value>.
	assignRE = regexp.MustCompile(`(?i)(?:` + credWords + `)[\w.-]*` + quoteSlot + `\s*` + assignOp + `\s*` + assignValue)
	// shortAssignRE covers short names that are credentials only as a whole
	// name or a name suffix (pass, pw, otp, seed, auth, sig).
	shortAssignRE = regexp.MustCompile(`(?i)\b(?:[\w.-]*[_.-])?(?:pass|pw|[th]?otp|seed|auth|sig)\b` + quoteSlot + `\s*` + assignOp + `\s*` + assignValue)
	// camelAssignRE covers camelCase suffixes (dbPass=, adminPw:), which carry no
	// separator for shortAssignRE; it is case-sensitive so "bypass" and
	// "compass" do not match.
	camelAssignRE = regexp.MustCompile(`[\w.-]*[A-Za-z0-9](?:Pass|Pw|Otp)\b` + quoteSlot + `\s*` + assignOp + `\s*` + assignValue)
	// keywordRE is the whitespace-separated form ("password hunter2",
	// "password is hunter2", "_authToken hunter2").
	keywordRE = regexp.MustCompile(`(?i)(?:password|passwd|pwd|passphrase|secret|token|api[_-]?key|authorization|x-api-key|secret[_-]?key|private[_-]?key|access[_-]?key|secret[_-]?access[_-]?key)\b` + quoteSlot + `\s+(?:(?:is|was|are|were|=>|::=|:=|\?=|[:=])\s+)?\S+`)
	prefixRE  = regexp.MustCompile(`\b(?:(?i:gh[pousr])_[A-Za-z0-9]{6,}|(?i:github_pat_)\w{6,}|(?i:glpat-)[\w-]{6,}|(?i:xox[a-z])-[\w-]{6,}|(?i:xapp-)[\w-]{6,}|(?i:sk[-_])[\w-]{6,}|(?i:rk_(?:live|test)_)\w{6,}|(?i:whsec_)\w{6,}|(?i:shp(?:at|ca|pa)_)\w{6,}|(?i:dop_v1_)\w{6,}|(?i:npm_)\w{6,}|(?i:hf_)[A-Za-z0-9]{8,}|(?i:pypi-)[\w-]{6,}|(?i:aiza)[\w-]{8,}|ya29\.[\w-]{6,}|SG\.[\w-]{6,}|SK[0-9a-f]{32}|A[KS]IA[0-9A-Z]{8,})`)
	envRE     = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{2,}=` + assignValue)
	digestRE  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	tokenRE   = regexp.MustCompile(`\S+`)
	// escapedRE finds a run of backslashes before a quote or slash (JSON string
	// escapes of any nesting depth).
	escapedRE = regexp.MustCompile(`\\+(["'/])`)
)

// denyKeys are the substrings (of a normalized key or flag name) that make a
// name a credential. The structured-key rule and the CLI-flag rule both use
// this one list (via denyHit) so the two cannot diverge.
//
// Additions beyond the original list, with the reason for each: passphrase,
// secretkey, clientsecret, privatekey, accesskey (explicit forms of common
// credential names; the first three are also covered by shorter substrings and
// are listed so the intent survives a future narrowing), dsn and
// connectionstring (embed passwords), encryptionkey and signingkey (key
// material), hmac (shared secrets), mnemonic (wallet seed phrases).
var denyKeys = []string{
	"password", "passwd", "secret", "token", "apikey", "authorization",
	"credential", "privatekey", "cookie", "bearer", "sessionid",
	"pwd", "auth", "accesskey", "sshkey", "signature", "session", "jwt",
	"passphrase", "secretkey", "clientsecret", "dsn", "connectionstring",
	"encryptionkey", "signingkey", "hmac", "mnemonic", "userpass", "userpwd", "dbpass",
	"accountkey", "licensekey", "oauth",
}

// denyExact are normalized names that are credentials only as a whole name
// (substring matching would over-redact ordinary words): pass (as in --pass),
// pw, otp (one-time passwords) and seed (RNG or wallet seeds; "seeded" or
// "seedling" are not).
//
// "key", "sig", "salt" and "license" are whole-name credentials for the same
// reason ("api_key" is caught by substring; "cache_key" or "sort_key" are not
// credentials).
var denyExact = map[string]bool{
	"pass": true, "pw": true, "otp": true, "totp": true, "hotp": true, "seed": true, "key": true, "sig": true, "salt": true, "license": true,
}

// denySegment are the name segments (split on "-_. " and camelCase boundaries)
// that make a name a credential wherever they appear: db_pass, DB_PASS,
// mysql-pw, adminPass. "seed" stays whole-name only.
var denySegment = map[string]bool{"pass": true, "pw": true, "otp": true, "totp": true, "hotp": true}

// counterKeys are the only token-named keys whose value may be kept, and only
// when the value is a JSON number.
var counterKeys = map[string]bool{
	"inputtokens": true, "outputtokens": true, "totaltokens": true, "tokencount": true,
	"tokenlimit": true, "tokenusage": true, "maxtokens": true, "cachedtokens": true, "reasoningtokens": true,
	"prompttokens": true, "completiontokens": true, "cachereadinputtokens": true, "cachecreationinputtokens": true,
	"maxoutputtokens": true,
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
	window, budget                           int // redaction window and per-record budget (see redactWindow)
	deny                                     []string
	rules                                    []rule
}

// tally accumulates the Sanitization counters of one record.
type tally struct {
	red, trunc uint32
	replaced   bool
	// scan is the number of free-text bytes run through the rules so far (the
	// work budget, see redactBudget); it is not a reported counter.
	scan int
}

func (t tally) wire() wire.Sanitization {
	return wire.Sanitization{Redactions: t.red, Truncations: t.trunc, PayloadReplaced: t.replaced}
}

func (t tally) ptr() *wire.Sanitization {
	if t.red == 0 && t.trunc == 0 && !t.replaced {
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
		window: redactWindow, budget: redactBudget,
		deny: append([]string(nil), denyKeys...),
		rules: []rule{
			{jwtRE, redacted}, {pemRE, redacted}, {userinfoRE, "//" + redacted + "@"}, {dsnRE, redacted}, {webhookRE, redacted},
			{sshKeyRE, redacted}, {authRE, redacted}, {cookieRE, redacted}, {schemeRE, redacted}, {assignRE, redacted},
			{shortAssignRE, redacted}, {camelAssignRE, redacted}, {queryKeyRE, "${1}" + redacted}, {dockerLoginRE, "${1}" + redacted}, {keywordRE, redacted},
			{prefixRE, redacted}, {envRE, redacted},
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

// normalizeKey lowercases k and drops separators and invisible format (Cf)
// runes. Non-ASCII keys are additionally invalid map keys (see key), so their
// values are redacted regardless of what they normalize to.
func normalizeKey(k string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', '.', ' ':
			return -1
		}
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, strings.ToLower(k))
}

// tokenAllowed reports whether the key is an exact token counter and the value
// is numeric; every other token-named key is a credential.
func tokenAllowed(n string, v any) bool {
	_, num := v.(json.Number)
	return num && counterKeys[n]
}

// keyRule reports the replacement value a key forces, if any.
func (s *Sanitizer) keyRule(k string, v any) (string, bool) {
	n := normalizeKey(k)
	if rawKey(n) {
		return "[OMITTED:raw]", true
	}
	if s.denyHit(k, tokenAllowed(n, v)) {
		return redacted, true
	}
	return "", false
}

// authorRepl removes "author" (author, authority, authoritative) before the
// "auth" substring test; "authorization" is a deny substring of its own.
var authorRepl = strings.NewReplacer("author", "")

// nameSegments splits a raw key or flag name on "-_. " and camelCase boundaries
// into lowercase segments (invisible Cf runes are dropped first).
func nameSegments(raw string) []string {
	var segs []string
	var cur []rune
	var prev rune // last appended rune in its original case
	flush := func() {
		if len(cur) > 0 {
			segs = append(segs, string(cur))
			cur = cur[:0]
		}
	}
	rs := []rune(raw)
	for i, r := range rs {
		switch {
		case unicode.Is(unicode.Cf, r):
		case r == '-' || r == '_' || r == '.' || r == ' ':
			flush()
		default:
			if unicode.IsUpper(r) && len(cur) > 0 {
				nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
					flush()
				}
			}
			cur = append(cur, unicode.ToLower(r))
			prev = r
		}
	}
	flush()
	return segs
}

// denyHit reports whether the raw name is a credential name. It is the single
// decision shared by structured keys and CLI flags. counterOK exempts the
// "token" substring (numeric token counters).
func (s *Sanitizer) denyHit(raw string, counterOK bool) bool {
	n := normalizeKey(raw)
	if denyExact[n] {
		return true
	}
	for _, seg := range nameSegments(raw) {
		if denySegment[seg] {
			return true
		}
	}
	for _, d := range s.deny {
		hay := n
		if d == "auth" {
			hay = authorRepl.Replace(n)
		}
		if strings.Contains(hay, d) && !(counterOK && d == "token") {
			return true
		}
	}
	return false
}

// secretFlagName reports whether a command-line argument without its value
// ("--client_secret", "-token") names a credential flag: the leading dashes are
// stripped and the rest takes the structured-key decision.
func (s *Sanitizer) secretFlagName(arg string) bool {
	if !strings.HasPrefix(arg, "-") {
		return false
	}
	return s.denyHit(strings.TrimLeft(arg, "-"), false)
}

// userFlagName reports flags whose value is "user:password" (curl, kubectl).
// Only the separate and "=" forms are handled; "-uuser:pass" and "mysql -pSECRET"
// are documented limits.
func userFlagName(name string) bool {
	return name == "-u" || name == "--user" || name == "--proxy-user"
}

// redactFlags masks the value of credential flags in free text: "--flag=value"
// keeps the flag and masks the value, "--flag value" masks the next word.
func (s *Sanitizer) redactFlags(t *tally, str string) string {
	var b strings.Builder
	last, maskNext, maskColon := 0, false, false
	for _, m := range tokenRE.FindAllStringIndex(str, -1) {
		tok := str[m[0]:m[1]]
		repl := ""
		name, val, hasEq := strings.Cut(tok, "=")
		switch {
		case maskNext:
			maskNext, repl = false, redacted
		case maskColon:
			maskColon = false
			if strings.Contains(tok, ":") {
				repl = redacted
			}
		case userFlagName(name):
			if !hasEq {
				maskColon = true
			} else if strings.Contains(val, ":") {
				repl = name + "=" + redacted
			}
		case s.secretFlagName(name):
			if hasEq {
				repl = name + "=" + redacted
			} else {
				maskNext = true
			}
		}
		if repl != "" && repl != tok {
			t.red++
			b.WriteString(str[last:m[0]] + repl)
			last = m[1]
		}
	}
	return b.String() + str[last:]
}

// foldText maps look-alike and invisible characters that would hide a keyword
// from the rules onto ASCII: format (Cf) runes are dropped, Unicode spaces and
// line/paragraph separators become ' ', and full-width ASCII forms become their
// ASCII counterparts.
//
// JSON string escapes are also undone for matching: a run of backslashes before
// a quote or slash is dropped, so a double-encoded body (`{\"password\":\"x\"}`)
// is seen as JSON. The persisted text is this folded text, never the original.
func foldText(str string) string {
	if strings.Contains(str, `\`) {
		str = escapedRE.ReplaceAllString(str, "$1")
	}
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Cf, r):
			return -1
		case unicode.Is(unicode.Zs, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			return ' '
		case r >= 0xFF01 && r <= 0xFF5E:
			return r - 0xFEE0
		}
		return r
	}, str)
}

// redact replaces secret-looking content in str (token-level where possible).
func (s *Sanitizer) redact(t *tally, str string) string {
	str = foldText(str)
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
	str = s.redactFlags(t, str)
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

// Free-text work bounds. The rules run on at most redactWindow bytes of one
// string and on at most redactBudget bytes per record, so adversarial 1 MiB
// inputs cost milliseconds rather than seconds. Anything beyond is truncated or
// omitted (never persisted unredacted).
const (
	redactWindow = 8192
	redactBudget = 256 << 10
)

// windowText returns the prefix of str that is redacted: at most redactWindow
// bytes, cut at a UTF-8 boundary and, when whitespace allows, before the last
// token so a secret is never cut in the middle by the window.
func (s *Sanitizer) windowText(str string) string {
	if len(str) <= s.window {
		return str
	}
	w := truncUTF8(str, s.window)
	if i := strings.LastIndexAny(w, " \t"); i > 0 {
		w = w[:i]
	}
	return w
}

// text sanitizes free text: multiline omission, secret redaction, truncation
// at a UTF-8 boundary. The truncation suffix is appended after the cap. Text
// beyond the redaction window is dropped and counted in the suffix.
func (s *Sanitizer) text(t *tally, str string) string {
	if strings.Contains(strings.TrimSuffix(str, "\n"), "\n") {
		t.trunc++
		return "[OMITTED:multiline " + lengthBucket(len(str)) + "]"
	}
	w := s.windowText(str)
	if t.scan += len(w); t.scan > s.budget {
		t.trunc++
		return "[OMITTED:budget]"
	}
	out := s.redact(t, w)
	dropped := len(str) - len(w)
	if len(out) > s.maxString {
		kept := truncUTF8(out, s.maxString)
		t.trunc++
		return fmt.Sprintf("%s...[truncated %d bytes]", kept, len(out)-len(kept)+dropped)
	}
	if dropped > 0 {
		t.trunc++
		return fmt.Sprintf("%s...[truncated %d bytes]", out, dropped)
	}
	return out
}

// scalarLimits returns the byte limit, the extra ASCII characters and whether
// non-ASCII letters, marks and digits are allowed (paths and locators may hold
// spaces and non-ASCII names; every secret rule still runs afterwards).
func scalarLimits(k ScalarKind) (limit int, extra string, wide bool) {
	switch k {
	case ScalarLocator:
		return maxLocatorBytes, " ", true
	case ScalarMediaType:
		return maxScalarBytes, "+", false
	}
	return maxScalarBytes, "", false
}

// validScalar implements ^[A-Za-z0-9._:/@=-]{0,limit}$ plus extra characters and,
// when wide, non-ASCII letters, marks and numbers (control, format and
// separator runes and invalid UTF-8 are never valid).
func validScalar(v string, limit int, extra string, wide bool) bool {
	if len(v) > limit {
		return false
	}
	for _, r := range v {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r < 0x80 && strings.ContainsRune("._:/@=-"+extra, r) ||
			wide && r >= 0x80 && r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r))
		if !ok {
			return false
		}
	}
	return true
}

func (s *Sanitizer) scalar(t *tally, kind ScalarKind, v string) string {
	limit, extra, wide := scalarLimits(kind)
	if !validScalar(v, limit, extra, wide) {
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

// key returns the persisted form of a map key; ok is false when the key was
// replaced (invalid or secret-looking), in which case the value must not be
// kept either (default-deny: the deny check cannot see the original name).
func (s *Sanitizer) key(t *tally, k string, ordinal int) (string, bool) {
	var scratch tally
	if !validScalar(k, maxKeyBytes, "", false) || s.redact(&scratch, k) != k {
		t.red++
		return fmt.Sprintf("[INVALID_KEY_%d]", ordinal), false
	}
	return k, true
}

// omitBytes replaces []byte values (found in maps and slices) by a marker so
// their base64 form is never persisted. Maps and slices are copied; other
// values (including structs with byte fields) pass through to the heuristics.
func omitBytes(t *tally, v any, depth int) any {
	if depth > DefaultMaxDepth+1 {
		return v
	}
	switch x := v.(type) {
	case []byte:
		t.trunc++
		return "[OMITTED:bytes]"
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = omitBytes(t, e, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = omitBytes(t, e, depth+1)
		}
		return out
	}
	return v
}

func (s *Sanitizer) walk(t *tally, v any, depth int) any {
	if depth > s.maxDepth {
		t.trunc++
		t.replaced = true
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
		pairKey := s.pairValueKey(x)
		for i, k := range keys {
			if i >= s.maxEntries {
				out["_truncated"] = json.Number(strconv.Itoa(len(keys) - i))
				t.trunc++
				break
			}
			ek, valid := s.key(t, k, i)
			if repl, ok := s.keyRule(k, x[k]); ok || (pairKey != "" && k == pairKey) {
				if !ok {
					repl = redacted
				}
				t.red++
				out[ek] = repl
				continue
			}
			if !valid {
				t.red++
				out[ek] = redacted
				continue
			}
			out[ek] = s.walk(t, x[k], depth+1)
		}
		return out
	case []any:
		n := min(len(x), s.maxEntries)
		out := make([]any, 0, n+1)
		for i, e := range x[:n] {
			if i > 0 && s.maskValue(x[i-1], e) { // the value of a preceding --password style flag
				t.red++
				out = append(out, redacted)
				continue
			}
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

// maskValue reports whether cur is the value of the credential flag prev
// ("--flag=value" carries its value inline and is handled as text); a user flag
// masks only a "user:password" value.
func (s *Sanitizer) maskValue(prev, cur any) bool {
	str, ok := prev.(string)
	if !ok || strings.Contains(str, "=") {
		return false
	}
	if userFlagName(str) {
		c, _ := cur.(string)
		return strings.Contains(c, ":")
	}
	return s.secretFlagName(str)
}

// pairValueKey implements name/value pairs ({"name":"DB_PASSWORD","value":"x"},
// the Kubernetes env shape, or {"key":..,"value":..} headers): when a name-like
// string field is a credential name, the sibling "value" key is returned so the
// walk redacts it. It returns "" when the map is not such a pair.
func (s *Sanitizer) pairValueKey(m map[string]any) string {
	valKey := ""
	var val any
	for k, e := range m {
		if strings.EqualFold(k, "value") {
			valKey, val = k, e
		}
	}
	if valKey == "" {
		return ""
	}
	for k, e := range m {
		if name, ok := e.(string); ok && (strings.EqualFold(k, "name") || strings.EqualFold(k, "key")) &&
			s.denyHit(name, tokenAllowed(normalizeKey(name), val)) {
			return valKey
		}
	}
	return ""
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
	switch v.(type) {
	case []byte:
		return []byte(`"[OMITTED:bytes]"`), tally{trunc: 1}
	}
	v = omitBytes(&t, v, 0)
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
	if err := dec.Decode(&tree); err != nil { // e.g. nesting beyond the decoder limit
		return []byte(unserializableDoc), tally{replaced: true}
	}
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

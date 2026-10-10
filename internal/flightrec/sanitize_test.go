package flightrec

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
)

type panickingJSON struct{}

func (panickingJSON) MarshalJSON() ([]byte, error) { panic("boom") }

type fakeProto struct{}

func (fakeProto) ProtoReflect() {}

func sanitized(t *testing.T, s *Sanitizer, v any) (string, wire.Sanitization) {
	t.Helper()
	out, san, err := s.JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), san
}

func TestSanitizerConfig(t *testing.T) {
	if _, err := NewSanitizer(SanitizerConfig{MaxStringBytes: 10, MaxDepth: 2, MaxEntries: 2, MaxJSONBytes: 100,
		ExtraDenyKeys: []string{"My-Key"}, ExtraPatterns: []*regexp.Regexp{regexp.MustCompile(`corp-\d+`)}}); err != nil {
		t.Fatal(err)
	}
	bad := []SanitizerConfig{
		{MaxStringBytes: DefaultMaxStringBytes + 1}, {MaxDepth: -1}, {MaxEntries: 1 << 20}, {MaxJSONBytes: DefaultMaxJSONBytes + 1},
		{ExtraDenyKeys: []string{"-_. "}}, {ExtraPatterns: []*regexp.Regexp{nil}},
	}
	for i, cfg := range bad {
		if _, err := NewSanitizer(cfg); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	custom, _ := NewSanitizer(SanitizerConfig{ExtraDenyKeys: []string{"my-key"}, ExtraPatterns: []*regexp.Regexp{regexp.MustCompile(`corp-\d+`)}})
	got, san := sanitized(t, custom, map[string]any{"MyKey": "x", "note": "see corp-42 now"})
	if got != `{"MyKey":"[REDACTED]","note":"see [REDACTED] now"}` || san.Redactions != 2 {
		t.Fatalf("got %s %+v", got, san)
	}
}

func TestSanitizerKeys(t *testing.T) {
	s := DefaultSanitizer()
	redactedKeys := []string{
		"password", "PassWd", "client_secret", "access-token", "api_key", "API.KEY", "Authorization", "credentials",
		"private_key", "Set-Cookie", "bearer", "session_id", "auth_token", "secret_tokens", "refreshToken",
	}
	for _, k := range redactedKeys {
		got, san := sanitized(t, s, map[string]any{k: "v"})
		if !strings.Contains(got, `"[REDACTED]"`) || san.Redactions == 0 {
			t.Errorf("key %q not redacted: %s", k, got)
		}
	}
	rawKeys := []string{"prompt", "completion", "source", "contents", "content", "body", "diff", "stdout", "stderr", "env", "environ", "environment", "config_contents", "raw-config"}
	for _, k := range rawKeys {
		got, _ := sanitized(t, s, map[string]any{k: "anything"})
		if !strings.Contains(got, `"[OMITTED:raw]"`) {
			t.Errorf("raw key %q not omitted: %s", k, got)
		}
	}
	kept := map[string]any{"input_tokens": 1, "output_tokens": 2, "max_tokens": 3, "total_tokens": 4, "token_count": 5, "token-limit": 6, "tokenUsage": 7}
	got, san := sanitized(t, s, kept)
	if strings.Contains(got, "REDACTED") || san.Redactions != 0 {
		t.Fatalf("allow-exceptions redacted: %s", got)
	}
	for _, k := range []string{"tokenizer", "token"} {
		if got, _ := sanitized(t, s, map[string]any{k: "x"}); !strings.Contains(got, "REDACTED") {
			t.Fatalf("%q must stay redacted: %s", k, got)
		}
	}
}

func TestSanitizerValuePatterns(t *testing.T) {
	s := DefaultSanitizer()
	digest := "sha256:" + strings.Repeat("ab", 32)
	cases := map[string]string{
		"sk-abcdef123456":                  "[REDACTED]",
		"ghp_abcdefghijkl":                 "[REDACTED]",
		"bearer abc.def":                   "[REDACTED]",
		"see sk-abcdef123456 now":          "see [REDACTED] now",
		"API_KEY=abc":                      "[REDACTED]",
		"run FOO_BAR=baz --x":              "run [REDACTED] --x",
		"https://user:pw@example.com/path": "https://[REDACTED]@example.com/path",
		"Authorization: Bearer abc123":     "[REDACTED]",
		"-----BEGIN RSA PRIVATE KEY-----":  "[REDACTED]",
		"tok eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcde12345": "tok [REDACTED]",
		"digest " + digest: "digest " + digest,
		"plain text":       "plain text",
	}
	for in, want := range cases {
		got, _ := sanitized(t, s, in)
		if got != `"`+want+`"` {
			t.Errorf("%q: got %s want %q", in, got, want)
		}
	}
	// counts: one URL userinfo + one secret token
	_, san := sanitized(t, s, "https://u:p@h/x sk-abcdef123456")
	if san.Redactions != 2 {
		t.Fatalf("redactions %d", san.Redactions)
	}
}

func TestSanitizerBounds(t *testing.T) {
	s := DefaultSanitizer()

	// Multiline omission and buckets.
	for _, tc := range []struct {
		n    int
		want string
	}{{100, "256b"}, {800, "1k"}, {3000, "4k"}, {10000, "16k"}, {20000, "over16k"}} {
		in := "a\n" + strings.Repeat("b", tc.n)
		got, san := sanitized(t, s, in)
		if want := `"[OMITTED:multiline ` + tc.want + `]"`; got != want || san.Truncations != 1 {
			t.Errorf("n=%d got %s %+v", tc.n, got, san)
		}
	}
	// A trailing newline alone is one line.
	if got, _ := sanitized(t, s, "one line\n"); got != `"one line\n"` {
		t.Fatalf("got %s", got)
	}

	// Truncation at a UTF-8 boundary with suffix.
	in := "a" + strings.Repeat("é", 200) // rune starts at odd offsets: byte 256 is a continuation byte
	got, san := sanitized(t, s, in)
	var str string
	if err := json.Unmarshal([]byte(got), &str); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(str, "...[truncated 146 bytes]") || san.Truncations != 1 {
		t.Fatalf("got %q %+v", str, san)
	}
	kept := strings.TrimSuffix(str, "...[truncated 146 bytes]")
	if len(kept) != 255 || !strings.HasPrefix(in, kept) {
		t.Fatalf("kept %d bytes", len(kept))
	}

	// Depth.
	deep := map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": map[string]any{"e": map[string]any{"f": map[string]any{"g": 1}}}}}}}
	if got, _ := sanitized(t, s, deep); got != `{"a":{"b":{"c":{"d":{"e":{"f":{"g":"[DEPTH_LIMIT]"}}}}}}}` {
		t.Fatalf("got %s", got)
	}

	// Entries: object keeps the first N sorted keys, array keeps the first N.
	obj := map[string]any{}
	arr := []any{}
	for i := 0; i < 70; i++ {
		obj[string(rune('a'+i/26))+string(rune('a'+i%26))] = i
		arr = append(arr, i)
	}
	gotObj, _ := sanitized(t, s, obj)
	var decObj map[string]any
	if err := json.Unmarshal([]byte(gotObj), &decObj); err != nil {
		t.Fatal(err)
	}
	if len(decObj) != 65 || decObj["_truncated"] != float64(6) {
		t.Fatalf("object: %d entries, _truncated=%v", len(decObj), decObj["_truncated"])
	}
	gotArr, san := sanitized(t, s, arr)
	var decArr []any
	if err := json.Unmarshal([]byte(gotArr), &decArr); err != nil {
		t.Fatal(err)
	}
	if len(decArr) != 65 || san.Truncations != 1 || decArr[64].(map[string]any)["_truncated"] != float64(6) {
		t.Fatalf("array: %d entries %+v", len(decArr), san)
	}

	// Total document bound.
	big := map[string]any{}
	for i := 0; i < 64; i++ {
		big[strings.Repeat("k", 10)+string(rune('a'+i/26))+string(rune('a'+i%26))] = strings.Repeat("v", 250)
	}
	gotBig, san := sanitized(t, s, big)
	if !strings.HasPrefix(gotBig, `{"_truncated":true,"_original_bytes":`) || !san.PayloadReplaced {
		t.Fatalf("got %.80s %+v", gotBig, san)
	}

	// Pre-marshal bounds: oversize string, bytes and a large marshalled value.
	for _, v := range []any{strings.Repeat("x", MaxMarshalBytes+1), make([]byte, MaxMarshalBytes+1)} {
		if got, san := sanitized(t, s, v); !strings.HasPrefix(got, `{"_truncated":true,"_original_bytes":`) || !san.PayloadReplaced {
			t.Fatalf("got %.80s %+v", got, san)
		}
	}
	var many []string
	for i := 0; i < 1100; i++ {
		many = append(many, strings.Repeat("y", 1000))
	}
	if got, san := sanitized(t, s, many); !strings.HasPrefix(got, `{"_truncated":true,"_original_bytes":`) || !san.PayloadReplaced {
		t.Fatalf("got %.80s %+v", got, san)
	}
}

func TestSanitizerReplacementAndDeterminism(t *testing.T) {
	s := DefaultSanitizer()
	for _, v := range []any{panickingJSON{}, make(chan int), math.NaN(), func() {}} {
		got, san := sanitized(t, s, v)
		if got != `{"_unserializable":true}` || !san.PayloadReplaced {
			t.Errorf("%T: got %s %+v", v, got, san)
		}
	}
	if out, san, err := s.JSON(nil); err != nil || out != nil || san.Redactions != 0 || san.Truncations != 0 || san.PayloadReplaced {
		t.Fatalf("nil: %q %+v %v", out, san, err)
	}
	in := map[string]any{"z": 1, "a": []any{true, nil, 1.5, "s"}, "m": map[string]any{"y": 2, "b": 3}}
	first, _ := sanitized(t, s, in)
	for i := 0; i < 20; i++ {
		if again, _ := sanitized(t, s, in); again != first {
			t.Fatalf("nondeterministic: %s vs %s", first, again)
		}
	}
	if first != `{"a":[true,null,1.5,"s"],"m":{"b":3,"y":2},"z":1}` {
		t.Fatalf("got %s", first)
	}
}

func TestSanitizerMapKeys(t *testing.T) {
	s := DefaultSanitizer()
	in := map[string]any{
		"ok_key":                1,
		"has space":             2,
		strings.Repeat("k", 65): 3,
		"api_key=1":             4,
		"[INVALID_KEY_0]":       5,
		"password":              "x",
	}
	got, san := sanitized(t, s, in)
	// sorted keys: "[INVALID_KEY_0]"(0) "api_key=1"(1) "has space"(2) kkk..(3) ok_key(4) password(5)
	// default-deny: a replaced key never keeps its value
	want := `{"[INVALID_KEY_0]":"[REDACTED]","[INVALID_KEY_1]":"[REDACTED]","[INVALID_KEY_2]":"[REDACTED]","[INVALID_KEY_3]":"[REDACTED]","ok_key":1,"password":"[REDACTED]"}`
	if got != want {
		t.Fatalf("got %s", got)
	}
	if san.Redactions != 9 {
		t.Fatalf("redactions %d", san.Redactions)
	}
}

func TestSanitizerScalar(t *testing.T) {
	s := DefaultSanitizer()
	cases := []struct {
		kind ScalarKind
		in   string
		want string
	}{
		{ScalarID, "task_01:a/b@c=d.e-f", "task_01:a/b@c=d.e-f"},
		{ScalarID, "", ""},
		{ScalarID, "has space", "[INVALID]"},
		{ScalarID, strings.Repeat("a", 129), "[INVALID]"},
		{ScalarID, strings.Repeat("a", 128), strings.Repeat("a", 128)},
		{ScalarLocator, strings.Repeat("a", 256), strings.Repeat("a", 256)},
		{ScalarLocator, strings.Repeat("a", 257), "[INVALID]"},
		{ScalarLocator, "/tmp/with space/x", "[INVALID]"},
		{ScalarLocator, "/tmp/API_KEY=abc", "[REDACTED]"},
		{ScalarLocator, "https://u:p@h/x", "[REDACTED]"},
		{ScalarID, "sk-abcdef123456", "[REDACTED]"},
		{ScalarMediaType, "application/vnd.api+json", "application/vnd.api+json"},
		{ScalarID, "application/vnd.api+json", "[INVALID]"},
		{"unknown-kind", "plain", "plain"},
	}
	for _, c := range cases {
		if got := s.Scalar(c.kind, c.in); got != c.want {
			t.Errorf("%s %q: got %q want %q", c.kind, c.in, got, c.want)
		}
	}
}

func TestSanitizerRejectsAnyPayloads(t *testing.T) {
	s := DefaultSanitizer()
	for _, v := range []any{wire.Any{TypeURL: "x"}, &wire.Any{}, fakeProto{}, &fakeProto{}} {
		if _, _, err := s.JSON(v); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("%T accepted: %v", v, err)
		}
	}
	if _, _, err := s.JSON(struct{ A int }{1}); err != nil {
		t.Fatal(err)
	}
}

func TestErrorTextSanitized(t *testing.T) {
	s := DefaultSanitizer()
	var tl tally
	got := s.text(&tl, errors.New("failed with token=abcdef").Error())
	if got != "[REDACTED]" || tl.red != 1 {
		t.Fatalf("got %q %+v", got, tl)
	}
}

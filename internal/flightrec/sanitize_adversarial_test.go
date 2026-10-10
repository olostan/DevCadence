package flightrec

import (
	"strings"
	"testing"
)

// B1: invisible or look-alike key characters must not smuggle a secret value
// past the key deny check.
func TestSanitizerKeyEvasionRedactsValue(t *testing.T) {
	s := DefaultSanitizer()
	keys := []string{
		"api​_key",        // zero-width space (Cf)
		"pаssword",        // Cyrillic a
		"pass­word",       // soft hyphen (Cf)
		"to⁠ken",          // word joiner (Cf)
		"sk-abcdef123456", // valid chars, secret-looking key
		"has space",       // invalid key
		strings.Repeat("k", 65),
	}
	for _, k := range keys {
		got, san := sanitized(t, s, map[string]any{k: "VALUE1"})
		if strings.Contains(got, "VALUE1") || san.Redactions == 0 {
			t.Errorf("key %q leaked: %s", k, got)
		}
	}
	if got, _ := sanitized(t, s, map[string]any{"api​_key": "VALUE1"}); got != `{"[INVALID_KEY_0]":"[REDACTED]"}` {
		t.Fatalf("got %s", got)
	}
	if n := normalizeKey("API​_Key"); n != "apikey" {
		t.Fatalf("normalizeKey %q", n)
	}
}

// B2: only exact numeric counters keep a token-named value.
func TestSanitizerTokenKeysBypass(t *testing.T) {
	s := DefaultSanitizer()
	for _, k := range []string{"access_tokens", "auth_tokens", "refresh_tokens", "tokens", "input_tokens_secret"} {
		got, _ := sanitized(t, s, map[string]any{k: "VALUE2"})
		if strings.Contains(got, "VALUE2") {
			t.Errorf("%q leaked: %s", k, got)
		}
	}
	// A counter name with a non-numeric value is redacted.
	for _, v := range []any{"VALUE3", map[string]any{"x": "VALUE3"}, []any{"VALUE3"}, true} {
		if got, _ := sanitized(t, s, map[string]any{"input_tokens": v}); strings.Contains(got, "VALUE3") || strings.Contains(got, "true") {
			t.Errorf("non-numeric counter kept: %s", got)
		}
	}
	counters := map[string]any{
		"input_tokens": 1, "output_tokens": 2, "total_tokens": 3, "token_count": 4, "token_limit": 5,
		"token_usage": 6, "max_tokens": 7, "cached_tokens": 8, "reasoning_tokens": 9,
	}
	if got, san := sanitized(t, s, counters); strings.Contains(got, "REDACTED") || san.Redactions != 0 {
		t.Fatalf("counters redacted: %s", got)
	}
}

// B3: credentials in free text and under innocuous keys.
func TestSanitizerFreeTextCredentials(t *testing.T) {
	s := DefaultSanitizer()
	leaks := []string{
		"https://VALUE13@h/",
		"request failed: Bearer abcdef",
		"password: VALUE4",
		"Passwd VALUE5",
		"x-api-key: VALUE17",
		"authorization=Basic VALUE16",
		"token abc",
		"--password VALUE6",
		"--token=VALUE7",
		"run --api-key VALUE8 now",
		"git clone https://ghp_VALUE11@github.com/a/b",
		"git clone https://ghp_VALUE12abc/a/b",
		"cloning with glpat-VALUE18abc and more",
		"slack xoxb-VALUE19abc sent",
		"aws AKIAVALUE20ABCDEFGH used",
		"key AIzaVALUE21abcdefgh used",
		"pwd=VALUE22",
		"secret: VALUE23 leaked",
		`{"password":"VALUE24"}`,
		"connect ssh://deploy@host/x",
		"npm_VALUE25abc failed",
		"github_pat_VALUE26abc",
	}
	for _, in := range leaks {
		for _, v := range []any{in, map[string]any{"note": in}, []any{"a", in}} {
			got, san := sanitized(t, s, v)
			if strings.Contains(got, "VALUE") || strings.Contains(got, "abcdef\"") || strings.Contains(got, "abc\"") || strings.Contains(got, "deploy") || san.Redactions == 0 {
				t.Errorf("%q leaked: %s", in, got)
			}
		}
	}
	// Redaction does not reveal the secret length.
	a, _ := sanitized(t, s, "password: x")
	b, _ := sanitized(t, s, "password: "+strings.Repeat("y", 50))
	if a != b {
		t.Fatalf("length leaks: %s vs %s", a, b)
	}
}

func TestSanitizerSecretFlagArrays(t *testing.T) {
	s := DefaultSanitizer()
	got, san := sanitized(t, s, []any{"deploy", "--password", "VALUE9", "--env", "prod", "--api-key", "VALUE10", "-token", "VALUE11"})
	if strings.Contains(got, "VALUE") || san.Redactions != 3 || !strings.Contains(got, `"--env","prod"`) {
		t.Fatalf("got %s %+v", got, san)
	}
	if got, _ := sanitized(t, s, []any{"--password", 5, "tail"}); strings.Contains(got, "5") || !strings.Contains(got, "tail") {
		t.Fatalf("got %s", got)
	}
	if got, _ := sanitized(t, s, []any{1, "--password"}); got != `[1,"--password"]` {
		t.Fatalf("got %s", got)
	}
}

func TestSanitizerNewDenyKeys(t *testing.T) {
	s := DefaultSanitizer()
	for _, k := range []string{"pwd", "auth", "accessKey", "ssh_key", "signature", "session", "jwt", "cookie", "bearer", "credential", "credentials", "x-auth"} {
		if got, _ := sanitized(t, s, map[string]any{k: "VALUE14"}); strings.Contains(got, "VALUE14") {
			t.Errorf("%q leaked: %s", k, got)
		}
	}
}

// Legitimate diagnostics must survive.
func TestSanitizerNegativeFreeText(t *testing.T) {
	s := DefaultSanitizer()
	keep := []string{
		"QUOTA_EXCEEDED", "reason: timeout", "task_01HZX:attempt-3", "gpt-4", "gpt-4o-mini", "claude-sonnet-5",
		"main.go:42", "internal/flightrec/sanitize.go", "duration 1.5s", "retry in 30 seconds",
		"token-expired", "tokens exhausted", "tokenizer ready", "disk-usage-high", "task-abcdefghi done",
		"password", "the secret", "basic ok", "bearer", "risk-assessment complete", "git@github.com:a/b.git",
		"https://example.com/path?x=1",
	}
	for _, in := range keep {
		if got, san := sanitized(t, s, in); got != `"`+in+`"` || san.Redactions != 0 {
			t.Errorf("%q over-redacted: %s %+v", in, got, san)
		}
	}
}

// N1: byte slices are never persisted (not even base64).
func TestSanitizerOmitsBytes(t *testing.T) {
	s := DefaultSanitizer()
	if got, san := sanitized(t, s, []byte("VALUE15 secret bytes")); got != `"[OMITTED:bytes]"` || san.Truncations != 1 {
		t.Fatalf("got %s %+v", got, san)
	}
	got, san := sanitized(t, s, map[string]any{"blob": []byte("VALUE15"), "list": []any{[]byte("VALUE15"), 1}, "n": 2})
	if strings.Contains(got, "VALUE15") || strings.Contains(got, "VkFMVUUx") || san.Truncations != 2 {
		t.Fatalf("got %s %+v", got, san)
	}
	// The caller's data is not mutated.
	in := map[string]any{"blob": []byte("x")}
	_, _, _ = s.JSON(in)
	if _, ok := in["blob"].([]byte); !ok {
		t.Fatal("input mutated")
	}
	// Beyond the pre-walk depth the value passes through to the heuristics.
	var deep any = []byte("x")
	for i := 0; i < DefaultMaxDepth+4; i++ {
		deep = []any{deep}
	}
	if _, _, err := s.JSON(deep); err != nil {
		t.Fatal(err)
	}
}

// N2: the depth limit marks the payload replaced; undecodable output is an
// unserializable replacement, not a silent empty tree.
func TestSanitizerDepthAndDecodeErrors(t *testing.T) {
	s := DefaultSanitizer()
	deep := map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": map[string]any{"e": map[string]any{"f": map[string]any{"g": 1}}}}}}}
	if _, san := sanitized(t, s, deep); !san.PayloadReplaced || san.Truncations != 1 {
		t.Fatalf("%+v", san)
	}
	var nested any = 1
	for i := 0; i < 10100; i++ { // json.Marshal accepts this; the decoder limit is 10000
		nested = []any{nested}
	}
	if got, san := sanitized(t, s, nested); got != `{"_unserializable":true}` || !san.PayloadReplaced {
		t.Fatalf("got %.40s %+v", got, san)
	}
}

package flightrec

import (
	"strings"
	"testing"
)

func TestNameSegments(t *testing.T) {
	cases := map[string]string{
		"db_pass":     "db|pass",
		"DB_PASS":     "db|pass",
		"dbPass":      "db|pass",
		"DBPass":      "db|pass",
		"mysql-pw":    "mysql|pw",
		"db2Pass":     "db2|pass",
		"a.b c":       "a|b|c",
		"pa\u200bss":  "pass",
		"":            "",
		"__":          "",
		"HTTPServer":  "http|server",
		"userOTPCode": "user|otp|code",
	}
	for in, want := range cases {
		if got := strings.Join(nameSegments(in), "|"); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestDenyHitSegmentsAndExact(t *testing.T) {
	s := DefaultSanitizer()
	for _, n := range []string{"db_pass", "DB_PASS", "admin_pass", "mysql_pw", "dbPass", "user-otp", "key", "sig", "salt", "license", "seed", "userpass", "accountkey", "oauth_state", "x-auth"} {
		if !s.denyHit(n, false) {
			t.Errorf("%q must be a credential name", n)
		}
	}
	for _, n := range []string{"author", "authors", "authority", "authoritative", "authorName", "cache_key", "sort_key", "salty", "seeded", "bypass", "passes", "licensed", "signal", "keyboard", "retries"} {
		if s.denyHit(n, false) {
			t.Errorf("%q must not be a credential name", n)
		}
	}
	if !s.denyHit("max_tokens", false) || s.denyHit("max_tokens", true) {
		t.Fatal("token counter exemption")
	}
}

func TestFoldTextUnescapesJSONQuotes(t *testing.T) {
	for in, want := range map[string]string{
		`{\"a\":\"b\"}`:       `{"a":"b"}`,
		`{\\\"a\\\":1}`:       `{"a":1}`,
		`https:\/\/h\/x`:      `https://h/x`,
		`it\'s`:               `it's`,
		`C:\dir\file`:         `C:\dir\file`,
		"no backslash at all": "no backslash at all",
	} {
		if got := foldText(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

// TestSanitizerWindowAndBudget: text beyond the redaction window is dropped and
// counted, a secret never survives the cut, and the per-record budget omits
// further text. A small window and budget keep the test fast.
func TestSanitizerWindowAndBudget(t *testing.T) {
	s := DefaultSanitizer()
	if s.window != redactWindow || s.budget != redactBudget {
		t.Fatalf("defaults %d %d", s.window, s.budget)
	}
	s.window, s.budget = 64, 300
	var tl tally
	long := strings.Repeat("word ", 40) + "token=" + round3Marker
	got := s.text(&tl, long)
	if !strings.Contains(got, "...[truncated ") || strings.Contains(got, round3Marker) || tl.trunc != 1 {
		t.Fatalf("%q %+v", got, tl)
	}
	// No whitespace to cut at: the window is cut mid-token.
	tl = tally{}
	if got := s.text(&tl, strings.Repeat("a", 200)); !strings.HasPrefix(got, strings.Repeat("a", 64)+"...[truncated 136 bytes]") || tl.trunc != 1 {
		t.Fatalf("%q", got)
	}
	// Redaction shrinks the window below the cap: the dropped count is reported.
	tl = tally{}
	got = s.text(&tl, "Authorization: "+strings.Repeat("x ", 100))
	if got != "[REDACTED]...[truncated "+strings.TrimPrefix(got, "[REDACTED]...[truncated ") || !strings.HasPrefix(got, "[REDACTED]...[truncated ") || tl.trunc != 1 {
		t.Fatalf("%q", got)
	}
	// Budget: after s.budget bytes of text in one record, text is omitted.
	tl = tally{}
	var outs []string
	for i := 0; i < 8; i++ {
		outs = append(outs, s.text(&tl, strings.Repeat("a", 60)))
	}
	if outs[4] != strings.Repeat("a", 60) || outs[5] != "[OMITTED:budget]" || outs[7] != "[OMITTED:budget]" || tl.trunc != 3 {
		t.Fatalf("%q %+v", outs, tl)
	}
	if (tally{scan: 5}).ptr() != nil || tl.ptr() == nil {
		t.Fatal("scan alone is not a counter")
	}
}

func TestSanitizerPairValueKey(t *testing.T) {
	s := DefaultSanitizer()
	for _, c := range []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"name": "DB_PASSWORD", "value": "x"}, `{"name":"[REDACTED]","value":"[REDACTED]"}`},
		{map[string]any{"name": "REGION", "value": "x"}, `{"name":"REGION","value":"x"}`},
		{map[string]any{"name": "REGION"}, `{"name":"REGION"}`},
		{map[string]any{"name": 5, "value": "x"}, `{"name":5,"value":"x"}`},
		{map[string]any{"name": "input_tokens", "value": 5}, `{"name":"input_tokens","value":5}`},
	} {
		if got, _ := sanitized(t, s, c.in); got != c.want && !strings.Contains(c.want, "REDACTED") {
			t.Errorf("%v: got %s want %s", c.in, got, c.want)
		} else if strings.Contains(c.want, "REDACTED") && strings.Contains(got, `"value":"x"`) {
			t.Errorf("%v: value kept: %s", c.in, got)
		}
	}
}

func TestSanitizerUserFlags(t *testing.T) {
	s := DefaultSanitizer()
	for in, want := range map[string]string{
		"curl -u a:b h":        "curl -u [REDACTED] h",
		"curl --user a:b h":    "curl --user [REDACTED] h",
		"curl --user=a:b h":    "curl --user=[REDACTED] h",
		"curl -u alice h":      "curl -u alice h",
		"curl --user=alice h":  "curl --user=alice h",
		"docker run -u 1000 x": "docker run -u 1000 x",
	} {
		if got := s.text(&tally{}, in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

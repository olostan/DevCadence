package flightrec

import (
	"strings"
	"testing"
)

// R2-B2: flag names are decided by the same deny list as structured keys; an
// inline --flag=value keeps the following element.
func TestSanitizerFlagSharesKeyDenyList(t *testing.T) {
	s := DefaultSanitizer()
	for _, f := range []string{"--client_secret", "--secret-key", "--private-key", "--access-key", "--access_token", "--db_password", "--passphrase", "--pass", "--bearer", "--auth", "--cookie", "--api_key", "-token"} {
		if !s.secretFlagName(f) || !s.denyHit(normalizeKey(strings.TrimLeft(f, "-")), false) {
			t.Errorf("%s not a secret flag", f)
		}
	}
	if s.secretFlagName("verbose") || s.secretFlagName("--verbose") || s.secretFlagName("-5") {
		t.Fatal("benign flag treated as secret")
	}
	if got, _ := sanitized(t, s, []any{"--token=x", "tail"}); got != `["[REDACTED]","tail"]` {
		t.Fatalf("got %s", got)
	}
	got, _ := sanitized(t, s, "--private-key LEAKVAL9 --verbose  --secret-key=LEAKVAL9")
	if containsMarker(got) || !strings.Contains(got, "--verbose") {
		t.Fatalf("got %s", got)
	}
}

// A configured extra deny key applies to flags too (one shared decision).
func TestSanitizerExtraDenyKeyAppliesToFlags(t *testing.T) {
	s, err := NewSanitizer(SanitizerConfig{ExtraDenyKeys: []string{"corp-key"}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := sanitized(t, s, "run --corp-key=LEAKVAL9 --corp_key LEAKVAL9 --ok 1")
	if containsMarker(got) || !strings.Contains(got, "--corp-key=[REDACTED]") || !strings.Contains(got, "--ok 1") {
		t.Fatalf("got %s", got)
	}
	if got, _ := sanitized(t, s, map[string]any{"corpKey": "LEAKVAL9"}); containsMarker(got) {
		t.Fatalf("got %s", got)
	}
}

// R2-NB(a): invisible, Unicode-space and full-width forms are folded to ASCII
// before the rules run.
func TestFoldText(t *testing.T) {
	if got := foldText("pass\u200bword\u00a0\uff1d\u2028x\u3000\uff21"); got != "password = x A" {
		t.Fatalf("got %q", got)
	}
	if foldText("plain ascii: 1") != "plain ascii: 1" {
		t.Fatal("ascii changed")
	}
}

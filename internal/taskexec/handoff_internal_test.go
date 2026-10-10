package taskexec

import "testing"

func TestParseNameStatusZ(t *testing.T) {
	got := parseNameStatusZ([]byte("M\x00a.go\x00A\x00b dir/c.go\x00R100\x00old.go\x00new.go\x00D\x00gone.go\x00"))
	want := [][2]string{{"M", "a.go"}, {"A", "b dir/c.go"}, {"R100", "new.go"}, {"D", "gone.go"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		if got[i].Status != w[0] || got[i].Path != w[1] {
			t.Errorf("entry %d = %+v, want %v", i, got[i], w)
		}
	}
	if n := len(parseNameStatusZ([]byte("M\x00"))); n != 0 {
		t.Errorf("truncated input yields %d entries", n)
	}
}

func TestParseModelIdentityAndQuote(t *testing.T) {
	m := parseModelIdentity("ollama-local/qwen2.5-coder:7b@sha256:abc")
	if m.EndpointID != "ollama-local" || m.Model != "qwen2.5-coder:7b" || m.Digest != "sha256:abc" {
		t.Errorf("model = %+v", m)
	}
	if m := parseModelIdentity("m"); m.Model != "m" || m.EndpointID != "" || m.Digest != "" {
		t.Errorf("bare = %+v", m)
	}
	if got := shellQuote("/a b/it's"); got != `'/a b/it'\''s'` {
		t.Errorf("quote = %s", got)
	}
	if got := CandidateRefName("t", "a"); got != "refs/devcadence/candidates/t-a" {
		t.Errorf("ref = %s", got)
	}
}

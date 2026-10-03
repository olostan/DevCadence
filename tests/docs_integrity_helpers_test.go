package tests

import "testing"

func TestGitHubHeadingSlug(t *testing.T) {
	cases := map[string]string{
		"10B. Adaptive Context [Implemented - WP-M3C-1 / WP-M3C-2B]": "10b-adaptive-context-implemented---wp-m3c-1--wp-m3c-2b",
		"The `my_func` helper":        "the-my_func-helper",
		"See [the spec](SPEC.md) now": "see-the-spec-now",
		"Use <code>x</code> here":     "use-x-here",
		"**Bold** heading":            "bold-heading",
	}
	for in, want := range cases {
		if got := githubHeadingSlug(in); got != want {
			t.Errorf("githubHeadingSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripFencesHonoursFenceLength(t *testing.T) {
	text := "before\n````md\n```\n[bad](nowhere.md)\n```\n````\nafter [ok](x.md)\n"
	got := stripFences(text)
	if want := "before\n\n\n\n\n\nafter [ok](x.md)\n"; got != want {
		t.Fatalf("stripFences = %q, want %q", got, want)
	}
}

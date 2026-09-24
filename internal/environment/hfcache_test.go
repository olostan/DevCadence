package environment_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/environment"
)

func TestHuggingFaceCacheDirPrecedence(t *testing.T) {
	getenv := func(vars map[string]string) func(string) string {
		return func(key string) string { return vars[key] }
	}

	cases := []struct {
		name     string
		explicit string
		env      map[string]string
		homeDir  string
		want     string
	}{
		{
			name:    "falls back to homeDir/.cache/huggingface/hub with nothing set",
			homeDir: "/home/dev",
			want:    "/home/dev/.cache/huggingface/hub",
		},
		{
			name:    "XDG_CACHE_HOME wins over the homeDir default",
			env:     map[string]string{"XDG_CACHE_HOME": "/xdg-cache"},
			homeDir: "/home/dev",
			want:    "/xdg-cache/huggingface/hub",
		},
		{
			name:    "HF_HOME wins over XDG_CACHE_HOME",
			env:     map[string]string{"XDG_CACHE_HOME": "/xdg-cache", "HF_HOME": "/hf-home"},
			homeDir: "/home/dev",
			want:    "/hf-home/hub",
		},
		{
			name:    "HF_HUB_CACHE wins over HF_HOME",
			env:     map[string]string{"HF_HOME": "/hf-home", "HF_HUB_CACHE": "/hub-cache"},
			homeDir: "/home/dev",
			want:    "/hub-cache",
		},
		{
			name:     "an explicit override wins over everything, including HF_HUB_CACHE",
			explicit: "/explicit-override",
			env:      map[string]string{"HF_HUB_CACHE": "/hub-cache"},
			homeDir:  "/home/dev",
			want:     "/explicit-override",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := environment.HuggingFaceCacheDir(tc.explicit, getenv(tc.env), tc.homeDir)
			if got != tc.want {
				t.Errorf("HuggingFaceCacheDir(%q, env=%v, %q) = %q, want %q", tc.explicit, tc.env, tc.homeDir, got, tc.want)
			}
		})
	}
}

func TestHuggingFaceCacheDirNilGetenv(t *testing.T) {
	got := environment.HuggingFaceCacheDir("", nil, "/home/dev")
	want := "/home/dev/.cache/huggingface/hub"
	if got != want {
		t.Errorf("HuggingFaceCacheDir(\"\", nil, ...) = %q, want %q", got, want)
	}
}

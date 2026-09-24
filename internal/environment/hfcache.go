package environment

import "path"

// HuggingFaceCacheDir resolves the Hugging Face Hub cache directory using
// the same precedence the Hugging Face Hub client libraries themselves
// use (https://huggingface.co/docs/huggingface_hub/main/package_reference/environment_variables):
// an explicit override, then HF_HUB_CACHE, then HF_HOME/hub, then
// XDG_CACHE_HOME/huggingface/hub, then homeDir/.cache/huggingface/hub.
//
// This is the single resolver internal/setup's MLXAdapter (which installs
// and verifies models) and internal/cognition/mlx's Adapter (which
// discovers already-cached models for inference) both call, so the two
// layers can never independently drift on where "the" Hugging Face cache
// is — a model M3B installs into one location and M3A looks for in
// another would silently break the setup-then-discover contract this
// runtime-agnostic design depends on.
//
// getenv and homeDir are injected (never os.Getenv/os.UserHomeDir called
// directly here) so this package stays free of direct OS I/O and both
// callers can test every precedence branch with a fake. getenv returning
// "" for a variable is treated the same as it being unset. Paths are
// joined with "/" (path.Join, not filepath.Join): MLX-LM only runs on
// Darwin/arm64 today (gated at the call sites), where "/" is already the
// native separator, and internal/cognition/mlx's existing SysProbe
// abstraction is POSIX-path-only by design — a single join convention
// keeps both callers' paths comparable byte-for-byte.
func HuggingFaceCacheDir(explicit string, getenv func(string) string, homeDir string) string {
	if explicit != "" {
		return explicit
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if v := getenv("HF_HUB_CACHE"); v != "" {
		return v
	}
	if v := getenv("HF_HOME"); v != "" {
		return path.Join(v, "hub")
	}
	if v := getenv("XDG_CACHE_HOME"); v != "" {
		return path.Join(v, "huggingface", "hub")
	}
	return path.Join(homeDir, ".cache", "huggingface", "hub")
}

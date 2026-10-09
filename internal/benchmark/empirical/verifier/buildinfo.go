package verifier

import (
	"runtime/debug"
)

// defaultBuildInfoSource implements BuildInfoSource using runtime/debug.ReadBuildInfo.
type defaultBuildInfoSource struct{}

// VCSRevision inspects the running binary's debug.BuildInfo settings for vcs.revision and vcs.modified.
func (defaultBuildInfoSource) VCSRevision() (revision string, modified bool, ok bool) {
	info, readOK := debug.ReadBuildInfo()
	if !readOK {
		return "", false, false
	}
	var rev string
	var hasRev bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
			hasRev = true
		case "vcs.modified":
			modified = (s.Value == "true")
		}
	}
	if !hasRev || rev == "" {
		return "", false, false
	}
	return rev, modified, true
}

// DefaultBuildInfoSource returns the production BuildInfoSource inspecting debug.ReadBuildInfo.
func DefaultBuildInfoSource() BuildInfoSource {
	return defaultBuildInfoSource{}
}

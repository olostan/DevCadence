// Package validation implements validation-profile loading and execution
// (docs/IMPLEMENTATION_PLAN.md M2, docs/PROTOCOLS.md §10).
//
// A profile is a named, ordered list of deterministic checks: argv, timeout,
// and optional environment overrides. Execution runs each check through the
// controlled process runner, captures stdout/stderr as artifacts, and builds
// the real M1 protocol.ValidationResult — never a placeholder digest or a
// model-authored summary standing in for tool output (DCI-041).
package validation

import (
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"gopkg.in/yaml.v3"
)

// CheckSpec is one deterministic check within a profile.
type CheckSpec struct {
	// ID is a short stable name for the check, e.g. "go-test". Defaults to
	// the joined argv when empty.
	ID string `yaml:"id,omitempty" json:"id,omitempty"`
	// Kind classifies the check, e.g. "test", "lint", "build". Defaults to
	// ID when empty.
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
	// Argv is the exact command: executable followed by arguments. It is
	// never a shell string (docs/SECURITY.md §5).
	Argv []string `yaml:"argv" json:"argv"`
	// Timeout bounds this check's execution.
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
	// Env layers KEY=VALUE overrides onto the runner's base environment for
	// this check only.
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// Profile is a named ordered sequence of checks.
type Profile struct {
	Name   string      `yaml:"-" json:"-"`
	Checks []CheckSpec `yaml:"-" json:"-"`
}

// yamlTimeout lets profile files write "10m" the way config/project.example
// .yaml does; time.Duration itself decodes only from an integer number of
// nanoseconds, so CheckSpec.Timeout is unmarshalled through this shim type
// via UnmarshalYAML below.
func (c *CheckSpec) UnmarshalYAML(value *yaml.Node) error {
	type raw struct {
		ID      string            `yaml:"id"`
		Kind    string            `yaml:"kind"`
		Argv    []string          `yaml:"argv"`
		Timeout string            `yaml:"timeout"`
		Env     map[string]string `yaml:"env"`
	}
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	c.ID, c.Kind, c.Argv, c.Env = r.ID, r.Kind, r.Argv, r.Env
	if r.Timeout != "" {
		d, err := time.ParseDuration(r.Timeout)
		if err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "validation: invalid timeout %q", r.Timeout)
		}
		c.Timeout = d
	}
	return nil
}

// LoadProfiles parses a `validation:` document such as
// config/project.example.yaml's `validation.profiles` block:
//
//	validation:
//	  profiles:
//	    fast:
//	      - argv: ["go", "test", "./internal/..."]
//	        timeout: 10m
//	    full:
//	      - argv: ["go", "test", "./..."]
//	        timeout: 30m
//
// Two other shapes are also accepted, so a profile file can stand alone or
// live under a project's full configuration in whichever of these forms it
// was written:
//
//   - `validation:` directly holding the profile map, with no nested
//     `profiles:` key (`{"validation": {"fast": [...]}}`);
//   - no top-level `validation:` key at all — the document is already just
//     the profile map (`{"fast": [...], "full": [...]}`).
func LoadProfiles(data []byte) (map[string]Profile, error) {
	var nested struct {
		Validation struct {
			Profiles map[string][]CheckSpec `yaml:"profiles"`
		} `yaml:"validation"`
	}
	if err := yaml.Unmarshal(data, &nested); err == nil && nested.Validation.Profiles != nil {
		return buildProfiles(nested.Validation.Profiles)
	}

	var doc struct {
		Validation map[string][]CheckSpec `yaml:"validation"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "validation: parse profiles")
	}
	raw := doc.Validation
	if raw == nil {
		var direct map[string][]CheckSpec
		if err := yaml.Unmarshal(data, &direct); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "validation: parse profiles")
		}
		raw = direct
	}
	return buildProfiles(raw)
}

func buildProfiles(raw map[string][]CheckSpec) (map[string]Profile, error) {
	out := make(map[string]Profile, len(raw))
	for name, checks := range raw {
		profile := Profile{Name: name, Checks: checks}
		if err := profile.Validate(); err != nil {
			return nil, err
		}
		for i := range profile.Checks {
			if profile.Checks[i].ID == "" {
				profile.Checks[i].ID = defaultCheckID(profile.Checks[i])
			}
			if profile.Checks[i].Kind == "" {
				profile.Checks[i].Kind = profile.Checks[i].ID
			}
		}
		out[name] = profile
	}
	return out, nil
}

// Validate checks structural constraints LoadProfiles cannot express through
// decoding alone.
func (p Profile) Validate() error {
	if p.Name == "" {
		return errs.New(errs.CategoryInvalidArgument, "validation: profile name is required")
	}
	if len(p.Checks) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "validation: profile %q has no checks", p.Name)
	}
	seen := make(map[string]int, len(p.Checks))
	for i, c := range p.Checks {
		if len(c.Argv) == 0 {
			return errs.New(errs.CategoryInvalidArgument, "validation: profile %q check %d has an empty argv", p.Name, i)
		}
		if c.Timeout <= 0 {
			return errs.New(errs.CategoryInvalidArgument,
				"validation: profile %q check %d (%v) requires a positive timeout", p.Name, i, c.Argv)
		}
		// The effective ID (explicit, or the joined-argv default every
		// check will actually be assigned) must be unique within the
		// profile: ValidationCompleted.FailedChecks and every stored
		// CheckResult are keyed by ID, and a collision - e.g. two checks
		// both defaulting to "go" from "go test ..." and "go vet ..." -
		// would make a journal reader unable to tell which check failed.
		id := c.ID
		if id == "" {
			id = defaultCheckID(c)
		}
		if prev, ok := seen[id]; ok {
			return errs.New(errs.CategoryInvalidArgument,
				"validation: profile %q checks %d and %d both resolve to check id %q; give one an explicit id",
				p.Name, prev, i, id)
		}
		seen[id] = i
	}
	return nil
}

// defaultCheckID is the ID a check without an explicit one is assigned: its
// full argv joined by "-", so that two checks sharing an executable name
// (e.g. "go test ./..." and "go vet ./...") do not collide on the
// executable name alone. Validate (called by both LoadProfiles and
// RunProfile) rejects any profile where this would still produce a
// duplicate ID, so every check ID persisted in evidence is unambiguous.
func defaultCheckID(c CheckSpec) string {
	if len(c.Argv) > 0 {
		return strings.Join(c.Argv, "-")
	}
	return ""
}

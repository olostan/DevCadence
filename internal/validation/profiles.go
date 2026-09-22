// Package validation implements validation-profile loading and execution
// (docs/IMPLEMENTATION_PLAN.md M2, docs/PROTOCOLS.md §10, ADR-0015, ADR-0016).
//
// A profile is a named, ordered list of deterministic checks and optional
// supervised test services. Execution runs each check through the controlled
// process runner, captures stdout/stderr as artifacts, and builds the real M1
// protocol.ValidationResult — never a placeholder digest or a model-authored
// summary standing in for tool output (DCI-041).
package validation

import (
	"os"
	"path/filepath"
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
	// ModuleID optionally associates this check with a specific monorepo module.
	ModuleID string `yaml:"module_id,omitempty" json:"module_id,omitempty"`
	// Dir is an optional working directory relative to the worktree or module root.
	Dir string `yaml:"dir,omitempty" json:"dir,omitempty"`
	// Argv is the exact command: executable followed by arguments. It is
	// never a shell string (docs/SECURITY.md §5).
	Argv []string `yaml:"argv" json:"argv"`
	// Timeout bounds this check's execution.
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
	// Env layers KEY=VALUE overrides onto the runner's base environment for
	// this check only.
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// PortHandoffMode specifies how an allocated port is handed to a service.
type PortHandoffMode string

const (
	PortHandoffEnvVar            PortHandoffMode = "env_var"
	PortHandoffCLIFlag           PortHandoffMode = "cli_flag"
	PortHandoffSocketInheritance PortHandoffMode = "socket_inheritance"
)

// PortConfig configures how a service receives its network port.
type PortConfig struct {
	Mode         PortHandoffMode `yaml:"mode" json:"mode"`
	EnvVarName   string          `yaml:"env_var_name,omitempty" json:"env_var_name,omitempty"`
	FlagTemplate string          `yaml:"flag_template,omitempty" json:"flag_template,omitempty"`
}

// ReadinessProbe configures how the supervisor verifies service readiness.
type ReadinessProbe struct {
	Kind           string        `yaml:"kind" json:"kind"` // "tcp_port", "http_get", "process_output"
	Path           string        `yaml:"path,omitempty" json:"path,omitempty"`
	ExpectedStatus int           `yaml:"expected_status,omitempty" json:"expected_status,omitempty"`
	ExpectedBody   string        `yaml:"expected_body,omitempty" json:"expected_body,omitempty"`
	OutputPattern  string        `yaml:"output_pattern,omitempty" json:"output_pattern,omitempty"`
	Interval       time.Duration `yaml:"interval,omitempty" json:"interval,omitempty"`
}

// ServiceSpec configures an auxiliary supervised background service (ADR-0016).
type ServiceSpec struct {
	ID              string            `yaml:"id" json:"id"`
	ModuleID        string            `yaml:"module_id,omitempty" json:"module_id,omitempty"`
	Dir             string            `yaml:"dir,omitempty" json:"dir,omitempty"`
	Argv            []string          `yaml:"argv" json:"argv"`
	Env             map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	StartupTimeout  time.Duration     `yaml:"startup_timeout" json:"startup_timeout"`
	ShutdownTimeout time.Duration     `yaml:"shutdown_timeout" json:"shutdown_timeout"`
	MaxLifetime     time.Duration     `yaml:"max_lifetime,omitempty" json:"max_lifetime,omitempty"`
	ReadinessProbe  ReadinessProbe    `yaml:"readiness" json:"readiness"`
	PortConfig      PortConfig        `yaml:"port_config" json:"port_config"`
}

// Profile is a named ordered sequence of checks and optional companion services.
type Profile struct {
	Name     string        `yaml:"-" json:"name"`
	ModuleID string        `yaml:"module_id,omitempty" json:"module_id,omitempty"`
	Services []ServiceSpec `yaml:"services,omitempty" json:"services,omitempty"`
	Checks   []CheckSpec   `yaml:"checks,omitempty" json:"checks"`
}

// UnmarshalYAML unmarshals CheckSpec supporting duration string timeouts.
func (c *CheckSpec) UnmarshalYAML(value *yaml.Node) error {
	type raw struct {
		ID       string            `yaml:"id"`
		Kind     string            `yaml:"kind"`
		ModuleID string            `yaml:"module_id"`
		Dir      string            `yaml:"dir"`
		Argv     []string          `yaml:"argv"`
		Timeout  string            `yaml:"timeout"`
		Env      map[string]string `yaml:"env"`
	}
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	c.ID, c.Kind, c.ModuleID, c.Dir, c.Argv, c.Env = r.ID, r.Kind, r.ModuleID, r.Dir, r.Argv, r.Env
	if r.Timeout != "" {
		d, err := time.ParseDuration(r.Timeout)
		if err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "validation: invalid timeout %q", r.Timeout)
		}
		c.Timeout = d
	}
	return nil
}

// UnmarshalYAML unmarshals ServiceSpec supporting duration string timeouts.
func (s *ServiceSpec) UnmarshalYAML(value *yaml.Node) error {
	type rawProbe struct {
		Kind           string `yaml:"kind"`
		Path           string `yaml:"path"`
		ExpectedStatus int    `yaml:"expected_status"`
		ExpectedBody   string `yaml:"expected_body"`
		OutputPattern  string `yaml:"output_pattern"`
		Interval       string `yaml:"interval"`
	}
	type raw struct {
		ID              string            `yaml:"id"`
		ModuleID        string            `yaml:"module_id"`
		Dir             string            `yaml:"dir"`
		Argv            []string          `yaml:"argv"`
		Env             map[string]string `yaml:"env"`
		StartupTimeout  string            `yaml:"startup_timeout"`
		ShutdownTimeout string            `yaml:"shutdown_timeout"`
		MaxLifetime     string            `yaml:"max_lifetime"`
		ReadinessProbe  rawProbe          `yaml:"readiness"`
		PortConfig      PortConfig        `yaml:"port_config"`
	}
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	s.ID, s.ModuleID, s.Dir, s.Argv, s.Env, s.PortConfig = r.ID, r.ModuleID, r.Dir, r.Argv, r.Env, r.PortConfig
	s.ReadinessProbe = ReadinessProbe{
		Kind:           r.ReadinessProbe.Kind,
		Path:           r.ReadinessProbe.Path,
		ExpectedStatus: r.ReadinessProbe.ExpectedStatus,
		ExpectedBody:   r.ReadinessProbe.ExpectedBody,
		OutputPattern:  r.ReadinessProbe.OutputPattern,
	}
	if r.ReadinessProbe.Interval != "" {
		d, err := time.ParseDuration(r.ReadinessProbe.Interval)
		if err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "validation: invalid readiness interval %q", r.ReadinessProbe.Interval)
		}
		s.ReadinessProbe.Interval = d
	}
	if r.StartupTimeout != "" {
		d, err := time.ParseDuration(r.StartupTimeout)
		if err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "validation: invalid startup_timeout %q", r.StartupTimeout)
		}
		s.StartupTimeout = d
	}
	if r.ShutdownTimeout != "" {
		d, err := time.ParseDuration(r.ShutdownTimeout)
		if err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "validation: invalid shutdown_timeout %q", r.ShutdownTimeout)
		}
		s.ShutdownTimeout = d
	}
	if r.MaxLifetime != "" {
		d, err := time.ParseDuration(r.MaxLifetime)
		if err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "validation: invalid max_lifetime %q", r.MaxLifetime)
		}
		s.MaxLifetime = d
	}
	return nil
}

// LoadProfiles parses a validation document in any supported shape:
//
//  1. Nested profiles block:
//     validation:
//     profiles:
//     fast: [...]
//  2. Direct validation map:
//     validation:
//     fast: [...]
//  3. Raw map of profile names to check lists or profile objects.
func LoadProfiles(data []byte) (map[string]Profile, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "validation: parse profiles")
	}

	targetMap := findProfilesMapping(&root)
	if targetMap == nil || targetMap.Kind != yaml.MappingNode {
		return nil, errs.New(errs.CategoryInvalidArgument, "validation: no profile definitions found")
	}

	out := make(map[string]Profile, len(targetMap.Content)/2)
	for i := 0; i < len(targetMap.Content); i += 2 {
		nameNode := targetMap.Content[i]
		valNode := targetMap.Content[i+1]
		name := nameNode.Value

		var profile Profile
		profile.Name = name

		if valNode.Kind == yaml.SequenceNode {
			// Legacy list of checks: fast: [ {argv: ...} ]
			var checks []CheckSpec
			if err := valNode.Decode(&checks); err != nil {
				return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "validation: decode profile %q checks", name)
			}
			profile.Checks = checks
		} else if valNode.Kind == yaml.MappingNode {
			// Object-shaped profile: fast: { checks: [...], services: [...] }
			type rawProfile struct {
				ModuleID string        `yaml:"module_id"`
				Services []ServiceSpec `yaml:"services"`
				Checks   []CheckSpec   `yaml:"checks"`
			}
			var r rawProfile
			if err := valNode.Decode(&r); err != nil {
				return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "validation: decode profile %q", name)
			}
			profile.ModuleID = r.ModuleID
			profile.Services = r.Services
			profile.Checks = r.Checks
		} else {
			return nil, errs.New(errs.CategoryInvalidArgument, "validation: profile %q must be a list of checks or a profile object", name)
		}

		if err := profile.Validate(); err != nil {
			return nil, err
		}
		for j := range profile.Checks {
			if profile.Checks[j].ID == "" {
				profile.Checks[j].ID = defaultCheckID(profile.Checks[j])
			}
			if profile.Checks[j].Kind == "" {
				profile.Checks[j].Kind = profile.Checks[j].ID
			}
		}
		out[name] = profile
	}
	return out, nil
}

// findProfilesMapping locates the map of profile definitions in the YAML AST.
func findProfilesMapping(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return findProfilesMapping(node.Content[0])
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]
		if key == "validation" && val.Kind == yaml.MappingNode {
			// Check for nested "profiles:"
			for j := 0; j < len(val.Content); j += 2 {
				if val.Content[j].Value == "profiles" && val.Content[j+1].Kind == yaml.MappingNode {
					return val.Content[j+1]
				}
			}
			return val
		}
		if key == "profiles" && val.Kind == yaml.MappingNode {
			return val
		}
	}
	// Direct map of profile names to specs
	return node
}

// Validate checks structural constraints.
func (p Profile) Validate() error {
	if p.Name == "" {
		return errs.New(errs.CategoryInvalidArgument, "validation: profile name is required")
	}
	if len(p.Checks) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "validation: profile %q has no checks", p.Name)
	}

	seenServices := make(map[string]int, len(p.Services))
	for i, s := range p.Services {
		if s.ID == "" {
			return errs.New(errs.CategoryInvalidArgument, "validation: profile %q service %d has an empty id", p.Name, i)
		}
		if len(s.Argv) == 0 {
			return errs.New(errs.CategoryInvalidArgument, "validation: profile %q service %q has an empty argv", p.Name, s.ID)
		}
		if s.StartupTimeout <= 0 {
			return errs.New(errs.CategoryInvalidArgument, "validation: profile %q service %q requires a positive startup_timeout", p.Name, s.ID)
		}
		if s.ShutdownTimeout <= 0 {
			return errs.New(errs.CategoryInvalidArgument, "validation: profile %q service %q requires a positive shutdown_timeout", p.Name, s.ID)
		}
		if prev, exists := seenServices[s.ID]; exists {
			return errs.New(errs.CategoryInvalidArgument, "validation: profile %q services %d and %d have duplicate id %q", p.Name, prev, i, s.ID)
		}
		seenServices[s.ID] = i
	}

	seenChecks := make(map[string]int, len(p.Checks))
	for i, c := range p.Checks {
		if len(c.Argv) == 0 {
			return errs.New(errs.CategoryInvalidArgument, "validation: profile %q check %d has an empty argv", p.Name, i)
		}
		if c.Timeout <= 0 {
			return errs.New(errs.CategoryInvalidArgument,
				"validation: profile %q check %d (%v) requires a positive timeout", p.Name, i, c.Argv)
		}
		id := c.ID
		if id == "" {
			id = defaultCheckID(c)
		}
		if prev, ok := seenChecks[id]; ok {
			return errs.New(errs.CategoryInvalidArgument,
				"validation: profile %q checks %d and %d both resolve to check id %q; give one an explicit id",
				p.Name, prev, i, id)
		}
		seenChecks[id] = i
	}
	return nil
}

// defaultCheckID derives an ID from argv joined by "-".
func defaultCheckID(c CheckSpec) string {
	if len(c.Argv) > 0 {
		return strings.Join(c.Argv, "-")
	}
	return ""
}

// ValidateDirContainment ensures that targetDir (joined with baseDir if relative)
// resolves to a directory strictly within baseDir, preventing symlink escapes and .. traversals.
func ValidateDirContainment(baseDir, targetDir string) (string, error) {
	if baseDir == "" {
		return "", errs.New(errs.CategoryInvalidArgument, "validation: base directory is required")
	}
	resolvedBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInvalidArgument, err, "validation: resolve base dir %q", baseDir)
	}

	var fullPath string
	if filepath.IsAbs(targetDir) {
		fullPath = filepath.Clean(targetDir)
	} else {
		fullPath = filepath.Clean(filepath.Join(resolvedBase, targetDir))
	}

	// Check containment before evaluating symlinks
	rel, err := filepath.Rel(resolvedBase, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errs.New(errs.CategoryPolicyDenied, "validation: target directory %q escapes base %q", targetDir, baseDir)
	}

	// Verify target exists and is a directory
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", errs.Wrap(errs.CategoryNotFound, err, "validation: directory %q does not exist", fullPath)
	}
	if !info.IsDir() {
		return "", errs.New(errs.CategoryInvalidArgument, "validation: path %q is not a directory", fullPath)
	}

	// Now evaluate symlinks on the target to ensure it doesn't escape out
	resolvedTarget, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInvalidArgument, err, "validation: resolve symlinks for %q", fullPath)
	}
	relTarget, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil || strings.HasPrefix(relTarget, "..") {
		return "", errs.New(errs.CategoryPolicyDenied, "validation: target directory %q resolves via symlink to %q escaping base %q", targetDir, resolvedTarget, baseDir)
	}

	return resolvedTarget, nil
}

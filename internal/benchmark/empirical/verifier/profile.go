package verifier

import (
	"bytes"
	"encoding/json"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

var forbiddenShells = map[string]bool{
	"sh":         true,
	"bash":       true,
	"zsh":        true,
	"cmd":        true,
	"cmd.exe":    true,
	"powershell": true,
	"pwsh":       true,
	"dash":       true,
	"csh":        true,
	"tcsh":       true,
	"ksh":        true,
	"fish":       true,
}

var sha256Pattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// LoadProfile parses and strictly validates a JSON verification profile document.
func LoadProfile(data []byte) (*VerificationProfile, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p VerificationProfile
	if err := dec.Decode(&p); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "profile: invalid json")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errs.New(errs.CategoryInvalidArgument, "profile: unexpected trailing tokens")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate checks that the verification profile conforms strictly to WP-M5-R3 requirements.
func (p VerificationProfile) Validate() error {
	if p.Version != "1.0" {
		return errs.New(errs.CategoryInvalidArgument, "profile: version must be %q, got %q", "1.0", p.Version)
	}
	if strings.TrimSpace(p.ProfileID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "profile: profile_id is required")
	}
	if len(p.Executables) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "profile: executables allowlist must not be empty")
	}

	execSet := make(map[string]bool, len(p.Executables))
	for _, exe := range p.Executables {
		exeTrimmed := strings.TrimSpace(exe)
		if exeTrimmed == "" {
			return errs.New(errs.CategoryInvalidArgument, "profile: executable name cannot be blank")
		}
		if strings.ContainsAny(exeTrimmed, "/\\") {
			return errs.New(errs.CategoryInvalidArgument, "profile: executable %q cannot contain path separators", exe)
		}
		lower := strings.ToLower(exeTrimmed)
		if forbiddenShells[lower] {
			return errs.New(errs.CategoryInvalidArgument, "profile: shell executable %q is forbidden in executables allowlist", exe)
		}
		if execSet[exeTrimmed] {
			return errs.New(errs.CategoryInvalidArgument, "profile: duplicate executable %q in executables allowlist", exe)
		}
		execSet[exeTrimmed] = true
	}

	if len(p.Tasks) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "profile: tasks must not be empty")
	}

	for i, task := range p.Tasks {
		if i > 0 && p.Tasks[i-1].TaskID >= task.TaskID {
			return errs.New(errs.CategoryInvalidArgument,
				"profile: tasks must be sorted ascending by task_id without duplicates; got %q after %q",
				task.TaskID, p.Tasks[i-1].TaskID)
		}
		if err := validateTaskVerification(task, execSet); err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "profile: task %q", task.TaskID)
		}
	}

	return nil
}

func validateTaskVerification(task TaskVerification, allowedExecs map[string]bool) error {
	if strings.TrimSpace(task.TaskID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "task_id is required")
	}
	if !sha256Pattern.MatchString(task.TaskDigest) {
		return errs.New(errs.CategoryInvalidArgument, "task_digest must be valid sha256 digest, got %q", task.TaskDigest)
	}
	if strings.TrimSpace(task.BaseCommit) == "" {
		return errs.New(errs.CategoryInvalidArgument, "base_commit is required")
	}

	checkMap := make(map[string]CheckSpec, len(task.Checks))
	for _, c := range task.Checks {
		if strings.TrimSpace(c.CheckID) == "" {
			return errs.New(errs.CategoryInvalidArgument, "check_id is required")
		}
		if _, exists := checkMap[c.CheckID]; exists {
			return errs.New(errs.CategoryInvalidArgument, "duplicate check_id %q", c.CheckID)
		}
		if err := validateCheckSpec(c, allowedExecs); err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "check %q", c.CheckID)
		}
		checkMap[c.CheckID] = c
	}

	switch task.Class {
	case "implementation":
		if len(task.WriteScope) == 0 {
			return errs.New(errs.CategoryInvalidArgument, "implementation task requires non-empty write_scope")
		}
		for _, ws := range task.WriteScope {
			if err := validateWriteScopeEntry(ws); err != nil {
				return err
			}
		}
		if len(task.Checks) == 0 {
			return errs.New(errs.CategoryInvalidArgument, "implementation task requires at least one check")
		}
		if len(task.AcceptanceCheckIDs) == 0 {
			return errs.New(errs.CategoryInvalidArgument, "implementation task requires at least one acceptance_check_id")
		}
		for _, accID := range task.AcceptanceCheckIDs {
			if _, ok := checkMap[accID]; !ok {
				return errs.New(errs.CategoryInvalidArgument, "acceptance_check_id %q references unknown check", accID)
			}
		}
		defectIDs := make(map[string]bool, len(task.Defects))
		for _, def := range task.Defects {
			if strings.TrimSpace(def.DefectID) == "" {
				return errs.New(errs.CategoryInvalidArgument, "defect_id is required")
			}
			if defectIDs[def.DefectID] {
				return errs.New(errs.CategoryInvalidArgument, "duplicate defect_id %q", def.DefectID)
			}
			defectIDs[def.DefectID] = true
			if def.Anchor != nil {
				return errs.New(errs.CategoryInvalidArgument, "defect %q: implementation defect probe must not specify anchor", def.DefectID)
			}
			if len(def.CatchCheckIDs) == 0 {
				return errs.New(errs.CategoryInvalidArgument, "defect %q: implementation defect probe requires catch_check_ids", def.DefectID)
			}
			for _, cid := range def.CatchCheckIDs {
				if _, ok := checkMap[cid]; !ok {
					return errs.New(errs.CategoryInvalidArgument, "defect %q: catch_check_id %q references unknown check", def.DefectID, cid)
				}
			}
		}

	case "review":
		if len(task.WriteScope) != 0 {
			return errs.New(errs.CategoryInvalidArgument, "review task write_scope must be empty")
		}
		if len(task.AcceptanceCheckIDs) != 0 {
			return errs.New(errs.CategoryInvalidArgument, "review task acceptance_check_ids must be empty")
		}
		defectIDs := make(map[string]bool, len(task.Defects))
		for _, def := range task.Defects {
			if strings.TrimSpace(def.DefectID) == "" {
				return errs.New(errs.CategoryInvalidArgument, "defect_id is required")
			}
			if defectIDs[def.DefectID] {
				return errs.New(errs.CategoryInvalidArgument, "duplicate defect_id %q", def.DefectID)
			}
			defectIDs[def.DefectID] = true
			if len(def.CatchCheckIDs) != 0 {
				return errs.New(errs.CategoryInvalidArgument, "defect %q: review defect probe must not specify catch_check_ids", def.DefectID)
			}
			if def.Anchor == nil {
				return errs.New(errs.CategoryInvalidArgument, "defect %q: review defect probe requires anchor", def.DefectID)
			}
			if err := validateReviewAnchor(*def.Anchor); err != nil {
				return errs.Wrap(errs.CategoryInvalidArgument, err, "defect %q anchor", def.DefectID)
			}
		}

	default:
		return errs.New(errs.CategoryInvalidArgument, "task class %q is not valid (must be implementation or review)", task.Class)
	}

	return nil
}

func validateWriteScopeEntry(ws string) error {
	trimmed := strings.TrimSpace(ws)
	if trimmed == "" {
		return errs.New(errs.CategoryInvalidArgument, "write_scope entry cannot be blank")
	}
	if strings.HasPrefix(trimmed, "/") {
		return errs.New(errs.CategoryInvalidArgument, "write_scope entry %q cannot be absolute", ws)
	}
	if strings.Contains(trimmed, "..") || strings.Contains(trimmed, "\\") {
		return errs.New(errs.CategoryInvalidArgument, "write_scope entry %q contains invalid path traversal", ws)
	}
	clean := path.Clean(trimmed)
	if strings.HasSuffix(trimmed, "/") && !strings.HasSuffix(clean, "/") {
		clean += "/"
	}
	if clean != trimmed {
		return errs.New(errs.CategoryInvalidArgument, "write_scope entry %q is not clean (expected %q)", ws, clean)
	}
	return nil
}

func validateReviewAnchor(a ReviewAnchor) error {
	p := strings.TrimSpace(a.Path)
	if p == "" {
		return errs.New(errs.CategoryInvalidArgument, "anchor path is required")
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "..") || strings.Contains(p, "\\") {
		return errs.New(errs.CategoryInvalidArgument, "anchor path %q is not a clean relative path", a.Path)
	}
	if clean := path.Clean(p); clean != p {
		return errs.New(errs.CategoryInvalidArgument, "anchor path %q is not clean (expected %q)", a.Path, clean)
	}
	if a.StartLine < 1 {
		return errs.New(errs.CategoryInvalidArgument, "anchor start_line must be >= 1, got %d", a.StartLine)
	}
	if a.EndLine < a.StartLine {
		return errs.New(errs.CategoryInvalidArgument, "anchor end_line (%d) must be >= start_line (%d)", a.EndLine, a.StartLine)
	}
	return nil
}

func validateCheckSpec(c CheckSpec, allowedExecs map[string]bool) error {
	if len(c.Argv) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "argv cannot be empty")
	}
	exe := c.Argv[0]
	if strings.TrimSpace(exe) == "" {
		return errs.New(errs.CategoryInvalidArgument, "argv[0] cannot be blank")
	}
	if strings.ContainsAny(exe, "/\\") {
		return errs.New(errs.CategoryInvalidArgument, "argv[0] %q cannot contain path separators", exe)
	}
	lower := strings.ToLower(exe)
	if forbiddenShells[lower] {
		return errs.New(errs.CategoryInvalidArgument, "shell executable %q is forbidden in argv[0]", exe)
	}
	if !allowedExecs[exe] {
		return errs.New(errs.CategoryInvalidArgument, "argv[0] %q is not in executables allowlist", exe)
	}
	for i, arg := range c.Argv {
		if containsDotDot(arg) {
			return errs.New(errs.CategoryInvalidArgument, "argv[%d] %q contains forbidden '..' traversal", i, arg)
		}
		if strings.Contains(arg, "://") {
			return errs.New(errs.CategoryInvalidArgument, "argv[%d] %q contains forbidden url scheme", i, arg)
		}
	}
	if c.Dir != "" && c.Dir != "." {
		if strings.HasPrefix(c.Dir, "/") || containsDotDot(c.Dir) || strings.Contains(c.Dir, "\\") {
			return errs.New(errs.CategoryInvalidArgument, "dir %q must be clean relative directory without '..'", c.Dir)
		}
		clean := path.Clean(c.Dir)
		if clean != c.Dir {
			return errs.New(errs.CategoryInvalidArgument, "dir %q is not clean (expected %q)", c.Dir, clean)
		}
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 600 {
		return errs.New(errs.CategoryInvalidArgument, "timeout_seconds must be between 1 and 600, got %d", c.TimeoutSeconds)
	}
	return nil
}

func containsDotDot(s string) bool {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	return false
}

// Digest computes the canonical SHA-256 digest of the verification profile.
func (p VerificationProfile) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return protocol.Digest(p)
}

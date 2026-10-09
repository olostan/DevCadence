package verifier_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/benchmark/empirical/verifier"
)

func validImplTask() verifier.TaskVerification {
	return verifier.TaskVerification{
		TaskID:     "task-01-impl",
		TaskDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Class:      "implementation",
		BaseCommit: "commit-base-01",
		WriteScope: []string{"src/", "pkg/file.go"},
		Checks: []verifier.CheckSpec{
			{
				CheckID:        "chk-test",
				Argv:           []string{"go", "test", "./..."},
				Dir:            ".",
				TimeoutSeconds: 30,
				ExpectExitCode: 0,
			},
			{
				CheckID:        "chk-probe",
				Argv:           []string{"git", "status"},
				Dir:            "",
				TimeoutSeconds: 10,
				ExpectExitCode: 0,
			},
		},
		AcceptanceCheckIDs: []string{"chk-test"},
		Defects: []verifier.DefectProbe{
			{
				DefectID:      "defect-01",
				CatchCheckIDs: []string{"chk-probe"},
			},
		},
	}
}

func validReviewTask() verifier.TaskVerification {
	return verifier.TaskVerification{
		TaskID:     "task-02-review",
		TaskDigest: "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Class:      "review",
		BaseCommit: "commit-base-02",
		Defects: []verifier.DefectProbe{
			{
				DefectID: "defect-rev-01",
				Anchor: &verifier.ReviewAnchor{
					Path:      "internal/auth/checker.go",
					StartLine: 10,
					EndLine:   25,
				},
			},
		},
	}
}

func validProfile() verifier.VerificationProfile {
	return verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "profile-2026-10",
		Executables: []string{"git", "go", "make"},
		Tasks: []verifier.TaskVerification{
			validImplTask(),
			validReviewTask(),
		},
	}
}

func TestProfile_Valid(t *testing.T) {
	p := validProfile()
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid profile, got: %v", err)
	}

	digest, err := p.Digest()
	if err != nil {
		t.Fatalf("Digest() failed: %v", err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("Digest() does not start with sha256: %s", digest)
	}

	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	loaded, err := verifier.LoadProfile(raw)
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	loadedDigest, err := loaded.Digest()
	if err != nil {
		t.Fatalf("loaded.Digest() failed: %v", err)
	}
	if digest != loadedDigest {
		t.Fatalf("digest mismatch between original and loaded: %s != %s", digest, loadedDigest)
	}
}

func TestProfile_RejectInvalidVersion(t *testing.T) {
	p := validProfile()
	p.Version = "2.0"
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for version != 1.0, got nil")
	}
}

func TestProfile_RejectEmptyProfileID(t *testing.T) {
	p := validProfile()
	p.ProfileID = "   "
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for empty profile_id, got nil")
	}
}

func TestProfile_RejectEmptyExecutables(t *testing.T) {
	p := validProfile()
	p.Executables = nil
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for empty executables, got nil")
	}
}

func TestProfile_RejectShellExecutables(t *testing.T) {
	shells := []string{"sh", "bash", "zsh", "cmd", "powershell", "dash", "csh", "tcsh", "fish"}
	for _, sh := range shells {
		p := validProfile()
		p.Executables = append(p.Executables, sh)
		if err := p.Validate(); err == nil {
			t.Errorf("expected error for shell executable %q, got nil", sh)
		}
	}
}

func TestProfile_RejectDuplicateExecutables(t *testing.T) {
	p := validProfile()
	p.Executables = []string{"go", "git", "go"}
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for duplicate executable, got nil")
	}
}

func TestProfile_RejectExecutableWithSlash(t *testing.T) {
	p := validProfile()
	p.Executables = []string{"/bin/go"}
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for executable with slash, got nil")
	}
}

func TestProfile_RejectUnsortedOrDuplicateTasks(t *testing.T) {
	p := validProfile()
	p.Tasks[0], p.Tasks[1] = p.Tasks[1], p.Tasks[0]
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for unsorted tasks, got nil")
	}

	p2 := validProfile()
	p2.Tasks[1].TaskID = p2.Tasks[0].TaskID
	if err := p2.Validate(); err == nil {
		t.Fatalf("expected error for duplicate task_id, got nil")
	}
}

func TestProfile_RejectInvalidTaskDigest(t *testing.T) {
	p := validProfile()
	p.Tasks[0].TaskDigest = "bad-digest"
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for bad task_digest, got nil")
	}
}

func TestProfile_RejectUnknownTaskClass(t *testing.T) {
	p := validProfile()
	p.Tasks[0].Class = "exploration"
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for unknown task class, got nil")
	}
}

func TestProfile_RejectImplementationInvalidWriteScope(t *testing.T) {
	cases := []struct {
		name  string
		scope []string
	}{
		{"empty", nil},
		{"blank entry", []string{"   "}},
		{"absolute path", []string{"/root/file.go"}},
		{"dotdot traversal", []string{"../escape.go"}},
		{"backslash", []string{"src\\file.go"}},
		{"unclean path", []string{"src/sub/../file.go"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validProfile()
			p.Tasks[0].WriteScope = tc.scope
			if err := p.Validate(); err == nil {
				t.Fatalf("expected error for write_scope %v, got nil", tc.scope)
			}
		})
	}
}

func TestProfile_RejectImplementationChecksAndAcceptance(t *testing.T) {
	p := validProfile()
	p.Tasks[0].Checks = nil
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for empty checks on impl task, got nil")
	}

	p2 := validProfile()
	p2.Tasks[0].AcceptanceCheckIDs = nil
	if err := p2.Validate(); err == nil {
		t.Fatalf("expected error for empty acceptance_check_ids on impl task, got nil")
	}

	p3 := validProfile()
	p3.Tasks[0].AcceptanceCheckIDs = []string{"non-existent"}
	if err := p3.Validate(); err == nil {
		t.Fatalf("expected error for unknown acceptance_check_id, got nil")
	}
}

func TestProfile_RejectImplementationDefectWithAnchor(t *testing.T) {
	p := validProfile()
	p.Tasks[0].Defects[0].Anchor = &verifier.ReviewAnchor{
		Path:      "file.go",
		StartLine: 1,
		EndLine:   5,
	}
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for impl defect with anchor, got nil")
	}
}

func TestProfile_RejectReviewWithWriteScopeOrAcceptance(t *testing.T) {
	p := validProfile()
	p.Tasks[1].WriteScope = []string{"file.go"}
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for review task with write_scope, got nil")
	}

	p2 := validProfile()
	p2.Tasks[1].AcceptanceCheckIDs = []string{"chk"}
	if err := p2.Validate(); err == nil {
		t.Fatalf("expected error for review task with acceptance_check_ids, got nil")
	}
}

func TestProfile_RejectReviewDefectInvalidAnchor(t *testing.T) {
	cases := []struct {
		name   string
		anchor *verifier.ReviewAnchor
	}{
		{"nil anchor", nil},
		{"empty path", &verifier.ReviewAnchor{Path: "", StartLine: 1, EndLine: 5}},
		{"absolute path", &verifier.ReviewAnchor{Path: "/etc/file.go", StartLine: 1, EndLine: 5}},
		{"dotdot path", &verifier.ReviewAnchor{Path: "../file.go", StartLine: 1, EndLine: 5}},
		{"zero startline", &verifier.ReviewAnchor{Path: "file.go", StartLine: 0, EndLine: 5}},
		{"endline before startline", &verifier.ReviewAnchor{Path: "file.go", StartLine: 10, EndLine: 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validProfile()
			p.Tasks[1].Defects[0].Anchor = tc.anchor
			if err := p.Validate(); err == nil {
				t.Fatalf("expected error for anchor %+v, got nil", tc.anchor)
			}
		})
	}
}

func TestProfile_RejectCheckWithForbiddenArgv(t *testing.T) {
	cases := []struct {
		name string
		argv []string
	}{
		{"empty argv", nil},
		{"shell in argv0", []string{"bash", "-c", "echo hello"}},
		{"unregistered argv0", []string{"python3", "script.py"}},
		{"dotdot in arg", []string{"go", "run", "../main.go"}},
		{"http url in arg", []string{"go", "get", "http://evil.com"}},
		{"https url in arg", []string{"go", "get", "https://evil.com"}},
		{"git url in arg", []string{"git", "clone", "git://evil.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validProfile()
			p.Tasks[0].Checks[0].Argv = tc.argv
			if err := p.Validate(); err == nil {
				t.Fatalf("expected error for argv %v, got nil", tc.argv)
			}
		})
	}
}

func TestProfile_RejectCheckWithInvalidDirOrTimeout(t *testing.T) {
	p := validProfile()
	p.Tasks[0].Checks[0].Dir = "../outside"
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for dotdot dir, got nil")
	}

	p2 := validProfile()
	p2.Tasks[0].Checks[0].Dir = "/absolute"
	if err := p2.Validate(); err == nil {
		t.Fatalf("expected error for absolute dir, got nil")
	}

	p3 := validProfile()
	p3.Tasks[0].Checks[0].TimeoutSeconds = 0
	if err := p3.Validate(); err == nil {
		t.Fatalf("expected error for timeout 0, got nil")
	}

	p4 := validProfile()
	p4.Tasks[0].Checks[0].TimeoutSeconds = 601
	if err := p4.Validate(); err == nil {
		t.Fatalf("expected error for timeout 601, got nil")
	}
}

func TestProfile_LoadProfile_RejectUnknownFields(t *testing.T) {
	jsonStr := `{"version":"1.0","profile_id":"p1","executables":["go"],"tasks":[],"unknown_field":true}`
	if _, err := verifier.LoadProfile([]byte(jsonStr)); err == nil {
		t.Fatalf("expected error for unknown fields, got nil")
	}
}

func TestProfile_LoadProfile_RejectTrailingTokens(t *testing.T) {
	p := validProfile()
	raw, _ := json.Marshal(p)
	raw = append(raw, []byte("   extra")...)
	if _, err := verifier.LoadProfile(raw); err == nil {
		t.Fatalf("expected error for trailing tokens, got nil")
	}
}

func TestProfile_LoadProfile_RejectInvalidProfile(t *testing.T) {
	p := validProfile()
	p.Version = "invalid"
	raw, _ := json.Marshal(p)
	if _, err := verifier.LoadProfile(raw); err == nil {
		t.Fatalf("expected error when validating loaded profile, got nil")
	}
}

func TestProfile_RejectBlankExecutable(t *testing.T) {
	p := validProfile()
	p.Executables = []string{"git", "   "}
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for blank executable, got nil")
	}
}

func TestProfile_RejectEmptyTasks(t *testing.T) {
	p := validProfile()
	p.Tasks = nil
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for empty tasks, got nil")
	}
}

func TestProfile_RejectTaskMissingIDOrBaseCommit(t *testing.T) {
	p := validProfile()
	p.Tasks[0].TaskID = "   "
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for blank task_id, got nil")
	}

	p2 := validProfile()
	p2.Tasks[0].BaseCommit = ""
	if err := p2.Validate(); err == nil {
		t.Fatalf("expected error for blank base_commit, got nil")
	}
}

func TestProfile_RejectCheckMissingIDOrDuplicate(t *testing.T) {
	p := validProfile()
	p.Tasks[0].Checks[0].CheckID = "  "
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for blank check_id, got nil")
	}

	p2 := validProfile()
	p2.Tasks[0].Checks[1].CheckID = p2.Tasks[0].Checks[0].CheckID
	if err := p2.Validate(); err == nil {
		t.Fatalf("expected error for duplicate check_id, got nil")
	}
}

func TestProfile_RejectImplementationDefectValidationErrors(t *testing.T) {
	// Blank defect_id
	p := validProfile()
	p.Tasks[0].Defects[0].DefectID = "  "
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for blank defect_id, got nil")
	}

	// Duplicate defect_id
	p2 := validProfile()
	p2.Tasks[0].Defects = append(p2.Tasks[0].Defects, p2.Tasks[0].Defects[0])
	if err := p2.Validate(); err == nil {
		t.Fatalf("expected error for duplicate defect_id, got nil")
	}

	// Empty catch_check_ids
	p3 := validProfile()
	p3.Tasks[0].Defects[0].CatchCheckIDs = nil
	if err := p3.Validate(); err == nil {
		t.Fatalf("expected error for empty catch_check_ids, got nil")
	}

	// Unknown catch_check_ids
	p4 := validProfile()
	p4.Tasks[0].Defects[0].CatchCheckIDs = []string{"non-existent-check"}
	if err := p4.Validate(); err == nil {
		t.Fatalf("expected error for unknown catch_check_id, got nil")
	}
}

func TestProfile_RejectReviewDefectValidationErrors(t *testing.T) {
	// Blank defect_id
	p := validProfile()
	p.Tasks[1].Defects[0].DefectID = " "
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for blank review defect_id, got nil")
	}

	// Duplicate defect_id
	p2 := validProfile()
	p2.Tasks[1].Defects = append(p2.Tasks[1].Defects, p2.Tasks[1].Defects[0])
	if err := p2.Validate(); err == nil {
		t.Fatalf("expected error for duplicate review defect_id, got nil")
	}

	// Specified catch_check_ids in review defect
	p3 := validProfile()
	p3.Tasks[1].Defects[0].CatchCheckIDs = []string{"some-check"}
	if err := p3.Validate(); err == nil {
		t.Fatalf("expected error when review defect specifies catch_check_ids, got nil")
	}
}

func TestProfile_RejectUncleanPathsAndArgvErrors(t *testing.T) {
	// Unclean write scope
	p := validProfile()
	p.Tasks[0].WriteScope = []string{"src/./sub/"}
	if err := p.Validate(); err == nil {
		t.Fatalf("expected error for unclean write scope, got nil")
	}

	// Unclean review anchor path
	p2 := validProfile()
	p2.Tasks[1].Defects[0].Anchor.Path = "internal/./auth/checker.go"
	if err := p2.Validate(); err == nil {
		t.Fatalf("expected error for unclean review anchor path, got nil")
	}

	// Blank argv[0]
	p3 := validProfile()
	p3.Tasks[0].Checks[0].Argv = []string{"  "}
	if err := p3.Validate(); err == nil {
		t.Fatalf("expected error for blank argv[0], got nil")
	}

	// Path separator in argv[0]
	p4 := validProfile()
	p4.Tasks[0].Checks[0].Argv = []string{"bin/go"}
	if err := p4.Validate(); err == nil {
		t.Fatalf("expected error for path separator in argv[0], got nil")
	}

	// Unclean check Dir
	p5 := validProfile()
	p5.Tasks[0].Checks[0].Dir = "sub/./dir"
	if err := p5.Validate(); err == nil {
		t.Fatalf("expected error for unclean check Dir, got nil")
	}

	// Backslash in check Dir
	p6 := validProfile()
	p6.Tasks[0].Checks[0].Dir = "sub\\dir"
	if err := p6.Validate(); err == nil {
		t.Fatalf("expected error for backslash in check Dir, got nil")
	}
}

func TestProfile_Digest_ErrorOnInvalidProfile(t *testing.T) {
	p := validProfile()
	p.Version = "invalid"
	if _, err := p.Digest(); err == nil {
		t.Fatalf("expected error from Digest() on invalid profile, got nil")
	}
}

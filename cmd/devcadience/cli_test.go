package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// cli runs the CLI in-process and returns stdout, stderr and the error.
//
// Running in-process rather than building a binary keeps the suite fast and
// lets it assert on error categories rather than only on exit codes.
type cli struct {
	t  *testing.T
	db string
}

func newCLI(t *testing.T) *cli {
	t.Helper()
	return &cli{t: t, db: filepath.Join(t.TempDir(), "control-plane.db")}
}

func (c *cli) run(args ...string) (string, string, error) {
	c.t.Helper()
	var stdout, stderr bytes.Buffer
	full := append([]string{"-db", c.db}, args...)
	err := run(context.Background(), full, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func (c *cli) mustRun(args ...string) string {
	c.t.Helper()
	stdout, stderr, err := c.run(args...)
	if err != nil {
		c.t.Fatalf("devcadience %s: %v\nstderr: %s", strings.Join(args, " "), err, stderr)
	}
	return stdout
}

func TestCLIVersion(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("version")
	if !strings.HasPrefix(out, "devcadience ") {
		t.Fatalf("version output = %q", out)
	}
}

func TestCLIUnknownSubcommandFails(t *testing.T) {
	c := newCLI(t)
	if _, _, err := c.run("teleport"); err == nil {
		t.Fatal("an unknown subcommand succeeded")
	}
}

// TestCLIDrivesASyntheticProjectToDone is the operator-facing form of the M1
// exit criterion: the whole lifecycle is reachable from the command line
// with no model runtime present.
func TestCLIDrivesASyntheticProjectToDone(t *testing.T) {
	c := newCLI(t)
	c.mustRun("project", "init", "-id", "demo", "-name", "Demo",
		"-milestone-id", "M1", "-milestone-title", "Domain core",
		"-invariants", "DCI-053,DCI-020")
	c.mustRun("task", "create", "-project", "demo", "-alias", "DC-001",
		"-title", "Bounded journal reads", "-class", "systemic")

	steps := [][]string{
		{"TaskDesignStarted", `{"reason":"initial design"}`},
		{"WorkPackageApproved", `{"work_package_id":"wp_1","work_package_version":1,` +
			`"record_digest":"sha256:0","project_state_revision":"ps_000000003",` +
			`"base_commit":"91acd8273f1","change_class":"systemic"}`},
		{"TaskDelegated", `{"work_package_id":"wp_1","worker_role":"implementer","max_attempts":3}`},
		{"AttemptStarted", `{"attempt_id":"att_1","work_package_id":"wp_1","work_package_version":1,` +
			`"project_state_revision":"ps_000000003","worker_role":"implementer"}`},
		{"CandidateProduced", `{"attempt_id":"att_1","candidate_commit":"cafebabe1234",` +
			`"summary":"implemented"}`},
		{"ValidationCompleted", `{"attempt_id":"att_1","validation_id":"val_1","scope":"attempt",` +
			`"status":"pass","commit":"cafebabe1234","record_digest":"sha256:0"}`},
		{"ReviewCompleted", `{"attempt_id":"att_1","review_id":"rev_1","dimension":"correctness",` +
			`"verdict":"pass","record_digest":"sha256:0"}`},
		{"ChangeAccepted", `{"attempt_id":"att_1","work_package_id":"wp_1",` +
			`"candidate_commit":"cafebabe1234","semantic_summary":"Bounded reads.",` +
			`"validation_ids":["val_1"],"review_ids":["rev_1"],` +
			`"decided_by":"principal"}`},
		{"IntegrationStarted", `{"integration_id":"int_1"}`},
		{"IntegrationValidationStarted", `{"integration_id":"int_1","integrated_commit":"deadbeef9988"}`},
		{"ValidationCompleted", `{"validation_id":"val_2","scope":"integration","status":"pass",` +
			`"commit":"deadbeef9988","record_digest":"sha256:0"}`},
	}
	for _, step := range steps {
		c.mustRun("event", "append", "-project", "demo", "-type", step[0],
			"-task", "DC-001", "-payload", step[1])
	}

	var projectState map[string]any
	if err := json.Unmarshal([]byte(c.mustRun("state", "show", "-project", "demo")), &projectState); err != nil {
		t.Fatalf("state show did not emit JSON: %v", err)
	}
	milestone := projectState["milestone"].(map[string]any)
	if milestone["completed_tasks"].(float64) != 1 {
		t.Fatalf("completed tasks = %v, want 1", milestone["completed_tasks"])
	}
	git := projectState["git"].(map[string]any)
	if git["accepted_commit"] != "deadbeef9988" {
		t.Fatalf("accepted commit = %v", git["accepted_commit"])
	}

	if out := c.mustRun("task", "list", "-project", "demo"); !strings.Contains(out, "done") {
		t.Fatalf("task list = %q, want the task in state done", out)
	}
	if out := c.mustRun("task", "show", "-project", "demo", "-task", "DC-001"); !strings.Contains(out, "att_1") {
		t.Fatalf("task show did not report the attempt:\n%s", out)
	}

	// Rebuilding from the journal must not change the state the CLI reports.
	before := c.mustRun("state", "show", "-project", "demo")
	c.mustRun("state", "rebuild", "-project", "demo")
	after := c.mustRun("state", "show", "-project", "demo")
	if before != after {
		t.Fatalf("rebuild changed the reported state:\n%s\n%s", before, after)
	}
}

func TestCLIRefusesAnIllegalTransition(t *testing.T) {
	c := newCLI(t)
	c.mustRun("project", "init", "-id", "demo", "-milestone-id", "M1", "-milestone-title", "Domain core")
	c.mustRun("task", "create", "-project", "demo", "-alias", "DC-001", "-title", "x")

	_, _, err := c.run("event", "append", "-project", "demo", "-type", "TaskDelegated",
		"-task", "DC-001", "-payload", `{"work_package_id":"wp_1","worker_role":"implementer"}`)
	if err == nil {
		t.Fatal("an illegal transition was accepted from the CLI")
	}
	if code := exitCode(err); code != 4 {
		t.Fatalf("exit code = %d, want 4 for an invalid transition", code)
	}
	// The journal must be unchanged.
	if out := c.mustRun("events", "list", "-project", "demo"); strings.Contains(out, "TaskDelegated") {
		t.Fatalf("the refused event was written to the journal:\n%s", out)
	}
}

func TestCLIRefusesAnUnknownEventType(t *testing.T) {
	c := newCLI(t)
	c.mustRun("project", "init", "-id", "demo", "-milestone-id", "M1", "-milestone-title", "Domain core")
	_, _, err := c.run("event", "append", "-project", "demo", "-type", "Teleported", "-payload", `{}`)
	if err == nil {
		t.Fatal("an unregistered event type was accepted")
	}
	if code := exitCode(err); code != 5 {
		t.Fatalf("exit code = %d, want 5 for an unsupported schema condition", code)
	}
}

func TestCLIRefusesAnUnknownPayloadField(t *testing.T) {
	c := newCLI(t)
	c.mustRun("project", "init", "-id", "demo", "-milestone-id", "M1", "-milestone-title", "Domain core")
	c.mustRun("task", "create", "-project", "demo", "-alias", "DC-001", "-title", "x")
	_, _, err := c.run("event", "append", "-project", "demo", "-type", "TaskDesignStarted",
		"-task", "DC-001", "-payload", `{"reason":"x","surprise":true}`)
	if err == nil {
		t.Fatal("an unknown payload field was accepted")
	}
}

func TestCLIReadOnlyCommandsDoNotCreateADatabase(t *testing.T) {
	c := newCLI(t)
	// A typo in -db must report a missing project, not silently create one.
	_, _, err := c.run("state", "show", "-project", "demo")
	if err == nil {
		t.Fatal("state show succeeded against a database that was never initialised")
	}
	if code := exitCode(err); code != 3 {
		t.Fatalf("exit code = %d, want 3 for not-found", code)
	}
}

func TestCLISchemaValidateChecksFixtures(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("schema", "validate", "../../fixtures/protocol/project-state.valid.json")
	if !strings.Contains(out, "ok") {
		t.Fatalf("schema validate output = %q", out)
	}
	stdout, _, err := c.run("schema", "validate", "../../fixtures/protocol/project-state.invalid-unknown-field.json")
	if err == nil {
		t.Fatalf("an invalid fixture passed validation:\n%s", stdout)
	}
}

func TestCLIMigrateStatusReportsAppliedMigrations(t *testing.T) {
	c := newCLI(t)
	c.mustRun("project", "init", "-id", "demo", "-milestone-id", "M1", "-milestone-title", "Domain core")
	out := c.mustRun("migrate", "status")
	if !strings.Contains(out, "applied") {
		t.Fatalf("migrate status = %q", out)
	}
}

func TestCLIEventTypesListsTheVocabulary(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("event", "types")
	for _, want := range []string{"ProjectInitialized", "TaskCreated", "ChangeAccepted", "EscalationRaised"} {
		if !strings.Contains(out, want) {
			t.Fatalf("event types output is missing %s:\n%s", want, out)
		}
	}
}

func TestCLITaskStatesPrintsTheLifecycle(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("task", "states")
	for _, want := range []string{"proposed", "integration_validating", "blocked", "done"} {
		if !strings.Contains(out, want) {
			t.Fatalf("task states output is missing %s:\n%s", want, out)
		}
	}
}
